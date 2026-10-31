@file:OptIn(ExperimentalTextApi::class)

package app.mediacore

import android.graphics.Bitmap
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.ExperimentalTextApi
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.zIndex
import app.mediacore.core.Api
import app.mediacore.core.Info
import app.mediacore.core.Item
import app.mediacore.core.leftText
import coil3.compose.AsyncImage
import coil3.request.ImageRequest
import coil3.request.allowHardware
import coil3.request.crossfade
import coil3.toBitmap
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

// Media Core's look (docs/design-system.md). A value that isn't here doesn't get used.
val Base = Color(0xFF0E1014) // every page
val Raised = Color(0xFF181B21) // menus, panels, empty cards
val Overlay = Color(0xFF262A32) // controls at rest, the search box
val Ink = Color(0xFFF4F5F8) // titles, focused text, progress, focus
val Muted = Color(0xFFA4A8B0) // everything secondary
val Accent = Color(0xFF0F1E66) // large areas: Resume or Play while selected
val AccentWord = Color(0xFF1E3AB8) // small things look darker than large ones: the current tab, a selected player icon

val CardRadius = 6.dp
val CardShape = RoundedCornerShape(CardRadius) // every card, button and menu
val ChipShape = RoundedCornerShape(4.dp) // small marks
val FocusLine = 2.dp // the focus border on a card, the line under a focused word

private fun weight(w: Int) = FontVariation.Settings(FontVariation.weight(w))

val Inter = FontFamily(
    // a little over Regular: thin strokes are the first thing the TV's stretch to 4K softens
    Font(R.font.inter, FontWeight.Normal, variationSettings = weight(450)),
    Font(R.font.inter, FontWeight.Medium, variationSettings = weight(500)),
    Font(R.font.inter, FontWeight.SemiBold, variationSettings = weight(600)),
    Font(R.font.inter, FontWeight.Bold, variationSettings = weight(700)),
    Font(R.font.inter, FontWeight.ExtraBold, variationSettings = weight(800)),
)

/** The app's text styles: six sizes, and Label is Body's size with weight for words you act on. */
object Type {
    val Display = TextStyle(fontFamily = Inter, color = Ink, fontSize = 32.sp, lineHeight = 36.sp, fontWeight = FontWeight.Bold)
    val Title = TextStyle(fontFamily = Inter, color = Ink, fontSize = 24.sp, lineHeight = 28.sp, fontWeight = FontWeight.SemiBold)
    val Heading = TextStyle(fontFamily = Inter, color = Ink, fontSize = 18.sp, lineHeight = 22.sp, fontWeight = FontWeight.SemiBold)
    val Body = TextStyle(fontFamily = Inter, color = Ink, fontSize = 14.5.sp, lineHeight = 20.sp, fontWeight = FontWeight.Normal)
    val Label = TextStyle(fontFamily = Inter, color = Ink, fontSize = 14.5.sp, lineHeight = 20.sp, fontWeight = FontWeight.SemiBold)
    val Meta = TextStyle(fontFamily = Inter, color = Ink, fontSize = 13.sp, lineHeight = 16.sp, fontWeight = FontWeight.Medium)
    val Caption = TextStyle(fontFamily = Inter, color = Ink, fontSize = 11.5.sp, lineHeight = 14.sp, fontWeight = FontWeight.SemiBold)
}

/** Digits of one width, for values that change or stack. */
fun TextStyle.figures() = copy(fontFeatureSettings = "tnum")

/** true on Android TV (D-pad, 10-foot UI), false on the phone. */
val LocalTv = compositionLocalOf { false }

@Composable
fun AppTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = darkColorScheme(
            background = Base, surface = Base, primary = Color.White, onPrimary = Base,
            onBackground = Ink, onSurface = Ink,
        ),
    ) {
        CompositionLocalProvider(LocalTextStyle provides TextStyle(fontFamily = Inter, color = Ink), content = content)
    }
}

/** Every animation's timing, so the whole app moves alike. */
object Motion {
    const val FocusMs = 180 // a card or button answering the remote
    const val FadeInMs = 160 // controls and panels appearing
    const val FadeOutMs = 200 // and leaving
    const val BackdropMs = 320 // one picture dissolving into the next
    const val SettleMs = 300 // a scroll has landed: start heavier work only after this
}

private val Ease = CubicBezierEasing(0.2f, 0.8f, 0.2f, 1f)

/** A page arriving: it fades up from the base colour once [ready]. One layer, so it stays smooth on the TV. */
@Composable
fun FadeIn(ready: Boolean = true, content: @Composable BoxScope.() -> Unit) {
    val a = remember { Animatable(0f) }
    LaunchedEffect(ready) { if (ready) a.animateTo(1f, tween(Motion.FadeInMs, easing = Ease)) }
    Box(Modifier.fillMaxSize().graphicsLayer { alpha = a.value }, content = content)
}

