package app.mediacore

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.focusable
import androidx.compose.foundation.gestures.detectTapGestures
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
import androidx.compose.foundation.layout.requiredWidth
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
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.clipRect
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.IntOffset
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.unit.dp
import app.mediacore.core.ThumbStore
import app.mediacore.core.clock
import coil3.compose.AsyncImage
import kotlinx.coroutines.delay
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.roundToInt
import kotlin.math.sin

/**
 * The player, after Netflix's TV player (docs/player-netflix.md).
 * Hidden: just the picture. Seek: a tap of Left/Right; the bar and the picture you'll land on, the film still playing.
 * Scrub: a hold of Left/Right; the film waits while the playhead runs. Full: paused, or asked for with Up/Down.
 */
enum class Mode { Hidden, Seek, Scrub, Full }
enum class Sheet { Tracks, Episodes }

/** Everything the player overlay shows; the activity writes it, Compose reads it. */
class PlayerState {
    var mode by mutableStateOf(Mode.Hidden)
    var sheet by mutableStateOf<Sheet?>(null)
    var playing by mutableStateOf(false)
    var buffering by mutableStateOf(true)
    var firstFrame by mutableStateOf(false) // until then: artwork, a ring and the title instead of a black screen
    var pos by mutableLongStateOf(0L)
    var buffered by mutableLongStateOf(0L)
    var dur by mutableLongStateOf(0L)
    var scrub by mutableStateOf<Long?>(null) // where you'll land while still pressing Left/Right
    var scrubFrom by mutableLongStateOf(0L)
    var skipped by mutableIntStateOf(0) // seconds the last few taps of Left/Right add up to
    var taps by mutableIntStateOf(0) // one more on each tap: the skip mark turns once each time
    var title by mutableStateOf("")
    var sub by mutableStateOf("") // year, or "S1, E2 · Its title"
    var logo by mutableStateOf<String?>(null) // title art, when there is some
    var cover by mutableStateOf<String?>(null)
    var offer by mutableStateOf<Offer?>(null)
    var nextLeft by mutableLongStateOf(0L) // ms until the next episode starts by itself
    var hasNext by mutableStateOf(false)
    var note by mutableStateOf<String?>(null)
    var error by mutableStateOf<PlayError?>(null)
    var audio by mutableStateOf<List<TrackOption>>(emptyList())
    var text by mutableStateOf<List<TrackOption>>(emptyList())
    var subSize by mutableIntStateOf(1) // 0 small, 1 medium, 2 large
    var episodes by mutableStateOf<List<TrackOption>>(emptyList())
    var thumbs: ThumbStore? = null // scrub pictures (TV)
    var combos by mutableStateOf<List<TrackOption>>(emptyList()) // the likely audio and subtitle pairs, as pills under the bar
    var tracksAt = 0 // the panel opens on audio (0) or on subtitle size (2)
    var focusTo: String? = null // where focus lands when the controls show: Play/Pause unless this names an icon or a pill
    var lastKey = "play" // the icon or pill focus was last on, to come back to after a panel
    var barFocused = false // Left/Right on the bar skip and scrub, as they do with nothing on screen
}

data class Offer(val kind: String, val title: String, val detail: String, val still: String?)
data class PlayError(val title: String, val message: String, val detail: String)
class TrackOption(val title: String, val detail: String, val selected: Boolean, val image: String? = null, val pick: () -> Unit)

