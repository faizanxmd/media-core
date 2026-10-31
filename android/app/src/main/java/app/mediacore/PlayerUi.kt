package app.mediacore

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.positionInRoot
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import app.mediacore.core.ThumbStore
import app.mediacore.core.clock
import coil3.compose.AsyncImage
import kotlinx.coroutines.delay
import kotlin.math.abs

/**
 * Hidden: just the picture (a tap on Left/Right skips, with only a small mark).
 * Scrub: the picture strip (TV), from a hold of Left/Right until OK or Back. Seek: a thin bar while skipping (phone).
 * Full: title, bar and controls (paused, or asked for with Up/Down).
 */
enum class Mode { Hidden, Seek, Scrub, Full }
enum class Sheet { Tracks, Chapters, Episodes }

/** Everything the player overlay shows; the activity writes it, Compose reads it. */
class PlayerState {
    var mode by mutableStateOf(Mode.Hidden)
    var sheet by mutableStateOf<Sheet?>(null)
    var playing by mutableStateOf(false)
    var buffering by mutableStateOf(true)
    var firstFrame by mutableStateOf(false) // until then: artwork + title instead of a black screen
    var pos by mutableLongStateOf(0L)
    var buffered by mutableLongStateOf(0L)
    var dur by mutableLongStateOf(0L)
    var scrub by mutableStateOf<Long?>(null) // where you'll land while still pressing Left/Right
    var scrubFrom by mutableLongStateOf(0L) // where that skip started, so "+10s" doesn't drift as the film plays
    var skipped by mutableIntStateOf(0) // seconds the last few taps of Left/Right add up to; shows the small mark
    var speed by mutableStateOf(1f)
    var title by mutableStateOf("")
    var sub by mutableStateOf("") // year, or "S1, E2 · Its title"
    var logo by mutableStateOf<String?>(null) // title art, when there is some
    var cover by mutableStateOf<String?>(null)
    var offer by mutableStateOf<Offer?>(null)
    var countdown by mutableIntStateOf(0)
    var hasNext by mutableStateOf(false)
    var note by mutableStateOf<String?>(null)
    var error by mutableStateOf<PlayError?>(null)
    var audio by mutableStateOf<List<TrackOption>>(emptyList())
    var text by mutableStateOf<List<TrackOption>>(emptyList())
    var episodes by mutableStateOf<List<TrackOption>>(emptyList())
    var chapters by mutableStateOf<List<Chapter>>(emptyList())
    var thumbs: ThumbStore? = null // scrub pictures (TV)
    var focusMenu = false // Full opened with Down: land on the controls instead of the bar
    var barFocused = false // Left/Right on the bar skip and scrub, as they do with nothing on screen
}

data class Offer(val kind: String, val title: String, val detail: String, val still: String?)
data class PlayError(val title: String, val message: String, val detail: String)
data class Chapter(val start: Long, val title: String)
class TrackOption(val title: String, val detail: String, val selected: Boolean, val image: String? = null, val pick: () -> Unit)

interface PlayerActions {
    fun togglePlay()
    fun scrub(deltaMs: Long)
    fun scrubTo(ms: Long, commit: Boolean)
    fun commitScrub()
    fun next()
    fun takeOffer()
    fun openSheet(s: Sheet)
    fun closeSheet()
    fun cycleSpeed()
    fun setSpeed(speed: Float)
    fun tapped()
    fun retry()
    fun close()
}

/** Seek step for a held key: 10 s taps, speeding up the longer it's held. */
fun scrubStep(repeat: Int): Long = when {
    repeat < 4 -> 10_000L
    repeat < 12 -> 30_000L
    repeat < 24 -> 60_000L
    else -> 120_000L
}

/** The chapter a position falls in, unless it's only called "Chapter 7". */
fun chapterAt(chs: List<Chapter>, ms: Long): String? =
    chs.lastOrNull { it.start <= ms }?.title?.takeIf { it.isNotBlank() && !Regex("""(?i)^chapter\s*\d+$""").matches(it.trim()) }

fun speedLabel(s: Float) = (if (s == s.toInt().toFloat()) "${s.toInt()}" else "$s") + "×"


/** The bottom margin and the height of the controls row, shared by everything anchored to them. */
private val BottomGap = 24.dp
private val CtlHeight = 44.dp
private val CtlIcon = 24.dp

