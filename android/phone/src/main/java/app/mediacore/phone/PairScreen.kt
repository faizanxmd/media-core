package app.mediacore.phone

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.mediacore.core.Api
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay

/**
 * The server wants this phone to pair. It shows a code; someone signed in to the server's website as its admin
 * enters it under Settings, Devices (or taps the button, if this phone's browser is that admin), and the app goes
 * on to Home by itself.
 */
@Composable
fun PairScreen(onPaired: () -> Unit) {
    val ctx = LocalContext.current
    var code by remember { mutableStateOf<Api.PairCode?>(null) }
    var note by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        while (true) {
            val c = try {
                Api.pairStart("phone", Api.deviceName(ctx))
            } catch (e: Exception) {
                if (e is CancellationException) throw e
                code = null
                note = e.message ?: Api.unreachable
                delay(5000)
                continue
            }
            code = c
            note = null
            val end = System.currentTimeMillis() + c.expiresIn * 1000L
            var state = "pending"
            while (state == "pending" && System.currentTimeMillis() < end) {
                delay(c.interval * 1000L)
                state = Api.pairPoll(c.poll)
            }
            if (state == "approved") { onPaired(); return@LaunchedEffect }
            if (state == "denied") note = "That code was turned down. Here's a new one."
        }
    }

    Box(Modifier.fillMaxSize().background(Base).safeDrawingPadding().padding(horizontal = Side), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text("Pair with ${Api.name}", style = Type.Display, textAlign = TextAlign.Center)
            Spacer(Modifier.height(12.dp))
            val c = code
            Text(
                if (c != null) "On the server's website, go to Settings, Devices and enter this code."
                else "Getting a code from ${Api.name}…",
                style = Type.Body, color = Muted, textAlign = TextAlign.Center,
            )
            if (c != null) {
                Spacer(Modifier.height(32.dp))
                Text(
                    c.code,
                    style = Type.Display.figures().copy(fontSize = 44.sp, lineHeight = 52.sp, letterSpacing = 4.sp),
                    modifier = Modifier.semantics { contentDescription = "Code: " + c.code.filter { it != '-' }.toList().joinToString(" ") },
                )
                Spacer(Modifier.height(8.dp))
                Text(c.url.substringBefore("/#"), style = Type.Meta, color = Muted)
                Spacer(Modifier.height(32.dp))
                // this phone's own browser may be the server's admin: then one tap approves it
                WordButton("Open the website") {
                    runCatching { ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(c.url))) }
                }
            }
            Spacer(Modifier.height(24.dp))
            Text(
                note ?: if (c != null) "Waiting for approval" else "",
                style = Type.Meta, color = Muted, textAlign = TextAlign.Center,
                modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite },
            )
        }
    }
}
