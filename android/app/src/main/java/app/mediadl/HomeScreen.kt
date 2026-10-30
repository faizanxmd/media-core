package app.mediadl

import android.content.Context
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.gestures.BringIntoViewSpec
import androidx.compose.foundation.gestures.LocalBringIntoViewSpec
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Refresh
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** TV "pivot" scrolling: the focused row slides up to the top of the list, title included. */
@OptIn(ExperimentalFoundationApi::class)
class PivotSpec(private val titlePx: Float) : BringIntoViewSpec {
    override fun calculateScrollDistance(offset: Float, size: Float, containerSize: Float): Float = offset - titlePx
}

/** Scroll only as far as needed to show the focused thing, with a margin. */
@OptIn(ExperimentalFoundationApi::class)
class MinimalSpec(private val marginPx: Float) : BringIntoViewSpec {
    override fun calculateScrollDistance(offset: Float, size: Float, containerSize: Float): Float {
        val start = offset - marginPx
        val end = offset + size + marginPx - containerSize
        return when {
            start < 0 -> start
            end > 0 -> minOf(end, start)
            else -> 0f
        }
    }
}

/** The TV's way of scrolling everywhere: move only when the focused thing nears an edge. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun rememberMinimalSpec(margin: Dp = 56.dp): BringIntoViewSpec {
    val density = LocalDensity.current
    return remember(density, margin) { MinimalSpec(density.run { margin.toPx() }) }
}

/** Last home seen: shown instantly on a cold start, replaced as soon as mint answers. */
fun Nav.restoreHome(cache: java.io.File) {
    if (home != null) return
    runCatching {
        val text = cache.readText()
        home = Home.parse(org.json.JSONObject(text))
        homeJson = text
    }
}

/** Asks mint for Home, off the UI thread. An unchanged answer redraws nothing (every return from the player asks). */
suspend fun Nav.loadHome(cache: java.io.File) {
    Offline.sync()
    val j = Api.get("/api/v1/home")
    withContext(Dispatchers.IO) {
        val text = j.toString()
        if (text != homeJson) {
            val parsed = Home.parse(j)
            runCatching { cache.writeText(text) }
            homeJson = text
            home = parsed
        }
    }
}

/** Where the rows start: under the hero, which shows whatever card has focus. */
private val RowsTop = 298.dp