/** Which menu is open above the controls (the activity only knows "a sheet is open"). */
private enum class Menu { Speed, Audio, Subtitles, Episodes }

private val Speeds = listOf(0.75f, 1f, 1.25f, 1.5f, 2f)

@Composable
fun PlayerOverlay(st: PlayerState, a: PlayerActions, tv: Boolean, onChrome: (Boolean) -> Unit) {
    val full = st.mode == Mode.Full
    val scrubbing = st.mode == Mode.Scrub
    LaunchedEffect(full) { onChrome(full) } // subtitles move up out of the way
    Box(Modifier.fillMaxSize().pointerInput(Unit) { detectTapGestures(onTap = { a.tapped() }) }) {
        // the second before the first frame: artwork and title, not a black screen
        AnimatedVisibility(!st.firstFrame && st.error == null, enter = fadeIn(tween(0)), exit = fadeOut(tween(Motion.BackdropMs))) {
            Box(Modifier.fillMaxSize().background(Base)) {
                st.cover?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
                Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Base.copy(alpha = 0.35f), Base.copy(alpha = 0.9f)))))
                Row(Modifier.align(Alignment.BottomStart).padding(Side), verticalAlignment = Alignment.CenterVertically) {
                    LoadingRing(size = 20.dp)
                    Spacer(Modifier.width(14.dp))
                    Column {
                        Text(st.title, style = Type.Heading, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        if (st.sub.isNotEmpty()) Text(st.sub, style = Type.Meta, color = Muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
            }
        }
        QuietLoading(st.firstFrame && st.buffering && st.error == null && !scrubbing, Modifier.align(Alignment.Center))

        SkipMark(st.skipped)

        AnimatedVisibility(scrubbing, enter = fadeIn(tween(Motion.FadeInMs)), exit = fadeOut(tween(Motion.FadeOutMs))) { ScrubView(st) }
        AnimatedVisibility(full, enter = fadeIn(tween(Motion.FadeInMs)), exit = fadeOut(tween(Motion.FadeOutMs))) { Full(st, a, tv) }

        val offer = st.offer
        if (offer != null && st.sheet == null && st.error == null && !scrubbing) {
            Box(Modifier.align(Alignment.BottomEnd).padding(end = Side, bottom = if (full) 170.dp else Side)) {
                if (offer.kind == "next") NextCard(offer, st.countdown, armed = tv && !full, onClick = a::takeOffer)
                else SkipCapsule(offer.title, armed = tv && !full, onClick = a::takeOffer)
            }
        }

        val note = st.note
        if (note != null && st.mode == Mode.Hidden) {
            Text(
                note, style = Type.Meta,
                modifier = Modifier.align(Alignment.TopCenter).padding(top = 32.dp)
                    .clip(CardShape).background(Raised).padding(horizontal = 16.dp, vertical = 8.dp),
            )
        }

        st.error?.let { ErrorCard(it, a) }
    }
}

