package app.mediadl

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
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
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.FormatListBulleted
import androidx.compose.material.icons.rounded.GraphicEq
import androidx.compose.material.icons.rounded.SkipNext
import androidx.compose.material.icons.rounded.Speed
import androidx.compose.material.icons.rounded.Subtitles
import androidx.compose.material.icons.rounded.VideoLibrary
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
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
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import kotlinx.coroutines.delay

/** Hidden: just the picture. Seek: a thin bar while you skip. Full: title, bar and menu (paused, or asked for). */
enum class Mode { Hidden, Seek, Full }
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
    var speed by mutableStateOf(1f)
    var title by mutableStateOf("")
    var sub by mutableStateOf("") // year, or "Season 1, Episode 2"
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
    var focusMenu = false // Full opened with Down: land on the menu instead of the bar
}

data class Offer(val kind: String, val title: String, val detail: String, val still: String?)
data class PlayError(val title: String, val message: String, val detail: String)
data class Chapter(val start: Long, val title: String)
class TrackOption(val title: String, val detail: String, val selected: Boolean, val pick: () -> Unit)

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


private val Dim70 = Color(0xB3FFFFFF)
private val Dim88 = Color(0xE0FFFFFF)

/** Which compact menu is open above the controls (the activity only knows "a sheet is open"). */
private enum class Menu { Audio, Subtitles, Chapters, Episodes }

@Composable
fun PlayerOverlay(st: PlayerState, a: PlayerActions, tv: Boolean, onChrome: (Boolean) -> Unit) {
    val full = st.mode == Mode.Full
    LaunchedEffect(full) { onChrome(full) } // subtitles move up out of the way
    Box(Modifier.fillMaxSize().pointerInput(Unit) { detectTapGestures(onTap = { a.tapped() }) }) {
        // the second before the first frame: artwork and title, not a black screen
        AnimatedVisibility(!st.firstFrame && st.error == null, enter = fadeIn(tween(0)), exit = fadeOut(tween(400))) {
            Box(Modifier.fillMaxSize().background(Bg)) {
                st.cover?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
                Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Color(0x59000000), Color(0xE6000000)))))
                Row(Modifier.align(Alignment.BottomStart).padding(40.dp), verticalAlignment = Alignment.CenterVertically) {
                    CircularProgressIndicator(color = Ink, strokeWidth = 2.dp, trackColor = Color(0x33FFFFFF), modifier = Modifier.size(20.dp))
                    Spacer(Modifier.width(14.dp))
                    Column {
                        Text(st.title, fontSize = 20.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        if (st.sub.isNotEmpty()) Text(st.sub, color = Dim70, fontSize = 12.5.sp, fontWeight = FontWeight.Medium)
                    }
                }
            }
        }
        if (st.firstFrame && st.buffering && st.error == null) {
            CircularProgressIndicator(
                color = Ink, strokeWidth = 2.dp, trackColor = Color(0x33FFFFFF),
                modifier = Modifier.align(Alignment.Center).size(32.dp),
            )
        }

        if (st.mode == Mode.Seek && st.sheet == null) Glance(st)
        AnimatedVisibility(full, enter = fadeIn(tween(150)), exit = fadeOut(tween(200))) { Full(st, a, tv) }

        val offer = st.offer
        if (offer != null && st.sheet == null && st.error == null && st.mode != Mode.Seek) {
            Box(Modifier.align(Alignment.BottomEnd).padding(end = 40.dp, bottom = if (full) 134.dp else 40.dp)) {
                if (offer.kind == "next") NextCard(offer, st.countdown, armed = tv && !full, onClick = a::takeOffer)
                else SkipCapsule(offer.title, armed = tv && !full, onClick = a::takeOffer)
            }
        }

        val note = st.note
        if (note != null && st.mode == Mode.Hidden) {
            Text(
                note, fontSize = 13.5.sp, fontWeight = FontWeight.Medium,
                modifier = Modifier.align(Alignment.TopCenter).padding(top = 30.dp)
                    .clip(RoundedCornerShape(20.dp)).background(Surface).padding(horizontal = 16.dp, vertical = 9.dp),
            )
        }

        st.error?.let { ErrorCard(it, a) }
    }
}

