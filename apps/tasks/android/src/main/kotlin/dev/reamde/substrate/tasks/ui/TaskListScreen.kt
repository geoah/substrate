package dev.reamde.substrate.tasks.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowBack
import androidx.compose.material.icons.automirrored.rounded.List
import androidx.compose.material.icons.automirrored.rounded.Notes
import androidx.compose.material.icons.rounded.AddCircle
import androidx.compose.material.icons.rounded.CalendarToday
import androidx.compose.material.icons.rounded.EditNote
import androidx.compose.material.icons.rounded.MoreVert
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.AccessBanner
import dev.reamde.substrate.kit.ui.KitSpacing
import dev.reamde.substrate.kit.ui.KitText
import dev.reamde.substrate.kit.ui.style
import dev.reamde.substrate.tasks.bridge.ListAction
import dev.reamde.substrate.tasks.bridge.ListPresentation
import dev.reamde.substrate.tasks.bridge.ListSelection
import dev.reamde.substrate.tasks.bridge.TaskMenuWords
import dev.reamde.substrate.tasks.bridge.TaskPriority
import dev.reamde.substrate.tasks.bridge.TaskRow
import dev.reamde.substrate.tasks.bridge.TaskStatus
import dev.reamde.substrate.tasks.bridge.TasksApp
import dev.reamde.substrate.tasks.bridge.TasksPresentation

/**
 * One list's tasks: the open ones in the order the presentation gives, then the completed ones where shown, the quick add field at the bottom and New Task as
 * the floating button. A row's mark completes or reopens it, a tap opens it and a long press offers the rest. Every change is a command.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun TaskListScreen(
    list: ListPresentation,
    app: TasksPresentation,
    banner: AccessBanner?,
    actions: TasksActions,
    name: (ListNaming) -> Unit,
    onBack: (() -> Unit)?,
    selected: String? = null,
) {
    var options by remember { mutableStateOf(false) }
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(list.title) },
                navigationIcon = {
                    onBack?.let { IconButton(onClick = it) { Icon(Icons.AutoMirrored.Rounded.ArrowBack, contentDescription = "Back") } }
                },
                actions = {
                    if (list.selection != ListSelection.Today || list.actions.isNotEmpty()) {
                        Box {
                            IconButton(onClick = { options = true }) { Icon(Icons.Rounded.MoreVert, contentDescription = "List Options") }
                            DropdownMenu(expanded = options, onDismissRequest = { options = false }) {
                                if (list.selection != ListSelection.Today) {
                                    DropdownMenuItem(
                                        text = { Text(list.toggleClosed) },
                                        onClick = {
                                            options = false
                                            actions.send(TasksApp.Command.Settings(showCompleted(!list.showsClosed)))
                                        },
                                    )
                                }
                                for (action in list.actions) {
                                    val chosen = action.action
                                    DropdownMenuItem(
                                        text = {
                                            Text(
                                                action.title,
                                                color =
                                                    if (chosen is ListAction.Delete) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface,
                                            )
                                        },
                                        onClick = {
                                            options = false
                                            when (chosen) {
                                                is ListAction.Rename -> name(ListNaming(chosen.value, list.title))
                                                is ListAction.Delete -> actions.send(TasksApp.Command.DeleteList(chosen.value))
                                            }
                                        },
                                    )
                                }
                            }
                        }
                    }
                },
            )
        },
        floatingActionButton = {
            FloatingActionButton(onClick = { actions.send(TasksApp.Command.NewTask) }) {
                Icon(Icons.Rounded.EditNote, contentDescription = list.newTask)
            }
        },
        bottomBar = { QuickAdd(list.quickAdd) { actions.send(TasksApp.Command.QuickAdd(it)) } },
    ) { padding ->
        PullToRefreshBox(isRefreshing = false, onRefresh = { actions.send(TasksApp.Command.Refresh) }, modifier = Modifier.padding(padding)) {
            LazyColumn(Modifier.fillMaxSize()) {
                item { Notices(banner, app.notices, actions) }
                list.orderNote?.let { note ->
                    item {
                        Text(
                            note,
                            style = KitText.footnote.style,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(horizontal = KitSpacing.content, vertical = KitSpacing.small),
                        )
                    }
                }
                items(list.open.rows, key = { it.id }) { row -> TaskRowView(row, row.id == selected, list.rowMenu, app, actions) }
                if (list.showsClosed && list.closed.rows.isNotEmpty()) {
                    item {
                        Text(
                            list.closedTitle,
                            style = KitText.heading.style,
                            color = MaterialTheme.colorScheme.primary,
                            modifier = Modifier.padding(start = KitSpacing.content, top = KitSpacing.screen, bottom = KitSpacing.medium),
                        )
                    }
                    items(list.closed.rows, key = { it.id }) { row -> TaskRowView(row, row.id == selected, list.rowMenu, app, actions) }
                }
                item { Box(Modifier.padding(KitSpacing.block * 2)) }
            }
            val empty = list.empty
            if (empty != null && list.open.rows.isEmpty() && list.closed.rows.isEmpty()) Placeholder(empty.title, empty.message)
        }
    }
}

/** The quick add field: what is typed is added on Done, and the field stays for the next. */
@Composable
private fun QuickAdd(prompt: String, add: (String) -> Unit) {
    var draft by remember { mutableStateOf("") }
    Surface(tonalElevation = 3.dp) {
        OutlinedTextField(
            value = draft,
            onValueChange = { draft = it },
            placeholder = { Text(prompt) },
            leadingIcon = { Icon(Icons.Rounded.AddCircle, contentDescription = null, tint = MaterialTheme.colorScheme.primary) },
            singleLine = true,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
            keyboardActions =
                KeyboardActions(
                    onDone = {
                        if (draft.isNotBlank()) add(draft)
                        draft = ""
                    }
                ),
            modifier = Modifier.fillMaxWidth().navigationBarsPadding().imePadding().padding(horizontal = KitSpacing.content, vertical = KitSpacing.medium),
        )
    }
}

