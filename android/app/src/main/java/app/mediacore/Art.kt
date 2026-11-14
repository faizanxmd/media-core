package app.mediacore

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapShader
import android.graphics.Canvas
import android.graphics.ColorMatrix
import android.graphics.ColorMatrixColorFilter
import android.graphics.LinearGradient
import android.graphics.Matrix
import android.graphics.Paint
import android.graphics.PorterDuff
import android.graphics.PorterDuffXfermode
import android.graphics.RadialGradient
import android.graphics.RectF
import android.graphics.Shader
import android.util.LruCache
import androidx.compose.animation.core.Animatable
import androidx.compose.foundation.Canvas as ComposeCanvas
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import coil3.imageLoader
import coil3.request.ImageRequest
import coil3.request.SuccessResult
import coil3.request.allowHardware
import coil3.size.Precision
import coil3.toBitmap
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlin.random.Random

/**
 * Every screen's background: its picture, the layered gradients over it and the film grain, drawn once off the UI
 * thread into one picture the size of the screen (SPEC.md, Performance). Moving around then never redraws any of it,
 * and changing the picture is one fade between two finished pictures.
 */
enum class ArtStyle {
    Home, // board 01: the hero's art across the whole screen
    Film, // board 04: a film's title page
    Show, // board 09: a show's title page
    Poster, // board 07: no backdrop, so the poster, heavily blurred
    Episode, // board 10: the focused episode's still, top-right
    Blurred, // boards 11 and 12: blurred and darkened almost to black
    Plain, // board 13: nothing but the base and grain
}

/** What to draw: a picture (or none), how, and where the crop sits vertically (CSS object-position y). */
data class ArtSpec(val url: String?, val style: ArtStyle, val posY: Float = 0.5f)

object Art {
    private val cache = LruCache<ArtSpec, Bitmap>(6) // ~8 MB each at 1080p: a few screens to go back to instantly

    suspend fun bake(ctx: Context, spec: ArtSpec, w: Int, h: Int): Bitmap {
        cache.get(spec)?.let { return it }
        val src = spec.url?.let { load(ctx, it, if (spec.style == ArtStyle.Episode) w * 2 / 3 else w, h) }
        val out = withContext(Dispatchers.Default) { draw(spec, src, w, h) }
        cache.put(spec, out)
        return out
    }

    fun cached(spec: ArtSpec): Bitmap? = cache.get(spec)

    /** A picture as plain pixels (not a GPU-only bitmap: it's drawn into another), decoded near the size it's shown. */
    suspend fun load(ctx: Context, url: String, w: Int, h: Int): Bitmap? {
        val r = ctx.imageLoader.execute(
            ImageRequest.Builder(ctx).data(url).size(w, h).precision(Precision.INEXACT).allowHardware(false).build(),
        )
        return (r as? SuccessResult)?.image?.toBitmap()
    }

