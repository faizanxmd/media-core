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
import androidx.compose.animation.expandHorizontally
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.scaleIn
import androidx.compose.animation.scaleOut
import androidx.compose.animation.shrinkHorizontally
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.ui.platform.LocalContext
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
 * The player, after the big streaming apps' TV players.
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
    var stallNote by mutableStateOf<String?>(null) // calm message under the spinner if buffering drags on
    var error by mutableStateOf<PlayError?>(null)
    var audio by mutableStateOf<List<TrackOption>>(emptyList())
    var text by mutableStateOf<List<TrackOption>>(emptyList())
    var subSize by mutableIntStateOf(1) // 0 small, 1 medium, 2 large
    var episodes by mutableStateOf<List<TrackOption>>(emptyList())
    var thumbs: ThumbStore? = null // scrub pictures (TV)
    var combos by mutableStateOf<List<TrackOption>>(emptyList()) // the likely audio and subtitle pairs, as pills under the bar
    var tracksAt = 0 // the panel opens on audio (0) or on subtitle size (2)
    var focusTo: String? = null // where focus lands when the controls show: the bar unless this names an icon or a pill
    var lastKey by mutableStateOf("bar") // the icon or pill focus was last on, to come back to after a panel
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
private const val THUMB_W = 352
private const val THUMB_H = 198

