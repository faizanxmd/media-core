package app.mediacore

import org.json.JSONObject

data class Info(
    val res: String, val hdr: String, val mbps: Double, val audio: String, val vcodec: String,
    val tracks: List<String>, val subs: List<String>, val dur: Double, val fps: Double,
)

data class Person(val name: String, val role: String, val photo: String)

data class Meta(
    val title: String, val year: Int, val overview: String, val tagline: String,
    val poster: String, val backdrop: String, val logo: String, val still: String,
    val rating: Double, val genres: List<String>, val cast: List<Person>, val runtime: Int,
    val cert: String, val date: String, val studio: String,
)

data class Watch(val pos: Long, val dur: Long, val watched: Boolean)

data class Item(
    val id: String, val type: String, val title: String, val year: Int,
    val showId: String, val show: String, val season: Int, val episode: Int,
    val added: Long, val size: Long, val count: Int, val unseen: Int,
    val info: Info?, val meta: Meta?, val watch: Watch?,
    val seasons: List<Item>, val episodes: List<Item>,
    val raw: JSONObject, // handed to the player as-is
) {
    val isEpisode get() = type == "episode"
    val isShow get() = type == "show"
    val displayTitle get() = meta?.title?.takeIf { it.isNotEmpty() && !isEpisode } ?: title
    val showTitle get() = show.ifEmpty { displayTitle }
    val displayYear get() = if (year > 0) year else meta?.year ?: 0
    val pos get() = watch?.pos ?: 0L
    val watched get() = watch?.watched == true
    val durMs: Long get() = watch?.dur?.takeIf { it > 0 } ?: ((info?.dur ?: 0.0) * 1000).toLong()
    val progress: Float get() = if (pos > 0 && durMs > 0) (pos.toFloat() / durMs).coerceIn(0f, 1f) else 0f
    val code get() = "S$season · E$episode"

    val poster get() = Api.img(meta?.poster, "w342")
    val backdrop get() = Api.img(meta?.backdrop, "w1280")
    /** The picture across a whole title page: full size, because 1280 px stretched over the screen goes soft. */
    val banner get() = Api.img(meta?.backdrop, "original")
    val logo get() = Api.img(meta?.logo, "w500")
    /** 16:9 picture for wide cards: an episode's still, else the backdrop. */
    val wide get() = Api.img(meta?.still?.ifEmpty { null }, "w780") ?: Api.img(meta?.backdrop, "w780")
    /** The same at a TV card's size: less to fetch and decode. */
    val wideSmall get() = Api.img(meta?.still?.ifEmpty { null }, "w500") ?: Api.img(meta?.backdrop, "w500")

    companion object {
        fun parse(j: JSONObject): Item = Item(
            id = j.optString("id"), type = j.optString("type"), title = j.optString("title"),
            year = j.optInt("year"), showId = j.optString("show_id"), show = j.optString("show"),
            season = j.optInt("season"), episode = j.optInt("episode"), added = j.optLong("added"),
            size = j.optLong("size"), count = j.optInt("count"), unseen = j.optInt("unseen"),
            info = j.optJSONObject("info")?.let {
                Info(
                    it.optString("res"), it.optString("hdr"), it.optDouble("mbps", 0.0), it.optString("audio"),
                    it.optString("vcodec"), it.optJSONArray("tracks").strings(), it.optJSONArray("subs").strings(),
                    it.optDouble("dur", 0.0), it.optDouble("fps", 0.0),
                )
            },
            meta = j.optJSONObject("meta")?.let { m ->
                Meta(
                    m.optString("title"), m.optInt("year"), m.optString("overview"), m.optString("tagline"),
                    m.optString("poster"), m.optString("backdrop"), m.optString("logo"), m.optString("still"),
                    m.optDouble("rating", 0.0), m.optJSONArray("genres").strings(),
                    m.optJSONArray("cast").objects().map { Person(it.optString("name"), it.optString("role"), it.optString("photo")) },
                    m.optInt("runtime"), m.optString("cert"), m.optString("date"), m.optString("studio"),
                )
            },
            watch = j.optJSONObject("watch")?.let { Watch(it.optLong("pos"), it.optLong("dur"), it.optBoolean("watched")) },
            seasons = j.optJSONArray("seasons").objects().map(::parse),
            episodes = j.optJSONArray("episodes").objects().map(::parse),
            raw = j,
        )
    }
}

