package app.mediacore.phone

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
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
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject

/** A film's or a show's page: the same block for both; films end with the cast, shows with episodes. */
@Composable
fun TitleScreen(nav: Nav, id: String) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var data by remember(id) { mutableStateOf(nav.titles[id]) }
    var error by remember(id) { mutableStateOf<String?>(null) }
    suspend fun load() {
        try {
            val j = Api.get("/api/v1/item/$id")
            nav.titles[id] = j
            data = j
        } catch (e: Exception) {
            if (data == null) error = e.message
        }
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
    var details by remember(id) { mutableStateOf(false) }
    val resumable = target != null && target.pos > 0 && !target.watched && target.durMs > target.pos

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).navigationBarsPadding().padding(bottom = 24.dp)) {
        Box(Modifier.fillMaxWidth()) {
            Hero(main.backdrop)
            Box(
                Modifier.statusBarsPadding().padding(start = 8.dp, top = 8.dp).size(ButtonHeight).tap { nav.back() },
                contentAlignment = Alignment.Center,
            ) {
                Box(Modifier.size(40.dp).clip(CardShape).background(Base.copy(alpha = 0.72f)), contentAlignment = Alignment.Center) {
                    Icon(AppIcons.Back, "Back", tint = Ink, modifier = Modifier.size(24.dp))
                }
            }
            Column(Modifier.padding(top = heroHeight() - HeroOverlap).padding(horizontal = Side)) {
                TitleMark(main.logo, main.displayTitle)
                Text(
                    (listOf(if (show) "Show" else "Film") + main.meta?.genres.orEmpty().take(2) + listOfNotNull(main.meta?.cert?.ifEmpty { null })).joinToString(" · "),
                    style = Type.Meta, color = Muted, modifier = Modifier.padding(top = 12.dp),
                )
                val ep = if (show) target else null
                if (ep != null) Text("S${ep.season}, E${ep.episode} · ${ep.title}", style = Type.Label, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 12.dp))
                val story = ep?.meta?.overview?.ifEmpty { null } ?: main.meta?.overview.orEmpty()
                if (story.isNotEmpty()) Text(story, style = Type.Body, color = Muted, maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = if (ep != null) 4.dp else 12.dp))
                val len = (target ?: main).let { t -> t.durMs.takeIf { it > 0 } ?: ((t.meta?.runtime ?: 0) * 60000L) }
                val facts = listOfNotNull(if (ep == null) main.displayYear.takeIf { it > 0 }?.toString() else null, len.takeIf { it > 0 }?.let(::hm)) + formats(target?.info ?: main.info)
                if (facts.isNotEmpty()) Text(facts.joinToString(" · "), style = Type.Meta, color = Muted, modifier = Modifier.padding(top = 8.dp))
                Row(Modifier.padding(top = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                    PlayButton(playLabel(target), Modifier.weight(1f)) { play(false) }
                    if (resumable) ToolButton(AppIcons.Replay, "Start over") { play(true) }
                    ToolButton(AppIcons.Check, if (allWatched) "Mark unwatched" else "Mark watched", on = allWatched, tint = Muted) { setWatched(!allWatched) }
                    if (target != null) DownloadButton(target)
                    ToolButton(AppIcons.Info, "Details") { details = true }
                }
            }
        }
        if (show) Episodes(main, upNext) else Cast(main.meta?.cast.orEmpty())
    }
    if (details) DetailsPage(target ?: main) { details = false }
}

/** Saves what Play would start for watching with no connection. Downloaded or under way, it offers to take it back. */
@Composable
private fun DownloadButton(item: Item) {
    val scope = rememberCoroutineScope()
    var state by remember(item.id) { mutableStateOf<Offline.State?>(null) }
    var menu by remember(item.id) { mutableStateOf(false) }
    var asked by remember(item.id) { mutableIntStateOf(0) } // a press wakes the watcher below
    LaunchedEffect(item.id, asked) {
        while (true) {
            val s = withContext(Dispatchers.IO) { Offline.state(item.id) }
            state = s
            if (s == null || s.done) break
            delay(1000)
        }
    }
    val s = state
    Box {
        Box(contentAlignment = Alignment.BottomStart) {
            ToolButton(if (s?.done == true) AppIcons.Downloaded else AppIcons.Download, if (s?.done == true) "Downloaded" else "Download") {
                if (s == null) scope.launch { Offline.download(item); asked++ } else menu = true
            }
            if (s != null && !s.done) Box(Modifier.padding(horizontal = 8.dp, vertical = 6.dp).width(32.dp * s.progress.coerceIn(0.05f, 1f)).height(2.dp).background(Ink))
        }
        DropdownMenu(menu, { menu = false }, shape = CardShape, containerColor = Raised, tonalElevation = 0.dp, shadowElevation = 0.dp) {
            DropdownMenuItem(
                text = { Text(if (s?.done == true) "Remove download" else "Cancel download", style = Type.Label) },
                onClick = { menu = false; Offline.remove(item.id); asked++ },
            )
        }
    }
}

