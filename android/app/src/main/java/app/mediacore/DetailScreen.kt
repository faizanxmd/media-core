package app.mediacore

import androidx.compose.ui.graphics.Color
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

private const val LEFT = EDGE

/** A title page's places: its first screen, and for a show, its episodes (Down). A film is one screen. */
private enum class Place { First, Episodes }

/**
 * A film's or a show's page (boards 04-10): the title's art with its logo, one line of facts, the story, the credits
 * and the buttons. Nothing more: the file's numbers are in Picture details. On a show, Down slides the page up to
 * its seasons and episodes.
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
            Place.Episodes -> -(EPISODES_Y - 110f) * Px.screen
        },
        Move.slide(Move.Screen), label = "page",
    )
    Box(Modifier.fillMaxSize()) {
        Box(Modifier.fillMaxSize().graphicsLayer { translationY = pageY }) {
            SettingsButton(nav, fr("settings"), fr("play")) { focus = "settings" }
            FirstScreen(main, show, target, fr = ::fr, onFocus = { focus = it }, onDown = {
                if (show && main.seasons.isNotEmpty()) go(Place.Episodes, "ep/$season/${epOf(season)}")
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
        SettingsGlass(
            Modifier.align(Alignment.TopEnd).padding(end = EDGE.px, top = TOP.px).focusProperties { this.down = down },
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
    // top of: logo (its foot 24 px over the facts), facts, story (two lines of 40), Starring, buttons
    val y = listOf(0, 500, 548, 650, 780)
    val logoW = if (show) 560 else 500
    val logoH = if (show) 220 else 240
    Box(Modifier.offset(LEFT.px, (y[1] - 24 - logoH).px)) {
        TitleArt(main.logo, main.displayTitle, logoW, logoH, Alignment.BottomStart, typeSize = 84, typeLine = 88)
    }
    val facts = listOfNotNull(
        main.displayYear.takeIf { it > 0 }?.toString(), main.meta?.genres?.firstOrNull(),
        if (show) null else runtimeOf(main)?.let(::span), main.meta?.cert?.ifEmpty { null },
    )
    DotLine(facts, ts(26, 32, 600, Pal.InkSoft, cond = true, track = 0.01f), modifier = Modifier.offset(LEFT.px, y[1].px))
    main.meta?.overview?.ifEmpty { null }?.let {
        Text(it, style = ts(29, 40, 400, Pal.InkSoft), maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.offset(LEFT.px, y[2].px).width(940.px))
    }
    // The credits: who made it, who scored it, and three of the cast. Each column only when TMDB has it.
    val meta = main.meta
    val credits = listOfNotNull(
        (if (show) meta?.creator else meta?.director).orEmpty().take(2).ifEmpty { null }
            ?.let { (if (show) "CREATED BY" else if (it.size > 1) "DIRECTORS" else "DIRECTOR") to it },
        meta?.music.orEmpty().take(1).ifEmpty { null }?.let { "MUSIC" to it },
        meta?.cast.orEmpty().take(3).map { it.name }.ifEmpty { null }?.let { "STARRING" to it },
    )
    if (credits.isNotEmpty()) Row(Modifier.offset(LEFT.px, y[3].px), horizontalArrangement = Arrangement.spacedBy(56.px)) {
        credits.forEach { (label, names) ->
            Column {
                Text(label, style = CapsLabel)
                DotLine(names, ts(29, 36, 600), modifier = Modifier.padding(top = 10.px))
            }
        }
    }
    val started = target != null && target.word == "Resume"
    Row(
        Modifier.offset(LEFT.px, y[4].px).onPreviewKeyEvent { e ->
            if (e.type == KeyEventType.KeyDown && e.key == Key.DirectionDown) { onDown(); true } else false
        },
        horizontalArrangement = Arrangement.spacedBy(16.px), verticalAlignment = Alignment.Top,
    ) {
        val up = Modifier.focusProperties { up = fr("settings") }
        if (target != null) PlayButton(target.word, target.detail, up, fr = fr("play"), onFocus = { if (it) onFocus("play") }) { onPlay(false) }
        if (started) IconButton(Icons.StartOver, "Start over", up, fr = fr("over"), onFocus = { if (it) onFocus("over") }) { onPlay(true) }
        if (target?.item?.info != null || main.info != null) {
            if (target != null) IconButton(Icons.Info, "Picture details", up, fr = fr("details"), onFocus = { if (it) onFocus("details") }, onClick = onDetails)
            else GlassButton("Picture details", Icons.Info, up, weight = 700, fr = fr("details"), onFocus = { if (it) onFocus("details") }, onClick = onDetails)
        }
    }
}

// ---------- a show's seasons and episodes (boards 09 and 10) ----------

// Under a show's first screen only the heading shows, never half a picture: the list starts at the screen's foot
private const val EPISODES_Y = 1024 // "Episodes"; its list starts 56 below
private const val EP_X = 480
private const val EP_W = 1920 - EDGE - EP_X
private const val EP_PITCH = 204 // a 180 px row and 24 under it
private const val SEASON_PITCH = 88 // a 76 px row and 12 under it
private const val SEASON_W = 340

/**
 * "Episodes ⌄", then the seasons down the left and the season's episodes on the right. Laid out under the first
 * screen; Down slides it up into view. The focused episode holds the top slot and its still lifts.
 */
