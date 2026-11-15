@file:OptIn(ExperimentalTextApi::class)

package app.mediacore

import androidx.compose.animation.togetherWith
import androidx.compose.animation.fadeOut
import androidx.compose.animation.fadeIn
import androidx.compose.animation.ContentTransform
import androidx.compose.ui.graphics.drawscope.withTransform
import androidx.compose.ui.graphics.vector.PathParser
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

/** The accent choices. Each has a deep shade for a focused Play, so it never glows, and a light one for words. */
enum class AccentColor(val main: Color, val deep: Color, val text: Color, val light: Boolean = false) {
    Blue(Color(0xFF1D39F3), Color(0xFF1F2F8C), Color(0xFF7C95FF)),
    White(Color(0xFFF2F3F7), Color(0xFFF2F3F7), Color(0xFFF2F3F7), light = true),
    Ember(Color(0xFFD9622B), Color(0xFF8C3415), Color(0xFFF2945F)),
    Red(Color(0xFFE50914), Color(0xFFA8070F), Color(0xFFFF6B70)),
}

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
    /** The accent, chosen in Settings: Play while focused, progress bars, Settings' headings. */
    var accent by mutableStateOf(AccentColor.Blue)
    val Accent: Color get() = accent.main // big areas: progress, Settings headings
    val Cobalt = Color(0xFF4F84FE) // small marks
    val Problem = Color(0xFFF08A84)
    val ProblemBg = Color(0xFF2A1416)
    val Glass = Color(0xD124272E) // rgba(36,39,46,.82): every resting control over art
    val AccentText: Color get() = accent.text // the accent as words on the dark ground: light enough to read
    val Deep: Color get() = accent.deep // Play and Resume while focused: deep enough not to glow
    val OnDeep: Color get() = if (accent.light) OnInk else Ink // their words
    val OnInk = Color(0xFF0B0D11) // text on a focused (white) control
    val OnInkSoft = Color(0xFF4A4F5A) // its quieter parts
}

val Corner get() = RoundedCornerShape(8.px)

/**
 * Source Sans 3 (Adobe, SIL Open Font License; android/app/brand-fonts-OFL.txt), one family at one width for
 * everything. It runs small and light for its size next to the Archivo the boards were drawn in, so every size is
 * drawn 7% larger (line heights, which place things, stay), nothing is lighter than 500, and tight tracking is eased.
 */
val SourceSans = FontFamily(
    listOf(500, 600, 650, 700, 800).map { w -> Font(R.font.source_sans, FontWeight(w), variationSettings = FontVariation.Settings(FontVariation.weight(w))) },
)

/** A text style in board pixels: size, line height, weight, colour, [track] in em. [cond] is kept for the boards' sake. */
fun ts(size: Number, line: Number, weight: Int, color: Color = Pal.Ink, @Suppress("UNUSED_PARAMETER") cond: Boolean = false, track: Float = 0f, tabular: Boolean = false) = TextStyle(
    fontFamily = SourceSans,
    fontSize = (size.toFloat() * 1.07f).fs, lineHeight = line.fs, fontWeight = FontWeight(maxOf(weight, 500)), color = color,
    letterSpacing = maxOf(track, -0.01f).em,
    fontFeatureSettings = if (tabular) "tnum" else null,
    lineHeightStyle = LineHeightStyle(LineHeightStyle.Alignment.Center, LineHeightStyle.Trim.None),
)

/**
 * How far along, inside a picture's foot (a card, an episode's still), as Netflix and Apple TV do: it lifts with the
 * picture. A 4 px bar 16 px in from the sides and 14 up, the accent's light shade on white at 30%, over a dark
 * gradient so it holds on bright art. Call it last inside the picture's Box.
 */
@Composable
fun androidx.compose.foundation.layout.BoxScope.CardProgress(p: Float) {
    Box(
        Modifier.align(Alignment.BottomStart).fillMaxWidth().height(72.px)
            .background(Brush.verticalGradient(0f to Color.Transparent, 1f to Color.Black.copy(alpha = 0.6f))),
    )
    Box(
        Modifier.align(Alignment.BottomStart).padding(start = 16.px, end = 16.px, bottom = 14.px).fillMaxWidth().height(4.px)
            .clip(RoundedCornerShape(2.px)).background(Color.White.copy(alpha = 0.3f)),
    ) { Box(Modifier.fillMaxHeight().fillMaxWidth(p.coerceIn(0.03f, 1f)).background(Pal.AccentText)) }
}

