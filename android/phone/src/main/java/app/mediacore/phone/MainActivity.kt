package app.mediacore.phone

import android.content.Intent
import android.graphics.Color
import android.net.Uri
import android.os.Bundle
import android.provider.Settings
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.core.content.FileProvider
import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.Request
import org.json.JSONObject

sealed interface Screen {
    data object Home : Screen
    data object Films : Screen
    data object Shows : Screen
    data object Search : Screen
    data class Detail(val id: String) : Screen
}

enum class Tab { Home, Films, Shows, Search }

/** App-wide state that must outlive a screen (so Back lands where you were, as you left it). */
class Nav {
    val stack = mutableStateListOf<Screen>(Screen.Home)
    var refresh by mutableIntStateOf(0) // bumped when we come back from the player
    var home by mutableStateOf<Home?>(null)
    var homeJson: String? = null // what [home] was parsed from: an identical answer changes nothing on screen
    val homeList = LazyListState()
    val grids = mutableMapOf<String, LazyGridState>() // each grid's scroll
    val lists = mutableMapOf<String, List<Item>>() // each grid's last answer, shown at once
    val titles = mutableMapOf<String, JSONObject>() // each title page's last answer
    var query = ""
    var update by mutableStateOf(false) // a newer app build is on the server

    val tab: Tab get() = when (stack.lastOrNull { it !is Screen.Detail }) {
        Screen.Films -> Tab.Films
        Screen.Shows -> Tab.Shows
        Screen.Search -> Tab.Search
        else -> Tab.Home
    }

    /** Bottom bar: Home is always underneath, so Back from a tab lands on Home. */
    fun go(t: Tab) {
        stack.clear()
        stack.add(Screen.Home)
        when (t) {
            Tab.Films -> stack.add(Screen.Films)
            Tab.Shows -> stack.add(Screen.Shows)
            Tab.Search -> stack.add(Screen.Search)
            Tab.Home -> {}
        }
    }

    fun open(s: Screen) { stack.add(s) }
    fun back() { if (stack.size > 1) stack.removeAt(stack.lastIndex) }
}

class MainActivity : ComponentActivity() {
    private val nav = Nav()
    private val scope = MainScope()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Api.init(this)
        Offline.init(this)
        // always light icons: the app is dark whatever the phone's own theme is
        val bars = SystemBarStyle.dark(Color.TRANSPARENT)
        enableEdgeToEdge(statusBarStyle = bars, navigationBarStyle = bars)
        setContent { AppTheme { Root(nav, ::installUpdate) } }
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
        keepUp.run()
        scope.launch { checkUpdate() }
    }

    override fun onDestroy() {
        super.onDestroy()
        scope.cancel()
    }

    private suspend fun checkUpdate() {
        val code = runCatching { Api.get("/api/v1/phone-app").optInt("version_code") }.getOrNull() ?: return
        nav.update = code > BuildConfig.VERSION_CODE
    }

    /** Downloads the new APK from mint and hands it to the system installer. */
    private fun installUpdate() {
        if (!packageManager.canRequestPackageInstalls()) {
            Toast.makeText(this, "Allow Media Core to install updates, then tap Update again", Toast.LENGTH_LONG).show()
            runCatching { startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:$packageName"))) }
            return
        }
        Toast.makeText(this, "Downloading update…", Toast.LENGTH_SHORT).show()
        scope.launch {
            val f = withContext(Dispatchers.IO) {
                runCatching {
                    val dir = File(cacheDir, "update").apply { mkdirs() }
                    val out = File(dir, "media-core-phone.apk")
                    Api.http.newCall(Request.Builder().url("${Api.base}/api/v1/phone-app.apk").build()).execute().use { r ->
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
}
