package app.mediacore.core

import android.content.Context
import android.content.SharedPreferences
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancelChildren
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * Talks to the engine's player API on mint. At home that's the LAN address
 * (full speed, no VPN); away it's the Tailscale address. The app races all
 * known addresses and prefers the LAN one, so it never needs a settings page.
 */
object Api {
    val http: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(4, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .build()

    private val probe = http.newBuilder()
        .connectTimeout(1500, TimeUnit.MILLISECONDS)
        .readTimeout(2500, TimeUnit.MILLISECONDS)
        .callTimeout(4, TimeUnit.SECONDS) // also bounds DNS (MagicDNS when Tailscale is off)
        .build()

    // Baked-in guesses for the very first start; /hello replaces them.
    private val defaults = listOf(
        "http://192.168.68.101:8790",
        "http://100.75.19.19:8790",
        "http://mint.tail25d09d.ts.net:8790",
    )

    @Volatile var base: String = ""
        private set
    private lateinit var prefs: SharedPreferences
    private val discoverLock = Mutex()

    fun init(ctx: Context) {
        prefs = ctx.getSharedPreferences("media-core", Context.MODE_PRIVATE)
        base = prefs.getString("base", "") ?: ""
    }

    val viaTailscale: Boolean get() = !isLan(base)

    private fun isLan(u: String): Boolean {
        val host = u.substringAfter("://").substringBefore(":").substringBefore("/")
        return host.startsWith("192.168.") || host.startsWith("10.") ||
            Regex("""^172\.(1[6-9]|2\d|3[01])\.""").containsMatchIn(host)
    }

    /** Finds a server address that answers; LAN wins if it replies within ~1 s of a Tailscale reply. */
    suspend fun discover(): String? = discoverLock.withLock {
        val known = (listOf(prefs.getString("base", "") ?: "") +
            (prefs.getStringSet("known", emptySet()) ?: emptySet()) + defaults)
            .filter { it.isNotBlank() }.distinct()
        val found = withContext(Dispatchers.IO) {
            coroutineScope {
                val ch = Channel<String?>(known.size)
                known.forEach { u -> launch { ch.send(if (hello(u)) u else null) } }
                var fallback: String? = null
                var fallbackAt = 0L
                var pick: String? = null
                var left = known.size
                while (left > 0 && pick == null) {
                    val wait = if (fallback != null) maxOf(1L, 1000 - (System.currentTimeMillis() - fallbackAt)) else 6000L
                    val got = withTimeoutOrNull(wait) { listOf(ch.receive()) }
                        ?: break // waited long enough (for the LAN, or for anyone)
                    left--
                    val r = got[0] ?: continue
                    if (isLan(r)) pick = r
                    else if (fallback == null) { fallback = r; fallbackAt = System.currentTimeMillis() }
                }
                coroutineContext.cancelChildren()
                pick ?: fallback
            }
        }
        if (found != null) {
            base = found
            prefs.edit().putString("base", found).apply()
        }
        found
    }

    private fun hello(u: String): Boolean = try {
        probe.newCall(Request.Builder().url("$u/api/v1/hello").build()).execute().use { r ->
            if (!r.isSuccessful) return false
            val j = JSONObject(r.body!!.string())
            val all = mutableSetOf<String>()
            for (k in listOf("lan", "tailscale")) {
                val a = j.optJSONArray(k) ?: continue
                for (i in 0 until a.length()) all += a.getString(i)
            }
            j.optString("magicdns").takeIf { it.isNotEmpty() }?.let { all += it }
            if (all.isNotEmpty()) prefs.edit().putStringSet("known", all).apply()
            true
        }
    } catch (e: Exception) {
        false
    }

    /** GET with one rediscovery + retry when the current address stopped answering (left home, etc.). */
    suspend fun get(path: String): JSONObject = call { b ->
        Request.Builder().url(b + path).build()
    }

    /** A list of films or shows, parsed away from the UI thread. */
    suspend fun items(path: String): List<Item> {
        val j = get(path)
        return withContext(Dispatchers.Default) { j.optJSONArray("items").objects().map(Item::parse) }
    }

    suspend fun post(path: String, body: JSONObject): JSONObject = call { b ->
        Request.Builder().url(b + path)
            .post(body.toString().toRequestBody("application/json".toMediaType())).build()
    }

    private suspend fun call(build: (String) -> Request): JSONObject {
        if (base.isEmpty()) discover() ?: throw IOException("Can't reach mint")
        return try {
            exec(build(base))
        } catch (e: IOException) {
            discover() ?: throw IOException("Can't reach mint")
            exec(build(base))
        }
    }

    private suspend fun exec(req: Request): JSONObject = withContext(Dispatchers.IO) {
        http.newCall(req).execute().use { r ->
            val s = r.body?.string() ?: ""
            if (!r.isSuccessful) {
                val msg = runCatching { JSONObject(s).optString("error") }.getOrNull()
                throw ServerError(if (msg.isNullOrEmpty()) "HTTP ${r.code}" else msg)
            }
            JSONObject(s)
        }
    }

    class ServerError(msg: String) : Exception(msg)

    fun img(path: String?, size: String): String? =
        if (path.isNullOrEmpty() || base.isEmpty()) null else "$base/api/v1/img/$size$path"

    fun stream(id: String): String = "$base/api/v1/stream/$id"
    fun sub(id: String, n: Int): String = "$base/api/v1/sub/$id/$n"

    private val sending = java.util.concurrent.atomic.AtomicInteger()

    /** Waits briefly for positions still on their way, so what's asked for next already includes them. */
    suspend fun settled() {
        withTimeoutOrNull(1500) { while (sending.get() > 0) kotlinx.coroutines.delay(25) }
    }

    fun progress(id: String, pos: Long, dur: Long, ended: Boolean) {
        val body = JSONObject().put("id", id).put("pos", pos).put("dur", dur).put("ended", ended)
        // fire-and-forget on OkHttp's own threads; works even while the activity is finishing
        sending.incrementAndGet()
        http.newCall(
            Request.Builder().url("$base/api/v1/progress")
                .post(body.toString().toRequestBody("application/json".toMediaType())).build()
        ).enqueue(object : okhttp3.Callback {
            override fun onFailure(call: okhttp3.Call, e: IOException) { sending.decrementAndGet() }
            override fun onResponse(call: okhttp3.Call, response: okhttp3.Response) { response.close(); sending.decrementAndGet() }
        })
    }
}

fun JSONArray?.objects(): List<JSONObject> =
    if (this == null) emptyList() else (0 until length()).map { getJSONObject(it) }

fun JSONArray?.strings(): List<String> =
    if (this == null) emptyList() else (0 until length()).map { getString(it) }
