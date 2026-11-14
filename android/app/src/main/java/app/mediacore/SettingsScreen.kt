package app.mediacore

import android.content.Context
import android.os.SystemClock
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.platform.LocalContext
import app.mediacore.core.Api
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.Request

/** One setting: its name, the values it steps through, and where it's kept. */
private class Setting(val key: String, val name: String, val values: List<String>, val default: String)

private val Watching = listOf(
    Setting("audio", "Audio language", listOf("Original", "English", "Hindi"), "English"),
    Setting("subs", "Subtitles", listOf("Off", "English", "Forced only"), "Off"),
    Setting("autoplay", "Autoplay next episode", listOf("On", "Off"), "On"),
    Setting("credits", "Skip credits", listOf("On", "Off"), "On"),
)
private val PictureSound = listOf(
    Setting("rate", "Match frame rate", listOf("On", "Off"), "On"),
    Setting("range", "Match dynamic range", listOf("On", "Off"), "On"),
    Setting("passthrough", "Audio passthrough", listOf("On", "Off"), "On"),
)

/**
 * Settings (board 12): one list. Up and Down move, Left and Right change the value in place. On the right, read-only,
 * how well mint reaches this TV, measured when the screen opens.
 *
 * The values are kept in "settings"; the player doesn't read them yet (it stays as it is for now).
 */
@Composable
fun SettingsScreen(nav: Nav) {
    val ctx = LocalContext.current
    val art = LocalArt.current
    val prefs = remember { ctx.getSharedPreferences("settings", Context.MODE_PRIVATE) }
    LaunchedEffect(Unit) {
        val h = nav.home?.let { (it.cont + it.recent).firstOrNull() }
        val pic = h?.let { nav.home?.shows?.firstOrNull { s -> s.id == h.showId } ?: h }?.banner
        art.show(ArtSpec(pic, ArtStyle.Blurred), Move.Screen)
    }
    val all = Watching + PictureSound
    val requesters = remember { all.associate { it.key to FocusRequester() } }
    LaunchedEffect(Unit) { withFrameNanos { }; runCatching { requesters.getValue(all.first().key).requestFocus() } }

    Box(Modifier.fillMaxSize()) {
        Text("Settings", style = ts(60, 68, 700, track = -0.015f), modifier = Modifier.offset(EDGE.px, 140.px))
        // the rows' words line up with the edge; a focused row's white reaches 24 px out from them
        Column(Modifier.offset((EDGE - 24).px, 244.px).width(940.px)) {
            Section("Watching", top = 0)
            Watching.forEachIndexed { i, s -> SettingRow(s, prefs, requesters.getValue(s.key), up = all.getOrNull(all.indexOf(s) - 1)?.let { requesters[it.key] }) }
            Section("Picture and sound", top = 30)
            PictureSound.forEach { s -> SettingRow(s, prefs, requesters.getValue(s.key), up = null) }
        }
        Connection(nav, Modifier.offset(1180.px, 244.px).width(660.px))
        Text(
            "Film and show information from TMDB. This app is not endorsed or certified by TMDB.",
            style = ts(23, 28, 400, Pal.Faint), modifier = Modifier.align(Alignment.BottomStart).padding(start = EDGE.px, bottom = TOP.px),
        )
    }
}

@Composable
private fun Section(text: String, top: Int) {
    Text(text, style = ts(29, 36, 700, Pal.Accent, cond = true, track = 0.01f), modifier = Modifier.padding(top = top.px, bottom = 8.px, start = 24.px))
}

