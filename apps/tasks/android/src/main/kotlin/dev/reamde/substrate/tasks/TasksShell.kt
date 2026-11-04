package dev.reamde.substrate.tasks

import android.content.Context
import dev.reamde.substrate.kit.Bridge
import dev.reamde.substrate.kit.BridgeHandle
import dev.reamde.substrate.kit.StartResult
import dev.reamde.substrate.kit.bridge.AccessAction
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.ConnectController
import dev.reamde.substrate.kit.bridge.LinkController
import dev.reamde.substrate.kit.bridge.LinkSnapshot
import dev.reamde.substrate.kit.bridge.RouteCommand
import dev.reamde.substrate.kit.bridge.SettingsCommand
import dev.reamde.substrate.kit.bridge.SettingsRowID
import dev.reamde.substrate.kit.bridge.SignInPresentation
import dev.reamde.substrate.kit.bridge.StatusAction
import dev.reamde.substrate.tasks.bridge.ProtocolFingerprint
import dev.reamde.substrate.tasks.bridge.TaskEditor
import dev.reamde.substrate.tasks.bridge.TasksApp
import dev.reamde.substrate.tasks.bridge.TasksFrame
import dev.reamde.substrate.tasks.bridge.TasksOpening
import dev.reamde.substrate.tasks.bridge.TasksShellCommand
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement

/**
 * The app over its two roots, as Apple's `AppShell` is over its link: `tasks` (the link with the app's root under it, one presentation tree) and, while the
 * owner signs in, `tasks.connect`. A finished sign-in is a signal on the connect screen's presentation: the link is opened again, over the repository's folder
 * the new session names. Every command is Swift's to apply; this only says which root it goes to.
 */
class TasksShell(context: Context, val start: StartResult) {
    private val app = context.applicationContext
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val prefs = app.getSharedPreferences("tasks", Context.MODE_PRIVATE)

    private var link: BridgeHandle? = null
    private var linkFollowing: Job? = null
    private var connect: BridgeHandle? = null
    private var connectFollowing: Job? = null
    private var signedInSeen = 0L

    /** Whether this process has seen the link signed in: a sign-out after it shows the sign-in again. */
    private var wasSignedIn = false

    private val _frame = MutableStateFlow<TasksFrame?>(null)
    private val _signIn = MutableStateFlow<SignInPresentation?>(null)
    private val _problem = MutableStateFlow<String?>(null)

    /** The link's newest tree, with the app's under it. */
    val frame: StateFlow<TasksFrame?> = _frame

    /** The connect screen, while one is open. */
    val signIn: StateFlow<SignInPresentation?> = _signIn

    /** A root that would not open: the bridge's own failure, said as it came. */
    val problem: StateFlow<String?> = _problem

    init {
        if (start is StartResult.Started) openLink()
    }

    private fun opening(address: String? = prefs.getString(ADDRESS, null)) =
        TasksOpening(
            fingerprint = ProtocolFingerprint.VALUE,
            debug = BuildConfig.DEBUG,
            clientID = BuildConfig.CLIENT_ID.ifEmpty { null },
            redirect = BuildConfig.REDIRECT.ifEmpty { null },
            version = "${BuildConfig.VERSION_NAME} (${BuildConfig.VERSION_CODE})",
            channel = BuildConfig.CHANNEL.ifEmpty { null },
            device = if (app.resources.configuration.smallestScreenWidthDp >= 600) "this tablet" else "this phone",
            address = address,
        )

    private fun settings(opening: TasksOpening): JsonElement = BridgeJson.encodeToJsonElement(TasksOpening.serializer(), opening)

    private fun openLink() {
        linkFollowing?.cancel()
        link?.close()
        val handle =
            Bridge.open("tasks", settings(opening())).getOrElse {
                _problem.value = it.message
                return
            }
        link = handle
        linkFollowing = scope.launch {
            handle.presentations(TasksFrame.serializer()).collect { frame ->
                _frame.value = frame
                when (frame.link.snapshot.phase) {
                    LinkSnapshot.Phase.signedIn -> wasSignedIn = true
                    LinkSnapshot.Phase.signedOut -> if (!frame.stored || wasSignedIn) openConnect()
                    LinkSnapshot.Phase.accessEnded -> Unit
                }
            }
        }
    }

