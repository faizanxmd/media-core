package app.mediadl

import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.gestures.BringIntoViewSpec
import androidx.compose.foundation.gestures.LocalBringIntoViewSpec
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
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
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import coil3.imageLoader
import coil3.request.ImageRequest
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
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

/** Home's poster size: a focused row with its title fits, the next row's title shows whole, and its posters peek in below. */
private val HomePoster = 100.dp
private val HomeWide = 190.dp

/**
 * How far down a focused row's cards sit: a margin, then the row's title. The margin is smaller than the stage's
 * bottom gap, so the first row leaves the stage's empty edge on screen, and with it the stage and tabs ready to return to.
 */
private val RowPivot = 20.dp + RowTitleHeight

/** Scrolling on Home: a focused row rises to the top of the screen; the featured title's own button moves nothing. */
@OptIn(ExperimentalFoundationApi::class)
private class HomeSpec(private val pivotPx: Float, private val onStage: () -> Boolean) : BringIntoViewSpec {
    override fun calculateScrollDistance(offset: Float, size: Float, containerSize: Float): Float =
        if (onStage()) 0f else offset - pivotPx
}

/**
 * The featured title up top, which stays put while you browse. Moving down, it scrolls away and the rows
 * take over. The page never redraws for each title, and the posters speak for themselves.
 */
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

    val focusedRow = remember { mutableStateOf(nav.homeFocus?.substringBefore('/')) }
    val featured = remember(home) { (home.cont + home.recent + home.shows + home.movies).firstOrNull() }
    val stageFr = remember { FocusRequester() }
    val onStage = remember { booleanArrayOf(nav.homeFocus == null || nav.homeFocus == "stage") }

    LaunchedEffect(Unit) {
        delay(60)
        val target = nav.homeFocus?.let { if (it == "stage") stageFr else requesters[it] } ?: stageFr
        runCatching { target.requestFocus() }.onFailure { runCatching { stageFr.requestFocus() } }
    }

    // down in the rows the tabs have scrolled away: Back returns to the top before it leaves the app
    val scrolled by remember { derivedStateOf { nav.homeList.firstVisibleItemIndex > 0 || nav.homeList.firstVisibleItemScrollOffset > 0 } }
    BackHandler(enabled = scrolled) {
        onStage[0] = true
        nav.homeFocus = "stage"
        scope.launch {
            nav.homeList.animateScrollToItem(0)
            withFrameNanos { }
            runCatching { stageFr.requestFocus() }
        }
    }

    // the page a focused title would open, decoded ahead so it arrives whole; only once the remote has settled
    val preload = remember { arrayOfNulls<Job>(1) }
    val preloadSoon = { it: Item ->
        preload[0]?.cancel()
        preload[0] = scope.launch {
            delay(Motion.SettleMs.toLong())
            it.banner?.let { url -> ctx.imageLoader.enqueue(ImageRequest.Builder(ctx).data(url).size(1920, 1080).build()) }
        }
        Unit
    }

    Box(Modifier.fillMaxSize()) {
        val density = LocalDensity.current
        val rowSpec = rememberMinimalSpec()
        val listSpec = remember(density) { HomeSpec(density.run { RowPivot.toPx() }) { onStage[0] } }
        val topRow = when {
            home.cont.isNotEmpty() -> "continue"
            home.recent.isNotEmpty() -> "recent"
            home.shows.isNotEmpty() -> "shows"
            else -> "movies"
        }
        CompositionLocalProvider(LocalBringIntoViewSpec provides listSpec) {
            LazyColumn(
                state = nav.homeList,
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(bottom = 300.dp),
            ) {
                item("stage") {
                    Column {
                        PageTabs(nav, onUpdate)
                        if (featured != null) FeaturedStage(featured, home.shows, stageFr, nav) {
                            onStage[0] = true
                            nav.homeFocus = "stage"
                            nav.contentFocus = stageFr
                            focusedRow.value = null
                            preloadSoon(home.shows.firstOrNull { s -> s.id == featured.showId } ?: featured)
                            scope.launch { nav.homeList.animateScrollToItem(0) }
                        }
                    }
                }
                fun row(key: String, title: String, list: List<Item>, wide: Boolean) {
                    if (list.isEmpty()) return
                    item(key) {
                        CompositionLocalProvider(LocalBringIntoViewSpec provides rowSpec) {
                            Column {
                                HomeRowTitle(title, key, focusedRow)
                                LazyRow(
                                    state = nav.rowStates.getOrPut(key) { LazyListState() },
                                    contentPadding = PaddingValues(start = Side, end = Side, top = 12.dp, bottom = SectionGap),
                                    horizontalArrangement = Arrangement.spacedBy(16.dp),
                                    modifier = Modifier.layoutOffset((-12).dp),
                                ) {
                                    itemsIndexed(list, key = { _, it -> it.id }) { i, it ->
                                        val k = "$key/${it.id}"
                                        val fr = requesters.getOrPut(k) { FocusRequester() }
                                        val onFocus = { onStage[0] = false; nav.homeFocus = k; nav.contentFocus = fr; focusedRow.value = key; preloadSoon(it) }
                                        val click = { openItem(ctx, scope, nav, it, resume = key == "continue") }
                                        // Up from the top row goes to the featured title's button, not whatever is nearest
                                        val mod = Modifier.rowEnds(i, list.size).then(if (key == topRow) Modifier.focusProperties { up = stageFr } else Modifier)
                                        if (wide) WideCard(it, HomeWide, click, mod, onFocus, fr)
                                        else PosterCard(it, HomePoster, click, mod, onFocus, fr)
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
                    Text("Nothing in the library yet. Add links at mint:8780.", style = Type.Body, color = Muted, modifier = Modifier.padding(start = Side))
                }
            }
        }
    }
}

/** The same top as a title page, for the title you're most likely to want next. */
@Composable
private fun FeaturedStage(it: Item, shows: List<Item>, fr: FocusRequester, nav: Nav, onFocus: () -> Unit) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val show = if (it.isEpisode) shows.firstOrNull { s -> s.id == it.showId } else null
    val base = show ?: it
    Stage(Api.img(it.meta?.backdrop?.ifEmpty { null } ?: base.meta?.backdrop?.ifEmpty { null }, "w1280")) {
        TitleMark(it.logo ?: show?.logo, it.showTitle)
        val facts = listOfNotNull(
            base.displayYear.takeIf { y -> y > 0 }?.toString(),
            base.meta?.genres?.firstOrNull(),
            (base.meta?.cert ?: it.meta?.cert)?.ifEmpty { null },
        )
        if (facts.isNotEmpty()) Text(facts.joinToString(" · "), style = Type.Meta, color = Muted, modifier = Modifier.padding(top = 12.dp))
        if (it.isEpisode) Text("S${it.season}, E${it.episode} · ${it.title}", style = Type.Label, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 12.dp))
        (it.meta?.overview ?: base.meta?.overview)?.takeIf { o -> o.isNotEmpty() }?.let { o ->
            Text(o, style = Type.Body, color = Muted, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = if (it.isEpisode) 4.dp else 12.dp))
        }
        Row(Modifier.padding(top = 16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            val resumable = it.pos > 0 && !it.watched && it.durMs > it.pos
            PlayCapsule(
                if (resumable) hm(it.durMs - it.pos) else "Play",
                Modifier.focusProperties { up = nav.railFocus }, focusRequester = fr, onFocus = onFocus,
            ) { openItem(ctx, scope, nav, it, resume = true) }
            ToolButton(AppIcons.Info, "More about this", Modifier.focusProperties { up = nav.railFocus }, onFocus = onFocus) {
                nav.open(Screen.Detail(it.showId.ifEmpty { it.id }))
            }
        }
    }
}

/** The focused row's title is bright and the others quiet. On its own, so a press redraws only the two that change. */
@Composable
private fun HomeRowTitle(title: String, key: String, focusedRow: androidx.compose.runtime.State<String?>) {
    RowTitle(title, Modifier.padding(start = Side), color = if (focusedRow.value == key) Ink else Muted)
}

/** Moves content up by [dy] and gives the space back: the row's padding (room for a lifted card) tucks under its title. */
private fun Modifier.layoutOffset(dy: Dp): Modifier = layout { m, c ->
    val p = m.measure(c)
    val d = dy.roundToPx()
    layout(p.width, (p.height + d).coerceAtLeast(0)) { p.place(0, d) }
}

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
    else PlayerActivity.play(ctx, eps, i, fromStart, show?.logo)
}

/** Every episode in viewing order (the server already puts specials last). */
fun episodeQueue(show: Item): List<Item> = show.seasons.flatMap { it.episodes }

/** Shows only if the wait is real: a page that arrives quickly never flashes a ring first. */
@Composable
fun Loading() {
    var show by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { delay(500); show = true }
    if (show) Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        LoadingRing()
    }
}

@Composable
fun ErrorView(msg: String, onRetry: () -> Unit, extra: @Composable () -> Unit = {}) {
    val fr = remember { FocusRequester() }
    Box(Modifier.fillMaxSize().padding(Side), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text("Can't reach mint", style = Type.Title)
            Spacer(Modifier.height(12.dp))
            Text("At home: check that mint is on.\nAway from home: open the Tailscale app and connect.", style = Type.Body, color = Muted)
            if (msg != "Can't reach mint") Text(msg, style = Type.Caption, color = Muted, modifier = Modifier.padding(top = 8.dp))
            Spacer(Modifier.height(24.dp))
            ToolButton(AppIcons.Refresh, "Try again", focusRequester = fr, onClick = onRetry)
            Spacer(Modifier.height(24.dp))
            extra()
        }
    }
    LaunchedEffect(Unit) { runCatching { fr.requestFocus() } }
}
