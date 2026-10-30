package app.mediadl

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
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.runtime.Composable
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.core.content.FileProvider
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.Request
import java.io.File

sealed interface Screen {
    data object Home : Screen
    data object Films : Screen
    data object Shows : Screen
    data object Search : Screen
    data class Detail(val id: String) : Screen
}

/** App-wide state that must outlive a screen (so Back lands where you were). */
class Nav {
    val stack = mutableStateListOf<Screen>(Screen.Home)
    var refresh by mutableIntStateOf(0) // bumped when we come back from the player
    var update by mutableStateOf<Int?>(null) // newer app build on the server
    // Home's scroll/focus memory
    val homeList = LazyListState()
    val rowStates = mutableMapOf<String, LazyListState>()
    var homeFocus: String? = null // "row/id" of the last focused card
    var home by mutableStateOf<Home?>(null)
    val gridStates = mutableMapOf<String, LazyGridState>()
    var query = ""
    val railFocus = FocusRequester()

    val tab: Tab get() = when (stack.lastOrNull { it !is Screen.Detail }) {
        Screen.Films -> Tab.Films
        Screen.Shows -> Tab.Shows
        Screen.Search -> Tab.Search
        else -> Tab.Home
    }

    /** Sidebar / bottom bar: Home is always underneath, so Back from a tab lands on Home. */
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
        enableEdgeToEdge()
        val tv = isTv(this)
        setContent {
            CompositionLocalProvider(LocalTv provides tv) {
                AppTheme { Root(nav, tv, ::installUpdate) } // TV or phone app, per build flavor
            }
        }
    }

    override fun onResume() {
        super.onResume()
        nav.refresh++
        scope.launch { checkUpdate() }
    }

    override fun onDestroy() {
        super.onDestroy()
        scope.coroutineContext[kotlinx.coroutines.Job]?.cancel()
    }

    private suspend fun checkUpdate() {
        if (BuildConfig.FLAVOR != "tv") return // the server only publishes the TV app for now
        val code = runCatching { Api.get("/api/v1/app").optInt("version_code") }.getOrNull() ?: return
        nav.update = if (code > BuildConfig.VERSION_CODE) code else null
    }

    /** Downloads the new APK from mint and hands it to the system installer. */
    private fun installUpdate() {
        if (!packageManager.canRequestPackageInstalls()) {
            Toast.makeText(this, "Allow Theater to install updates, then press Update again", Toast.LENGTH_LONG).show()
            runCatching { startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:$packageName"))) }
            return
        }
        Toast.makeText(this, "Downloading update…", Toast.LENGTH_SHORT).show()
        scope.launch {
            val f = withContext(Dispatchers.IO) {
                runCatching {
                    val dir = File(cacheDir, "update").apply { mkdirs() }
                    val out = File(dir, "theater.apk")
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
