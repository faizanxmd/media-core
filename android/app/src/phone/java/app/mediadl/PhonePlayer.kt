package app.mediadl

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.platform.LocalContext
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import kotlin.math.abs

private const val SKIP = 10_000L

/**
 * The phone app's player controls. Tap shows/hides them; double-tap the left or right
 * half skips 10 s and shows only a thin line with where you landed.
 */
@Composable
fun DeviceOverlay(st: PlayerState, a: PlayerActions, tv: Boolean, onChrome: (Boolean) -> Unit) {
    val full = st.mode == Mode.Full && st.sheet == null
    LaunchedEffect(full) { onChrome(full) }
    var menu by remember { mutableStateOf("audio") } // which list the Tracks sheet shows

    CompositionLocalProvider(LocalTextStyle provides TextStyle(fontFamily = Inter, color = Color.White)) {
        Box(
            Modifier.fillMaxSize().pointerInput(Unit) {
                detectTapGestures(
                    onTap = { if (st.sheet != null) a.closeSheet() else a.tapped() },
                    onDoubleTap = { o -> if (st.sheet == null) a.scrub(if (o.x < size.width / 2) -SKIP else SKIP) },
                )
            },
        ) {
            AnimatedVisibility(!st.firstFrame && st.error == null, enter = fadeIn(tween(0)), exit = fadeOut(tween(Motion.BackdropMs))) {
                Box(Modifier.fillMaxSize().background(Color.Black)) {
                    st.cover?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
                    Box(Modifier.fillMaxSize().background(Color(0x99000000)))
                    CircularProgressIndicator(color = Color.White, strokeWidth = 2.dp, trackColor = Color(0x33FFFFFF), modifier = Modifier.align(Alignment.Center).size(36.dp))
                }
            }
            if (st.firstFrame && st.buffering && st.error == null && !full) {
                CircularProgressIndicator(color = Color.White, strokeWidth = 2.dp, trackColor = Color(0x33FFFFFF), modifier = Modifier.align(Alignment.Center).size(36.dp))
            }

            if (st.mode == Mode.Seek && st.sheet == null) Glance(st)
            AnimatedVisibility(full || st.sheet != null, enter = fadeIn(tween(Motion.FadeInMs)), exit = fadeOut(tween(Motion.FadeOutMs))) {
                Controls(st, a, onTracks = { kind -> menu = kind; a.openSheet(Sheet.Tracks) })
            }

            val offer = st.offer
            if (offer != null && st.sheet == null && st.error == null && st.mode != Mode.Seek) {
                Box(Modifier.align(Alignment.BottomEnd).safeDrawingPadding().padding(end = 24.dp, bottom = if (full) 96.dp else 24.dp)) {
                    if (offer.kind == "next") NextUp(offer, st.countdown, a::takeOffer)
                    else Text(
                        offer.title, color = PBg, fontSize = 15.sp, fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.clip(RoundedCornerShape(12.dp)).background(Color.White).clickable(onClick = a::takeOffer)
                            .padding(horizontal = 22.dp, vertical = 12.dp),
                    )
                }
            }

            when (st.sheet) {
                Sheet.Tracks -> if (menu == "audio") Menu("Audio", st.audio, a) else Menu("Subtitles", st.text, a)
                Sheet.Chapters -> Menu("Chapters", st.chapters.mapIndexed { i, c ->
                    val next = st.chapters.getOrNull(i + 1)?.start ?: Long.MAX_VALUE
                    TrackOption(c.title.ifBlank { "Chapter ${i + 1}" }, clock(c.start), st.pos in c.start until next) { a.scrubTo(c.start, commit = true) }
                }, a)
                Sheet.Episodes -> Menu("Episodes", st.episodes, a)
                null -> {}
            }
            st.note?.let {
                Text(it, fontSize = 13.sp, modifier = Modifier.align(Alignment.TopCenter).safeDrawingPadding().padding(top = 16.dp)
                    .clip(RoundedCornerShape(10.dp)).background(Color(0xE01E1E21)).padding(horizontal = 14.dp, vertical = 8.dp))
            }
            st.error?.let { ErrorCard(it, a) }
        }
    }
}

