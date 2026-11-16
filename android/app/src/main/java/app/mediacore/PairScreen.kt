package app.mediacore

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.semantics
import app.mediacore.core.Api
import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.qrcode.QRCodeWriter
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay

/**
 * The server wants this TV to pair. It shows a code and a QR code for the approval page; someone signed in to the
 * server's website (as its admin) approves it there, and the TV goes on to Home by itself. Nothing here takes
 * focus, so the remote has nothing to get lost in: Back leaves the app as usual.
 */
@Composable
fun PairScreen(onPaired: () -> Unit) {
    val ctx = LocalContext.current
    var code by remember { mutableStateOf<Api.PairCode?>(null) }
    var note by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        while (true) {
            val c = try {
                Api.pairStart("tv", Api.deviceName(ctx))
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

    Box(Modifier.fillMaxSize().background(Pal.Base).padding(horizontal = EDGE.px, vertical = TOP.px)) {
        Wordmark(Modifier.align(Alignment.TopStart))
        Row(Modifier.align(Alignment.Center), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.width(900.px)) {
                Text("Pair this TV with ${Api.name}", style = ts(56, 64, 700))
                Spacer(Modifier.height(24.px))
                val c = code
                Text(
                    if (c != null) "Scan this with your phone, or open ${c.url.substringBefore("/#")}/#/devices on a computer and enter the code."
                    else "Getting a code from ${Api.name}…",
                    style = ts(30, 42, 500, Pal.Muted),
                )
                Spacer(Modifier.height(40.px))
                if (c != null) Text(
                    c.code,
                    style = ts(96, 104, 700, track = 0.12f, tabular = true),
                    // read out one character at a time, not as a word
                    modifier = Modifier.semantics { contentDescription = "Code: " + c.code.filter { it != '-' }.toList().joinToString(" ") },
                )
                Spacer(Modifier.height(40.px))
                Text(
                    note ?: if (c != null) "Waiting for approval. The code changes every ${c.expiresIn / 60} minutes." else "",
                    style = ts(26, 36, 500, if (note != null) Pal.AccentText else Pal.Faint),
                    modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite },
                )
            }
            code?.let { c ->
                Spacer(Modifier.width(96.px))
                Qr(c.url, Modifier.size(400.px))
            }
        }
    }
}

/** A QR code, dark on a white tile with a quiet border, as every scanner expects. Screen readers skip it: the address is in the words. */
@Composable
private fun Qr(text: String, modifier: Modifier) {
    val bits = remember(text) {
        runCatching { QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, 0, 0, mapOf(EncodeHintType.MARGIN to 0)) }.getOrNull()
    } ?: return
    Box(modifier.clip(RoundedCornerShape(16.px)).background(Color.White).padding(24.px).clearAndSetSemantics {}) {
        Canvas(Modifier.fillMaxSize()) {
            val n = bits.width
            val cell = size.minDimension / n
            for (y in 0 until n) for (x in 0 until n) {
                if (bits[x, y]) drawRect(Color.Black, Offset(x * cell, y * cell), Size(cell + 0.5f, cell + 0.5f))
            }
        }
    }
}
