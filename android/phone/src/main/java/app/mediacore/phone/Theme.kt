@file:OptIn(ExperimentalTextApi::class)

package app.mediacore.phone

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.ExperimentalTextApi
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

val Base = Color(0xFF0E1014)
val Raised = Color(0xFF181B21)
val Overlay = Color(0xFF262A32)
val Ink = Color(0xFFF4F5F8)
val Muted = Color(0xFFA4A8B0)

val CardShape = RoundedCornerShape(6.dp)

private fun weight(w: Int) = FontVariation.Settings(FontVariation.weight(w))

val Inter = FontFamily(
    Font(R.font.inter, FontWeight.Normal, variationSettings = weight(450)),
    Font(R.font.inter, FontWeight.Medium, variationSettings = weight(500)),
    Font(R.font.inter, FontWeight.SemiBold, variationSettings = weight(600)),
    Font(R.font.inter, FontWeight.Bold, variationSettings = weight(700)),
)

object Type {
    val Title = TextStyle(fontFamily = Inter, color = Ink, fontSize = 24.sp, lineHeight = 28.sp, fontWeight = FontWeight.SemiBold)
    val Body = TextStyle(fontFamily = Inter, color = Ink, fontSize = 14.5.sp, lineHeight = 20.sp, fontWeight = FontWeight.Normal)
    val Label = TextStyle(fontFamily = Inter, color = Ink, fontSize = 14.5.sp, lineHeight = 20.sp, fontWeight = FontWeight.SemiBold)
    val Caption = TextStyle(fontFamily = Inter, color = Ink, fontSize = 11.5.sp, lineHeight = 14.sp, fontWeight = FontWeight.SemiBold)
}

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
    const val FadeInMs = 160 // controls and panels appearing
    const val FadeOutMs = 200 // and leaving
    const val BackdropMs = 320 // one picture dissolving into the next
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
