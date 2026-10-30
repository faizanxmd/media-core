@file:OptIn(ExperimentalTextApi::class)

package app.mediadl

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.ExperimentalTextApi
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// The phone app's look: Inter, neutral black, white text at a few opacities.
val PBg = Color(0xFF0B0B0D)
val PText = Color(0xF0FFFFFF)
val PDim = Color(0xB8FFFFFF)
val PFaint = Color(0x8CFFFFFF)

/** The phone app's screens: pages over a bottom tab bar (title pages cover it). */
@Composable
fun Root(nav: Nav, tv: Boolean, onUpdate: () -> Unit) {
    CompositionLocalProvider(LocalTextStyle provides TextStyle(fontFamily = Inter, color = PText)) {
        Box(Modifier.fillMaxSize().background(PBg)) {
            BackHandler(enabled = nav.stack.size > 1) { nav.back() }
            when (val s = nav.stack.last()) {
                is Screen.Detail -> PhoneDetail(nav, s.id)
                else -> Column(Modifier.fillMaxSize()) {
                    Box(Modifier.weight(1f).fillMaxWidth()) {
                        when (s) {
                            Screen.Films -> PhoneLibrary(nav, shows = false)
                            Screen.Shows -> PhoneLibrary(nav, shows = true)
                            Screen.Search -> PhoneSearch(nav)
                            else -> PhoneHome(nav)
                        }
                    }
                    TabBar(nav.tab, nav::go)
                }
            }
        }
    }
}

@Composable
private fun TabBar(current: Tab, onPick: (Tab) -> Unit) {
    Row(
        Modifier.fillMaxWidth().background(Color(0xF20B0B0D))
            .border(width = 0.5.dp, color = Color(0x14FFFFFF))
            .navigationBarsPadding().height(64.dp),
        horizontalArrangement = Arrangement.SpaceAround, verticalAlignment = Alignment.CenterVertically,
    ) {
        TabItem("Home", AppIcons.Home, AppIcons.HomeOutline, current == Tab.Home) { onPick(Tab.Home) }
        TabItem("Search", AppIcons.Search, AppIcons.SearchOutline, current == Tab.Search) { onPick(Tab.Search) }
        TabItem("Films", AppIcons.Movie, AppIcons.MovieOutline, current == Tab.Films) { onPick(Tab.Films) }
        TabItem("Shows", AppIcons.Tv, AppIcons.TvOutline, current == Tab.Shows) { onPick(Tab.Shows) }
    }
}

@Composable
private fun TabItem(label: String, on: ImageVector, off: ImageVector, selected: Boolean, onClick: () -> Unit) {
    Column(
        Modifier.width(76.dp).clickable(remember { MutableInteractionSource() }, null, onClick = onClick),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        val c = if (selected) PText else PFaint
        Icon(if (selected) on else off, null, tint = c, modifier = Modifier.size(26.dp))
        Text(label, color = c, fontSize = 11.sp, fontWeight = FontWeight.SemiBold)
    }
}