/** After a double-tap: a thin line along the bottom with "+30s" and the new time over where you'll land. */
@Composable
private fun Glance(st: PlayerState) {
    val to = st.scrub ?: st.pos
    val delta = if (st.scrub != null) to - st.scrubFrom else 0L
    Box(Modifier.fillMaxSize()) {
        Box(Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(90.dp).background(Brush.verticalGradient(listOf(Color.Transparent, Color(0x8C000000)))))
        BoxWithConstraints(Modifier.align(Alignment.BottomCenter).safeDrawingPadding().padding(horizontal = 24.dp, vertical = 18.dp).fillMaxWidth()) {
            val f = if (st.dur > 0) (to.toFloat() / st.dur).coerceIn(0f, 1f) else 0f
            Column(Modifier.offset(x = (maxWidth * f - 40.dp).coerceIn(0.dp, maxWidth - 80.dp)).width(80.dp).padding(bottom = 10.dp),
                horizontalAlignment = Alignment.CenterHorizontally) {
                if (abs(delta) >= 1000) Text((if (delta > 0) "+" else "−") + "${abs(delta) / 1000}s", fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = Color(0xCCFFFFFF))
                Text(clock(to), fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
            }
            Box(Modifier.align(Alignment.BottomStart).fillMaxWidth().height(3.dp).clip(CircleShape).background(Color(0x4DFFFFFF))) {
                Box(Modifier.fillMaxWidth(f).fillMaxHeight().background(Color.White))
            }
        }
    }
}

@Composable
private fun Controls(st: PlayerState, a: PlayerActions, onTracks: (String) -> Unit) {
    Box(Modifier.fillMaxSize()) {
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(0f to Color(0x8C000000), 0.25f to Color(0x26000000), 0.6f to Color(0x26000000), 1f to Color(0xBF000000))))
        // top: back + what's playing; when it'll end
        Row(Modifier.align(Alignment.TopStart).safeDrawingPadding().padding(horizontal = 20.dp, vertical = 12.dp).fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically) {
            Icon(AppIcons.Back, "Back", tint = Color.White,
                modifier = Modifier.clip(CircleShape).clickable(onClick = a::close).padding(8.dp).size(24.dp))
            Column(Modifier.padding(start = 8.dp).weight(1f)) {
                Text(st.title, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val chapter = chapterAt(st.chapters, st.pos)
                val sub = listOfNotNull(st.sub.ifEmpty { null }, chapter).joinToString(" · ")
                if (sub.isNotEmpty()) Text(sub, fontSize = 12.sp, color = Color(0xB3FFFFFF), maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            if (st.dur > 0) {
                Text("Ends at ", fontSize = 13.sp, color = Color(0xBFFFFFFF))
                Text(endsAt(LocalContext.current, ((st.dur - st.pos) / st.speed).toLong()), fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
            }
        }
        // middle: back 10, play/pause, forward 10
        Row(Modifier.align(Alignment.Center), horizontalArrangement = Arrangement.spacedBy(56.dp), verticalAlignment = Alignment.CenterVertically) {
            RoundTap(AppIcons.Rewind, "Back 10 seconds", 44) { a.scrub(-SKIP) }
            Box(Modifier.size(72.dp).clip(CircleShape).background(Color(0x2EFFFFFF)).clickable(onClick = a::togglePlay), contentAlignment = Alignment.Center) {
                Icon(if (st.playing) AppIcons.Pause else AppIcons.Play, if (st.playing) "Pause" else "Play", tint = Color.White, modifier = Modifier.size(48.dp))
            }
            RoundTap(AppIcons.FastForward, "Forward 10 seconds", 44) { a.scrub(SKIP) }
        }
        // bottom: the bar with times at its ends, then the controls on the right
        Column(Modifier.align(Alignment.BottomCenter).safeDrawingPadding().padding(start = 24.dp, end = 24.dp, bottom = 8.dp).fillMaxWidth()) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                val shown = st.scrub ?: st.pos
                Text(clock(shown), fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
                Scrubber(st, a, Modifier.weight(1f).padding(horizontal = 14.dp))
                Text("−" + clock((st.dur - shown).coerceAtLeast(0)), fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
            }
            Row(Modifier.align(Alignment.End), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                if (st.episodes.size > 1) RoundTap(AppIcons.Episodes, "Episodes", 24) { a.openSheet(Sheet.Episodes) }
                if (st.chapters.size > 1) RoundTap(AppIcons.Chapters, "Chapters", 24) { a.openSheet(Sheet.Chapters) }
                Row(Modifier.clip(CircleShape).clickable(onClick = a::cycleSpeed).padding(10.dp), verticalAlignment = Alignment.CenterVertically) {
                    Icon(AppIcons.Speed, "Speed", tint = Color.White, modifier = Modifier.size(24.dp))
                    if (st.speed != 1f) Text(speedLabel(st.speed), fontSize = 12.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(start = 4.dp))
                }
                RoundTap(AppIcons.Audio, "Audio", 24) { onTracks("audio") }
                RoundTap(AppIcons.Subtitles, "Subtitles", 24) { onTracks("subs") }
                if (st.hasNext) RoundTap(AppIcons.SkipNext, "Next episode", 24, onClick = a::next)
            }
        }
    }
}

@Composable
private fun RoundTap(icon: ImageVector, label: String, size: Int, onClick: () -> Unit) {
    Icon(icon, label, tint = Color.White, modifier = Modifier.clip(CircleShape).clickable(onClick = onClick).padding(10.dp).size(size.dp))
}

/** Drag or tap anywhere on the bar; it seeks when you let go. */
@Composable
private fun Scrubber(st: PlayerState, a: PlayerActions, modifier: Modifier) {
    BoxWithConstraints(
        modifier.height(28.dp)
            .pointerInput(st.dur) { detectTapGestures { o -> a.scrubTo((o.x / size.width * st.dur).toLong(), commit = true) } }
            .pointerInput(st.dur) {
                detectHorizontalDragGestures(onDragEnd = { a.commitScrub() }) { ch, _ ->
                    a.scrubTo((ch.position.x / size.width * st.dur).toLong().coerceIn(0, st.dur), commit = false)
                }
            },
        contentAlignment = Alignment.CenterStart,
    ) {
        val pos = st.scrub ?: st.pos
        val f = if (st.dur > 0) (pos.toFloat() / st.dur).coerceIn(0f, 1f) else 0f
        val b = if (st.dur > 0) (st.buffered.toFloat() / st.dur).coerceIn(0f, 1f) else 0f
        Box(Modifier.fillMaxWidth().height(4.dp).clip(CircleShape).background(Color(0x40FFFFFF))) {
            Box(Modifier.fillMaxWidth(b).fillMaxHeight().background(Color(0x59FFFFFF)))
            Box(Modifier.fillMaxWidth(f).fillMaxHeight().background(Color.White))
        }
        val dot = if (st.scrub != null) 18.dp else 14.dp
        Box(Modifier.offset(x = maxWidth * f - dot / 2).size(dot).clip(CircleShape).background(Color.White))
    }
}

/** A tight list anchored bottom-right, above the controls. */
@Composable
private fun Menu(title: String, options: List<TrackOption>, a: PlayerActions) {
    Box(Modifier.fillMaxSize().safeDrawingPadding()) {
        Column(
            Modifier.align(Alignment.BottomEnd).padding(end = 24.dp, bottom = 64.dp).width(300.dp)
                .clip(RoundedCornerShape(16.dp)).background(Color(0xF21E1E21))
                .clickable(enabled = false) {}.padding(8.dp),
        ) {
            Text(title, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, color = Color(0x8CFFFFFF), modifier = Modifier.padding(start = 12.dp, top = 6.dp, bottom = 6.dp))
            Column(Modifier.heightIn(max = 220.dp).verticalScroll(rememberScrollState())) {
                if (options.isEmpty()) Text("None in this file", fontSize = 14.sp, color = Color(0x8CFFFFFF), modifier = Modifier.padding(12.dp))
                options.forEach { o ->
                    Row(
                        Modifier.fillMaxWidth().clip(RoundedCornerShape(10.dp)).clickable { o.pick(); a.closeSheet() }.padding(horizontal = 12.dp, vertical = 11.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Box(Modifier.width(26.dp)) { if (o.selected) Icon(AppIcons.Check, null, tint = Color.White, modifier = Modifier.size(18.dp)) }
                        Text(o.title, fontSize = 15.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                        Spacer(Modifier.width(10.dp).weight(0.01f))
                        if (o.detail.isNotEmpty()) Text(o.detail, fontSize = 12.sp, color = Color(0x8CFFFFFF), maxLines = 1)
                    }
                }
            }
        }
    }
}

@Composable
private fun NextUp(o: Offer, countdown: Int, onClick: () -> Unit) {
    Row(
        Modifier.width(300.dp).clip(RoundedCornerShape(14.dp)).background(Color(0xF21E1E21)).clickable(onClick = onClick).padding(10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.width(112.dp).height(63.dp).clip(RoundedCornerShape(8.dp)).background(Color(0xFF2A2A2D))) {
            o.still?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
        }
        Column(Modifier.padding(start = 12.dp)) {
            Text("Next Episode", fontSize = 11.sp, fontWeight = FontWeight.SemiBold, color = Color(0x99FFFFFF))
            Text(o.title, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (countdown > 0) Text("Starts in $countdown s", fontSize = 11.sp, color = Color(0x99FFFFFF))
        }
    }
}
