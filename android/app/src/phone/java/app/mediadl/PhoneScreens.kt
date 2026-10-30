package app.mediadl

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.input.ImeAction
import kotlinx.coroutines.delay
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import kotlinx.coroutines.launch
import org.json.JSONObject

// ---------- shared pieces ----------

private val BtnH = 44.dp // the play capsule and the round buttons share one height

/** White capsule: play icon, then progress + time left when resuming, else "Play". */
@Composable
fun PlayCapsule(item: Item?, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val resume = item != null && item.pos > 0 && !item.watched && item.durMs > item.pos
    Row(
        modifier.height(BtnH).clip(RoundedCornerShape(BtnH / 2)).background(Color.White).clickable(onClick = onClick)
            .padding(start = 12.dp, end = 16.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp, Alignment.CenterHorizontally),
    ) {
        Icon(AppIcons.Play, "Play", tint = PBg, modifier = Modifier.size(24.dp))
        if (resume) {
            Text(hm(item!!.durMs - item.pos), color = PBg, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
        } else {
            Text("Play", color = PBg, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
        }
    }
}

@Composable
fun RoundIcon(icon: ImageVector, label: String, on: Boolean = false, onClick: () -> Unit) {
    Box(
        Modifier.size(BtnH).clip(CircleShape).background(if (on) Color.White else Color(0x29FFFFFF)).clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) { Icon(icon, label, tint = if (on) PBg else PText, modifier = Modifier.size(22.dp)) }
}

/** "4K · Dolby Vision · DTS-HD MA": only the formats that are a reason to pick it. */
fun formats(i: Info?): List<String> {
    if (i == null) return emptyList()
    val out = mutableListOf<String>()
    if (i.res == "4K") out += "4K"
    when {
        i.hdr.startsWith("Dolby Vision") -> out += "Dolby Vision"
        i.hdr.isNotEmpty() -> out += i.hdr
    }
    val a = i.audio
    when {
        a.contains("Atmos") -> out += "Dolby Atmos"
        a.startsWith("DTS:X") -> out += "DTS:X"
        a.startsWith("DTS-HD MA") -> out += "DTS-HD MA"
        a.startsWith("TrueHD") -> out += "TrueHD"
    }
    return out
}

@Composable
private fun SectionTitle(text: String) {
    Text(text, fontSize = 20.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(start = 16.dp, top = 26.dp, bottom = 12.dp))
}

// ---------- home ----------

@Composable
fun PhoneHome(nav: Nav) {
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
    val home = nav.home
    if (home == null) {
        if (error != null) ErrorView(error!!, onRetry = { error = null; nav.refresh++ }) else Loading()
        return
    }
    val featured = home.cont.firstOrNull() ?: home.recent.firstOrNull()
    val cont = if (featured != null && home.cont.firstOrNull() == featured) home.cont.drop(1) else home.cont
    val offline = remember(nav.refresh) { Offline.items().filter { Offline.state(it.id)?.done == true } }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
        item("top") {
            Text("Home", fontSize = 28.sp, fontWeight = FontWeight.Bold, modifier = Modifier.statusBarsPadding().padding(start = 16.dp, top = 10.dp, bottom = 14.dp))
        }
        if (featured != null) item("featured") {
            Featured(featured,
                onPlay = { openItem(ctx, scope, nav, featured, resume = true) },
                onInfo = { nav.open(Screen.Detail(featured.showId.ifEmpty { featured.id })) })
        }
        if (cont.isNotEmpty()) item("cont") {
            SectionTitle("Continue Watching")
            LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                items(cont, key = { it.id }) { WideTile(it) { openItem(ctx, scope, nav, it, resume = true) } }
            }
        }
        if (offline.isNotEmpty()) item("offline") {
            SectionTitle("On This Phone")
            LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                items(offline, key = { it.id }) { WideTile(it) { PlayerActivity.play(ctx, listOf(it), 0) } }
            }
        }
        listOf("Recently Added" to home.recent, "Films" to home.movies, "Shows" to home.shows).forEach { (title, list) ->
            if (list.isNotEmpty()) item(title) {
                SectionTitle(title)
                LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    items(list, key = { it.id }) { Poster(it) { nav.open(Screen.Detail(it.id)) } }
                }
            }
        }
    }
}

