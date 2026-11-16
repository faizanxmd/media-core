package app.mediacore

import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.graphics.Typeface
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.KeyEvent
import android.view.ViewGroup
import android.view.WindowManager
import android.widget.FrameLayout
import androidx.activity.ComponentActivity
import androidx.activity.OnBackPressedCallback
import androidx.annotation.OptIn
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.platform.ComposeView
import androidx.compose.ui.platform.ViewCompositionStrategy
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.Format
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.MimeTypes
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.TrackSelectionOverride
import androidx.media3.common.Tracks
import androidx.media3.exoplayer.SeekParameters
import androidx.media3.exoplayer.upstream.DefaultBandwidthMeter
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DefaultDataSource
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.DefaultLoadControl
import androidx.media3.exoplayer.DefaultRenderersFactory
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.exoplayer.trackselection.DefaultTrackSelector
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.CaptionStyleCompat
import androidx.media3.ui.PlayerView
import androidx.media3.ui.SubtitleView
import app.mediacore.core.Api
import app.mediacore.core.Item
import app.mediacore.core.Offline
import app.mediacore.core.Speed
import app.mediacore.core.StallWatch
import app.mediacore.core.ThumbStore
import app.mediacore.core.mins
import app.mediacore.core.objects
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.cancel
import org.json.JSONArray
import org.json.JSONObject
import java.util.Locale
import kotlin.math.abs

/**
 * Direct play only: the original file is streamed over HTTP and decoded on
 * the device (MKV, HEVC, Dolby Vision, PGS/SRT/ASS subtitles; DTS/TrueHD via
 * FFmpeg only when the TV can't). The server never transcodes.
 * The controls are our own Compose overlay (PlayerUi.kt) on a bare PlayerView.
 */
@OptIn(UnstableApi::class)
class PlayerActivity : ComponentActivity(), PlayerActions {
    private lateinit var player: ExoPlayer
    private lateinit var view: PlayerView

    // ---------- the way in and out (the redesign's Motion): nothing else in the player changes ----------
    private lateinit var shade: android.view.View // over the video until its first frame, then fades away (300 ms)
    private lateinit var curtain: android.view.View // over everything: leaving fades the screen to black (200 ms)
    private var leaving = false
    private val ease = android.view.animation.PathInterpolator(0.2f, 0f, 0f, 1f)
    private val st = PlayerState()
    private var tv = false
    private val queue = mutableListOf<JSONObject>()
    private val ui = Handler(Looper.getMainLooper())
    private var currentId = ""
    private var finishedCurrent = false
    private var released = false
    private val localIds = mutableSetOf<String>() // playing a downloaded copy
    private val lightIds = mutableSetOf<String>() // playing the Light copy (a slow connection)
    private val stall = StallWatch()
    private var slowFor = "" // the title the slow-connection card was offered for: once per film
    private var slowAt = 0L // when it was offered; it goes after 30 s
    private val meter by lazy { DefaultBandwidthMeter.getSingletonInstance(this) }
    private var dismissedNext = "" // Back on the next-episode card: let the credits run
    private var nextLeftMs = 0L
    private var lastTick = 0L
    private val scope = MainScope()
    private var showLogo: String? = null // a show's title art: its episodes don't carry it

    // holding Left/Right
    private var holdKey = 0
    private var holdDir = 0
    private var holdStart = 0L
    private var holdLast = 0L
    private var holdFrom = Mode.Hidden
    private var holdResume = false
    private var holdOrigin = 0L // where you were before skipping or scrubbing: Back returns here
    private var holding = false // the key is down right now; the playhead stays after it comes up

    private val current get() = queue.firstOrNull { it.optString("id") == currentId }

    private val report10s = object : Runnable {
        override fun run() {
            if (player.isPlaying) report(false)
            ui.postDelayed(this, 10_000)
        }
    }

    /** Keeps the clock, the offers and the next-episode countdown current. Cheap when nothing is shown. */
    private val tick = object : Runnable {
        override fun run() {
            refresh()
            ui.postDelayed(this, if (st.mode != Mode.Hidden || st.offer != null) 250 else 1000)
        }
    }

    private val autoHide = Runnable {
        if (st.mode == Mode.Full && player.isPlaying && st.sheet == null && st.scrub == null) st.mode = Mode.Hidden
    }

    /** Moves the held position, faster the longer it's held: 1, then 3, then 8 minutes of film per second. */
    private val holdTick = object : Runnable {
        override fun run() {
            if (st.mode != Mode.Scrub || !holding) return
            val now = System.currentTimeMillis()
            val rate = when (now - holdStart) {
                in 0 until 1500 -> 60L
                in 1500 until 4000 -> 180L
                else -> 480L
            }
            st.scrub = ((st.scrub ?: 0L) + holdDir * rate * (now - holdLast)).coerceIn(0, maxOf(0, st.dur - 1000))
            holdLast = now
            ui.postDelayed(this, 50)
        }
    }
    private val hideSeek = Runnable { if (st.mode == Mode.Seek && st.scrub == null) st.mode = Mode.Hidden }
    private val commit = Runnable { commitScrub() }
    private val clearNote = Runnable { st.note = null }
    private val clearSkipped = Runnable { st.skipped = 0 }

    private fun refresh() {
        st.pos = player.currentPosition
        st.buffered = player.bufferedPosition
        st.dur = player.duration.takeIf { it != C.TIME_UNSET && it > 0 } ?: 0
        st.playing = player.playWhenReady // what you asked for: a moment's loading doesn't flip Pause to Play
        st.buffering = player.playbackState == Player.STATE_BUFFERING
        st.hasNext = player.hasNextMediaItem()
        stall.state(st.buffering, player.playbackState == Player.STATE_READY, player.playWhenReady)
        if (slowFor != currentId && stall.stuck()) checkSlow()
        val o = offer()
        if (o?.kind == "next" && st.offer?.kind != "next") nextLeftMs = 10_000
        st.offer = o
        val now = System.currentTimeMillis()
        // the countdown runs only while you can see it: not under a panel, nor while the controls hide the card
        if (o?.kind == "next" && player.isPlaying && st.sheet == null && !(tv && st.mode == Mode.Full)) {
            nextLeftMs -= (now - lastTick).coerceIn(0, 1000)
            if (nextLeftMs <= 0) takeOffer()
        }
        st.nextLeft = if (st.offer?.kind == "next") nextLeftMs.coerceIn(0, 10_000) else 0
        lastTick = now
    }

