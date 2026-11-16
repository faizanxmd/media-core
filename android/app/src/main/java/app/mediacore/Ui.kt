@file:OptIn(ExperimentalTextApi::class)

package app.mediacore

import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.ExperimentalTextApi
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight

// The player's look (docs/design-system.md), kept exactly as it was: the redesigned screens use Design.kt instead.
val Base = Color(0xFF0E1014) // every page
val Ink = Color(0xFFF4F5F8) // titles, focused text, progress, focus

private fun weight(w: Int) = FontVariation.Settings(FontVariation.weight(w))

val Inter = FontFamily(
    // a little over Regular: thin strokes are the first thing the TV's stretch to 4K softens
    Font(R.font.inter, FontWeight.Normal, variationSettings = weight(450)),
    Font(R.font.inter, FontWeight.Medium, variationSettings = weight(500)),
    Font(R.font.inter, FontWeight.SemiBold, variationSettings = weight(600)),
    Font(R.font.inter, FontWeight.Bold, variationSettings = weight(700)),
    Font(R.font.inter, FontWeight.ExtraBold, variationSettings = weight(800)),
)

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
