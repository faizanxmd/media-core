package app.mediacore.phone

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import app.mediacore.core.Info
import app.mediacore.core.Item
import app.mediacore.core.hm
import coil3.compose.AsyncImage

/** The picture at the top of Home and every title page: 4:3 across the screen, under the status bar. */
@Composable
fun heroHeight(): Dp = LocalConfiguration.current.screenWidthDp.dp * 3 / 4

/** How far the words climb up into the hero, over its fade. */
val HeroOverlap = 72.dp

@Composable
fun Hero(url: String?, modifier: Modifier = Modifier) {
    Box(modifier.fillMaxWidth().height(heroHeight())) {
        AsyncImage(url, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(0.45f to Base.copy(alpha = 0f), 1f to Base)))
    }
}

/** Title art if there is some, else the name. */
@Composable
fun TitleMark(logo: String?, name: String) {
    if (logo != null) {
        AsyncImage(
            logo, name, Modifier.fillMaxWidth(0.55f).height(HeroOverlap),
            contentScale = ContentScale.Fit, alignment = Alignment.BottomStart,
        )
    } else {
        Text(name, style = Type.Display, maxLines = 2, overflow = TextOverflow.Ellipsis)
    }
}

/** Flat and white, flush on the picture's bottom edge. */
@Composable
fun BoxScope.CardProgress(f: Float) {
    if (f <= 0f) return
    Box(Modifier.align(Alignment.BottomStart).fillMaxWidth(f).height(3.dp).background(Ink))
}

/** A poster carries its own name; words only when there's no picture. */
@Composable
fun PosterCard(item: Item, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Box(modifier.aspectRatio(2f / 3f).clip(CardShape).background(Raised).tap(onClick = onClick)) {
        val p = item.poster
        if (p != null) AsyncImage(p, item.displayTitle, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        else Text(item.displayTitle, style = Type.Meta, modifier = Modifier.align(Alignment.BottomStart).padding(8.dp))
        if (!item.watched) CardProgress(item.progress)
    }
}

/** A 16:9 picture with the name under it, and what's left. */
@Composable
fun WideCard(item: Item, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Column(modifier.width(240.dp).tap(onClick = onClick)) {
        Box(Modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(CardShape).background(Raised)) {
            AsyncImage(item.wide, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
            CardProgress(item.progress)
        }
        Text(item.showTitle, style = Type.Label, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 8.dp))
        val left = item.durMs - item.pos
        val sub = listOfNotNull(
            if (item.isEpisode) "S${item.season}, E${item.episode}" else null,
            if (item.pos > 0 && left > 0) "${hm(left)} left" else null,
        ).joinToString(" · ")
        if (sub.isNotEmpty()) Text(sub, style = Type.Meta, color = Muted, maxLines = 1)
    }
}

/** A row's title. */
@Composable
fun RowTitle(text: String, modifier: Modifier = Modifier) {
    Text(text, style = Type.Heading, modifier = modifier.padding(bottom = 12.dp))
}

/** Clears the status bar on a page with no hero. */
fun Modifier.pageTop(): Modifier = statusBarsPadding().padding(top = Side)

/** "4K · Dolby Vision · DTS-HD MA": only the formats that are a reason to pick it. */
fun formats(i: Info?): List<String> {
    if (i == null) return emptyList()
    val out = mutableListOf<String>()
    if (i.res == "4K") out += "4K"
    when {
        i.hdr.startsWith("Dolby Vision") -> out += "Dolby Vision"
        i.hdr.isNotEmpty() -> out += i.hdr
    }
    val a = i.audio
    when {
        a.contains("Atmos") -> out += "Dolby Atmos"
        a.startsWith("DTS:X") -> out += "DTS:X"
        a.startsWith("DTS-HD MA") -> out += "DTS-HD MA"
        a.startsWith("TrueHD") -> out += "TrueHD"
    }
    return out
}
