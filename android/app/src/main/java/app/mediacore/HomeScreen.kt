package app.mediacore

import android.app.Activity
import android.content.Context
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.BringIntoViewSpec
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.requiredWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.graphics.Shadow
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import app.mediacore.core.Api
import app.mediacore.core.Download
import app.mediacore.core.Home
import app.mediacore.core.Item
import app.mediacore.core.Offline
import coil3.compose.AsyncImage
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.IOException

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
    Api.settled()
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

/** A title page's data, kept so a page opened again shows at once. */
suspend fun Nav.fetchDetail(id: String): Pair<Item, Item?> {
    Api.settled()
    val j = Api.get("/api/v1/item/$id")
    val d = withContext(Dispatchers.Default) { Item.parse(j.getJSONObject("item")) to j.optJSONObject("up_next")?.let(Item::parse) }
    details[id] = d
    return d
}

/** A failed request: mint not answering (show the offline screen) or something else (keep what's on screen). */
fun Nav.failed(e: Exception) {
    if (e is IOException && e !is Api.ServerError) offline = true
}

// ---------- the board's numbers (01-home.html), in board pixels ----------

private const val LEFT = 120
private const val ROW0 = 560 // the first row's title: the focused row always lands here
private const val TITLE_TO_CARDS = 36
private const val ROW_GAP = 28 // under a row's last line, before the next row's title
private const val WIDE_W = 336
private const val WIDE_H = 189
private const val POSTER_W = 160
private const val POSTER_H = 240
private const val WIDE_BLOCK = WIDE_H + 10 + 6 + 11 + 22 + 2 + 20 // picture, bar, title, line under it
private const val POSTER_BLOCK = POSTER_H

private class HomeRow(val key: String, val title: String, val wide: Boolean, val cards: List<Card>)

/** A card: a library item, or a title still downloading. */
private class Card(val key: String, val item: Item?, val download: Download?)

/** What the hero describes: the show for an episode, the film itself, or a download. */
private data class Hero(val item: Item?, val download: Download?, val key: String)

/**
 * Home is the whole app: the hero (the focused title's art, logo, line and story) over two rows, Continue watching
 * and Recently added. Settings is top-right, the logo mark top-left. The focused card holds x = 120 as its row slides,
 * and the focused row holds y = 560 as the page slides.
 */