/** A skip while it plays: a thin line at the bottom and how far you went, nothing else. */
@Composable
private fun Glance(st: PlayerState) {
    val dur = st.dur.coerceAtLeast(1)
    val target = st.scrub
    val at = target ?: st.pos
    val frac = (at.toFloat() / dur).coerceIn(0f, 1f)
    Box(Modifier.fillMaxSize()) {
        Box(Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(100.dp).background(Brush.verticalGradient(listOf(Color.Transparent, Color(0x8C000000)))))
        BoxWithConstraints(Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(start = 40.dp, end = 40.dp, bottom = 32.dp)) {
            val w = maxWidth
            ProgressTrack(frac, Modifier.fillMaxWidth(), height = 2.5.dp, back = Color(0x4DFFFFFF))
            if (target != null) {
                val delta = (target - st.pos) / 1000
                Column(
                    Modifier.offset(x = (w * frac - 60.dp).coerceIn(0.dp, w - 120.dp), y = (-44).dp).width(120.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Text(skipLabel(delta), fontSize = 15.5.sp, fontWeight = FontWeight.SemiBold, style = Shadowed)
                    Text(clock(target), color = Color(0xBFFFFFFF), fontSize = 11.5.sp, fontWeight = FontWeight.SemiBold, style = Shadowed)
                }
            }
        }
    }
}

/** "+10s", "−30s", "+2m 30s" */
private fun skipLabel(sec: Long): String {
    val s = kotlin.math.abs(sec)
    val sign = if (sec < 0) "−" else "+"
    return sign + if (s >= 60) "${s / 60}m" + (if (s % 60 != 0L) " ${s % 60}s" else "") else "${s}s"
}

private val Shadowed = TextStyle(fontFamily = Inter, shadow = androidx.compose.ui.graphics.Shadow(Color(0x99000000), blurRadius = 10f))

/** Paused or asked for: title above the bar, times at its ends, round controls on the right. */
@Composable
private fun Full(st: PlayerState, a: PlayerActions, tv: Boolean) {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val barFr = remember { FocusRequester() }
    var menu by remember { mutableStateOf<Menu?>(null) }
    var last by remember { mutableIntStateOf(0) }

    data class Ctl(val icon: ImageVector, val label: String, val open: () -> Unit, val text: String? = null)
    val ctls = buildList {
        if (st.episodes.size > 1) add(Ctl(Icons.Rounded.VideoLibrary, "Episodes", { menu = Menu.Episodes; a.openSheet(Sheet.Episodes) }))
        if (st.chapters.size > 1) add(Ctl(Icons.AutoMirrored.Rounded.FormatListBulleted, "Chapters", { menu = Menu.Chapters; a.openSheet(Sheet.Chapters) }))
        add(Ctl(Icons.Rounded.Speed, "Speed", a::cycleSpeed, text = if (st.speed != 1f) speedLabel(st.speed) else null))
        add(Ctl(Icons.Rounded.GraphicEq, "Audio", { menu = Menu.Audio; a.openSheet(Sheet.Tracks) }))
        add(Ctl(Icons.Rounded.Subtitles, "Subtitles", { menu = Menu.Subtitles; a.openSheet(Sheet.Tracks) }))
        if (st.hasNext) add(Ctl(Icons.Rounded.SkipNext, "Next episode", a::next))
    }
    val frs = remember(ctls.size) { List(ctls.size) { FocusRequester() } }

    LaunchedEffect(Unit) {
        if (!tv) return@LaunchedEffect
        delay(16)
        runCatching { (if (st.focusMenu) frs.getOrNull(last.coerceIn(0, frs.lastIndex)) ?: barFr else barFr).requestFocus() }
        st.focusMenu = false
    }
    // a menu just closed: back to the button that opened it
    LaunchedEffect(st.sheet) {
        if (tv && st.sheet == null) { delay(16); runCatching { frs.getOrNull(last)?.requestFocus() } }
    }

    val scrubbing = st.scrub != null
    Box(Modifier.fillMaxSize()) {
        Box(Modifier.align(Alignment.TopCenter).fillMaxWidth().height(100.dp).background(Brush.verticalGradient(listOf(Color(0x80000000), Color.Transparent))))
        Box(Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(240.dp).background(Brush.verticalGradient(0f to Color.Transparent, 0.3f to Color(0x4D000000), 0.65f to Color(0xB8000000), 1f to Color(0xDB000000))))
        if (st.dur > 0) Row(Modifier.align(Alignment.TopEnd).padding(end = 40.dp, top = 30.dp)) {
            Text("Ends at ", color = Dim70, fontSize = 12.5.sp, fontWeight = FontWeight.Medium)
            Text(endsAt(ctx, ((st.dur - (st.scrub ?: st.pos)) / st.speed).toLong().coerceAtLeast(0)), fontSize = 12.5.sp, fontWeight = FontWeight.SemiBold)
        }
        Column(Modifier.align(Alignment.BottomStart).padding(start = 40.dp, end = 300.dp, bottom = 121.dp).graphicsLayer { alpha = if (scrubbing) 0f else 1f }) {
            Text(st.title, fontSize = 15.5.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-0.15).sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val sub = chapterAt(st.chapters, st.pos) ?: st.sub
            if (sub.isNotEmpty()) Text(sub, color = Dim70, fontSize = 12.5.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 2.dp))
        }
        Row(
            Modifier.align(Alignment.BottomStart).fillMaxWidth().padding(start = 40.dp, end = 40.dp, bottom = 83.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            val shown = st.scrub ?: st.pos
            Text(clock(shown), color = Dim88, fontSize = 12.5.sp, fontWeight = FontWeight.Medium, modifier = Modifier.widthIn(min = 44.dp))
            Spacer(Modifier.width(12.dp))
            FocusBar(st, a, barFr, frs.getOrNull(last), Modifier.weight(1f))
            Spacer(Modifier.width(12.dp))
            Text(if (st.dur > 0) "−" + clock((st.dur - shown).coerceAtLeast(0)) else "", color = Dim88, fontSize = 12.5.sp, fontWeight = FontWeight.Medium, modifier = Modifier.widthIn(min = 52.dp))
        }
        Row(
            Modifier.align(Alignment.BottomEnd).padding(end = 40.dp, bottom = 44.dp).graphicsLayer { alpha = if (scrubbing) 0f else 1f },
            horizontalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            ctls.forEachIndexed { i, c ->
                CircleButton(
                    c.icon, c.label, size = 36.dp, iconSize = 17.dp, focusRequester = frs[i], onFocus = { last = i },
                    modifier = Modifier.focusProperties { up = barFr }.rowEnds(i, ctls.size),
                    content = c.text?.let { t -> { f: Boolean -> Text(t, color = if (f) Bg else Ink, fontSize = 11.sp, fontWeight = FontWeight.Bold) } },
                    onClick = { last = i; c.open() },
                )
            }
        }

        val m = menu.takeIf { st.sheet != null }
        if (m != null) {
            val end = 40.dp + 45.dp * (ctls.size - 1 - last)
            Box(Modifier.align(Alignment.BottomEnd).padding(end = end, bottom = 91.dp)) {
                when (m) {
                    Menu.Audio -> MenuPanel("Audio", st.audio.map { o -> Choice(o.title, o.detail, o.selected) { o.pick(); a.closeSheet() } })
                    Menu.Subtitles -> MenuPanel("Subtitles", st.text.map { o -> Choice(o.title, o.detail, o.selected) { o.pick(); a.closeSheet() } })
                    Menu.Episodes -> MenuPanel("Episodes", st.episodes.map { o -> Choice(o.title, o.detail, o.selected) { o.pick(); a.closeSheet() } }, width = 300.dp)
                    Menu.Chapters -> MenuPanel("Chapters", st.chapters.mapIndexed { i, c ->
                        val next = st.chapters.getOrNull(i + 1)?.start ?: Long.MAX_VALUE
                        Choice(c.title.ifBlank { "Chapter ${i + 1}" }, clock(c.start), st.pos in c.start until next) { a.scrubTo(c.start, commit = true); a.closeSheet() }
                    }, width = 300.dp)
                }
            }
        }
    }
}

private class Choice(val title: String, val detail: String, val selected: Boolean, val pick: () -> Unit)

/** A compact list over the picture, above the button that opened it. Back closes it. */
@Composable
private fun MenuPanel(title: String, options: List<Choice>, width: Dp = 240.dp) {
    val fr = remember { FocusRequester() }
    val sel = options.indexOfFirst { it.selected }.coerceAtLeast(0)
    val state = rememberLazyListState(initialFirstVisibleItemIndex = (sel - 2).coerceAtLeast(0))
    LaunchedEffect(Unit) { delay(32); runCatching { fr.requestFocus() } }
    Column(
        Modifier.width(width).clip(RoundedCornerShape(11.dp)).background(Surface).padding(7.dp)
            .pointerInput(Unit) { detectTapGestures { } },
    ) {
        Text(title, color = Color(0x8CFFFFFF), fontSize = 11.5.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(start = 9.dp, top = 4.dp, bottom = 5.dp))
        if (options.isEmpty()) Text("None", color = Mut, fontSize = 13.5.sp, modifier = Modifier.padding(start = 9.dp, bottom = 8.dp))
        LazyColumn(Modifier.heightIn(max = 290.dp), state = state) {
            itemsIndexed(options) { i, o ->
                Box(Modifier.listEnds(i, options.size)) { MenuOption(o.title, o.detail, o.selected, if (i == sel) fr else null, o.pick) }
            }
        }
    }
}

/** Keeps the remote inside a menu: no Left/Right out of it, no Up past the first row or Down past the last. */
private fun Modifier.listEnds(index: Int, count: Int): Modifier = onPreviewKeyEvent { e ->
    e.type == KeyEventType.KeyDown && when (e.key) {
        Key.DirectionLeft, Key.DirectionRight -> true
        Key.DirectionUp -> index == 0
        Key.DirectionDown -> index == count - 1
        else -> false
    }
}

/** The focusable bar: Left/Right scrub, OK plays/pauses (or lands a scrub now). */
@Composable
private fun FocusBar(st: PlayerState, a: PlayerActions, fr: FocusRequester, downTo: FocusRequester?, modifier: Modifier) {
    var focused by remember { mutableStateOf(false) }
    Box(
        modifier
            .focusRequester(fr)
            .focusProperties { if (downTo != null) down = downTo }
            .onFocusChanged { focused = it.isFocused }
            .onKeyEvent { e ->
                if (e.type != KeyEventType.KeyDown) return@onKeyEvent false
                when (e.key) {
                    Key.DirectionLeft -> { a.scrub(-scrubStep(e.nativeKeyEvent.repeatCount)); true }
                    Key.DirectionRight -> { a.scrub(scrubStep(e.nativeKeyEvent.repeatCount)); true }
                    Key.DirectionCenter, Key.Enter, Key.NumPadEnter -> {
                        if (e.nativeKeyEvent.repeatCount == 0) { if (st.scrub != null) a.commitScrub() else a.togglePlay() }
                        true
                    }
                    else -> false
                }
            }
            .focusable()
            .pointerInput(st.dur) { detectTapGestures { o -> a.scrubTo((o.x / size.width * st.dur).toLong(), commit = true) } }
            .pointerInput(st.dur) {
                detectHorizontalDragGestures(onDragEnd = { a.commitScrub() }) { ch, _ ->
                    a.scrubTo((ch.position.x / size.width * st.dur).toLong().coerceIn(0, st.dur), commit = false)
                }
            },
    ) { Bar(st, Modifier.fillMaxWidth(), target = st.scrub, focused = focused) }
}

/**
 * Track, buffer, played part; while scrubbing a mark stays where you were and the white head
 * carries the time and chapter you'll land on. One Canvas: cheap to redraw.
 */
@Composable
private fun Bar(st: PlayerState, modifier: Modifier, target: Long?, focused: Boolean) {
    val dur = st.dur.coerceAtLeast(1)
    val playFrac = (st.pos.toFloat() / dur).coerceIn(0f, 1f)
    val bufFrac = (st.buffered.toFloat() / dur).coerceIn(0f, 1f)
    val tFrac = target?.let { (it.toFloat() / dur).coerceIn(0f, 1f) }
    BoxWithConstraints(modifier.height(24.dp), contentAlignment = Alignment.CenterStart) {
        val w = maxWidth
        Canvas(Modifier.fillMaxWidth().height(24.dp)) {
            val h = (if (target != null) 6.dp else 4.dp).toPx()
            val y = size.height / 2
            val r = CornerRadius(h / 2)
            drawRoundRect(Color(0x47FFFFFF), Offset(0f, y - h / 2), Size(size.width, h), r)
            drawRoundRect(Color(0x2EFFFFFF), Offset(0f, y - h / 2), Size(size.width * bufFrac, h), r)
            drawRoundRect(Color(0xF5FFFFFF), Offset(0f, y - h / 2), Size(size.width * playFrac, h), r)
            if (tFrac != null) {
                drawRoundRect(Color(0x99FFFFFF), Offset(size.width * playFrac - 1.dp.toPx(), y - 7.dp.toPx()), Size(2.dp.toPx(), 14.dp.toPx()), CornerRadius(1.dp.toPx()))
                drawCircle(Color(0x66000000), 9.dp.toPx(), Offset(size.width * tFrac, y + 1.dp.toPx()))
                drawCircle(Color.White, 7.5.dp.toPx(), Offset(size.width * tFrac, y))
            } else if (focused) {
                drawCircle(Color(0x66000000), 7.dp.toPx(), Offset(size.width * playFrac, y + 1.dp.toPx()))
                drawCircle(Color.White, 6.dp.toPx(), Offset(size.width * playFrac, y))
            }
        }
        if (target != null && tFrac != null) {
            Column(
                Modifier.offset(x = (w * tFrac - 100.dp).coerceIn(0.dp, w - 200.dp), y = (-46).dp).width(200.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text(clock(target), fontSize = 24.sp, lineHeight = 26.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-0.25).sp, style = Shadowed)
                chapterAt(st.chapters, target)?.let {
                    Text(it, color = Color(0xCCFFFFFF), fontSize = 12.5.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis, style = Shadowed)
                }
            }
        }
    }
}

/** "Skip Intro": a capsule like the title page's play button. */
@Composable
private fun SkipCapsule(label: String, armed: Boolean, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val lit = focused || armed
    Row(
        Modifier.onFocusChanged { focused = it.isFocused }
            .lift(lit, RoundedCornerShape(20.dp), 1.05f)
            .height(40.dp)
            .background(if (lit) Color.White else Color(0x59000000))
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .padding(start = 18.dp, end = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, color = if (lit) Bg else Ink, fontSize = 14.5.sp, fontWeight = FontWeight.SemiBold)
        Spacer(Modifier.width(6.dp))
        Icon(Icons.Rounded.SkipNext, null, tint = if (lit) Bg else Ink, modifier = Modifier.size(19.dp))
    }
}

/** Up next in the credits: still, title, and a ring counting down to it. */
@Composable
private fun NextCard(o: Offer, countdown: Int, armed: Boolean, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val lit = focused || armed
    Row(
        Modifier.onFocusChanged { focused = it.isFocused }
            .width(320.dp)
            .lift(lit, RoundedCornerShape(12.dp), 1.04f)
            .background(if (lit) Color.White else Surface)
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .padding(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(128.dp, 72.dp).clip(RoundedCornerShape(6.dp)).background(Surface2)) {
            o.still?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
            if (countdown > 0) {
                Box(Modifier.align(Alignment.BottomEnd).padding(6.dp).size(24.dp), contentAlignment = Alignment.Center) {
                    Canvas(Modifier.fillMaxSize()) {
                        drawCircle(Color(0x8C000000))
                        drawArc(
                            Color.White, -90f, 360f * countdown / 10f, false,
                            topLeft = Offset(1.5.dp.toPx(), 1.5.dp.toPx()),
                            size = Size(size.width - 3.dp.toPx(), size.height - 3.dp.toPx()),
                            style = Stroke(2.dp.toPx(), cap = StrokeCap.Round),
                        )
                    }
                    Text("$countdown", color = Color.White, fontSize = 11.sp, fontWeight = FontWeight.Bold)
                }
            }
        }
        Spacer(Modifier.width(12.dp))
        Column {
            val c = if (lit) Bg else Ink
            Text("Next Episode", color = c.copy(alpha = 0.6f), fontSize = 11.sp, fontWeight = FontWeight.SemiBold)
            Text(o.title, color = c, fontSize = 15.sp, lineHeight = 18.sp, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 2.dp))
            if (o.detail.isNotEmpty()) Text(o.detail, color = c.copy(alpha = 0.6f), fontSize = 11.5.sp, modifier = Modifier.padding(top = 2.dp))
        }
    }
}

