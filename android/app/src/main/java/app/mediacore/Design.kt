@file:OptIn(ExperimentalTextApi::class)

package app.mediacore

import android.provider.Settings
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathFillType
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.text.ExperimentalTextApi
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.LineHeightStyle
import androidx.compose.ui.layout.layout
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp

// The redesign's look (~/redesign-resources/tv-spec, home-brief.md). Every number here is read from the 1920 x 1080
// boards, so screens are written in board pixels: 120.px is 120 board pixels wherever the TV's density puts them.
// The player keeps its own look in Ui.kt and doesn't use any of this.

/** Board pixels to dp, set once from the screen's width. */
object Px {
    var k = 0.5f // 1920 px across a 960 dp TV screen
    var font = 1f // the system's text scale: the boards' sizes are fixed, so it's divided back out
    var screen = 1f // screen pixels per board pixel

    fun init(ctx: android.content.Context) {
        val m = ctx.resources.displayMetrics
        val wide = maxOf(m.widthPixels, m.heightPixels)
        screen = wide / 1920f
        k = screen / m.density
        font = ctx.resources.configuration.fontScale
    }
}

val Number.px: Dp get() = (toFloat() * Px.k).dp
val Number.fs: TextUnit get() = (toFloat() * Px.k / Px.font).sp

object Pal {
    val BaseDeep = Color(0xFF040507) // ends of fades, vignette corners
    val Base = Color(0xFF090B0E) // every screen
    val Raised = Color(0xFF14161B) // empty cards, panels
    val Ink = Color(0xFFF2F3F7) // titles, focused fills
    val InkSoft = Color(0xFFD5D7DD) // descriptions, values
    val Muted = Color(0xFFA5A9B2) // secondary text
    val Label = Color(0xFF8B909A) // small capitals over credits and figures
    val Faint = Color(0xFF6B707A) // middle dots, dates, episode numbers
    val RowTitle = Color(0xFFC9CCD3) // row titles while focus is down in the rows
    val Accent = Color(0xFF1D39F3) // big areas: Play, progress, Settings headings
    val Cobalt = Color(0xFF4F84FE) // small marks
    val Problem = Color(0xFFF08A84)
    val ProblemBg = Color(0xFF2A1416)
    val Glass = Color(0xD124272E) // rgba(36,39,46,.82): every resting control over art
    val Deep = Color(0xFF1F2F8C) // Play and Resume while focused: the brand's blue, deep enough not to glow
    val OnInk = Color(0xFF0B0D11) // text on a focused (white) control
    val OnInkSoft = Color(0xFF4A4F5A) // its quieter parts
}

val Corner get() = RoundedCornerShape(8.px)

private fun axes(w: Int, wdth: Float) = FontVariation.Settings(FontVariation.weight(w), FontVariation.width(wdth))

private fun archivo(wdth: Float) = FontFamily(
    listOf(400, 500, 600, 650, 700, 800).map { w -> Font(R.font.archivo, FontWeight(w), variationSettings = axes(w, wdth)) },
)

/** Archivo at its normal width, and condensed (width 87.5) for headings, meta lines and labels. */
val Archivo = archivo(100f)
val ArchivoCond = archivo(87.5f)

/** A text style in board pixels: size, line height, weight, colour; [cond] for the condensed cut, [track] in em. */
fun ts(size: Number, line: Number, weight: Int, color: Color = Pal.Ink, cond: Boolean = false, track: Float = 0f, tabular: Boolean = false) = TextStyle(
    fontFamily = if (cond) ArchivoCond else Archivo,
    fontSize = size.fs, lineHeight = line.fs, fontWeight = FontWeight(weight), color = color,
    letterSpacing = track.em,
    fontFeatureSettings = if (tabular) "tnum" else null,
    lineHeightStyle = LineHeightStyle(LineHeightStyle.Alignment.Center, LineHeightStyle.Trim.None),
)

/** One curve and three speeds for everything that moves (SPEC.md, Motion). */
object Move {
    val Ease = CubicBezierEasing(0.2f, 0f, 0f, 1f) // fast out, gentle stop
    const val Focus = 150
    const val Screen = 250
    const val Art = 400
    const val Settle = 300L // the remote has rested: only then does the art change
    const val Black = 200 // into and out of the player
    const val Video = 300

    /** Android's "remove animations": no slides, only short plain fades. */
    @Volatile var reduced = false

    fun read(ctx: android.content.Context) {
        reduced = runCatching { Settings.Global.getFloat(ctx.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) }.getOrDefault(1f) == 0f
    }

    /** A fade: its own duration, or a short plain one with animations off. */
    fun <T> fade(ms: Int): FiniteAnimationSpec<T> = tween(if (reduced) 100 else ms, easing = Ease)

