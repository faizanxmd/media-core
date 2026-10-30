@file:OptIn(ExperimentalTextApi::class)

package app.mediadl

import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.PlayArrow
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.ExperimentalTextApi
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.zIndex
import coil3.compose.AsyncImage

// Theater's look: neutral black, white at a few opacities, no accent colour.
val Bg = Color(0xFF0B0B0D)
val Surface = Color(0xF21E1E21) // menus and panels
val Surface2 = Color(0xFF17171A) // empty card
val Line = Color(0x1FFFFFFF)
val Ink = Color(0xF0FFFFFF)
val Mut = Color(0xB8FFFFFF)
val Faint = Color(0x8CFFFFFF)
val Glass = Color(0x2EFFFFFF) // resting buttons

private fun weight(w: Int) = FontVariation.Settings(FontVariation.weight(w))

val Inter = FontFamily(
    Font(R.font.inter, FontWeight.Normal, variationSettings = weight(400)),
    Font(R.font.inter, FontWeight.Medium, variationSettings = weight(500)),
    Font(R.font.inter, FontWeight.SemiBold, variationSettings = weight(600)),
    Font(R.font.inter, FontWeight.Bold, variationSettings = weight(700)),
    Font(R.font.inter, FontWeight.ExtraBold, variationSettings = weight(800)),
)

/** true on Android TV (D-pad, 10-foot UI), false on the phone. */
val LocalTv = compositionLocalOf { false }

@Composable
fun AppTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = darkColorScheme(
            background = Bg, surface = Bg, primary = Color.White, onPrimary = Bg,
            onBackground = Ink, onSurface = Ink,
        ),
    ) {
        CompositionLocalProvider(LocalTextStyle provides TextStyle(fontFamily = Inter, color = Ink), content = content)
    }
}

private val Ease = CubicBezierEasing(0.2f, 0.8f, 0.2f, 1f)

/** tvOS focus: the thing lifts (grows, casts a shadow) and comes to the front. */
@Composable
fun Modifier.lift(focused: Boolean, shape: Shape, scale: Float = 1.1f): Modifier {
    val s by animateFloatAsState(if (focused) scale else 1f, tween(220, easing = Ease), label = "lift")
    return zIndex(if (focused) 1f else 0f).graphicsLayer {
        scaleX = s; scaleY = s
        shadowElevation = if (focused) 18.dp.toPx() else 0f
        this.shape = shape
        clip = true
    }
}

/** The light catching a focused card: a soft sheen from the top left and a fine edge. */
fun Modifier.sheen(on: Boolean, radius: Dp): Modifier = drawWithContent {
    drawContent()
    if (on) {
        drawRect(Brush.linearGradient(0f to Color(0x33FFFFFF), 0.28f to Color(0x0FFFFFFF), 0.46f to Color.Transparent, start = Offset.Zero, end = Offset(size.width, size.height * 0.9f)))
        val w = 1.dp.toPx()
        drawRoundRect(Color(0x99FFFFFF), Offset(w / 2, w / 2), size.copy(size.width - w, size.height - w), CornerRadius(radius.toPx()), style = Stroke(w))
    }
}

/** A focusable picture card: lifts with a sheen when focused. */
@Composable
fun FocusCard(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    radius: Dp = 6.dp,
    scale: Float = 1.1f,
    focusRequester: FocusRequester? = null,
    onFocus: () -> Unit = {},
    content: @Composable BoxScope.(focused: Boolean) -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    val shape = RoundedCornerShape(radius)
    Box(
        modifier
            .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            // focus sits outside the lift: scrolling to a card must see its resting size,
            // or the list chases the card as it grows and every row bobs on each press
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .lift(focused, shape, scale)
            .background(Surface2)
            .sheen(focused, radius),
    ) { content(focused) }
}

/** Thin rounded progress track. */
@Composable
fun ProgressTrack(fraction: Float, modifier: Modifier = Modifier, height: Dp = 3.dp, back: Color = Color(0x42FFFFFF), fill: Color = Color(0xF2FFFFFF)) {
    Box(modifier.height(height).clip(CircleShape).background(back)) {
        Box(Modifier.fillMaxWidth(fraction).fillMaxHeight().clip(CircleShape).background(fill))
    }
}

