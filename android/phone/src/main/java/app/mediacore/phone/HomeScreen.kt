package app.mediacore.phone

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.mediacore.core.Item
import app.mediacore.core.Offline
import app.mediacore.core.hm

@Composable
fun HomeScreen(nav: Nav) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var error by remember { mutableStateOf<String?>(null) }
    val cache = remember { java.io.File(ctx.cacheDir, "home.json") }
    remember { nav.restoreHome(cache) }
    LaunchedEffect(nav.refresh) {
        try {
            nav.loadHome(cache)
            error = null
        } catch (e: Exception) {
            if (nav.home == null) error = e.message ?: "Can't reach mint"
        }
    }
    val offline = remember(nav.refresh) { Offline.items().filter { Offline.state(it.id)?.done == true } }
    val home = nav.home
    if (home == null && offline.isEmpty()) {
        if (error != null) ErrorView(error!!, onRetry = { error = null; nav.refresh++ }) else Loading()
        return
    }

    val featured = home?.cont?.firstOrNull() ?: home?.recent?.firstOrNull()
    // each title once: the first row it belongs to keeps it
    val seen = remember(home) { mutableSetOf<String>() }.apply { clear(); featured?.let { add(it.id) } }
    fun fresh(list: List<Item>) = list.filter { seen.add(it.id) }
    val cont = fresh(home?.cont.orEmpty())
    val recent = fresh(home?.recent.orEmpty())
    val films = fresh(home?.movies.orEmpty())
    val shows = fresh(home?.shows.orEmpty())

    LazyColumn(Modifier.fillMaxSize(), state = nav.homeList, contentPadding = PaddingValues(bottom = 24.dp)) {
        if (featured != null) item("featured") {
            Featured(
                featured,
                onPlay = { openItem(ctx, scope, nav, featured, resume = true) },
                onInfo = { nav.open(Screen.Detail(featured.showId.ifEmpty { featured.id })) },
            )
        } else item("top") { Box(Modifier.pageTop()) }
        if (cont.isNotEmpty()) item("cont") {
            WideRow("Continue Watching", cont) { openItem(ctx, scope, nav, it, resume = true) }
        }
        if (offline.isNotEmpty()) item("offline") {
            WideRow("On This Phone", offline) { PlayerActivity.play(ctx, listOf(it), 0) }
        }
        listOf("Recently Added" to recent, "Films" to films, "Shows" to shows).forEach { (title, list) ->
            if (list.isNotEmpty()) item(title) {
                Column(Modifier.padding(top = 24.dp)) {
                    RowTitle(title, Modifier.padding(start = Side))
                    LazyRow(contentPadding = PaddingValues(horizontal = Side), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        items(list, key = { it.id }) { PosterCard(it, Modifier.width(120.dp)) { nav.open(Screen.Detail(it.id)) } }
                    }
                }
            }
        }
    }
}

/** The title at the top: the same block as a title page, with only the two things Home needs. */
@Composable
private fun Featured(item: Item, onPlay: () -> Unit, onInfo: () -> Unit) {
    Box(Modifier.fillMaxWidth()) {
        Hero(item.backdrop ?: item.wide, Modifier.tap(onClick = onInfo))
        Column(Modifier.padding(top = heroHeight() - HeroOverlap).padding(horizontal = Side)) {
            TitleMark(item.logo, item.showTitle)
            if (item.isEpisode) {
                Text("S${item.season}, E${item.episode} · ${item.title}", style = Type.Label, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 12.dp))
            } else {
                val kind = (listOf(if (item.isShow) "Show" else "Film") + item.meta?.genres.orEmpty().take(2)).joinToString(" · ")
                Text(kind, style = Type.Meta, color = Muted, maxLines = 1, modifier = Modifier.padding(top = 12.dp))
            }
            Row(Modifier.padding(top = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                PlayButton(playLabel(item), Modifier.weight(1f), onClick = onPlay)
                ToolButton(AppIcons.Info, "Details", onClick = onInfo)
            }
        }
    }
}

@Composable
private fun WideRow(title: String, list: List<Item>, onClick: (Item) -> Unit) {
    Column(Modifier.padding(top = 24.dp)) {
        RowTitle(title, Modifier.padding(start = Side))
        LazyRow(contentPadding = PaddingValues(horizontal = Side), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            items(list, key = { it.id }) { WideCard(it) { onClick(it) } }
        }
    }
}

/** The time left when there's something to resume, else the word. */
fun playLabel(item: Item?): String =
    if (item != null && item.pos > 0 && !item.watched && item.durMs > item.pos) hm(item.durMs - item.pos) else "Play"