    /** The connect screen, opened once; [address] is what its field starts with. */
    private fun openConnect(address: String? = null) {
        if (connect != null) return
        val handle =
            Bridge.open("tasks.connect", settings(opening(address ?: prefs.getString(ADDRESS, null)))).getOrElse {
                _problem.value = it.message
                return
            }
        connect = handle
        signedInSeen = 0
        connectFollowing = scope.launch {
            handle.presentations(SignInPresentation.serializer()).collect { presented ->
                _signIn.value = presented
                if (presented.signedIn > signedInSeen) {
                    signedInSeen = presented.signedIn
                    prefs.edit().putString(ADDRESS, presented.connect.field.text).apply()
                    closeConnect()
                    openLink()
                }
            }
        }
    }

    private fun closeConnect() {
        connectFollowing?.cancel()
        connectFollowing = null
        connect?.close()
        connect = null
        _signIn.value = null
    }

    // --- commands ------------------------------------------------------------

    private fun route(path: List<String>, command: JsonElement) {
        link?.send(TasksShellCommand.Serializer, TasksShellCommand.Route(RouteCommand(path, command)))
    }

    fun send(command: TasksApp.Command) = route(listOf(APP), BridgeJson.encodeToJsonElement(TasksApp.Command.Serializer, command))

    fun send(command: LinkController.Command) = route(emptyList(), BridgeJson.encodeToJsonElement(LinkController.Command.Serializer, command))

    fun send(editor: String, command: TaskEditor.Command) = route(listOf(APP, editor), BridgeJson.encodeToJsonElement(TaskEditor.Command.Serializer, command))

    /** A command for the connect screen, the root of its runtime: a route with no path. */
    fun send(command: ConnectController.Command) {
        connect?.send(RouteCommand.serializer(), RouteCommand(emptyList(), BridgeJson.encodeToJsonElement(ConnectController.Command.Serializer, command)))
    }

    /**
     * A Settings row's command: the app's and the link's go to Swift, which routes them as `SettingsRoute` does; what only the UI can run (the browser for
     * Change Access, the connect screen) runs here.
     */
    fun settings(command: SettingsCommand) {
        val row = (command as? SettingsCommand.Perform)?.row
        val action = ((row as? SettingsRowID.Status)?.value as? SettingsRowID.StatusRow.Action)?.value
        val repository = (row as? SettingsRowID.Repository)?.value
        when {
            action == StatusAction.Review || action == StatusAction.Renew || repository == SettingsRowID.RepositoryRow.ChangeAccess -> changeAccess()
            action == StatusAction.SignIn -> openConnect()
            else -> link?.send(TasksShellCommand.Serializer, TasksShellCommand.Settings(command))
        }
    }

    /** The Access banner's actions. */
    fun perform(action: AccessAction) {
        when (action) {
            AccessAction.Review,
            AccessAction.SignInAgain,
            AccessAction.MoveToAppAccess -> changeAccess()
            else -> send(LinkController.Command.Refresh(force = true))
        }
    }

    /** The whole request again on the same server: a change of access, or signing in after it ended. */
    fun changeAccess() {
        openConnect(_frame.value?.link?.snapshot?.server)
    }

    /** The connect screen's own way back, where a session is still there. */
    fun dismissSignIn() {
        if (_frame.value?.link?.snapshot?.phase != LinkSnapshot.Phase.signedOut) closeConnect()
    }

    /** The app came to the front: what the system may have changed is read again. */
    fun cameForward() {
        send(TasksApp.Command.Foreground)
        send(LinkController.Command.Refresh(force = false))
    }

    companion object {
        const val APP = "app"
        private const val ADDRESS = "signin.server"
    }
}