@OptIn(ExperimentalFoundationApi::class)
@Composable
fun HomeScreen(nav: Nav, onUpdate: () -> Unit) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var error by remember { mutableStateOf<String?>(null) }
    val requesters = remember { mutableMapOf<String, FocusRequester>() }
    // last home seen: shown instantly on a cold start, replaced as soon as mint answers
    val cache = remember { java.io.File(ctx.cacheDir, "home.json") }
    remember { nav.restoreHome(cache) }

    suspend fun load() {
        try {
            nav.loadHome(cache)
            error = null
        } catch (e: Exception) {
            if (nav.home == null) error = e.message ?: "Can't reach mint"
        }
    }
    LaunchedEffect(nav.refresh) { load() }

    val home = nav.home
    if (home == null) {
        if (error != null) ErrorView(error!!, onRetry = { error = null; scope.launch { Api.discover(); load() } })
        else Loading()
        return
    }

    // only the hero reads this, so a press redraws the hero and two cards rather than every row
    val focusedId = remember { mutableStateOf(nav.homeFocus?.substringAfter('/') ?: (home.cont + home.recent).firstOrNull()?.id) }

    LaunchedEffect(Unit) {
        delay(60)
        val target = nav.homeFocus?.let { requesters[it] } ?: requesters.values.firstOrNull()
        runCatching { target?.requestFocus() }.onFailure { runCatching { requesters.values.firstOrNull()?.requestFocus() } }
    }

    Box(Modifier.fillMaxSize()) {
        HomeHero(home, focusedId)

        val density = LocalDensity.current
        val rowSpec = rememberMinimalSpec()
        // a little room above the focused row: the lifted card must not be cut off at the top
        val listSpec = remember(density) { PivotSpec(density.run { 10.dp.toPx() }) }
        CompositionLocalProvider(LocalBringIntoViewSpec provides listSpec) {
            LazyColumn(
                state = nav.homeList,
                modifier = Modifier.padding(top = RowsTop).fillMaxSize(),
                contentPadding = PaddingValues(bottom = 300.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                fun row(key: String, title: String, list: List<Item>, wide: Boolean) {
                    if (list.isEmpty()) return
                    item(key) {
                        CompositionLocalProvider(LocalBringIntoViewSpec provides rowSpec) {
                            Column {
                                RowTitle(title, Modifier.padding(start = 40.dp))
                                LazyRow(
                                    state = nav.rowStates.getOrPut(key) { LazyListState() },
                                    contentPadding = PaddingValues(horizontal = 40.dp, vertical = 12.dp),
                                    horizontalArrangement = Arrangement.spacedBy(20.dp),
                                    modifier = Modifier.layoutOffset((-12).dp),
                                ) {
                                    itemsIndexed(list, key = { _, it -> it.id }) { i, it ->
                                        val k = "$key/${it.id}"
                                        val fr = requesters.getOrPut(k) { FocusRequester() }
                                        val onFocus = { nav.homeFocus = k; focusedId.value = it.id }
                                        val click = { openItem(ctx, scope, nav, it, resume = key == "continue") }
                                        if (wide) WideCard(it, 205.dp, click, Modifier.rowEnds(i, list.size), onFocus, fr)
                                        else PosterCard(it, 125.dp, click, Modifier.rowEnds(i, list.size), onFocus, fr)
                                    }
                                }
                            }
                        }
                    }
                }
                row("continue", "Continue Watching", home.cont, wide = true)
                row("recent", "Recently Added", home.recent, wide = false)
                row("shows", "Shows", home.shows, wide = false)
                row("movies", "Films", home.movies, wide = false)
                if (home.movies.isEmpty() && home.shows.isEmpty()) item("empty") {
                    Text("Nothing in the library yet. Add links at mint:8780.", color = Mut, fontSize = 14.5.sp, modifier = Modifier.padding(start = 40.dp))
                }
            }
        }
    }
}

/** The backdrop and text for the focused card; they change once the scroll has landed, so the two never compete for frames. */
@Composable
private fun BoxScope.HomeHero(home: Home, focusedId: androidx.compose.runtime.State<String?>) {
    val items = remember(home) { (home.cont + home.recent + home.shows + home.movies).associateBy { it.id } }
    val hero = focusedId.value?.let(items::get) ?: (home.cont + home.recent + home.movies + home.shows).firstOrNull()
    var shown by remember { mutableStateOf(hero) }
    LaunchedEffect(hero?.id) { delay(if (shown == null) 0 else Motion.SettleMs.toLong()); shown = hero }
    MaskedBackdrop(shown?.let { Api.img(it.meta?.backdrop?.ifEmpty { null }, "w1280") })
    shown?.let { Hero(it, home, Modifier.padding(start = 40.dp).width(440.dp).height(RowsTop - 21.dp)) }
}

/** Moves content up by [dy] and gives the space back: the row's padding (room for a lifted card) tucks under its title. */
private fun Modifier.layoutOffset(dy: Dp): Modifier = layout { m, c ->
    val p = m.measure(c)
    val d = dy.roundToPx()
    layout(p.width, (p.height + d).coerceAtLeast(0)) { p.place(0, d) }
}