@Composable
fun PlayerOverlay(st: PlayerState, a: PlayerActions, tv: Boolean, onChrome: (Float) -> Unit) {
    val chrome = st.mode != Mode.Hidden && st.sheet == null && st.error == null
    // subtitles rise above whatever is showing (a fraction of the screen's height)
    val lift = when {
        st.error != null -> 0f
        st.sheet == Sheet.Tracks -> 0.42f // clear of the drawer, so size changes show on live subtitles
        st.sheet != null -> 0.40f
        st.mode == Mode.Full -> 0.22f
        st.mode == Mode.Seek -> 0.14f
        else -> 0f
    }
    LaunchedEffect(lift) { onChrome(lift) }
    Box(Modifier.fillMaxSize().pointerInput(Unit) { detectTapGestures(onTap = { a.tapped() }) }) {
        AnimatedVisibility(!st.firstFrame && st.error == null, enter = fadeIn(tween(0)), exit = fadeOut(Move.fade(Move.Video))) { Starting(st) }
        QuietLoading(st.firstFrame && st.buffering && st.error == null && st.mode != Mode.Scrub, st.stallNote, Modifier.align(Alignment.Center))

        SkipMark(st)

        AnimatedVisibility(chrome, enter = fadeIn(Move.fade(Move.Focus)), exit = fadeOut(Move.fade(Move.Screen))) { Chrome(st, a, tv) }
        AnimatedVisibility(
            st.sheet == Sheet.Tracks, Modifier.align(Alignment.BottomCenter),
            enter = slideInVertically(Move.slide(Move.Screen)) { it / 4 } + fadeIn(Move.fade(Move.Screen)),
            exit = slideOutVertically(Move.slide(Move.Focus)) { it / 4 } + fadeOut(Move.fade(Move.Focus)),
        ) { TracksPanel(st, a) }
        AnimatedVisibility(st.sheet == Sheet.Episodes, enter = fadeIn(Move.fade(Move.Screen)), exit = fadeOut(Move.fade(Move.Focus))) { EpisodesPanel(st) }

        val offer = st.offer
        // not while the controls are up on the TV: focus can't reach it there, so it waits until they hide again
        if (offer != null && st.sheet == null && st.error == null && st.mode != Mode.Scrub && st.scrub == null && !(tv && st.mode == Mode.Full)) {
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
private fun QuietLoading(on: Boolean, note: String?, modifier: Modifier) {
    var show by remember { mutableStateOf(false) }
    LaunchedEffect(on) {
        show = false
        if (on) { delay(600); show = true }
    }
    AnimatedVisibility(on && show, modifier, enter = fadeIn(Move.fade(Move.Focus)), exit = fadeOut(Move.fade(Move.Focus))) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Spinner(76)
            AnimatedVisibility(note != null, enter = fadeIn(Move.fade(Move.Focus)), exit = fadeOut(Move.fade(Move.Focus))) {
                Text(
                    note ?: "", style = ts(26, 32, 600),
                    modifier = Modifier.padding(top = 24.px)
                        .clip(Corner).background(Pal.BaseDeep.copy(alpha = 0.72f))
                        .padding(horizontal = 28.px, vertical = 12.px),
                )
            }
        }
    }
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

// ---------- the controls: one calm bottom dock, top of the screen stays clear ----------
//
// Nothing in the top corners. Over a single bottom gradient:
// - Title and episode line, left-aligned just above the bar.
// - Timeline bar (the focus anchor): OK plays/pauses, Left/Right skips or scrubs, Down opens actions.
// - A split row below the bar: panels on the left, transport jumps on the right, matte dark keys when focused.

private const val SIDE = 112
private const val PLAY_D = 72 // offer clearance above the bar
private const val ROW_Y = 892 // the timeline bar's vertical anchor on the 1080 board
private const val ACTION_H = 56
private const val STRIP_FRAMES = 7 // scrub filmstrip: odd, so one frame sits dead center
private const val STRIP_W = 288
private const val STRIP_H = 162
private const val STRIP_GAP = 8

private val NextEp by lazy { lineIcon("NextEp", "M6 4.5v15L16 12z" to 2f, "M19 4.5v15" to 2f) }
private val Captions by lazy { lineIcon("Captions", "M5 5.5h14a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-9a2 2 0 0 1 2-2z" to 2.2f, "M7 11h4M13 11h4M7 14.5h7M16 14.5h1" to 2.2f) }

private fun lineIcon(name: String, vararg parts: Pair<String, Float>) =
    ImageVector.Builder(name, 24.dp, 24.dp, 24f, 24f).apply {
        parts.forEach { (d, w) ->
            addPath(addPathNodes(d), stroke = SolidColor(Color.White), strokeLineWidth = w, strokeLineCap = StrokeCap.Round, strokeLineJoin = StrokeJoin.Round)
        }
    }.build()

@Composable
private fun Chrome(st: PlayerState, a: PlayerActions, tv: Boolean) {
    val full = st.mode == Mode.Full
    val bar = remember { mutableStateOf(Offset.Zero) }
    val requesters = remember { mutableMapOf<String, FocusRequester>() }
    fun fr(k: String) = requesters.getOrPut(k) { FocusRequester() }

    class Action(val icon: ImageVector, val word: String, val panel: Boolean, val run: () -> Unit) { var key = "" }
    // left: panels (icon + word); right: transport jumps (icon, word on focus). One ordered list, keys act/0, act/1, ...
    val leftActions = buildList {
        if (st.audio.size > 1 || st.text.isNotEmpty()) add(Action(Captions, "Audio & Subtitles", true) { st.tracksAt = 0; a.openSheet(Sheet.Tracks) })
        if (st.episodes.size > 1) add(Action(AppIcons.Episodes, "Episodes", true) { a.openSheet(Sheet.Episodes) })
    }
    val rightActions = buildList {
        add(Action(Icons.StartOver, "Start over", false, a::startOver))
        if (st.hasNext) add(Action(NextEp, "Next episode", false, a::next))
    }
    val actions = (leftActions + rightActions).onEachIndexed { i, act -> act.key = "act/$i" }

    val panelOpen = st.sheet != null
    LaunchedEffect(full, panelOpen) {
        if (!tv || !full || panelOpen) return@LaunchedEffect
        delay(16)
        val want = st.focusTo?.takeIf { k -> actions.any { it.key == k } }
            ?: if (st.focusTo?.startsWith("act/") == true) actions.firstOrNull()?.key else null
        runCatching { fr(want ?: "bar").requestFocus() }
        st.focusTo = null
    }

    // the title and actions step aside while scrubbing so the scrub filmstrip owns the stage
    val words by animateFloatAsState(if (full && st.mode != Mode.Scrub) 1f else 0f, Move.fade(Move.Focus), label = "words")
    val shade by animateFloatAsState(if (full) 1f else 0.7f, Move.fade(Move.Screen), label = "shade")

    Box(Modifier.fillMaxSize()) {
        // One smooth bottom gradient; the upper screen stays completely clear
        Box(
            Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(540.px).fadeBy { shade }
                .background(
                    Brush.verticalGradient(
                        0f to Color.Transparent,
                        0.42f to Pal.BaseDeep.copy(alpha = 0.56f),
                        1f to Pal.BaseDeep.copy(alpha = 0.92f),
                    ),
                ),
        )

        if (full) {
            Column(
                Modifier.align(Alignment.BottomStart)
                    .padding(start = SIDE.px, end = SIDE.px, bottom = (1080 - ROW_Y + 42).px)
                    .widthIn(max = 1200.px)
                    .fadeBy { words },
            ) {
                HeaderTitle(st)
                if (st.sub.isNotEmpty()) {
                    Text(
                        st.sub,
                        style = ts(25, 32, 500, Pal.InkSoft.copy(alpha = 0.62f)).copy(shadow = ChromeShadow),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(top = 6.px),
                    )
                }
            }
            EndsAt(st, Modifier.padding(end = SIDE.px, bottom = (1080 - ROW_Y + 42).px).fadeBy { words })
        }

        BarRow(st, a, fr("bar"), up = null, down = actions.firstOrNull()?.let { fr(it.key) }, bar = bar)

        if (full && actions.isNotEmpty()) {
            Row(
                Modifier.align(Alignment.TopStart).fillMaxWidth()
                    .padding(start = (SIDE - 20).px, end = (SIDE - 20).px)
                    .offset(y = (ROW_Y + 52).px)
                    .fadeBy { words },
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                @Composable
                fun ActionKey(i: Int) {
                    val act = actions[i]
                    DockAction(
                        act.icon, act.word, act.panel, fr(act.key),
                        Modifier.focusProperties {
                            up = fr("bar"); down = FocusRequester.Cancel
                            left = if (i == 0) FocusRequester.Cancel else fr(actions[i - 1].key)
                            right = if (i == actions.lastIndex) FocusRequester.Cancel else fr(actions[i + 1].key)
                        },
                        onFocus = { st.lastKey = act.key },
                        onClick = act.run,
                    )
                }
                Row(horizontalArrangement = Arrangement.spacedBy(12.px), verticalAlignment = Alignment.CenterVertically) {
                    for (i in leftActions.indices) ActionKey(i)
                }
                Row(horizontalArrangement = Arrangement.spacedBy(12.px), verticalAlignment = Alignment.CenterVertically) {
                    for (i in rightActions.indices) ActionKey(leftActions.size + i)
                }
            }
        }
        ScrubPreview(st, bar)
    }
}

/** The title's own logo art, left-aligned over the bar; its name in type while the art loads, or when there is none. */
@Composable
private fun HeaderTitle(st: PlayerState) {
    val logo = st.logo?.takeIf { it.isNotBlank() }
    var loaded by remember(logo) { mutableStateOf(false) }
    var failed by remember(logo) { mutableStateOf(false) }
    val art by animateFloatAsState(if (loaded) 1f else 0f, Move.fade(Move.Focus), label = "logo")
    Box(contentAlignment = Alignment.BottomStart) {
        // the name holds the space until the art is in, then gives way to it; never a blank gap
        if (logo == null || failed || art < 1f) {
            Text(
                st.title,
                style = ts(40, 48, 700, track = -0.015f).copy(shadow = ChromeShadow),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.graphicsLayer { alpha = 1f - art },
            )
        }
        if (logo != null && !failed) {
            AsyncImage(
                logo, st.title,
                Modifier.heightIn(max = 68.px).widthIn(max = 560.px).graphicsLayer { alpha = art },
                alignment = Alignment.BottomStart,
                contentScale = ContentScale.Fit,
                onSuccess = { loaded = true },
                onError = { failed = true },
            )
        }
    }
}

private val KeyMetal = Color(0xFF272C37)
private val KeyDim = Color(0xDC12151C) // the quieter key over the film, for what OK won't do

/** The matte key's surface: fill, a hairline edge and, on a lit key, a faint highlight along its top. */
private fun Modifier.metalKey(fill: Color, edge: Color, width: Float = 1.5f, glint: Float = 0f): Modifier {
    val shape = RoundedCornerShape(10.px)
    return clip(shape).background(fill).border(width.px, edge, shape).drawBehind {
        if (glint > 0f) drawLine(
            Pal.Ink.copy(alpha = glint), Offset(12 * Px.screen, 1.5f * Px.screen),
            Offset(size.width - 12 * Px.screen, 1.5f * Px.screen), 1.5f * Px.screen,
        )
    }
}

/**
 * A matte dark-metal key under the bar: bare at rest, charcoal with a hairline edge and the icon in the accent when
 * focused. Panels always show their word; transport jumps show only the icon until focused, then the word slides in.
 */
@Composable
private fun DockAction(icon: ImageVector, word: String, labelled: Boolean, fr: FocusRequester, modifier: Modifier, onFocus: () -> Unit, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val fill = focusFill(focused, Color.Transparent, KeyMetal)
    val edge = focusFill(focused, Color.Transparent, Pal.Ink.copy(alpha = 0.20f))
    val ink = focusFill(focused, Pal.Ink.copy(alpha = 0.56f), Pal.Ink)
    val tint = focusFill(focused, Pal.Ink.copy(alpha = 0.56f), Pal.AccentText)
    Row(
        modifier.height(ACTION_H.px).metalKey(fill, edge, glint = edge.alpha * 0.6f) // the highlight only on the lit key
            .control(fr, { focused = it; if (it) onFocus() }, onClick)
            .padding(horizontal = 20.px),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, null, tint = tint, modifier = Modifier.size(26.px))
        if (labelled) {
            Spacer(Modifier.width(12.px))
            Text(word, style = ts(24, 30, if (focused) 700 else 600, ink), maxLines = 1)
        } else {
            AnimatedVisibility(
                focused,
                enter = expandHorizontally(Move.fade(Move.Focus), expandFrom = Alignment.Start) + fadeIn(Move.fade(Move.Focus)),
                exit = shrinkHorizontally(Move.fade(Move.Focus), shrinkTowards = Alignment.Start) + fadeOut(Move.fade(Move.Focus)),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Spacer(Modifier.width(12.px))
                    Text(word, style = ts(24, 30, 700, ink), maxLines = 1)
                }
            }
        }
    }
}

