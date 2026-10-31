package app.mediacore.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import app.mediacore.core.Api
import app.mediacore.core.Item
import kotlinx.coroutines.delay

/** Films or Shows: the bottom bar and a poster grid, nothing else. */
@Composable
fun LibraryScreen(nav: Nav, shows: Boolean) {
    val key = if (shows) "shows" else "films"
    var items by remember(key) { mutableStateOf(nav.lists[key]) }
    var error by remember(key) { mutableStateOf<String?>(null) }
    LaunchedEffect(key, nav.refresh) {
        try {
            val got = Api.items(if (shows) "/api/v1/shows" else "/api/v1/movies")
            nav.lists[key] = got
            items = got
        } catch (e: Exception) {
            if (items == null) error = e.message
        }
    }
    val all = items
    if (all == null) {
        if (error != null) ErrorView(error!!, onRetry = { error = null; nav.refresh++ }) else Loading()
        return
    }
    PosterGrid(nav, key, all) { Box(Modifier.pageTop()) }
}

/** The whole library until you type, then what matches. */
@Composable
fun SearchScreen(nav: Nav) {
    var q by remember { mutableStateOf(nav.query) }
    var results by remember { mutableStateOf<List<Item>?>(null) }
    var everything by remember { mutableStateOf(nav.lists["all"].orEmpty()) }
    val keyboard = LocalSoftwareKeyboardController.current
    LaunchedEffect(Unit) {
        runCatching {
            val all = Api.items("/api/v1/movies") + Api.items("/api/v1/shows")
            nav.lists["all"] = all
            everything = all
        }
    }
    LaunchedEffect(q) {
        nav.query = q
        if (q.isBlank()) { results = null; return@LaunchedEffect }
        delay(250)
        results = runCatching { Api.items("/api/v1/search?q=" + java.net.URLEncoder.encode(q, "UTF-8")) }.getOrDefault(results)
    }
    Box(Modifier.fillMaxSize().imePadding()) {
        PosterGrid(nav, "search", results ?: everything) {
            Column(Modifier.pageTop()) {
                Row(
                    Modifier.fillMaxWidth().height(ButtonHeight).clip(CardShape).background(Overlay).padding(start = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Icon(AppIcons.Search, null, tint = Muted, modifier = Modifier.size(24.dp))
                    Box(Modifier.padding(horizontal = 8.dp).weight(1f)) {
                        if (q.isEmpty()) Text("Search", style = Type.Body, color = Muted)
                        BasicTextField(
                            q, { q = it }, singleLine = true,
                            textStyle = Type.Body,
                            cursorBrush = SolidColor(Ink),
                            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                            keyboardActions = KeyboardActions(onSearch = { keyboard?.hide() }),
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                    if (q.isNotEmpty()) Box(Modifier.size(ButtonHeight).tap { q = "" }, contentAlignment = Alignment.Center) {
                        Icon(AppIcons.Close, "Clear", tint = Muted, modifier = Modifier.size(24.dp))
                    }
                }
                if (results?.isEmpty() == true) Text("No matches for “$q”", style = Type.Body, color = Muted, modifier = Modifier.padding(top = 16.dp))
            }
        }
    }
}

@Composable
private fun PosterGrid(nav: Nav, key: String, items: List<Item>, header: @Composable () -> Unit) {
    val state = remember(key) { nav.grids.getOrPut(key) { LazyGridState() } }
    LazyVerticalGrid(
        GridCells.Fixed(3), Modifier.fillMaxSize(), state = state,
        contentPadding = PaddingValues(start = Side, end = Side, bottom = 24.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item(span = { GridItemSpan(maxLineSpan) }) { header() }
        items(items, key = { it.id }) { PosterCard(it) { nav.open(Screen.Detail(if (it.isEpisode) it.showId.ifEmpty { it.id } else it.id)) } }
    }
}
