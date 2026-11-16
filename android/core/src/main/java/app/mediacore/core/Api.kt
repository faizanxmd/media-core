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
 * Talks to the engine's player API on the home network. The app races every address it knows. It learns them from
 * the server itself (/hello lists them all), from a broadcast on the home network, from a connect link
 * (mediacore://connect?u=…, on the server's download page), or from an address typed on the "can't find" screen.
 * Reaching the server away from home isn't supported yet.
 */
object Api {
    val http: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(4, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .build()

    private val probe = http.newBuilder()
        .connectTimeout(1500, TimeUnit.MILLISECONDS)
        .readTimeout(2500, TimeUnit.MILLISECONDS)
        .callTimeout(4, TimeUnit.SECONDS) // also bounds DNS (a typed name that doesn't resolve)
        .build()

    /** The player API's port, and the one the server answers broadcasts on. */
    const val PORT = 8790

    @Volatile var base: String = ""
        private set
    /** What the server calls itself (its host name unless set otherwise), for "… is offline" and the like. */
    @Volatile var name: String = "the server"
        private set
    private lateinit var prefs: SharedPreferences
    private lateinit var app: Context
    private val discoverLock = Mutex()

    fun init(ctx: Context) {
        app = ctx.applicationContext
        prefs = ctx.getSharedPreferences("media-core", Context.MODE_PRIVATE)
        base = prefs.getString("base", "") ?: ""
        prefs.getString("name", null)?.let { name = it }
        // installs from before the server sent its name: ask once, quietly
        if (base.isNotEmpty() && !prefs.contains("name")) {
            http.newCall(Request.Builder().url("$base/api/v1/hello").build()).enqueue(object : okhttp3.Callback {
                override fun onFailure(call: okhttp3.Call, e: IOException) {}
                override fun onResponse(call: okhttp3.Call, response: okhttp3.Response) {
                    response.use { r -> runCatching { if (r.isSuccessful) learn(JSONObject(r.body!!.string())) } }
                }
            })
        }
    }

    /** The name at the start of a sentence: "The server is offline", "the server is offline". */
    val nameAtStart: String get() = if (name == "the server") "The server" else name

    /** Whether a server has ever answered this app (the offline screen says "Looking for your server" until one has). */
    val hasServer: Boolean get() = base.isNotEmpty() || prefs.contains("name")

    /** Can't reach the server: the message every caller shows. */
    val unreachable: String get() = "Can't reach $name"

    /** The server is reached by an address outside the home network (a typed name, say): treat it as a thin link. */
    val away: Boolean get() = !isLan(base)

    /** The host of an address like "http://192.168.1.5:8790"; empty if it carries a user part ("a@b"). */
    private fun hostOf(u: String): String {
        val rest = u.substringAfter("://").substringBefore("/")
        return if ('@' in rest) "" else rest.substringBefore(":")
    }

    /** The four numbers of an IPv4 address, or null if the host isn't one. */
    private fun ipv4(host: String): List<Int>? {
        val p = host.split(".")
        if (p.size != 4) return null
        val n = p.map { it.toIntOrNull() ?: return null }
        return if (n.all { it in 0..255 }) n else null
    }

    private fun isLan(u: String): Boolean {
        val p = ipv4(hostOf(u)) ?: return false
        return p[0] == 10 || (p[0] == 192 && p[1] == 168) || (p[0] == 172 && p[1] in 16..31)
    }

    /**
     * A connect link from the server's download page: mediacore://connect?u=<address>&u=… . Only home-network
     * addresses are taken, so a link on some other web page can't point the app at a server on the internet.
     */
    fun connect(uri: android.net.Uri?): Boolean {
        if (uri == null || uri.scheme != "mediacore" || uri.host != "connect") return false
        val us = uri.getQueryParameters("u").map { it.trimEnd('/') }
            .filter { (it.startsWith("http://") || it.startsWith("https://")) && isLan(it) }
        if (us.isEmpty()) return false
        forget(us.toSet())
        return true
    }

    /** An address typed on the "can't find" screen: "192.168.1.5", "myserver:8790" or a full URL. */
    fun useAddress(text: String): Boolean {
        var u = text.trim().trimEnd('/')
        if (u.isEmpty()) return false
        if ("://" !in u) u = "http://$u"
        val scheme = u.substringBefore("://")
        val host = u.substringAfter("://").substringBefore('/')
        if (scheme != "http" && scheme != "https") return false
        // a plain address means the apps' port; https keeps its own default
        u = "$scheme://$host" + if (scheme == "http" && ':' !in host) ":$PORT" else ""
        forget(setOf(u))
        return true
    }

    /** Starts over with just these addresses; the next request finds which one answers. */
    private fun forget(addresses: Set<String>) {
        base = ""
        name = "the server" // it may be another server: its own /hello names it
        prefs.edit().putString("base", "").putStringSet("known", addresses).remove("name").apply()
    }

    /** True while a search for the server is running (the offline screen waits for it before trying again). */
    val searching: Boolean get() = discoverLock.isLocked

    /** Finds a server address that answers; a home-network one wins if it replies within ~1 s of another. */
    suspend fun discover(): String? = discoverLock.withLock {
        val known = (listOf(prefs.getString("base", "") ?: "") +
            (prefs.getStringSet("known", emptySet()) ?: emptySet()))
            .filter { it.isNotBlank() }.distinct()
        // nothing saved answers (or nothing saved yet): ask the home network
        val found = race(known) ?: withContext(Dispatchers.IO) { broadcast() }.let { race(it) }
        if (found != null) {
            base = found
            prefs.edit().putString("base", found).apply()
        }
        found
    }

    private suspend fun race(known: List<String>): String? {
        if (known.isEmpty()) return null
        return withContext(Dispatchers.IO) {
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
    }

    /**
     * Asks the home network for a server: "media-core?" to the broadcast address of each Wi-Fi or Ethernet network,
     * and the server answers with the same addresses /hello lists. Bound to that network, so a VPN on the phone
     * doesn't carry it off. About 1.5 s; nothing found is an empty list.
     */
    private fun broadcast(): List<String> {
        val cm = app.getSystemService(android.net.ConnectivityManager::class.java) ?: return emptyList()
        val out = linkedSetOf<String>()
        @Suppress("DEPRECATION")
        for (net in cm.allNetworks) {
            val caps = cm.getNetworkCapabilities(net) ?: continue
            if (!caps.hasTransport(android.net.NetworkCapabilities.TRANSPORT_WIFI) &&
                !caps.hasTransport(android.net.NetworkCapabilities.TRANSPORT_ETHERNET)) continue
            // a VPN reports the Wi-Fi under it too; broadcasting into it finds nothing
            if (!caps.hasCapability(android.net.NetworkCapabilities.NET_CAPABILITY_NOT_VPN)) continue
            val lp = cm.getLinkProperties(net) ?: continue
            for (la in lp.linkAddresses) {
                val a = la.address as? java.net.Inet4Address ?: continue
                runCatching { ask(net, broadcastOf(a, la.prefixLength), out) }
            }
        }
        return out.toList()
    }

    private fun ask(net: android.net.Network, to: java.net.InetAddress, out: MutableSet<String>) {
        java.net.DatagramSocket().use { s ->
            net.bindSocket(s)
            s.broadcast = true
            s.soTimeout = 250
            val msg = "media-core?".toByteArray()
            val end = System.currentTimeMillis() + 1500
            var sent = 0
            val buf = ByteArray(4096)
            while (System.currentTimeMillis() < end) {
                if (sent < 2) { s.send(java.net.DatagramPacket(msg, msg.size, to, PORT)); sent++ } // twice: UDP can drop one
                val p = java.net.DatagramPacket(buf, buf.size)
                try { s.receive(p) } catch (e: java.net.SocketTimeoutException) { continue }
                val j = runCatching { JSONObject(String(p.data, 0, p.length)) }.getOrNull() ?: continue
                out += "http://${p.address.hostAddress}:$PORT" // where it answered from comes first
                out += addresses(j)
                break // one answer is enough
            }
        }
    }

    private fun broadcastOf(a: java.net.Inet4Address, prefix: Int): java.net.InetAddress {
        val b = a.address
        val ip = ((b[0].toInt() and 255) shl 24) or ((b[1].toInt() and 255) shl 16) or
            ((b[2].toInt() and 255) shl 8) or (b[3].toInt() and 255)
        val mask = if (prefix <= 0) 0 else -1 shl (32 - prefix.coerceAtMost(32))
        val bc = ip or mask.inv()
        return java.net.InetAddress.getByAddress(byteArrayOf((bc ushr 24).toByte(), (bc ushr 16).toByte(), (bc ushr 8).toByte(), bc.toByte()))
    }

    /** Every address a /hello answer lists (its home-network addresses). */
    private fun addresses(j: JSONObject): Set<String> {
        val all = linkedSetOf<String>()
        val a = j.optJSONArray("lan") ?: return all
        for (i in 0 until a.length()) all += a.getString(i)
        return all
    }

    /** Keeps what a /hello answer says: every address, and the server's name. */
    private fun learn(j: JSONObject) {
        val e = prefs.edit()
        val all = addresses(j)
        if (all.isNotEmpty()) e.putStringSet("known", all)
        j.optString("name").takeIf { it.isNotBlank() }?.let { name = it; e.putString("name", it) }
        e.apply()
    }

    private fun hello(u: String): Boolean = try {
        probe.newCall(Request.Builder().url("$u/api/v1/hello").build()).execute().use { r ->
            if (!r.isSuccessful) return false
            learn(JSONObject(r.body!!.string()))
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
        if (base.isEmpty()) discover() ?: throw IOException(unreachable)
        return try {
            exec(build(base))
        } catch (e: IOException) {
            discover() ?: throw IOException(unreachable)
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
        if (base.isEmpty()) return // no server yet: offline positions are kept and sent later (Offline.sync)
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