/** After a tap of Left/Right: two arrows and the seconds, small, on the side you went. Stays still while taps add up. */
@Composable
private fun BoxScope.SkipMark(seconds: Int) {
    val last = remember { intArrayOf(0) }
    if (seconds != 0) last[0] = seconds
    AnimatedVisibility(
        seconds != 0,
        modifier = Modifier.align(if (last[0] > 0) Alignment.CenterEnd else Alignment.CenterStart).padding(horizontal = Side),
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

// ---------- holding Left/Right: the picture strip ----------

/** Pictures this far apart either side of the big one, so the strip shows where you're heading. */
private const val StripStepMs = 30_000L

/**
 * Everything else clears away and the film dims: the picture you'd land on, large and centred,
 * its neighbours beside it, and the time. Without pictures yet, just the time over the bar.
 */
@Composable
private fun ScrubView(st: PlayerState) {
    val store = st.thumbs
    // new pictures only when the position crosses a 10 s step, however fast the time runs
    val step by remember { derivedStateOf { (st.scrub ?: st.pos) / 10_000 } }
    Box(Modifier.fillMaxSize().background(Base.copy(alpha = 0.72f))) {
        val strip = store != null && store.ready
        if (strip) {
            val here = step * 10_000
            val base = here / StripStepMs * StripStepMs
            Row(
                Modifier.align(Alignment.BottomCenter).padding(bottom = 96.dp),
                horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.CenterVertically,
            ) {
                StripPicture(store!!, base - 2 * StripStepMs, st.dur, Modifier.width(128.dp))
                StripPicture(store, base - StripStepMs, st.dur, Modifier.width(128.dp))
                StripPicture(store, here, st.dur, Modifier.width(320.dp).border(FocusLine, Ink, CardShape), big = true)
                StripPicture(store, base + StripStepMs, st.dur, Modifier.width(128.dp))
                StripPicture(store, base + 2 * StripStepMs, st.dur, Modifier.width(128.dp))
            }
        }
        ScrubTime(st, Modifier.align(Alignment.BottomCenter).padding(bottom = 46.dp))
        Timeline(st, Modifier.align(Alignment.BottomStart).padding(start = Side, end = Side, bottom = 28.dp), 8.dp)
    }
}

/** On its own, so the running clock redraws only itself and not the strip. */
@Composable
private fun ScrubTime(st: PlayerState, modifier: Modifier) {
    Text(clock(st.scrub ?: st.pos), style = Type.Title.figures(), modifier = modifier)
}

@Composable
private fun StripPicture(store: ThumbStore, ms: Long, dur: Long, modifier: Modifier, big: Boolean = false) {
    val shape = CardShape
    val inFilm = ms in 0..dur
    Box(
        modifier.aspectRatio(16f / 9f)
            .graphicsLayer { alpha = if (inFilm) (if (big) 1f else 0.6f) else 0f }
            .clip(shape).background(Raised),
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

// ---------- paused, or asked for: title, bar, controls ----------

/** The progress line: flat, white and square-ended. Reads the position while drawing, so it never recomposes. */
@Composable
private fun Timeline(st: PlayerState, modifier: Modifier, thick: Dp) {
    val h by animateDpAsState(thick, tween(Motion.FocusMs), label = "bar")
    Canvas(modifier.fillMaxWidth().height(18.dp)) {
        val dur = st.dur.coerceAtLeast(1).toFloat()
        val at = (st.scrub ?: st.pos) / dur * size.width
        val buf = st.buffered / dur * size.width
        val px = h.toPx()
        val y = (size.height - px) / 2
        drawRect(Ink.copy(alpha = 0.28f), Offset(0f, y), Size(size.width, px))
        drawRect(Ink.copy(alpha = 0.18f), Offset(0f, y), Size(buf.coerceIn(0f, size.width), px))
        drawRect(Ink, Offset(0f, y), Size(at.coerceIn(0f, size.width), px))
    }
}

@Composable
private fun Full(st: PlayerState, a: PlayerActions, tv: Boolean) {
    val ctx = LocalContext.current
    val barFr = remember { FocusRequester() }
    var menu by remember { mutableStateOf<Menu?>(null) }
    var last by remember { mutableIntStateOf(0) }

    data class Ctl(val icon: ImageVector, val label: String, val open: () -> Unit, val chip: String? = null)
    val ctls = buildList {
        add(Ctl(AppIcons.Audio, "Audio", { menu = Menu.Audio; a.openSheet(Sheet.Tracks) }))
        add(Ctl(AppIcons.Subtitles, "Subtitles", { menu = Menu.Subtitles; a.openSheet(Sheet.Tracks) }))
        add(Ctl(AppIcons.Speed, "Speed", { menu = Menu.Speed; a.openSheet(Sheet.Tracks) }, chip = if (st.speed != 1f) speedLabel(st.speed) else null))
        if (st.episodes.size > 1) add(Ctl(AppIcons.Episodes, "Episodes", { menu = Menu.Episodes; a.openSheet(Sheet.Episodes) }))
        if (st.hasNext) add(Ctl(AppIcons.SkipNext, "Next episode", a::next))
    }
    val frs = remember(ctls.size) { List(ctls.size) { FocusRequester() } }
    val ends = remember(ctls.size) { FloatArray(ctls.size) } // where each button ends: its menu lines up with it and grows leftward

    LaunchedEffect(Unit) {
        if (!tv) return@LaunchedEffect
        delay(16)
        runCatching { (if (st.focusMenu) frs.getOrNull(last.coerceIn(0, frs.lastIndex)) ?: barFr else barFr).requestFocus() }
        st.focusMenu = false
    }
    // a menu just closed: back to the button that opened it (not when the controls first appear: that's the bar's focus)
    var menuWasOpen by remember { mutableStateOf(false) }
    LaunchedEffect(st.sheet) {
        if (st.sheet != null) menuWasOpen = true
        else if (tv && menuWasOpen) {
            menuWasOpen = false
            delay(16)
            runCatching { frs.getOrNull(last)?.requestFocus() }
        }
    }

    val open = menu.takeIf { st.sheet != null }
    val picturesOpen = open == Menu.Episodes
    Box(Modifier.fillMaxSize()) {
        Box(Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(280.dp).background(Brush.verticalGradient(0f to Color.Transparent, 0.5f to Base.copy(alpha = 0.5f), 1f to Base.copy(alpha = 0.94f))))

        // kept in place under the picture menu, so focus can come back to it
        Column(
            Modifier.align(Alignment.BottomStart).fillMaxWidth().padding(start = Side, end = Side, bottom = BottomGap)
                .graphicsLayer { alpha = if (picturesOpen) 0f else 1f },
        ) {
            val logo = st.logo
            if (logo != null) AsyncImage(logo, st.title, Modifier.height(40.dp).widthIn(max = 200.dp), contentScale = ContentScale.Fit, alignment = Alignment.BottomStart)
            else Text(st.title, style = Type.Heading, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Row(Modifier.fillMaxWidth().padding(top = 8.dp), verticalAlignment = Alignment.Bottom) {
                Text(st.sub, style = Type.Meta, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f)) // takes the room, so Ends sits at the bar's end
                if (st.dur > 0) {
                    val left = ((st.dur - (st.scrub ?: st.pos)) / st.speed).toLong().coerceAtLeast(0)
                    Text("Ends " + endsAt(ctx, left), style = Type.Meta.figures(), color = Muted, maxLines = 1)
                }
            }
            Spacer(Modifier.height(4.dp))
            FocusBar(st, a, barFr, frs.getOrNull(last))
            Spacer(Modifier.height(4.dp))
            Row(Modifier.fillMaxWidth().height(CtlHeight), verticalAlignment = Alignment.CenterVertically) {
                val shown = st.scrub ?: st.pos
                Text(clock(shown), style = Type.Label.figures())
                if (st.dur > 0) Text("  /  " + clock(st.dur), style = Type.Label.figures().copy(fontWeight = androidx.compose.ui.text.font.FontWeight.Medium), color = Muted)
                Spacer(Modifier.weight(1f))
                // pushed out by the icon's own margin, so the last icon ends exactly where the bar does
                Row(Modifier.offset(x = (CtlHeight - CtlIcon) / 2), horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
                    ctls.forEachIndexed { i, c ->
                        CtlButton(
                            c.icon, c.label, c.chip, frs[i],
                            Modifier.focusProperties { up = barFr }.rowEnds(i, ctls.size)
                                .onGloballyPositioned { ends[i] = it.positionInRoot().x + it.size.width },
                            onFocus = { last = i }, onClick = { last = i; c.open() },
                        )
                    }
                }
            }
        }

        when (open) {
            Menu.Episodes -> PicturePanel("Episodes", st.episodes.map { o -> Pic(o.title, o.detail, o.selected, o.image, o.pick) })
            null -> {}
            else -> {
                val rightGap = with(LocalDensity.current) { (LocalConfiguration.current.screenWidthDp.dp.toPx() - ends.getOrElse(last) { 0f }).toDp() }.coerceAtLeast(Side)
                Box(Modifier.align(Alignment.BottomEnd).padding(end = rightGap, bottom = BottomGap + CtlHeight + 8.dp)) {
                    when (open) {
                        Menu.Speed -> MenuPanel("Speed", Speeds.map { s -> Choice(if (s == 1f) "Normal" else speedLabel(s), "", st.speed == s) { a.setSpeed(s); a.closeSheet() } }, 180.dp)
                        Menu.Audio -> MenuPanel("Audio", st.audio.map { o -> Choice(o.title, o.detail, o.selected) { o.pick(); a.closeSheet() } }, 280.dp)
                        else -> MenuPanel("Subtitles", st.text.map { o -> Choice(o.title, o.detail, o.selected) { o.pick(); a.closeSheet() } }, 280.dp)
                    }
                }
            }
        }
    }
}

/** A plain white icon at rest. Selected, the icon itself turns blue: nothing behind it, nothing resizes. */
@Composable
private fun CtlButton(icon: ImageVector, label: String, chip: String?, fr: FocusRequester, modifier: Modifier, onFocus: () -> Unit, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val tint = if (focused) AccentWord else Ink
    Box(
        modifier
            .focusRequester(fr)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .size(CtlHeight),
        contentAlignment = Alignment.Center,
    ) {
        if (chip != null) Text(chip, style = Type.Label, color = tint)
        else Icon(icon, label, tint = tint, modifier = Modifier.size(CtlIcon))
    }
}

private class Choice(val title: String, val detail: String, val selected: Boolean, val pick: () -> Unit)

/** A compact list over the picture, above the button that opened it. Back closes it. */
@Composable
private fun MenuPanel(title: String, options: List<Choice>, width: Dp) {
    val fr = remember { FocusRequester() }
    val sel = options.indexOfFirst { it.selected }.coerceAtLeast(0)
    val state = rememberLazyListState(initialFirstVisibleItemIndex = (sel - 2).coerceAtLeast(0))
    LaunchedEffect(Unit) { delay(32); runCatching { fr.requestFocus() } }
    Column(
        Modifier.width(width).clip(CardShape).background(Raised).padding(8.dp)
            .pointerInput(Unit) { detectTapGestures { } },
    ) {
        // starts where the names do, past the option's padding and its check column
        Text(title, style = Type.Caption, color = Muted, modifier = Modifier.padding(start = 36.dp, top = 4.dp, bottom = 4.dp))
        if (options.isEmpty()) Text("None", style = Type.Body, color = Muted, modifier = Modifier.padding(start = 36.dp, bottom = 8.dp))
        LazyColumn(Modifier.heightIn(max = 290.dp), state = state) {
            itemsIndexed(options) { i, o ->
                Box(Modifier.listEnds(i, options.size)) { MenuOption(o.title, o.detail, o.selected, if (i == sel) fr else null, o.pick) }
            }
        }
    }
}

/** Keeps the remote inside a menu: no Left/Right out of it, no Up past the first row or Down past the last. */
internal fun Modifier.listEnds(index: Int, count: Int): Modifier = onPreviewKeyEvent { e ->
    e.type == KeyEventType.KeyDown && when (e.key) {
        Key.DirectionLeft, Key.DirectionRight -> true
        Key.DirectionUp -> index == 0
        Key.DirectionDown -> index == count - 1
        else -> false
    }
}

private class Pic(val title: String, val detail: String, val selected: Boolean, val url: String?, val pick: () -> Unit)

/** Episodes: a row of pictures along the bottom, starting at where you are. Back closes it. */
@Composable
private fun PicturePanel(title: String, pics: List<Pic>) {
    val fr = remember { FocusRequester() }
    val sel = pics.indexOfFirst { it.selected }.coerceAtLeast(0)
    val state = rememberLazyListState(initialFirstVisibleItemIndex = (sel - 1).coerceAtLeast(0))
    LaunchedEffect(Unit) { delay(32); runCatching { fr.requestFocus() } }
    Box(Modifier.fillMaxSize()) {
        Column(
            Modifier.align(Alignment.BottomStart).fillMaxWidth()
                .background(Brush.verticalGradient(0f to Color.Transparent, 0.3f to Base.copy(alpha = 0.72f), 1f to Base.copy(alpha = 0.94f)))
                .padding(top = 40.dp, bottom = 24.dp),
        ) {
            Text(title, style = Type.Heading, modifier = Modifier.padding(start = Side))
            LazyRow(
                state = state,
                contentPadding = PaddingValues(horizontal = Side, vertical = 16.dp),
                horizontalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                itemsIndexed(pics) { i, p ->
                    Column(Modifier.width(196.dp)) {
                        FocusCard(
                            p.pick,
                            Modifier.fillMaxWidth().aspectRatio(16f / 9f).onPreviewKeyEvent { e ->
                                e.type == KeyEventType.KeyDown && when (e.key) { // the row keeps the remote until Back
                                    Key.DirectionUp, Key.DirectionDown -> true
                                    Key.DirectionLeft -> i == 0
                                    Key.DirectionRight -> i == pics.lastIndex
                                    else -> false
                                }
                            },
                            focusRequester = if (i == sel) fr else null,
                        ) {
                            p.url?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
                        }
                        Text(p.title, style = Type.Meta, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
                        Text(if (p.selected) "Now playing" else p.detail, style = Type.Caption, color = Muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 4.dp))
                    }
                }
            }
        }
    }
}

/** The bar with focus: OK plays or pauses (or lands a skip now). Left/Right are the activity's, on any screen. */
@Composable
private fun FocusBar(st: PlayerState, a: PlayerActions, fr: FocusRequester, downTo: FocusRequester?) {
    var focused by remember { mutableStateOf(false) }
    Box(
        Modifier
            .focusRequester(fr)
            .focusProperties { if (downTo != null) down = downTo }
            .onFocusChanged { focused = it.isFocused; st.barFocused = it.isFocused }
            .onKeyEvent { e ->
                if (e.type != KeyEventType.KeyDown) return@onKeyEvent false
                when (e.key) {
                    Key.DirectionCenter, Key.Enter, Key.NumPadEnter -> {
                        if (e.nativeKeyEvent.repeatCount == 0) { if (st.scrub != null) a.commitScrub() else a.togglePlay() }
                        true
                    }
                    else -> false
                }
            }
            .focusable(),
    ) { Timeline(st, Modifier, if (focused) 8.dp else 4.dp) }
}

/** "Skip Intro": a worded button, lit while it's what OK would do. */
@Composable
private fun SkipCapsule(label: String, armed: Boolean, onClick: () -> Unit) {
    WordButton(label, lit = armed, icon = AppIcons.SkipNext, onClick = onClick)
}

/** Up next in the credits: still, title, and a ring counting down to it. */
@Composable
private fun NextCard(o: Offer, countdown: Int, armed: Boolean, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val lit = focused || armed
    Row(
        Modifier.onFocusChanged { focused = it.isFocused }
            .width(320.dp)
            .clip(CardShape)
            .background(if (lit) Ink else Raised)
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .padding(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(128.dp, 72.dp).clip(CardShape).background(Overlay)) {
            o.still?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
            if (countdown > 0) {
                Box(Modifier.align(Alignment.BottomEnd).padding(4.dp).size(24.dp), contentAlignment = Alignment.Center) {
                    Canvas(Modifier.fillMaxSize()) {
                        drawCircle(Base.copy(alpha = 0.6f))
                        drawArc(
                            Color.White, -90f, 360f * countdown / 10f, false,
                            topLeft = Offset(1.5.dp.toPx(), 1.5.dp.toPx()),
                            size = Size(size.width - 3.dp.toPx(), size.height - 3.dp.toPx()),
                            style = Stroke(2.dp.toPx(), cap = StrokeCap.Round),
                        )
                    }
                    Text("$countdown", style = Type.Caption, color = Color.White)
                }
            }
        }
        Spacer(Modifier.width(12.dp))
        Column {
            val c = if (lit) Base else Ink
            val soft = if (lit) Base else Muted
            Text("Next Episode", style = Type.Caption, color = soft)
            Text(o.title, style = Type.Label, color = c, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (o.detail.isNotEmpty()) Text(o.detail, style = Type.Caption, color = soft)
        }
    }
}

@Composable
fun ErrorCard(e: PlayError, a: PlayerActions) {
    val fr = remember { FocusRequester() }
    LaunchedEffect(e) { delay(16); runCatching { fr.requestFocus() } }
    Box(Modifier.fillMaxSize().background(Base.copy(alpha = 0.8f)), contentAlignment = Alignment.Center) {
        Column(Modifier.widthIn(max = 460.dp).clip(CardShape).background(Raised).padding(24.dp)) {
            Text(e.title, style = Type.Title)
            Spacer(Modifier.height(8.dp))
            Text(e.message, style = Type.Body, color = Muted)
            if (e.detail.isNotEmpty()) {
                Spacer(Modifier.height(8.dp))
                Text(e.detail, style = Type.Caption, color = Muted, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
            Spacer(Modifier.height(24.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                WordButton("Try again", focusRequester = fr, onClick = a::retry)
                WordButton("Close", onClick = a::close)
            }
        }
    }
}