@Composable
fun HomeScreen(nav: Nav, onUpdate: () -> Unit) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val art = LocalArt.current
    val cache = remember { java.io.File(ctx.cacheDir, "home.json") }
    remember { nav.restoreHome(cache) }

    LaunchedEffect(nav.refresh) {
        try {
            nav.loadHome(cache)
            nav.offline = false
        } catch (e: Exception) {
            nav.failed(e)
        }
    }
    val home = nav.home ?: return
    // while something downloads, ask often, so its progress moves smoothly instead of jumping once a minute
    LaunchedEffect(home.downloads.isNotEmpty()) {
        while (home.downloads.isNotEmpty()) {
            delay(5000)
            runCatching { nav.loadHome(cache) }
        }
    }

    val rows = remember(home) { homeRows(home) }
    val showsById = remember(home) { home.shows.associateBy { it.id } }
    fun heroOf(c: Card): Hero = when {
        c.download != null -> Hero(null, c.download, "dl/" + c.download.title)
        c.item!!.isEpisode -> (showsById[c.item.showId] ?: c.item).let { Hero(it, null, it.id) }
        else -> Hero(c.item, null, c.item.id)
    }
    val featured = remember(rows) { rows.firstOrNull()?.cards?.firstOrNull()?.let(::heroOf) }

    val requesters = remember { mutableMapOf<String, FocusRequester>() }
    fun fr(k: String) = requesters.getOrPut(k) { FocusRequester() }
    var focus by remember { mutableStateOf(nav.homeFocus ?: "play") }
    val inRows = focus.contains('/')
    val focusRow = rows.indexOfFirst { r -> focus.startsWith(r.key + "/") }.coerceAtLeast(0)

    // the hero follows focus, but only once the remote has rested: fast scrolling doesn't flash pictures
    val wanted = if (inRows) rows.flatMap { it.cards }.firstOrNull { it.key == focus }?.let(::heroOf) ?: featured else (nav.heroKey?.let { k -> rows.flatMap { it.cards }.map(::heroOf).firstOrNull { it.key == k } } ?: featured)
    var hero by remember { mutableStateOf(wanted) }
    LaunchedEffect(wanted?.key) {
        if (wanted?.key != hero?.key) delay(Move.Settle)
        hero = wanted
        nav.heroKey = wanted?.key
    }
    // its show page, for the seasons count and what Play would start
    var heroDetail by remember { mutableStateOf<Pair<Item, Item?>?>(null) }
    LaunchedEffect(hero?.key) {
        val it = hero?.item ?: return@LaunchedEffect
        heroDetail = nav.details[it.id]
        if (it.isShow) heroDetail = runCatching { nav.fetchDetail(it.id) }.getOrNull() ?: heroDetail
    }
    LaunchedEffect(hero?.key) {
        val h = hero ?: return@LaunchedEffect
        art.show(ArtSpec(h.item?.banner, ArtStyle.Home), Move.Art)
    }

    // focus comes back where it was: the card you opened, or the hero's buttons
    LaunchedEffect(Unit) {
        withFrameNanos { }
        val k = nav.homeFocus ?: "play"
        runCatching { fr(k).requestFocus() }.onFailure { runCatching { fr("play").requestFocus() } }
    }
    LaunchedEffect(nav.offline) {
        if (!nav.offline) { withFrameNanos { }; runCatching { fr(focus).requestFocus() } }
    }
    fun onFocus(k: String) { focus = k; nav.homeFocus = k }

    val heroPlay = hero?.item?.let { playTarget(it, heroDetail, home) }

    Box(Modifier.fillMaxSize()) {
        LogoMark(Modifier.offset(LEFT.px, 56.px))
        TopRight(nav, onUpdate, fr("settings"), fr("update"), down = fr(if (hero?.download != null) firstCardKey(rows, nav) else "play")) { onFocus(it) }

        // the hero's words: logo, meta line, story. They fade out and in on their own, apart from the art
        AnimatedContent(
            hero,
            transitionSpec = { fadeIn(Move.fade(Move.Focus), initialAlpha = 0f) togetherWith fadeOut(Move.fade(Move.Focus)) },
            contentKey = { it?.key },
            label = "hero",
        ) { h -> if (h != null) HeroWords(h, heroDetail?.takeIf { it.first.id == h.item?.id }?.first) }

        // Play and More info: gone while focus is down in the rows
        val buttons by animateFloatAsState(if (inRows || hero?.download != null) 0f else 1f, Move.fade(Move.Focus), label = "buttons")
        if (hero?.item != null) Row(
            Modifier.offset(LEFT.px, 444.px).graphicsLayer { alpha = buttons },
            horizontalArrangement = Arrangement.spacedBy(12.px), verticalAlignment = Alignment.CenterVertically,
        ) {
            val downKey = firstCardKey(rows, nav)
            val p = heroPlay
            PlayButton(
                p?.word ?: "Play", p?.detail, p?.progress, height = 58, padStart = 24, padEnd = 26,
                modifier = Modifier.focusProperties { up = fr("settings"); down = fr(downKey) },
                fr = fr("play"), onFocus = { if (it) onFocus("play") },
            ) { p?.let { t -> startPlay(ctx, scope, nav, t.item, fromStart = false) } }
            GlassButton(
                "More info", Icons.Info, height = 58,
                modifier = Modifier.focusProperties { up = fr("settings"); down = fr(downKey) },
                fr = fr("info"), onFocus = { if (it) onFocus("info") },
            ) { hero?.item?.let { nav.open(Screen.Detail(it.id)) } }
        }

        // the rows: the page slides so the focused row's title sits at y = 560
        val rowTops = remember(rows) { rowTops(rows) }
        val pageY by animateFloatAsState(if (inRows) -(rowTops[focusRow] - ROW0) * Px.screen else 0f, Move.slide(Move.Screen), label = "page")
        Box(Modifier.fillMaxSize().graphicsLayer { translationY = pageY }) {
            rows.forEachIndexed { r, row ->
                val gone by animateFloatAsState(if (inRows && r < focusRow) 0f else 1f, Move.fade(Move.Screen), label = "row")
                val titleColor by animateColorAsState(if (inRows) Pal.RowTitle else Pal.Ink, Move.fade(Move.Focus), label = "rowtitle")
                Column(Modifier.offset(LEFT.px, rowTops[r].px).graphicsLayer { alpha = gone }) {
                    Text(row.title, style = ts(22, 26, 700, titleColor, cond = true, track = -0.005f))
                    CardRow(
                        row, nav, focus, inRows, ::fr, ::onFocus,
                        up = if (r == 0) (if (hero?.download != null) "settings" else "play") else rows[r - 1].let { u -> u.cards[(nav.homeCols[u.key] ?: 0).coerceAtMost(u.cards.lastIndex)].key },
                        down = rows.getOrNull(r + 1)?.let { d -> d.cards[(nav.homeCols[d.key] ?: 0).coerceAtMost(d.cards.lastIndex)].key },
                    ) { c ->
                        when {
                            c.download != null -> nav.open(Screen.Pending(c.download.title))
                            row.key == "continue" -> startPlay(ctx, scope, nav, c.item!!, fromStart = false)
                            else -> nav.open(Screen.Detail(c.item!!.id))
                        }
                    }
                }
            }
            if (rows.isEmpty()) Text(
                "Nothing in the library yet. Add links at mint:8780.", style = ts(21, 30, 400, Pal.InkSoft),
                modifier = Modifier.offset(LEFT.px, ROW0.px),
            )
        }
    }
}

