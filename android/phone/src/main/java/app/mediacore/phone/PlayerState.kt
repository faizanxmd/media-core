package app.mediacore.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/** Hidden: just the picture. Seek: a thin bar while skipping. Full: title, bar and controls. */
enum class Mode { Hidden, Seek, Full }
enum class Sheet { Tracks, Chapters, Episodes }

/** Everything the player overlay shows; the activity writes it, Compose reads it. */
class PlayerState {
    var mode by mutableStateOf(Mode.Hidden)
    var sheet by mutableStateOf<Sheet?>(null)
    var playing by mutableStateOf(false)
    var buffering by mutableStateOf(true)
    var firstFrame by mutableStateOf(false) // until then: artwork + title instead of a black screen
    var pos by mutableLongStateOf(0L)
    var buffered by mutableLongStateOf(0L)
    var dur by mutableLongStateOf(0L)
    var scrub by mutableStateOf<Long?>(null) // where you'll land while still seeking
    var scrubFrom by mutableLongStateOf(0L) // where that skip started, so "+10s" doesn't drift as the film plays
    var speed by mutableStateOf(1f)
    var title by mutableStateOf("")
    var sub by mutableStateOf("") // year, or "S1, E2 · Its title"
    var cover by mutableStateOf<String?>(null)
    var offer by mutableStateOf<Offer?>(null)
    var countdown by mutableIntStateOf(0)
    var hasNext by mutableStateOf(false)
    var note by mutableStateOf<String?>(null)
    var error by mutableStateOf<PlayError?>(null)
    var audio by mutableStateOf<List<TrackOption>>(emptyList())
    var text by mutableStateOf<List<TrackOption>>(emptyList())
    var episodes by mutableStateOf<List<TrackOption>>(emptyList())
    var chapters by mutableStateOf<List<Chapter>>(emptyList())
    var thumbs: ThumbStore? = null // scrub pictures, once the phone has a strip
}

data class Offer(val kind: String, val title: String, val detail: String, val still: String?)
data class PlayError(val title: String, val message: String, val detail: String)
data class Chapter(val start: Long, val title: String)
class TrackOption(val title: String, val detail: String, val selected: Boolean, val image: String? = null, val pick: () -> Unit)

interface PlayerActions {
    fun togglePlay()
    fun scrub(deltaMs: Long)
    fun scrubTo(ms: Long, commit: Boolean)
    fun commitScrub()
    fun next()
    fun takeOffer()
    fun openSheet(s: Sheet)
    fun closeSheet()
    fun cycleSpeed()
    fun tapped()
    fun retry()
    fun close()
}

/** Seek step for a held media key: 10 s taps, speeding up the longer it's held. */
fun scrubStep(repeat: Int): Long = when {
    repeat < 4 -> 10_000L
    repeat < 12 -> 30_000L
    repeat < 24 -> 60_000L
    else -> 120_000L
}

/** The chapter a position falls in, unless it's only called "Chapter 7". */
fun chapterAt(chs: List<Chapter>, ms: Long): String? =
    chs.lastOrNull { it.start <= ms }?.title?.takeIf { it.isNotBlank() && !Regex("""(?i)^chapter\s*\d+$""").matches(it.trim()) }

fun speedLabel(s: Float) = (if (s == s.toInt().toFloat()) "${s.toInt()}" else "$s") + "×"

@Composable
fun ErrorCard(e: PlayError, a: PlayerActions) {
    Box(Modifier.fillMaxSize().background(Base.copy(alpha = 0.8f)), contentAlignment = Alignment.Center) {
        Column(Modifier.widthIn(max = 460.dp).clip(CardShape).background(Raised).padding(24.dp)) {
            Text(e.title, style = Type.Title)
            Spacer(Modifier.height(8.dp))
            Text(e.message, style = Type.Body, color = Muted)
            if (e.detail.isNotEmpty()) {
                Spacer(Modifier.height(8.dp))
                Text(e.detail, style = Type.Caption, color = Muted, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
            Spacer(Modifier.height(24.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                WordButton("Try again", a::retry)
                WordButton("Close", a::close)
            }
        }
    }
}

@Composable
private fun WordButton(text: String, onClick: () -> Unit) {
    Box(
        Modifier.height(40.dp).clip(CardShape).background(Overlay).clickable(onClick = onClick).padding(horizontal = 16.dp),
        contentAlignment = Alignment.Center,
    ) { Text(text, style = Type.Label, maxLines = 1) }
}
