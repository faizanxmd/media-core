package app.mediacore.core

import android.content.Context
import android.content.SharedPreferences
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancelChildren
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
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
    private val plain: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(4, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .build()

    /**
     * Every request the app makes. It carries this device's key to the server, and only once that server has
     * shown it knows the key ([proven]): another box answering on the same address (a different Wi-Fi, say)
     * never sees it.
     */
    val http: OkHttpClient = plain.newBuilder()
        .addInterceptor { chain ->
            val req = chain.request()
            val tok = token
            val o = origin(req.url)
            if (tok.isEmpty() || !isServer(o) || proven(o, tok) != Proof.OK) chain.proceed(req)
            else chain.proceed(req.newBuilder().header("Authorization", "Bearer $tok").build())
        }
        .build()

    // without the key: finding the server and the proof itself
    private val probe = plain.newBuilder()
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
    /** This device's key ("mc_…"), from pairing. Empty until the server has approved this device. */
    @Volatile private var token: String = ""
    /** The server wants a key this app hasn't got: the app shows its pairing screen until this turns false. */
    var needsPair by mutableStateOf(false)
        private set
    private lateinit var prefs: SharedPreferences
    private lateinit var app: Context
    private val discoverLock = Mutex()

    fun init(ctx: Context) {
        app = ctx.applicationContext
        prefs = ctx.getSharedPreferences("media-core", Context.MODE_PRIVATE)
        base = prefs.getString("base", "") ?: ""
        token = prefs.getString("token", "") ?: ""
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

    private fun origin(u: okhttp3.HttpUrl) = "${u.scheme}://${u.host}:${u.port}"
    private fun origin(u: String) = u.toHttpUrlOrNull()?.let(::origin) ?: u

    // ---------- the server proves it knows this device's key before the key is sent ----------

    enum class Proof { OK, UNKNOWN, FAILED } // UNKNOWN: it answered, but doesn't know this key (revoked, or another server)

    private class Proved(val proof: Proof, val at: Long)
    private val proofs = java.util.concurrent.ConcurrentHashMap<String, Proved>()

    /**
     * Whether the server at origin [o] knows [tok]. The answer is kept a few minutes (half a minute when it
     * couldn't be asked), then asked again, so moving to another network with the same addresses is noticed.
     */
    private fun proven(o: String, tok: String): Proof {
        fun fresh(p: Proved?) = p != null &&
            android.os.SystemClock.elapsedRealtime() - p.at < if (p.proof == Proof.FAILED) 30_000L else 300_000L
        proofs[o]?.let { if (fresh(it)) return it.proof }
        synchronized(proofs) {
            proofs[o]?.let { if (fresh(it)) return it.proof }
            val p = prove(o, tok)
            proofs[o] = Proved(p, android.os.SystemClock.elapsedRealtime())
            return p
        }
    }

    /** Sends a random nonce and the start of the key's hash; the server answers HMAC-SHA256(nonce) keyed by the hash. */
    private fun prove(o: String, tok: String): Proof = try {
        val h = sha256Hex(tok)
        val n = ByteArray(16).also { java.security.SecureRandom().nextBytes(it) }.joinToString("") { "%02x".format(it) }
        probe.newCall(Request.Builder().url("$o/api/v1/prove?k=${h.take(16)}&n=$n").build()).execute().use { r ->
            if (r.code == 404) return Proof.UNKNOWN
            if (!r.isSuccessful) return Proof.FAILED
            val mac = JSONObject(r.body!!.string()).optString("mac")
            val m = javax.crypto.Mac.getInstance("HmacSHA256")
            m.init(javax.crypto.spec.SecretKeySpec(h.toByteArray(), "HmacSHA256"))
            val want = m.doFinal(n.toByteArray()).joinToString("") { "%02x".format(it) }
            if (java.security.MessageDigest.isEqual(want.toByteArray(), mac.toByteArray())) Proof.OK else Proof.FAILED
        }
    } catch (e: Exception) {
        Proof.FAILED
    }

    private fun sha256Hex(s: String): String =
        java.security.MessageDigest.getInstance("SHA-256").digest(s.toByteArray()).joinToString("") { "%02x".format(it) }

    /** Whether an address (scheme://host:port) is the server's: its current address or one it listed. */
    private fun isServer(u: String): Boolean {
        val b = base
        if (b.isNotEmpty() && same(b, u)) return true
        return (prefs.getStringSet("known", emptySet()) ?: emptySet()).any { same(it, u) }
    }

    private fun same(a: String, b: String): Boolean {
        val x = a.toHttpUrlOrNull() ?: return false
        val y = b.toHttpUrlOrNull() ?: return false
        return x.scheme == y.scheme && x.host == y.host && x.port == y.port
    }

    /**
     * The key, for a download the system makes on the app's behalf (it can't go through [http]); null unless the
     * server has proven itself. Not on the main thread: it may ask the server.
     */
    fun authHeader(): String? {
        val tok = token
        if (tok.isEmpty() || base.isEmpty() || proven(origin(base), tok) != Proof.OK) return null
        return "Bearer $tok"
    }

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
                val ch = Channel<Pair<String, Int>>(known.size)
                known.forEach { u -> launch { ch.send(u to hello(u)) } }
                // best: one that knows this device's key, then one on the home network
                val top = if (token.isEmpty()) 2 else 4
                var best: String? = null
                var bestScore = 0
                var deadline = System.currentTimeMillis() + 6000
                var left = known.size
                while (left > 0 && bestScore < top) {
                    val wait = deadline - System.currentTimeMillis()
                    if (wait <= 0) break
                    val (u, answer) = withTimeoutOrNull(wait) { ch.receive() } ?: break
                    left--
                    if (answer == 0) continue
                    val score = (answer - 1) * 2 + (if (isLan(u)) 1 else 0) + 1
                    if (score > bestScore) { best = u; bestScore = score }
                    // something answered: give the rest about a second to do better
                    deadline = minOf(deadline, System.currentTimeMillis() + 1000)
                }
                coroutineContext.cancelChildren()
                best
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

    /**
     * Every address a /hello answer lists: its home-network ones, then any a remote-access add-on on the server
     * offers ("remote"; none yet). The race prefers a home-network address whenever one answers.
     */
    private fun addresses(j: JSONObject): Set<String> {
        val all = linkedSetOf<String>()
        for (key in listOf("lan", "remote")) {
            val a = j.optJSONArray(key) ?: continue
            for (i in 0 until a.length()) a.optString(i).trimEnd('/').takeIf { it.startsWith("http") }?.let { all += it }
        }
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

    /**
     * Asks an address for /hello: 0 no answer, 1 an answer, 2 an answer from a server that knows this device's key.
     * Only a server that knows the key (or any, before this device has one) may add to the addresses kept.
     */
    private fun hello(u: String): Int = try {
        probe.newCall(Request.Builder().url("$u/api/v1/hello").build()).execute().use { r ->
            if (!r.isSuccessful) return 0
            val j = JSONObject(r.body!!.string())
            val tok = token
            val mine = tok.isNotEmpty() && proven(origin(u), tok) == Proof.OK
            if (tok.isEmpty() || mine) learn(j)
            if (mine) 2 else 1
        }
    } catch (e: Exception) {
        0
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
                val j = runCatching { JSONObject(s) }.getOrNull()
                val code = j?.optString("error").orEmpty()
                if ((r.code == 401 && code == "pair") || (r.code == 403 && code == "setup")) refused(r)
                val msg = j?.optString("message").orEmpty().ifEmpty { code }
                throw ServerError(msg.ifEmpty { "HTTP ${r.code}" })
            }
            JSONObject(s)
        }
    }

    /**
     * The server wants a key. If this request carried the current key, the server turned it down (revoked): it's
     * dropped and the app pairs again. If it carried none because this server couldn't prove it knows the key,
     * pair only when it answered that it doesn't (a server that lost its devices, or another one); if it couldn't
     * be asked, treat it as not reachable for now. The key stays until a new one replaces it.
     */
    private fun refused(r: okhttp3.Response) {
        val tok = token
        val sent = r.request.header("Authorization")
        when {
            tok.isEmpty() -> needsPair = true
            sent == "Bearer $tok" -> { setToken(""); needsPair = true }
            proofs[origin(r.request.url)]?.proof == Proof.UNKNOWN -> needsPair = true
            else -> { proofs.remove(origin(r.request.url)); throw IOException(unreachable) }
        }
    }

    private fun setToken(tok: String) {
        token = tok
        proofs.clear()
        prefs.edit().apply { if (tok.isEmpty()) remove("token") else putString("token", tok) }.apply()
    }

    // ---------- pairing: the app shows a code, someone approves it on the server's website ----------

    /** A code waiting for approval. [url] opens the approval page for it on the server's website. */
    class PairCode(val code: String, val poll: String, val url: String, val expiresIn: Int, val interval: Int)

    /** Asks the server for a code; [kind] is "tv" or "phone", [device] what the device calls itself. */
    suspend fun pairStart(kind: String, device: String): PairCode {
        val j = post("/api/v1/pair/start", JSONObject().put("kind", kind).put("name", device))
        return PairCode(j.getString("code"), j.getString("poll"), j.optString("url"),
            j.optInt("expires_in", 600), j.optInt("interval", 2).coerceIn(1, 10))
    }

    /**
     * Whether the code has been answered: "pending", "approved", "denied" or "expired". On "approved" the key is
     * kept and [needsPair] turns false.
     */
    suspend fun pairPoll(poll: String): String = withContext(Dispatchers.IO) {
        if (base.isEmpty()) return@withContext "pending"
        val req = Request.Builder().url("$base/api/v1/pair/poll")
            .post(JSONObject().put("poll", poll).toString().toRequestBody("application/json".toMediaType())).build()
        try {
            http.newCall(req).execute().use { r ->
                val j = runCatching { JSONObject(r.body?.string() ?: "") }.getOrNull() ?: return@use "pending"
                val state = j.optString("state", if (r.code == 404) "expired" else "pending")
                val tok = j.optString("token")
                if (state == "approved" && tok.startsWith("mc_")) {
                    setToken(tok)
                    needsPair = false
                }
                state
            }
        } catch (e: IOException) {
            "pending" // a dropped poll: the next one asks again
        }
    }

    /** What this device calls itself, for the server's device list: the name set in Android, or its model. */
    fun deviceName(ctx: Context): String =
        runCatching { android.provider.Settings.Global.getString(ctx.contentResolver, "device_name") }.getOrNull()
            ?.takeIf { it.isNotBlank() } ?: android.os.Build.MODEL

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