    private fun draw(spec: ArtSpec, src: Bitmap?, w: Int, h: Int): Bitmap {
        val out = Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888)
        val c = Canvas(out)
        c.drawColor(BASE)
        val f = w / 1920f // board pixels to screen pixels
        when (spec.style) {
            ArtStyle.Home -> {
                src?.let { cover(c, it, RectF(0f, 0f, w.toFloat(), h.toFloat()), 0.5f, spec.posY) }
                horizontal(c, w, h, BASE, 0f to .86f, .16f to .78f, .40f to .42f, .62f to 0f)
                vertical(c, w, h, BASE, .34f to 0f, .50f to .62f, .68f to .9f, 1f to .97f)
                vignette(c, w, h, .66f, .34f)
                topShade(c, w, h, .5f, 160 * f)
            }
            ArtStyle.Film, ArtStyle.Poster -> {
                if (spec.style == ArtStyle.Poster) {
                    src?.let { blurred(c, it, RectF(-80 * f, -80 * f, 2000 * f, 1160 * f), 0.5f, 0.3f, 8, 0.8f) }
                    c.drawColor(argb(.72f, BASE))
                } else src?.let { cover(c, it, RectF(0f, 0f, w.toFloat(), h.toFloat()), 0.5f, spec.posY) }
                horizontal(c, w, h, BASE, 0f to .9f, .20f to .82f, .44f to .44f, .66f to 0f)
                vertical(c, w, h, BASE, .36f to 0f, .56f to .66f, .72f to .94f, 1f to .98f)
                vignette(c, w, h, .66f, .30f)
                topShade(c, w, h, .62f, 200 * f)
            }
            ArtStyle.Show -> {
                src?.let { cover(c, it, RectF(0f, 0f, w.toFloat(), h.toFloat()), 0.5f, spec.posY) }
                horizontal(c, w, h, BASE, 0f to .9f, .20f to .82f, .44f to .44f, .66f to 0f)
                vertical(c, w, h, BASE, .34f to 0f, .52f to .66f, .70f to .95f, .86f to 1f)
                topShade(c, w, h, .62f, 200 * f)
            }
            ArtStyle.Episode -> {
                src?.let { blurred(c, it, RectF(w - 1280 * f, 0f, w.toFloat(), 720 * f), 0.5f, 0.5f, 1, 0.9f, scale = 2) }
                horizontal(c, w, h, BASE, 0f to 1f, .30f to 1f, .50f to .88f, 1f to .7f)
                vertical(c, w, h, BASE, 0f to .4f, .45f to .7f, .72f to 1f)
            }
            ArtStyle.Blurred -> {
                src?.let { blurred(c, it, RectF(-40 * f, -40 * f, 1960 * f, 1120 * f), 0.5f, spec.posY, 7, 0.8f, scale = 4) }
                c.drawColor(argb(.93f, BASE))
            }
            ArtStyle.Plain -> {}
        }
        grain(c, w, h)
        return out
    }

    // ---------- the layers, as the boards' CSS draws them ----------

    private val BASE = 0xFF090B0E.toInt()
    private val DEEP = 0xFF040507.toInt()

    private fun argb(a: Float, rgb: Int) = ((a * 255).toInt().coerceIn(0, 255) shl 24) or (rgb and 0xFFFFFF)

    /** object-fit: cover, with object-position (px, py). */
    private fun cover(c: Canvas, b: Bitmap, dst: RectF, px: Float, py: Float, paint: Paint? = null) {
        val s = maxOf(dst.width() / b.width, dst.height() / b.height)
        val dw = b.width * s
        val dh = b.height * s
        val l = dst.left + (dst.width() - dw) * px
        val t = dst.top + (dst.height() - dh) * py
        c.save()
        c.clipRect(dst)
        c.drawBitmap(b, null, RectF(l, t, l + dw, t + dh), paint ?: Paint(Paint.FILTER_BITMAP_FLAG))
        c.restore()
    }

    /**
     * filter: blur() saturate(), drawn into [dst]: the picture is shrunk by [scale] first, blurred there with three box
     * passes of [r] (a Gaussian, near enough), and stretched back up smoothly.
     */
    private fun blurred(c: Canvas, b: Bitmap, dst: RectF, px: Float, py: Float, r: Int, saturation: Float, scale: Int = 8) {
        val sw = maxOf(8, (dst.width() / scale).toInt())
        val sh = maxOf(8, (dst.height() / scale).toInt())
        val small = Bitmap.createBitmap(sw, sh, Bitmap.Config.ARGB_8888)
        cover(Canvas(small), b, RectF(0f, 0f, sw.toFloat(), sh.toFloat()), px, py)
        val pix = IntArray(sw * sh)
        small.getPixels(pix, 0, sw, 0, 0, sw, sh)
        repeat(3) { boxBlur(pix, sw, sh, r) }
        small.setPixels(pix, 0, sw, 0, 0, sw, sh)
        val p = Paint(Paint.FILTER_BITMAP_FLAG).apply { colorFilter = ColorMatrixColorFilter(ColorMatrix().apply { setSaturation(saturation) }) }
        c.drawBitmap(small, null, dst, p)
    }

    private fun horizontal(c: Canvas, w: Int, h: Int, rgb: Int, vararg stops: Pair<Float, Float>) =
        c.drawRect(0f, 0f, w.toFloat(), h.toFloat(), Paint().apply {
            shader = LinearGradient(0f, 0f, w.toFloat(), 0f, stops.map { argb(it.second, rgb) }.toIntArray(), stops.map { it.first }.toFloatArray(), Shader.TileMode.CLAMP)
        })

    private fun vertical(c: Canvas, w: Int, h: Int, rgb: Int, vararg stops: Pair<Float, Float>) =
        c.drawRect(0f, 0f, w.toFloat(), h.toFloat(), Paint().apply {
            shader = LinearGradient(0f, 0f, 0f, h.toFloat(), stops.map { argb(it.second, rgb) }.toIntArray(), stops.map { it.first }.toFloatArray(), Shader.TileMode.CLAMP)
        })

    /** radial-gradient(ellipse 78% 72% at cx cy, deep 0 46%, deep .6 100%). */
    private fun vignette(c: Canvas, w: Int, h: Int, cx: Float, cy: Float) {
        val rx = .78f * w
        val ry = .72f * h
        val g = RadialGradient(0f, 0f, rx, intArrayOf(argb(0f, DEEP), argb(0f, DEEP), argb(.6f, DEEP)), floatArrayOf(0f, .46f, 1f), Shader.TileMode.CLAMP)
        g.setLocalMatrix(Matrix().apply { setScale(1f, ry / rx); postTranslate(cx * w, cy * h) })
        c.drawRect(0f, 0f, w.toFloat(), h.toFloat(), Paint().apply { shader = g })
    }

    /** linear-gradient(180deg, deep a, transparent [to]px). */
    private fun topShade(c: Canvas, w: Int, h: Int, a: Float, to: Float) =
        c.drawRect(0f, 0f, w.toFloat(), to, Paint().apply {
            shader = LinearGradient(0f, 0f, 0f, to, argb(a, DEEP), argb(0f, DEEP), Shader.TileMode.CLAMP)
        })

    /** Faint film grain: grey noise laid over the picture in overlay mode at 8%, as the boards' feTurbulence does. */
    private fun grain(c: Canvas, w: Int, h: Int) {
        c.drawRect(0f, 0f, w.toFloat(), h.toFloat(), Paint().apply {
            shader = BitmapShader(grainTile, Shader.TileMode.REPEAT, Shader.TileMode.REPEAT)
            xfermode = PorterDuffXfermode(PorterDuff.Mode.OVERLAY)
            alpha = (0.08f * 255).toInt()
        })
    }

    private val grainTile: Bitmap by lazy {
        val n = 256
        val rnd = Random(7)
        val px = IntArray(n * n) {
            // a fine fractal noise: mostly per pixel, a little coarser texture under it
            val v = (128 + (rnd.nextFloat() + rnd.nextFloat() + rnd.nextFloat() - 1.5f) * 120).toInt().coerceIn(0, 255)
            val a = (128 + (rnd.nextFloat() - 0.5f) * 160).toInt().coerceIn(0, 255)
            (a shl 24) or (v shl 16) or (v shl 8) or v
        }
        Bitmap.createBitmap(px, n, n, Bitmap.Config.ARGB_8888)
    }

    /** One horizontal and one vertical box pass over [px]: three of these make a soft Gaussian. Keeps alpha. */
    fun boxBlur(px: IntArray, w: Int, h: Int, r: Int) {
        if (r <= 0) return
        val line = IntArray(maxOf(w, h))
        val d = 2 * r + 1
        fun run(n: Int, read: (Int) -> Int, write: (Int, Int) -> Unit) {
            var a = 0; var rr = 0; var g = 0; var b = 0
            for (k in -r..r) {
                val p = read(k.coerceIn(0, n - 1))
                a += p ushr 24; rr += p shr 16 and 255; g += p shr 8 and 255; b += p and 255
            }
            for (k in 0 until n) {
                line[k] = ((a / d) shl 24) or ((rr / d) shl 16) or ((g / d) shl 8) or (b / d)
                val add = read((k + r + 1).coerceAtMost(n - 1))
                val del = read((k - r).coerceAtLeast(0))
                a += (add ushr 24) - (del ushr 24)
                rr += (add shr 16 and 255) - (del shr 16 and 255)
                g += (add shr 8 and 255) - (del shr 8 and 255)
                b += (add and 255) - (del and 255)
            }
            for (k in 0 until n) write(k, line[k])
        }
        for (y in 0 until h) run(w, { px[y * w + it] }, { x, v -> px[y * w + x] = v })
        for (x in 0 until w) run(h, { px[it * w + x] }, { y, v -> px[y * w + x] = v })
    }
}

