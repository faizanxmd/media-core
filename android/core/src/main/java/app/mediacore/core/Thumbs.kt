package app.mediacore.core

import android.content.Context
import android.graphics.BitmapFactory
import android.util.LruCache
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.Request
import java.io.File
import java.io.RandomAccessFile

/**
 * The scrub pictures of what's playing (the server's thumbs.go makes them). The whole file is
 * downloaded once and pictures are read out of it locally, so scrubbing never waits on the network.
 */
class ThumbStore(ctx: Context, private val scope: CoroutineScope) {
    private val dir = File(ctx.cacheDir, "thumbs").apply { mkdirs() }
    private val reader = Dispatchers.IO.limitedParallelism(1)
    private val cache = LruCache<Int, ImageBitmap>(24)
    private val wanted = LinkedHashSet<Int>() // newest last
    private var decoding = false
    private var version by mutableIntStateOf(0) // a picture arrived
    private var raf: RandomAccessFile? = null // accessed only on [reader]
    private var offsets = LongArray(0) // accessed on Main for bounds, passed to [reader]
    private var every = 10
    private var thumbCount = 0
    private var id = ""
    private var count = 0
    private var job: Job? = null

    var ready by mutableStateOf(false)
        private set

    val stepMs: Long get() = every * 1000L

    /** Switches to a video. [eager]: fetch its pictures soon; otherwise on the first [ensure]. */
    fun load(id: String, count: Int, eager: Boolean) {
        if (id == this.id) return
        this.id = id
        this.count = count
        close()
        if (count <= 0) return
        val f = file()
        if (f.exists()) job = scope.launch { open(f) }
        else if (eager) job = scope.launch { delay(1000); fetch(f) } // right after the film starts
    }

    /** Scrubbing has begun: if the pictures aren't here yet, get them now. */
    fun ensure() {
        if (ready || count <= 0 || job?.isActive == true) return
        job = scope.launch { fetch(file()) }
    }

    /** The picture nearest [ms], or null while it's being read (it then redraws whoever asked). */
    fun at(ms: Long): ImageBitmap? {
        version
        if (!ready || thumbCount <= 0) return null
        val i = (ms / 1000 / every).toInt().coerceIn(0, thumbCount - 1)
        cache.get(i)?.let { return it }
        request(i)
        return null
    }

    /** Pre-decodes the neighbor frames around [ms] so a 5-frame filmstrip never flashes empty. */
    fun prefetch(ms: Long) {
        if (!ready || thumbCount <= 0) return
        val center = (ms / 1000 / every).toInt().coerceIn(0, thumbCount - 1)
        for (d in listOf(0, 1, -1, 2, -2)) {
            val i = center + d
            if (i in 0 until thumbCount && cache.get(i) == null) request(i)
        }
    }

    private fun request(i: Int) {
        wanted.remove(i)
        wanted.add(i)
        if (wanted.size > 8) wanted.remove(wanted.first()) // scrubbing outran us: skip the old ones
        if (decoding) return
        decoding = true
        scope.launch {
            while (wanted.isNotEmpty()) {
                val next = wanted.last()
                wanted.remove(next)
                val forId = id
                val offs = offsets
                val pic = withContext(reader) { runCatching { decode(next, offs) }.getOrNull() }
                if (pic != null && forId == id) {
                    cache.put(next, pic)
                    version++
                }
            }
            decoding = false
        }
    }

    private fun decode(i: Int, offs: LongArray): ImageBitmap? {
        val r = raf ?: return null
        if (i < 0 || i + 1 >= offs.size) return null
        val len = (offs[i + 1] - offs[i]).toInt()
        if (len <= 0) return null
        val b = ByteArray(len)
        r.seek(offs[i])
        r.readFully(b)
        return BitmapFactory.decodeByteArray(b, 0, b.size)?.asImageBitmap()
    }

    private fun file() = File(dir, "$id-$count.bin")

    private suspend fun fetch(f: File) {
        val part = File(f.path + ".part")
        val call = Api.http.newCall(Request.Builder().url("${Api.base}/api/v1/thumbs/$id").build())
        val ok = withContext(Dispatchers.IO) {
            coroutineContext[Job]?.invokeOnCompletion { if (it != null) call.cancel() }
            runCatching {
                call.execute().use { r ->
                    if (!r.isSuccessful) error("HTTP ${r.code}")
                    part.outputStream().use { out -> r.body!!.byteStream().copyTo(out) }
                }
                if (!part.renameTo(f)) error("rename failed")
                // only the last few videos stay on the device
                dir.listFiles { x -> x.name.endsWith(".bin") }?.sortedByDescending { it.lastModified() }?.drop(6)?.forEach { it.delete() }
            }.onFailure { part.delete() }.isSuccess
        }
        if (ok) open(f)
    }

    private suspend fun open(f: File) {
        val forId = id
        val opened = withContext(reader) {
            var r: RandomAccessFile? = null
            runCatching {
                r = RandomAccessFile(f, "r")
                val magic = ByteArray(4).also { r!!.readFully(it) }
                check(String(magic) == "MCT1")
                val secs = r!!.readInt()
                val n = r!!.readInt()
                check(n > 0)
                val offs = LongArray(n + 1) { r!!.readLong() }
                runCatching { raf?.close() }
                raf = r
                Pair(secs, offs)
            }.onFailure { runCatching { r?.close() } }.getOrNull()
        } ?: return
        if (forId != id) { withContext(reader) { runCatching { raf?.close() }; raf = null }; return }
        every = opened.first.coerceAtLeast(1)
        offsets = opened.second
        thumbCount = (offsets.size - 1).coerceAtLeast(0)
        ready = thumbCount > 0
    }

    private fun close() {
        job?.cancel()
        ready = false
        thumbCount = 0
        wanted.clear()
        cache.evictAll()
        scope.launch(reader) { runCatching { raf?.close() }; raf = null }
    }

    fun release() {
        id = ""
        close()
    }
}