@Composable
private fun Episodes(main: Item, upNext: Item?) {
    if (main.seasons.isEmpty()) return
    val ctx = LocalContext.current
    var season by remember(main.id) {
        mutableIntStateOf(main.seasons.indexOfFirst { s -> upNext != null && s.episodes.any { it.id == upNext.id } }.coerceAtLeast(0))
    }
    Column(Modifier.padding(top = 24.dp)) {
        Row(Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = Side), horizontalArrangement = Arrangement.spacedBy(24.dp)) {
            main.seasons.forEachIndexed { i, s ->
                Column(Modifier.width(androidx.compose.foundation.layout.IntrinsicSize.Max).tap { season = i }) {
                    Text(s.title, style = Type.Label, color = if (i == season) Ink else Muted, maxLines = 1, modifier = Modifier.padding(vertical = 8.dp))
                    Spacer(Modifier.fillMaxWidth().height(2.dp).background(if (i == season) Ink else Base))
                }
            }
        }
        Spacer(Modifier.height(8.dp))
        val queue = remember(main) { episodeQueue(main) }
        main.seasons.getOrNull(season)?.episodes.orEmpty().forEach { ep ->
            EpisodeRow(ep) { PlayerActivity.play(ctx, queue, queue.indexOfFirst { it.id == ep.id }.coerceAtLeast(0)) }
        }
    }
}

/** Number and name, and how long; the story stays with the title block. */
@Composable
private fun EpisodeRow(ep: Item, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().tap(onClick = onClick).padding(horizontal = Side, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.width(128.dp).aspectRatio(16f / 9f).clip(CardShape).background(Raised)) {
            AsyncImage(ep.wide, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
            if (!ep.watched) CardProgress(ep.progress)
        }
        Column(Modifier.padding(start = 12.dp).weight(1f)) {
            Text("${ep.episode}. ${ep.title}", style = Type.Label, maxLines = 2, overflow = TextOverflow.Ellipsis)
            val sub = when {
                ep.watched -> "Watched"
                ep.pos > 0 && ep.durMs > ep.pos -> "${hm(ep.durMs - ep.pos)} left"
                ep.durMs > 0 -> hm(ep.durMs)
                else -> ""
            }
            if (sub.isNotEmpty()) Text(sub, style = Type.Meta, color = Muted, modifier = Modifier.padding(top = 4.dp))
        }
    }
}