    /** Skip Intro inside the intro; the next-episode card in the credits / last 40 s of an episode. */
    private fun offer(): Offer? {
        val j = current ?: return null
        val pos = player.currentPosition / 1000.0
        val dur = st.dur / 1000.0
        val intro = j.optJSONObject("intro")
        val creditsAt = j.optJSONObject("credits")?.optDouble("start") ?: (if (dur > 0) dur - 40 else Double.MAX_VALUE)
        return when {
            slowFor == currentId && slowAt > 0 && System.currentTimeMillis() - slowAt < 30_000 && currentId !in lightIds ->
                Offer("slow", "Slow connection · Switch to Light", "", null)
            intro != null && pos >= intro.optDouble("start") && pos < intro.optDouble("end") - 2 -> Offer("intro", "Skip intro", "", null)
            player.hasNextMediaItem() && j.optString("type") == "episode" && dismissedNext != currentId &&
                pos >= creditsAt && dur - pos > 3 -> {
                val n = queue.getOrNull(player.nextMediaItemIndex) ?: return null
                Offer(
                    "next", n.optString("title"), "Season ${n.optInt("season")}, Episode ${n.optInt("episode")}",
                    Api.img(n.optJSONObject("meta")?.optString("still")?.ifEmpty { null }, "w300"),
                )
            }
            else -> null
        }
    }

    // ---------- PlayerActions (the overlay calls these) ----------

    override fun togglePlay() {
        if (player.playbackState == Player.STATE_ENDED) player.seekTo(0)
        if (player.playWhenReady) {
            player.pause()
            showFull() // paused: title, bar and menu stay up
        } else {
            player.play()
            if (tv) st.mode = Mode.Hidden // back to just the picture, at once
            else if (st.mode == Mode.Full) poke()
        }
        st.playing = player.playWhenReady
    }

    /**
     * A press of Left/Right, as streaming apps do it: the film jumps 10 s at once and plays on. Nothing else appears,
     * only the turning mark with the seconds; taps add up. Each jump goes to the nearest keyframe, so it's instant.
     */
    private fun tapSkip(dir: Int) {
        ui.removeCallbacks(settle)
        ui.removeCallbacks(commit)
        ui.removeCallbacks(hideSeek)
        ui.removeCallbacks(autoHide)
        ui.removeCallbacks(clearSkipped)
        if (st.scrub == null) { holdOrigin = player.currentPosition; st.scrubFrom = holdOrigin }
        val from = st.scrub ?: player.currentPosition
        val to = (from + dir * 10_000L).coerceIn(0, maxOf(0, st.dur - 1000))
        st.scrub = to
        if (st.skipped != 0 && (st.skipped > 0) != (dir > 0)) st.skipped = 0 // turned back: count afresh
        st.skipped += ((to - from) / 1000).toInt() // what the film really moves, near its start or end
        st.taps++
        seekSync(to)
        st.thumbs?.ensure() // ready in case the next press is a hold
        ui.postDelayed(clearSkipped, 900)
        ui.postDelayed(settle, 600)
    }

    /** The taps have stopped: the film is already there, so only forget the target the taps were adding to. */
    private val settle = Runnable {
        st.scrub = null
        refresh()
        if (st.mode == Mode.Full) poke()
    }

    /**
     * Left/Right held: the film holds still and the picture strip takes over. It stays after the key
     * comes up, so you can go back and forth: Left/Right step, a hold scrubs, OK lands and plays, Back cancels.
     */
    private fun startHold(dir: Int, key: Int) {
        ui.removeCallbacks(clearSkipped)
        st.skipped = 0
        ui.removeCallbacks(commit)
        ui.removeCallbacks(settle)
        ui.removeCallbacks(autoHide)
        holdFrom = st.mode
        holdResume = player.playWhenReady
        if (st.scrub == null) holdOrigin = player.currentPosition
        st.scrub = st.scrub ?: player.currentPosition
        player.playWhenReady = false
        st.mode = Mode.Scrub
        view.subtitleView?.visibility = android.view.View.INVISIBLE
        st.thumbs?.ensure()
        runHold(dir, key)
    }

    private fun runHold(dir: Int, key: Int) {
        holdKey = key
        holdDir = dir
        holdStart = System.currentTimeMillis()
        holdLast = holdStart
        holding = true
        ui.removeCallbacks(holdTick)
        ui.post(holdTick)
    }

    private fun stopHold() {
        holding = false
        ui.removeCallbacks(holdTick)
    }

    /** A hold cut short by something else (the next episode, an error): stop it and bring the subtitles back. */
    private fun abandonHold() {
        if (st.mode != Mode.Scrub && !holding) return
        stopHold()
        view.subtitleView?.visibility = android.view.View.VISIBLE
        st.scrub = null
    }

    /** OK: land there and play. */
    private fun landHold() {
        stopHold()
        view.subtitleView?.visibility = android.view.View.VISIBLE
        st.focusTo = null
        st.mode = Mode.Hidden
        commitScrub()
        player.playWhenReady = true
    }

    /** Back: as if nothing happened. */
    private fun cancelHold() {
        stopHold()
        view.subtitleView?.visibility = android.view.View.VISIBLE
        st.focusTo = null
        st.mode = holdFrom
        st.scrub = null
        // the press that began the hold already jumped 10 s: go back to where you were
        if (abs(player.currentPosition - holdOrigin) > 1500) seekSync(holdOrigin)
        if (holdResume) player.playWhenReady = true
        poke()
        if (st.mode == Mode.Seek) ui.postDelayed(hideSeek, 1200)
    }

    override fun scrub(deltaMs: Long) {
        if (st.mode == Mode.Hidden) st.mode = Mode.Seek
        if (st.scrub == null) st.scrubFrom = player.currentPosition
        val from = st.scrub ?: player.currentPosition
        st.scrub = (from + deltaMs).coerceIn(0, maxOf(0, st.dur - 1000))
        ui.removeCallbacks(commit)
        ui.removeCallbacks(hideSeek)
        ui.removeCallbacks(autoHide)
        ui.postDelayed(commit, 400) // one seek when you stop pressing, not one per press
    }

