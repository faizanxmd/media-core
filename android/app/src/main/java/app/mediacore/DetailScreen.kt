package app.mediacore

import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateMapOf
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
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import app.mediacore.core.Api
import app.mediacore.core.Item
import app.mediacore.core.objects
import coil3.compose.AsyncImage
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

private const val LEFT = 120

/** A title page's three places: its first screen, and what Down shows (episodes for a show, the rest for a film). */
private enum class Place { First, Episodes, More }

/**
 * A film's or a show's page (boards 04-10). The first screen is the title's art with its logo, one line of facts,
 * the story, the credits and the buttons. Down slides the page up: a show's seasons and episodes, or a film's cast,
 * About, chapters and file.
 */
@Composable
fun TitlePage(nav: Nav, id: String) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val art = LocalArt.current
    var data by remember(id) { mutableStateOf(nav.details[id]) }
    LaunchedEffect(id, nav.refresh) {
        try {
            data = nav.fetchDetail(id)
            nav.offline = false
        } catch (e: Exception) {
            nav.failed(e)
        }
    }
    val d = data ?: return
    val (main, upNext) = d
    val show = main.isShow

    val requesters = remember { mutableMapOf<String, FocusRequester>() }
    fun fr(k: String) = requesters.getOrPut(k) { FocusRequester() }
    var place by remember { mutableStateOf(Place.First) }
    var sheet by remember { mutableStateOf(false) }
    var focus by remember { mutableStateOf(nav.titleFocus ?: "play") }

    // the season and episode in view: up next to begin with
    val startSeason = remember(id) { main.seasons.indexOfFirst { s -> s.episodes.any { it.id == upNext?.id } }.coerceAtLeast(0) }
    var season by remember(id) { mutableIntStateOf(startSeason) }
    val epIndex = remember(id) { mutableStateMapOf<Int, Int>() }
    fun epOf(s: Int) = epIndex[s] ?: main.seasons.getOrNull(s)?.episodes?.indexOfFirst { it.id == upNext?.id }?.takeIf { it >= 0 } ?: 0
    val focusedEp = main.seasons.getOrNull(season)?.episodes?.getOrNull(epOf(season))

    // the art: the title's backdrop, or its poster blurred; down in the episodes, the focused episode's still
    val firstArt = remember(main) {
        when {
            main.meta?.backdrop?.isNotEmpty() == true -> ArtSpec(main.banner, if (show) ArtStyle.Show else ArtStyle.Film, if (show) 0.25f else 0.2f)
            main.meta?.poster?.isNotEmpty() == true -> ArtSpec(Api.img(main.meta?.poster, "w780"), ArtStyle.Poster, 0.3f)
            else -> ArtSpec(null, ArtStyle.Plain)
        }
    }
    val epArt = focusedEp?.let { ArtSpec(Api.img(it.meta?.still?.ifEmpty { null }, "w1280") ?: main.banner, ArtStyle.Episode) }
    val wantArt = if (place == Place.Episodes && epArt != null) epArt else firstArt
    var firstShown by remember { mutableStateOf(false) }
    LaunchedEffect(wantArt) {
        if (firstShown) delay(Move.Settle) // a new still only once the remote rests
        art.show(wantArt, if (firstShown) Move.Art else Move.Screen)
        firstShown = true
    }

    val target = playTarget(main, d, nav.home)
    // focus: on Resume (or Play) when the page opens or the player closes, else where it was
    LaunchedEffect(Unit) {
        withFrameNanos { }
        nav.titleFocus = null
        runCatching { fr(focus).requestFocus() }.onFailure { runCatching { fr("play").requestFocus() } }
    }
    LaunchedEffect(nav.refresh) { // back from the player
        if (nav.titleFocus != null) { focus = "play"; nav.titleFocus = null; withFrameNanos { }; runCatching { fr("play").requestFocus() } }
    }
    fun go(p: Place, k: String) { place = p; scope.launchFocus(fr(k)) }

    BackHandler(enabled = place != Place.First && !sheet) { go(Place.First, "play") }

    val pageY by animateFloatAsState(
        when (place) {
            Place.First -> 0f
            Place.Episodes -> -750f * Px.screen
            Place.More -> -810f * Px.screen
        },
        Move.slide(Move.Screen), label = "page",
    )
    Box(Modifier.fillMaxSize()) {
        Box(Modifier.fillMaxSize().graphicsLayer { translationY = pageY }) {
            LogoMark(Modifier.offset(LEFT.px, 56.px))
            SettingsButton(nav, fr("settings"), fr("play")) { focus = "settings" }
            FirstScreen(main, show, target, fr = ::fr, onFocus = { focus = it }, onDown = {
                if (show && main.seasons.isNotEmpty()) go(Place.Episodes, "ep/$season/${epOf(season)}") else if (!show) go(Place.More, "more")
            }, onPlay = { fromStart ->
                target?.let { t ->
                    nav.titleFocus = "play"
                    if (t.item.isEpisode) startPlay(ctx, scope, nav, t.item, fromStart, main.logo, episodeQueue(main))
                    else startPlay(ctx, scope, nav, t.item, fromStart)
                }
            }, onDetails = { sheet = true })
            if (show && main.seasons.isNotEmpty()) EpisodesBlock(
                main, upNext, season, ::epOf, active = place == Place.Episodes, fr = ::fr,
                onSeason = { s -> season = s },
                onEpisode = { s, i -> season = s; epIndex[s] = i },
                onUp = { go(Place.First, "play") },
                onPlay = { ep ->
                    nav.titleFocus = "play"
                    startPlay(ctx, scope, nav, ep, false, main.logo, episodeQueue(main))
                },
            )
            if (!show) MoreBlock(main, fr("more")) { go(Place.First, "play") }
        }
        val detailOf = if (show) (target?.item ?: focusedEp) else main
        if (sheet && detailOf != null) PictureDetails(main, detailOf, firstArt) { sheet = false; scope.launchFocus(fr("details")) }
    }
}