    /** A slide: jumps straight there with animations off. */
    fun <T> slide(ms: Int): FiniteAnimationSpec<T> = if (reduced) snap() else tween(ms, easing = Ease)
}

// ---------- icons, drawn from the boards' own SVG ----------

private fun strokeIcon(name: String, vararg parts: Pair<String, Float>, fills: List<String> = emptyList()) =
    ImageVector.Builder(name, 24.dp, 24.dp, 24f, 24f).apply {
        parts.forEach { (d, w) ->
            addPath(addPathNodes(d), stroke = SolidColor(Color.White), strokeLineWidth = w, strokeLineCap = StrokeCap.Round, strokeLineJoin = StrokeJoin.Round)
        }
        fills.forEach { d -> addPath(addPathNodes(d), fill = SolidColor(Color.White), pathFillType = PathFillType.NonZero) }
    }.build()

private fun circle(cx: Float, cy: Float, r: Float) = "M${cx - r},${cy}a$r,$r 0 1,0 ${2 * r},0a$r,$r 0 1,0 ${-2 * r},0"

object Icons {
    val Settings by lazy {
        strokeIcon(
            "Settings",
            "M19.05 10.11 L21.51 10.66 L21.51 13.34 L19.05 13.89 L18.32 15.65 L19.67 17.78 L17.78 19.67 L15.65 18.32 L13.89 19.05 L13.34 21.51 L10.66 21.51 L10.11 19.05 L8.35 18.32 L6.22 19.67 L4.33 17.78 L5.68 15.65 L4.95 13.89 L2.49 13.34 L2.49 10.66 L4.95 10.11 L5.68 8.35 L4.33 6.22 L6.22 4.33 L8.35 5.68 L10.11 4.95 L10.66 2.49 L13.34 2.49 L13.89 4.95 L15.65 5.68 L17.78 4.33 L19.67 6.22 L18.32 8.35Z" to 1.75f,
            circle(12f, 12f, 3f) to 1.75f,
        )
    }
    val Info by lazy { strokeIcon("Info", circle(12f, 12f, 9.25f) to 1.9f, "M12 11V16.5" to 2.1f, fills = listOf(circle(12f, 7.6f, 1.25f))) }
    val StartOver by lazy { strokeIcon("StartOver", "M4.5 12a7.5 7.5 0 1 0 2.2-5.3" to 1.9f, "M4.2 3.6v4.2h4.2" to 1.9f) }
    val Down by lazy { strokeIcon("Down", "M6 9l6 6 6-6" to 2.2f) }
    val Left by lazy { strokeIcon("Left", "M15 6l-6 6 6 6" to 2.4f) }
    val Right by lazy { strokeIcon("Right", "M9 6l6 6-6 6" to 2.4f) }
    val Back by lazy { strokeIcon("Back", "M9 14L4 9l5-5" to 2f, "M4 9h10.5a5.5 5.5 0 0 1 0 11H11" to 2f) }

    /** Play, on a 13 x 15 box. */
    val Play by lazy {
        ImageVector.Builder("Play", 13.dp, 15.dp, 13f, 15f).apply {
            addPath(addPathNodes("M1.2 1.9A1.2 1.2 0 0 1 3 .9L11.6 6.4A1.3 1.3 0 0 1 11.6 8.6L3 14.1A1.2 1.2 0 0 1 1.2 13.1Z"), fill = SolidColor(Color.White))
        }.build()
    }
}

// ---------- the logo mark, top-left on every screen ----------

/**
 * The O of the wordmark (android/brand/mark.svg), cropped so the ring fills [size]: Montserrat's ring, thinner at the
 * top and bottom, around a lit core whose soft cobalt glow fills the counter.
 */
@Composable
fun LogoMark(modifier: Modifier = Modifier, size: Dp = 48.px) {
    Canvas(modifier.size(size)) {
        val s = this.size.width / 60f // the mark's ring is 60 units across
        val c = Offset(this.size.width / 2, this.size.height / 2)
        drawCircle(Brush.radialGradient(0f to Pal.Accent.copy(alpha = 0.7f), 1f to Pal.Accent.copy(alpha = 0f), center = c, radius = 20.114f * s), 20.114f * s, c)
        val ring = Path().apply {
            fillType = PathFillType.EvenOdd
            addOval(androidx.compose.ui.geometry.Rect(c, 30f * s))
            addOval(androidx.compose.ui.geometry.Rect(c.x - 18.499f * s, c.y - 20.114f * s, c.x + 18.499f * s, c.y + 20.114f * s))
        }
        drawPath(ring, Color(0xFFF4F5F8))
        drawCircle(
            Brush.radialGradient(0f to Color(0xFF7FA2FF), 0.55f to Color(0xFF3A5CF8), 1f to Pal.Accent, center = c, radius = 11.403f * s),
            11.403f * s, c,
        )
    }
}

