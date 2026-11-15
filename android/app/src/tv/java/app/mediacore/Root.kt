package app.mediacore

import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.focusable
import androidx.compose.foundation.gestures.LocalBringIntoViewSpec
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.offset
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.delay

/**
 * The TV app: Home is the whole app, with title pages and Settings opened from it. Under every screen lies one
 * picture (ArtLayer) that each screen asks for; the screens themselves fade through over it (300 ms), so a picture two
 * screens share stays where it is.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun Root(nav: Nav, tv: Boolean, onUpdate: () -> Unit) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val art = remember { ArtState(ctx.applicationContext, scope) }
    CompositionLocalProvider(LocalBringIntoViewSpec provides rememberMinimalSpec(), LocalArt provides art) {
        Box(
            // no fill here: ArtLayer paints the base colour itself when it has no picture, so a frame paints the screen once
            Modifier.fillMaxSize().onSizeChanged { art.w = it.width; art.h = it.height },
        ) {
            HomeData(nav)
            ArtLayer(art)
            BackHandler(enabled = nav.stack.size > 1) { nav.back() }
            AnimatedContent(
                nav.stack.last(),
                transitionSpec = { Move.through() },
                label = "screen",
            ) { s ->
                when (s) {
                    is Screen.Detail -> TitlePage(nav, s.id)
                    is Screen.Pending -> PendingPage(nav, s.title)
                    Screen.Settings -> SettingsScreen(nav)
                    Screen.Home -> HomeScreen(nav, onUpdate)
                }
            }
            Offline(nav)
            Intro(nav)
            // into and out of the player: the whole screen goes to black and comes back from it
            Black(nav)
        }
    }
}

/**
 * Opening the app: the wordmark, centred on the dark ground Android's splash already showed, until Home has its
 * titles (at least 600 ms, so it reads as a beat and not a flash; at most 2.5 s), then it fades away into Home.
 */
@Composable
private fun Intro(nav: Nav) {
    if (nav.introDone) return
    val a = remember { androidx.compose.animation.core.Animatable(1f) }
    LaunchedEffect(Unit) {
        val start = System.nanoTime()
        while (nav.home == null && !nav.offline && System.nanoTime() - start < 2_500_000_000L) delay(50)
        val waited = (System.nanoTime() - start) / 1_000_000
        if (waited < 600) delay(600 - waited)
        a.animateTo(0f, Move.fade(Move.Through))
        nav.introDone = true
    }
    Box(Modifier.fillMaxSize().fadeBy { a.value }.background(Pal.Base), contentAlignment = androidx.compose.ui.Alignment.Center) {
        Wordmark(height = 120.px)
    }
}

/** The black into and out of the player. Its own small composable, so its fade doesn't recompose all of Root. */
@Composable
private fun Black(nav: Nav) {
    if (nav.black.value > 0f) Box(Modifier.fillMaxSize().fadeBy { nav.black.value }.background(Color.Black))
}

/** "mint is offline", over everything, until mint answers again; then it fades back to the app. */
@Composable
private fun Offline(nav: Nav) {
    val a by animateFloatAsState(if (nav.offline) 1f else 0f, Move.fade(Move.Screen), label = "offline")
    if (a == 0f) return
    val fr = remember { FocusRequester() }
    // Back does nothing while it's up: it would otherwise leave screens, or ask to quit, hidden under it
    BackHandler(enabled = nav.offline) { }
    // it holds focus while it's up, so the remote can't move around the app hidden under it
    LaunchedEffect(nav.offline) { if (nav.offline) { delay(16); runCatching { fr.requestFocus() } } }
    LaunchedEffect(nav.offline) {
        while (nav.offline) {
            delay(5000)
            runCatching { nav.refresh++ }
        }
    }
    Box(Modifier.fillMaxSize().fadeBy { a }.background(Pal.Base)
        .focusProperties { up = FocusRequester.Cancel; down = FocusRequester.Cancel; left = FocusRequester.Cancel; right = FocusRequester.Cancel }
        .focusRequester(fr).focusable()) {
        GrainPlain()
        Text("mint is offline", style = ts(60, 68, 700, track = -0.015f), modifier = Modifier.offset(EDGE.px, 460.px))
    }
}

/** The base colour with the grain on it: board 13, and what's under a page that has no picture. */
@Composable
fun GrainPlain() {
    val ctx = LocalContext.current
    val art = LocalArt.current
    val spec = ArtSpec(null, ArtStyle.Plain)
    val bmp = remember { androidx.compose.runtime.mutableStateOf<android.graphics.Bitmap?>(Art.cached(spec)) }
    LaunchedEffect(Unit) { if (bmp.value == null) bmp.value = Art.bake(ctx, spec, art.w, art.h) }
    bmp.value?.let { b -> androidx.compose.foundation.Image(b.asImageBitmap(), null, Modifier.fillMaxSize()) }
}