/**
 * The art under every screen. A screen asks for its picture with [show]; once it's drawn, it fades in over the old
 * one ([ms]: 400 for a new picture on the same screen, 250 when the screen itself changes).
 */
@Stable
class ArtState(private val ctx: Context, private val scope: CoroutineScope) {
    var w = 1920
    var h = 1080
    var front by mutableStateOf<ImageBitmap?>(null)
        private set
    var back by mutableStateOf<ImageBitmap?>(null)
        private set
    val fade = Animatable(1f)
    private var wanted: ArtSpec? = null
    private var job: Job? = null

    fun show(spec: ArtSpec, ms: Int) {
        if (spec == wanted) return
        wanted = spec
        job?.cancel()
        job = scope.launch {
            val bmp = runCatching { Art.bake(ctx, spec, w, h) }.getOrNull() ?: return@launch
            if (wanted != spec) return@launch
            val img = bmp.asImageBitmap()
            // a fade still running finishes where it was headed
            if (fade.value < 1f) { back = front; fade.snapTo(1f) }
            back = front
            front = img
            fade.snapTo(0f) // the first picture fades up from the base colour, later ones over the last

            fade.animateTo(1f, Move.fade(ms))
            back = null
        }
    }

    /** Draws a picture ahead, so the screen that wants it next finds it ready. */
    fun warm(spec: ArtSpec) {
        if (Art.cached(spec) != null) return
        scope.launch { runCatching { Art.bake(ctx, spec, w, h) } }
    }
}