interface PlayerActions {
    fun togglePlay()
    fun scrub(deltaMs: Long)
    fun scrubTo(ms: Long, commit: Boolean)
    fun commitScrub()
    fun next()
    fun takeOffer()
    fun dismissOffer()
    fun openSheet(s: Sheet)
    fun closeSheet()
    fun subSize(size: Int)
    fun startOver()
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

val SubSizes = listOf("Small", "Medium", "Large")

// Board px, on the 1920 x 1080 board. Everything sits on the bar, which never moves between modes.
private const val BUTTONS = 64 // offer buttons' height
private const val THUMB_W = 416
private const val THUMB_H = 234

@Composable
fun PlayerOverlay(st: PlayerState, a: PlayerActions, tv: Boolean, onChrome: (Float) -> Unit) {
    val chrome = st.mode != Mode.Hidden && st.sheet == null && st.error == null
    // subtitles rise above whatever is showing (a fraction of the screen's height)
    val lift = when {
        st.error != null -> 0f
        st.sheet != null -> 0.40f
        st.mode == Mode.Full -> 0.22f
        st.mode == Mode.Seek -> 0.14f
        else -> 0f
    }
    LaunchedEffect(lift) { onChrome(lift) }
    Box(Modifier.fillMaxSize().pointerInput(Unit) { detectTapGestures(onTap = { a.tapped() }) }) {
        AnimatedVisibility(!st.firstFrame && st.error == null, enter = fadeIn(tween(0)), exit = fadeOut(Move.fade(Move.Video))) { Starting(st) }
        QuietLoading(st.firstFrame && st.buffering && st.error == null && st.mode != Mode.Scrub, Modifier.align(Alignment.Center))

        SkipMark(st)

        AnimatedVisibility(chrome, enter = fadeIn(Move.fade(Move.Focus)), exit = fadeOut(Move.fade(Move.Screen))) { Chrome(st, a, tv) }
        AnimatedVisibility(st.sheet == Sheet.Tracks, enter = fadeIn(Move.fade(Move.Screen)), exit = fadeOut(Move.fade(Move.Focus))) { TracksPanel(st, a) }
        AnimatedVisibility(st.sheet == Sheet.Episodes, enter = fadeIn(Move.fade(Move.Screen)), exit = fadeOut(Move.fade(Move.Focus))) { EpisodesPanel(st) }

        val offer = st.offer
        if (offer != null && st.sheet == null && st.error == null && st.mode != Mode.Scrub && st.scrub == null) {
            val armed = tv && st.mode != Mode.Full // OK does it, with nothing to focus
            val above = if (st.mode == Mode.Hidden) 72 else 1080 - ROW_Y + PLAY_D / 2 + 24 // above the bar while it shows
            Box(Modifier.align(Alignment.BottomEnd).padding(end = SIDE.px, bottom = above.px)) {
                if (offer.kind == "next") NextUp(st, offer, armed, a)
                else OfferPill(offer.title, if (offer.kind == "slow") null else AppIcons.SkipNext, armed, onClick = a::takeOffer)
            }
        }

        val note = st.note
        if (note != null && st.mode == Mode.Hidden) {
            Text(
                note, style = ts(25, 30, 600),
                modifier = Modifier.align(Alignment.TopCenter).padding(top = TOP.px)
                    .clip(Corner).background(Pal.Glass).padding(horizontal = 28.px, vertical = 14.px),
            )
        }

        st.error?.let { ErrorCard(it, a) }
    }
}

// ---------- waiting ----------

/** Before the first frame: the title's art, dimmed, a ring in the accent and the title under it. */
@Composable
private fun Starting(st: PlayerState) {
    Box(Modifier.fillMaxSize().background(Pal.Base)) {
        st.cover?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
        Box(Modifier.fillMaxSize().background(Pal.BaseDeep.copy(alpha = 0.72f)))
        Column(Modifier.align(Alignment.Center), horizontalAlignment = Alignment.CenterHorizontally) {
            Spinner(76)
            Spacer(Modifier.height(36.px))
            Text(st.title, style = ts(34, 40, 700), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.widthIn(max = 1200.px))
            if (st.sub.isNotEmpty()) Text(st.sub, style = ts(25, 32, 600, Pal.Muted), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.px).widthIn(max = 1200.px))
        }
    }
}

/** A thin ring with a quarter of it in the accent, turning. */
@Composable
private fun Spinner(size: Int, modifier: Modifier = Modifier) {
    val turn by rememberInfiniteTransition(label = "spin").animateFloat(0f, 360f, infiniteRepeatable(tween(900, easing = LinearEasing), RepeatMode.Restart), label = "turn")
    val accent = Pal.Accent
    Canvas(modifier.size(size.px)) {
        val w = this.size.width * 0.07f
        val inset = w / 2
        val box = Size(this.size.width - w, this.size.height - w)
        drawArc(Pal.Ink.copy(alpha = 0.16f), 0f, 360f, false, Offset(inset, inset), box, style = Stroke(w))
        drawArc(accent, turn - 90f, 100f, false, Offset(inset, inset), box, style = Stroke(w, cap = StrokeCap.Round))
    }
}

/** Mid-film, only if the wait is real: a skip inside the buffer lands before this ever appears. */
@Composable
private fun QuietLoading(on: Boolean, modifier: Modifier) {
    var show by remember { mutableStateOf(false) }
    LaunchedEffect(on) {
        show = false
        if (on) { delay(600); show = true }
    }
    AnimatedVisibility(on && show, modifier, enter = fadeIn(Move.fade(Move.Focus)), exit = fadeOut(Move.fade(Move.Focus))) { Spinner(76) }
}

// ---------- tapping Left/Right: the turning mark ----------

/**
 * A circular arrow on the side you went, with the seconds the taps add up to inside it.
 * Each tap turns it a little that way and lets it spring back.
 */
