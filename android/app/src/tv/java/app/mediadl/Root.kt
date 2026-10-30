package app.mediadl

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.LocalBringIntoViewSpec
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.key
import androidx.compose.ui.Modifier

/** The TV app's screens, each one canvas: the tabs are part of the page and scroll away with it; title pages have none. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun Root(nav: Nav, tv: Boolean, onUpdate: () -> Unit) {
    CompositionLocalProvider(LocalBringIntoViewSpec provides rememberMinimalSpec()) { // every list scrolls alike
        Box(Modifier.fillMaxSize().background(Base)) {
            BackHandler(enabled = nav.stack.size > 1) { nav.back() }
            val s = nav.stack.last()
            key(s) {
                when (s) {
                    is Screen.Detail -> DetailScreen(nav, s.id) // fades itself in, once its picture is ready
                    Screen.Films -> FadeIn { LibraryScreen(nav, shows = false, onUpdate = onUpdate) }
                    Screen.Shows -> FadeIn { LibraryScreen(nav, shows = true, onUpdate = onUpdate) }
                    Screen.Search -> FadeIn { SearchScreen(nav, onUpdate) }
                    else -> FadeIn { HomeScreen(nav, onUpdate = onUpdate) }
                }
            }
        }
    }
}
