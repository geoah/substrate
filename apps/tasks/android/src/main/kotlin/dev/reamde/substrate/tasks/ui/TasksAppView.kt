package dev.reamde.substrate.tasks.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.VerticalDivider
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.AccessBanner
import dev.reamde.substrate.kit.bridge.SettingsPresentation
import dev.reamde.substrate.kit.ui.AppMenu
import dev.reamde.substrate.kit.ui.KitText
import dev.reamde.substrate.kit.ui.SettingsScreen
import dev.reamde.substrate.kit.ui.style
import dev.reamde.substrate.tasks.bridge.ListSelection
import dev.reamde.substrate.tasks.bridge.TasksApp
import dev.reamde.substrate.tasks.bridge.TasksPresentation

/** What a phone's or a narrow window's one pane shows, and a wide one's panes. */
internal enum class Panes {
    One,
    Two,
    Three,
}

/**
 * The App menu's sections (§5.6): a navigation bar on a phone, a rail or a drawer on a wider screen (kit-ui's `AppMenu`), the same presentation under each.
 * What a pane shows is the presentation's (the list shown, the task open), so a reminder's tap or a command from anywhere moves it, and going back is a
 * command. What is open over the sections (an editor, a question, a problem) is drawn here once.
 */
@Composable
fun TasksAppView(app: TasksPresentation, banner: AccessBanner?, settings: SettingsPresentation?, actions: TasksActions) {
    var naming by remember { mutableStateOf<ListNaming?>(null) }
    AppMenu(app.menu, select = { actions.send(TasksApp.Command.Select(it)) }, title = "Substrate Tasks") { section ->
        BoxWithConstraints(Modifier.fillMaxSize()) {
            val panes =
                when {
                    maxWidth >= 900.dp -> Panes.Three
                    maxWidth >= 600.dp -> Panes.Two
                    else -> Panes.One
                }
            Section(section.id, app, banner, settings, panes, actions, name = { naming = it })
        }
    }
    app.editors.entries
        .sortedBy { it.key }
        .firstOrNull()
        ?.let { (key, editor) ->
            TaskEditorDialog(editor, send = { actions.send(key, it) })
        }
    app.confirmation?.let { ConfirmationDialog(it, actions) }
    app.problem?.let { ProblemDialog(it) { actions.send(TasksApp.Command.DismissProblem) } }
    naming?.let { ListNamingDialog(it, dismiss = { naming = null }, actions) }
}

@Composable
private fun Section(
    id: String,
    app: TasksPresentation,
    banner: AccessBanner?,
    settings: SettingsPresentation?,
    panes: Panes,
    actions: TasksActions,
    name: (ListNaming) -> Unit,
) {
    val list = app.list
    val detail = app.detail
    val back = { command: TasksApp.Command -> actions.send(command) }
    when (id) {
        MENU_SETTINGS -> SettingsPane(settings, actions)
        MENU_NEEDS ->
            when {
                panes == Panes.One && detail != null -> {
                    BackHandler { back(TasksApp.Command.Open(null)) }
                    TaskDetailScreen(detail, actions, onBack = { back(TasksApp.Command.Open(null)) })
                }
                list != null && list.selection == ListSelection.NeedsYou && panes != Panes.One ->
                    Row(Modifier.fillMaxSize()) {
                        Box(Modifier.weight(1f)) { TaskListScreen(list, app, banner, actions, name, onBack = null, selected = detail?.id) }
                        VerticalDivider()
                        Box(Modifier.weight(1f)) { DetailPane(app, actions) }
                    }
                list != null && list.selection == ListSelection.NeedsYou -> TaskListScreen(list, app, banner, actions, name, onBack = null)
                else -> Box(Modifier.fillMaxSize())
            }
        else ->
            when (panes) {
                Panes.One ->
                    when {
                        detail != null -> {
                            BackHandler { back(TasksApp.Command.Open(null)) }
                            TaskDetailScreen(detail, actions, onBack = { back(TasksApp.Command.Open(null)) })
                        }
                        list != null -> {
                            BackHandler { back(TasksApp.Command.Show(null)) }
                            TaskListScreen(list, app, banner, actions, name, onBack = { back(TasksApp.Command.Show(null)) })
                        }
                        else -> SidebarScreen(app.sidebar, app, banner, actions, name, showsNeedsYou = false)
                    }
                Panes.Two,
                Panes.Three ->
                    Row(Modifier.fillMaxSize()) {
                        Box(Modifier.width(if (panes == Panes.Three) 300.dp else 280.dp).fillMaxHeight()) {
                            SidebarScreen(app.sidebar, app, banner, actions, name, showsNeedsYou = true, selected = app.sidebar.selected)
                        }
                        VerticalDivider()
                        Box(Modifier.weight(1f)) {
                            when {
                                panes == Panes.Two && detail != null -> {
                                    BackHandler { back(TasksApp.Command.Open(null)) }
                                    TaskDetailScreen(detail, actions, onBack = { back(TasksApp.Command.Open(null)) })
                                }
                                list != null -> TaskListScreen(list, app, null, actions, name, onBack = null, selected = detail?.id)
                                else -> Placeholder("Choose a list")
                            }
                        }
                        if (panes == Panes.Three) {
                            VerticalDivider()
                            Box(Modifier.weight(1f)) { DetailPane(app, actions) }
                        }
                    }
            }
    }
}

@Composable
private fun DetailPane(app: TasksPresentation, actions: TasksActions) {
    val detail = app.detail
    if (detail != null) TaskDetailScreen(detail, actions, onBack = null) else Placeholder("No task open", "Choose a task to see it here.")
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun SettingsPane(settings: SettingsPresentation?, actions: TasksActions) {
    Scaffold(topBar = { TopAppBar(title = { Text("Settings") }) }) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            settings?.let { SettingsScreen(it, actions::settings) }
        }
    }
}

@Composable
internal fun Placeholder(title: String, message: String? = null) {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        androidx.compose.foundation.layout.Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(title, style = KitText.heading.style)
            message?.let { Text(it, style = KitText.secondary.style, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
    }
}

/** The App menu's sections, by the ids TasksKit's `TasksMenu` gives them. */
internal const val MENU_NEEDS = "needs"
internal const val MENU_SETTINGS = "settings"
