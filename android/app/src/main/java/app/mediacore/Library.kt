package app.mediacore

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.gestures.LocalBringIntoViewSpec
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.layout
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay

enum class Tab(val label: String) { Search("Search"), Home("Home"), Films("Films"), Shows("Shows") }

/** The tabs' own height at the top of a page. */
val TabsHeight = 60.dp

/** The gap between a page's tabs and its first row of posters, and from one row of posters to the next. */
private val GridGap = 24.dp

/** Six 125 dp posters across 880 dp: every poster starts on a whole pixel. */
private val GridSide = 26.dp

/** A page's tabs, for the top of whatever it scrolls: they're part of the page, on the one canvas. */
@Composable
fun PageTabs(nav: Nav, onUpdate: () -> Unit) {
    TopTabs(nav.tab, nav::go, nav.update != null, onUpdate, nav.railFocus) { nav.contentFocus }
}

/**
 * Tabs across the top: plain words. The current one is blue and heavier, and the focused one is
 * white with a crisp line under it. An update, when there is one, sits at the far right.
 */
@Composable
fun TopTabs(current: Tab, onPick: (Tab) -> Unit, update: Boolean, onUpdate: () -> Unit, currentFr: FocusRequester, downTo: () -> FocusRequester?) {
    Box(Modifier.fillMaxWidth().height(TabsHeight)) {
        Row(
            Modifier.padding(start = Side, top = 20.dp),
            horizontalArrangement = Arrangement.spacedBy(24.dp), verticalAlignment = Alignment.CenterVertically,
        ) {
            Tab.entries.forEach { t ->
                TabWord(t.label, t == current, if (t == current) currentFr else null, downTo) { onPick(t) }
            }
        }
        if (update) Box(Modifier.align(Alignment.TopEnd).padding(end = Side, top = 20.dp)) {
            TabWord("Update available", selected = false, fr = null, downTo = downTo, dot = true, onClick = onUpdate)
        }
    }
}

@Composable
private fun TabWord(label: String, selected: Boolean, fr: FocusRequester?, downTo: () -> FocusRequester?, dot: Boolean = false, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    Row(
        Modifier
            .focusProperties { downTo()?.let { down = it } }
            .then(if (fr != null) Modifier.focusRequester(fr) else Modifier)
            .onFocusChanged { focused = it.isFocused }
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .focusLine(focused)
            .height(24.dp),
        verticalAlignment = Alignment.Top,
    ) {
        if (dot) {
            Box(Modifier.padding(top = 7.dp).size(6.dp).background(if (focused) Ink else AccentWord))
            Spacer(Modifier.width(8.dp))
        }
        val c = when { selected -> AccentWord; focused -> Ink; else -> Muted }
        Text(label, style = if (selected) Type.Label.copy(fontWeight = FontWeight.ExtraBold) else Type.Label, color = c)
    }
}

/** Films or Shows: a grid of posters, nothing else. */
@Composable
fun LibraryScreen(nav: Nav, shows: Boolean, onUpdate: () -> Unit) {
    var items by remember(shows) { mutableStateOf<List<Item>?>(null) }
    var error by remember(shows) { mutableStateOf<String?>(null) }
    LaunchedEffect(shows, nav.refresh) {
        try {
            items = Api.items(if (shows) "/api/v1/shows" else "/api/v1/movies")
        } catch (e: Exception) {
            if (items == null) error = e.message
        }
    }
    val all = items
    if (all == null) {
        if (error != null) ErrorView(error!!, onRetry = { error = null; nav.refresh++ }) else Loading()
        return
    }
    val first = remember { FocusRequester() }
    LaunchedEffect(shows) { delay(60); runCatching { first.requestFocus() } }
    val key = if (shows) "shows" else "films"
    PosterGrid(
        all, nav.gridStates.getOrPut(key) { LazyGridState() }, first, nav, onUpdate, focusKey = key,
        header = if (all.isEmpty()) ({ Text("Nothing here yet.", style = Type.Body, color = Muted) }) else null,
    )
}