@Composable
private fun BoxScope.SkipMark(st: PlayerState) {
    val last = remember { intArrayOf(10) }
    if (st.skipped != 0) last[0] = st.skipped
    val dir = if (last[0] > 0) 1 else -1
    val turn = remember { Animatable(0f) }
    LaunchedEffect(st.taps) {
        if (st.taps == 0 || Move.reduced) return@LaunchedEffect
        turn.animateTo(dir * 34f, tween(110, easing = Move.Ease))
        turn.animateTo(0f, tween(260, easing = Move.Ease))
    }
    AnimatedVisibility(
        st.skipped != 0 && st.mode != Mode.Scrub,
        modifier = Modifier.align(Alignment.Center).offset(x = (dir * 420).px),
        enter = fadeIn(Move.fade(Move.Focus)) + scaleIn(Move.fade(Move.Focus), initialScale = 0.85f),
        exit = fadeOut(Move.fade(Move.Screen)) + scaleOut(Move.fade(Move.Screen), targetScale = 0.92f),
    ) {
        Box(Modifier.size(150.px), contentAlignment = Alignment.Center) {
            Canvas(Modifier.fillMaxSize().graphicsLayer { rotationZ = turn.value; scaleX = dir.toFloat() }) {
                // a soft dark disc, so the mark reads on a white sky
                drawCircle(Pal.BaseDeep.copy(alpha = 0.38f), radius = size.minDimension / 2)
                val w = size.width * 0.045f
                val r = size.width * 0.34f
                val c = center
                val gapFrom = -62f // the opening at the top, where the arrow points into
                val sweep = 300f
                drawArc(Pal.Ink, gapFrom, sweep, false, Offset(c.x - r, c.y - r), Size(2 * r, 2 * r), style = Stroke(w, cap = StrokeCap.Round))
                // the head at the arc's end, pointing on round clockwise
                val end = Math.toRadians((gapFrom + sweep).toDouble())
                val p = Offset(c.x + r * cos(end).toFloat(), c.y + r * sin(end).toFloat())
                val along = Offset(-sin(end).toFloat(), cos(end).toFloat())
                val out = Offset(cos(end).toFloat(), sin(end).toFloat())
                val h = size.width * 0.12f
                val head = Path().apply {
                    moveTo(p.x + along.x * h, p.y + along.y * h)
                    lineTo(p.x + out.x * h * 0.62f, p.y + out.y * h * 0.62f)
                    lineTo(p.x - out.x * h * 0.62f, p.y - out.y * h * 0.62f)
                    close()
                }
                drawPath(head, Pal.Ink)
            }
            Text("${abs(last[0])}", style = ts(36, 40, 700, tabular = true))
        }
    }
}

// ---------- the controls: Netflix's TV player ----------
//
// Top left, bare icons: Back, Start over, Next episode, Episodes; the focused one names itself underneath.
// Top right, the title and the episode. Bottom: a round Play/Pause, the time watched, the bar, the time left.
// Under the bar, the likely audio and subtitle pairs as pills, then Other... and a gear for the rest.

private const val SIDE = 112 // the controls sit further in than the rest of the app, as Netflix's do
private const val PLAY_D = 96 // the round Play/Pause
private const val ROW_Y = 890 // the centre of Play/Pause and the bar, on the 1080 board
private const val PILL_H = 52

private val ArrowBack by lazy { lineIcon("Back", "M20 12H4.5" to 2f, "M10.5 5.5 4 12l6.5 6.5" to 2f) }
private val NextEp by lazy { lineIcon("NextEp", "M5.5 4.8v14.4L16.5 12z" to 1.8f, "M19.5 4.5v15" to 2f) }

private fun lineIcon(name: String, vararg parts: Pair<String, Float>) =
    ImageVector.Builder(name, 24.dp, 24.dp, 24f, 24f).apply {
        parts.forEach { (d, w) ->
            addPath(addPathNodes(d), stroke = SolidColor(Color.White), strokeLineWidth = w, strokeLineCap = StrokeCap.Round, strokeLineJoin = StrokeJoin.Round)
        }
    }.build()