/** The title in plain type when there's no artwork for it. */
@Composable
fun TitleArt(title: String, modifier: Modifier = Modifier, size: Int = 15) {
    Box(modifier.fillMaxSize().background(Surface2).padding(10.dp), contentAlignment = Alignment.BottomStart) {
        Text(title, fontSize = size.sp, lineHeight = (size * 1.2).sp, fontWeight = FontWeight.SemiBold, maxLines = 4, overflow = TextOverflow.Ellipsis)
    }
}

/** A poster and nothing else: the art carries the title. */
@Composable
fun PosterCard(
    item: Item,
    width: Dp,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    onFocus: () -> Unit = {},
    focusRequester: FocusRequester? = null,
) {
    FocusCard(onClick, modifier.width(width).aspectRatio(2f / 3f), focusRequester = focusRequester, onFocus = onFocus) {
        if (item.poster != null) AsyncImage(item.poster, item.displayTitle, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        else TitleArt(item.displayTitle)
        if (item.progress > 0f) {
            ProgressTrack(item.progress, Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(horizontal = 8.dp, vertical = 7.dp))
        }
    }
}

/** Continue Watching: 16:9 art with the title, time left and progress on it. */
@Composable
fun WideCard(
    item: Item,
    width: Dp,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    onFocus: () -> Unit = {},
    focusRequester: FocusRequester? = null,
) {
    FocusCard(onClick, modifier.width(width).aspectRatio(16f / 9f), focusRequester = focusRequester, onFocus = onFocus) {
        val pic = Api.img(item.meta?.backdrop?.ifEmpty { null }, "w780") ?: item.wide
        if (pic != null) AsyncImage(pic, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        Box(Modifier.matchParentSize().background(Brush.verticalGradient(0.34f to Color.Transparent, 0.58f to Color(0x59000000), 1f to Color(0xD1000000))))
        Column(Modifier.align(Alignment.BottomStart).padding(start = 11.dp, end = 11.dp, bottom = 15.dp)) {
            Text(item.showTitle, fontSize = 13.sp, lineHeight = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (item.pos > 0 && item.durMs > item.pos) {
                Text(leftText(item.durMs - item.pos), color = Mut, fontSize = 11.5.sp, lineHeight = 14.sp, fontWeight = FontWeight.Medium)
            }
        }
        if (item.progress > 0f) {
            ProgressTrack(item.progress, Modifier.align(Alignment.BottomCenter).fillMaxWidth().padding(start = 11.dp, end = 11.dp, bottom = 7.dp))
        }
    }
}

/**
 * The main action: a capsule with a play icon and the time left ("52m") or "Play".
 * Frosted at rest, solid white and lifted when focused.
 */
@Composable
fun PlayCapsule(
    label: String,
    modifier: Modifier = Modifier,
    focusRequester: FocusRequester? = null,
    onFocus: () -> Unit = {},
    onClick: () -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    val shape = RoundedCornerShape(20.dp)
    Row(
        modifier
            .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .lift(focused, shape, 1.05f)
            .height(40.dp)
            .background(if (focused) Color.White else Glass)
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick)
            .padding(start = 11.dp, end = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        val c = if (focused) Bg else Ink
        Icon(Icons.Rounded.PlayArrow, null, tint = c, modifier = Modifier.size(23.dp))
        Text(label, color = c, fontSize = 15.5.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
    }
}

/** Round icon button next to the capsule (start over, watched). */
@Composable
fun CircleButton(
    icon: ImageVector,
    label: String,
    modifier: Modifier = Modifier,
    size: Dp = 40.dp,
    iconSize: Dp = 19.dp,
    focusRequester: FocusRequester? = null,
    onFocus: () -> Unit = {},
    content: (@Composable (focused: Boolean) -> Unit)? = null,
    onClick: () -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    Box(
        modifier
            .then(if (focusRequester != null) Modifier.focusRequester(focusRequester) else Modifier)
            .onFocusChanged { focused = it.isFocused; if (it.isFocused) onFocus() }
            .lift(focused, CircleShape, 1.08f)
            .size(size)
            .background(if (focused) Color.White else Glass)
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        if (content != null) content(focused)
        else Icon(icon, label, tint = if (focused) Bg else Ink, modifier = Modifier.size(iconSize))
    }
}

/** 4K as a small solid chip, then the real Dolby / DTS / HDR10 marks in the text's grey. */
@Composable
fun FormatMarks(info: Info?, color: Color = Mut, height: Dp = 13.dp) {
    if (info == null) return
    val hdr = info.hdr
    val a = info.audio
    if (info.res == "4K") {
        Text(
            "4K", color = Bg, fontSize = 9.5.sp, lineHeight = 10.sp, fontWeight = FontWeight.ExtraBold, letterSpacing = 0.2.sp,
            modifier = Modifier.clip(RoundedCornerShape(2.5.dp)).background(Color(0xD9FFFFFF)).padding(horizontal = 4.dp, vertical = 1.5.dp),
        )
    }
    when {
        hdr.startsWith("Dolby Vision") -> Logo(R.drawable.logo_dv, height, color)
        hdr == "HDR10+" -> Logo(R.drawable.logo_hdr10p, height * 1.15f, color)
        hdr == "HDR10" -> Logo(R.drawable.logo_hdr10, height * 1.15f, color)
        hdr.isNotEmpty() -> Text(hdr, color = color, fontSize = 13.5.sp, fontWeight = FontWeight.Medium)
    }
    when {
        a.contains("Atmos") -> Logo(R.drawable.logo_atmos, height, color)
        a.startsWith("DTS:X") -> Logo(R.drawable.logo_dtsx, height * 1.15f, color)
        a.startsWith("DTS-HD MA") -> Logo(R.drawable.logo_dtshdma, height * 1.4f, color)
        a.startsWith("TrueHD") -> Logo(R.drawable.logo_truehd, height * 1.4f, color)
    }
}

@Composable
private fun Logo(res: Int, height: Dp, color: Color) {
    Image(painterResource(res), null, colorFilter = ColorFilter.tint(color), contentScale = ContentScale.FillHeight, modifier = Modifier.height(height))
}

/** Row heading. */
@Composable
fun RowTitle(text: String, modifier: Modifier = Modifier) {
    Text(text, fontSize = 18.sp, fontWeight = FontWeight.SemiBold, letterSpacing = (-0.2).sp, modifier = modifier.padding(bottom = 13.dp))
}

/** Stops D-pad Right at the last card of a row. */
fun Modifier.rowEnds(index: Int, count: Int): Modifier = onPreviewKeyEvent { e ->
    e.type == KeyEventType.KeyDown && e.key == Key.DirectionRight && index == count - 1
}

/**
 * The backdrop as Apple places it: top right, 3/4 of the width, fading out to the left and the bottom
 * so text on the left sits on plain black.
 */
@Composable
fun BoxScope.MaskedBackdrop(url: String?) {
    if (url == null) return
    Box(Modifier.align(Alignment.TopEnd).fillMaxWidth(0.75f).aspectRatio(16f / 9f)) {
        AsyncImage(url, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        Box(Modifier.matchParentSize().background(Brush.horizontalGradient(0f to Bg, 0.12f to Bg.copy(alpha = 0.85f), 0.28f to Bg.copy(alpha = 0.45f), 0.44f to Bg.copy(alpha = 0.1f), 0.56f to Color.Transparent)))
        Box(Modifier.matchParentSize().background(Brush.verticalGradient(0.46f to Color.Transparent, 0.7f to Bg.copy(alpha = 0.45f), 0.92f to Bg)))
    }
    Box(Modifier.matchParentSize().background(Brush.horizontalGradient(0f to Bg.copy(alpha = 0.5f), 0.5f to Color.Transparent)))
}