val LocalArt = staticCompositionLocalOf<ArtState> { error("no art") }

@Composable
fun ArtLayer(art: ArtState, modifier: Modifier = Modifier) {
    ComposeCanvas(modifier.fillMaxSize()) {
        art.back?.let { drawImage(it) }
        art.front?.let { drawImage(it, alpha = art.fade.value) }
    }
}

// ---------- shadows ----------

/** The boards' box-shadows: offset, blur and strength. */
enum class ShadowKind(val layers: List<Triple<Float, Float, Float>>) { // (dy, blur radius, alpha), in board px
    Card(listOf(Triple(18f, 40f, .72f), Triple(2f, 6f, .55f))),
    Small(listOf(Triple(14f, 30f, .6f))), // episode stills, season posters
    Button(listOf(Triple(12f, 28f, .5f))),
}

/** A shadow drawn once per size, as a picture placed [pad] px out from the thing it's under. */
class BakedShadow(val bitmap: Bitmap, val pad: Int, val dy: Int = 0)

object Shadows {
    private val cache = HashMap<String, BakedShadow>()

    fun get(kind: ShadowKind, w: Int, h: Int): BakedShadow = synchronized(cache) {
        cache.getOrPut("$kind/$w/$h") { bake(kind, w, h) }
    }

    private fun bake(kind: ShadowKind, w: Int, h: Int): BakedShadow {
        val f = Px.screen // board px to screen px
        val pad = (kind.layers.maxOf { it.first + it.second } * f).toInt() + 2
        val bw = w + 2 * pad
        val bh = h + 2 * pad
        val out = Bitmap.createBitmap(bw, bh, Bitmap.Config.ARGB_8888)
        val c = Canvas(out)
        val radius = 6 * f
        for ((dy, blur, a) in kind.layers) {
            // CSS blur radius is two standard deviations; three box passes of r give a deviation of about r
            val sigma = blur * f / 2
            val scale = if (sigma > 6) 2 else 1
            val lw = bw / scale
            val lh = bh / scale
            val layer = Bitmap.createBitmap(lw, lh, Bitmap.Config.ARGB_8888)
            Canvas(layer).drawRoundRect(
                RectF(pad.toFloat() / scale, (pad + dy * f) / scale, (pad + w).toFloat() / scale, (pad + h + dy * f) / scale),
                radius / scale, radius / scale, Paint(Paint.ANTI_ALIAS_FLAG).apply { color = argb(a) },
            )
            val pix = IntArray(lw * lh)
            layer.getPixels(pix, 0, lw, 0, 0, lw, lh)
            val r = maxOf(1, (sigma / scale).toInt())
            repeat(3) { Art.boxBlur(pix, lw, lh, r) }
            layer.setPixels(pix, 0, lw, 0, 0, lw, lh)
            c.drawBitmap(layer, null, RectF(0f, 0f, bw.toFloat(), bh.toFloat()), Paint(Paint.FILTER_BITMAP_FLAG))
        }
        return BakedShadow(out, pad)
    }