private fun CoroutineScope.launchFocus(fr: FocusRequester) {
    launch { withFrameNanos { }; runCatching { fr.requestFocus() } }
}

/** Settings at the top right of a title page, as on Home. */
@Composable
fun SettingsButton(nav: Nav, fr: FocusRequester, down: FocusRequester, onFocus: () -> Unit) {
    Box(Modifier.fillMaxWidth()) {
        GlassButton(
            "Settings", Icons.Settings, height = 44, textSize = 18, iconSize = 20, iconGap = 10, padStart = 14, padEnd = 18,
            modifier = Modifier.align(Alignment.TopEnd).padding(end = 120.px, top = 50.px).focusProperties { this.down = down },
            fr = fr, onFocus = { if (it) onFocus() },
        ) { nav.open(Screen.Settings) }
    }
}

/** The page's first screen: logo, facts, story, credits, buttons (boards 04-09, at their fixed places). */
@Composable
private fun FirstScreen(
    main: Item, show: Boolean, target: PlayTarget?, fr: (String) -> FocusRequester, onFocus: (String) -> Unit,
    onDown: () -> Unit, onPlay: (Boolean) -> Unit, onDetails: () -> Unit,
) {
    // a show's block sits higher, leaving room for "Episodes" and its first row at the bottom
    val y = if (show) listOf(299, 502, 538, 630, 726) else listOf(332, 586, 622, 716, 812)
    Box(Modifier.offset(if (show) 115.px else LEFT.px, y[0].px)) {
        TitleArt(main.logo, main.displayTitle, 620, if (show) 180 else 232, Alignment.BottomStart, typeSize = if (show) 80 else 92, typeLine = if (show) 76 else 86)
    }
    val facts = listOfNotNull(
        main.displayYear.takeIf { it > 0 }?.toString(), main.meta?.genres?.firstOrNull(),
        if (show) null else runtimeOf(main)?.let(::span), main.meta?.cert?.ifEmpty { null },
    )
    DotLine(facts, ts(19, 24, 600, Pal.InkSoft, cond = true, track = 0.01f), modifier = Modifier.offset(LEFT.px, y[1].px))
    main.meta?.overview?.ifEmpty { null }?.let {
        Text(it, style = ts(21, 30, 400, Pal.InkSoft), maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.offset(LEFT.px, y[2].px).width(660.px))
    }
    // Director, Music (or Created by) need crew from TMDB, which mint doesn't fetch yet: only Starring for now
    val stars = main.meta?.cast.orEmpty().take(3).map { it.name }
    if (stars.isNotEmpty()) Row(Modifier.offset(LEFT.px, y[3].px), horizontalArrangement = Arrangement.spacedBy(56.px)) {
        Column {
            Text("STARRING", style = CapsLabel)
            DotLine(stars, ts(22, 28, 600), gap = if (show) 12 else 10, modifier = Modifier.padding(top = 8.px))
        }
    }
    val started = target != null && target.word == "Resume"
    Row(
        Modifier.offset(LEFT.px, y[4].px).onPreviewKeyEvent { e ->
            if (e.type == KeyEventType.KeyDown && e.key == Key.DirectionDown) { onDown(); true } else false
        },
        horizontalArrangement = Arrangement.spacedBy(10.px), verticalAlignment = Alignment.Top,
    ) {
        val up = Modifier.focusProperties { up = fr("settings") }
        if (target != null) PlayButton(target.word, target.detail, target.progress, up, fr = fr("play"), onFocus = { if (it) onFocus("play") }) { onPlay(false) }
        if (started) IconButton(Icons.StartOver, "Start over", up, fr = fr("over"), onFocus = { if (it) onFocus("over") }) { onPlay(true) }
        if (target?.item?.info != null || main.info != null) {
            if (target != null) IconButton(Icons.Info, "Picture details", up, fr = fr("details"), onFocus = { if (it) onFocus("details") }, onClick = onDetails)
            else GlassButton("Picture details", Icons.Info, up, iconSize = 24, padStart = 16, padEnd = 22, weight = 700, fr = fr("details"), onFocus = { if (it) onFocus("details") }, onClick = onDetails)
        }
    }
}