/** Under words that sit on art (a title's facts and story): soft and dark, so any picture behind stays readable. */
val WordShadow get() = androidx.compose.ui.graphics.Shadow(Pal.BaseDeep.copy(alpha = 0.75f), Offset(0f, 2 * Px.screen), 14 * Px.screen)

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

    const val Through = 300

    /**
     * One thing giving way to another (a screen, the hero's words, a season's episodes): the old fades out quickly,
     * then the new fades in, so the two are never seen on top of each other half-faded.
     */
    fun through(ms: Int = Through): ContentTransform {
        val out = if (reduced) 100 else ms * 35 / 100
        val enter = if (reduced) 100 else ms - out
        return fadeIn(tween(enter, delayMillis = if (reduced) 0 else out, easing = Ease)) togetherWith fadeOut(tween(out, easing = Ease))
    }
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
 * The Media Core wordmark: a small MEDIA over a wide CORE whose O is the ring with the lit blue core, drawn from
 * android/brand/wordmark.svg's outlines so it stays sharp at any size. [height] is the whole mark's height; it is
 * about 2.6 times as wide. No glow at this size: the core is the only colour.
 */
@Composable
fun Wordmark(modifier: Modifier = Modifier, height: Dp = 44.px) {
    val letters = remember { PathParser().parsePathString(WORDMARK_LETTERS).toPath() }
    Canvas(modifier.size(height * (WM_W / WM_H), height)) {
        val s = size.height / WM_H
        withTransform({ scale(s, s, Offset.Zero); translate(-WM_X, -WM_Y) }) {
            val ink = Color(0xFFF4F5F8)
            drawPath(letters, ink)
            val c = Offset(134.678f, 106f)
            val ring = Path().apply {
                fillType = PathFillType.EvenOdd
                addOval(androidx.compose.ui.geometry.Rect(c, 23.678f))
                addOval(androidx.compose.ui.geometry.Rect(c.x - 14.601f, c.y - 15.875f, c.x + 14.601f, c.y + 15.875f))
            }
            drawPath(ring, ink)
            drawCircle(Brush.radialGradient(0f to Color(0xFF7FA2FF), 0.55f to Color(0xFF3A5CF8), 1f to Color(0xFF1D39F3), center = c, radius = 9f), 9f, c) // the brand's blue, whatever the accent
        }
    }
}