/** Six posters across under the page's tabs. Back lands on the poster you left ([focusKey] remembers it). */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun PosterGrid(
    items: List<Item>, state: LazyGridState, first: FocusRequester?, nav: Nav, onUpdate: () -> Unit, focusKey: String? = null,
    header: (@Composable () -> Unit)? = null,
) {
    val target = remember(items) { items.indexOfFirst { it.id == nav.gridFocus[focusKey] }.coerceAtLeast(0) }
    val requesters = remember { mutableMapOf<String, FocusRequester>() }
    // the top row sits exactly where the page puts it, tabs in view; lower rows keep the same distance from the top
    CompositionLocalProvider(LocalBringIntoViewSpec provides rememberMinimalSpec(TabsHeight + GridGap)) {
        LazyVerticalGrid(
            GridCells.Fixed(6),
            Modifier.fillMaxSize(),
            state = state,
            contentPadding = PaddingValues(start = Side, end = Side, bottom = 64.dp),
            horizontalArrangement = Arrangement.spacedBy(GridSide),
            verticalArrangement = Arrangement.spacedBy(GridGap),
        ) {
            item(key = "tabs", span = { GridItemSpan(maxLineSpan) }) { Box(Modifier.layoutOffsetStart(-Side)) { PageTabs(nav, onUpdate) } }
            if (header != null) item(key = "header", span = { GridItemSpan(maxLineSpan) }) { header() }
            itemsIndexed(items, key = { _, it -> it.id }) { i, it ->
                val fr = if (i == target && first != null) first else requesters.getOrPut(it.id) { FocusRequester() }
                PosterCard(
                    it, 125.dp, onClick = { nav.open(Screen.Detail(it.id)) },
                    onFocus = { nav.contentFocus = fr; if (focusKey != null) nav.gridFocus[focusKey] = it.id },
                    focusRequester = fr,
                    modifier = Modifier.rowEnds(i % 6, if (i / 6 == (items.size - 1) / 6) (items.size - 1) % 6 + 1 else 6)
                        // the top row goes up to the current tab
                        .then(if (i < 6 && focusKey != null) Modifier.focusProperties { up = nav.railFocus } else Modifier),
                )
            }
        }
    }
}

/** Lays content out wider than its cell, back out to the page's edge: the tabs bring their own margin. */
private fun Modifier.layoutOffsetStart(dx: Dp): Modifier = layout { m, c ->
    val d = -dx.roundToPx()
    val p = m.measure(c.copy(maxWidth = c.maxWidth + 2 * d, minWidth = c.maxWidth + 2 * d))
    layout(c.maxWidth, p.height) { p.place(-d, 0) }
}

fun gb(bytes: Long): String = if (bytes >= 1e9) "%.1f GB".format(bytes / 1e9) else "%d MB".format((bytes / 1e6).toInt())

private val KeyRows = listOf("ABCDEF", "GHIJKL", "MNOPQR", "STUVWX", "YZ1234", "567890")
private val KeySize = 36.dp
private val KeyGap = 4.dp
private val KeyboardWidth = KeySize * 6 + KeyGap * 5
private val SearchTop = 68.dp // where Home's words start

/** Four 125 dp posters and 32 dp gaps, ending on the page's margin: every one starts on a whole pixel. */
private val ResultsStart = 324.dp

