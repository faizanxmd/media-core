package app.mediacore

import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.gestures.animateScrollBy
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.mediacore.core.Api
import app.mediacore.core.Item
import app.mediacore.core.Person
import app.mediacore.core.hm
import app.mediacore.core.objects
import coil3.compose.AsyncImage
import coil3.imageLoader
import coil3.request.ImageRequest
import coil3.request.crossfade
import coil3.request.transformations
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import org.json.JSONObject

/** A film's or a show's page: the same block for both; films end with the cast, shows with episodes. */
@Composable
fun DetailScreen(nav: Nav, id: String) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var data by remember(id) { mutableStateOf<Pair<Item, Item?>?>(null) }
    var error by remember(id) { mutableStateOf<String?>(null) }
    val playFr = remember { FocusRequester() }
    val infoFr = remember { FocusRequester() }
    val seasonFr = remember { FocusRequester() }

    suspend fun load() {
        try {
            Api.settled()
            val j = Api.get("/api/v1/item/$id")
            data = withContext(Dispatchers.Default) { Item.parse(j.getJSONObject("item")) to j.optJSONObject("up_next")?.let(Item::parse) }
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
    val (main, upNext) = d
    val show = main.isShow
    LaunchedEffect(id) { delay(60); runCatching { playFr.requestFocus() } }

    // the page arrives whole: it waits a moment for its picture, then fades up in one piece
    var bannerDone by remember(id) { mutableStateOf(false) }
    var arrived by remember(id) { mutableStateOf(false) }
    LaunchedEffect(id) {
        withTimeoutOrNull(300) { snapshotFlow { bannerDone }.first { it } }
        arrived = true
    }
    val still = remember(id) { blurredStill(ctx, Api.img(main.meta?.backdrop, "w780")) }
    LaunchedEffect(still) { if (still != null) ctx.imageLoader.enqueue(still) }

    val target = if (show || main.type == "season") upNext else main
    val play = { fromStart: Boolean ->
        if (target != null) {
            if (target.isEpisode) scope.launch { playEpisode(ctx, target, fromStart) }
            else PlayerActivity.play(ctx, listOf(target), 0, fromStart)
        }
    }
    val allWatched = if (show) main.unseen == 0 else main.watched
    val setWatched = { w: Boolean ->
        scope.launch {
            runCatching { Api.post("/api/v1/watched", JSONObject().put("id", main.id).put("watched", w)) }
            load()
        }
        Unit
    }
    // the episode you're on, which the words above the cards describe; else what Play would start
    var focusedEp by remember(id) { mutableStateOf<Item?>(null) }
    var details by remember(id) { mutableStateOf(false) }
    var seasons by remember(id) { mutableStateOf(false) }
    var season by remember(id) {
        mutableIntStateOf(main.seasons.indexOfFirst { s -> upNext != null && s.episodes.any { it.id == upNext.id } }.coerceAtLeast(0))
    }
    val described = focusedEp ?: target
    val leaveCards = { focusedEp = null }
    val refocus = { fr: FocusRequester -> scope.launch { delay(16); runCatching { fr.requestFocus() } }; Unit }

    FadeIn(arrived) {
        Banner(main.banner) { bannerDone = true }
        Column(Modifier.fillMaxSize().padding(top = PageTop)) {
            Column(Modifier.padding(start = Side).width(430.dp)) {
                TitleMark(main.logo, main.displayTitle)
                Text(
                    (listOf(if (show) "Show" else "Film") + main.meta?.genres.orEmpty().take(2) + listOfNotNull(main.meta?.cert?.ifEmpty { null }))
                        .joinToString(" · "),
                    style = Type.Meta, color = Muted, modifier = Modifier.padding(top = 12.dp),
                )
                val ep = if (show) described else null
                if (ep != null) Text("S${ep.season}, E${ep.episode} · ${ep.title}", style = Type.Label, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 12.dp))
                val story = (if (ep != null) ep.meta?.overview else null)?.ifEmpty { null } ?: main.meta?.overview.orEmpty()
                // always two lines' room, so the buttons stay put from one episode to the next
                Text(story, style = Type.Body, color = Muted, minLines = 2, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = if (ep != null) 4.dp else 12.dp))
                Row(Modifier.padding(top = 8.dp).height(16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    val len = (described ?: main).let { t -> t.durMs.takeIf { it > 0 } ?: ((t.meta?.runtime ?: 0) * 60000L) }
                    val facts = listOfNotNull(main.displayYear.takeIf { it > 0 }?.toString(), len.takeIf { it > 0 }?.let(::hm))
                    if (facts.isNotEmpty()) Text(facts.joinToString(" · "), style = Type.Meta, color = Muted)
                    FormatMarks(described?.info ?: main.info)
                }
                val resumable = target != null && target.pos > 0 && !target.watched && target.durMs > target.pos
                Row(Modifier.padding(top = 16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    PlayCapsule(if (resumable) hm(target!!.durMs - target.pos) else "Play", focusRequester = playFr, onFocus = leaveCards) { play(false) }
                    if (resumable) ToolButton(AppIcons.Replay, "Start over", onFocus = leaveCards) { play(true) }
                    ToolButton(AppIcons.Check, if (allWatched) "Mark unwatched" else "Mark watched", tint = if (allWatched) Ink else Muted, onFocus = leaveCards) { setWatched(!allWatched) }
                    ToolButton(AppIcons.Info, "Details", focusRequester = infoFr, onFocus = leaveCards) { details = true }
                }
            }
            Spacer(Modifier.height(SectionGap))
            if (show && main.seasons.isNotEmpty()) Episodes(main, upNext, season, seasonFr, { seasons = true }) { focusedEp = it }
            else if (!show) Cast(main.meta?.cast.orEmpty())
        }
        if (seasons) SeasonMenu(main.seasons.map { it.title }, season, onPick = { season = it; seasons = false; refocus(seasonFr) }) { seasons = false; refocus(seasonFr) }
        if (details) DetailsSheet(described ?: main, still) { details = false; refocus(infoFr) }
    }
}

/** The title page's picture, blurred once and ready to sit behind the details sheet. */
private fun blurredStill(ctx: Context, url: String?): ImageRequest? =
    url?.let { ImageRequest.Builder(ctx).data(it).size(320, 180).transformations(BlurOnce()).crossfade(false).build() }

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

/** The numbers, one press away: a sheet over the page's blurred picture. Left, Right, OK or Back close it; Up and Down scroll. */
@Composable
private fun DetailsSheet(it: Item, still: ImageRequest?, onClosed: () -> Unit) {
    val fr = remember { FocusRequester() }
    val scroll = rememberScrollState()
    val scope = rememberCoroutineScope()
    val a = remember { Animatable(0f) }
    var closing by remember { mutableStateOf(false) }
    val close = {
        if (!closing) {
            closing = true
            scope.launch { a.animateTo(0f, tween(Motion.FadeOutMs)); onClosed() }
        }
        Unit
    }
    LaunchedEffect(Unit) {
        delay(32); runCatching { fr.requestFocus() }
    }
    LaunchedEffect(Unit) { a.animateTo(1f, tween(Motion.FadeInMs)) }
    BackHandler(onBack = close)
    Box(
        Modifier.fillMaxSize().graphicsLayer { alpha = a.value }.background(Base)
            .focusRequester(fr)
            .onPreviewKeyEvent { e ->
                if (e.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
                when (e.key) {
                    Key.DirectionDown -> { scope.launch { scroll.animateScrollBy(120f) }; true }
                    Key.DirectionUp -> { scope.launch { scroll.animateScrollBy(-120f) }; true }
                    Key.DirectionLeft, Key.DirectionRight, Key.DirectionCenter, Key.Enter, Key.NumPadEnter -> { close(); true }
                    else -> false
                }
            }
            .focusable(),
    ) {
        if (still != null) AsyncImage(still, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        Box(Modifier.fillMaxSize().background(Base.copy(alpha = 0.6f)))
        Column(
            Modifier.align(Alignment.CenterEnd).width(420.dp).fillMaxHeight().background(Raised)
                .verticalScroll(scroll)
                .padding(start = 32.dp, end = 32.dp, top = 36.dp, bottom = 32.dp),
        ) {
            Text("Details", style = Type.Title)
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
            val picture = listOfNotNull(
                ("Resolution" to "$w × $h").takeIf { w > 0 && h > 0 },
                i?.vcodec?.ifEmpty { null }?.let { v -> "Video" to codecName(v) },
                i?.hdr?.ifEmpty { null }?.let { d -> "HDR" to d },
            )
            SpecGroup("Picture", picture)
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

/** A headline number at the top of the sheet, with what it counts under it. */
@Composable
private fun RowScope.KeyFigure(value: String, name: String) {
    Column(Modifier.weight(1f)) {
        Text(value, style = Type.Title.figures(), maxLines = 1)
        Text(name, style = Type.Caption, color = Muted, maxLines = 1, modifier = Modifier.padding(top = 4.dp))
    }
}

/** A heading under a hairline, then its rows: the name on the left, its value on the right. */
@Composable
private fun SpecGroup(heading: String, rows: List<Pair<String, String>>) {
    if (rows.isEmpty()) return
    Box(Modifier.padding(top = 20.dp).fillMaxWidth().height(1.dp).background(Overlay))
    Text(heading, style = Type.Caption, color = Muted, modifier = Modifier.padding(top = 12.dp, bottom = 4.dp))
    rows.forEach { (name, value) ->
        Row(Modifier.fillMaxWidth().height(30.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(name, style = Type.Body, color = Muted, maxLines = 1)
            Spacer(Modifier.weight(1f))
            Text(value, style = Type.Label.figures(), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 16.dp))
        }
    }
}

/** Faces with names under them, lined up with the title above; the page's last thing. */
@Composable
private fun Cast(cast: List<Person>) {
    if (cast.isEmpty()) return
    Column(Modifier.padding(start = Side)) {
        RowTitle("Cast")
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            cast.take(8).forEach { p ->
                Column(Modifier.width(102.dp)) {
                    Box(Modifier.size(72.dp).clip(CardShape).background(Raised), contentAlignment = Alignment.Center) {
                        val photo = Api.img(p.photo.ifEmpty { null }, "w185")
                        if (photo != null) AsyncImage(photo, p.name, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
                        else Text(p.name.split(" ").mapNotNull { it.firstOrNull()?.toString() }.take(2).joinToString(""), style = Type.Label, color = Muted)
                    }
                    Text(p.name, style = Type.Meta, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
                    if (p.role.isNotEmpty()) Text(p.role, style = Type.Caption, color = Muted, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}

/** "Season 1 ⌄" (a picker when there are more), then that season's episodes. */
@Composable
private fun Episodes(main: Item, upNext: Item?, season: Int, headingFr: FocusRequester, onOpenSeasons: () -> Unit, onFocusEp: (Item) -> Unit) {
    val ctx = LocalContext.current
    val s = main.seasons.getOrNull(season) ?: return
    val queue = episodeQueue(main)
    Column {
        SeasonHeading(s.title, main.seasons.size > 1, headingFr, onOpenSeasons)
        key(season) { // another season starts at its own beginning, not at the old scroll position
            val state = rememberLazyListState(initialFirstVisibleItemIndex = s.episodes.indexOfFirst { it.id == upNext?.id }.coerceAtLeast(0))
            LazyRow(
                state = state,
                contentPadding = PaddingValues(start = Side, end = Side, top = 12.dp, bottom = 12.dp),
                horizontalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                itemsIndexed(s.episodes, key = { _, it -> it.id }) { i, ep ->
                    EpisodeCard(ep, Modifier.rowEnds(i, s.episodes.size), { onFocusEp(ep) }) {
                        PlayerActivity.play(ctx, queue, queue.indexOfFirst { it.id == ep.id }.coerceAtLeast(0), logo = main.logo)
                    }
                }
            }
        }
    }
}

@Composable
private fun SeasonHeading(title: String, many: Boolean, fr: FocusRequester, onOpen: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    Row(
        Modifier.padding(start = Side)
            .then(if (many) Modifier.focusRequester(fr) else Modifier)
            .onFocusChanged { focused = it.isFocused }
            .then(if (many) Modifier.clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onOpen) else Modifier)
            .focusLine(focused && many)
            .height(30.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(title, style = Type.Heading)
        if (many) Icon(AppIcons.ExpandMore, null, tint = if (focused) Ink else Muted, modifier = Modifier.padding(start = 4.dp).size(24.dp))
    }
}

/** The list of seasons, low on the page's left, over the episodes. Back closes it. */
@Composable
private fun BoxScope.SeasonMenu(titles: List<String>, current: Int, onPick: (Int) -> Unit, onClose: () -> Unit) {
    val fr = remember { FocusRequester() }
    LaunchedEffect(Unit) { delay(32); runCatching { fr.requestFocus() } }
    BackHandler(onBack = onClose)
    LazyColumn(
        Modifier.align(Alignment.BottomStart).padding(start = Side, bottom = 32.dp).width(220.dp).heightIn(max = 300.dp).clip(CardShape).background(Raised).padding(8.dp),
        state = rememberLazyListState(initialFirstVisibleItemIndex = (current - 2).coerceAtLeast(0)),
    ) {
        itemsIndexed(titles) { i, t ->
            Box(Modifier.listEnds(i, titles.size)) { MenuOption(t, "", i == current, if (i == current) fr else null) { onPick(i) } }
        }
    }
}

/** The picture and how far you got on its edge, with the number and name under it: the full story is in the block above. */
@Composable
private fun EpisodeCard(ep: Item, modifier: Modifier, onFocus: () -> Unit, onClick: () -> Unit) {
    Column(Modifier.width(190.dp)) {
        FocusCard(onClick, modifier.fillMaxWidth().aspectRatio(16f / 9f), onFocus = onFocus) {
            ep.wideSmall?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
            if (!ep.watched && ep.pos > 0 && ep.durMs > ep.pos) ProgressTrack(ep.progress, Modifier.align(Alignment.BottomCenter).fillMaxWidth())
        }
        Row(Modifier.padding(top = 4.dp).height(24.dp), verticalAlignment = Alignment.CenterVertically) {
            Text("${ep.episode} · ${ep.title}", style = Type.Meta, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (ep.watched) Icon(AppIcons.Check, "Watched", tint = Muted, modifier = Modifier.size(24.dp))
        }
    }
}

/** One row of a compact menu: a check for the current choice, the name, a detail on the right. */
@Composable
fun MenuOption(title: String, detail: String, selected: Boolean, fr: FocusRequester?, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    Row(
        Modifier.fillMaxWidth().height(36.dp)
            .then(if (fr != null) Modifier.focusRequester(fr) else Modifier)
            .onFocusChanged { focused = it.isFocused }
            .clip(CardShape)
            .background(if (focused) Ink else Color.Transparent)
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .padding(horizontal = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.width(28.dp)) {
            if (selected) Icon(AppIcons.Check, null, tint = if (focused) Base else Ink, modifier = Modifier.size(24.dp))
        }
        Text(title, style = Type.Body, color = if (focused) Base else Ink, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        if (detail.isNotEmpty()) Text(detail, style = Type.Meta, color = if (focused) Base else Muted, maxLines = 1, modifier = Modifier.padding(start = 8.dp))
    }
}
