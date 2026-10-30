package app.mediadl

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Search
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
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay

enum class Tab(val label: String) { Search("Search"), Home("Home"), Films("Films"), Shows("Shows") }

/** Space the pages leave at the top for the tab row. */
val TabsHeight = 76.dp

/**
 * tvOS-style tabs across the top: plain words, the current one white with a short underline,
 * the focused one a solid white capsule. An update, when there is one, sits at the far right.
 */
@Composable
fun TopTabs(current: Tab, onPick: (Tab) -> Unit, update: Boolean, onUpdate: () -> Unit, currentFr: FocusRequester) {
    Box(Modifier.fillMaxWidth().height(TabsHeight).background(Brush.verticalGradient(0f to Bg.copy(alpha = 0.7f), 1f to Color.Transparent))) {
        Row(
            Modifier.padding(start = 30.dp, top = 22.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically,
        ) {
            Tab.entries.forEach { t ->
                TabWord(t.label, t == current, if (t == current) currentFr else null) { onPick(t) }
            }
        }
        if (update) Box(Modifier.align(Alignment.TopEnd).padding(end = 30.dp, top = 22.dp)) {
            TabWord("Update available", selected = false, fr = null, dot = true, onClick = onUpdate)
        }
    }
}

@Composable
private fun TabWord(label: String, selected: Boolean, fr: FocusRequester?, dot: Boolean = false, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    Column(
        Modifier
            .width(IntrinsicSize.Max)
            .then(if (fr != null) Modifier.focusRequester(fr) else Modifier)
            .onFocusChanged { focused = it.isFocused }
            .lift(focused, RoundedCornerShape(16.dp), 1.06f)
            .background(if (focused) Color.White else Color.Transparent)
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .padding(horizontal = 10.dp, vertical = 6.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (dot) {
                Box(Modifier.size(6.dp).clip(CircleShape).background(if (focused) Bg else Ink))
                Spacer(Modifier.width(7.dp))
            }
            Text(
                label, fontSize = 14.5.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-0.1).sp,
                color = when { focused -> Bg; selected -> Ink; else -> Color(0x8CFFFFFF) },
            )
        }
        Box(Modifier.padding(top = 4.dp).height(2.dp).fillMaxWidth().clip(CircleShape).background(if (selected && !focused) Ink else Color.Transparent))
    }
}

/** Films or Shows: every poster in a grid under the tabs. Nothing else. */
@Composable
fun LibraryScreen(nav: Nav, shows: Boolean) {
    var items by remember(shows) { mutableStateOf<List<Item>?>(null) }
    var error by remember(shows) { mutableStateOf<String?>(null) }
    LaunchedEffect(shows, nav.refresh) {
        try {
            items = Api.get(if (shows) "/api/v1/shows" else "/api/v1/movies").optJSONArray("items").objects().map(Item::parse)
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
    PosterGrid(all, nav.gridStates.getOrPut(if (shows) "shows" else "films") { LazyGridState() }, first, nav) {
        if (all.isEmpty()) Text("Nothing here yet.", color = Mut, fontSize = 14.5.sp)
    }
}

/** Six posters across, 40 dp margins, the tvOS 2:3 grid. */
@Composable
private fun PosterGrid(items: List<Item>, state: LazyGridState, first: FocusRequester?, nav: Nav, header: @Composable () -> Unit) {
    LazyVerticalGrid(
        GridCells.Fixed(6),
        Modifier.fillMaxSize(),
        state = state,
        contentPadding = PaddingValues(start = 40.dp, end = 40.dp, top = TabsHeight + 6.dp, bottom = 40.dp),
        horizontalArrangement = Arrangement.spacedBy(26.dp),
        verticalArrangement = Arrangement.spacedBy(30.dp),
    ) {
        item(span = { GridItemSpan(maxLineSpan) }) { header() }
        itemsIndexed(items, key = { _, it -> it.id }) { i, it ->
            PosterCard(
                it, 125.dp, onClick = { nav.open(Screen.Detail(it.id)) },
                focusRequester = if (i == 0) first else null,
                modifier = Modifier.rowEnds(i % 6, if (i / 6 == (items.size - 1) / 6) (items.size - 1) % 6 + 1 else 6),
            )
        }
    }
}

fun gb(bytes: Long): String = if (bytes >= 1e9) "%.1f GB".format(bytes / 1e9) else "%d MB".format((bytes / 1e6).toInt())

/** Search titles and cast; results as posters. */
@Composable
fun SearchScreen(nav: Nav) {
    var q by remember { mutableStateOf(nav.query) }
    var results by remember { mutableStateOf<List<Item>>(emptyList()) }
    val fr = remember { FocusRequester() }
    LaunchedEffect(Unit) { delay(60); runCatching { fr.requestFocus() } }
    LaunchedEffect(q) {
        nav.query = q
        if (q.isBlank()) { results = emptyList(); return@LaunchedEffect }
        delay(250)
        results = runCatching {
            Api.get("/api/v1/search?q=" + java.net.URLEncoder.encode(q, "UTF-8")).optJSONArray("items").objects().map(Item::parse)
        }.getOrDefault(results)
    }
    PosterGrid(results, remember { LazyGridState() }, null, nav) {
        Column {
            var focused by remember { mutableStateOf(false) }
            Row(
                Modifier.padding(top = 4.dp, bottom = 6.dp).width(440.dp).height(44.dp)
                    .clip(RoundedCornerShape(10.dp))
                    .background(if (focused) Color(0x47FFFFFF) else Color(0x1FFFFFFF))
                    .padding(horizontal = 14.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(Icons.Rounded.Search, null, tint = Mut, modifier = Modifier.size(20.dp))
                Spacer(Modifier.width(10.dp))
                Box(Modifier.weight(1f)) {
                    if (q.isEmpty()) Text("Films, shows, actors", color = Faint, fontSize = 15.sp)
                    BasicTextField(
                        q, { q = it }, singleLine = true,
                        textStyle = TextStyle(fontFamily = Inter, fontSize = 15.sp, color = Ink),
                        cursorBrush = SolidColor(Color.White),
                        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                        modifier = Modifier.fillMaxWidth().focusRequester(fr).onFocusChanged { focused = it.isFocused },
                    )
                }
            }
            if (q.isNotBlank() && results.isEmpty()) Text("No matches for “$q”.", color = Mut, fontSize = 14.5.sp, modifier = Modifier.padding(top = 10.dp))
        }
    }
}