private fun firstCardKey(rows: List<HomeRow>, nav: Nav): String =
    rows.firstOrNull()?.let { r -> r.cards[(nav.homeCols[r.key] ?: 0).coerceAtMost(r.cards.lastIndex)].key } ?: "play"

private fun homeRows(home: Home): List<HomeRow> {
    val out = mutableListOf<HomeRow>()
    if (home.cont.isNotEmpty()) out += HomeRow("continue", "Continue watching", true, home.cont.map { Card("continue/${it.id}", it, null) })
    // a title still downloading comes first in Recently added, from the moment it starts
    val recent = home.downloads.map { Card("recent/dl:${it.title}", null, it) } + home.recent.map { Card("recent/${it.id}", it, null) }
    if (recent.isNotEmpty()) out += HomeRow("recent", "Recently added", false, recent)
    return out
}

private fun rowTops(rows: List<HomeRow>): List<Int> {
    var y = ROW0
    return rows.map { r ->
        val top = y
        y += 26 + (TITLE_TO_CARDS - 26) + (if (r.wide) WIDE_BLOCK else POSTER_BLOCK + 10 + 6) + ROW_GAP
        top
    }
}

/** Settings top-right in the shared control surface, and Update beside it when mint has a newer build. */
@Composable
private fun TopRight(nav: Nav, onUpdate: () -> Unit, settingsFr: FocusRequester, updateFr: FocusRequester, down: FocusRequester, onFocus: (String) -> Unit) {
    Box(Modifier.fillMaxWidth()) {
        Row(Modifier.align(Alignment.TopEnd).padding(end = 120.px, top = 50.px), horizontalArrangement = Arrangement.spacedBy(12.px)) {
            if (nav.update != null) GlassButton(
                "Update", null, height = 44, textSize = 18, padStart = 18, padEnd = 18,
                modifier = Modifier.focusProperties { this.down = down }, fr = updateFr, onFocus = { if (it) onFocus("update") }, onClick = onUpdate,
            )
            GlassButton(
                "Settings", Icons.Settings, height = 44, textSize = 18, iconSize = 20, iconGap = 10, padStart = 14, padEnd = 18,
                modifier = Modifier.focusProperties { this.down = down }, fr = settingsFr, onFocus = { if (it) onFocus("settings") },
            ) { nav.open(Screen.Settings) }
        }
    }
}