/** Square faces with names under them. */
@Composable
private fun Cast(cast: List<Person>) {
    if (cast.isEmpty()) return
    Column(Modifier.padding(top = 24.dp)) {
        RowTitle("Cast", Modifier.padding(start = Side))
        LazyRow(contentPadding = PaddingValues(horizontal = Side), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            items(cast.take(12)) { p ->
                Column(Modifier.width(88.dp)) {
                    Box(Modifier.size(88.dp).clip(CardShape).background(Raised), contentAlignment = Alignment.Center) {
                        val photo = Api.img(p.photo.ifEmpty { null }, "w185")
                        if (photo != null) AsyncImage(photo, p.name, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
                        else Text(p.name.split(" ").mapNotNull { it.firstOrNull()?.toString() }.take(2).joinToString(""), style = Type.Label, color = Muted)
                    }
                    Text(p.name, style = Type.Meta, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
                    if (p.role.isNotEmpty()) Text(p.role, style = Type.Meta, color = Muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}

// ---------- details: the numbers, one tap away ----------

@Composable
private fun DetailsPage(it: Item, onClose: () -> Unit) {
    BackHandler(onBack = onClose)
    Box(Modifier.fillMaxSize().background(Base)) {
        FadeIn {
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).statusBarsPadding().navigationBarsPadding().padding(bottom = 24.dp)) {
                Box(Modifier.padding(start = 8.dp, top = 8.dp).size(ButtonHeight).tap(onClick = onClose), contentAlignment = Alignment.Center) {
                    Icon(AppIcons.Close, "Close", tint = Ink, modifier = Modifier.size(24.dp))
                }
                Column(Modifier.padding(horizontal = Side)) {
                    Text("Details", style = Type.Title, modifier = Modifier.padding(top = 8.dp))
                    if (it.isEpisode) Text("S${it.season}, E${it.episode} · ${it.title}", style = Type.Meta, color = Muted, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 4.dp))
                    val i = it.info
                    val raw = it.raw.optJSONObject("info")
                    val w = raw?.optInt("w") ?: 0
                    val h = raw?.optInt("h") ?: 0
                    Row(Modifier.fillMaxWidth().padding(top = 24.dp)) {
                        i?.res?.ifEmpty { null }?.let { r -> KeyFigure(r, qualityName(r)) }
                        i?.mbps?.takeIf { m -> m > 0 }?.let { m -> KeyFigure("%.1f".format(m), "Mb/s") }
                        i?.fps?.takeIf { f -> f > 0 }?.let { f -> KeyFigure("%.3f".format(f).replace(".000", ""), "fps") }
                    }
                    SpecGroup(
                        "Picture",
                        listOfNotNull(
                            ("Resolution" to "$w × $h").takeIf { w > 0 && h > 0 },
                            i?.vcodec?.ifEmpty { null }?.let { v -> "Video" to codecName(v) },
                            i?.hdr?.ifEmpty { null }?.let { d -> "HDR" to d },
                        ),
                    )
                    // English first, the way you'd want to read it
                    val tracks = i?.tracks.orEmpty().ifEmpty { listOfNotNull(i?.audio?.ifEmpty { null }) }.map(::audioLine)
                        .sortedByDescending { t -> t.startsWith("English") }
                        .map { t -> if (", " in t) t.substringBefore(", ") to t.substringAfter(", ") else "Audio" to t }
                    // however many a file carries, only English matters here
                    val sidecars = it.raw.optJSONArray("subs").objects().map { s -> s.optString("label") }
                    val subs = (i?.subs.orEmpty() + sidecars).filter { s -> s.isNotEmpty() }
                    val subtitles = if (subs.isEmpty()) emptyList() else listOf("Subtitles" to if (subs.any { s -> s.contains("english", ignoreCase = true) }) "English" else "No English")
                    SpecGroup("Languages", tracks + subtitles)
                    SpecGroup(
                        "File",
                        listOfNotNull(
                            it.size.takeIf { s -> s > 0 }?.let { s -> "Size" to gb(s) },
                            i?.dur?.takeIf { d -> d > 0 }?.let { d -> "Length" to lengthName(d) },
                        ),
                    )
                }
            }
        }
    }
}

@Composable
private fun androidx.compose.foundation.layout.RowScope.KeyFigure(value: String, name: String) {
    Column(Modifier.weight(1f)) {
        Text(value, style = Type.Title.figures())
        Text(name, style = Type.Meta, color = Muted)
    }
}

/** A heading under a hairline, then its rows: the name on the left, its value on the right. */
@Composable
private fun SpecGroup(heading: String, rows: List<Pair<String, String>>) {
    if (rows.isEmpty()) return
    Box(Modifier.padding(top = 24.dp).fillMaxWidth().height(1.dp).background(Overlay))
    Text(heading, style = Type.Caption, color = Muted, modifier = Modifier.padding(top = 12.dp, bottom = 4.dp))
    rows.forEach { (name, value) ->
        Row(Modifier.fillMaxWidth().height(32.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(name, style = Type.Body, color = Muted, maxLines = 1)
            Spacer(Modifier.weight(1f))
            Text(value, style = Type.Label.figures(), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 16.dp))
        }
    }
}

private fun gb(bytes: Long): String = if (bytes >= 1e9) "%.1f GB".format(bytes / 1e9) else "%d MB".format((bytes / 1e6).toInt())

/** "AAC 5.1 · English" as the server sends it, read the way a person says it: "English, AAC 5.1". */
private fun audioLine(track: String): String {
    val parts = track.split(" · ")
    val format = parts[0].replace("DD+", "Dolby Digital+").replace("DD ", "Dolby Digital ").replace("stereo", "Stereo").replace("mono", "Mono")
    return if (parts.size > 1) "${parts[1]}, $format" else format
}

/** "Full HD", "4K Ultra HD": the words for a resolution. */
private fun qualityName(res: String) = when (res) {
    "4K" -> "4K Ultra HD"
    "1080p" -> "Full HD"
    "720p" -> "HD"
    else -> res
}

/** "HEVC, 10-bit": the codec without the jargon around it. */
private fun codecName(v: String) = when {
    v.contains("Main 10") -> v.substringBefore(" Main") + ", 10-bit"
    v.contains(" Main") || v.contains(" High") -> v.substringBefore(" ")
    else -> v
}

/** "59m 45s". */
private fun lengthName(sec: Double): String {
    val s = sec.toLong()
    return listOfNotNull((s / 3600).takeIf { it > 0 }?.let { "${it}h" }, "${s / 60 % 60}m", "${s % 60}s").joinToString(" ")
}
