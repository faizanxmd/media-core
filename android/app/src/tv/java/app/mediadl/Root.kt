package app.mediadl

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier

/** The TV app's screens: pages under a row of tabs; title pages cover everything. */
@Composable
fun Root(nav: Nav, tv: Boolean, onUpdate: () -> Unit) {
    Box(Modifier.fillMaxSize().background(Bg)) {
        BackHandler(enabled = nav.stack.size > 1) { nav.back() }
        when (val s = nav.stack.last()) {
            is Screen.Detail -> DetailScreen(nav, s.id)
            else -> {
                when (s) {
                    Screen.Films -> LibraryScreen(nav, shows = false)
                    Screen.Shows -> LibraryScreen(nav, shows = true)
                    Screen.Search -> SearchScreen(nav)
                    else -> HomeScreen(nav, onUpdate = onUpdate)
                }
                TopTabs(nav.tab, nav::go, nav.update != null, onUpdate, nav.railFocus)
            }
        }
    }
}
