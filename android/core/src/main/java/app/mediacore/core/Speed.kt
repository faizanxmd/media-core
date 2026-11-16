package app.mediacore.core

import android.content.Context
import android.content.SharedPreferences
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.os.SystemClock
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import okhttp3.Request
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/**
 * How fast the server reaches this device, measured in the background so it's known before a title is picked:
 * a few seconds after the app opens, again when the network changes, and again when a title starts and the
 * last result is over 10 minutes old. One result is kept per network (Wi-Fi, mobile data, ethernet; at home
 * or away), so moving between them uses what that network last measured.
 */
object Speed {
    private const val STALE_MS = 10 * 60_000L
    private lateinit var prefs: SharedPreferences
    private var cm: ConnectivityManager? = null
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var job: Job? = null
    @Volatile private var lastKey = ""

    fun start(ctx: Context) {
        if (::prefs.isInitialized) return
        prefs = ctx.applicationContext.getSharedPreferences("speed", Context.MODE_PRIVATE)
        cm = ctx.getSystemService(ConnectivityManager::class.java)
        runCatching {
            cm?.registerDefaultNetworkCallback(object : ConnectivityManager.NetworkCallback() {
                override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                    val k = transport(caps)
                    if (k != lastKey) {
                        lastKey = k
                        measureSoon(3000) // the new network has settled
                    }
                }
            })
        }
        measureSoon(4000) // after Home's pictures, which would otherwise compete with the test
    }

    /** A title is starting: measure again if this network's result is old. Playback doesn't wait for it. */
    fun freshen() {
        if (!::prefs.isInitialized) return
        val at = prefs.getString(key(), null)?.substringAfter(',')?.toLongOrNull() ?: 0L
        if (System.currentTimeMillis() - at > STALE_MS) measureSoon(0)
    }

    /** Mb/s last measured on this network, or null if it hasn't been measured. */
    fun mbps(): Double? {
        if (!::prefs.isInitialized) return null
        return prefs.getString(key(), null)?.substringBefore(',')?.toDoubleOrNull()
    }

    /**
     * Original or Light: the Light copy when the title has one and this connection can't carry the
     * Original with a margin (1.3x its bitrate), or when this network hasn't been measured yet.
     */
    fun useLight(item: JSONObject): Boolean {
        if (item.optJSONObject("light") == null) return false
        val original = item.optJSONObject("info")?.optDouble("mbps", 0.0) ?: 0.0
        if (original <= 0) return false
        val have = mbps() ?: return true
        return have < original * 1.3
    }

    private fun transport(caps: NetworkCapabilities?): String = when {
        caps == null -> "none"
        caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
        caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
        caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "mobile"
        else -> "other"
    }

    private fun key(): String {
        val caps = runCatching { cm?.getNetworkCapabilities(cm?.activeNetwork) }.getOrNull()
        return transport(caps) + if (Api.away) ":away" else ":home"
    }

    private fun measureSoon(delayMs: Long) {
        job?.cancel()
        job = scope.launch {
            delay(delayMs)
            if (Api.base.isEmpty()) Api.discover() ?: return@launch
            measure()?.let { prefs.edit().putString(key(), "%.1f,%d".format(java.util.Locale.ROOT, it, System.currentTimeMillis())).apply() }
        }
    }

    private val client by lazy {
        Api.http.newBuilder().readTimeout(4, TimeUnit.SECONDS).callTimeout(8, TimeUnit.SECONDS).build()
    }

    /**
     * Times a download of test data from the server: the first half second is left out (connections start slowly
     * or in a burst), and it stops after 3 s, or sooner once the answer is clear. Mb/s, or null if it failed.
     */
    private fun measure(): Double? = runCatching {
        val req = Request.Builder().url("${Api.base}/api/v1/speedtest?bytes=${40 shl 20}").build()
        client.newCall(req).execute().use { r ->
            if (!r.isSuccessful) return null
            val input = r.body!!.byteStream()
            val buf = ByteArray(64 * 1024)
            var start = 0L
            var warmBytes = 0L
            var warmAt = 0L
            var total = 0L
            while (true) {
                val n = input.read(buf)
                if (n < 0) break
                val now = SystemClock.elapsedRealtime()
                if (start == 0L) start = now
                total += n
                val t = now - start
                if (warmAt == 0L && t >= 500) { warmAt = now; warmBytes = total }
                if (warmAt > 0) {
                    val secs = (now - warmAt) / 1000.0
                    val rate = if (secs > 0) (total - warmBytes) * 8 / secs / 1e6 else 0.0
                    if (t >= 3000 || secs >= 1.0 && rate > 80) return rate // done, or clearly fast enough for anything
                }
            }
            // ran out of data before 3 s: a very fast connection
            val secs = (SystemClock.elapsedRealtime() - start) / 1000.0
            if (secs > 0) total * 8 / secs / 1e6 else null
        }
    }.getOrNull()
}

/**
 * Notices playback getting stuck: buffering for 15 s at once, or three stalls of a second or more within two
 * minutes. Loading when a film starts or after a skip doesn't count, only running dry while playing.
 */
class StallWatch {
    private var settled = false // playing normally since the last start or skip
    private var since = 0L
    private val stalls = ArrayDeque<Long>()

    /** A new title or a skip: what loads next isn't a stall. */
    fun reset() {
        settled = false
        since = 0L
    }

    fun clear() {
        reset()
        stalls.clear()
    }

    fun state(buffering: Boolean, ready: Boolean, wantsToPlay: Boolean) {
        val now = SystemClock.elapsedRealtime()
        when {
            ready -> { settled = true; since = 0L }
            buffering && settled && wantsToPlay && since == 0L -> { since = now; stalls.addLast(now) }
            !buffering -> {
                if (since > 0 && now - since < 1000 && stalls.lastOrNull() == since) stalls.removeLast() // a blip, not a stall
                since = 0L
            }
        }
    }

    fun stuck(): Boolean {
        val now = SystemClock.elapsedRealtime()
        while (stalls.isNotEmpty() && now - stalls.first() > 120_000) stalls.removeFirst()
        return since > 0 && now - since >= 15_000 || stalls.size >= 3
    }
}
