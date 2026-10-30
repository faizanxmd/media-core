package app.mediadl

import androidx.compose.runtime.Composable

/** The TV app's player controls (D-pad driven). */
@Composable
fun DeviceOverlay(st: PlayerState, a: PlayerActions, tv: Boolean, onChrome: (Boolean) -> Unit) = PlayerOverlay(st, a, tv, onChrome)