// ---------- a show's seasons and episodes (boards 09 and 10) ----------

private const val EP_X = 500
private const val EP_W = 1300
private const val EP_PITCH = 176 // a 162 px row and 14 under it
private const val SEASON_PITCH = 144 // a 126 px poster and 18 under it

/**
 * "Episodes ⌄", then the seasons down the left and the season's episodes on the right. Laid out under the first
 * screen; Down slides it up into view. The focused episode is at full brightness and holds the top slot.
 */
@Composable
private fun EpisodesBlock(
    main: Item, upNext: Item?, season: Int, epOf: (Int) -> Int, active: Boolean, fr: (String) -> FocusRequester,
    onSeason: (Int) -> Unit, onEpisode: (Int, Int) -> Unit, onUp: () -> Unit, onPlay: (Item) -> Unit,
) {
    Row(Modifier.offset(LEFT.px, 900.px), verticalAlignment = Alignment.CenterVertically) {
        Text("Episodes", style = ts(24, 28, 700, cond = true))
        Icon(Icons.Down, null, tint = Pal.Ink, modifier = Modifier.padding(start = 10.px).size(22.px))
    }
    var onSeasons by remember { mutableStateOf(false) }
    // seasons
    Box(Modifier.offset(LEFT.px, 952.px)) {
        main.seasons.forEachIndexed { s, it ->
            val lit by animateFloatAsState(if (s == season) 1f else 0.45f, Move.fade(Move.Focus), label = "season")
            Row(
                Modifier.offset(y = (s * SEASON_PITCH).px).graphicsLayer { alpha = lit }
                    .focusRequester(fr("season/$s"))
                    .onFocusChanged { f -> if (f.isFocused) { onSeasons = true; onSeason(s) } }
                    .focusProperties {
                        up = if (s > 0) fr("season/${s - 1}") else FocusRequester.Cancel
                        down = if (s < main.seasons.lastIndex) fr("season/${s + 1}") else FocusRequester.Cancel
                        right = fr("ep/$s/${epOf(s)}")
                        left = FocusRequester.Cancel
                    }
                    .onPreviewKeyEvent { e -> if (e.type == KeyEventType.KeyDown && e.key == Key.DirectionUp && s == 0) { onUp(); true } else false }
                    .focusable(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(Modifier.size(84.px, 126.px).softShadow(ShadowKind.Small).clip(Corner).background(Pal.Raised)) {
                    Api.img(it.meta?.poster?.ifEmpty { null } ?: main.meta?.poster, "w185")?.let { u -> AsyncImage(u, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
                }
                Column(Modifier.padding(start = 18.px)) {
                    Text(it.title, style = ts(26, 30, 700, cond = true), maxLines = 1)
                    Text(
                        if (it.unseen == 0 && it.episodes.isNotEmpty()) "All watched" else "${it.episodes.size} episode" + if (it.episodes.size == 1) "" else "s",
                        style = ts(16, 20, 400, Pal.Muted), modifier = Modifier.padding(top = 4.px),
                    )
                }
            }
        }
    }
    // the season's episodes: another season crossfades in
    AnimatedContent(
        season, transitionSpec = { fadeIn(Move.fade(Move.Screen)) togetherWith fadeOut(Move.fade(Move.Screen)) }, label = "season",
        modifier = Modifier.offset(EP_X.px, 952.px),
    ) { s ->
        val eps = main.seasons.getOrNull(s)?.episodes.orEmpty()
        val focusedI = epOf(s)
        val top by animateFloatAsState(-focusedI * EP_PITCH * Px.screen, Move.slide(Move.Screen), label = "list")
        Box(Modifier.graphicsLayer { translationY = top }) {
            eps.forEachIndexed { i, ep ->
                val lit by animateFloatAsState(
                    when {
                        i < focusedI -> 0f // above the top slot: out of the way of the heading
                        i == focusedI && !onSeasons -> 1f
                        i == focusedI && !active -> 1f
                        else -> 0.45f
                    },
                    Move.fade(Move.Focus), label = "ep",
                )
                EpisodeRow(
                    ep, ep.id == upNext?.id,
                    Modifier.offset(y = (i * EP_PITCH).px).graphicsLayer { alpha = lit }
                        .focusRequester(fr("ep/$s/$i"))
                        .onFocusChanged { f -> if (f.isFocused) { onSeasons = false; onEpisode(s, i) } }
                        .focusProperties {
                            up = if (i > 0) fr("ep/$s/${i - 1}") else FocusRequester.Cancel
                            down = if (i < eps.lastIndex) fr("ep/$s/${i + 1}") else FocusRequester.Cancel
                            left = fr("season/$s")
                            right = FocusRequester.Cancel
                        }
                        .onPreviewKeyEvent { e ->
                            if (e.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
                            when {
                                e.key == Key.DirectionUp && i == 0 -> { onUp(); true }
                                e.key == Key.DirectionCenter || e.key == Key.Enter || e.key == Key.NumPadEnter -> { onPlay(ep); true }
                                else -> false
                            }
                        }
                        .focusable(),
                )
            }
        }
    }
}

/** One episode: still, number and name, two lines of story; length (or what's left) and date on the right. */
@Composable
private fun EpisodeRow(ep: Item, upNext: Boolean, modifier: Modifier) {
    Row(modifier.width(EP_W.px).height(162.px), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(288.px, 162.px).softShadow(ShadowKind.Small).clip(Corner).background(Pal.Raised)) {
            ep.wideSmall?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
        }
        Column(Modifier.padding(start = 30.px).widthIn(max = 760.px).weight(1f, fill = false)) {
            Row {
                Text("${ep.episode}", style = ts(24, 30, 700, Pal.Faint, tabular = true, track = -0.01f), modifier = Modifier.padding(end = 14.px))
                Text(ep.title, style = ts(24, 30, 700, track = -0.01f), maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            ep.meta?.overview?.ifEmpty { null }?.let {
                Text(it, style = ts(16, 23, 400, Pal.Muted), maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.px))
            }
        }
        Spacer(Modifier.weight(1f))
        Column(horizontalAlignment = Alignment.End) {
            if (upNext) Text("Up next", style = ts(16, 20, 700), modifier = Modifier.padding(bottom = 6.px))
            val started = ep.pos > 0 && !ep.watched && ep.durMs > ep.pos
            val len = if (started) leftWords(ep.durMs - ep.pos) else runtimeOf(ep)?.let(::span)
            if (len != null) Text(len, style = ts(19, 24, 600, Pal.InkSoft, tabular = true))
            dateWords(ep.meta?.date)?.let { Text(it, style = ts(15, 20, 400, Pal.Faint), modifier = Modifier.padding(top = 3.px)) }
        }
    }
}

/** "2025-10-16" as "Oct 16, 2025". */
fun dateWords(d: String?): String? = runCatching {
    val p = d!!.split("-").map { it.toInt() }
    val months = listOf("Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec")
    "${months[p[1] - 1]} ${p[2]}, ${p[0]}"
}.getOrNull()

// ---------- below a film's first screen: cast, About, chapters, file (no board yet: plain, same style) ----------

@Composable
private fun MoreBlock(main: Item, fr: FocusRequester, onUp: () -> Unit) {
    // one stop: everything below fits one screen, so Up goes straight back
    Column(
        Modifier.offset(LEFT.px, 960.px).width(1680.px).focusRequester(fr)
            .onPreviewKeyEvent { e -> if (e.type == KeyEventType.KeyDown && e.key == Key.DirectionUp) { onUp(); true } else e.type == KeyEventType.KeyDown && e.key == Key.DirectionDown }
            .focusable(),
    ) {
        val cast = main.meta?.cast.orEmpty().take(10)
        if (cast.isNotEmpty()) {
            Heading("Cast")
            Row(horizontalArrangement = Arrangement.spacedBy(8.px), modifier = Modifier.padding(top = 16.px)) {
                cast.forEach { p ->
                    Column(Modifier.width(150.px)) {
                        Box(Modifier.size(150.px, 200.px).softShadow(ShadowKind.Small).clip(Corner).background(Pal.Raised)) {
                            Api.img(p.photo.ifEmpty { null }, "w185")?.let { AsyncImage(it, p.name, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
                        }
                        Text(p.name, style = ts(17, 22, 700), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 10.px))
                        if (p.role.isNotEmpty()) Text(p.role, style = ts(15, 20, 400, Pal.Muted), maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
            }
        }
        Heading("About", Modifier.padding(top = 44.px))
        main.meta?.tagline?.ifEmpty { null }?.let { Text(it, style = ts(21, 28, 650), modifier = Modifier.padding(top = 14.px)) }
        main.meta?.overview?.ifEmpty { null }?.let {
            Text(it, style = ts(19, 28, 400, Pal.InkSoft), maxLines = 3, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.px).width(1100.px))
        }
        val about = listOfNotNull(dateWords(main.meta?.date), main.meta?.studio?.ifEmpty { null }, main.meta?.genres?.joinToString(", ")?.ifEmpty { null })
        if (about.isNotEmpty()) DotLine(about, ts(17, 22, 600, Pal.Muted), modifier = Modifier.padding(top = 12.px))
        val chapters = main.raw.optJSONObject("info")?.optJSONArray("chapters").objects()
        if (chapters.size > 1) {
            Heading("Chapters", Modifier.padding(top = 44.px))
            val shown = chapters.take(12)
            Column(Modifier.padding(top = 12.px)) {
                shown.chunked(4).forEach { line ->
                    Row(Modifier.padding(top = 6.px)) {
                        line.forEach { c ->
                            Row(Modifier.width(400.px)) {
                                Text("%02d".format(chapters.indexOf(c) + 1), style = ts(17, 22, 600, Pal.Faint, tabular = true), modifier = Modifier.width(40.px))
                                Text(c.optString("title").ifEmpty { "Chapter ${chapters.indexOf(c) + 1}" }, style = ts(17, 22, 600, Pal.InkSoft), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                                Text(app.mediacore.core.clock((c.optDouble("start") * 1000).toLong()), style = ts(17, 22, 400, Pal.Muted, tabular = true), modifier = Modifier.padding(start = 12.px, end = 32.px))
                            }
                        }
                    }
                }
                if (chapters.size > shown.size) Text("and ${chapters.size - shown.size} more", style = ts(17, 22, 400, Pal.Muted), modifier = Modifier.padding(top = 8.px))
            }
        }
        fileLine(main)?.let {
            Heading("File", Modifier.padding(top = 44.px))
            DotLine(it, ts(19, 24, 600, Pal.InkSoft, tabular = true), modifier = Modifier.padding(top = 12.px))
        }
    }
}

@Composable
private fun Heading(text: String, modifier: Modifier = Modifier) = Text(text, style = ts(24, 28, 700, cond = true), modifier = modifier)

private fun fileLine(it: Item): List<String>? {
    val raw = it.raw.optJSONObject("info") ?: return null
    return listOfNotNull(
        raw.optString("container").ifEmpty { null }?.uppercase(),
        it.size.takeIf { s -> s > 0 }?.let(::gb),
        it.info?.dur?.takeIf { d -> d > 0 }?.let { d -> span((d * 1000).toLong()) },
        it.info?.res?.ifEmpty { null },
        it.info?.mbps?.takeIf { m -> m > 0 }?.let { m -> "%.1f Mb/s".format(m) },
    ).takeIf { l -> l.isNotEmpty() }
}

// ---------- a title still downloading (board 08) ----------

/** All mint knows of a title that's still downloading: its name and how far along it is. */
@Composable
fun PendingPage(nav: Nav, title: String) {
    val art = LocalArt.current
    LaunchedEffect(Unit) { art.show(ArtSpec(null, ArtStyle.Plain), Move.Screen) }
    val d = nav.home?.downloads?.firstOrNull { it.title == title }
    // when it's done it's in the library: its page replaces this one
    val done = nav.home?.recent?.firstOrNull { it.title == title || it.displayTitle == title }
    LaunchedEffect(done?.id) { if (done != null && d == null) { nav.back(); nav.open(Screen.Detail(done.id)) } }
    val fr = remember { FocusRequester() }
    LaunchedEffect(Unit) { withFrameNanos { }; runCatching { fr.requestFocus() } }
    Box(Modifier.fillMaxSize()) {
        LogoMark(Modifier.offset(LEFT.px, 56.px))
        SettingsButton(nav, fr, fr) {}
        Box(Modifier.offset(LEFT.px, 332.px)) { TitleArt(null, title, 620, 232, Alignment.BottomStart, typeSize = 92, typeLine = 86) }
        if (d != null) DownloadBlock(d.progress, etaText(d.eta), Modifier.offset(LEFT.px, 812.px))
    }
}

// ---------- Picture details (board 11) ----------

/**
 * The numbers behind a title, full-screen over its art blurred and darkened (drawn once, then faded in with the
 * sheet). Back closes it.
 */
@Composable
private fun PictureDetails(title: Item, it: Item, spec: ArtSpec, onClose: () -> Unit) {
    val ctx = LocalContext.current
    val art = LocalArt.current
    val scope = rememberCoroutineScope()
    val a = remember { Animatable(0f) }
    var closing by remember { mutableStateOf(false) }
    val blurred = remember(spec) { mutableStateOf(Art.cached(spec.copy(style = ArtStyle.Blurred))) }
    LaunchedEffect(spec) {
        if (blurred.value == null) blurred.value = runCatching { Art.bake(ctx, spec.copy(style = ArtStyle.Blurred), art.w, art.h) }.getOrNull()
        a.animateTo(1f, Move.fade(Move.Screen))
    }
    val close = {
        if (!closing) {
            closing = true
            scope.launch { a.animateTo(0f, Move.fade(Move.Screen)); onClose() }
        }
    }
    BackHandler(onBack = close)
    val fr = remember { FocusRequester() }
    LaunchedEffect(Unit) { withFrameNanos { }; runCatching { fr.requestFocus() } }
    Box(
        Modifier.fillMaxSize().graphicsLayer { alpha = a.value }.background(Pal.Base).focusRequester(fr)
            .onPreviewKeyEvent { e -> e.type == KeyEventType.KeyDown && e.key != Key.Back } // only Back does anything here
            .focusable(),
    ) {
        blurred.value?.let { Image(it.asImageBitmap(), null, Modifier.fillMaxSize()) }
        val info = it.info
        val raw = it.raw.optJSONObject("info")
        val name = if (it.isEpisode) "${title.displayTitle} · S${it.season} E${it.episode}" else title.displayTitle
        Text(name, style = ts(22, 26, 600, Pal.Muted, cond = true), modifier = Modifier.offset(LEFT.px, 180.px))
        Text("Picture details", style = ts(48, 54, 700, track = -0.015f), modifier = Modifier.offset(LEFT.px, 212.px))
        // the four that matter most, big
        Row(Modifier.offset(LEFT.px, 350.px), horizontalArrangement = Arrangement.spacedBy(96.px)) {
            info?.res?.ifEmpty { null }?.let { Figure("Resolution", it, null) }
            Figure("Dynamic range", dynamicRange(info?.hdr), null)
            info?.mbps?.takeIf { m -> m > 0 }?.let { m -> Figure("Bitrate", "%.1f".format(m), "Mb/s") }
            val w = raw?.optInt("w") ?: 0
            val h = raw?.optInt("h") ?: 0
            if (w > 0 && h > 0) Figure("Aspect ratio", "%.2f".format(w.toFloat() / h), ": 1")
        }
        Row(Modifier.offset(LEFT.px, 580.px), horizontalArrangement = Arrangement.spacedBy(110.px)) {
            Column(Modifier.width(380.px)) {
                Text("PICTURE", style = CapsLabel)
                info?.vcodec?.ifEmpty { null }?.let { v -> Spec("Codec", v.substringBefore(" Main").substringBefore(" High")) }
                raw?.optInt("vbits")?.takeIf { b -> b > 0 }?.let { b -> Spec("Bit depth", "$b-bit") }
                info?.fps?.takeIf { f -> f > 0 }?.let { f -> Spec("Frame rate", "%.3f".format(f).trimEnd('0').trimEnd('.') + " fps") }
            }
            val tracks = soundTracks(info?.tracks.orEmpty().ifEmpty { listOfNotNull(info?.audio?.ifEmpty { null }) })
            if (tracks.isNotEmpty()) Column(Modifier.width(320.px)) {
                Text("SOUND", style = CapsLabel)
                tracks.forEachIndexed { i, t ->
                    Column(Modifier.padding(top = 18.px)) {
                        if (i == 0) {
                            Text(t.name, style = ts(24, 30, 700), maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text((listOfNotNull(t.lang) + listOfNotNull(if (t.lossless) "lossless" else null, t.extra) + "default").joinToString(" · "), style = ts(16, 22, 400, Pal.Muted), maxLines = 1)
                        } else {
                            Text(t.name, style = ts(20, 30, 600, Pal.InkSoft), maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text((listOfNotNull(t.lang) + listOfNotNull(if (t.lossless) "lossless" else null, t.extra)).joinToString(" · ").ifEmpty { " " }, style = ts(16, 22, 400, Pal.Faint), maxLines = 1)
                        }
                    }
                }
            }
            val subs = subtitleCounts(info?.subs.orEmpty() + it.raw.optJSONArray("subs").objects().map { s -> s.optString("label") })
            if (subs.isNotEmpty()) Column(Modifier.width(240.px)) {
                Text("SUBTITLES", style = CapsLabel)
                subs.forEach { (lang, n) ->
                    Row(Modifier.padding(top = 16.px)) {
                        Text(lang, style = ts(20, 26, 600, Pal.InkSoft))
                        if (n > 1) Text("×$n", style = ts(20, 26, 500, Pal.Faint), modifier = Modifier.padding(start = 10.px))
                    }
                }
            }
            Column(Modifier.width(320.px)) {
                Text("FILE", style = CapsLabel)
                raw?.optString("container")?.ifEmpty { null }?.let { c -> Spec("Format", c.uppercase()) }
                it.size.takeIf { s -> s > 0 }?.let { s -> Spec("Size", gb(s)) }
                info?.dur?.takeIf { d -> d > 0 }?.let { d -> Spec("Length", span((d * 1000).toLong())) }
                raw?.optJSONArray("chapters")?.length()?.takeIf { n -> n > 1 }?.let { n -> Spec("Chapters", "$n") }
            }
        }
        Row(Modifier.align(Alignment.BottomStart).padding(start = LEFT.px, bottom = 80.px), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Back, null, tint = Pal.Ink, modifier = Modifier.padding(end = 12.px).size(22.px))
            Text("Back", style = ts(19, 24, 600))
            Text("to close", style = ts(19, 24, 500, Pal.Faint), modifier = Modifier.padding(start = 8.px))
        }
    }
}

@Composable
private fun Figure(label: String, value: String, unit: String?) {
    Column {
        Text(label.uppercase(), style = CapsLabel)
        Row(Modifier.padding(top = 12.px), verticalAlignment = Alignment.Bottom) {
            Text(value, style = ts(60, 64, 650, tabular = true, track = -0.02f), maxLines = 1)
            if (unit != null) Text(unit, style = ts(28, 44, 500, Pal.Muted), modifier = Modifier.padding(start = 8.px))
        }
    }
}

@Composable
private fun Spec(name: String, value: String) {
    Row(Modifier.padding(top = 16.px).fillMaxWidth()) {
        Text(name, style = ts(20, 26, 400, Pal.Muted))
        Spacer(Modifier.weight(1f).padding(start = 24.px))
        Text(value, style = ts(20, 26, 600, tabular = true), maxLines = 1)
    }
}

/** "SDR" when there's no HDR; "Dolby Vision", "HDR10", "HDR10+" otherwise. */
private fun dynamicRange(hdr: String?): String = when {
    hdr.isNullOrEmpty() -> "SDR"
    hdr.startsWith("Dolby Vision") -> "Dolby Vision"
    else -> hdr
}

/** One sound track as the sheet shows it. */
class SoundTrack(val name: String, val lang: String?, val lossless: Boolean, val extra: String?, val channels: Int)

/** "DD+ Atmos 5.1 · Hindi" as the server writes a track, read the way a person says it. */
fun soundTrack(s: String): SoundTrack {
    val parts = s.split(" · ")
    val fmt = parts[0]
    val lang = parts.getOrNull(1)?.ifEmpty { null }
    val atmos = fmt.contains("Atmos")
    val layout = Regex("(\\d\\.\\d|stereo|mono)$").find(fmt)?.value
    val codec = fmt.removeSuffix(layout ?: "").replace("Atmos", "").trim()
    val name = when {
        codec == "DD+" -> "Dolby Digital Plus"
        codec == "DD" -> "Dolby Digital"
        else -> codec
    }
    val lossless = listOf("TrueHD", "DTS-HD MA", "FLAC", "PCM", "LPCM", "ALAC").any { codec.startsWith(it) }
    val ch = when (layout) { "7.1" -> 8; "5.1" -> 6; "stereo" -> 2; "mono" -> 1; null -> 0; else -> layout.substringBefore('.').toIntOrNull()?.plus(1) ?: 0 }
    return SoundTrack(listOfNotNull(name.ifEmpty { null }, layout).joinToString(" "), lang, lossless, if (atmos) "Atmos" else null, ch)
}

/** Every track, the file's own first (its default), as the sheet lists them. */
private fun soundTracks(tracks: List<String>) = tracks.map(::soundTrack)

/** The one track worth naming on its own (SPEC.md): lossless above lossy, then more channels. */
fun bestTrack(tracks: List<String>): SoundTrack? =
    tracks.map(::soundTrack).maxWithOrNull(compareBy<SoundTrack> { it.lossless }.thenBy { it.channels })

/** "English" ×2: subtitles by language, in the file's order; a forced track counts with its language. */
private fun subtitleCounts(labels: List<String>): List<Pair<String, Int>> {
    val out = LinkedHashMap<String, Int>()
    labels.filter { it.isNotBlank() }.forEach { l ->
        val lang = l.substringBefore(" (").substringBefore(" | ").trim().let { if (it.equals("Unknown", true) || it.equals("und", true)) "Unlabelled" else it }
        out[lang] = (out[lang] ?: 0) + 1
    }
    return out.toList()
}
