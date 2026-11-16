package app.mediacore.phone

import android.content.Context
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.ui.Alignment
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import app.mediacore.core.Api
import app.mediacore.core.Home
import app.mediacore.core.Item
import app.mediacore.core.Offline
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** The last Home seen, so the app opens on it at once. */
fun Nav.restoreHome(cache: java.io.File) {
    if (home != null) return
    runCatching {
        val text = cache.readText()
        home = Home.parse(org.json.JSONObject(text))
        homeJson = text
    }
}

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

fun endsAt(ctx: Context, leftMs: Long): String =
    android.text.format.DateFormat.getTimeFormat(ctx).format(java.util.Date(System.currentTimeMillis() + leftMs))

/** Continue Watching plays straight away; anything else opens its page. */
fun openItem(ctx: Context, scope: CoroutineScope, nav: Nav, it: Item, resume: Boolean = false) {
    when {
        it.isEpisode -> scope.launch { playEpisode(ctx, it) }
        resume && it.type == "movie" -> PlayerActivity.play(ctx, listOf(it), 0)
        // a show's Play starts the episode it's up to
        resume && it.isShow -> scope.launch {
            val next = runCatching { Api.get("/api/v1/item/${it.id}").optJSONObject("up_next")?.let(Item::parse) }.getOrNull()
            if (next != null) playEpisode(ctx, next) else nav.open(Screen.Detail(it.id))
        }
        else -> nav.open(Screen.Detail(it.id))
    }
}

/** Plays an episode with the rest of the show queued behind it (autoplay next). */
suspend fun playEpisode(ctx: Context, ep: Item, fromStart: Boolean = false) {
    val show = runCatching { Item.parse(Api.get("/api/v1/item/${ep.showId}").getJSONObject("item")) }.getOrNull()
    val eps = show?.let(::episodeQueue).orEmpty()
    val i = eps.indexOfFirst { it.id == ep.id }
    if (i < 0) PlayerActivity.play(ctx, listOf(ep), 0, fromStart)
    else PlayerActivity.play(ctx, eps, i, fromStart)
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

/**
 * Nothing to show and the server doesn't answer. Until a server has ever answered, it explains how the app finds one;
 * either way there's a field for its address, for networks the app can't search (guest Wi-Fi, away from home).
 */
@Composable
fun ErrorView(msg: String, onRetry: () -> Unit) {
    var address by remember { mutableStateOf("") }
    fun connect() { if (Api.useAddress(address)) onRetry() }
    Box(Modifier.fillMaxSize().padding(16.dp), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            if (Api.hasServer) {
                Text(Api.unreachable, style = Type.Title)
                Spacer(Modifier.height(12.dp))
                Text("Check that ${Api.name} is on, and that you're on the home Wi-Fi.", style = Type.Body, color = Muted)
                if (msg != Api.unreachable) Text(msg, style = Type.Caption, color = Muted, modifier = Modifier.padding(top = 8.dp))
            } else {
                Text("Looking for your server", style = Type.Title)
                Spacer(Modifier.height(12.dp))
                Text(
                    "Join the same Wi-Fi as the server, or open its download page and tap Open in Media Core.",
                    style = Type.Body, color = Muted, textAlign = TextAlign.Center,
                )
            }
            Spacer(Modifier.height(24.dp))
            Box(
                Modifier.size(40.dp).clip(CardShape).background(Overlay).clickable(onClick = onRetry),
                contentAlignment = Alignment.Center,
            ) { Icon(AppIcons.Refresh, "Try again", tint = Ink, modifier = Modifier.size(24.dp)) }
            Spacer(Modifier.height(32.dp))
            Text("Or type its address", style = Type.Caption, color = Muted)
            Spacer(Modifier.height(8.dp))
            BasicTextField(
                address,
                onValueChange = { address = it },
                singleLine = true,
                textStyle = Type.Body.copy(color = Ink),
                cursorBrush = SolidColor(Ink),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go, autoCorrectEnabled = false),
                keyboardActions = KeyboardActions(onGo = { connect() }),
                modifier = Modifier.width(260.dp).clip(CardShape).background(Overlay).padding(horizontal = 12.dp, vertical = 10.dp),
                decorationBox = { field ->
                    Box {
                        if (address.isEmpty()) Text("192.168.1.20", style = Type.Body, color = Muted)
                        field()
                    }
                },
            )
        }
    }
}
