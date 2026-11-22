@file:OptIn(ExperimentalTextApi::class)

package app.mediacore.phone

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.composed
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.ExperimentalTextApi
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// The phone's look (docs/design.md). A value that isn't here doesn't get used.
val Base = Color(0xFF0E1014) // every page
val Raised = Color(0xFF181B21) // panels, empty cards
val Overlay = Color(0xFF262A32) // controls at rest, the search box, hairlines
val Ink = Color(0xFFF4F5F8) // titles, progress, pressed controls
val Muted = Color(0xFFA4A8B0) // everything secondary
val Accent = Color(0xFF1E3AB8) // Play, and nothing else

val CardShape = RoundedCornerShape(6.dp) // every card, button and panel
val Side = 16.dp // the page's side margin
val ButtonHeight = 48.dp // the smallest thing a thumb should have to hit

private fun weight(w: Int) = FontVariation.Settings(FontVariation.weight(w))

val Inter = FontFamily(
    Font(R.font.inter, FontWeight.Normal, variationSettings = weight(450)),
    Font(R.font.inter, FontWeight.Medium, variationSettings = weight(500)),
    Font(R.font.inter, FontWeight.SemiBold, variationSettings = weight(600)),
    Font(R.font.inter, FontWeight.Bold, variationSettings = weight(700)),
)

object Type {
    val Display = TextStyle(fontFamily = Inter, color = Ink, fontSize = 28.sp, lineHeight = 32.sp, fontWeight = FontWeight.Bold)
    val Title = TextStyle(fontFamily = Inter, color = Ink, fontSize = 20.sp, lineHeight = 24.sp, fontWeight = FontWeight.SemiBold)
    val Heading = TextStyle(fontFamily = Inter, color = Ink, fontSize = 16.sp, lineHeight = 20.sp, fontWeight = FontWeight.SemiBold)
    val Body = TextStyle(fontFamily = Inter, color = Ink, fontSize = 14.sp, lineHeight = 20.sp, fontWeight = FontWeight.Normal)
    val Label = TextStyle(fontFamily = Inter, color = Ink, fontSize = 14.sp, lineHeight = 20.sp, fontWeight = FontWeight.SemiBold)
    val Meta = TextStyle(fontFamily = Inter, color = Ink, fontSize = 12.sp, lineHeight = 16.sp, fontWeight = FontWeight.Medium)
    val Caption = TextStyle(fontFamily = Inter, color = Ink, fontSize = 11.sp, lineHeight = 14.sp, fontWeight = FontWeight.SemiBold)
}

/** Digits of one width, for values that change or stack. */
fun TextStyle.figures() = copy(fontFeatureSettings = "tnum")

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
    const val FadeInMs = 160 // pages, controls and panels appearing
    const val FadeOutMs = 200 // and leaving
}

/** A page arriving: it fades up from the base colour, in one piece. */
@Composable
fun FadeIn(content: @Composable BoxScope.() -> Unit) {
    val a = remember { Animatable(0f) }
    LaunchedEffect(Unit) { a.animateTo(1f, tween(Motion.FadeInMs)) }
    Box(Modifier.fillMaxSize().graphicsLayer { alpha = a.value }, content = content)
}

/** A tap target that dims while it's held: no ripple on a flat surface. */
fun Modifier.tap(enabled: Boolean = true, onClick: () -> Unit): Modifier = composed {
    val source = remember { MutableInteractionSource() }
    val pressed by source.collectIsPressedAsState()
    graphicsLayer { alpha = if (pressed) 0.6f else 1f }.clickable(source, null, enabled, onClick = onClick)
}

/** Play, or the time left when resuming. The one blue thing on a page. */
@Composable
fun PlayButton(label: String, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Row(
        modifier.height(ButtonHeight).clip(CardShape).background(Accent).tap(onClick = onClick),
        horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(AppIcons.Play, null, tint = Ink, modifier = Modifier.size(24.dp))
        Text(label, style = Type.Label.figures(), maxLines = 1)
    }
}

/** A square icon button; [on] is a state that stays, like watched. */
@Composable
fun ToolButton(icon: ImageVector, label: String, on: Boolean = false, tint: Color = Ink, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Box(
        modifier.size(ButtonHeight).clip(CardShape).background(if (on) Ink else Overlay).tap(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) { Icon(icon, label, tint = if (on) Base else tint, modifier = Modifier.size(24.dp)) }
}

/** A worded button, for a card's answer. */
@Composable
fun WordButton(text: String, onClick: () -> Unit) {
    Box(
        Modifier.height(ButtonHeight).clip(CardShape).background(Overlay).tap(onClick = onClick).padding(horizontal = 16.dp),
        contentAlignment = Alignment.Center,
    ) { Text(text, style = Type.Label, maxLines = 1) }
}

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
