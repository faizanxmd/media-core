package app.mediacore.phone

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import app.mediacore.core.ThumbStore

/**
 * Hidden: just the picture (a double tap skips, with only a small mark).
 * Scrub: a finger is moving through the film; the picture strip takes over until it lifts.
 * Full: title, bar and controls (paused, or asked for with a tap).
 */
enum class Mode { Hidden, Scrub, Full }
enum class Sheet { Speed, Audio, Subtitles, Episodes }

/** Everything the player overlay shows; the activity writes it, Compose reads it. */
class PlayerState {
    var mode by mutableStateOf(Mode.Hidden)
    var sheet by mutableStateOf<Sheet?>(null)
    var playing by mutableStateOf(false)
    var buffering by mutableStateOf(true)
    var firstFrame by mutableStateOf(false) // until then: artwork instead of a black screen
    var pos by mutableLongStateOf(0L)
    var buffered by mutableLongStateOf(0L)
    var dur by mutableLongStateOf(0L)
    var scrub by mutableStateOf<Long?>(null) // where you'll land while still skipping or scrubbing
    var skipped by mutableIntStateOf(0) // seconds the last few double taps add up to; shows the small mark
    var speed by mutableStateOf(1f)
    var title by mutableStateOf("")
    var sub by mutableStateOf("") // year, or "S1, E2 · Its title"
    var cover by mutableStateOf<String?>(null)
    var offer by mutableStateOf<Offer?>(null)
    var countdown by mutableIntStateOf(0)
    var hasNext by mutableStateOf(false)
    var error by mutableStateOf<PlayError?>(null)
    var audio by mutableStateOf<List<TrackOption>>(emptyList())
    var text by mutableStateOf<List<TrackOption>>(emptyList())
    var episodes by mutableStateOf<List<TrackOption>>(emptyList())
    var thumbs: ThumbStore? = null // scrub pictures
}

data class Offer(val kind: String, val title: String, val detail: String, val still: String?)
data class PlayError(val title: String, val message: String, val detail: String)
class TrackOption(val title: String, val detail: String, val selected: Boolean, val image: String? = null, val pick: () -> Unit)

interface PlayerActions {
    fun togglePlay()
    fun skip(dir: Int)
    fun scrubStart()
    fun scrubMove(ms: Long)
    fun scrubEnd()
    fun next()
    fun takeOffer()
    fun openSheet(s: Sheet)
    fun closeSheet()
    fun setSpeed(speed: Float)
    fun tapped()
    fun retry()
    fun close()
}

fun speedLabel(s: Float) = (if (s == s.toInt().toFloat()) "${s.toInt()}" else "$s") + "×"