/** The hero's words for one title, at the board's fixed places. */
@Composable
private fun HeroWords(h: Hero, detail: Item?) {
    Box(Modifier.fillMaxSize()) {
        val it = h.item
        val name = it?.displayTitle ?: h.download!!.title
        Box(Modifier.offset(LEFT.px, 150.px).size(520.px, 130.px)) {
            TitleArt(it?.logo, name, 520, 130, Alignment.TopStart, typeSize = 60, typeLine = 64)
        }
        if (it != null) {
            val facts = if (it.isShow) {
                val seasons = (detail ?: it).seasons.size.takeIf { n -> n > 0 }
                listOfNotNull(
                    it.displayYear.takeIf { y -> y > 0 }?.toString(), it.meta?.genres?.firstOrNull(),
                    seasons?.let { n -> if (n == 1) "1 Season" else "$n Seasons" } ?: it.count.takeIf { n -> n > 0 }?.let { n -> "$n episodes" },
                )
            } else listOfNotNull(
                it.displayYear.takeIf { y -> y > 0 }?.toString(), it.meta?.genres?.firstOrNull(),
                runtimeOf(it)?.let(::span), it.meta?.cert?.ifEmpty { null },
            )
            DotLine(facts, ts(19, 24, 600, Pal.InkSoft, cond = true, track = 0.01f), modifier = Modifier.offset(LEFT.px, 318.px))
            it.meta?.overview?.ifEmpty { null }?.let { o ->
                Text(
                    o, style = ts(21, 30, 400, Pal.InkSoft), maxLines = 2, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.offset(LEFT.px, 356.px).width(660.px),
                )
            }
        } else {
            val d = h.download!!
            DotLine(
                listOfNotNull("Downloading", "${d.progress}%", etaText(d.eta)), ts(21, 28, 400, Pal.Muted, tabular = true),
                lead = ts(21, 28, 650, Pal.Ink), modifier = Modifier.offset(LEFT.px, 462.px),
            )
        }
    }
}

/**
 * A title's own logo art with its soft drop shadow, or its name set large when it has none. The logo is fitted
 * into [w] x [h] board pixels.
 */
@Composable
fun TitleArt(logo: String?, name: String, w: Int, h: Int, align: Alignment, typeSize: Int, typeLine: Int, modifier: Modifier = Modifier) {
    val ctx = LocalContext.current
    var failed by remember(logo) { mutableStateOf(logo == null) }
    Box(modifier.size(w.px, h.px), contentAlignment = align) {
        if (!failed) {
            val img by androidx.compose.runtime.produceState<LogoArt?>(Logos.cached(logo!!, w, h), logo) {
                value = Logos.get(ctx, logo, w, h).also { if (it == null) failed = true }
            }
            img?.let { l ->
                val pad = (l.pad / Px.screen).px
                androidx.compose.foundation.Image(
                    l.bitmap.asImageBitmap(), name,
                    Modifier.offset(x = -pad, y = if (align == Alignment.BottomStart) pad else -pad)
                        .requiredWidth((l.bitmap.width / Px.screen).px).height((l.bitmap.height / Px.screen).px),
                    contentScale = ContentScale.None,
                )
            }
        } else Text(
            name, maxLines = 2, overflow = TextOverflow.Ellipsis,
            style = ts(typeSize, typeLine, 800, cond = true, track = -0.025f).copy(shadow = Shadow(Pal.BaseDeep.copy(alpha = 0.6f), androidx.compose.ui.geometry.Offset(0f, 6 * Px.screen), 24 * Px.screen)),
        )
    }
}

