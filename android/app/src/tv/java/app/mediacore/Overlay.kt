package app.mediacore

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.gestures.LocalBringIntoViewSpec
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider

/** The TV app's player controls (D-pad driven). */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun DeviceOverlay(st: PlayerState, a: PlayerActions, tv: Boolean, onChrome: (Float) -> Unit) {
    CompositionLocalProvider(LocalBringIntoViewSpec provides rememberMinimalSpec()) { PlayerOverlay(st, a, tv, onChrome) }
}