/** A row: its name on the left, its value on the right; focused, it turns white with ‹ value › to change. */
@Composable
private fun SettingRow(s: Setting, prefs: android.content.SharedPreferences, fr: FocusRequester, @Suppress("UNUSED_PARAMETER") up: FocusRequester?) {
    var focused by remember { mutableStateOf(false) }
    var index by remember { mutableIntStateOf(s.values.indexOf(prefs.getString(s.key, s.default)).coerceAtLeast(0)) }
    var dir by remember { mutableIntStateOf(1) }
    val fill = focusFill(focused, Pal.Ink.copy(alpha = 0f), Pal.Ink)
    val ink = focusFill(focused, Pal.Ink, Pal.OnInk)
    val step = { d: Int ->
        val n = (index + d).coerceIn(0, s.values.lastIndex)
        if (n != index) {
            dir = d
            index = n
            prefs.edit().putString(s.key, s.values[n]).apply()
        }
    }
    Row(
        Modifier.padding(bottom = 4.px).fillMaxWidth().height(76.px).clip(Corner).background(fill)
            .focusRequester(fr).onFocusChanged { focused = it.isFocused }
            .onPreviewKeyEvent { e ->
                if (e.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
                when (e.key) {
                    Key.DirectionLeft -> { step(-1); true }
                    Key.DirectionRight -> { step(1); true }
                    Key.DirectionCenter, Key.Enter, Key.NumPadEnter -> { step(if (index == s.values.lastIndex) -index else 1); true }
                    else -> false
                }
            }
            .focusable()
            .padding(start = 24.px, end = if (focused) 20.px else 24.px),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(s.name, style = ts(29, 36, 600, ink))
        Spacer(Modifier.weight(1f))
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.px)) {
            if (focused) Icon(Icons.Left, null, tint = Pal.OnInkSoft.copy(alpha = if (index > 0) 1f else 0.35f), modifier = Modifier.size(26.px))
            // a new value slides in about 8 px from the side pressed, fading (150 ms)
            AnimatedContent(
                index,
                transitionSpec = {
                    val dx = { _: Int -> (if (Move.reduced) 0f else 8f * Px.screen).toInt() * dir }
                    (slideInHorizontally(Move.slide(Move.Focus), dx) + fadeIn(Move.fade(Move.Focus))) togetherWith
                        (slideOutHorizontally(Move.slide(Move.Focus)) { -dx(it) } + fadeOut(Move.fade(Move.Focus)))
                },
                label = "value",
            ) { i ->
                Text(
                    s.values[i], style = ts(29, 36, 600, if (focused) Pal.OnInk else Pal.InkSoft),
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                    modifier = if (focused) Modifier.widthIn(min = 56.px) else Modifier,
                )
            }
            if (focused) Icon(Icons.Right, null, tint = Pal.OnInkSoft.copy(alpha = if (index < s.values.lastIndex) 1f else 0.35f), modifier = Modifier.size(26.px))
        }
    }
}

/** How fast mint reaches this TV and how long a request takes, and whether that's enough for the heaviest file. */
@Composable
private fun Connection(nav: Nav, modifier: Modifier) {
    var speed by remember { mutableStateOf<Double?>(null) }
    var delay by remember { mutableStateOf<Long?>(null) }
    LaunchedEffect(Unit) {
        withContext(Dispatchers.IO) {
            delay = runCatching { (1..3).minOf { ping() } }.getOrNull()
            speed = runCatching { measure() }.getOrNull()
        }
    }
    val heaviest = nav.home?.let { h -> (h.cont + h.recent + h.movies).mapNotNull { it.info?.mbps }.maxOrNull() }
    Column(modifier) {
        Text("Connection to mint", style = ts(29, 36, 700, Pal.Accent, cond = true, track = 0.01f), modifier = Modifier.padding(bottom = 8.px))
        Row(Modifier.padding(top = 10.px), horizontalArrangement = Arrangement.spacedBy(80.px)) {
            Reading(speed?.let { "%.0f".format(it) } ?: "–", "Mb/s", "Speed")
            Reading(delay?.toString() ?: "–", "ms", "Delay")
        }
        val verdict = when {
            speed == null -> "Measuring…"
            heaviest == null -> null
            speed!! >= heaviest * 1.3 && (speed!! >= 60 || nav.home?.movies?.any { it.info?.res == "4K" } == true) -> "Good for 4K streaming"
            speed!! >= heaviest * 1.3 -> "Good for every file"
            else -> "Too slow for the heaviest file (%.0f Mb/s)".format(heaviest)
        }
        if (verdict != null) Text(verdict, style = ts(29, 36, 650), modifier = Modifier.padding(top = 32.px))
    }
}

@Composable
private fun Reading(value: String, unit: String, name: String) {
    Column {
        Row(verticalAlignment = Alignment.Bottom) {
            Text(value, style = ts(80, 84, 650, tabular = true, track = -0.02f))
            Text(unit, style = ts(32, 62, 500, Pal.Muted), modifier = Modifier.padding(start = 10.px))
        }
        Text(name, style = ts(25, 30, 400, Pal.Muted), modifier = Modifier.padding(top = 4.px))
    }
}

/** One small request's round trip, in ms. */
private fun ping(): Long {
    val t = SystemClock.elapsedRealtime()
    Api.http.newCall(Request.Builder().url("${Api.base}/api/v1/hello").build()).execute().use { it.body?.bytes() }
    return SystemClock.elapsedRealtime() - t
}

/** Megabits per second over a 16 MB download from mint's speed test. */
private fun measure(): Double {
    val bytes = 16L shl 20
    val t = SystemClock.elapsedRealtime()
    Api.http.newCall(Request.Builder().url("${Api.base}/api/v1/speedtest?bytes=$bytes").build()).execute().use { r ->
        val s = r.body!!.byteStream()
        val buf = ByteArray(64 shl 10)
        while (s.read(buf) >= 0) Unit
    }
    val sec = (SystemClock.elapsedRealtime() - t).coerceAtLeast(1) / 1000.0
    return bytes * 8 / sec / 1e6
}