@Composable
private fun Featured(item: Item, onPlay: () -> Unit, onInfo: () -> Unit) {
    Box(Modifier.padding(horizontal = 16.dp).fillMaxWidth().height(470.dp).clip(RoundedCornerShape(18.dp)).background(Color(0xFF1A1A1D)).clickable(onClick = onInfo)) {
        AsyncImage(Api.img(item.meta?.poster, "w780"), null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(0.4f to Color.Transparent, 0.66f to Color(0x8C000000), 1f to Color(0xEB000000))))
        Column(Modifier.align(Alignment.BottomCenter).padding(18.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            val logo = item.logo
            if (logo != null) AsyncImage(logo, item.showTitle, Modifier.widthIn(max = 230.dp).height(80.dp), contentScale = ContentScale.Fit)
            else Text(item.showTitle, fontSize = 28.sp, fontWeight = FontWeight.Bold, textAlign = TextAlign.Center)
            Spacer(Modifier.height(10.dp))
            val sub = if (item.isEpisode) "S${item.season}, E${item.episode} · ${item.title}" else item.meta?.genres?.take(2)?.joinToString(" · ").orEmpty()
            if (sub.isNotEmpty()) Text(sub, color = PDim, fontSize = 13.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Spacer(Modifier.height(14.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                PlayCapsule(item, onClick = onPlay)
                RoundIcon(AppIcons.InfoOutline, "Details", onClick = onInfo)
            }
        }
    }
}

@Composable
private fun WideTile(item: Item, onClick: () -> Unit) {
    Box(Modifier.width(236.dp).height(133.dp).clip(RoundedCornerShape(10.dp)).background(Color(0xFF1A1A1D)).clickable(onClick = onClick)) {
        AsyncImage(item.wide, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(0.38f to Color.Transparent, 1f to Color(0xD1000000))))
        Column(Modifier.align(Alignment.BottomStart).padding(start = 12.dp, end = 12.dp, bottom = 14.dp)) {
            Text(item.showTitle, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val left = item.durMs - item.pos
            val sub = when {
                item.isEpisode && item.pos > 0 -> "S${item.season}, E${item.episode} · ${hm(left)} left"
                item.isEpisode -> "S${item.season}, E${item.episode}"
                item.pos > 0 && left > 0 -> "${hm(left)} left"
                else -> ""
            }
            if (sub.isNotEmpty()) Text(sub, color = PDim, fontSize = 12.sp, fontWeight = FontWeight.Medium, maxLines = 1)
        }
        if (item.progress > 0f) {
            Box(Modifier.align(Alignment.BottomStart).padding(horizontal = 12.dp, vertical = 7.dp).fillMaxWidth().height(3.dp).clip(CircleShape).background(Color(0x4DFFFFFF))) {
                Box(Modifier.fillMaxWidth(item.progress).height(3.dp).background(Color.White))
            }
        }
    }
}

@Composable
private fun Poster(item: Item, onClick: () -> Unit) {
    Box(Modifier.width(118.dp).height(177.dp).clip(RoundedCornerShape(8.dp)).background(Color(0xFF1A1A1D)).clickable(onClick = onClick)) {
        val p = item.poster
        if (p != null) AsyncImage(p, item.displayTitle, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        else Text(item.displayTitle, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(10.dp).align(Alignment.BottomStart))
    }
}

// ---------- title page: one layout for films and shows ----------

@Composable
fun PhoneDetail(nav: Nav, id: String) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var data by remember(id) { mutableStateOf<JSONObject?>(null) }
    var error by remember(id) { mutableStateOf<String?>(null) }
    suspend fun load() {
        try { data = Api.get("/api/v1/item/$id") } catch (e: Exception) { if (data == null) error = e.message }
    }
    LaunchedEffect(id, nav.refresh) { load() }
    val d = data
    if (d == null) {
        if (error != null) ErrorView(error!!, onRetry = { error = null; scope.launch { load() } }) else Loading()
        return
    }
    val main = remember(d) { Item.parse(d.getJSONObject("item")) }
    val upNext = remember(d) { d.optJSONObject("up_next")?.let(Item::parse) }
    val show = main.isShow
    val target = if (show || main.type == "season") upNext else main
    val play = { fromStart: Boolean ->
        if (target != null) {
            if (target.isEpisode) scope.launch { playEpisode(ctx, target, fromStart) }
            else PlayerActivity.play(ctx, listOf(target), 0, fromStart)
        }
        Unit
    }
    val allWatched = if (show) main.unseen == 0 else main.watched
    val setWatched = { w: Boolean ->
        scope.launch {
            runCatching { Api.post("/api/v1/watched", JSONObject().put("id", main.id).put("watched", w)) }
            load()
        }
        Unit
    }

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(bottom = 32.dp)) {
        Box(Modifier.fillMaxWidth().height(280.dp)) {
            AsyncImage(main.backdrop, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
            Box(Modifier.fillMaxSize().background(Brush.verticalGradient(0f to Color(0x73000000), 0.25f to Color.Transparent, 0.6f to Color(0x590B0B0D), 1f to PBg)))
            Box(
                Modifier.statusBarsPadding().padding(12.dp).size(40.dp).clip(CircleShape).background(Color(0x59000000)).clickable { nav.back() },
                contentAlignment = Alignment.Center,
            ) { Icon(AppIcons.Back, "Back", tint = PText, modifier = Modifier.size(24.dp)) }
        }
        Column(Modifier.offset(y = (-52).dp).padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            val logo = main.logo
            if (logo != null) AsyncImage(logo, main.displayTitle, Modifier.widthIn(max = 240.dp).height(84.dp), contentScale = ContentScale.Fit, alignment = Alignment.BottomStart)
            else Text(main.displayTitle, fontSize = 30.sp, fontWeight = FontWeight.Bold, lineHeight = 34.sp)

            // what it is, then when, how long and the formats worth knowing; the rating is plain text
            val kind = listOf(if (show) "Show" else "Film") + main.meta?.genres?.take(2).orEmpty() +
                listOfNotNull(main.meta?.cert?.ifEmpty { null })
            Text(kind.joinToString(" · "), color = PDim, fontSize = 14.sp, fontWeight = FontWeight.Medium)
            val length = when {
                show -> if (main.seasons.size > 1) "${main.seasons.size} seasons" else "${main.count} episodes"
                main.durMs > 0 -> hm(main.durMs)
                (main.meta?.runtime ?: 0) > 0 -> hm(main.meta!!.runtime * 60000L)
                else -> null
            }
            val facts = listOfNotNull(main.displayYear.takeIf { it > 0 }?.toString(), length) + formats(main.info ?: upNext?.info)
            if (facts.isNotEmpty()) Text(facts.joinToString(" · "), color = PFaint, fontSize = 13.sp, fontWeight = FontWeight.Medium)

            val syn = if (show && target != null) buildAnnotatedString {
                withStyle(SpanStyle(fontWeight = FontWeight.SemiBold, color = PText)) { append("S${target.season}, E${target.episode} · ${target.title}: ") }
                append(target.meta?.overview.orEmpty())
            } else buildAnnotatedString { append(main.meta?.overview.orEmpty()) }
            if (syn.isNotEmpty()) Text(syn, color = PDim, fontSize = 15.sp, lineHeight = 22.sp, maxLines = 3, overflow = TextOverflow.Ellipsis)

            Row(Modifier.padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                PlayCapsule(target) { play(false) }
                if (target != null && target.pos > 0 && !target.watched) RoundIcon(AppIcons.Replay, "Start over") { play(true) }
                RoundIcon(AppIcons.Check, if (allWatched) "Mark unwatched" else "Mark watched", on = allWatched) { setWatched(!allWatched) }
            }

            if (show) Episodes(main, upNext) else Cast(main)
        }
    }
}

@Composable
private fun Cast(main: Item) {
    val cast = main.meta?.cast.orEmpty()
    if (cast.isEmpty()) return
    Text("Cast", fontSize = 18.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(top = 16.dp, bottom = 4.dp))
    LazyRow(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
        items(cast.take(12)) { p ->
            Column(Modifier.width(72.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                AsyncImage(Api.img(p.photo.ifEmpty { null }, "w185"), p.name, Modifier.size(64.dp).clip(CircleShape).background(Color(0xFF1A1A1D)), contentScale = ContentScale.Crop)
                Text(p.name, fontSize = 11.sp, fontWeight = FontWeight.SemiBold, textAlign = TextAlign.Center, lineHeight = 14.sp, maxLines = 2, modifier = Modifier.padding(top = 6.dp))
                Text(p.role, color = PFaint, fontSize = 11.sp, textAlign = TextAlign.Center, lineHeight = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

@Composable
private fun Episodes(main: Item, upNext: Item?) {
    val ctx = LocalContext.current
    if (main.seasons.isEmpty()) return
    var season by remember(main.id) {
        mutableIntStateOf(main.seasons.indexOfFirst { s -> upNext != null && s.episodes.any { it.id == upNext.id } }.coerceAtLeast(0))
    }
    var menu by remember { mutableStateOf(false) }
    Box(Modifier.padding(top = 16.dp)) {
        Row(
            Modifier.clickable(enabled = main.seasons.size > 1) { menu = true },
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(main.seasons[season].title, fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
            if (main.seasons.size > 1) Icon(AppIcons.ExpandMore, null, tint = PFaint, modifier = Modifier.size(24.dp))
        }
        DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
            main.seasons.forEachIndexed { i, s -> DropdownMenuItem(text = { Text(s.title) }, onClick = { season = i; menu = false }) }
        }
    }
    val queue = episodeQueue(main)
    main.seasons[season].episodes.forEach { ep ->
        EpisodeRow(ep, 128.dp) { PlayerActivity.play(ctx, queue, queue.indexOfFirst { it.id == ep.id }.coerceAtLeast(0)) }
    }
}

@Composable
private fun EpisodeRow(ep: Item, thumb: Dp, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.width(thumb).height(thumb * 9 / 16).clip(RoundedCornerShape(8.dp)).background(Color(0xFF1A1A1D))) {
            AsyncImage(ep.wide, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
            if (ep.progress > 0f && !ep.watched) {
                Box(Modifier.align(Alignment.BottomStart).padding(6.dp).fillMaxWidth().height(3.dp).clip(CircleShape).background(Color(0x59FFFFFF))) {
                    Box(Modifier.fillMaxWidth(ep.progress).height(3.dp).background(Color.White))
                }
            }
        }
        Column(Modifier.padding(start = 12.dp).weight(1f)) {
            Text("${ep.episode}. ${ep.title}", fontSize = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis, lineHeight = 19.sp)
            val sub = when {
                ep.watched -> "Watched"
                ep.pos > 0 && ep.durMs > ep.pos -> "${hm(ep.durMs - ep.pos)} left"
                ep.durMs > 0 -> hm(ep.durMs)
                else -> ""
            }
            if (sub.isNotEmpty()) Text(sub, color = PFaint, fontSize = 12.sp, fontWeight = FontWeight.Medium, modifier = Modifier.padding(top = 3.dp))
        }
        if (ep.watched) Icon(AppIcons.Check, "Watched", tint = PFaint, modifier = Modifier.size(20.dp))
    }
}

// ---------- films, shows, search: clean poster grids under a big title ----------

@Composable
fun PhoneLibrary(nav: Nav, shows: Boolean) {
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
    PosterGrid(nav, all) {
        Text(if (shows) "Shows" else "Films", fontSize = 28.sp, fontWeight = FontWeight.Bold,
            modifier = Modifier.statusBarsPadding().padding(top = 10.dp, bottom = 14.dp))
    }
}

@Composable
fun PhoneSearch(nav: Nav) {
    var q by remember { mutableStateOf(nav.query) }
    var results by remember { mutableStateOf<List<Item>>(emptyList()) }
    val fr = remember { FocusRequester() }
    LaunchedEffect(Unit) { runCatching { fr.requestFocus() } }
    LaunchedEffect(q) {
        nav.query = q
        if (q.isBlank()) { results = emptyList(); return@LaunchedEffect }
        delay(250)
        results = runCatching { Api.items("/api/v1/search?q=" + java.net.URLEncoder.encode(q, "UTF-8")) }.getOrDefault(results)
    }
    PosterGrid(nav, results) {
        Column(Modifier.statusBarsPadding().padding(top = 10.dp, bottom = 14.dp)) {
            Text("Search", fontSize = 28.sp, fontWeight = FontWeight.Bold, modifier = Modifier.padding(bottom = 14.dp))
            Row(
                Modifier.fillMaxWidth().height(44.dp).clip(RoundedCornerShape(12.dp)).background(Color(0x1FFFFFFF)).padding(horizontal = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(AppIcons.Search, null, tint = PFaint, modifier = Modifier.size(22.dp))
                Box(Modifier.padding(start = 10.dp).weight(1f)) {
                    if (q.isEmpty()) Text("Films, shows, people", color = PFaint, fontSize = 16.sp)
                    BasicTextField(
                        q, { q = it }, singleLine = true,
                        textStyle = TextStyle(fontFamily = Inter, fontSize = 16.sp, color = PText),
                        cursorBrush = SolidColor(Color.White),
                        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                        modifier = Modifier.fillMaxWidth().focusRequester(fr),
                    )
                }
            }
            if (q.isNotBlank() && results.isEmpty()) Text("No matches for “$q”", color = PFaint, fontSize = 14.sp, modifier = Modifier.padding(top = 18.dp))
        }
    }
}

@Composable
private fun PosterGrid(nav: Nav, items: List<Item>, header: @Composable () -> Unit) {
    LazyVerticalGrid(
        GridCells.Fixed(3), Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item(span = { GridItemSpan(maxLineSpan) }) { header() }
        items(items, key = { it.id }) { it ->
            Box(Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(RoundedCornerShape(8.dp)).background(Color(0xFF1A1A1D))
                .clickable { nav.open(Screen.Detail(if (it.isEpisode) it.showId.ifEmpty { it.id } else it.id)) }) {
                val p = it.poster
                if (p != null) AsyncImage(p, it.displayTitle, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
                else Text(it.displayTitle, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.align(Alignment.BottomStart).padding(8.dp))
                if (it.progress > 0f && !it.watched) {
                    Box(Modifier.align(Alignment.BottomStart).padding(6.dp).fillMaxWidth().height(3.dp).clip(CircleShape).background(Color(0x59FFFFFF))) {
                        Box(Modifier.fillMaxWidth(it.progress).height(3.dp).background(Color.White))
                    }
                }
            }
        }
    }
}