/** A focused word, here or in the tabs: a crisp line under it, painted so it takes no room. */
fun Modifier.focusLine(on: Boolean): Modifier = drawBehind {
    if (on) drawRect(Ink, Offset(0f, size.height - FocusLine.toPx()), Size(size.width, FocusLine.toPx()))
}

/** A control's fill: the shared timing, so everything answers the remote alike. */
@Composable
internal fun fillFor(color: Color) = animateColorAsState(color, tween(Motion.FocusMs, easing = Ease), label = "fill").value

/**
 * A focusable picture card. At rest it sits slightly dimmed; focused it comes up to full brightness and
 * grows downward, so the gap under a row's title never changes, with a border sitting right on its edge.
 */
@Composable
fun FocusCard(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    scale: Float = 1.04f,
    focusRequester: FocusRequester? = null,
    onFocus: () -> Unit = {},
    content: @Composable BoxScope.(focused: Boolean) -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    val t by animateFloatAsState(if (focused) 1f else 0f, tween(Motion.FocusMs, easing = Ease), label = "focus")
    Box(
        modifier
            .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            // focus sits outside the lift: scrolling to a card must see its resting size,
            // or the list chases the card as it grows and every row bobs on each press
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .zIndex(if (focused) 1f else 0f)
            .graphicsLayer {
                val s = 1f + (scale - 1f) * t
                scaleX = s; scaleY = s
                transformOrigin = TransformOrigin(0.5f, 0f)
            },
    ) {
        Box(
            Modifier.matchParentSize().clip(CardShape).background(Raised)
                .drawWithContent {
                    drawContent()
                    if (t < 1f) drawRect(Color.Black, alpha = 0.18f * (1f - t))
                    if (t > 0f) {
                        val w = FocusLine.toPx()
                        drawRoundRect(
                            Ink, Offset(w / 2, w / 2), Size(size.width - w, size.height - w),
                            CornerRadius(CardRadius.toPx() - w / 2), alpha = t, style = Stroke(w),
                        )
                    }
                },
        ) { content(focused) }
    }
}

/** A thin turning arc: the app's only loading mark. */
@Composable
fun LoadingRing(modifier: Modifier = Modifier, size: Dp = 26.dp) {
    val turn = rememberInfiniteTransition(label = "loading")
    val angle by turn.animateFloat(0f, 360f, infiniteRepeatable(tween(1000, easing = LinearEasing)), label = "angle")
    Canvas(modifier.size(size)) {
        val w = 2.dp.toPx()
        drawCircle(Ink.copy(alpha = 0.16f), radius = (this.size.minDimension - w) / 2, style = Stroke(w))
        drawArc(
            Ink, angle, 90f, false, topLeft = Offset(w / 2, w / 2),
            size = this.size.copy(this.size.width - w, this.size.height - w), style = Stroke(w, cap = StrokeCap.Round),
        )
    }
}

/** Progress: a plain white bar with square ends. On a card it sits flush along the picture's bottom edge. */
@Composable
fun ProgressTrack(fraction: Float, modifier: Modifier = Modifier, height: Dp = 3.dp) {
    Box(modifier.height(height).background(Ink.copy(alpha = 0.28f))) {
        Box(Modifier.fillMaxWidth(fraction).fillMaxHeight().background(Ink))
    }
}