    override fun scrubTo(ms: Long, commit: Boolean) {
        st.scrub = ms.coerceIn(0, maxOf(0, st.dur - 1000))
        ui.removeCallbacks(this.commit)
        if (commit) commitScrub()
    }

    override fun commitScrub() {
        ui.removeCallbacks(commit)
        val to = st.scrub ?: return
        seekSync(to)
        st.scrub = null
        refresh()
        if (st.mode == Mode.Seek) ui.postDelayed(hideSeek, 1200) else poke()
    }

    /**
     * Lands on the nearest keyframe: the picture is there at once. An exact seek decodes forward from the keyframe
     * before, which is the freeze a skip used to have.
     */
    private fun seekSync(to: Long) {
        player.setSeekParameters(SeekParameters.CLOSEST_SYNC)
        player.seekTo(to)
    }

    override fun next() {
        if (!player.hasNextMediaItem()) return
        report(false)
        player.seekToNextMediaItem()
        poke()
    }

    /** Stuck on a connection that can't carry this title: offer its Light copy, once per film. */
    private fun checkSlow() {
        val j = current ?: return
        if (currentId in lightIds || currentId in localIds || j.optJSONObject("light") == null) return
        val need = (j.optJSONObject("info")?.optDouble("mbps", 0.0) ?: 0.0) * 1e6
        if (need > 0 && meter.bitrateEstimate >= need) { stall.clear(); return } // the connection keeps up: a hiccup
        slowFor = currentId
        slowAt = System.currentTimeMillis()
    }

    /** Reopens the same title's Light copy where it was, which takes a few seconds. */
    private fun switchToLight() {
        val j = current ?: return
        val index = queue.indexOf(j)
        val pos = player.currentPosition
        lightIds += currentId
        slowAt = 0L
        stall.clear()
        // a track picked in the original file isn't in the Light one; the language preference carries over
        player.trackSelectionParameters = player.trackSelectionParameters.buildUpon().clearOverrides().build()
        player.setMediaItems(queue.map(::mediaItem), index, pos)
        player.prepare()
        player.playWhenReady = true
    }

    override fun takeOffer() {
        when (st.offer?.kind) {
            "slow" -> switchToLight()
            "intro" -> current?.optJSONObject("intro")?.let { seekSync((it.optDouble("end") * 1000).toLong()) }
            "next" -> {
                send(currentId, 0, 0, ended = true)
                finishedCurrent = true
                player.seekToNextMediaItem()
            }
        }
        st.offer = null
    }

    override fun openSheet(s: Sheet) {
        when (s) {
            Sheet.Tracks -> buildTrackOptions(player.currentTracks)
            Sheet.Episodes -> buildEpisodes()
        }
        st.sheet = s
        ui.removeCallbacks(autoHide)
    }

    override fun closeSheet() {
        st.sheet = null
        st.focusTo = st.lastKey
        if (st.mode != Mode.Full) st.mode = Mode.Full
        poke()
    }

    /** Back on the next-episode buttons, or Watch credits: let the credits run. */
    override fun dismissOffer() {
        when (st.offer?.kind) {
            "next" -> dismissedNext = currentId
            "slow" -> slowAt = 0L
        }
        st.offer = null
        if (st.mode == Mode.Full) poke()
    }

    /** Start over: back to the first frame, playing. */
    override fun startOver() {
        seekSync(0)
        player.playWhenReady = true
        st.mode = Mode.Hidden
    }

    private val settings by lazy { getSharedPreferences("settings", MODE_PRIVATE) }

    /** Small, Medium or Large, kept for every title. */
    override fun subSize(size: Int) {
        st.subSize = size.coerceIn(0, SubSizes.lastIndex)
        settings.edit().putString("subsize", SubSizes[st.subSize]).apply()
        applySubSize()
    }

    private fun applySubSize() {
        val scale = listOf(0.86f, 1.02f, 1.24f)[st.subSize]
        view.subtitleView?.setFractionalTextSize(SubtitleView.DEFAULT_TEXT_SIZE_FRACTION * scale)
    }

    override fun tapped() {
        when {
            st.sheet != null -> closeSheet()
            st.mode == Mode.Full -> st.mode = Mode.Hidden
            else -> showFull()
        }
    }

    override fun retry() {
        st.error = null
        player.prepare()
        player.play()
    }

    override fun close() = finish()

    private fun showFull(menu: Boolean = false) {
        ui.removeCallbacks(hideSeek)
        st.focusTo = if (menu) "pill/0" else null // Down: straight to the audio and subtitle pills
        st.mode = Mode.Full
        refresh()
        poke()
    }

    /** Restarts the auto-hide countdown (only while playing; paused stays up). */
    private fun poke() {
        ui.removeCallbacks(autoHide)
        if (player.playWhenReady) ui.postDelayed(autoHide, 5000)
    }

    private fun note(text: String) {
        st.note = text
        ui.removeCallbacks(clearNote)
        ui.postDelayed(clearNote, 2500)
    }

    /** The queue as a list to jump around in (episodes of the show). */
    private fun buildEpisodes() {
        st.episodes = if (queue.size < 2) emptyList() else queue.mapIndexed { i, j ->
            val dur = j.optJSONObject("info")?.optDouble("dur", 0.0) ?: 0.0
            TrackOption(
                "${j.optInt("episode")}. ${j.optString("title")}",
                dur.takeIf { it > 0 }?.let { mins((it * 1000).toLong()) } ?: "",
                j.optString("id") == currentId,
                Api.img(j.optJSONObject("meta")?.optString("still")?.ifEmpty { null }, "w500"),
            ) {
                st.sheet = null
                if (j.optString("id") != currentId) {
                    report(false)
                    player.seekTo(i, 0)
                }
                st.mode = Mode.Hidden
            }
        }
    }

    private fun loadThumbs(j: JSONObject?) {
        // pictures for scrubbing: fetched right away at home, only when first needed on a thin link
        if (j != null) st.thumbs?.load(j.optString("id"), j.optInt("thumbs"), eager = !Api.away)
    }