/** One row of cards. Cards are 8 px apart; the row slides so the focused card stays at x = 120. */
@Composable
private fun CardRow(
    row: HomeRow, nav: Nav, focus: String, inRows: Boolean,
    fr: (String) -> FocusRequester, onFocus: (String) -> Unit, up: String, down: String?, onClick: (Card) -> Unit,
) {
    val col = (row.cards.indexOfFirst { it.key == focus }.takeIf { it >= 0 } ?: nav.homeCols[row.key] ?: 0).coerceAtMost(row.cards.lastIndex)
    val step = (if (row.wide) WIDE_W else POSTER_W) + 8
    val x by animateFloatAsState(-col * step * Px.screen, Move.slide(Move.Focus), label = "row-x")
    Row(
        Modifier.padding(top = (TITLE_TO_CARDS - 26).px).requiredWidth((row.cards.size * step).px).graphicsLayer { translationX = x },
        horizontalArrangement = Arrangement.spacedBy(8.px),
    ) {
        row.cards.forEachIndexed { i, c ->
            val lit by animateFloatAsState(if (!inRows || c.key == focus) 1f else 0.45f, Move.fade(Move.Focus), label = "lit")
            val keys = Modifier
                .focusProperties {
                    this.up = fr(up)
                    if (down != null) this.down = fr(down)
                    left = if (i > 0) fr(row.cards[i - 1].key) else FocusRequester.Cancel
                    right = if (i < row.cards.lastIndex) fr(row.cards[i + 1].key) else FocusRequester.Cancel
                }
            val focused = { f: Boolean -> if (f) { onFocus(c.key); nav.homeCols[row.key] = i } }
            Box(Modifier.graphicsLayer { alpha = lit }) {
                if (row.wide) WideCard(c, keys, fr(c.key), focused) { onClick(c) }
                else PosterCard(c, keys, fr(c.key), focused) { onClick(c) }
            }
        }
    }
}

