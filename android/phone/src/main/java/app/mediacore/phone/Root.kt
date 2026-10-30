package app.mediacore.phone

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.key
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

/** Pages over the bottom bar; a title page covers it. */
@Composable
fun Root(nav: Nav) {
    Box(Modifier.fillMaxSize().background(Base)) {
        BackHandler(enabled = nav.stack.size > 1) { nav.back() }
        val s = nav.stack.last()
        key(s) {
            if (s is Screen.Detail) FadeIn { TitleScreen(nav, s.id) }
            else Column(Modifier.fillMaxSize()) {
                Box(Modifier.weight(1f).fillMaxWidth()) {
                    FadeIn {
                        when (s) {
                            Screen.Films -> LibraryScreen(nav, shows = false)
                            Screen.Shows -> LibraryScreen(nav, shows = true)
                            Screen.Search -> SearchScreen(nav)
                            else -> HomeScreen(nav)
                        }
                    }
                }
                TabBar(nav.tab, nav::go)
            }
        }
    }
}

@Composable
private fun TabBar(current: Tab, onPick: (Tab) -> Unit) {
    Column(Modifier.fillMaxWidth().background(Base).navigationBarsPadding()) {
        Spacer(Modifier.fillMaxWidth().height(1.dp).background(Overlay))
        Row(Modifier.fillMaxWidth().height(56.dp)) {
            TabItem("Home", AppIcons.Home, current == Tab.Home) { onPick(Tab.Home) }
            TabItem("Films", AppIcons.Movie, current == Tab.Films) { onPick(Tab.Films) }
            TabItem("Shows", AppIcons.Tv, current == Tab.Shows) { onPick(Tab.Shows) }
            TabItem("Search", AppIcons.Search, current == Tab.Search) { onPick(Tab.Search) }
        }
    }
}

@Composable
private fun androidx.compose.foundation.layout.RowScope.TabItem(label: String, icon: ImageVector, selected: Boolean, onClick: () -> Unit) {
    Column(
        Modifier.weight(1f).fillMaxSize().tap(onClick = onClick).padding(top = 8.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        val c = if (selected) Ink else Muted
        Icon(icon, null, tint = c, modifier = Modifier.size(24.dp))
        Text(label, style = Type.Caption, color = c, modifier = Modifier.padding(top = 2.dp))
    }
}