    // ---------- lifecycle ----------

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        WindowCompat.setDecorFitsSystemWindows(window, false)
        WindowInsetsControllerCompat(window, window.decorView).apply {
            hide(WindowInsetsCompat.Type.systemBars())
            systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }

        Api.init(this)
        Px.init(this)
        Speed.start(this)
        Speed.freshen() // an old result on this network: measure again while it plays
        Offline.init(this, overMetered = true)
        val arr = JSONArray(intent.getStringExtra("queue") ?: bigQueue.also { bigQueue = null } ?: "[]")
        for (i in 0 until arr.length()) queue += arr.getJSONObject(i)
        if (queue.isEmpty()) { finish(); return }
        val index = intent.getIntExtra("index", 0).coerceIn(0, queue.size - 1)
        val startPos = intent.getLongExtra("pos", 0L)

        tv = MainActivity.isTv(this)
        showLogo = intent.getStringExtra("logo")
        if (tv) st.thumbs = ThumbStore(this, scope)
        val fps =queue[index].optJSONObject("info")?.optDouble("fps", 0.0) ?: 0.0
        switchedRate = tv && matchRefreshRate(fps)

        player = newPlayer()
        buildViews()
        currentId = queue[index].optString("id")
        showTitle(queue[index])
        loadThumbs(queue[index])
        buildEpisodes()
        start(index, startPos)
        ui.postDelayed(report10s, 10_000)
        ui.post(tick)

        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                when {
                    st.error != null -> finish()
                    st.sheet != null -> closeSheet()
                    st.scrub != null && st.mode == Mode.Seek -> { ui.removeCallbacks(commit); st.scrub = null; st.mode = Mode.Hidden } // cancel the seek
                    st.scrub != null -> { ui.removeCallbacks(settle); settle.run(); if (st.mode == Mode.Full && player.playWhenReady) st.mode = Mode.Hidden else if (st.mode == Mode.Hidden) finish() } // taps already landed
                    st.mode == Mode.Full && tv -> if (player.playWhenReady) st.mode = Mode.Hidden else finish() // paused: leave
                    st.mode == Mode.Seek -> st.mode = Mode.Hidden
                    st.offer?.kind == "next" || st.offer?.kind == "slow" -> dismissOffer() // keep watching as it is
                    else -> finish()
                }
            }
        })
    }

    private var switchedRate = false

    private fun newPlayer(): ExoPlayer {
        val renderers = DefaultRenderersFactory(this)
            // the TV's own decoders first (DTS, DD+, Atmos...); FFmpeg only for what it can't do (TrueHD etc.)
            .setExtensionRendererMode(DefaultRenderersFactory.EXTENSION_RENDERER_MODE_ON)
            .setEnableDecoderFallback(true)
        val load = DefaultLoadControl.Builder()
            // start (and resume after a seek) at 0.5 s buffered; keep up to ~50 s ahead and the last
            // 20 s behind, so skips both ways stay in memory instead of reloading from the server.
            // Capped at 128 MB (largeHeap, about what big streaming apps use): all 70 s of a 15 Mb/s WEB-DL
            .setBufferDurationsMs(15_000, 50_000, 500, 2_000)
            .setBackBuffer(20_000, true)
            .setTargetBufferBytes(128 * 1024 * 1024)
            .setPrioritizeTimeOverSizeThresholds(false)
            .build()
        val p = ExoPlayer.Builder(this, renderers)
            .setTrackSelector(DefaultTrackSelector(this))
            .setLoadControl(load)
            .setBandwidthMeter(meter) // how fast data is arriving, for the slow-connection card
            // http(s) through OkHttp; file:// (offline downloads) through the plain file reader
            .setMediaSourceFactory(DefaultMediaSourceFactory(DefaultDataSource.Factory(this, OkHttpDataSource.Factory(Api.http))))
            .setAudioAttributes(
                AudioAttributes.Builder().setUsage(C.USAGE_MEDIA).setContentType(C.AUDIO_CONTENT_TYPE_MOVIE).build(),
                true,
            )
            .setHandleAudioBecomingNoisy(true)
            .build()
        // land on the nearest keyframe: seeks are instant instead of decoding up to the exact frame
        p.setSeekParameters(SeekParameters.CLOSEST_SYNC)
        if (switchedRate) p.videoChangeFrameRateStrategy = C.VIDEO_CHANGE_FRAME_RATE_STRATEGY_OFF
        p.addListener(listener)
        return p
    }

    private fun start(index: Int, pos: Long) {
        applyLanguagePrefs(queue[index])
        player.setMediaItems(queue.map(::mediaItem), index, pos)
        player.prepare()
        player.playWhenReady = true
    }

    // ---------- per-show audio/subtitle language memory ----------

    private val langPrefs by lazy { getSharedPreferences("languages", MODE_PRIVATE) }

    /** Movies remember per film, episodes per show. */
    private fun langKey(j: JSONObject) = j.optString("show_id").ifEmpty { j.optString("id") }

    private fun applyLanguagePrefs(j: JSONObject) {
        val k = langKey(j)
        val audio = langPrefs.getString("a:$k", null)
        val text = langPrefs.getString("t:$k", null)
        player.trackSelectionParameters = player.trackSelectionParameters.buildUpon().apply {
            if (audio != null) setPreferredAudioLanguage(audio)
            when (text) {
                null -> {}
                // off stays off, as when it was picked: forced or default tracks don't switch it back on
                "off" -> setIgnoredTextSelectionFlags(C.SELECTION_FLAG_DEFAULT).setPreferredTextLanguage(null).setTrackTypeDisabled(C.TRACK_TYPE_TEXT, true)
                else -> setTrackTypeDisabled(C.TRACK_TYPE_TEXT, false).setPreferredTextLanguage(text)
            }
        }.build()
    }

    // ---------- the audio & subtitles menu ----------

    private fun buildTrackOptions(tracks: Tracks) {
        val audio = mutableListOf<TrackOption>()
        val text = mutableListOf<TrackOption>()
        var anyText = false
        for (g in tracks.groups) {
            for (i in 0 until g.length) {
                if (!g.isTrackSupported(i)) continue
                val f = g.getTrackFormat(i)
                val sel = g.isTrackSelected(i)
                when (g.type) {
                    C.TRACK_TYPE_AUDIO -> audio += TrackOption(langName(f.language, f.label), audioDetail(f), sel) { pickAudio(g, i) }
                    C.TRACK_TYPE_TEXT -> {
                        if (sel) anyText = true
                        text += TrackOption(langName(f.language, f.label), textDetail(f), sel) { pickText(g, i) }
                    }
                }
            }
        }
        if (text.isNotEmpty()) text.add(0, TrackOption("Off", "", !anyText) { pickText(null, 0) })
        st.audio = audio
        st.text = text
        st.combos = combos(tracks)
    }

    /**
     * Pills under the bar, as streaming apps show them: the likely pairs, picked in one press. Each of the first two audio languages
     * on its own, then the language playing with its own subtitles and with English ones. Other... has the rest.
     */
    private fun combos(tracks: Tracks): List<TrackOption> {
        class T(val g: Tracks.Group, val i: Int, val lang: String, val selected: Boolean, val forced: Boolean)
        val audio = mutableListOf<T>()
        val text = mutableListOf<T>()
        for (g in tracks.groups) for (i in 0 until g.length) {
            if (!g.isTrackSupported(i)) continue
            val f = g.getTrackFormat(i)
            val lang = f.language?.takeIf { it.isNotEmpty() && it != "und" }?.let { Locale.forLanguageTag(it).language } ?: continue
            val forced = f.selectionFlags and C.SELECTION_FLAG_FORCED != 0 || f.label?.contains("forced", true) == true
            when (g.type) {
                C.TRACK_TYPE_AUDIO -> audio += T(g, i, lang, g.isTrackSelected(i), forced)
                C.TRACK_TYPE_TEXT -> text += T(g, i, lang, g.isTrackSelected(i), forced)
            }
        }
        val now = audio.firstOrNull { it.selected } ?: audio.firstOrNull() ?: return emptyList()
        val subOn = text.firstOrNull { it.selected && !it.forced }
        val name = { l: String -> langName(l, null) }
        val out = mutableListOf<TrackOption>()
        val langs = (listOf(now) + audio).distinctBy { it.lang }.take(2)
        for (a in langs) out += TrackOption(name(a.lang), "", a.lang == now.lang && subOn == null) { pickAudio(a.g, a.i); pickText(null, 0) }
        val subs = text.filter { !it.forced && (it.lang == now.lang || it.lang == "en") }.distinctBy { it.lang }.sortedBy { if (it.lang == now.lang) 0 else 1 }
        for (t in subs) {
            val words = if (t.lang == now.lang) "${name(now.lang)} with Subtitles" else "${name(now.lang)} with ${name(t.lang)} Subtitles"
            out += TrackOption(words, "", subOn?.lang == t.lang) { pickAudio(now.g, now.i); pickText(t.g, t.i) }
        }
        return out.take(4)
    }

    private fun pickAudio(g: Tracks.Group, i: Int) {
        val lang = g.getTrackFormat(i).language
        player.trackSelectionParameters = player.trackSelectionParameters.buildUpon()
            .setOverrideForType(TrackSelectionOverride(g.mediaTrackGroup, i))
            .apply { if (lang != null) setPreferredAudioLanguage(lang) }
            .build()
        current?.let { j -> lang?.let { langPrefs.edit().putString("a:${langKey(j)}", it).apply() } }
    }

    private fun pickText(g: Tracks.Group?, i: Int) {
        val b = player.trackSelectionParameters.buildUpon()
        val k = current?.let(::langKey)
        if (g == null) {
            b.clearOverridesOfType(C.TRACK_TYPE_TEXT).setTrackTypeDisabled(C.TRACK_TYPE_TEXT, true)
            k?.let { langPrefs.edit().putString("t:$it", "off").apply() }
        } else {
            val lang = g.getTrackFormat(i).language
            b.setTrackTypeDisabled(C.TRACK_TYPE_TEXT, false).setIgnoredTextSelectionFlags(0)
                .setOverrideForType(TrackSelectionOverride(g.mediaTrackGroup, i))
                .setPreferredTextLanguage(lang)
            if (k != null && lang != null) langPrefs.edit().putString("t:$k", lang).apply()
        }
        player.trackSelectionParameters = b.build()
    }

    /** "English": the language, in the TV's own words. A file's raw track title is only the last resort. */
    private fun langName(code: String?, label: String?): String {
        val name = code?.takeIf { it.isNotEmpty() && it != "und" }
            ?.let { Locale.forLanguageTag(it).getDisplayLanguage(Locale.getDefault()) }
            ?.takeIf { it.isNotEmpty() && it != code }
        return name ?: label?.substringBefore('|')?.trim()?.takeIf { it.isNotEmpty() } ?: "Unknown"
    }

    /** "Dolby Digital+ 5.1": what it is, never a file's raw track title. */
    private fun audioDetail(f: Format): String {
        val codec = when (f.sampleMimeType) {
            MimeTypes.AUDIO_E_AC3_JOC -> "Dolby Atmos"
            MimeTypes.AUDIO_E_AC3 -> "Dolby Digital+"
            MimeTypes.AUDIO_AC3 -> "Dolby Digital"
            MimeTypes.AUDIO_TRUEHD -> "TrueHD"
            MimeTypes.AUDIO_DTS_X -> "DTS:X"
            MimeTypes.AUDIO_DTS_HD -> "DTS-HD"
            MimeTypes.AUDIO_DTS, MimeTypes.AUDIO_DTS_EXPRESS -> "DTS"
            MimeTypes.AUDIO_AAC -> "AAC"
            MimeTypes.AUDIO_OPUS -> "Opus"
            MimeTypes.AUDIO_FLAC -> "FLAC"
            MimeTypes.AUDIO_VORBIS -> "Vorbis"
            MimeTypes.AUDIO_MPEG -> "MP3"
            MimeTypes.AUDIO_RAW -> "PCM"
            else -> ""
        }
        val ch = when (f.channelCount) {
            Format.NO_VALUE -> ""
            1 -> "Mono"
            2 -> "Stereo"
            6 -> "5.1"
            8 -> "7.1"
            else -> "${f.channelCount} ch"
        }
        val commentary = "Commentary".takeIf { f.label?.contains("commentary", ignoreCase = true) == true }
        return listOfNotNull(commentary, listOf(codec, ch).filter { it.isNotEmpty() }.joinToString(" ").ifEmpty { null }).joinToString(" · ")
    }

    /** "SDH", "Forced": only what tells two subtitle tracks apart. */
    private fun textDetail(f: Format): String {
        val label = f.label.orEmpty()
        return listOfNotNull(
            "Forced".takeIf { f.selectionFlags and C.SELECTION_FLAG_FORCED != 0 || label.contains("forced", ignoreCase = true) },
            "SDH".takeIf { f.roleFlags and C.ROLE_FLAG_DESCRIBES_MUSIC_AND_SOUND != 0 || Regex("""(?i)\b(sdh|cc|hi)\b""").containsMatchIn(label) },
            "Commentary".takeIf { label.contains("commentary", ignoreCase = true) },
        ).joinToString(" · ")
    }

    // ---------- media items ----------

    private fun mediaItem(j: JSONObject): MediaItem {
        val id = j.optString("id")
        val local = Offline.localVideo(id)
        if (local != null) localIds += id
        // Original or Light, by this connection's last measured speed; once switched to Light, it stays
        val light = local == null && (id in lightIds || Speed.useLight(j))
        if (light) lightIds += id
        val subs = j.optJSONArray("subs").objects().mapIndexed { n, s ->
            val subUri = if (local != null) Offline.localSub(id, n)?.let { Uri.fromFile(it) } else null
            MediaItem.SubtitleConfiguration.Builder(subUri ?: Uri.parse(Api.sub(id, n)))
                .setMimeType(
                    when (s.optString("mime")) {
                        "text/x-ssa" -> MimeTypes.TEXT_SSA
                        "text/vtt" -> MimeTypes.TEXT_VTT
                        else -> MimeTypes.APPLICATION_SUBRIP
                    },
                )
                .setLanguage(s.optString("lang").ifEmpty { null })
                .setLabel(s.optString("label"))
                .setSelectionFlags(if (s.optBoolean("forced")) C.SELECTION_FLAG_FORCED else 0)
                .build()
        }
        val (title, sub) = titles(j)
        return MediaItem.Builder()
            .setUri(local?.let { Uri.fromFile(it) } ?: Uri.parse(if (light) Api.stream(id) + "?v=light" else Api.stream(id)))
            .setMediaId(id)
            .setSubtitleConfigurations(subs)
            .setMediaMetadata(MediaMetadata.Builder().setTitle(title).setSubtitle(sub).build())
            .build()
    }

    private fun titles(j: JSONObject): Pair<String, String> {
        val meta = j.optJSONObject("meta")
        return if (j.optString("type") == "episode") {
            j.optString("show") to "S${j.optInt("season")}, E${j.optInt("episode")} · ${j.optString("title")}"
        } else {
            (meta?.optString("title")?.ifEmpty { null } ?: j.optString("title")) to
                (j.optInt("year").takeIf { it > 0 }?.toString() ?: "")
        }
    }

    private fun showTitle(j: JSONObject) {
        val (t, s) = titles(j)
        st.title = t
        st.sub = s
        val m = j.optJSONObject("meta")
        st.logo = Api.img(m?.optString("logo")?.ifEmpty { null }, "w500") ?: showLogo
        st.cover =Api.img(m?.optString("backdrop")?.ifEmpty { null }, "w1280")
            ?: Api.img(m?.optString("still")?.ifEmpty { null }, "w780")
    }

    private val listener = object : Player.Listener {
        override fun onMediaItemTransition(item: MediaItem?, reason: Int) {
            if (reason == Player.MEDIA_ITEM_TRANSITION_REASON_AUTO && currentId.isNotEmpty()) {
                send(currentId, 0, 0, ended = true) // the previous episode played to the end
            }
            val changed = item?.mediaId != currentId
            currentId = item?.mediaId ?: ""
            finishedCurrent = false
            current?.let { showTitle(it); loadThumbs(it) }
            buildEpisodes()
            if (changed) {
                stall.clear()
                st.firstFrame = false
                abandonHold()
                st.mode = Mode.Hidden
                // a track picked for one episode doesn't exist in the next; the language preference carries over
                player.trackSelectionParameters = player.trackSelectionParameters.buildUpon().clearOverrides().build()
            }
            refresh()
        }

        override fun onRenderedFirstFrame() {
            st.firstFrame = true
            if (::shade.isInitialized && shade.alpha > 0f) shade.animate().alpha(0f).setDuration(if (Move.reduced) 100 else 300).setInterpolator(ease).start()
        }

        override fun onPositionDiscontinuity(oldPosition: Player.PositionInfo, newPosition: Player.PositionInfo, reason: Int) {
            if (reason == Player.DISCONTINUITY_REASON_SEEK) stall.reset() // loading after a skip isn't a stall
        }

        override fun onIsPlayingChanged(isPlaying: Boolean) {
            st.playing = player.playWhenReady
            if (!isPlaying && player.playbackState == Player.STATE_READY) report(false) // paused
            if (isPlaying && st.mode == Mode.Full) poke()
        }

        override fun onPlaybackStateChanged(state: Int) {
            refresh()
            if (state == Player.STATE_ENDED) {
                finishedCurrent = true
                send(currentId, 0, 0, ended = true)
                finish()
            }
        }

        override fun onTracksChanged(tracks: Tracks) {
            buildTrackOptions(tracks) // the pills under the bar follow what's playing
            // audio-only files never render a frame
            if (!tracks.isTypeSupported(C.TRACK_TYPE_VIDEO)) st.firstFrame = true
        }

        override fun onPlayerError(error: PlaybackException) {
            report(false)
            val decoder = error.errorCode in listOf(
                PlaybackException.ERROR_CODE_DECODING_FAILED, PlaybackException.ERROR_CODE_DECODER_INIT_FAILED,
                PlaybackException.ERROR_CODE_DECODER_QUERY_FAILED, PlaybackException.ERROR_CODE_DECODING_FORMAT_UNSUPPORTED,
                PlaybackException.ERROR_CODE_DECODING_FORMAT_EXCEEDS_CAPABILITIES,
            )
            val network = error.errorCode in PlaybackException.ERROR_CODE_IO_UNSPECIFIED..PlaybackException.ERROR_CODE_IO_READ_POSITION_OUT_OF_RANGE
            val hint = when {
                decoder -> "This TV can't decode this video."
                network -> "Lost the connection to ${Api.name}. Check that you're on the home Wi-Fi."
                else -> "Something went wrong while playing."
            }
            abandonHold()
            st.mode = Mode.Hidden
            st.sheet = null
            st.error = PlayError("Can't play this", hint, "${error.errorCodeName}  ${error.cause?.message ?: error.message ?: ""}".trim())
        }
    }

    private fun report(ended: Boolean) {
        if (currentId.isEmpty() || finishedCurrent || released) return
        val dur = player.duration.takeIf { it != C.TIME_UNSET && it > 0 } ?: 0
        // while the picture strip is up the film sits at a preview, not where you are: report where you were
        val pos = if (st.mode == Mode.Scrub) holdOrigin else player.currentPosition
        send(currentId, pos, dur, ended)
    }

    private fun send(id: String, pos: Long, dur: Long, ended: Boolean) {
        Api.progress(id, pos, dur, ended)
        if (id in localIds) Offline.savePos(id, pos, dur, ended) // synced to the server when back online
    }

    // ---------- remote control ----------

    /** -1 or 1 for the keys that move through the film, else 0. */
    private fun skipDir(keyCode: Int) = when (keyCode) {
        KeyEvent.KEYCODE_DPAD_LEFT, KeyEvent.KEYCODE_MEDIA_REWIND -> -1
        KeyEvent.KEYCODE_DPAD_RIGHT, KeyEvent.KEYCODE_MEDIA_FAST_FORWARD -> 1
        else -> 0
    }

    /** The strip is up: Left/Right step, a hold scrubs, OK lands and plays, Back cancels. Nothing else counts. */
    private fun scrubKeys(event: KeyEvent) {
        val down = event.action == KeyEvent.ACTION_DOWN
        val first = down && event.repeatCount == 0
        val dir = skipDir(event.keyCode)
        when {
            dir != 0 -> when {
                first -> st.scrub = ((st.scrub ?: player.currentPosition) + dir * 10_000L).coerceIn(0, maxOf(0, st.dur - 1000))
                down -> if (!holding) runHold(dir, event.keyCode)
                event.keyCode == holdKey -> stopHold()
            }
            event.keyCode == KeyEvent.KEYCODE_BACK -> if (event.action == KeyEvent.ACTION_UP) cancelHold()
            event.keyCode in listOf(
                KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER, KeyEvent.KEYCODE_NUMPAD_ENTER,
                KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE, KeyEvent.KEYCODE_MEDIA_PLAY, KeyEvent.KEYCODE_HEADSETHOOK,
            ) -> if (first) landHold()
        }
    }

    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        if (tv && st.mode == Mode.Scrub) { scrubKeys(event); return true }
        val down = event.action == KeyEvent.ACTION_DOWN
        val first = down && event.repeatCount == 0
        // media keys do the same thing whatever is on screen
        when (event.keyCode) {
            KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE, KeyEvent.KEYCODE_HEADSETHOOK -> { if (first) togglePlay(); return true }
            KeyEvent.KEYCODE_MEDIA_PLAY -> { if (first && !player.isPlaying) togglePlay(); return true }
            KeyEvent.KEYCODE_MEDIA_PAUSE -> { if (first && player.isPlaying) togglePlay(); return true }
            KeyEvent.KEYCODE_MEDIA_FAST_FORWARD -> if (!tv) { if (down) scrub(scrubStep(event.repeatCount) * 3); return true }
            KeyEvent.KEYCODE_MEDIA_REWIND -> if (!tv) { if (down) scrub(-scrubStep(event.repeatCount)); return true }
            KeyEvent.KEYCODE_MEDIA_NEXT -> { if (first) next(); return true }
        }
        if (tv) {
            // a press skips, a hold scrubs: with nothing on screen, on the bar, or from the remote's own keys
            val dir = skipDir(event.keyCode)
            val isDpad = event.keyCode == KeyEvent.KEYCODE_DPAD_LEFT || event.keyCode == KeyEvent.KEYCODE_DPAD_RIGHT
            val free = st.error == null && st.sheet == null && (st.mode != Mode.Full || st.barFocused || !isDpad)
            if (dir != 0 && free) {
                if (st.dur <= 0) return true // nothing loaded yet to skip through
                if (first) tapSkip(dir)
                else if (down) startHold(dir, event.keyCode)
                return true
            }
        }
        if (st.error != null || st.sheet != null) return super.dispatchKeyEvent(event)
        if (st.mode == Mode.Full) {
            if (down) poke()
            if (first && event.keyCode == KeyEvent.KEYCODE_MENU) { if (player.playWhenReady) st.mode = Mode.Hidden; return true }
            return super.dispatchKeyEvent(event)
        }
        // Hidden or seeking: OK pauses (or takes the offer), Left/Right skip, Up/Down/Menu open the controls
        if (down) {
            when (event.keyCode) {
                KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER, KeyEvent.KEYCODE_NUMPAD_ENTER -> {
                    if (first) when {
                        st.scrub != null && st.mode == Mode.Seek -> commitScrub()
                        st.offer != null -> takeOffer()
                        else -> togglePlay()
                    }
                    return true
                }
                KeyEvent.KEYCODE_DPAD_LEFT -> { scrub(-scrubStep(event.repeatCount)); return true }
                KeyEvent.KEYCODE_DPAD_RIGHT -> { scrub(scrubStep(event.repeatCount)); return true }
                KeyEvent.KEYCODE_DPAD_UP, KeyEvent.KEYCODE_MENU -> { if (first) showFull(); return true }
                KeyEvent.KEYCODE_DPAD_DOWN -> { if (first) showFull(menu = true); return true }
            }
        }
        if (event.action == KeyEvent.ACTION_UP && event.keyCode in listOf(
                KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER, KeyEvent.KEYCODE_NUMPAD_ENTER,
                KeyEvent.KEYCODE_DPAD_LEFT, KeyEvent.KEYCODE_DPAD_RIGHT, KeyEvent.KEYCODE_DPAD_UP, KeyEvent.KEYCODE_DPAD_DOWN,
            )
        ) return true // the matching key-up must not reach the controls that just appeared
        return super.dispatchKeyEvent(event)
    }

    // ---------- refresh-rate matching (24p films at 24 Hz: no judder) ----------

    private fun matchRefreshRate(fps: Double): Boolean {
        if (fps <= 0) return false
        @Suppress("DEPRECATION")
        val display = (if (Build.VERSION.SDK_INT >= 30) display else windowManager.defaultDisplay) ?: return false
        val cur = display.mode
        val modes = display.supportedModes.filter {
            it.physicalWidth == cur.physicalWidth && it.physicalHeight == cur.physicalHeight
        }
        // exact rate, then double (48/50/60 Hz), then near-exact (24 for 23.976)
        val pick = listOf(1 to 0.01, 2 to 0.02, 1 to 0.06, 2 to 0.12).firstNotNullOfOrNull { (mult, tol) ->
            modes.firstOrNull { abs(it.refreshRate - fps * mult) < tol }
        } ?: return false
        if (pick.modeId == cur.modeId) return false
        window.attributes = window.attributes.also { it.preferredDisplayModeId = pick.modeId }
        return true
    }

    // ---------- views ----------

    private fun buildViews() {
        val root = FrameLayout(this).apply { setBackgroundColor(Color.BLACK) }
        view = PlayerView(this).apply {
            player = this@PlayerActivity.player
            useController = false
            setShowBuffering(PlayerView.SHOW_BUFFERING_NEVER)
            resizeMode = AspectRatioFrameLayout.RESIZE_MODE_FIT
            keepScreenOn = true
            isFocusable = false
            descendantFocusability = ViewGroup.FOCUS_BLOCK_DESCENDANTS
            subtitleView?.apply {
                // soft shadow rather than a heavy outline; a little higher than the edge
                setStyle(
                    CaptionStyleCompat(
                        Color.WHITE, Color.TRANSPARENT, Color.TRANSPARENT,
                        CaptionStyleCompat.EDGE_TYPE_DROP_SHADOW, 0xE6000000.toInt(), subtitleFace(),
                    ),
                )
                setBottomPaddingFraction(0.09f)
            }
        }
        st.subSize = SubSizes.indexOf(settings.getString("subsize", null)).takeIf { it >= 0 } ?: 1
        applySubSize()
        root.addView(view, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        shade = android.view.View(this).apply { setBackgroundColor(Color.BLACK) }
        root.addView(shade, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))

        val overlay = ComposeView(this).apply {
            setViewCompositionStrategy(ViewCompositionStrategy.DisposeOnViewTreeLifecycleDestroyed)
            setContent {
                CompositionLocalProvider(LocalTv provides tv) {
                    AppTheme {
                        DeviceOverlay(st, this@PlayerActivity, tv, onChrome = { lift ->
                            // moved as a whole: image subtitles (PGS) ignore padding
                            view.subtitleView?.let { sv -> sv.animate().translationY(-lift * sv.height).setDuration(if (Move.reduced) 0L else Move.Screen.toLong()).setInterpolator(ease).start() }
                        })
                    }
                }
            }
        }
        root.addView(overlay, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        curtain = android.view.View(this).apply { setBackgroundColor(Color.BLACK); alpha = 0f }
        root.addView(curtain, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        setContentView(root)
    }

    /** The app's own Source Sans for subtitles, at a sturdy 600, as a file: a variable font's weight needs one. */
    private fun subtitleFace(): Typeface = runCatching {
        val f = java.io.File(cacheDir, "subtitles-${BuildConfig.VERSION_CODE}.ttf")
        if (f.length() == 0L) resources.openRawResource(R.font.source_sans).use { i -> f.outputStream().use { i.copyTo(it) } }
        Typeface.Builder(f).setFontVariationSettings("'wght' 600").build()
    }.getOrNull() ?: Typeface.create("sans-serif-medium", Typeface.NORMAL)

    /** Leaving while on screen: fade to black first; the app underneath then fades up from black. */
    override fun finish() {
        if (leaving || !::curtain.isInitialized || !lifecycle.currentState.isAtLeast(androidx.lifecycle.Lifecycle.State.STARTED)) {
            leaving = true
            super.finish()
            @Suppress("DEPRECATION") overridePendingTransition(0, 0)
            return
        }
        leaving = true
        curtain.animate().alpha(1f).setDuration(if (Move.reduced) 100 else Move.Black.toLong()).setInterpolator(ease).withEndAction {
            super.finish()
            @Suppress("DEPRECATION") overridePendingTransition(0, 0)
        }.start()
    }

    override fun onPause() {
        super.onPause()
        report(false) // before the screen underneath wakes and asks: onStop only comes after that
    }

    override fun onStop() {
        super.onStop()
        // leaving (Home button, Back, screen off): save the spot and free the decoder
        report(false)
        release()
        finish()
    }

    override fun onDestroy() {
        super.onDestroy()
        release()
    }

    private fun release() {
        if (released || !::player.isInitialized) return
        released = true
        st.thumbs?.release()
        scope.cancel()
        ui.removeCallbacksAndMessages(null)
        player.removeListener(listener)
        player.release()
    }

    companion object {
        /** A queue too big for the Intent, waiting for the player to pick it up. */
        private var bigQueue: String? = null

        fun play(ctx: Context, queue: List<Item>, index: Int, fromStart: Boolean = false, logo: String? = null) {
            val start = queue.getOrNull(index) ?: return
            val pos = if (!fromStart && !start.watched) start.pos else 0L
            val json = JSONArray(queue.map { it.raw }).toString()
            // a show with hundreds of episodes is too big to hand over in the Intent (TransactionTooLargeException)
            val big = json.length > 200_000
            bigQueue = if (big) json else null
            ctx.startActivity(
                Intent(ctx, PlayerActivity::class.java)
                    .apply { if (!big) putExtra("queue", json) }
                    .putExtra("index", index)
                    .putExtra("pos", pos)
                    .putExtra("logo", logo),
            )
        }
    }
}
