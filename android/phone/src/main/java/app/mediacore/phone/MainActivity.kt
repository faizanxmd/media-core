package app.mediacore.phone

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue

sealed interface Screen {
    data object Home : Screen
    data object Films : Screen
    data object Shows : Screen
    data object Search : Screen
    data class Detail(val id: String) : Screen
}

enum class Tab { Home, Search, Films, Shows }

/** App-wide state that must outlive a screen (so Back lands where you were). */
class Nav {
    val stack = mutableStateListOf<Screen>(Screen.Home)
    var refresh by mutableIntStateOf(0) // bumped when we come back from the player
    var home by mutableStateOf<Home?>(null)
    var homeJson: String? = null // what [home] was parsed from: an identical answer changes nothing on screen
    var query = ""

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

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Api.init(this)
        Offline.init(this)
        enableEdgeToEdge()
        setContent { AppTheme { Root(nav) } }
    }

    override fun onResume() {
        super.onResume()
        nav.refresh++
    }
}