/** A light shadow under the controls' words: the bottom shade does the real work. */
private val ChromeShadow get() = androidx.compose.ui.graphics.Shadow(Color.Black.copy(alpha = 0.5f), Offset(0f, 1 * Px.screen), 3 * Px.screen)

/** The timeline bar itself is the focus anchor: OK plays/pauses, Left/Right skips or scrubs, Up/Down opens controls. */
@Composable
private fun BoxScope.BarRow(
    st: PlayerState, a: PlayerActions, fr: FocusRequester, up: FocusRequester?, down: FocusRequester?,
    bar: androidx.compose.runtime.MutableState<Offset>,
) {
    var focused by remember { mutableStateOf(false) }
    val full = st.mode == Mode.Full
    val looking = st.scrub != null || st.mode == Mode.Scrub
    val knob by animateFloatAsState(if (looking || full) 1f else 0f, Move.fade(Move.Focus), label = "knob")
    val barFocus by animateFloatAsState(if (focused && full) 1f else 0f, Move.fade(Move.Focus), label = "barFocus")
    Column(
        Modifier.align(Alignment.TopStart).fillMaxWidth().offset(y = (ROW_Y - 36).px)
            .padding(start = SIDE.px, end = SIDE.px),
    ) {
        Row(
            Modifier.fillMaxWidth().height(56.px)
                .focusRequester(fr)
                .focusProperties {
                    this.up = up ?: FocusRequester.Cancel
                    this.down = down ?: FocusRequester.Cancel
                    left = FocusRequester.Cancel; right = FocusRequester.Cancel
                }
                .onFocusChanged { focused = it.isFocused; st.barFocused = it.isFocused; if (it.isFocused) st.lastKey = "bar" }
                .onKeyEvent { e ->
                    if (e.type != KeyEventType.KeyDown) return@onKeyEvent false
                    when (e.key) {
                        Key.DirectionCenter, Key.Enter, Key.NumPadEnter -> {
                            if (e.nativeKeyEvent.repeatCount == 0) a.togglePlay()
                            true
                        }
                        else -> false
                    }
                }
                .focusable(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Clock(st, left = false)
            Spacer(Modifier.width(28.px))
            val accent = Pal.Accent
            Canvas(Modifier.weight(1f).height(44.px).onGloballyPositioned { bar.value = Offset(it.positionInRoot().x, it.size.width.toFloat()) }) {
                val k = Px.screen
                val dur = st.dur.coerceAtLeast(1).toFloat()
                val at = ((st.scrub ?: st.pos) / dur).coerceIn(0f, 1f) * size.width
                // a hairline rail that firms up with focus
                val h = (5f + 3f * barFocus) * k
                val y = (size.height - h) / 2
                val r = CornerRadius(h / 2)
                drawRoundRect(Pal.Ink.copy(alpha = 0.22f + 0.06f * barFocus), Offset(0f, y), Size(size.width, h), r)
                drawRoundRect(Pal.Ink.copy(alpha = 0.88f + 0.12f * barFocus), Offset(0f, y), Size(at.coerceAtLeast(h), h), r)
                if (knob > 0f) {
                    // the playhead: an upright blade, taller and wider when the bar has focus
                    val bw = (4f + 2f * barFocus) * k
                    val bh = (22f + 18f * barFocus) * k
                    val cy = size.height / 2
                    if (barFocus > 0f) {
                        val hw = bw + 10f * k
                        val hh = bh + 10f * k
                        drawRoundRect(accent.copy(alpha = 0.35f * barFocus), Offset(at - hw / 2, cy - hh / 2), Size(hw, hh), CornerRadius(hw / 2))
                    }
                    drawRoundRect(Pal.Ink.copy(alpha = knob), Offset(at - bw / 2, cy - bh / 2), Size(bw, bh), CornerRadius(bw / 2))
                }
            }
            Spacer(Modifier.width(28.px))
            Clock(st, left = true)
        }
    }
}

/** The time watched, or the time left. Shown while scrubbing too, so the rail never changes width. */
@Composable
private fun Clock(st: PlayerState, left: Boolean) {
    val at = st.scrub ?: st.pos
    val t = if (left) (st.dur - at).coerceAtLeast(0) else at
    val ink = Pal.Ink.copy(alpha = if (left) 0.54f else 0.88f)
    Text(
        if (st.dur > 0) (if (left) "-" else "") + clock(t) else "",
        style = ts(26, 32, 600, ink, tabular = true).copy(shadow = ChromeShadow),
        maxLines = 1,
    )
}

/** "Ends 11:42 PM" while paused, alone and quiet on the right above the bar, level with the episode line. */
@Composable
private fun BoxScope.EndsAt(st: PlayerState, modifier: Modifier) {
    val rem = (st.dur - (st.scrub ?: st.pos)).coerceAtLeast(0)
    if (st.playing || st.dur <= 0 || rem <= 30_000L) return
    val ctx = LocalContext.current
    val fmt = remember(ctx) { android.text.format.DateFormat.getTimeFormat(ctx) }
    Text(
        "Ends ${fmt.format(java.util.Date(System.currentTimeMillis() + rem))}",
        style = ts(24, 30, 500, Pal.Ink.copy(alpha = 0.56f), tabular = true).copy(shadow = ChromeShadow),
        maxLines = 1,
        modifier = modifier.align(Alignment.BottomEnd),
    )
}

/**
 * Netflix-style filmstrip while holding Left/Right: seven even 16:9 frames run edge to edge right on top
 * of the bar, the center one lit with a white edge, the rest dimmed and fading out at the screen sides.
 * The time sits above the center frame.
 */
@Composable
private fun BoxScope.ScrubPreview(st: PlayerState, bar: androidx.compose.runtime.State<Offset>) {
    val looking = st.mode == Mode.Scrub // only while holding: a tap stays clean with the 10 s badge
    val shown by animateFloatAsState(if (looking) 1f else 0f, Move.fade(Move.Focus), label = "preview")
    if (shown == 0f) return
    val store = st.thumbs
    val pictures = store != null && store.ready
    val posMs = st.scrub ?: st.pos
    if (pictures) {
        val stepMs = store!!.stepMs.coerceAtLeast(1000L)
        // read the state inside derivedStateOf: a captured posMs would pin the strip to its first frame
        val step by remember(st, stepMs) { derivedStateOf { (st.scrub ?: st.pos) / stepMs } }
        LaunchedEffect(step) { store.prefetch(step * stepMs) }
        val stripW = STRIP_FRAMES * STRIP_W + (STRIP_FRAMES - 1) * STRIP_GAP // a little wider than the screen
        Column(
            Modifier.align(Alignment.BottomCenter)
                .padding(bottom = (1080 - ROW_Y + 32).px) // ~18 px clear of the bar's top edge
                .requiredWidth(stripW.px)
                .graphicsLayer { alpha = shown },
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                clock(posMs),
                style = ts(34, 40, 700, tabular = true).copy(shadow = ChromeShadow),
                maxLines = 1,
                modifier = Modifier.padding(bottom = 12.px),
            )
            Row(
                Modifier.drawWithContent {
                    drawContent()
                    val edge = Pal.BaseDeep
                    drawRect(
                        Brush.horizontalGradient(
                            0f to edge.copy(alpha = 0.9f),
                            0.1f to Color.Transparent,
                            0.9f to Color.Transparent,
                            1f to edge.copy(alpha = 0.9f),
                        ),
                    )
                },
                horizontalArrangement = Arrangement.spacedBy(STRIP_GAP.px),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                val half = STRIP_FRAMES / 2
                for (delta in -half..half) {
                    val tMs = (step + delta) * stepMs
                    val valid = tMs in 0L..st.dur
                    val center = delta == 0
                    Box(
                        Modifier.size(STRIP_W.px, STRIP_H.px)
                            .graphicsLayer { alpha = if (!valid) 0f else if (center) 1f else 0.65f }
                            .then(if (center) Modifier.softShadow(ShadowKind.Small) else Modifier)
                            .clip(RoundedCornerShape(4.px)).background(Pal.Raised)
                            .then(if (center) Modifier.border(3.px, Pal.Ink, RoundedCornerShape(4.px)) else Modifier),
                    ) {
                        if (valid) ThumbPicture(store, tMs)
                    }
                }
            }
        }
    } else {
        Column(
            Modifier.align(Alignment.BottomStart).padding(bottom = (1080 - ROW_Y + 48).px)
                .offset {
                    val k = Px.screen
                    val dur = st.dur.coerceAtLeast(1).toFloat()
                    val b = bar.value
                    val x = b.x + (posMs / dur).coerceIn(0f, 1f) * b.y - 90 * k
                    val screen = 1920 * k
                    IntOffset(x.coerceIn(SIDE * k, screen - SIDE * k - 180 * k).roundToInt(), 0)
                }
                .graphicsLayer { alpha = shown }
                .width(180.px),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(clock(posMs), style = ts(34, 40, 700, tabular = true).copy(shadow = ChromeShadow), maxLines = 1)
        }
    }
}