/**
 * A task as a row: the round mark that completes or reopens it, its name, and under it, each under its own mark, when it is due, how urgent it is, the list it
 * is in and whether it has notes. Read aloud as the presentation words it.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
internal fun TaskRowView(row: TaskRow, chosen: Boolean, words: TaskMenuWords, app: TasksPresentation, actions: TasksActions) {
    var menu by remember { mutableStateOf(false) }
    val open = row.status == TaskStatus.open
    Box {
        ListItem(
            leadingContent = {
                IconButton(onClick = { actions.send(if (open) TasksApp.Command.Complete(row.id) else TasksApp.Command.Reopen(row.id)) }) {
                    Icon(
                        TasksMarks.mark(row.status),
                        contentDescription = row.toggle,
                        tint = if (open) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.primary,
                    )
                }
            },
            headlineContent = {
                Text(
                    row.title,
                    textDecoration = if (row.status == TaskStatus.abandoned) TextDecoration.LineThrough else null,
                    color = if (open) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            },
            supportingContent =
                if (row.due != null || row.priority != null || row.listName != null || row.hasNotes) {
                    { Facts(row) }
                } else {
                    null
                },
            colors = ListItemDefaults.colors(containerColor = if (chosen) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surface),
            modifier =
                Modifier.combinedClickable(
                    onClick = { actions.send(TasksApp.Command.Open(row.id)) },
                    onLongClick = { menu = true },
                    onClickLabel = row.spoken,
                ),
        )
        TaskMenu(menu, dismiss = { menu = false }, row, words, app, actions)
    }
}

@Composable
private fun Facts(row: TaskRow) {
    Row(
        horizontalArrangement = Arrangement.spacedBy(KitSpacing.content),
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier.clearAndSetSemantics { contentDescription = row.spoken },
    ) {
        row.due?.let { Fact(Icons.Rounded.CalendarToday, it.text, TasksMarks.color(it.tone)) }
        row.priority?.let { Fact(TasksMarks.icon(it.priority), it.text, TasksMarks.color(it.priority)) }
        row.listName?.let { Fact(Icons.AutoMirrored.Rounded.List, it, MaterialTheme.colorScheme.onSurfaceVariant) }
        if (row.hasNotes)
            Icon(
                Icons.AutoMirrored.Rounded.Notes,
                contentDescription = "Has notes",
                tint = MaterialTheme.colorScheme.outline,
                modifier = Modifier.size(14.dp),
            )
    }
}

@Composable
private fun Fact(icon: androidx.compose.ui.graphics.vector.ImageVector, text: String, color: androidx.compose.ui.graphics.Color) {
    Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(14.dp))
        Text(text, style = KitText.footnote.style, color = color)
    }
}

/** What a task offers from its row: its state, its priority, its list, edit and delete, in the presentation's words. */
@Composable
private fun TaskMenu(expanded: Boolean, dismiss: () -> Unit, row: TaskRow, words: TaskMenuWords, app: TasksPresentation, actions: TasksActions) {
    var submenu by remember { mutableStateOf<String?>(null) }
    val close = {
        submenu = null
        dismiss()
    }
    val send = { command: TasksApp.Command ->
        close()
        actions.send(command)
    }
    DropdownMenu(expanded = expanded, onDismissRequest = close) {
        when (submenu) {
            words.priority ->
                for (level in words.priorities) {
                    DropdownMenuItem(
                        text = { Text(level.title) },
                        trailingIcon = {
                            if ((row.priority?.priority?.name ?: TaskPriority.none.name) == level.id) Icon(dev.reamde.substrate.kit.ui.KitMark.check.icon, null)
                        },
                        onClick = { send(TasksApp.Command.SetPriority(row.id, TaskPriority.valueOf(level.id))) },
                    )
                }
            words.move ->
                for (destination in app.sidebar.fixed.filter { it.selection != ListSelection.Today } + app.sidebar.lists.rows) {
                    DropdownMenuItem(
                        text = { Text(destination.title) },
                        onClick = {
                            send(TasksApp.Command.Move(row.id, (destination.selection as? ListSelection.List)?.value))
                        },
                    )
                }
            else -> {
                if (row.status == TaskStatus.open) {
                    DropdownMenuItem(text = { Text(words.complete) }, onClick = { send(TasksApp.Command.Complete(row.id)) })
                    DropdownMenuItem(text = { Text(words.abandon) }, onClick = { send(TasksApp.Command.Abandon(row.id)) })
                } else {
                    DropdownMenuItem(text = { Text(words.reopen) }, onClick = { send(TasksApp.Command.Reopen(row.id)) })
                }
                DropdownMenuItem(text = { Text(words.edit) }, onClick = { send(TasksApp.Command.Edit(row.id)) })
                DropdownMenuItem(text = { Text(words.priority) }, onClick = { submenu = words.priority })
                DropdownMenuItem(text = { Text(words.move) }, onClick = { submenu = words.move })
                HorizontalDivider()
                DropdownMenuItem(
                    text = { Text(words.delete, color = MaterialTheme.colorScheme.error) },
                    onClick = { send(TasksApp.Command.DeleteTask(row.id)) },
                )
            }
        }
    }
}

/** The app's Show Completed row, as the list's menu turns it. */
internal fun showCompleted(on: Boolean): dev.reamde.substrate.kit.bridge.SettingsCommand =
    dev.reamde.substrate.kit.bridge.SettingsCommand.Toggle(dev.reamde.substrate.kit.bridge.SettingsRowID.App("showCompleted"), on)
