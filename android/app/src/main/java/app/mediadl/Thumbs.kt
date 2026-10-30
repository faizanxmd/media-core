package app.mediadl

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
 * The scrub pictures of what's playing (mint's thumbs.go makes them). The whole file is
 * downloaded once and pictures are read out of it locally, so scrubbing never waits on the network.
 */
class ThumbStore(ctx: Context, private val scope: CoroutineScope) {
    private val dir = File(ctx.cacheDir, "thumbs").apply { mkdirs() }
    private val reader = Dispatchers.IO.limitedParallelism(1)
    private val cache = LruCache<Int, ImageBitmap>(24)
    private val wanted = LinkedHashSet<Int>() // newest last
    private var decoding = false
    private var version by mutableIntStateOf(0) // a picture arrived
    private var raf: RandomAccessFile? = null
    private var offsets = LongArray(0)
    private var every = 10
    private var id = ""
    private var count = 0
    private var job: Job? = null

    var ready by mutableStateOf(false)
        private set

    /** Switches to a video. [eager]: fetch its pictures soon; otherwise on the first [ensure]. */
    fun load(id: String, count: Int, eager: Boolean) {
        if (id == this.id) return
        this.id = id
        this.count = count
        close()
        if (count <= 0) return
        val f = file()
        if (f.exists()) job = scope.launch { open(f) }
        else if (eager) job = scope.launch { delay(4000); fetch(f) } // after the film's own start
    }

    /** Scrubbing has begun: if the pictures aren't here yet, get them now. */
    fun ensure() {
        if (ready || count <= 0 || job?.isActive == true) return
        job = scope.launch { fetch(file()) }
    }

    /** The picture nearest [ms], or null while it's being read (it then redraws whoever asked). */
    fun at(ms: Long): ImageBitmap? {
        version
        if (!ready) return null
        val i = (ms / 1000 / every).toInt().coerceIn(0, offsets.size - 2)
        cache.get(i)?.let { return it }
        request(i)
        return null
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
                val pic = withContext(reader) { runCatching { decode(next) }.getOrNull() }
                if (pic != null && forId == id) {
                    cache.put(next, pic)
                    version++
                }
            }
            decoding = false
        }
    }

    private fun decode(i: Int): ImageBitmap? {
        val r = raf ?: return null
        val b = ByteArray((offsets[i + 1] - offsets[i]).toInt())
        r.seek(offsets[i])
        r.readFully(b)
        return BitmapFactory.decodeByteArray(b, 0, b.size)?.asImageBitmap()
    }

    private fun file() = File(dir, "$id-$count.bin")

    private suspend fun fetch(f: File) {
        val ok = withContext(Dispatchers.IO) {
            runCatching {
                val part = File(f.path + ".part")
                Api.http.newCall(Request.Builder().url("${Api.base}/api/v1/thumbs/$id").build()).execute().use { r ->
                    if (!r.isSuccessful) error("HTTP ${r.code}")
                    part.outputStream().use { out -> r.body!!.byteStream().copyTo(out) }
                }
                part.renameTo(f)
                // only the last few videos stay on the TV
                dir.listFiles { x -> x.name.endsWith(".bin") }?.sortedByDescending { it.lastModified() }?.drop(6)?.forEach { it.delete() }
            }.isSuccess
        }
        if (ok) open(f)
    }

    private suspend fun open(f: File) {
        val forId = id
        val opened = withContext(reader) {
            runCatching {
                val r = RandomAccessFile(f, "r")
                val magic = ByteArray(4).also { r.readFully(it) }
                check(String(magic) == "MCT1")
                val secs = r.readInt()
                val n = r.readInt()
                r to Pair(secs, LongArray(n + 1) { r.readLong() })
            }.getOrNull()
        } ?: return
        if (forId != id) { withContext(reader) { opened.first.close() }; return }
        raf = opened.first
        every = opened.second.first.coerceAtLeast(1)
        offsets = opened.second.second
        ready = true
    }

    private fun close() {
        job?.cancel()
        ready = false
        wanted.clear()
        cache.evictAll()
        val r = raf
        raf = null
        if (r != null) Thread { runCatching { r.close() } }.start() // a read still under way just fails quietly
    }

    fun release() {
        id = ""
        close()
    }
}