// ---------- controls ----------

/** The screen's edges: content starts this far in from the left and right (the TV's safe area), and below the top. */
const val EDGE = 80
const val TOP = 60

/**
 * Focus on a picture (a card, an episode's still, a season's poster): it lifts 5% and a 3 px white border appears
 * just outside its edge, both over 150 ms. [t] is ringOf(focused). Gaps around pictures leave room for the lift.
 */
fun Modifier.lift(t: Float): Modifier = graphicsLayer {
    val s = 1f + 0.05f * t
    scaleX = s
    scaleY = s
}.drawWithContent {
    drawContent()
    if (t > 0f) {
        val w = 3 * Px.screen
        drawRoundRect(
            Pal.Ink.copy(alpha = t), topLeft = Offset(-w / 2, -w / 2), size = Size(size.width + w, size.height + w),
            cornerRadius = androidx.compose.ui.geometry.CornerRadius(8 * Px.screen + w / 2), style = Stroke(w),
        )
    }
}

/** How far along focus is: 0 to 1 over 150 ms. */
@Composable
fun ringOf(focused: Boolean): Float =
    animateFloatAsState(if (focused) 1f else 0f, Move.fade(Move.Focus), label = "ring").value

/** A control's fill as focus comes and goes (150 ms). */
@Composable
fun focusFill(focused: Boolean, rest: Color, on: Color): Color =
    animateColorAsState(if (focused) on else rest, Move.fade(Move.Focus), label = "fill").value

/** Shared plumbing: focus, a requester, OK. */
@Composable
fun Modifier.control(fr: FocusRequester?, onFocus: (Boolean) -> Unit, onClick: () -> Unit): Modifier =
    then(if (fr != null) Modifier.focusRequester(fr) else Modifier)
        .onFocusChanged { onFocus(it.isFocused) }
        .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)

/** A soft dark shadow under a control or card, drawn once as a picture (Shadows) and reused. */
fun Modifier.softShadow(kind: ShadowKind): Modifier = drawBehind {
    val sh = Shadows.get(kind, size.width.toInt(), size.height.toInt())
    drawImage(sh.bitmap.asImageBitmap(), Offset(-sh.pad.toFloat(), -sh.pad.toFloat() + sh.dy))
}

/** Every button's height. */
const val BUTTON_H = 76

/**
 * Play or Resume: grey like every control at rest, deep blue when focused. "Play | S2 E1", "Resume | 2 h 4 m left".
 * How far along a title is shows on its card, not here.
 */
@Composable
fun PlayButton(
    word: String,
    detail: String?,
    modifier: Modifier = Modifier,
    fr: FocusRequester? = null,
    onFocus: (Boolean) -> Unit = {},
    onClick: () -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    val fill = focusFill(focused, Pal.Glass, Pal.Deep)
    Row(
        modifier.softShadow(ShadowKind.Button).clip(Corner).background(fill)
            .control(fr, { focused = it; onFocus(it) }, onClick).height(BUTTON_H.px).padding(start = 30.px, end = 34.px),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Play, null, tint = Pal.Ink, modifier = Modifier.size(22.px, 26.px))
        Spacer(Modifier.width(16.px))
        Text(word, style = ts(29, 36, 700), maxLines = 1)
        if (detail != null) {
            Box(Modifier.padding(horizontal = 18.px).size(2.px, 30.px).background(Pal.Ink.copy(alpha = 0.25f)))
            Text(detail, style = ts(26, 32, 600, Pal.InkSoft, tabular = true), maxLines = 1)
        }
    }
}

/** A worded control: More info, Settings, Picture details while it's the only action. Focused, it turns white. */
@Composable
fun GlassButton(
    text: String,
    icon: ImageVector?,
    modifier: Modifier = Modifier,
    height: Int = BUTTON_H,
    textSize: Int = 29,
    weight: Int = 600,
    iconSize: Int = 30,
    iconGap: Int = 14,
    padStart: Int = 28,
    padEnd: Int = 32,
    fr: FocusRequester? = null,
    onFocus: (Boolean) -> Unit = {},
    onClick: () -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    val fill = focusFill(focused, Pal.Glass, Pal.Ink)
    val tint = focusFill(focused, Pal.Ink, Pal.OnInk)
    Row(
        modifier.clip(Corner).background(fill).control(fr, { focused = it; onFocus(it) }, onClick)
            .height(height.px).padding(start = padStart.px, end = padEnd.px),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (icon != null) {
            Icon(icon, null, tint = tint, modifier = Modifier.size(iconSize.px))
            Spacer(Modifier.width(iconGap.px))
        }
        Text(text, style = ts(textSize, textSize + 7, weight, tint), maxLines = 1)
    }
}