@Composable
private fun Chrome(st: PlayerState, a: PlayerActions, tv: Boolean) {
    val full = st.mode == Mode.Full
    val bar = remember { mutableStateOf(Offset.Zero) } // the bar's left edge (x) and width (y) on screen, for the picture above it
    val requesters = remember { mutableMapOf<String, FocusRequester>() }
    fun fr(k: String) = requesters.getOrPut(k) { FocusRequester() }

    class Ctl(val key: String, val icon: ImageVector, val word: String, val open: () -> Unit)
    val tops = buildList {
        add(Ctl("top/back", ArrowBack, "Back", a::close))
        add(Ctl("top/over", Icons.StartOver, "Start over", a::startOver))
        if (st.hasNext) add(Ctl("top/next", NextEp, "Next episode", a::next))
        if (st.episodes.size > 1) add(Ctl("top/episodes", AppIcons.Episodes, "Episodes") { a.openSheet(Sheet.Episodes) })
    }
    val pills = buildList {
        st.combos.forEachIndexed { i, c -> add(Ctl("pill/$i", AppIcons.Check, c.title, c.pick)) }
        if (st.audio.size > 1 || st.text.isNotEmpty()) add(Ctl("pill/other", AppIcons.Check, "Other…") { st.tracksAt = 0; a.openSheet(Sheet.Tracks) })
        if (st.text.isNotEmpty()) add(Ctl("pill/look", Icons.Settings, "") { st.tracksAt = 2; a.openSheet(Sheet.Tracks) })
    }

    // also when a panel closes quickly, before these controls have finished fading out
    val panelOpen = st.sheet != null
    LaunchedEffect(full, panelOpen) {
        if (!tv || !full || panelOpen) return@LaunchedEffect
        delay(16)
        val want = st.focusTo?.takeIf { k -> tops.any { it.key == k } || pills.any { it.key == k } }
            ?: if (st.focusTo?.startsWith("pill") == true) pills.firstOrNull()?.key else null
        runCatching { fr(want ?: "play").requestFocus() }
        st.focusTo = null
    }

    // the title, the icons and the pills step aside while you look for a place
    val words by animateFloatAsState(if (full && st.mode != Mode.Scrub) 1f else 0f, Move.fade(Move.Focus), label = "words")
    val shade by animateFloatAsState(if (full) 1f else 0.6f, Move.fade(Move.Screen), label = "shade")

    Box(Modifier.fillMaxSize()) {
        // dark at the top and the bottom, so white reads on any picture; the middle stays clear
        Box(
            Modifier.fillMaxWidth().height(320.px).graphicsLayer { alpha = shade * words }
                .background(Brush.verticalGradient(0f to Pal.BaseDeep.copy(alpha = 0.82f), 1f to Color.Transparent)),
        )
        Box(
            Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(440.px).graphicsLayer { alpha = shade }
                .background(Brush.verticalGradient(0f to Color.Transparent, 0.5f to Pal.BaseDeep.copy(alpha = 0.6f), 1f to Pal.BaseDeep.copy(alpha = 0.9f))),
        )

        if (full) {
            Row(
                Modifier.padding(start = (SIDE - 12).px, top = 96.px).graphicsLayer { alpha = words },
                horizontalArrangement = Arrangement.spacedBy(36.px),
            ) {
                tops.forEachIndexed { i, c ->
                    TopIcon(
                        c.icon, c.word, fr(c.key),
                        Modifier.focusProperties {
                            down = fr("play"); up = FocusRequester.Cancel
                            if (i == 0) left = FocusRequester.Cancel
                            if (i == tops.lastIndex) right = FocusRequester.Cancel
                        },
                        onFocus = { st.lastKey = c.key }, onClick = c.open,
                    )
                }
            }
            Column(
                Modifier.align(Alignment.TopEnd).padding(end = SIDE.px, top = 112.px).widthIn(max = 900.px).graphicsLayer { alpha = words },
                horizontalAlignment = Alignment.End,
            ) {
                Text(st.title, style = ts(32, 40, 700).copy(shadow = WordShadow), maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (st.sub.isNotEmpty()) Text(st.sub, style = ts(30, 38, 700).copy(shadow = WordShadow), maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }

        BarRow(st, a, fr("play"), up = tops.firstOrNull()?.let { fr(st.lastKey.takeIf { k -> k.startsWith("top") && tops.any { t -> t.key == k } } ?: it.key) },
            down = pills.firstOrNull()?.let { fr(it.key) }, bar = bar)

        if (full && pills.isNotEmpty()) {
            Row(
                Modifier.align(Alignment.TopStart).fillMaxWidth().offset(y = (ROW_Y + PLAY_D / 2 + 24).px).graphicsLayer { alpha = words },
                horizontalArrangement = Arrangement.spacedBy(14.px, Alignment.CenterHorizontally),
            ) {
                pills.forEachIndexed { i, c ->
                    val on = c.key.startsWith("pill/") && st.combos.getOrNull(c.key.removePrefix("pill/").toIntOrNull() ?: -1)?.selected == true
                    Pill(
                        c.word, if (c.word.isEmpty()) c.icon else null, on, fr(c.key),
                        Modifier.focusProperties {
                            up = fr("play"); down = FocusRequester.Cancel
                            if (i == 0) left = FocusRequester.Cancel
                            if (i == pills.lastIndex) right = FocusRequester.Cancel
                        },
                        onFocus = { st.lastKey = c.key }, onClick = c.open,
                    )
                }
            }
        }
        ScrubPreview(st, bar)
    }
}

/** A bare icon. Focused, it grows a little and its name appears under it in small capitals. */
@Composable
private fun TopIcon(icon: ImageVector, word: String, fr: FocusRequester, modifier: Modifier, onFocus: () -> Unit, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val t = ringOf(focused)
    Box(modifier.width(108.px), contentAlignment = Alignment.TopCenter) {
        Box(
            Modifier.size(84.px).control(fr, { focused = it; if (it) onFocus() }, onClick)
                .graphicsLayer { val s = 1f + 0.12f * t.value; scaleX = s; scaleY = s; alpha = 0.78f + 0.22f * t.value },
            contentAlignment = Alignment.Center,
        ) { Icon(icon, word, tint = Pal.Ink, modifier = Modifier.size(54.px)) }
        // wider than the icon's slot and centred under it: takes no room, so nothing moves when it shows
        Text(
            word.uppercase(), style = ts(21, 26, 700, track = 0.06f), maxLines = 1, softWrap = false,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            modifier = Modifier.requiredWidth(300.px).padding(top = 96.px).graphicsLayer { alpha = t.value },
        )
    }
}

/** A grey pill under the bar; the one in use has a tick. Focused, it turns white. */
@Composable
private fun Pill(word: String, icon: ImageVector?, selected: Boolean, fr: FocusRequester, modifier: Modifier, onFocus: () -> Unit, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val fill = focusFill(focused, Pal.Ink.copy(alpha = 0.28f), Pal.Ink)
    val ink = focusFill(focused, Pal.Ink, Pal.OnInk)
    Row(
        modifier.height(PILL_H.px).clip(RoundedCornerShape(8.px)).background(fill)
            .control(fr, { focused = it; if (it) onFocus() }, onClick)
            .padding(horizontal = if (word.isEmpty()) 14.px else 20.px),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (icon != null) Icon(icon, null, tint = ink, modifier = Modifier.size(28.px))
        if (selected) {
            Icon(AppIcons.Check, null, tint = ink, modifier = Modifier.size(26.px))
            Spacer(Modifier.width(10.px))
        }
        if (word.isNotEmpty()) Text(word, style = ts(25, 30, 700, ink), maxLines = 1)
    }
}

/** The round Play/Pause, the time watched, the bar and the time left, on one line. */
@Composable
private fun BoxScope.BarRow(
    st: PlayerState, a: PlayerActions, fr: FocusRequester, up: FocusRequester?, down: FocusRequester?,
    bar: androidx.compose.runtime.MutableState<Offset>,
) {
    var focused by remember { mutableStateOf(false) }
    val full = st.mode == Mode.Full
    val looking = st.scrub != null || st.mode == Mode.Scrub
    val big by animateFloatAsState(if (looking || (focused && full)) 1f else 0f, Move.fade(Move.Focus), label = "bar")
    val ring = ringOf(focused && full)
    Row(
        Modifier.align(Alignment.TopStart).fillMaxWidth().offset(y = (ROW_Y - PLAY_D / 2).px)
            .padding(start = SIDE.px, end = SIDE.px).height(PLAY_D.px),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // the focus of the whole player: OK plays or pauses (or lands a skip), Left/Right skip, as with nothing on screen
        Box(
            Modifier.size(PLAY_D.px)
                .graphicsLayer { val s = 1f + 0.06f * ring.value; scaleX = s; scaleY = s }
                .clip(androidx.compose.foundation.shape.CircleShape)
                .background(if (full) Pal.Ink else Pal.Ink.copy(alpha = 0.0f))
                .focusRequester(fr)
                .focusProperties {
                    if (up != null) this.up = up
                    this.down = down ?: FocusRequester.Cancel
                    left = FocusRequester.Cancel; right = FocusRequester.Cancel
                }
                .onFocusChanged { focused = it.isFocused; st.barFocused = it.isFocused }
                .onKeyEvent { e ->
                    if (e.type != KeyEventType.KeyDown) return@onKeyEvent false
                    when (e.key) {
                        Key.DirectionCenter, Key.Enter, Key.NumPadEnter -> {
                            if (e.nativeKeyEvent.repeatCount == 0) a.togglePlay() // a tap's jump has already landed
                            true
                        }
                        else -> false
                    }
                }
                .focusable(),
            contentAlignment = Alignment.Center,
        ) {
            if (full) Icon(if (st.playing) AppIcons.Pause else AppIcons.Play, if (st.playing) "Pause" else "Play", tint = Pal.OnInk, modifier = Modifier.size(44.px))
        }
        Spacer(Modifier.width(36.px))
        Clock(st, left = false)
        Spacer(Modifier.width(28.px))
        val accent = Pal.Accent
        Canvas(Modifier.weight(1f).height(40.px).onGloballyPositioned { bar.value = Offset(it.positionInRoot().x, it.size.width.toFloat()) }) {
            // read while drawing: the moving playhead redraws the bar and nothing else
            val dur = st.dur.coerceAtLeast(1).toFloat()
            val at = ((st.scrub ?: st.pos) / dur).coerceIn(0f, 1f) * size.width
            val buf = (st.buffered / dur).coerceIn(0f, 1f) * size.width
            val h = (5 + 3 * big) * Px.screen
            val y = (size.height - h) / 2
            val r = CornerRadius(h / 2)
            drawRoundRect(Pal.Ink.copy(alpha = 0.3f), Offset(0f, y), Size(size.width, h), r)
            drawRoundRect(Pal.Ink.copy(alpha = 0.22f), Offset(0f, y), Size(buf, h), r)
            drawRoundRect(accent, Offset(0f, y), Size(at.coerceAtLeast(h), h), r)
            if (big > 0f) drawCircle(accent, 13 * Px.screen * big, Offset(at, size.height / 2))
        }
        Spacer(Modifier.width(28.px))
        Clock(st, left = true)
    }
}

/** The time watched, or the time left; each on its own, so the clock redraws only itself. */
@Composable
private fun Clock(st: PlayerState, left: Boolean) {
    val at = st.scrub ?: st.pos
    val t = if (left) (st.dur - at).coerceAtLeast(0) else at
    Text(if (st.dur > 0) clock(t) else "", style = ts(31, 38, 700, tabular = true).copy(shadow = WordShadow), maxLines = 1)
}

/**
 * While you look for a place: the picture you'd land on, over the playhead, with its time under it.
 * Before the pictures arrive, just the time.
 */
@Composable
private fun BoxScope.ScrubPreview(st: PlayerState, bar: androidx.compose.runtime.State<Offset>) {
    val looking = st.mode == Mode.Scrub // only while holding: a tap just jumps, as on Netflix
    val shown by animateFloatAsState(if (looking) 1f else 0f, Move.fade(Move.Focus), label = "preview")
    if (shown == 0f) return
    val store = st.thumbs
    val pictures = store != null && store.ready
    val w = if (pictures) THUMB_W else 180
    Column(
        Modifier.align(Alignment.BottomStart).padding(bottom = (1080 - ROW_Y + 36).px)
            .offset {
                val k = Px.screen
                val dur = st.dur.coerceAtLeast(1).toFloat()
                val b = bar.value // read here: the picture moves once the bar has been placed
                val x = b.x + ((st.scrub ?: st.pos) / dur).coerceIn(0f, 1f) * b.y - w * k / 2
                val screen = 1920 * k
                IntOffset(x.coerceIn(SIDE * k, screen - SIDE * k - w * k).roundToInt(), 0)
            }
            .graphicsLayer { alpha = shown }
            .width(w.px),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        if (pictures) {
            // new pictures only when the position crosses a 10 s step, however fast the time runs
            val step by remember { derivedStateOf { (st.scrub ?: st.pos) / 10_000 } }
            Box(Modifier.size(THUMB_W.px, THUMB_H.px).softShadow(ShadowKind.Small).clip(Corner).background(Pal.Raised)) {
                ThumbPicture(store!!, step * 10_000)
            }
            Spacer(Modifier.height(12.px))
        }
        Text(clock(st.scrub ?: st.pos), style = ts(30, 36, 700, tabular = true).copy(shadow = WordShadow), maxLines = 1)
    }
}

/** A scrub picture; the previous one stays up until this one is read, so nothing flashes empty. */
@Composable
private fun ThumbPicture(store: ThumbStore, ms: Long) {
    val last = remember { arrayOfNulls<ImageBitmap>(1) }
    store.at(ms)?.let { last[0] = it }
    last[0]?.let { Image(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
}

// ---------- audio & subtitles ----------

private class Choice(val title: String, val detail: String, val selected: Boolean, val pick: () -> Unit)

/** One panel along the bottom: audio, subtitles and their size side by side. The film plays on behind it. */
@Composable
private fun TracksPanel(st: PlayerState, a: PlayerActions) {
    val fr = remember { FocusRequester() }
    LaunchedEffect(Unit) { delay(32); runCatching { fr.requestFocus() } }
    Box(Modifier.fillMaxSize()) {
        Box(
            Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(640.px)
                .background(Brush.verticalGradient(0f to Color.Transparent, 0.3f to Pal.BaseDeep.copy(alpha = 0.86f), 1f to Pal.BaseDeep.copy(alpha = 0.97f)))
                .pointerInput(Unit) { detectTapGestures { } },
        )
        Row(
            Modifier.align(Alignment.BottomStart).padding(start = (EDGE - 24).px, end = EDGE.px, bottom = 64.px),
            horizontalArrangement = Arrangement.spacedBy(56.px),
        ) {
            val audio = st.audio.map { Choice(it.title, it.detail, it.selected, it.pick) }
            val text = st.text.map { Choice(it.title, it.detail, it.selected, it.pick) }
            val sizeFirst = st.tracksAt == 2 && text.isNotEmpty()
            TrackColumn("Audio", audio, 640, if (audio.isNotEmpty() && !sizeFirst) fr else null)
            TrackColumn("Subtitles", text, 520, if (audio.isEmpty() && !sizeFirst) fr else null, empty = "None in this file")
            if (text.isNotEmpty()) TrackColumn("Subtitle size", SubSizes.mapIndexed { i, s -> Choice(s, "", st.subSize == i) { a.subSize(i) } }, 300, if (sizeFirst) fr else null)
        }
    }
}

@Composable
private fun TrackColumn(title: String, options: List<Choice>, width: Int, fr: FocusRequester?, empty: String = "") {
    val sel = options.indexOfFirst { it.selected }.coerceAtLeast(0)
    val state = rememberLazyListState(initialFirstVisibleItemIndex = (sel - 2).coerceAtLeast(0))
    Column(Modifier.width(width.px)) {
        Text(title, style = ts(30, 36, 700), modifier = Modifier.padding(start = 24.px, bottom = 18.px))
        if (options.isEmpty()) Text(empty, style = ts(26, 32, 600, Pal.Muted), modifier = Modifier.padding(start = 24.px))
        LazyColumn(Modifier.heightIn(max = (5 * 64 + 4 * 6).px), state = state, verticalArrangement = Arrangement.spacedBy(6.px)) {
            itemsIndexed(options) { i, o ->
                TrackRow(
                    o, if (i == sel) fr else null,
                    Modifier.onPreviewKeyEvent { e -> // Up and Down stay inside the list
                        e.type == KeyEventType.KeyDown && ((e.key == Key.DirectionUp && i == 0) || (e.key == Key.DirectionDown && i == options.lastIndex))
                    },
                )
            }
        }
    }
}

/** A row: a check if it's what plays now, the name, and what tells it apart. Focused, it turns white. */
@Composable
private fun TrackRow(o: Choice, fr: FocusRequester?, modifier: Modifier) {
    var focused by remember { mutableStateOf(false) }
    val fill = focusFill(focused, Pal.Ink.copy(alpha = 0f), Pal.Ink)
    val ink = focusFill(focused, if (o.selected) Pal.Ink else Pal.InkSoft, Pal.OnInk)
    val soft = focusFill(focused, Pal.Muted, Pal.OnInkSoft)
    Row(
        modifier.fillMaxWidth().height(64.px).clip(Corner).background(fill)
            .control(fr, { focused = it }, o.pick).padding(start = 16.px, end = 20.px),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.width(44.px)) { if (o.selected) Icon(AppIcons.Check, null, tint = ink, modifier = Modifier.size(28.px)) }
        Text(o.title, style = ts(28, 34, if (o.selected) 700 else 600, ink), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
        if (o.detail.isNotEmpty()) Text(o.detail, style = ts(23, 28, 600, soft), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 16.px))
    }
}

// ---------- episodes ----------

/** The episodes in a row of stills along the bottom, starting at the one playing. Back closes it. */
@Composable
private fun EpisodesPanel(st: PlayerState) {
    val pics = st.episodes
    val fr = remember { FocusRequester() }
    val sel = pics.indexOfFirst { it.selected }.coerceAtLeast(0)
    val state = rememberLazyListState(initialFirstVisibleItemIndex = (sel - 1).coerceAtLeast(0))
    LaunchedEffect(Unit) { delay(32); runCatching { fr.requestFocus() } }
    Box(Modifier.fillMaxSize()) {
        Column(
            Modifier.align(Alignment.BottomStart).fillMaxWidth()
                .background(Brush.verticalGradient(0f to Color.Transparent, 0.25f to Pal.BaseDeep.copy(alpha = 0.86f), 1f to Pal.BaseDeep.copy(alpha = 0.97f)))
                .padding(top = 120.px, bottom = 56.px),
        ) {
            Text("Episodes", style = ts(34, 40, 700), modifier = Modifier.padding(start = EDGE.px))
            LazyRow(
                state = state,
                contentPadding = PaddingValues(horizontal = EDGE.px, vertical = 24.px),
                horizontalArrangement = Arrangement.spacedBy(28.px),
            ) {
                itemsIndexed(pics) { i, p -> EpisodeCard(p, if (i == sel) fr else null, first = i == 0, last = i == pics.lastIndex) }
            }
        }
    }
}

@Composable
private fun EpisodeCard(p: TrackOption, fr: FocusRequester?, first: Boolean, last: Boolean) {
    var focused by remember { mutableStateOf(false) }
    Column(Modifier.width(400.px)) {
        Box(
            Modifier.fillMaxWidth().aspectRatio(16f / 9f).lift(ringOf(focused)).clip(Corner).background(Pal.Raised)
                .onPreviewKeyEvent { e ->
                    e.type == KeyEventType.KeyDown && when (e.key) { // the row keeps the remote until Back
                        Key.DirectionUp, Key.DirectionDown -> true
                        Key.DirectionLeft -> first
                        Key.DirectionRight -> last
                        else -> false
                    }
                }
                .control(fr, { focused = it }, p.pick),
        ) {
            p.image?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
        }
        Text(p.title, style = ts(26, 32, 600, if (focused) Pal.Ink else Pal.InkSoft), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 18.px))
        Text(
            if (p.selected) "Now playing" else p.detail, style = ts(22, 28, 600, if (p.selected) Pal.AccentText else Pal.Muted),
            maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 4.px),
        )
    }
}

// ---------- offers: skip the intro, the next episode ----------

/** "Skip intro": lit while it's what OK would do, otherwise glass that lights when focused. */
@Composable
private fun OfferPill(word: String, icon: ImageVector?, armed: Boolean, modifier: Modifier = Modifier, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val lit = armed || focused
    val fill = focusFill(lit, Pal.Glass, Pal.Ink)
    val ink = focusFill(lit, Pal.Ink, Pal.OnInk)
    Row(
        modifier.softShadow(ShadowKind.Button).clip(Corner).background(fill).control(null, { focused = it }, onClick)
            .height(BUTTONS.px).padding(start = 26.px, end = 30.px),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (icon != null) {
            Icon(icon, null, tint = ink, modifier = Modifier.size(26.px))
            Spacer(Modifier.width(14.px))
        }
        Text(word, style = ts(27, 34, 700, ink), maxLines = 1)
    }
}

/**
 * In the credits: "Watch credits" beside "Next episode", which fills with white as the countdown runs.
 * OK takes the next episode, Back stays for the credits.
 */
@Composable
private fun NextUp(st: PlayerState, o: Offer, armed: Boolean, a: PlayerActions) {
    val p by animateFloatAsState((1f - st.nextLeft / 10_000f).coerceIn(0f, 1f), tween(260, easing = LinearEasing), label = "next")
    Column(horizontalAlignment = Alignment.End) {
        DotLine(listOf(o.detail, o.title).filter { it.isNotEmpty() }, ts(24, 30, 600, Pal.InkSoft), gap = 12)
        Spacer(Modifier.height(16.px))
        Row(horizontalArrangement = Arrangement.spacedBy(16.px), verticalAlignment = Alignment.CenterVertically) {
            OfferPill("Watch credits", null, armed = false, onClick = a::dismissOffer)
            var focused by remember { mutableStateOf(false) }
            val t = ringOf(focused || armed)
            Box(
                Modifier.graphicsLayer { val s = 1f + 0.04f * t.value; scaleX = s; scaleY = s }
                    .softShadow(ShadowKind.Button).clip(Corner).background(Pal.Glass)
                    .control(null, { focused = it }, a::takeOffer)
                    .drawBehind { drawRect(Pal.Ink, size = Size(size.width * p, size.height)) }
                    .height(BUTTONS.px),
                contentAlignment = Alignment.CenterStart,
            ) {
                NextWords(Pal.Ink)
                // the same words in dark, shown only where the white has reached
                Box(Modifier.drawWithContent { clipRect(right = size.width * p) { this@drawWithContent.drawContent() } }) { NextWords(Pal.OnInk) }
            }
        }
    }
}

@Composable
private fun NextWords(ink: Color) {
    Row(Modifier.padding(start = 26.px, end = 30.px), verticalAlignment = Alignment.CenterVertically) {
        Icon(AppIcons.SkipNext, null, tint = ink, modifier = Modifier.size(26.px))
        Spacer(Modifier.width(14.px))
        Text("Next episode", style = ts(27, 34, 700, ink), maxLines = 1)
    }
}

// ---------- errors ----------

@Composable
fun ErrorCard(e: PlayError, a: PlayerActions) {
    val fr = remember { FocusRequester() }
    LaunchedEffect(e) { delay(16); runCatching { fr.requestFocus() } }
    Box(Modifier.fillMaxSize().background(Pal.BaseDeep.copy(alpha = 0.86f)), contentAlignment = Alignment.Center) {
        Column(Modifier.widthIn(max = 880.px).clip(Corner).background(Pal.Raised).padding(48.px)) {
            Text(e.title, style = ts(40, 48, 700))
            Spacer(Modifier.height(14.px))
            Text(e.message, style = ts(27, 36, 500, Pal.InkSoft))
            if (e.detail.isNotEmpty()) {
                Spacer(Modifier.height(14.px))
                Text(e.detail, style = ts(21, 28, 500, Pal.Faint), maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
            Spacer(Modifier.height(36.px))
            Row(horizontalArrangement = Arrangement.spacedBy(16.px)) {
                GlassButton("Try again", null, fr = fr, onClick = a::retry)
                GlassButton("Close", null, onClick = a::close)
            }
        }
    }
}
