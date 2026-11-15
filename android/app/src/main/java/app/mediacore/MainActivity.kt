package app.mediacore

import androidx.compose.ui.platform.AndroidUiDispatcher
import android.app.UiModeManager
import android.content.Context
import android.content.Intent
import android.content.res.Configuration
import android.net.Uri
import android.os.Bundle
import android.provider.Settings
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.core.content.FileProvider
import app.mediacore.core.Api
import app.mediacore.core.Home
import app.mediacore.core.Item
import androidx.compose.animation.core.Animatable
import app.mediacore.core.Offline
import app.mediacore.core.Speed
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.Request
import java.io.File

sealed interface Screen {
    data object Home : Screen
    data class Detail(val id: String) : Screen
    /** A title still downloading: not in the library yet, so all there is to show is its name and progress. */
    data class Pending(val title: String) : Screen
    data object Settings : Screen
}

/** App-wide state that must outlive a screen (so Back lands where you were). */
class Nav {
    val stack = mutableStateListOf<Screen>(Screen.Home)
    var refresh by mutableIntStateOf(0) // bumped when we come back from the player
    var update by mutableStateOf<Int?>(null) // newer app build on the server
    var home by mutableStateOf<Home?>(null)
    var homeJson: String? = null // what [home] was parsed from: an identical answer changes nothing on screen
    var offline by mutableStateOf(false) // mint isn't answering
    /** Home's focus, kept while a title page is open: "settings", "play", "info", or "row/id". */
    var homeFocus: String? = null
    val homeCols = mutableMapOf<String, Int>() // the card each row last had focused
    /** Title pages already fetched, shown at once when opened again, then refreshed. */
    val details = mutableMapOf<String, Pair<Item, Item?>>()
    /** Into the player: the screen goes black first, and comes back from black. */
    val black = Animatable(0f)
    var inPlayer = false
    /** For work that outlives a screen (the fade into the player); it has Compose's frame clock for animations. */
    val scope = kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.SupervisorJob() + AndroidUiDispatcher.Main)
    var titleFocus: String? = null
    var introDone by mutableStateOf(false) // the wordmark at start has made way for Home
    var heroKey: String? = null // the title Home's hero last showed // the title page's button to land on when it comes back

    fun open(s: Screen) { stack.add(s) }
    fun back() { if (stack.size > 1) stack.removeAt(stack.lastIndex) }
}

class MainActivity : ComponentActivity() {
    private val nav = Nav()
    private val scope = MainScope()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Api.init(this)
        Px.init(this)
        Move.read(this)
        loadAccent(this)
        Speed.start(this) // measures this connection in the background, for Original or Light
        Offline.init(this, overMetered = true)
        enableEdgeToEdge()
        val tv = isTv(this)
        setContent {
            CompositionLocalProvider(LocalTv provides tv) {
                AppTheme { Root(nav, tv, ::installUpdate) } // TV or phone app, per build flavor
            }
        }
        // the TV's Root paints the whole screen itself; the window's own fill under it was one more full-screen pass a frame
        if (tv) window.setBackgroundDrawable(null)
    }

    // while the app sits open, pick up what's being watched on other devices
    private val ui = android.os.Handler(android.os.Looper.getMainLooper())
    private val keepUp = object : Runnable {
        override fun run() {
            nav.refresh++
            ui.postDelayed(this, 60_000)
        }
    }

    override fun onPause() {
        super.onPause()
        ui.removeCallbacks(keepUp)
    }

    override fun onResume() {
        super.onResume()
        Move.read(this)
        if (nav.inPlayer) { // back from the player: the title page fades up from black
            nav.inPlayer = false
            // an animation needs Compose's frame clock: MainScope alone has none, and the app crashed here
            scope.launch(AndroidUiDispatcher.Main) { nav.black.animateTo(0f, Move.fade(Move.Screen)) }
        }
        keepUp.run()
        scope.launch { checkUpdate() }
    }

    override fun onDestroy() {
        super.onDestroy()
        scope.coroutineContext[kotlinx.coroutines.Job]?.cancel()
        nav.scope.coroutineContext[kotlinx.coroutines.Job]?.cancel()
    }

    private suspend fun checkUpdate() {
        if (BuildConfig.FLAVOR != "tv") return // the server only publishes the TV app for now
        val code = runCatching { Api.get("/api/v1/app").optInt("version_code") }.getOrNull() ?: return
        nav.update = if (code > BuildConfig.VERSION_CODE) code else null
    }

    /** Downloads the new APK from mint and hands it to the system installer. */
    private fun installUpdate() {
        if (!packageManager.canRequestPackageInstalls()) {
            Toast.makeText(this, "Allow Media Core to install updates, then press Update again", Toast.LENGTH_LONG).show()
            runCatching { startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:$packageName"))) }
            return
        }
        Toast.makeText(this, "Downloading update…", Toast.LENGTH_SHORT).show()
        scope.launch {
            val f = withContext(Dispatchers.IO) {
                runCatching {
                    val dir = File(cacheDir, "update").apply { mkdirs() }
                    val out = File(dir, "media-core.apk")
                    Api.http.newCall(Request.Builder().url("${Api.base}/api/v1/app.apk").build()).execute().use { r ->
                        if (!r.isSuccessful) error("HTTP ${r.code}")
                        out.outputStream().use { r.body!!.byteStream().copyTo(it) }
                    }
                    out
                }.getOrNull()
            }
            if (f == null) {
                Toast.makeText(this@MainActivity, "Update download failed", Toast.LENGTH_LONG).show()
                return@launch
            }
            val uri = FileProvider.getUriForFile(this@MainActivity, "$packageName.files", f)
            startActivity(
                Intent(Intent.ACTION_VIEW).setDataAndType(uri, "application/vnd.android.package-archive")
                    .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK),
            )
        }
    }

    companion object {
        fun isTv(ctx: Context): Boolean {
            val ui = ctx.getSystemService(Context.UI_MODE_SERVICE) as UiModeManager
            return ui.currentModeType == Configuration.UI_MODE_TYPE_TELEVISION ||
                ctx.packageManager.hasSystemFeature("android.software.leanback")
        }
    }
}