/** Settings, top-right on every screen that has it. */
@Composable
fun SettingsGlass(modifier: Modifier = Modifier, fr: FocusRequester, onFocus: (Boolean) -> Unit, onClick: () -> Unit) =
    GlassButton("Settings", Icons.Settings, modifier, height = 60, textSize = 25, iconSize = 28, iconGap = 12, padStart = 20, padEnd = 24, fr = fr, onFocus = onFocus, onClick = onClick)

/** A square icon control (Start over, Picture details). Focused, it turns white and its name fades in below it. */
@Composable
fun IconButton(
    icon: ImageVector,
    name: String,
    modifier: Modifier = Modifier,
    fr: FocusRequester? = null,
    onFocus: (Boolean) -> Unit = {},
    onClick: () -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    val fill = focusFill(focused, Pal.Glass, Pal.Ink)
    val tint = focusFill(focused, Pal.Ink, Pal.OnInk)
    val label = ringOf(focused)
    Box(modifier.size(BUTTON_H.px)) {
        Box(
            Modifier.size(BUTTON_H.px).clip(Corner).background(fill).control(fr, { focused = it; onFocus(it) }, onClick),
            contentAlignment = Alignment.Center,
        ) { Icon(icon, name, tint = tint, modifier = Modifier.size(32.px)) }
        // the name hangs under the button and takes no room, so nothing moves when it shows
        Text(
            name, style = ts(23, 28, 600, Pal.InkSoft), maxLines = 1, softWrap = false,
            modifier = Modifier.wrapUnbounded().padding(top = (BUTTON_H + 14).px).graphicsAlpha(label),
        )
    }
}

/** A focus-free block shaped like Resume: what mint is doing with a title that isn't ready yet. */
@Composable
fun DownloadBlock(percent: Int, eta: String?, modifier: Modifier = Modifier) {
    val shown by animateFloatAsState(percent / 100f, Move.slide(1000), label = "download")
    Box(modifier.clip(Corner).background(Pal.Glass).height(BUTTON_H.px)) {
        Row(Modifier.fillMaxHeight().padding(start = 30.px, end = 34.px), verticalAlignment = Alignment.CenterVertically) {
            Text("Downloading", style = ts(29, 36, 700))
            Box(Modifier.padding(horizontal = 18.px).size(2.px, 30.px).background(Pal.Ink.copy(alpha = 0.22f)))
            DotLine(listOfNotNull("$percent%", eta), ts(26, 32, 600, Pal.Muted, tabular = true), gap = 12)
        }
        Box(Modifier.matchParentSize(), contentAlignment = Alignment.BottomStart) {
            Box(Modifier.fillMaxWidth().height(5.px).background(Pal.Ink.copy(alpha = 0.10f))) {
                Box(Modifier.fillMaxHeight().fillMaxWidth(shown.coerceIn(0f, 1f)).background(Pal.Accent))
            }
        }
    }
}

/**
 * Words joined by a faint middle dot with [gap] board pixels each side: the design's only separator.
 * [lead] styles the first part differently (the bold "Downloading").
 */
@Composable
fun DotLine(parts: List<String>, style: TextStyle, gap: Int = 14, modifier: Modifier = Modifier, lead: TextStyle? = null) {
    Row(modifier, verticalAlignment = Alignment.CenterVertically) {
        parts.forEachIndexed { i, p ->
            if (i > 0) {
                Spacer(Modifier.width(gap.px))
                Text("·", style = style.copy(color = Pal.Faint, fontFeatureSettings = null), maxLines = 1)
                Spacer(Modifier.width(gap.px))
            }
            Text(p, style = if (i == 0 && lead != null) lead else style, maxLines = 1, softWrap = false)
        }
    }
}

/** A small capitals label over a value: DIRECTOR, RESOLUTION. */
val CapsLabel get() = ts(20, 24, 700, Pal.Label, cond = true, track = 0.12f)

/** Lays out content at its own size, however small its slot: a label hanging under a button, a row wider than the screen. */
fun Modifier.wrapUnbounded(): Modifier = layout { m, c ->
    val p = m.measure(c.copy(minWidth = 0, maxWidth = androidx.compose.ui.unit.Constraints.Infinity, minHeight = 0, maxHeight = androidx.compose.ui.unit.Constraints.Infinity))
    layout(c.minWidth.coerceAtLeast(0), c.minHeight.coerceAtLeast(0)) { p.place(0, 0) }
}

fun Modifier.graphicsAlpha(a: Float): Modifier = graphicsLayer { alpha = a }

/** Cards sit this far apart everywhere. */
val CardGap get() = 20.px

