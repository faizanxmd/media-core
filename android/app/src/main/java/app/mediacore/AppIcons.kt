package app.mediacore

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathFillType
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.addPathNodes
import androidx.compose.ui.unit.dp

/**
 * The app's icons (see android/brand/README.md). The TV uses Microsoft's Fluent System Icons, filled, always at 24 dp:
 * 48 px, 2 px to a grid unit.
 */
object AppIcons {
    val Play: ImageVector by lazy { icon("Play", 24f, "M5 5.27c0-1.7 1.83-2.79 3.33-1.97l12.36 6.72a2.25 2.25 0 0 1 0 3.96L8.33 20.7A2.25 2.25 0 0 1 5 18.73V5.27Z") }
    val Pause: ImageVector by lazy { icon("Pause", 24f, "M5.75 3C4.78 3 4 3.78 4 4.75v14.5c0 .97.78 1.75 1.75 1.75h3.5c.96 0 1.75-.78 1.75-1.75V4.75C11 3.78 10.2 3 9.25 3h-3.5Zm9 0C13.78 3 13 3.78 13 4.75v14.5c0 .97.78 1.75 1.75 1.75h3.5c.96 0 1.75-.78 1.75-1.75V4.75C20 3.78 19.2 3 18.25 3h-3.5Z") }
    val Check: ImageVector by lazy { icon("Check", 24f, "m8.5 16.59-3.8-3.8a1 1 0 0 0-1.4 1.42l4.5 4.5a1 1 0 0 0 1.4 0l11-11a1 1 0 0 0-1.4-1.42L8.5 16.6Z") }
    val Episodes: ImageVector by lazy { icon("Episodes", 24f, "M5.25 3A3.25 3.25 0 0 0 2 6.25v9c0 1.8 1.46 3.25 3.25 3.25h10.5c1.8 0 3.25-1.46 3.25-3.25v-9C19 4.45 17.54 3 15.75 3H5.25ZM8 13.25v-5.5c0-.6.66-.95 1.15-.63l4.62 2.96c.31.2.31.64 0 .84l-4.62 2.96A.75.75 0 0 1 8 13.25ZM7.75 21c-1.15 0-2.16-.6-2.74-1.5h11.24A3.75 3.75 0 0 0 20 15.75V6.01c.9.58 1.5 1.59 1.5 2.74v7c0 2.9-2.35 5.25-5.25 5.25h-8.5Z") }
    val SkipNext: ImageVector by lazy { icon("SkipNext", 24f, "M3 4.75c0-1.4 1.58-2.24 2.74-1.44l10.5 7.2c1 .68 1.01 2.17 0 2.87l-10.5 7.3A1.75 1.75 0 0 1 3 19.25V4.75Zm18-1a.75.75 0 0 0-1.5 0v16.5a.75.75 0 0 0 1.5 0V3.75Z") }

    /** A path starting with "!" fills even-odd. */
    private fun icon(name: String, box: Float, vararg paths: String): ImageVector =
        ImageVector.Builder(name, 24.dp, 24.dp, box, box).apply {
            paths.forEach { p ->
                val odd = p.startsWith("!")
                addPath(addPathNodes(p.removePrefix("!")), fill = SolidColor(Color.Black), pathFillType = if (odd) PathFillType.EvenOdd else PathFillType.NonZero)
            }
        }.build()
}
