package dev.reamde.substrate.tasks

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import dev.reamde.substrate.kit.device.Notifications
import dev.reamde.substrate.kit.ui.KitTheme
import dev.reamde.substrate.tasks.ui.TasksRootView

/**
 * The app's one activity. A reminder's tap reaches it as the intent it is
 * started or brought forward with, which kit-device turns into the response
 * Swift makes the app's command from; coming to the front reads again what
 * the system may have changed.
 */
class MainActivity : ComponentActivity() {
    private val shell get() = (application as TasksApplication).shell

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        if (savedInstanceState == null) Notifications.get(this).handle(intent)
        setContent {
            KitTheme { TasksRootView(shell) }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        Notifications.get(this).handle(intent)
    }

    override fun onResume() {
        super.onResume()
        shell.cameForward()
    }
}
