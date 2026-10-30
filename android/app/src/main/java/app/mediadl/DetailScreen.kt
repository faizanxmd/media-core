package app.mediadl

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Check
import androidx.compose.material.icons.rounded.Replay
import androidx.compose.material.icons.rounded.UnfoldMore
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
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
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Popup
import androidx.compose.ui.window.PopupProperties
import coil3.compose.AsyncImage
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.json.JSONObject

private val Text72 = Color(0xB8FFFFFF)
private val Text76 = Color(0xC2FFFFFF)

/** A film's or a show's page: the same block for both; films end with the cast, shows with episodes. */
@Composable
fun DetailScreen(nav: Nav, id: String) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var data by remember(id) { mutableStateOf<JSONObject?>(null) }
    var error by remember(id) { mutableStateOf<String?>(null) }
    val playFr = remember { FocusRequester() }

    suspend fun load() {
        try {
            data = Api.get("/api/v1/item/$id")
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
    LaunchedEffect(id) { delay(60); runCatching { playFr.requestFocus() } }

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

    Box(Modifier.fillMaxSize()) {
        MaskedBackdrop(main.backdrop)
        Column(Modifier.fillMaxSize().padding(top = if (show) 44.dp else 100.dp)) {
            Column(Modifier.padding(start = 40.dp).width(450.dp), verticalArrangement = Arrangement.spacedBy(11.dp)) {
                Box(Modifier.height(if (show) 75.dp else 95.dp).width(300.dp).padding(bottom = 3.dp), contentAlignment = Alignment.BottomStart) {
                    if (main.logo != null) AsyncImage(main.logo, main.displayTitle, Modifier.fillMaxSize(), contentScale = ContentScale.Fit, alignment = Alignment.BottomStart)
                    else Text(main.displayTitle, fontSize = 32.sp, lineHeight = 34.sp, fontWeight = FontWeight.Bold, maxLines = 2, overflow = TextOverflow.Ellipsis)
                }
                Text(
                    (listOf(if (show) "Show" else "Film") + main.meta?.genres.orEmpty().take(2) + listOfNotNull(main.meta?.cert?.ifEmpty { null }))
                        .joinToString(" · "),
                    color = Text72, fontSize = 13.5.sp, fontWeight = FontWeight.Medium,
                )
                val ep = if (show) target else null
                val story = (if (ep != null) ep.meta?.overview else null)?.ifEmpty { null } ?: main.meta?.overview.orEmpty()
                if (story.isNotEmpty() || ep != null) Text(
                    buildAnnotatedString {
                        if (ep != null) withStyle(SpanStyle(fontWeight = FontWeight.SemiBold, color = Ink)) {
                            append("S${ep.season}, E${ep.episode} · ${ep.title}")
                            append(if (story.isNotEmpty()) ": " else "")
                        }
                        append(story)
                    },
                    color = Text76, fontSize = 14.5.sp, lineHeight = 19.sp, maxLines = 2, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.width(430.dp),
                )
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    val len = (target ?: main).let { t -> t.durMs.takeIf { it > 0 } ?: ((t.meta?.runtime ?: 0) * 60000L) }
                    val facts = listOfNotNull(main.displayYear.takeIf { it > 0 }?.toString(), len.takeIf { it > 0 }?.let(::hm))
                    if (facts.isNotEmpty()) Text(facts.joinToString(" · "), color = Text72, fontSize = 13.5.sp, fontWeight = FontWeight.Medium)
                    FormatMarks(target?.info ?: main.info, Text72)
                }
                val resumable = target != null && target.pos > 0 && !target.watched && target.durMs > target.pos
                Row(
                    Modifier.padding(top = if (show) 3.dp else 11.dp),
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(11.dp),
                ) {
                    PlayCapsule(if (resumable) hm(target!!.durMs - target.pos) else "Play", focusRequester = playFr) { play(false) }
                    if (resumable) CircleButton(Icons.Rounded.Replay, "Start over") { play(true) }
                    CircleButton(Icons.Rounded.Check, if (allWatched) "Mark unwatched" else "Mark watched", content = { f ->
                        Icon(Icons.Rounded.Check, null, tint = if (f) Bg else if (allWatched) Ink else Faint, modifier = Modifier.size(19.dp))
                    }) { setWatched(!allWatched) }
                }
            }
            if (show && main.seasons.isNotEmpty()) {
                Spacer(Modifier.height(24.dp))
                Episodes(main, upNext)
            } else if (!show) {
                Spacer(Modifier.weight(1f))
                Cast(main.meta?.cast.orEmpty())
            }
        }
    }
}