data class Download(val title: String, val state: String, val progress: Int, val speed: String, val eta: String)

data class Home(
    val cont: List<Item>, val recent: List<Item>, val movies: List<Item>, val shows: List<Item>,
    val downloads: List<Download>,
) {
    companion object {
        fun parse(j: JSONObject) = Home(
            j.optJSONArray("continue").objects().map(Item::parse),
            j.optJSONArray("recent").objects().map(Item::parse),
            j.optJSONArray("movies").objects().map(Item::parse),
            j.optJSONArray("shows").objects().map(Item::parse),
            j.optJSONArray("downloading").objects().map {
                Download(it.optString("title"), it.optString("state"), it.optInt("progress"), it.optString("speed"), it.optString("eta"))
            },
        )
    }
}

// ---------- formatting ----------

fun hm(ms: Long): String {
    val m = (ms / 60000).toInt()
    return if (m >= 60) "${m / 60}h ${m % 60}m" else "${m}m"
}

fun clock(ms: Long): String {
    val s = ms / 1000
    return if (s >= 3600) "%d:%02d:%02d".format(s / 3600, s / 60 % 60, s % 60) else "%d:%02d".format(s / 60, s % 60)
}

/** Short quality badges: 4K · Dolby Vision · Atmos · 5.1 */
fun badges(i: Info?): List<String> {
    if (i == null) return emptyList()
    val out = mutableListOf<String>()
    if (i.res.isNotEmpty()) out += i.res
    when {
        i.hdr.startsWith("Dolby Vision") -> out += "Dolby Vision"
        i.hdr.isNotEmpty() -> out += i.hdr
    }
    val a = i.audio
    when {
        a.contains("Atmos") -> out += "Atmos"
        a.startsWith("DTS:X") -> out += "DTS:X"
        a.startsWith("DTS-HD") -> out += "DTS-HD"
        a.startsWith("TrueHD") -> out += "TrueHD"
    }
    when {
        a.endsWith("7.1") -> out += "7.1"
        a.endsWith("5.1") -> out += "5.1"
    }
    return out
}

/** "2003 · R · 2h 0m · ★ 8.3" */
fun metaLine(it: Item): String {
    val parts = mutableListOf<String>()
    if (it.isEpisode) {
        parts += it.code
        it.meta?.date?.takeIf { d -> d.length >= 4 }?.let { d -> parts += d.take(4) }
    } else if (it.displayYear > 0) parts += it.displayYear.toString()
    it.meta?.cert?.takeIf { c -> c.isNotEmpty() }?.let { c -> parts += c }
    when {
        it.isShow -> parts += if (it.seasons.size > 1) "${it.seasons.size} seasons" else "${it.count} episodes"
        it.durMs > 0 -> parts += hm(it.durMs)
        (it.meta?.runtime ?: 0) > 0 -> parts += hm(it.meta!!.runtime * 60000L)
    }
    it.meta?.rating?.takeIf { r -> r > 0 }?.let { r -> parts += "★ %.1f".format(r) }
    return parts.joinToString("  ·  ")
}

/** "2 h 17 min", "58 min" */
fun mins(ms: Long): String {
    val m = ((ms + 30_000) / 60000).toInt()
    return if (m >= 60) "${m / 60} h ${m % 60} min" else "$m min"
}

/** "22 min left", "1 h 4 min left", "less than a minute left" */
fun leftText(ms: Long): String = if (ms < 60_000) "less than a minute left" else mins(ms) + " left"