/** Continue watching: 16:9 art, its progress on a track under it, then the name and how much is left. */
@Composable
private fun WideCard(c: Card, keys: Modifier, fr: FocusRequester, onFocus: (Boolean) -> Unit, onClick: () -> Unit) {
    val it = c.item!!
    Column(Modifier.width(WIDE_W.px)) {
        Box(
            keys.size(WIDE_W.px, WIDE_H.px).softShadow(ShadowKind.Card).clip(Corner).background(Pal.Raised)
                .control(fr, onFocus, onClick),
        ) {
            it.wideSmall?.let { u -> AsyncImage(u, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
        }
        val shown by animateFloatAsState(it.progress, Move.slide(Move.Art), label = "progress")
        Box(Modifier.padding(top = 10.px).fillMaxWidth().height(6.px).clip(androidx.compose.foundation.shape.RoundedCornerShape(3.px)).background(Pal.Ink.copy(alpha = 0.12f))) {
            Box(Modifier.fillMaxHeight().width(maxOf(12f, WIDE_W * shown).px).clip(androidx.compose.foundation.shape.RoundedCornerShape(3.px)).background(Pal.Accent))
        }
        Text(it.showTitle, style = ts(18, 22, 700, track = -0.01f), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 11.px))
        val left = (it.durMs - it.pos).takeIf { l -> it.pos > 0 && l > 0 }?.let { l -> leftWords(l) }
        val lead = if (it.isEpisode) "S${it.season} E${it.episode}" else it.displayYear.takeIf { y -> y > 0 }?.toString()
        Row(Modifier.padding(top = 2.px)) {
            if (lead != null) Text("$lead · ", style = ts(15, 20, 400, Pal.Muted, tabular = true), maxLines = 1)
            if (left != null) Text(left, style = ts(15, 20, 400, Pal.InkSoft, tabular = true), maxLines = 1)
        }
    }
}

/** Recently added: a poster, or a title still downloading with its progress under it. */
@Composable
private fun PosterCard(c: Card, keys: Modifier, fr: FocusRequester, onFocus: (Boolean) -> Unit, onClick: () -> Unit) {
    Column(Modifier.width(POSTER_W.px)) {
        Box(
            keys.size(POSTER_W.px, POSTER_H.px).softShadow(ShadowKind.Card).clip(Corner).background(Pal.Raised)
                .control(fr, onFocus, onClick),
            contentAlignment = Alignment.BottomStart,
        ) {
            val poster = c.item?.poster
            if (poster != null) AsyncImage(poster, c.item.displayTitle, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
            else Text(
                c.item?.displayTitle ?: c.download!!.title, style = ts(18, 22, 700), maxLines = 4, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(14.px),
            )
        }
        c.download?.let { d ->
            val shown by animateFloatAsState(d.progress / 100f, tween(if (Move.reduced) 0 else 4800), label = "download")
            Box(Modifier.padding(top = 10.px).fillMaxWidth().height(6.px).clip(androidx.compose.foundation.shape.RoundedCornerShape(3.px)).background(Pal.Ink.copy(alpha = 0.12f))) {
                Box(Modifier.fillMaxHeight().fillMaxWidth(shown.coerceIn(0.04f, 1f)).background(Pal.Accent))
            }
        }
    }
}

/** What Play on a title starts, and how its button reads: "Play | S2 E1", "Resume | 2 h 4 m left". */
class PlayTarget(val item: Item, val word: String, val detail: String?, val progress: Float?)

fun playTarget(title: Item, detail: Pair<Item, Item?>?, home: Home?): PlayTarget? {
    if (title.isShow) {
        val ep = home?.cont?.firstOrNull { it.showId == title.id } ?: detail?.second
            ?: detail?.first?.seasons?.firstOrNull()?.episodes?.firstOrNull() ?: return null
        val started = ep.pos > 0 && !ep.watched && ep.durMs > ep.pos
        return PlayTarget(ep, if (started) "Resume" else "Play", "S${ep.season} E${ep.episode}", if (started) ep.progress else null)
    }
    val started = title.pos > 0 && !title.watched && title.durMs > title.pos
    return when {
        started -> PlayTarget(title, "Resume", leftWords(title.durMs - title.pos), title.progress)
        title.watched -> PlayTarget(title, "Play", "Watched", null)
        else -> PlayTarget(title, "Play", runtimeOf(title)?.let(::span), null)
    }
}

/** A film's or episode's length in ms, from the file, else TMDB. */
fun runtimeOf(it: Item): Long? = it.durMs.takeIf { d -> d > 0 } ?: it.meta?.runtime?.takeIf { m -> m > 0 }?.let { m -> m * 60_000L }

/** "2 h 4 m", "44 m". */
fun span(ms: Long): String {
    val m = ((ms + 30_000) / 60_000).toInt()
    return if (m >= 60) "${m / 60} h ${m % 60} m" else "$m m"
}

/** "44 m left", "2 h 4 m left". */
fun leftWords(ms: Long): String = if (ms < 60_000) "less than a minute left" else span(ms) + " left"

/** The engine's ETA ("1h05m", "12m30s", "40s") the way the boards say it: "about 12 m left". */
fun etaText(eta: String): String? {
    if (eta.isBlank()) return null
    val h = Regex("(\\d+)h").find(eta)?.groupValues?.get(1)?.toLong() ?: 0
    val m = Regex("(\\d+)m").find(eta)?.groupValues?.get(1)?.toLong() ?: 0
    val total = h * 60 + m
    return if (total < 1) "less than a minute left" else "about " + span(total * 60_000) + " left"
}

/**
 * Into the player: the screen fades to black (200 ms), then the player's own loading screen fades in over it.
 * Coming back, the screen fades up from black (MainActivity.onResume).
 */
fun startPlay(ctx: Context, scope: CoroutineScope, nav: Nav, it: Item, fromStart: Boolean, logo: String? = null, queue: List<Item>? = null) {
    if (nav.inPlayer) return
    nav.inPlayer = true
    scope.launch {
        nav.black.animateTo(1f, Move.fade(Move.Black))
        when {
            queue != null -> PlayerActivity.play(ctx, queue, queue.indexOfFirst { q -> q.id == it.id }.coerceAtLeast(0), fromStart, logo)
            it.isEpisode -> playEpisode(ctx, it, fromStart)
            else -> PlayerActivity.play(ctx, listOf(it), 0, fromStart)
        }
        (ctx as? Activity)?.overridePendingTransition(R.anim.player_in, R.anim.hold)
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

/** "22:42" (or "10:42 PM", per the TV's setting): now plus what's left. */
fun endsAt(ctx: Context, leftMs: Long): String =
    android.text.format.DateFormat.getTimeFormat(ctx).format(java.util.Date(System.currentTimeMillis() + leftMs))

fun gb(bytes: Long): String = if (bytes >= 1e9) "%.1f GB".format(bytes / 1e9) else "%d MB".format((bytes / 1e6).toInt())
