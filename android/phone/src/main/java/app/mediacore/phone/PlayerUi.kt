package app.mediacore.phone

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import kotlinx.coroutines.Job
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlin.math.abs

/** Two taps this close together are a double tap; until then a tap might still become one. */
private const val DoubleTapMs = 280L
/** After a skip, each further tap adds another. */
private const val SkipRunMs = 800L
/** A drag across the whole screen moves this far through the film. The bar is for long jumps. */
private const val ScreenWidthMs = 360_000L
/** Pictures this far apart either side of the big one, so the strip shows where you're heading. */
private const val StripStepMs = 30_000L

private val Speeds = listOf(0.75f, 1f, 1.25f, 1.5f, 2f)

/**
 * Nothing on screen while playing. A tap shows the controls, a double tap on either side skips 10 s
 * and further taps add up, and a sideways drag anywhere moves through the film with the picture strip.
 */
@Composable
fun PlayerOverlay(st: PlayerState, a: PlayerActions, onChrome: (Boolean) -> Unit) {
    val chrome = st.mode != Mode.Hidden
    val full = st.mode == Mode.Full && st.sheet == null
    LaunchedEffect(full) { onChrome(full) }

    Box(Modifier.fillMaxSize().playerGestures(st, a)) {
        AnimatedVisibility(!st.firstFrame && st.error == null, enter = fadeIn(tween(0)), exit = fadeOut(tween(Motion.FadeOutMs))) {
            Box(Modifier.fillMaxSize().background(Base)) {
                st.cover?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
                Box(Modifier.fillMaxSize().background(Base.copy(alpha = 0.6f)))
                LoadingRing(Modifier.align(Alignment.Center))
            }
        }
        QuietLoading(st.firstFrame && st.buffering && st.error == null && !chrome, Modifier.align(Alignment.Center))

        AnimatedVisibility(chrome, enter = fadeIn(tween(Motion.FadeInMs)), exit = fadeOut(tween(Motion.FadeOutMs))) {
            Controls(st, a)
        }
        SkipMark(st.skipped)

        val offer = st.offer
        if (offer != null && st.sheet == null && st.error == null && st.mode != Mode.Scrub) {
            Box(Modifier.align(Alignment.BottomEnd).safeDrawingPadding().padding(end = 24.dp, bottom = if (full) 104.dp else 24.dp)) {
                if (offer.kind == "next") NextCard(offer, st.countdown, a::takeOffer)
                else Box(
                    Modifier.height(ButtonHeight).clip(CardShape).background(Ink).tap(onClick = a::takeOffer).padding(horizontal = 16.dp),
                    contentAlignment = Alignment.Center,
                ) { Text(offer.title, style = Type.Label, color = Base) }
            }
        }

        when (st.sheet) {
            Sheet.Speed -> Panel("Speed", Speeds.map { s -> TrackOption(speedLabel(s), "", s == st.speed) { a.setSpeed(s) } }, a)
            Sheet.Audio -> Panel("Audio", st.audio, a)
            Sheet.Subtitles -> Panel("Subtitles", st.text, a)
            Sheet.Episodes -> Panel("Episodes", st.episodes, a)
            null -> {}
        }
        st.error?.let { ErrorCard(it, a) }
    }
}

/** Taps and drags on the picture itself; the controls' own buttons take theirs first. */
private fun Modifier.playerGestures(st: PlayerState, a: PlayerActions): Modifier = pointerInput(Unit) {
    var lastTap = 0L
    var lastSide = 0
    var lastSkip = 0L
    var single: Job? = null
    coroutineScope {
        val scope = this
        awaitEachGesture {
            val down = awaitFirstDown()
            val slop = viewConfiguration.touchSlop
            var scrubbing = false
            var from = 0L
            var abandoned = false // a mostly upright drag: not ours
            while (true) {
                val change = awaitPointerEvent().changes.firstOrNull { it.id == down.id } ?: break
                if (!change.pressed) {
                    change.consume()
                    if (scrubbing) {
                        a.scrubEnd()
                    } else if (!abandoned && st.error == null) {
                        val now = System.currentTimeMillis()
                        val side = if (change.position.x < size.width / 2) -1 else 1
                        when {
                            st.sheet != null -> a.closeSheet()
                            now - lastSkip < SkipRunMs -> { a.skip(side); lastSkip = now }
                            single?.isActive == true && side == lastSide && now - lastTap < DoubleTapMs -> {
                                single?.cancel()
                                a.skip(side)
                                lastSkip = now
                            }
                            else -> {
                                single?.cancel()
                                single = scope.launch { delay(DoubleTapMs); a.tapped() }
                            }
                        }
                        lastTap = now
                        lastSide = side
                    }
                    return@awaitEachGesture
                }
                val dx = change.position.x - down.position.x
                val dy = change.position.y - down.position.y
                if (!scrubbing && !abandoned) {
                    if (abs(dx) > slop * 1.5f && abs(dx) > abs(dy) && st.dur > 0 && st.sheet == null && st.error == null) {
                        single?.cancel()
                        a.scrubStart()
                        from = st.scrub ?: st.pos
                        scrubbing = true
                    } else if (abs(dy) > slop * 3) {
                        abandoned = true
                    }
                }
                if (scrubbing) {
                    change.consume()
                    a.scrubMove(from + (dx / size.width * ScreenWidthMs).toLong())
                }
            }
            // the finger was taken away from us (a system gesture, say): land where it was
            if (scrubbing) a.scrubEnd()
        }
    }
}