@Composable
fun ErrorCard(e: PlayError, a: PlayerActions) {
    val fr = remember { FocusRequester() }
    LaunchedEffect(e) { delay(16); runCatching { fr.requestFocus() } }
    Box(Modifier.fillMaxSize().background(Color(0xCC000000)), contentAlignment = Alignment.Center) {
        Column(Modifier.widthIn(max = 460.dp).clip(RoundedCornerShape(16.dp)).background(Surface).padding(26.dp)) {
            Text(e.title, fontSize = 20.sp, fontWeight = FontWeight.Bold)
            Spacer(Modifier.height(8.dp))
            Text(e.message, color = Mut, fontSize = 14.sp, lineHeight = 20.sp)
            if (e.detail.isNotEmpty()) {
                Spacer(Modifier.height(8.dp))
                Text(e.detail, color = Faint, fontSize = 11.5.sp, lineHeight = 15.sp, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
            Spacer(Modifier.height(20.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(11.dp)) {
                TextCapsule("Try again", fr, a::retry)
                TextCapsule("Close", null, a::close)
            }
        }
    }
}

@Composable
private fun TextCapsule(text: String, fr: FocusRequester?, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    Box(
        Modifier.then(if (fr != null) Modifier.focusRequester(fr) else Modifier)
            .onFocusChanged { focused = it.isFocused }
            .lift(focused, RoundedCornerShape(20.dp), 1.05f)
            .height(40.dp)
            .background(if (focused) Color.White else Glass)
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .padding(horizontal = 20.dp),
        contentAlignment = Alignment.Center,
    ) { Text(text, color = if (focused) Bg else Ink, fontSize = 14.5.sp, fontWeight = FontWeight.SemiBold) }
}
