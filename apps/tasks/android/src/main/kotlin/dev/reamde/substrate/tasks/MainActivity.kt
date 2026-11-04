package dev.reamde.substrate.tasks

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import dev.reamde.substrate.kit.ui.KitTheme
import dev.reamde.substrate.tasks.ui.TasksRootView

/**
 * The app's one activity. It is the exported launcher, so it reads nothing from the intent it is started with: a reminder's tap lands on kit-device's
 * `NotificationOpened`, which no other app may start, and that brings this one forward. Coming to the front reads again what the system may have changed.
 */
class MainActivity : ComponentActivity() {
    private val shell
        get() = (application as TasksApplication).shell

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            KitTheme { TasksRootView(shell) }
        }
    }

    override fun onResume() {
        super.onResume()
        shell.cameForward()
    }
}
