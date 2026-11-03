package dev.reamde.substrate.tasks.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import dev.reamde.substrate.kit.StartResult
import dev.reamde.substrate.kit.bridge.AccessAction
import dev.reamde.substrate.kit.bridge.LinkSnapshot
import dev.reamde.substrate.kit.bridge.SettingsCommand
import dev.reamde.substrate.kit.ui.AccessEndedScreen
import dev.reamde.substrate.kit.ui.ConnectHeader
import dev.reamde.substrate.kit.ui.ConnectScreen
import dev.reamde.substrate.kit.ui.KitSpacing
import dev.reamde.substrate.tasks.R
import dev.reamde.substrate.tasks.TasksShell
import dev.reamde.substrate.tasks.bridge.TaskEditor
import dev.reamde.substrate.tasks.bridge.TasksApp

/**
 * The sign-in while there is no session (or the owner asked to change
 * access), the access-ended screen while the token is refused, else the
 * app, as Apple's `AppShellRoot` chooses.
 */
@Composable
fun TasksRootView(shell: TasksShell) {
    Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.surface) {
        val start = shell.start
        if (start !is StartResult.Started) {
            Failure("Substrate Tasks could not start: ${(start as? StartResult.Failed)?.failure?.message ?: start}")
            return@Surface
        }
        val frame by shell.frame.collectAsState()
        val signIn by shell.signIn.collectAsState()
        val problem by shell.problem.collectAsState()
        val actions = remember(shell) { actionsOf(shell) }
        val current = frame
        val connect = signIn
        when {
            connect != null -> {
                if (current?.link?.snapshot?.phase != LinkSnapshot.Phase.signedOut) BackHandler { shell.dismissSignIn() }
                ConnectScreen(connect.connect, shell::send, Modifier.safeDrawingPadding(), header = {
                    ConnectHeader(stringResource(R.string.app_name), tagline = "Your to-do list, kept in your substrate.")
                })
            }
            problem != null && current == null -> Failure(problem ?: "")
            current == null -> Waiting()
            current.link.ended != null -> AccessEndedScreen(current.link.ended!!, onSignInAgain = shell::changeAccess, Modifier.safeDrawingPadding())
            current.link.app != null -> TasksAppView(current.link.app!!, current.link.banner, current.settings, actions)
            else -> Waiting()
        }
    }
}

private fun actionsOf(shell: TasksShell): TasksActions = object : TasksActions {
    override fun send(command: TasksApp.Command) = shell.send(command)

    override fun send(editor: String, command: TaskEditor.Command) = shell.send(editor, command)

    override fun settings(command: SettingsCommand) = shell.settings(command)

    override fun perform(action: AccessAction) = shell.perform(action)
}

@Composable
private fun Waiting() {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
}

@Composable
private fun Failure(text: String) {
    Box(Modifier.fillMaxSize().safeDrawingPadding(), contentAlignment = Alignment.Center) {
        Text(text, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(KitSpacing.screen))
    }
}