/**
 * Search titles and cast with our own keyboard, on the left under the tabs. On the right, posters scroll past
 * beside the tabs: the whole library until you type, then what matches.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun SearchScreen(nav: Nav, onUpdate: () -> Unit) {
    var q by remember { mutableStateOf(nav.query) }
    var results by remember { mutableStateOf<List<Item>>(emptyList()) }
    var library by remember { mutableStateOf<List<Item>>(emptyList()) }
    val firstKey = remember { FocusRequester() }
    val requesters = remember { mutableMapOf<String, FocusRequester>() }
    val state = nav.gridStates.getOrPut("search") { LazyGridState() }
    val shown = if (q.isBlank()) library else results
    // back from a title: to the poster you left; otherwise to the keys
    val wanted = remember { nav.gridFocus["search"] }
    var restored by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) {
        library = runCatching { Api.items("/api/v1/shows") + Api.items("/api/v1/movies") }.getOrDefault(emptyList())
    }
    LaunchedEffect(Unit) {
        delay(if (wanted == null) 60 else 500)
        if (!restored) runCatching { firstKey.requestFocus() }
    }
    LaunchedEffect(shown) {
        if (wanted != null && !restored && shown.any { it.id == wanted }) {
            delay(60)
            restored = runCatching { requesters[wanted]?.requestFocus() }.isSuccess
        }
    }
    LaunchedEffect(q) {
        nav.query = q
        if (q.isBlank()) { results = emptyList(); return@LaunchedEffect }
        delay(250)
        results = runCatching { Api.items("/api/v1/search?q=" + java.net.URLEncoder.encode(q, "UTF-8")) }.getOrDefault(results)
    }
    val type = { edit: (String) -> String ->
        nav.gridFocus.remove("search")
        q = edit(q)
    }

    Box(Modifier.fillMaxSize()) {
        CompositionLocalProvider(LocalBringIntoViewSpec provides rememberMinimalSpec(SearchTop)) {
            LazyVerticalGrid(
                GridCells.Fixed(4),
                Modifier.fillMaxSize().padding(start = ResultsStart),
                state = state,
                contentPadding = PaddingValues(end = Side, top = SearchTop, bottom = 64.dp),
                horizontalArrangement = Arrangement.spacedBy(32.dp),
                verticalArrangement = Arrangement.spacedBy(GridGap),
            ) {
                if (q.isNotBlank() && results.isEmpty()) item(key = "none", span = { GridItemSpan(maxLineSpan) }) {
                    Text("No matches for “$q”.", style = Type.Body, color = Muted)
                }
                itemsIndexed(shown, key = { _, it -> it.id }) { i, it ->
                    val fr = requesters.getOrPut(it.id) { FocusRequester() }
                    PosterCard(
                        it, 125.dp, onClick = { nav.open(Screen.Detail(it.id)) },
                        onFocus = { nav.contentFocus = fr; nav.gridFocus["search"] = it.id },
                        focusRequester = fr,
                        modifier = Modifier.rowEnds(i % 4, if (i / 4 == (shown.size - 1) / 4) (shown.size - 1) % 4 + 1 else 4)
                            .then(if (i < 4) Modifier.focusProperties { up = nav.railFocus } else Modifier),
                    )
                }
            }
        }
        PageTabs(nav, onUpdate)
        Column(Modifier.padding(start = Side, top = SearchTop).width(KeyboardWidth), verticalArrangement = Arrangement.spacedBy(KeyGap)) {
            QueryLine(q)
            Spacer(Modifier.height(12.dp))
            KeyRows.forEachIndexed { r, keys ->
                Row(horizontalArrangement = Arrangement.spacedBy(KeyGap)) {
                    keys.forEachIndexed { c, k ->
                        val fr = if (r == 0 && c == 0) firstKey else remember { FocusRequester() }
                        Key(
                            Modifier.size(KeySize).focusRequester(fr).then(if (r == 0) Modifier.focusProperties { up = nav.railFocus } else Modifier),
                            onFocus = { nav.contentFocus = fr; nav.gridFocus.remove("search") },
                            onClick = { type { if (it.length < 40) it + k else it } },
                        ) { on -> Text("$k", style = Type.Label, color = on) }
                    }
                }
            }
            val wide = (KeyboardWidth - KeyGap) / 2
            Row(horizontalArrangement = Arrangement.spacedBy(KeyGap)) {
                val spaceFr = remember { FocusRequester() }
                val deleteFr = remember { FocusRequester() }
                Key(
                    Modifier.size(wide, KeySize).focusRequester(spaceFr),
                    onFocus = { nav.contentFocus = spaceFr; nav.gridFocus.remove("search") },
                    onClick = { type { if (it.isNotEmpty() && !it.endsWith(" ") && it.length < 40) "$it " else it } },
                ) { on -> Text("Space", style = Type.Label, color = on) }
                Key(
                    Modifier.size(wide, KeySize).focusRequester(deleteFr),
                    onFocus = { nav.contentFocus = deleteFr; nav.gridFocus.remove("search") },
                    onClick = { type { it.dropLast(1) } },
                    onLongClick = { type { "" } },
                ) { on -> Icon(AppIcons.Backspace, "Delete", tint = on, modifier = Modifier.size(24.dp)) }
            }
        }
    }
}

/** What you've typed, ending at its cursor: the latest letters when there are too many for the line. */
@Composable
private fun QueryLine(q: String) {
    Row(
        Modifier.fillMaxWidth().height(44.dp).background(Overlay, CardShape).padding(horizontal = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(AppIcons.Search, null, tint = Muted, modifier = Modifier.size(24.dp))
        Spacer(Modifier.width(8.dp))
        if (q.isEmpty()) Text("Search", style = Type.Body, color = Muted)
        else {
            Text(if (q.length > 16) "…" + q.takeLast(16) else q, style = Type.Body, maxLines = 1)
            Box(Modifier.padding(start = 2.dp).size(FocusLine, 20.dp).background(Ink))
        }
    }
}

/** One key: flat at rest, white while selected. A held key does [onLongClick] where it has one. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun Key(modifier: Modifier, onFocus: () -> Unit, onClick: () -> Unit, onLongClick: (() -> Unit)? = null, label: @Composable (Color) -> Unit) {
    var focused by remember { mutableStateOf(false) }
    Box(
        modifier
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .combinedClickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onLongClick = onLongClick, onClick = onClick)
            .background(fillFor(if (focused) Ink else Raised), CardShape),
        contentAlignment = Alignment.Center,
    ) { label(if (focused) Base else Ink) }
}