/** Logo, one short line of facts, where you are, two lines of story, when it ends. */
@Composable
private fun Hero(it: Item, home: Home, modifier: Modifier) {
    val ctx = LocalContext.current
    val show = if (it.isEpisode) home.shows.firstOrNull { s -> s.id == it.showId } else null
    val base = show ?: it
    // laid out from the top, under the tabs: the logo stays put whatever the text below it
    Column(modifier.padding(top = 60.dp)) {
        Box(Modifier.height(85.dp).width(280.dp), contentAlignment = Alignment.BottomStart) {
            val logo = it.logo ?: show?.logo
            if (logo != null) AsyncImage(logo, it.showTitle, Modifier.fillMaxSize(), contentScale = ContentScale.Fit, alignment = Alignment.BottomStart)
            else Text(it.showTitle, fontSize = 30.sp, lineHeight = 32.sp, fontWeight = FontWeight.Bold, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        Spacer(Modifier.height(12.dp))
        val facts = listOfNotNull(
            base.displayYear.takeIf { y -> y > 0 }?.toString(),
            base.meta?.genres?.firstOrNull(),
            (base.meta?.cert ?: it.meta?.cert)?.ifEmpty { null },
        )
        if (facts.isNotEmpty()) Text(facts.joinToString("  ·  "), color = Color80, fontSize = 12.5.sp, fontWeight = FontWeight.Medium)
        if (it.isEpisode) {
            Text("S${it.season}, E${it.episode}  ${it.title}", fontSize = 14.5.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 10.dp))
        }
        (it.meta?.overview ?: base.meta?.overview)?.takeIf { o -> o.isNotEmpty() }?.let { o ->
            Text(o, color = Color76, fontSize = 14.5.sp, lineHeight = 19.sp, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 7.dp))
        }
        val left = when {
            it.isShow -> 0L
            it.pos > 0 && it.durMs > it.pos -> it.durMs - it.pos
            it.durMs > 0 -> it.durMs
            else -> (it.meta?.runtime ?: 0) * 60000L
        }
        if (left > 0) Row(Modifier.padding(top = 9.dp)) {
            Text("Ends at ", color = Faint, fontSize = 12.5.sp, fontWeight = FontWeight.Medium)
            Text(endsAt(ctx, left), fontSize = 12.5.sp, fontWeight = FontWeight.SemiBold)
        }
    }
}

private val Color80 = Color(0xCCFFFFFF)
private val Color76 = Color(0xC2FFFFFF)

/** "22:42" (or "10:42 PM", per the TV's setting): now plus what's left. */
fun endsAt(ctx: Context, leftMs: Long): String =
    android.text.format.DateFormat.getTimeFormat(ctx).format(java.util.Date(System.currentTimeMillis() + leftMs))

/** Continue Watching plays straight away; anything else opens its page. */
fun openItem(ctx: Context, scope: CoroutineScope, nav: Nav, it: Item, resume: Boolean = false) {
    when {
        it.isEpisode -> scope.launch { playEpisode(ctx, it) }
        resume && it.type == "movie" -> PlayerActivity.play(ctx, listOf(it), 0)
        else -> nav.open(Screen.Detail(it.id))
    }
}

/** Plays an episode with the rest of the show queued behind it (autoplay next). */
suspend fun playEpisode(ctx: Context, ep: Item, fromStart: Boolean = false) {
    val show = runCatching { Item.parse(Api.get("/api/v1/item/${ep.showId}").getJSONObject("item")) }.getOrNull()
    val eps = show?.let(::episodeQueue).orEmpty()
    val i = eps.indexOfFirst { it.id == ep.id }
    if (i < 0) PlayerActivity.play(ctx, listOf(ep), 0, fromStart)
    else PlayerActivity.play(ctx, eps, i, fromStart)
}

/** Every episode in viewing order (the server already puts specials last). */
fun episodeQueue(show: Item): List<Item> = show.seasons.flatMap { it.episodes }

@Composable
fun Loading() {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(color = Ink, strokeWidth = 2.dp, modifier = Modifier.size(32.dp))
    }
}

@Composable
fun ErrorView(msg: String, onRetry: () -> Unit, extra: @Composable () -> Unit = {}) {
    val fr = remember { FocusRequester() }
    Box(Modifier.fillMaxSize().padding(40.dp), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text("Can't reach mint", fontSize = 24.sp, fontWeight = FontWeight.Bold)
            Spacer(Modifier.height(10.dp))
            Text(
                "At home: check that mint is on.\nAway from home: open the Tailscale app and connect.",
                color = Mut, fontSize = 14.5.sp, lineHeight = 20.sp,
            )
            if (msg != "Can't reach mint") Text(msg, color = Faint, fontSize = 12.sp, modifier = Modifier.padding(top = 6.dp))
            Spacer(Modifier.height(20.dp))
            CircleButton(Icons.Rounded.Refresh, "Try again", focusRequester = fr, onClick = onRetry)
            Spacer(Modifier.height(24.dp))
            extra()
        }
    }
    LaunchedEffect(Unit) { runCatching { fr.requestFocus() } }
}
