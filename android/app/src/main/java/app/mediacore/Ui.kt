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

// The player's look (docs/design-system.md), kept exactly as it was: the redesigned screens use Design.kt instead.
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

/** Stops D-pad Right at the last card of a row. */
fun Modifier.rowEnds(index: Int, count: Int): Modifier = onPreviewKeyEvent { e ->
    e.type == KeyEventType.KeyDown && e.key == Key.DirectionRight && index == count - 1
}

/** The player's side margin. */
val Side = 40.dp

/** One row of a compact menu: a check for the current choice, the name, a detail on the right. */
@Composable
fun MenuOption(title: String, detail: String, selected: Boolean, fr: FocusRequester?, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    Row(
        Modifier.fillMaxWidth().height(36.dp)
            .then(if (fr != null) Modifier.focusRequester(fr) else Modifier)
            .onFocusChanged { focused = it.isFocused }
            .clip(CardShape)
            .background(if (focused) Ink else Color.Transparent)
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .padding(horizontal = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.width(28.dp)) {
            if (selected) Icon(AppIcons.Check, null, tint = if (focused) Base else Ink, modifier = Modifier.size(24.dp))
        }
        Text(title, style = Type.Body, color = if (focused) Base else Ink, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        if (detail.isNotEmpty()) Text(detail, style = Type.Meta, color = if (focused) Base else Muted, maxLines = 1, modifier = Modifier.padding(start = 8.dp))
    }
}