/** Faces with names under them; the page's last thing. */
@Composable
private fun Cast(cast: List<Person>) {
    if (cast.isEmpty()) return
    Column(Modifier.padding(start = 40.dp, bottom = 26.dp)) {
        RowTitle("Cast")
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp), modifier = Modifier.padding(start = 0.dp)) {
            cast.take(7).forEach { p ->
                Column(Modifier.width(100.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                    Box(Modifier.size(72.dp).clip(CircleShape).background(Surface2), contentAlignment = Alignment.Center) {
                        val photo = Api.img(p.photo.ifEmpty { null }, "w185")
                        if (photo != null) AsyncImage(photo, p.name, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
                        else Text(p.name.split(" ").mapNotNull { it.firstOrNull()?.toString() }.take(2).joinToString(""), color = Mut, fontSize = 16.sp, fontWeight = FontWeight.SemiBold)
                    }
                    Text(p.name, fontSize = 12.sp, lineHeight = 14.sp, fontWeight = FontWeight.SemiBold, textAlign = TextAlign.Center, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
                    if (p.role.isNotEmpty()) Text(p.role, color = Faint, fontSize = 11.sp, lineHeight = 13.sp, textAlign = TextAlign.Center, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}

/** "Season 1 ⌄" (a picker when there are more), then that season's episodes as Apple-style cards. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun Episodes(main: Item, upNext: Item?) {
    val ctx = LocalContext.current
    val density = LocalDensity.current
    val rowSpec = remember(density) { MinimalSpec(density.run { 56.dp.toPx() }) } // moves like Home's rows
    var season by remember(main.id) {
        mutableIntStateOf(main.seasons.indexOfFirst { s -> upNext != null && s.episodes.any { it.id == upNext.id } }.coerceAtLeast(0))
    }
    val s = main.seasons.getOrNull(season) ?: return
    val queue = episodeQueue(main)
    Column {
        SeasonHeading(main.seasons.map { it.title }, season) { season = it }
        val state = rememberLazyListState(initialFirstVisibleItemIndex = s.episodes.indexOfFirst { it.id == upNext?.id }.coerceAtLeast(0))
        CompositionLocalProvider(LocalBringIntoViewSpec provides rowSpec) {
            LazyRow(
                state = state,
                contentPadding = PaddingValues(horizontal = 40.dp, vertical = 12.dp),
                horizontalArrangement = Arrangement.spacedBy(20.dp),
            ) {
                itemsIndexed(s.episodes, key = { _, it -> it.id }) { i, ep ->
                    EpisodeCard(ep, Modifier.rowEnds(i, s.episodes.size)) {
                        PlayerActivity.play(ctx, queue, queue.indexOfFirst { it.id == ep.id }.coerceAtLeast(0))
                    }
                }
            }
        }
    }
}

@Composable
private fun SeasonHeading(titles: List<String>, current: Int, onPick: (Int) -> Unit) {
    var focused by remember { mutableStateOf(false) }
    var open by remember { mutableStateOf(false) }
    val many = titles.size > 1
    Box(Modifier.padding(start = 30.dp)) {
        Row(
            Modifier
                .onFocusChanged { focused = it.isFocused }
                .lift(focused && many, RoundedCornerShape(10.dp), 1.04f)
                .background(if (focused && many) Color.White else Color.Transparent)
                .then(if (many) Modifier.clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) { open = true } else Modifier)
                .padding(horizontal = 10.dp, vertical = 3.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            val c = if (focused && many) Bg else Ink
            Text(titles[current], color = c, fontSize = 18.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-0.2).sp)
            if (many) Icon(Icons.Rounded.UnfoldMore, null, tint = if (focused) Bg.copy(alpha = 0.6f) else Faint, modifier = Modifier.padding(start = 3.dp).size(18.dp))
        }
        if (open) Popup(
            alignment = Alignment.TopStart, offset = IntOffset(0, 0),
            onDismissRequest = { open = false }, properties = PopupProperties(focusable = true),
        ) {
            val fr = remember { FocusRequester() }
            LaunchedEffect(Unit) { delay(30); runCatching { fr.requestFocus() } }
            LazyColumn(
                Modifier.width(220.dp).heightIn(max = 300.dp).clip(RoundedCornerShape(11.dp)).background(Surface).padding(7.dp),
                state = rememberLazyListState(initialFirstVisibleItemIndex = (current - 2).coerceAtLeast(0)),
            ) {
                itemsIndexed(titles) { i, t ->
                    MenuOption(t, "", i == current, if (i == current) fr else null) { onPick(i); open = false }
                }
            }
        }
    }
}

/** Picture with the text on it over a soft fade: overline, title, two lines of story, time. */
@Composable
private fun EpisodeCard(ep: Item, modifier: Modifier, onClick: () -> Unit) {
    FocusCard(onClick, modifier.width(205.dp).height(180.dp), radius = 8.dp, scale = 1.06f) {
        ep.wide?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
        Box(Modifier.matchParentSize().background(Brush.verticalGradient(0.22f to Color.Transparent, 0.48f to Color(0x8C000000), 0.72f to Color(0xDB000000), 1f to Color(0xEB000000))))
        Column(Modifier.align(Alignment.BottomStart).fillMaxWidth().padding(start = 13.dp, end = 13.dp, bottom = 12.dp)) {
            Text("EPISODE ${ep.episode}", color = Color(0xA8FFFFFF), fontSize = 10.sp, fontWeight = FontWeight.SemiBold, letterSpacing = 0.4.sp)
            Text(ep.title, fontSize = 14.5.sp, lineHeight = 17.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 3.dp))
            ep.meta?.overview?.takeIf { it.isNotEmpty() }?.let {
                Text(it, color = Text72, fontSize = 12.sp, lineHeight = 15.5.sp, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 4.dp))
            }
            Row(Modifier.padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                val resumable = ep.pos > 0 && !ep.watched && ep.durMs > ep.pos
                if (resumable) {
                    ProgressTrack(ep.progress, Modifier.width(45.dp))
                    Spacer(Modifier.width(6.dp))
                }
                Text(
                    when {
                        ep.watched -> "Watched"
                        resumable -> hm(ep.durMs - ep.pos) + " left"
                        ep.durMs > 0 -> hm(ep.durMs)
                        else -> ""
                    },
                    color = Color(0xD9FFFFFF), fontSize = 12.sp, fontWeight = FontWeight.SemiBold,
                )
            }
        }
    }
}

/** One row of a compact menu: a check for the current choice, the name, a detail on the right. */
@Composable
fun MenuOption(title: String, detail: String, selected: Boolean, fr: FocusRequester?, onClick: () -> Unit) {
    var focused by remember { mutableStateOf(false) }
    Row(
        Modifier.fillMaxWidth().height(32.dp)
            .then(if (fr != null) Modifier.focusRequester(fr) else Modifier)
            .onFocusChanged { focused = it.isFocused }
            .clip(RoundedCornerShape(7.dp))
            .background(if (focused) Color.White else Color.Transparent)
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .padding(horizontal = 9.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.width(22.dp)) {
            if (selected) Icon(Icons.Rounded.Check, null, tint = if (focused) Bg else Ink, modifier = Modifier.size(15.dp))
        }
        Text(title, color = if (focused) Bg else Ink, fontSize = 13.5.sp, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        if (detail.isNotEmpty()) Text(detail, color = if (focused) Bg.copy(alpha = 0.6f) else Faint, fontSize = 11.5.sp, fontWeight = FontWeight.Medium, maxLines = 1, modifier = Modifier.padding(start = 8.dp))
    }
}