/** A poster and nothing else: the art carries the title. */
@Composable
fun PosterCard(
    item: Item,
    width: Dp,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    onFocus: () -> Unit = {},
    focusRequester: FocusRequester? = null,
) {
    FocusCard(onClick, modifier.width(width).aspectRatio(2f / 3f), focusRequester = focusRequester, onFocus = onFocus) {
        if (item.poster != null) AsyncImage(item.poster, item.displayTitle, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        else Box(Modifier.fillMaxSize().padding(12.dp), contentAlignment = Alignment.BottomStart) {
            Text(item.displayTitle, style = Type.Label, maxLines = 4, overflow = TextOverflow.Ellipsis)
        }
    }
}

/** Continue Watching: 16:9 art with the progress on its edge; the title and time left sit under it. */
@Composable
fun WideCard(
    item: Item,
    width: Dp,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    onFocus: () -> Unit = {},
    focusRequester: FocusRequester? = null,
) {
    Column(Modifier.width(width)) {
        FocusCard(onClick, modifier.fillMaxWidth().aspectRatio(16f / 9f), focusRequester = focusRequester, onFocus = onFocus) {
            val pic = Api.img(item.meta?.backdrop?.ifEmpty { null }, "w500") ?: item.wideSmall
            if (pic != null) AsyncImage(pic, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
            if (item.progress > 0f) ProgressTrack(item.progress, Modifier.align(Alignment.BottomCenter).fillMaxWidth())
        }
        Row(Modifier.padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(item.showTitle, style = Type.Meta, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
            if (item.pos > 0 && item.durMs > item.pos) Text(" · " + leftText(item.durMs - item.pos), style = Type.Meta, color = Muted, maxLines = 1)
        }
    }
}

/** The main action: the time left ("52m") or "Play". The one blue thing on a page, and only while selected. */
@Composable
fun PlayCapsule(
    label: String,
    modifier: Modifier = Modifier,
    focusRequester: FocusRequester? = null,
    onFocus: () -> Unit = {},
    onClick: () -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    Row(
        modifier
            .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .height(40.dp)
            .background(fillFor(if (focused) Accent else Overlay), CardShape)
            .padding(start = 12.dp, end = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(AppIcons.Play, null, tint = Color.White, modifier = Modifier.size(24.dp))
        Text(label, style = Type.Label, color = Color.White, maxLines = 1)
    }
}

/** An icon button next to the main action (start over, watched, details). Selected, it turns white. */
@Composable
fun ToolButton(
    icon: ImageVector,
    label: String,
    modifier: Modifier = Modifier,
    focusRequester: FocusRequester? = null,
    onFocus: () -> Unit = {},
    tint: Color = Ink,
    onClick: () -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    Box(
        modifier
            .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .size(40.dp)
            .background(fillFor(if (focused) Ink else Overlay), CardShape)
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) { Icon(icon, label, tint = if (focused) Base else tint, modifier = Modifier.size(24.dp)) }
}

/** A worded button (Try again, Close, Skip Intro): grey at rest, white while selected. */
@Composable
fun WordButton(
    text: String,
    lit: Boolean = false,
    modifier: Modifier = Modifier,
    focusRequester: FocusRequester? = null,
    icon: ImageVector? = null,
    onClick: () -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    val on = focused || lit
    Row(
        modifier
            .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
            .onFocusChanged { focused = it.isFocused }
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .height(40.dp)
            .background(fillFor(if (on) Ink else Overlay), CardShape)
            .padding(start = 16.dp, end = if (icon != null) 12.dp else 16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(text, style = Type.Label, color = if (on) Base else Ink, maxLines = 1)
        if (icon != null) Icon(icon, null, tint = if (on) Base else Ink, modifier = Modifier.size(24.dp))
    }
}

/** 4K as a small solid chip, then the real Dolby / DTS / HDR10 marks in the muted tone. */
@Composable
fun FormatMarks(info: Info?, height: Dp = 14.dp) {
    if (info == null) return
    val hdr = info.hdr
    val a = info.audio
    if (info.res == "4K") {
        Text("4K", style = Type.Caption, color = Base, modifier = Modifier.clip(ChipShape).background(Ink).padding(horizontal = 4.dp))
    }
    when {
        hdr.startsWith("Dolby Vision") -> Logo(R.drawable.logo_dv, height)
        hdr == "HDR10+" -> Logo(R.drawable.logo_hdr10p, height + 2.dp)
        hdr == "HDR10" -> Logo(R.drawable.logo_hdr10, height + 2.dp)
        hdr.isNotEmpty() -> Text(hdr, style = Type.Meta, color = Muted)
    }
    when {
        a.contains("Atmos") -> Logo(R.drawable.logo_atmos, height)
        a.startsWith("DTS:X") -> Logo(R.drawable.logo_dtsx, height + 2.dp)
        a.startsWith("DTS-HD MA") -> Logo(R.drawable.logo_dtshdma, height + 4.dp)
        a.startsWith("TrueHD") -> Logo(R.drawable.logo_truehd, height + 4.dp)
    }
}

@Composable
private fun Logo(res: Int, height: Dp) {
    Image(painterResource(res), null, colorFilter = ColorFilter.tint(Muted), contentScale = ContentScale.FillHeight, modifier = Modifier.height(height))
}

/** Height of [RowTitle]: its line plus the gap under it. The same over every row on every screen. */
val RowTitleHeight = 38.dp

/** Row heading. */
@Composable
fun RowTitle(text: String, modifier: Modifier = Modifier, color: Color = Ink) {
    Text(text, style = Type.Heading, color = color, modifier = modifier.padding(bottom = 16.dp))
}

/** Stops D-pad Right at the last card of a row. */
fun Modifier.rowEnds(index: Int, count: Int): Modifier = onPreviewKeyEvent { e ->
    e.type == KeyEventType.KeyDown && e.key == Key.DirectionRight && index == count - 1
}

/** Where the words start on title pages, the gap from them to the next row, and the side margin. */
val PageTop = 56.dp
val SectionGap = 24.dp
val Side = 40.dp
private val FrameWidth = 432.dp
private val FrameHeight = 243.dp
private val StageTop = 8.dp // under the tabs: Home's words start 68 from the top of the screen

/**
 * The top of Home: words on the left, one framed picture on the right.
 * One lit screen in a dark room: crisp edges, no fades, and nothing redrawn per title.
 */
@Composable
fun Stage(picture: String?, modifier: Modifier = Modifier, words: @Composable ColumnScope.() -> Unit) {
    Box(modifier.fillMaxWidth().height(StageTop + FrameHeight + SectionGap)) {
        Column(Modifier.padding(start = Side, top = StageTop).width(424.dp), content = words)
        Box(Modifier.align(Alignment.TopEnd).padding(end = Side, top = StageTop).size(FrameWidth, FrameHeight).clip(CardShape).background(Raised)) {
            if (picture != null) AsyncImage(picture, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        }
    }
}

/**
 * The back of a title page: its artwork across the whole screen, fading into the page where the words and
 * cards sit. One still picture per page, kept as one texture (Offscreen), so moving around never redraws it.
 */
@Composable
fun BoxScope.Banner(url: String?, onDone: () -> Unit) {
    LaunchedEffect(url) { if (url == null) onDone() }
    Box(Modifier.matchParentSize().graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }.background(Base)) {
        if (url != null) AsyncImage(url, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop, onSuccess = { onDone() }, onError = { onDone() })
        Box(Modifier.matchParentSize().background(Brush.horizontalGradient(0f to Base.copy(alpha = 0.98f), 0.36f to Base.copy(alpha = 0.95f), 0.46f to Base.copy(alpha = 0.85f), 0.58f to Base.copy(alpha = 0.5f), 0.7f to Base.copy(alpha = 0.15f), 0.8f to Color.Transparent)))
        Box(Modifier.matchParentSize().background(Brush.verticalGradient(0.36f to Color.Transparent, 0.52f to Base.copy(alpha = 0.5f), 0.66f to Base.copy(alpha = 0.92f), 0.8f to Base)))
    }
}

/** A still blurred once, off the UI thread, and then shown as a plain picture: a live blur would stutter on this TV. */
class BlurOnce : coil3.transform.Transformation() {
    override val cacheKey = "blur-once"

    override suspend fun transform(input: Bitmap, size: coil3.size.Size): Bitmap {
        val w = 160
        val h = 90
        val small = Bitmap.createScaledBitmap(input, w, h, true)
        val px = IntArray(w * h)
        small.getPixels(px, 0, w, 0, 0, w, h)
        repeat(3) { boxBlur(px, w, h, 6) }
        return Bitmap.createBitmap(px, w, h, Bitmap.Config.ARGB_8888)
    }

    private fun boxBlur(px: IntArray, w: Int, h: Int, r: Int) {
        val line = IntArray(maxOf(w, h))
        fun run(n: Int, read: (Int) -> Int, write: (Int, Int) -> Unit) {
            var a = 0; var b = 0; var c = 0
            for (k in -r..r) { val p = read(k.coerceIn(0, n - 1)); a += p shr 16 and 255; b += p shr 8 and 255; c += p and 255 }
            val d = 2 * r + 1
            for (k in 0 until n) {
                line[k] = (255 shl 24) or ((a / d) shl 16) or ((b / d) shl 8) or (c / d)
                val add = read((k + r + 1).coerceAtMost(n - 1)); val del = read((k - r).coerceAtLeast(0))
                a += (add shr 16 and 255) - (del shr 16 and 255)
                b += (add shr 8 and 255) - (del shr 8 and 255)
                c += (add and 255) - (del and 255)
            }
            for (k in 0 until n) write(k, line[k])
        }
        for (y in 0 until h) run(w, { px[y * w + it] }, { x, v -> px[y * w + x] = v })
        for (x in 0 until w) run(h, { px[it * w + x] }, { y, v -> px[y * w + x] = v })
    }
}

/** The name at the top of the words: title art when there is some, else the name set large. */
@Composable
fun TitleMark(logo: String?, name: String, modifier: Modifier = Modifier) {
    Box(modifier.height(72.dp).width(300.dp), contentAlignment = Alignment.BottomStart) {
        if (logo != null) AsyncImage(logo, name, Modifier.fillMaxSize(), contentScale = ContentScale.Fit, alignment = Alignment.BottomStart)
        else Text(name, style = Type.Display, maxLines = 2, overflow = TextOverflow.Ellipsis)
    }
}