/** After a double tap: two arrows and the seconds, small, on the side you went. Stays still while taps add up. */
@Composable
private fun BoxScope.SkipMark(seconds: Int) {
    val last = remember { intArrayOf(0) }
    if (seconds != 0) last[0] = seconds
    AnimatedVisibility(
        seconds != 0,
        modifier = Modifier.align(if (last[0] > 0) Alignment.CenterEnd else Alignment.CenterStart).safeDrawingPadding().padding(horizontal = 24.dp),
        enter = fadeIn(tween(Motion.FadeInMs)), exit = fadeOut(tween(Motion.FadeOutMs)),
    ) {
        Row(
            Modifier.background(Base.copy(alpha = 0.72f), CardShape).padding(start = 8.dp, end = 12.dp, top = 4.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Icon(if (last[0] > 0) AppIcons.SkipAhead else AppIcons.SkipBack, null, tint = Ink, modifier = Modifier.size(24.dp))
            Text("${abs(last[0])}", style = Type.Label.figures())
        }
    }
}

/** Shows only if the wait is real: a skip inside the buffer lands before this ever appears. */
@Composable
private fun QuietLoading(on: Boolean, modifier: Modifier) {
    var show by remember { mutableStateOf(false) }
    LaunchedEffect(on) {
        show = false
        if (on) { delay(700); show = true }
    }
    if (on && show) LoadingRing(modifier)
}

// ---------- title, bar, controls, and the strip while scrubbing ----------

/** One layout for paused and scrubbing, so the bar under a dragging finger never moves or restarts. */
@Composable
private fun Controls(st: PlayerState, a: PlayerActions) {
    val scrubbing = st.mode == Mode.Scrub
    val rest by animateFloatAsState(if (scrubbing) 0f else 1f, tween(Motion.FadeInMs), label = "rest")
    val dim by animateFloatAsState(if (scrubbing) 0.72f else 0f, tween(Motion.FadeInMs), label = "dim")
    Box(Modifier.fillMaxSize()) {
        Box(Modifier.fillMaxSize().graphicsLayer { alpha = rest }.background(Brush.verticalGradient(0f to Base.copy(alpha = 0.64f), 0.3f to Base.copy(alpha = 0.3f), 0.6f to Base.copy(alpha = 0.3f), 1f to Base.copy(alpha = 0.8f))))
        Box(Modifier.fillMaxSize().graphicsLayer { alpha = dim }.background(Base))
        if (scrubbing) ScrubView(st)

        Row(
            Modifier.align(Alignment.TopStart).safeDrawingPadding().padding(horizontal = 12.dp, vertical = 8.dp).fillMaxWidth().graphicsLayer { alpha = rest },
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Control(AppIcons.Back, "Back", enabled = !scrubbing, onClick = a::close)
            Column(Modifier.padding(start = 4.dp).weight(1f)) {
                Text(st.title, style = Type.Heading, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (st.sub.isNotEmpty()) Text(st.sub, style = Type.Meta, color = Muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (st.dur > 0) Text("Ends " + endsAt(LocalContext.current, ((st.dur - st.pos) / st.speed).toLong()), style = Type.Meta.figures(), modifier = Modifier.padding(end = 12.dp))
        }

        Row(
            Modifier.align(Alignment.Center).graphicsLayer { alpha = rest },
            horizontalArrangement = Arrangement.spacedBy(48.dp), verticalAlignment = Alignment.CenterVertically,
        ) {
            Control(AppIcons.Skip10Back, "Back 10 seconds", 40, !scrubbing) { a.skip(-1) }
            Control(if (st.playing) AppIcons.Pause else AppIcons.Play, if (st.playing) "Pause" else "Play", 56, !scrubbing, onClick = a::togglePlay)
            Control(AppIcons.Skip10Forward, "Forward 10 seconds", 40, !scrubbing) { a.skip(1) }
        }

        Column(Modifier.align(Alignment.BottomCenter).safeDrawingPadding().padding(horizontal = 24.dp).fillMaxWidth()) {
            Timeline(st, a)
            Row(Modifier.fillMaxWidth().height(ButtonHeight).graphicsLayer { alpha = rest }, verticalAlignment = Alignment.CenterVertically) {
                TimeText(st)
                Spacer(Modifier.weight(1f))
                if (st.episodes.size > 1) Control(AppIcons.Episodes, "Episodes", enabled = !scrubbing) { a.openSheet(Sheet.Episodes) }
                Row(Modifier.height(ButtonHeight).tap(enabled = !scrubbing) { a.openSheet(Sheet.Speed) }.padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Icon(AppIcons.Speed, "Speed", tint = Ink, modifier = Modifier.size(24.dp))
                    if (st.speed != 1f) Text(speedLabel(st.speed), style = Type.Label.figures(), modifier = Modifier.padding(start = 4.dp))
                }
                Control(AppIcons.Audio, "Audio", enabled = !scrubbing) { a.openSheet(Sheet.Audio) }
                Control(AppIcons.Subtitles, "Subtitles", enabled = !scrubbing) { a.openSheet(Sheet.Subtitles) }
                if (st.hasNext) Control(AppIcons.SkipNext, "Next episode", enabled = !scrubbing, onClick = a::next)
            }
        }
    }
}

@Composable
private fun Control(icon: ImageVector, label: String, size: Int = 24, enabled: Boolean = true, onClick: () -> Unit) {
    Box(Modifier.size((size + 24).dp).tap(enabled, onClick), contentAlignment = Alignment.Center) {
        Icon(icon, label, tint = Ink, modifier = Modifier.size(size.dp))
    }
}

/** "35:02 / 59:44": on its own, so the running clock redraws only itself. */
@Composable
private fun TimeText(st: PlayerState) {
    Text("${clock(st.scrub ?: st.pos)} / ${clock(st.dur)}", style = Type.Label.figures())
}

/**
 * The progress line: flat and white, thicker while it's held, with no knob. Put a finger anywhere on it
 * and it goes there. Reads the position while drawing, so it never recomposes.
 */
@Composable
private fun Timeline(st: PlayerState, a: PlayerActions) {
    val thick by animateDpAsState(if (st.mode == Mode.Scrub) 8.dp else 4.dp, tween(Motion.FadeInMs), label = "bar")
    Canvas(
        Modifier.fillMaxWidth().height(32.dp).pointerInput(Unit) {
            awaitEachGesture {
                val down = awaitFirstDown()
                down.consume()
                if (st.dur <= 0) return@awaitEachGesture
                a.scrubStart()
                a.scrubMove((down.position.x / size.width * st.dur).toLong())
                while (true) {
                    val change = awaitPointerEvent().changes.firstOrNull { it.id == down.id } ?: break
                    if (!change.pressed) break
                    change.consume()
                    a.scrubMove((change.position.x / size.width * st.dur).toLong())
                }
                a.scrubEnd()
            }
        },
    ) {
        val dur = st.dur.coerceAtLeast(1).toFloat()
        val at = (st.scrub ?: st.pos) / dur * size.width
        val buf = st.buffered / dur * size.width
        val px = thick.toPx()
        val y = (size.height - px) / 2
        drawRect(Ink.copy(alpha = 0.28f), Offset(0f, y), Size(size.width, px))
        drawRect(Ink.copy(alpha = 0.18f), Offset(0f, y), Size(buf.coerceIn(0f, size.width), px))
        drawRect(Ink, Offset(0f, y), Size(at.coerceIn(0f, size.width), px))
    }
}

/** The picture you'd land on, large and centred, its neighbours beside it, and the time. */
@Composable
private fun BoxScope.ScrubView(st: PlayerState) {
    val store = st.thumbs
    // new pictures only when the position crosses a 10 s step, however fast the time runs
    val step by remember { derivedStateOf { (st.scrub ?: st.pos) / 10_000 } }
    Column(
        Modifier.align(Alignment.BottomCenter).safeDrawingPadding().padding(bottom = 80.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        if (store != null && store.ready) {
            val here = step * 10_000
            val base = here / StripStepMs * StripStepMs
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                StripPicture(store, base - 2 * StripStepMs, st.dur, Modifier.width(112.dp))
                StripPicture(store, base - StripStepMs, st.dur, Modifier.width(112.dp))
                StripPicture(store, here, st.dur, Modifier.width(320.dp).border(2.dp, Ink, CardShape), big = true)
                StripPicture(store, base + StripStepMs, st.dur, Modifier.width(112.dp))
                StripPicture(store, base + 2 * StripStepMs, st.dur, Modifier.width(112.dp))
            }
        }
        ScrubTime(st)
    }
}

@Composable
private fun ScrubTime(st: PlayerState) {
    Text(clock(st.scrub ?: st.pos), style = Type.Title.figures(), modifier = Modifier.padding(top = 12.dp))
}

@Composable
private fun StripPicture(store: ThumbStore, ms: Long, dur: Long, modifier: Modifier, big: Boolean = false) {
    val inFilm = ms in 0..dur
    Box(
        modifier.aspectRatio(16f / 9f)
            .graphicsLayer { alpha = if (inFilm) (if (big) 1f else 0.6f) else 0f }
            .clip(CardShape).background(Raised),
    ) {
        if (inFilm) ThumbPicture(store, ms)
    }
}

/** A scrub picture; the previous one stays up until this one is read, so nothing flashes empty. */
@Composable
private fun ThumbPicture(store: ThumbStore, ms: Long) {
    val last = remember { arrayOfNulls<ImageBitmap>(1) }
    store.at(ms)?.let { last[0] = it }
    last[0]?.let { Image(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
}

// ---------- panels, offers, errors ----------

/** A list down the right-hand side; the title and the picture stay in view. A tap beside it closes it. */
@Composable
private fun BoxScope.Panel(title: String, options: List<TrackOption>, a: PlayerActions) {
    Column(
        Modifier.align(Alignment.CenterEnd).width(344.dp).fillMaxHeight().background(Raised)
            .clickable(remember { MutableInteractionSource() }, null) {}.safeDrawingPadding().padding(start = 8.dp, end = 8.dp, top = 12.dp),
    ) {
        Text(title, style = Type.Caption, color = Muted, modifier = Modifier.padding(horizontal = 8.dp).padding(bottom = 8.dp))
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
            if (options.isEmpty()) Text("None in this file", style = Type.Body, color = Muted, modifier = Modifier.padding(8.dp))
            options.forEach { o ->
                Row(
                    Modifier.fillMaxWidth().heightForOption(o).tap { o.pick(); a.closeSheet() }.padding(horizontal = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Box(Modifier.width(28.dp)) { if (o.selected) Icon(AppIcons.Check, null, tint = Ink, modifier = Modifier.size(20.dp)) }
                    if (o.image != null) Box(Modifier.padding(end = 12.dp).width(96.dp).aspectRatio(16f / 9f).clip(CardShape).background(Overlay)) {
                        AsyncImage(o.image, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
                    }
                    Text(o.title, style = Type.Label, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                    if (o.detail.isNotEmpty()) {
                        Spacer(Modifier.weight(1f))
                        Text(o.detail, style = Type.Meta, color = Muted, maxLines = 1, modifier = Modifier.padding(start = 8.dp))
                    }
                }
            }
        }
    }
}

private fun Modifier.heightForOption(o: TrackOption): Modifier = height(if (o.image != null) 70.dp else ButtonHeight)

@Composable
private fun NextCard(o: Offer, countdown: Int, onClick: () -> Unit) {
    Row(
        Modifier.width(300.dp).clip(CardShape).background(Raised).tap(onClick = onClick).padding(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.width(112.dp).aspectRatio(16f / 9f).clip(CardShape).background(Overlay)) {
            o.still?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
        }
        Column(Modifier.padding(start = 12.dp)) {
            Text(o.title, style = Type.Label, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(if (countdown > 0) "Next in $countdown s" else "Next", style = Type.Meta.figures(), color = Muted, modifier = Modifier.padding(top = 4.dp))
        }
    }
}

@Composable
fun ErrorCard(e: PlayError, a: PlayerActions) {
    Box(Modifier.fillMaxSize().background(Base.copy(alpha = 0.8f)), contentAlignment = Alignment.Center) {
        Column(Modifier.widthIn(max = 460.dp).clip(CardShape).background(Raised).padding(24.dp)) {
            Text(e.title, style = Type.Title)
            Text(e.message, style = Type.Body, color = Muted, modifier = Modifier.padding(top = 8.dp))
            if (e.detail.isNotEmpty()) Text(e.detail, style = Type.Caption, color = Muted, maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
            Row(Modifier.padding(top = 24.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                WordButton("Try again", a::retry)
                WordButton("Close", a::close)
            }
        }
    }
}