/**
 * A scrub picture. Until its own frame is read it shows the nearest one already in memory, then the
 * slot's previous one, so a slot never flashes empty or lags far behind.
 */
@Composable
private fun ThumbPicture(store: ThumbStore, ms: Long) {
    val last = remember { arrayOfNulls<ImageBitmap>(1) }
    val pic = store.at(ms) ?: store.near(ms)
    pic?.let { last[0] = it }
    last[0]?.let { Image(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
}

// ---------- audio & subtitles ----------

private class Choice(val title: String, val detail: String, val selected: Boolean, val pick: () -> Unit)

/**
 * Audio, subtitles and their size in a matte drawer across the bottom. The film and its subtitles, lifted clear,
 * play on above, so a size change shows at once. Left/Right moves between columns, Up/Down stays in one.
 */
@Composable
private fun TracksPanel(st: PlayerState, a: PlayerActions) {
    val fr = remember { FocusRequester() }
    LaunchedEffect(Unit) { delay(32); runCatching { fr.requestFocus() } }
    val hairline = Pal.Ink.copy(alpha = 0.12f)
    Box(
        Modifier.fillMaxWidth().height(416.px).background(Color(0xF00D0F14))
            .drawBehind { drawRect(hairline, size = Size(size.width, 1 * Px.screen)) }
            .pointerInput(Unit) { detectTapGestures { } },
    ) {
        Row(Modifier.fillMaxWidth().padding(horizontal = SIDE.px, vertical = 36.px)) {
            val audio = st.audio.map { Choice(it.title, it.detail, it.selected, it.pick) }
            val text = st.text.map { Choice(it.title, it.detail, it.selected, it.pick) }
            val sizeFirst = st.tracksAt == 2 && text.isNotEmpty()
            TrackColumn("AUDIO", audio, Modifier.weight(1.15f), if (audio.isNotEmpty() && !sizeFirst) fr else null, first = true)
            TrackDivider()
            TrackColumn(
                "SUBTITLES", text, Modifier.weight(1.15f), if (audio.isEmpty() && !sizeFirst) fr else null,
                empty = "None in this file", last = text.isEmpty(),
            )
            if (text.isNotEmpty()) {
                TrackDivider()
                TrackColumn(
                    "SUBTITLE SIZE", SubSizes.mapIndexed { i, s -> Choice(s, "", st.subSize == i) { a.subSize(i) } },
                    Modifier.weight(0.65f), if (sizeFirst) fr else null, last = true,
                )
            }
        }
    }
}

@Composable
private fun TrackDivider() {
    Box(Modifier.padding(horizontal = 28.px).width(1.px).height((4 * ROW_H + 18 + 42).px).background(Pal.Ink.copy(alpha = 0.08f)))
}

@Composable
private fun TrackColumn(
    title: String, options: List<Choice>, modifier: Modifier, fr: FocusRequester?,
    empty: String = "", first: Boolean = false, last: Boolean = false,
) {
    val sel = options.indexOfFirst { it.selected }.coerceAtLeast(0)
    val state = rememberLazyListState(initialFirstVisibleItemIndex = (sel - 3).coerceAtLeast(0))
    // the detail ("5.1 surround") only when it tells the rows apart; the same words on every row are noise
    val details = options.map { it.detail }.distinct().size > 1
    Column(modifier) {
        Text(title, style = ts(20, 24, 700, Pal.Ink.copy(alpha = 0.44f), track = 0.12f), modifier = Modifier.padding(start = 20.px, bottom = 18.px))
        if (options.isEmpty()) Text(empty, style = ts(26, 32, 600, Pal.Ink.copy(alpha = 0.44f)), modifier = Modifier.padding(start = 20.px))
        LazyColumn(
            Modifier.heightIn(max = (4 * ROW_H + 18).px), state = state,
            verticalArrangement = Arrangement.spacedBy(6.px),
        ) {
            itemsIndexed(options) { i, o ->
                TrackRow(
                    o, details, if (i == sel) fr else null,
                    Modifier.onPreviewKeyEvent { e -> // Up and Down stay inside the list; Left/Right stay inside the drawer
                        e.type == KeyEventType.KeyDown && when (e.key) {
                            Key.DirectionUp -> i == 0
                            Key.DirectionDown -> i == options.lastIndex
                            Key.DirectionLeft -> first
                            Key.DirectionRight -> last
                            else -> false
                        }
                    },
                )
            }
        }
    }
}

private const val ROW_H = 60

/** A row: a tick if it's what plays now, the name, and what tells it apart. Focused, it's the matte dark key. */
@Composable
private fun TrackRow(o: Choice, details: Boolean, fr: FocusRequester?, modifier: Modifier) {
    var focused by remember { mutableStateOf(false) }
    val fill = focusFill(focused, Color.Transparent, KeyMetal)
    val edge = focusFill(focused, Color.Transparent, Pal.Ink.copy(alpha = 0.20f))
    val ink = focusFill(focused, Pal.Ink.copy(alpha = if (o.selected) 0.92f else 0.56f), Pal.Ink)
    val soft = focusFill(focused, Pal.Ink.copy(alpha = 0.40f), Pal.Ink.copy(alpha = 0.64f))
    Row(
        modifier.fillMaxWidth().height(ROW_H.px).metalKey(fill, edge, glint = edge.alpha * 0.6f)
            .control(fr, { focused = it }, o.pick).padding(start = 20.px, end = 20.px),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.width(44.px)) { if (o.selected) Icon(AppIcons.Check, null, tint = Pal.AccentText, modifier = Modifier.size(28.px)) }
        Text(
            o.title, style = ts(28, 34, if (focused) 700 else if (o.selected) 600 else 500, ink),
            maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false),
        )
        if (details && o.detail.isNotEmpty()) Text(o.detail, style = ts(22, 28, 600, soft), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 14.px))
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
            // on the controls' own edge (SIDE), so the list lines up with Play and the icons it came from
            Text("Episodes", style = ts(34, 40, 700), modifier = Modifier.padding(start = SIDE.px))
            LazyRow(
                state = state,
                contentPadding = PaddingValues(horizontal = SIDE.px, vertical = 24.px),
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

/** "Skip intro": the matte dark key, lit while it's what OK would do or focused, a quieter one otherwise. */
@Composable
private fun OfferPill(word: String, icon: ImageVector?, armed: Boolean, modifier: Modifier = Modifier, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    val lit = armed || focused
    val fill = focusFill(lit, KeyDim, KeyMetal)
    val edge = focusFill(lit, Pal.Ink.copy(alpha = 0.10f), Pal.Ink.copy(alpha = 0.22f))
    val ink = focusFill(lit, Pal.Ink.copy(alpha = 0.68f), Pal.Ink)
    val tint = focusFill(lit, Pal.Ink.copy(alpha = 0.68f), Pal.AccentText)
    Row(
        modifier.height(BUTTONS.px).metalKey(fill, edge, width = if (lit) 1.5f else 1f, glint = if (lit) 0.13f else 0f)
            .control(null, { focused = it }, onClick)
            .padding(start = 24.px, end = 28.px),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (icon != null) {
            Icon(icon, null, tint = tint, modifier = Modifier.size(26.px))
            Spacer(Modifier.width(14.px))
        }
        Text(word, style = ts(27, 34, 700, ink), maxLines = 1)
    }
}

/**
 * In the credits: "Watch credits" beside "Next episode", whose thin accent line runs along its foot as the countdown runs.
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
            val lit = focused || armed
            val fill = focusFill(lit, KeyDim, KeyMetal)
            val edge = focusFill(lit, Pal.Ink.copy(alpha = 0.10f), Pal.Ink.copy(alpha = 0.22f))
            val ink = focusFill(lit, Pal.Ink.copy(alpha = 0.68f), Pal.Ink)
            val tint = focusFill(lit, Pal.Ink.copy(alpha = 0.68f), Pal.AccentText)
            val track = Pal.Ink.copy(alpha = 0.12f)
            val accent = Pal.Accent
            Row(
                Modifier.height(BUTTONS.px).metalKey(fill, edge, width = if (lit) 1.5f else 1f, glint = if (lit) 0.13f else 0f)
                    .drawWithContent {
                        drawContent()
                        // the countdown: a crisp line along the key's foot, filling left to right over the 10 s
                        val h = 4 * Px.screen
                        val y = size.height - h
                        drawRect(track, Offset(0f, y), Size(size.width, h))
                        drawRect(accent, Offset(0f, y), Size(size.width * p, h))
                    }
                    .control(null, { focused = it }, a::takeOffer)
                    .padding(start = 24.px, end = 28.px),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(AppIcons.SkipNext, null, tint = tint, modifier = Modifier.size(26.px))
                Spacer(Modifier.width(14.px))
                Text("Next episode", style = ts(27, 34, 700, ink), maxLines = 1)
            }
        }
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
