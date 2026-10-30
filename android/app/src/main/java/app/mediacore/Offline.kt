package app.mediacore

import android.app.DownloadManager
import android.content.Context
import android.net.Uri
import android.os.Environment
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.Request
import org.json.JSONArray
import org.json.JSONObject
import java.io.File

/**
 * Downloads for watching without a connection (plane, travel). The original
 * file is copied as-is by Android's DownloadManager (resumes, survives app
 * exit), into the app's own storage; positions watched offline are sent to
 * mint the next time it's reachable.
 */
object Offline {
    private lateinit var dir: File
    private lateinit var ctx: Context

    fun init(c: Context) {
        ctx = c.applicationContext
        dir = File(ctx.getExternalFilesDir(Environment.DIRECTORY_MOVIES) ?: ctx.filesDir, "offline").apply { mkdirs() }
    }

    private fun metaFile(id: String) = File(dir, "$id.json")
    private fun videoFile(id: String) = File(dir, "$id.video")
    private fun subFile(id: String, n: Int) = File(dir, "$id.sub$n")

    private val dm get() = ctx.getSystemService(Context.DOWNLOAD_SERVICE) as DownloadManager

    data class State(val done: Boolean, val progress: Float)

    /** null = not downloaded; otherwise finished or how far along. */
    fun state(id: String): State? {
        val meta = metaFile(id).takeIf { it.exists() } ?: return null
        val j = runCatching { JSONObject(meta.readText()) }.getOrNull() ?: return null
        if (j.optBoolean("_done")) return State(true, 1f)
        val dl = j.optLong("_dl", -1)
        dm.query(DownloadManager.Query().setFilterById(dl)).use { c ->
            if (!c.moveToFirst()) return null
            val status = c.getInt(c.getColumnIndexOrThrow(DownloadManager.COLUMN_STATUS))
            val got = c.getLong(c.getColumnIndexOrThrow(DownloadManager.COLUMN_BYTES_DOWNLOADED_SO_FAR))
            val total = c.getLong(c.getColumnIndexOrThrow(DownloadManager.COLUMN_TOTAL_SIZE_BYTES))
            if (status == DownloadManager.STATUS_SUCCESSFUL) {
                j.put("_done", true)
                metaFile(id).writeText(j.toString())
                return State(true, 1f)
            }
            if (status == DownloadManager.STATUS_FAILED) return null
            return State(false, if (total > 0) got.toFloat() / total else 0f)
        }
    }

    /** Local file to play instead of streaming, if the download is complete. */
    fun localVideo(id: String): File? = if (state(id)?.done == true) videoFile(id).takeIf { it.exists() } else null
    fun localSub(id: String, n: Int): File? = subFile(id, n).takeIf { it.exists() }

    suspend fun download(item: Item) = withContext(Dispatchers.IO) {
        val raw = JSONObject(item.raw.toString())
        // subtitles next to the video are tiny: fetch them right away
        raw.optJSONArray("subs").objects().forEachIndexed { n, _ ->
            runCatching {
                Api.http.newCall(Request.Builder().url(Api.sub(item.id, n)).build()).execute().use { r ->
                    if (r.isSuccessful) subFile(item.id, n).writeBytes(r.body!!.bytes())
                }
            }
        }
        val title = if (item.isEpisode) "${item.showTitle} ${item.code}" else item.displayTitle
        val req = DownloadManager.Request(Uri.parse(Api.stream(item.id) + "?dl=1"))
            .setTitle(title)
            .setDescription("Media Core · for offline")
            .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
            .setDestinationUri(Uri.fromFile(videoFile(item.id)))
            .setAllowedOverMetered(true)
        videoFile(item.id).delete()
        val dl = dm.enqueue(req)
        raw.put("_dl", dl).put("_at", System.currentTimeMillis())
        metaFile(item.id).writeText(raw.toString())
    }

    fun remove(id: String) {
        val j = runCatching { JSONObject(metaFile(id).readText()) }.getOrNull()
        j?.optLong("_dl", -1)?.takeIf { it >= 0 }?.let { runCatching { dm.remove(it) } }
        dir.listFiles { f -> f.name.startsWith("$id.") }?.forEach { it.delete() }
    }

    /** Everything downloaded or downloading, newest first. */
    fun items(): List<Item> = (dir.listFiles { f -> f.name.endsWith(".json") } ?: emptyArray())
        .mapNotNull { f -> runCatching { JSONObject(f.readText()) }.getOrNull() }
        .sortedByDescending { it.optLong("_at") }
        .map { j -> applyPos(Item.parse(j)) }

    fun usedBytes(): Long = dir.listFiles()?.sumOf { it.length() } ?: 0L

    // ---------- positions watched while offline ----------

    private val prefs get() = ctx.getSharedPreferences("offline-progress", Context.MODE_PRIVATE)

    fun savePos(id: String, pos: Long, dur: Long, ended: Boolean) {
        prefs.edit().putString(id, JSONObject().put("pos", pos).put("dur", dur).put("ended", ended).toString()).apply()
    }

    /** An offline item shows the newest position we know of (local beats the copy from download time). */
    private fun applyPos(it: Item): Item {
        val p = prefs.getString(it.id, null)?.let { s -> JSONObject(s) } ?: return it
        val w = if (p.optBoolean("ended")) Watch(0, p.optLong("dur"), true) else Watch(p.optLong("pos"), p.optLong("dur"), false)
        return it.copy(watch = w)
    }

    /** Sends offline positions to mint once it's reachable again. */
    suspend fun sync() {
        val all = prefs.all
        for ((id, v) in all) {
            val p = runCatching { JSONObject(v as String) }.getOrNull() ?: continue
            val ok = runCatching {
                Api.post("/api/v1/progress", JSONObject().put("id", id).put("pos", p.optLong("pos"))
                    .put("dur", p.optLong("dur")).put("ended", p.optBoolean("ended")))
            }.isSuccess
            if (ok) prefs.edit().remove(id).apply()
        }
    }

    fun queueJson(items: List<Item>) = JSONArray(items.map { it.raw }).toString()
}