// The wordmark's outline box in the SVG's units, and its letters (the O is drawn as the ring above).
private const val WM_X = 55.987f
private const val WM_Y = 50f
private const val WM_W = 263.377f - WM_X
private const val WM_H = 129.678f - WM_Y
private const val WORDMARK_LETTERS = "M80.631,129.678Q75.329,129.678 70.822,127.937Q66.315,126.195 63.001,123.002Q59.688,119.808 57.838,115.486Q55.987,111.163 55.987,106Q55.987,100.837 57.838,96.514Q59.688,92.192 63.028,88.998Q66.368,85.805 70.875,84.063Q75.383,82.322 80.684,82.322Q86.38,82.322 91.092,84.288Q95.805,86.253 99.006,90.081L93.222,95.567Q90.76,92.844 87.712,91.484Q84.664,90.125 81.099,90.125Q77.604,90.125 74.675,91.287Q71.746,92.449 69.588,94.574Q67.43,96.699 66.243,99.608Q65.055,102.518 65.055,106Q65.055,109.482 66.243,112.392Q67.43,115.301 69.588,117.426Q71.746,119.551 74.675,120.713Q77.604,121.875 81.099,121.875Q84.664,121.875 87.712,120.516Q90.76,119.156 93.222,116.38L99.006,121.865Q95.805,125.681 91.092,127.68Q86.38,129.678 80.631,129.678Z M172.35,129L172.35,83L191.51,83Q200.898,83 206.2,87.372Q211.503,91.745 211.503,99.442Q211.503,104.498 209.105,108.155Q206.706,111.813 202.259,113.77Q197.813,115.727 191.635,115.727L177.378,115.727L181.434,111.7L181.434,129ZM202.514,129L190.782,112.296L200.525,112.296L212.337,129ZM181.434,112.662L177.378,108.352L191.232,108.352Q196.745,108.352 199.543,105.999Q202.34,103.646 202.34,99.442Q202.34,95.184 199.543,92.858Q196.745,90.531 191.232,90.531L177.378,90.531L181.434,86.168Z M228.35,129L228.35,83L262.462,83L262.462,90.482L237.378,90.482L237.378,121.518L263.377,121.518L263.377,129ZM236.694,109.283L236.694,102.007L259.636,102.007L259.636,109.283Z M114.996,65L114.996,50L117.532,50L124.209,60.838L122.866,60.838L129.436,50L131.972,50L131.999,65L129.088,65L129.061,54.414L129.653,54.414L124.178,63.33L122.791,63.33L117.226,54.414L117.908,54.414L117.908,65Z M138.997,65L138.997,50L149.712,50L149.712,52.482L141.885,52.482L141.885,62.518L149.998,62.518L149.998,65ZM141.667,58.59L141.667,56.178L148.828,56.178L148.828,58.59Z M156.996,65L156.996,50L163.698,50Q166.162,50 168.029,50.936Q169.895,51.872 170.944,53.552Q171.992,55.232 171.992,57.5Q171.992,59.764 170.944,61.446Q169.895,63.128 168.029,64.064Q166.162,65 163.698,65ZM160.027,62.49L163.549,62.49Q165.193,62.49 166.397,61.872Q167.6,61.254 168.268,60.131Q168.935,59.007 168.935,57.5Q168.935,55.972 168.268,54.859Q167.6,53.746 166.397,53.128Q165.193,52.51 163.549,52.51L160.027,52.51Z M178.996,65L178.996,50L181.997,50L181.997,65Z M188.991,65L195.547,50L198.438,50L205.01,65L201.951,65L196.393,51.574L197.558,51.574L191.999,65ZM192.103,61.61L192.879,59.282L200.657,59.282L201.45,61.61Z"

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
 * Play or Resume: grey like every control at rest, the accent's deep shade when focused. "Play | S2 E1", "Resume | 2 h 4 m left".
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
    val ink = focusFill(focused, Pal.Ink, Pal.OnDeep)
    val soft = focusFill(focused, Pal.InkSoft, if (Pal.accent.light) Pal.OnInkSoft else Pal.InkSoft)
    Row(
        modifier.softShadow(ShadowKind.Button).clip(Corner).background(fill)
            .control(fr, { focused = it; onFocus(it) }, onClick).height(BUTTON_H.px).padding(start = 30.px, end = 34.px),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Play, null, tint = ink, modifier = Modifier.size(22.px, 26.px))
        Spacer(Modifier.width(16.px))
        Text(word, style = ts(29, 36, 700, ink), maxLines = 1)
        if (detail != null) {
            Box(Modifier.padding(horizontal = 18.px).size(2.px, 30.px).background(ink.copy(alpha = 0.25f)))
            Text(detail, style = ts(26, 32, 600, soft, tabular = true), maxLines = 1)
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
val CapsLabel get() = ts(22, 26, 600, Pal.Label, track = 0.08f)

/** Lays out content at its own size, however small its slot: a label hanging under a button, a row wider than the screen. */
fun Modifier.wrapUnbounded(): Modifier = layout { m, c ->
    val p = m.measure(c.copy(minWidth = 0, maxWidth = androidx.compose.ui.unit.Constraints.Infinity, minHeight = 0, maxHeight = androidx.compose.ui.unit.Constraints.Infinity))
    layout(c.minWidth.coerceAtLeast(0), c.minHeight.coerceAtLeast(0)) { p.place(0, 0) }
}

fun Modifier.graphicsAlpha(a: Float): Modifier = graphicsLayer { alpha = a }

/** Cards sit this far apart everywhere. */
val CardGap get() = 20.px