    private fun argb(a: Float) = ((a * 255).toInt() shl 24) or 0x040507
}

// ---------- title logos ----------

/** A title's logo art with its drop shadow drawn in, [pad] px of shadow around it. */
class LogoArt(val bitmap: Bitmap, val pad: Int)

/** Logo art fitted into a box and given the boards' soft shadow (drop-shadow(0 6px 18px rgba(4,5,7,.7))), once. */
object Logos {
    private val cache = LruCache<String, LogoArt>(12)

    fun cached(url: String, w: Int, h: Int): LogoArt? = cache.get("$url/$w/$h")

    suspend fun get(ctx: Context, url: String, w: Int, h: Int): LogoArt? {
        cached(url, w, h)?.let { return it }
        val f = Px.screen
        val src = Art.load(ctx, url, (w * f).toInt(), (h * f).toInt()) ?: return null
        val out = withContext(Dispatchers.Default) {
            val s = minOf(w * f / src.width, h * f / src.height)
            val lw = maxOf(1, (src.width * s).toInt())
            val lh = maxOf(1, (src.height * s).toInt())
            val pad = (24 * f).toInt() + (6 * f).toInt()
            val bw = lw + 2 * pad
            val bh = lh + 2 * pad
            // the shadow: the logo's own shape, darkened, blurred and set 6 px lower
            val shadow = Bitmap.createBitmap(bw / 2, bh / 2, Bitmap.Config.ARGB_8888)
            Canvas(shadow).drawBitmap(
                src, null, RectF(pad / 2f, (pad + 6 * f) / 2f, (pad + lw) / 2f, (pad + lh + 6 * f) / 2f),
                Paint(Paint.FILTER_BITMAP_FLAG).apply {
                    colorFilter = ColorMatrixColorFilter(ColorMatrix(floatArrayOf(0f, 0f, 0f, 0f, 4f, 0f, 0f, 0f, 0f, 5f, 0f, 0f, 0f, 0f, 7f, 0f, 0f, 0f, 0.7f, 0f)))
                },
            )
            val pix = IntArray(shadow.width * shadow.height)
            shadow.getPixels(pix, 0, shadow.width, 0, 0, shadow.width, shadow.height)
            val r = maxOf(1, (9 * f / 2).toInt()) // 18 px blur: a deviation of 9, at half size
            repeat(3) { Art.boxBlur(pix, shadow.width, shadow.height, r) }
            shadow.setPixels(pix, 0, shadow.width, 0, 0, shadow.width, shadow.height)
            val out = Bitmap.createBitmap(bw, bh, Bitmap.Config.ARGB_8888)
            val c = Canvas(out)
            c.drawBitmap(shadow, null, RectF(0f, 0f, bw.toFloat(), bh.toFloat()), Paint(Paint.FILTER_BITMAP_FLAG))
            c.drawBitmap(src, null, RectF(pad.toFloat(), pad.toFloat(), (pad + lw).toFloat(), (pad + lh).toFloat()), Paint(Paint.FILTER_BITMAP_FLAG))
            LogoArt(out, pad)
        }
        cache.put("$url/$w/$h", out)
        return out
    }
}