@Composable
private fun EpisodesBlock(
    main: Item, upNext: Item?, season: Int, epOf: (Int) -> Int, active: Boolean, fr: (String) -> FocusRequester,
    onSeason: (Int) -> Unit, onEpisode: (Int, Int) -> Unit, onUp: () -> Unit, onPlay: (Item) -> Unit,
) {
    Row(Modifier.offset(LEFT.px, EPISODES_Y.px), verticalAlignment = Alignment.CenterVertically) {
        Text("Episodes", style = ts(34, 40, 700, cond = true))
        Icon(Icons.Down, null, tint = Pal.Ink, modifier = Modifier.padding(start = 12.px).size(30.px))
    }
    var focusedSeason by remember { mutableStateOf(-1) }
    var focusedEpisode by remember { mutableStateOf<String?>(null) }
    // seasons, as words only: the one in view is named in ink, the others muted; the focused one fills white
    Box(Modifier.offset(LEFT.px, (EPISODES_Y + 56).px)) {
        main.seasons.forEachIndexed { s, it ->
            val name by androidx.compose.animation.animateColorAsState(if (s == season) Pal.Ink else Pal.Muted, Move.fade(Move.Focus), label = "season")
            Row(
                Modifier.offset(y = (s * SEASON_PITCH).px)
                    .size(SEASON_W.px, 76.px).clip(Corner).background(focusFill(focusedSeason == s, Color.Transparent, Pal.Ink))
                    .focusRequester(fr("season/$s"))
                    .onFocusChanged { f -> if (f.isFocused) { focusedSeason = s; onSeason(s) } else if (focusedSeason == s) focusedSeason = -1 }
                    .focusProperties {
                        up = if (s > 0) fr("season/${s - 1}") else FocusRequester.Cancel
                        down = if (s < main.seasons.lastIndex) fr("season/${s + 1}") else FocusRequester.Cancel
                        right = fr("ep/$s/${epOf(s)}")
                        left = FocusRequester.Cancel
                    }
                    .onPreviewKeyEvent { e -> if (e.type == KeyEventType.KeyDown && e.key == Key.DirectionUp && s == 0) { onUp(); true } else false }
                    .focusable()
                    .padding(horizontal = 24.px),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                val on = focusedSeason == s
                val ink = focusFill(on, name, Pal.OnInk)
                Text(it.title, style = ts(29, 36, 700, ink), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                Text(
                    if (it.unseen == 0 && it.episodes.isNotEmpty()) "Watched" else "${it.episodes.size} ep",
                    style = ts(23, 28, 600, focusFill(on, Pal.Muted, Pal.OnInkSoft), tabular = true),
                )
            }
        }
    }
    // the season's episodes: another season crossfades in
    AnimatedContent(
        season, transitionSpec = { fadeIn(Move.fade(Move.Screen)) togetherWith fadeOut(Move.fade(Move.Screen)) }, label = "season",
        modifier = Modifier.offset(EP_X.px, (EPISODES_Y + 56).px),
    ) { s ->
        val eps = main.seasons.getOrNull(s)?.episodes.orEmpty()
        val focusedI = epOf(s)
        val top by animateFloatAsState(-focusedI * EP_PITCH * Px.screen, Move.slide(Move.Screen), label = "list")
        Box(Modifier.graphicsLayer { translationY = top }) {
            eps.forEachIndexed { i, ep ->
                // above the top slot: out of the way of the heading
                val shown by animateFloatAsState(if (i < focusedI) 0f else 1f, Move.fade(Move.Focus), label = "ep")
                val key = "ep/$s/$i"
                EpisodeRow(
                    ep, ep.id == upNext?.id, focusedEpisode == key,
                    Modifier.offset(y = (i * EP_PITCH).px).graphicsLayer { alpha = shown }
                        .focusRequester(fr(key))
                        .onFocusChanged { f -> if (f.isFocused) { focusedEpisode = key; onEpisode(s, i) } else if (focusedEpisode == key) focusedEpisode = null }
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
private fun EpisodeRow(ep: Item, upNext: Boolean, focused: Boolean, modifier: Modifier) {
    Row(modifier.width(EP_W.px).height(180.px), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(320.px, 180.px).lift(ringOf(focused)).softShadow(ShadowKind.Small).clip(Corner).background(Pal.Raised)) {
            ep.wideSmall?.let { AsyncImage(it, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) }
        }
        Column(Modifier.padding(start = 32.px).weight(1f)) {
            // a real name gets "EPISODE 2" over it; a nameless one is just "Episode 2"
            val named = ep.episode > 0 && !ep.title.equals("Episode ${ep.episode}", ignoreCase = true)
            if (named) Text("EPISODE ${ep.episode}", style = CapsLabel, modifier = Modifier.padding(bottom = 6.px))
            Text(ep.title, style = ts(29, 36, 700, track = -0.01f), maxLines = 1, overflow = TextOverflow.Ellipsis)
            ep.meta?.overview?.ifEmpty { null }?.let {
                Text(it, style = ts(23, 31, 400, Pal.Muted), maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.px))
            }
        }
        Column(Modifier.padding(start = 48.px).width(220.px), horizontalAlignment = Alignment.End) {
            if (upNext) Text("Up next", style = ts(23, 28, 700), modifier = Modifier.padding(bottom = 6.px))
            val started = ep.pos > 0 && !ep.watched && ep.durMs > ep.pos
            val len = if (started) leftWords(ep.durMs - ep.pos) else runtimeOf(ep)?.let(::span)
            if (len != null) Text(len, style = ts(25, 30, 600, Pal.InkSoft, tabular = true))
            dateWords(ep.meta?.date)?.let { Text(it, style = ts(23, 28, 400, Pal.Faint), modifier = Modifier.padding(top = 4.px)) }
        }
    }
}

/** "2025-10-16" as "Oct 16, 2025". */
fun dateWords(d: String?): String? = runCatching {
    val p = d!!.split("-").map { it.toInt() }
    val months = listOf("Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec")
    "${months[p[1] - 1]} ${p[2]}, ${p[0]}"
}.getOrNull()

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
        SettingsButton(nav, fr, fr) {}
        Box(Modifier.offset(LEFT.px, 236.px)) { TitleArt(null, title, 500, 240, Alignment.BottomStart, typeSize = 84, typeLine = 88) }
        if (d != null) DownloadBlock(d.progress, etaText(d.eta), Modifier.offset(LEFT.px, 780.px))
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
        Text(name, style = ts(29, 36, 600, Pal.Muted, cond = true), modifier = Modifier.offset(LEFT.px, 140.px))
        Text("Picture details", style = ts(60, 68, 700, track = -0.015f), modifier = Modifier.offset(LEFT.px, 180.px))
        // the four that matter most, big
        Row(Modifier.offset(LEFT.px, 310.px), horizontalArrangement = Arrangement.spacedBy(110.px)) {
            info?.res?.ifEmpty { null }?.let { Figure("Resolution", it, null) }
            Figure("Dynamic range", dynamicRange(info?.hdr), null)
            info?.mbps?.takeIf { m -> m > 0 }?.let { m -> Figure("Bitrate", "%.1f".format(m), "Mb/s") }
            val w = raw?.optInt("w") ?: 0
            val h = raw?.optInt("h") ?: 0
            if (w > 0 && h > 0) Figure("Aspect ratio", "%.2f".format(w.toFloat() / h), ": 1")
        }
        Row(Modifier.offset(LEFT.px, 520.px), horizontalArrangement = Arrangement.spacedBy(80.px)) {
            Column(Modifier.width(400.px)) {
                Text("PICTURE", style = CapsLabel)
                info?.vcodec?.ifEmpty { null }?.let { v -> Spec("Codec", v.substringBefore(" Main").substringBefore(" High")) }
                raw?.optInt("vbits")?.takeIf { b -> b > 0 }?.let { b -> Spec("Bit depth", "$b-bit") }
                info?.fps?.takeIf { f -> f > 0 }?.let { f -> Spec("Frame rate", "%.3f".format(f).trimEnd('0').trimEnd('.') + " fps") }
            }
            val tracks = soundTracks(info?.tracks.orEmpty().ifEmpty { listOfNotNull(info?.audio?.ifEmpty { null }) })
            if (tracks.isNotEmpty()) Column(Modifier.width(380.px)) {
                Text("SOUND", style = CapsLabel)
                tracks.take(5).forEachIndexed { i, t ->
                    Column(Modifier.padding(top = 18.px)) {
                        if (i == 0) {
                            Text(t.name, style = ts(29, 36, 700), maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text((listOfNotNull(t.lang) + listOfNotNull(if (t.lossless) "lossless" else null, t.extra) + "default").joinToString(" · "), style = ts(23, 28, 400, Pal.Muted), maxLines = 1)
                        } else {
                            Text(t.name, style = ts(26, 34, 600, Pal.InkSoft), maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text((listOfNotNull(t.lang) + listOfNotNull(if (t.lossless) "lossless" else null, t.extra)).joinToString(" · ").ifEmpty { " " }, style = ts(23, 28, 400, Pal.Faint), maxLines = 1)
                        }
                    }
                }
                if (tracks.size > 5) Text("and ${tracks.size - 5} more", style = ts(23, 28, 400, Pal.Faint), modifier = Modifier.padding(top = 16.px))
            }
            val subs = subtitleCounts(info?.subs.orEmpty() + it.raw.optJSONArray("subs").objects().map { s -> s.optString("label") })
            if (subs.isNotEmpty()) Column(Modifier.width(260.px)) {
                Text("SUBTITLES", style = CapsLabel)
                subs.take(8).forEach { (lang, n) ->
                    Row(Modifier.padding(top = 16.px)) {
                        Text(lang, style = ts(26, 34, 600, Pal.InkSoft), maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                        if (n > 1) Text("×$n", style = ts(26, 34, 500, Pal.Faint), modifier = Modifier.padding(start = 10.px))
                    }
                }
                if (subs.size > 8) Text("and ${subs.size - 8} more", style = ts(23, 28, 400, Pal.Faint), modifier = Modifier.padding(top = 16.px))
            }
            Column(Modifier.width(400.px)) {
                Text("FILE", style = CapsLabel)
                raw?.optString("container")?.ifEmpty { null }?.let { c -> Spec("Format", c.uppercase()) }
                it.size.takeIf { s -> s > 0 }?.let { s -> Spec("Size", gb(s)) }
                info?.dur?.takeIf { d -> d > 0 }?.let { d -> Spec("Length", span((d * 1000).toLong())) }
                raw?.optJSONArray("chapters")?.length()?.takeIf { n -> n > 1 }?.let { n -> Spec("Chapters", "$n") }
            }
        }
        Row(Modifier.align(Alignment.BottomStart).padding(start = LEFT.px, bottom = TOP.px), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Back, null, tint = Pal.Ink, modifier = Modifier.padding(end = 14.px).size(30.px))
            Text("Back", style = ts(25, 30, 600))
            Text("to close", style = ts(25, 30, 500, Pal.Faint), modifier = Modifier.padding(start = 10.px))
        }
    }
}

@Composable
private fun Figure(label: String, value: String, unit: String?) {
    Column {
        Text(label.uppercase(), style = CapsLabel)
        Row(Modifier.padding(top = 12.px), verticalAlignment = Alignment.Bottom) {
            Text(value, style = ts(76, 80, 650, tabular = true, track = -0.02f), maxLines = 1)
            if (unit != null) Text(unit, style = ts(34, 56, 500, Pal.Muted), modifier = Modifier.padding(start = 10.px))
        }
    }
}

@Composable
private fun Spec(name: String, value: String) {
    Row(Modifier.padding(top = 16.px).fillMaxWidth()) {
        Text(name, style = ts(26, 34, 400, Pal.Muted))
        Spacer(Modifier.weight(1f).padding(start = 24.px))
        Text(value, style = ts(26, 34, 600, tabular = true), maxLines = 1)
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
