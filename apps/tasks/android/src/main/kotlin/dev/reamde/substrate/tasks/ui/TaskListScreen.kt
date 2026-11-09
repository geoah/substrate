package dev.reamde.substrate.tasks.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowBack
import androidx.compose.material.icons.automirrored.rounded.KeyboardArrowRight
import androidx.compose.material.icons.automirrored.rounded.List
import androidx.compose.material.icons.automirrored.rounded.Notes
import androidx.compose.material.icons.rounded.AddCircle
import androidx.compose.material.icons.rounded.CalendarToday
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material.icons.rounded.EditNote
import androidx.compose.material.icons.rounded.MoreVert
import androidx.compose.material.icons.rounded.PushPin
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalIconButton
import androidx.compose.material3.FilterChip
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
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.AccessBanner
import dev.reamde.substrate.kit.bridge.KindStatus
import dev.reamde.substrate.kit.ui.KitSpacing
import dev.reamde.substrate.kit.ui.KitText
import dev.reamde.substrate.kit.ui.brandAccent
import dev.reamde.substrate.kit.ui.style
import dev.reamde.substrate.tasks.bridge.CountedRow
import dev.reamde.substrate.tasks.bridge.ListAction
import dev.reamde.substrate.tasks.bridge.ListPresentation
import dev.reamde.substrate.tasks.bridge.ListSelection
import dev.reamde.substrate.tasks.bridge.PinAction
import dev.reamde.substrate.tasks.bridge.TaskAction
import dev.reamde.substrate.tasks.bridge.TaskBand
import dev.reamde.substrate.tasks.bridge.TaskMenuWords
import dev.reamde.substrate.tasks.bridge.TaskPriority
import dev.reamde.substrate.tasks.bridge.TaskRow
import dev.reamde.substrate.tasks.bridge.TasksApp
import dev.reamde.substrate.tasks.bridge.TasksPresentation

/**
 * One list's tasks: the open ones in the order the presentation gives, then the completed ones where shown, the quick add field at the bottom and New Task as
 * the floating button where a task can be added here. Needs you draws its pinned groups' chips and the suggestions' row above the rows, its rows under their
 * bands' headings, and the undated as one row at the end. A row's mark completes or reopens it, a tap opens it and a long press offers the rest. Every change
 * is a command.
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
                    list.pin?.let { pin ->
                        IconButton(
                            onClick = {
                                actions.send(
                                    when (val chosen = pin.action) {
                                        is PinAction.Pin -> TasksApp.Command.Pin(chosen.value)
                                        is PinAction.Unpin -> TasksApp.Command.Unpin(chosen.value)
                                    }
                                )
                            }
                        ) {
                            Icon(
                                Icons.Rounded.PushPin,
                                contentDescription = pin.title,
                                tint = if (pin.action is PinAction.Unpin) MaterialTheme.brandAccent else MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                    if (list.selection != ListSelection.NeedsYou || list.actions.isNotEmpty()) {
                        Box {
                            IconButton(onClick = { options = true }) { Icon(Icons.Rounded.MoreVert, contentDescription = "List Options") }
                            DropdownMenu(expanded = options, onDismissRequest = { options = false }) {
                                if (list.selection != ListSelection.NeedsYou) {
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
            if (list.quickAdd != null) {
                FloatingActionButton(onClick = { actions.send(TasksApp.Command.NewTask) }) {
                    Icon(Icons.Rounded.EditNote, contentDescription = list.newTask)
                }
            }
        },
        bottomBar = { list.quickAdd?.let { prompt -> QuickAdd(prompt) { actions.send(TasksApp.Command.QuickAdd(it)) } } },
    ) { padding ->
        PullToRefreshBox(isRefreshing = false, onRefresh = { actions.send(TasksApp.Command.Refresh) }, modifier = Modifier.padding(padding)) {
            LazyColumn(Modifier.fillMaxSize()) {
                item { Notices(banner, app.notices, actions) }
                if (list.chips.isNotEmpty()) item { Chips(list, actions) }
                list.queue?.let { queue -> item(key = "queue") { CountedRowView(queue, "suggestion", actions) } }
                list.clear?.let { clear ->
                    item {
                        Text(
                            clear,
                            style = KitText.secondary.style,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(horizontal = KitSpacing.content, vertical = KitSpacing.small),
                        )
                    }
                }
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
                if (list.bands.isEmpty()) {
                    items(list.open.rows, key = { it.id }) { row -> TaskRowView(row, row.id == selected, list.rowMenu, app, actions) }
                } else {
                    for (band in list.bands) {
                        item(key = "band-" + band.band.name) {
                            Text(
                                "${band.title}  ${band.count}",
                                style = KitText.heading.style,
                                color = MaterialTheme.colorScheme.primary,
                                modifier = Modifier.padding(start = KitSpacing.content, top = KitSpacing.screen, bottom = KitSpacing.medium),
                            )
                        }
                        items(list.open.rows.filter { it.band == band.band }, key = { it.id }) { row ->
                            TaskRowView(row, row.id == selected, list.rowMenu, app, actions)
                        }
                    }
                }
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
                list.noDate?.let { noDate -> item(key = "nodate") { CountedRowView(noDate, "nodate", actions) } }
                item { Box(Modifier.padding(KitSpacing.block * 2)) }
            }
            val empty = list.empty
            if (empty != null && list.open.rows.isEmpty() && list.closed.rows.isEmpty() && list.queue == null) Placeholder(empty.title, empty.message)
        }
    }
}

/** Needs you's chips: All, then each list, one chosen. */
@Composable
private fun Chips(list: ListPresentation, actions: TasksActions) {
    Row(
        horizontalArrangement = Arrangement.spacedBy(KitSpacing.small),
        modifier = Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = KitSpacing.content, vertical = KitSpacing.small),
    ) {
        for (chip in list.chips) {
            FilterChip(
                selected = chip.selected,
                onClick = { actions.send(TasksApp.Command.Filter(chip.selection)) },
                label = { Text(chip.count?.let { "${chip.title}  $it" } ?: chip.title) },
            )
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
            leadingIcon = { Icon(Icons.Rounded.AddCircle, contentDescription = null, tint = MaterialTheme.brandAccent) },
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

/** A row that stands for a list of its own, the suggestions or the undated: its mark, its title and words, its count, and a disclosure; a tap shows it. */
@Composable
internal fun CountedRowView(row: CountedRow, symbol: String, actions: TasksActions) {
    ListItem(
        leadingContent = { Icon(TasksMarks.symbol(symbol), contentDescription = null, tint = MaterialTheme.brandAccent) },
        headlineContent = { Text(row.title, style = KitText.heading.style) },
        supportingContent = { Text(row.detail, style = KitText.secondary.style, color = MaterialTheme.colorScheme.onSurfaceVariant) },
        trailingContent = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (symbol == "nodate") Text("${row.count}", color = MaterialTheme.colorScheme.onSurfaceVariant)
                Icon(Icons.AutoMirrored.Rounded.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        },
        modifier = Modifier.combinedClickable(onClick = { actions.send(TasksApp.Command.Show(row.selection)) }),
    )
}

/**
 * A task as a row: its title, one meta line (when it is due, how urgent it is, the group it is in, its collection where several are on; a suggestion's
 * collection and when it was written), and one trailing control: the round mark that completes or reopens it, filled while a completion waits its undo window,
 * when a tap on it takes the mark back; Undo for another decision in its window; Dismiss beside Accept for a suggestion. On Needs you a late task's row also
 * offers where to move it and Won't Do. While suggestions are picked, a tap picks the row. Read aloud as the presentation words it.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
internal fun TaskRowView(row: TaskRow, chosen: Boolean, words: TaskMenuWords, app: TasksPresentation, actions: TasksActions) {
    var menu by remember { mutableStateOf(false) }
    val open = row.standing == KindStatus.Standing.open
    val marked = row.pending || row.standing == KindStatus.Standing.done
    val picked = row.picked
    Box(Modifier.alpha(if (row.band == TaskBand.later) 0.62f else 1f)) {
        ListItem(
            leadingContent =
                picked?.let { on -> { Icon(if (on) TasksMarks.mark(KindStatus.Standing.done) else TasksMarks.mark(KindStatus.Standing.open), null) } },
            trailingContent = { Trailing(row, actions, open, marked) },
            headlineContent = {
                Text(
                    row.title,
                    textDecoration = if (row.standing == KindStatus.Standing.dropped || (row.pending && !row.suggestion)) TextDecoration.LineThrough else null,
                    color = if ((open || row.suggestion) && !row.pending) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            },
            supportingContent =
                if (
                    row.written != null ||
                        row.pendingWords != null ||
                        row.due != null ||
                        row.priority != null ||
                        row.listName != null ||
                        row.collection != null ||
                        row.hasNotes ||
                        row.reschedule != null ||
                        row.decisions.any { !row.suggestion }
                ) {
                    {
                        Column(verticalArrangement = Arrangement.spacedBy(KitSpacing.small)) {
                            row.written?.let { Fact(TasksMarks.symbol("suggestion"), it, MaterialTheme.colorScheme.onSurfaceVariant) }
                            if (row.pending) row.pendingWords?.let { Text(it, style = KitText.footnote.style, color = MaterialTheme.brandAccent) }
                            if (row.due != null || row.priority != null || row.listName != null || row.collection != null || row.hasNotes) Facts(row)
                            if (!row.suggestion && (row.reschedule != null || row.decisions.isNotEmpty())) Decisions(row, actions)
                        }
                    }
                } else {
                    null
                },
            colors = ListItemDefaults.colors(containerColor = if (chosen) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surface),
            modifier =
                Modifier.combinedClickable(
                    onClick = { actions.send(if (picked != null) TasksApp.Command.Picked(row.id) else TasksApp.Command.Open(row.id)) },
                    onLongClick = { menu = true },
                    onClickLabel = row.spoken,
                ),
        )
        TaskMenu(menu, dismiss = { menu = false }, row, words, app, actions)
    }
}

/** A row's one trailing control. */
@Composable
private fun Trailing(row: TaskRow, actions: TasksActions, open: Boolean, marked: Boolean) {
    when {
        row.pending && row.pendingWords != "Done" ->
            androidx.compose.material3.TextButton(onClick = { actions.send(TasksApp.Command.Undo(row.id)) }) { Text("Undo") }
        row.suggestion ->
            if (row.picked == null) {
                Row(horizontalArrangement = Arrangement.spacedBy(KitSpacing.small), verticalAlignment = Alignment.CenterVertically) {
                    for (decision in row.decisions) {
                        if (decision.action == TaskAction.Dismiss) {
                            FilledTonalIconButton(onClick = { actions.send(TasksApp.Command.Dismiss(row.id)) }) {
                                Icon(Icons.Rounded.Close, contentDescription = decision.title)
                            }
                        } else {
                            Button(onClick = { actions.send(command(decision.action, row.id)) }) { Text(decision.title) }
                        }
                    }
                }
            }
        row.toggle != null ->
            IconButton(onClick = { actions.send(if (open && !row.pending) TasksApp.Command.Complete(row.id) else TasksApp.Command.Reopen(row.id)) }) {
                Icon(
                    if (row.pending) TasksMarks.mark(KindStatus.Standing.done) else TasksMarks.mark(row.standing),
                    contentDescription = row.toggle,
                    tint = if (marked) MaterialTheme.brandAccent else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
    }
}

/** A row's or a page's action as the command that does it. */
internal fun command(action: TaskAction, task: String): TasksApp.Command =
    when (action) {
        TaskAction.Complete -> TasksApp.Command.Complete(task)
        TaskAction.Reopen -> TasksApp.Command.Reopen(task)
        TaskAction.Abandon -> TasksApp.Command.Abandon(task)
        TaskAction.Delete -> TasksApp.Command.DeleteTask(task)
        TaskAction.Accept -> TasksApp.Command.Accept(task)
        TaskAction.Dismiss -> TasksApp.Command.Dismiss(task)
        TaskAction.Undo -> TasksApp.Command.Undo(task)
    }

/** What a late task's row decides from where it is: the days it can move to, and Won't Do. */
@Composable
private fun Decisions(row: TaskRow, actions: TasksActions) {
    var days by remember { mutableStateOf(false) }
    Row(horizontalArrangement = Arrangement.spacedBy(KitSpacing.small), verticalAlignment = Alignment.CenterVertically) {
        row.reschedule?.let { menu ->
            Box {
                androidx.compose.material3.Button(
                    onClick = { days = true },
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 12.dp),
                ) {
                    Text(menu.title)
                }
                DropdownMenu(expanded = days, onDismissRequest = { days = false }) {
                    for (option in menu.options) {
                        DropdownMenuItem(
                            text = { Text(option.title) },
                            onClick = {
                                days = false
                                actions.send(TasksApp.Command.Reschedule(row.id, option.action))
                            },
                        )
                    }
                }
            }
        }
        for (decision in row.decisions) {
            androidx.compose.material3.TextButton(onClick = { actions.send(command(decision.action, row.id)) }) {
                Text(decision.title)
            }
        }
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
        row.collection?.let { Fact(TasksMarks.symbol("records"), it, MaterialTheme.colorScheme.onSurfaceVariant) }
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
                for (destination in app.sidebar.sections.filter { it.reorders }.flatMap { it.rows + it.quiet }) {
                    val list = (destination.selection as? ListSelection.Group)?.value ?: continue
                    DropdownMenuItem(text = { Text(destination.title) }, onClick = { send(TasksApp.Command.Move(row.id, list.substringAfterLast('/'))) })
                }
            else -> {
                if (row.pending) {
                    DropdownMenuItem(text = { Text(words.undo) }, onClick = { send(TasksApp.Command.Undo(row.id)) })
                } else if (row.suggestion) {
                    DropdownMenuItem(text = { Text(words.accept) }, onClick = { send(TasksApp.Command.Accept(row.id)) })
                    DropdownMenuItem(text = { Text(words.dismiss) }, onClick = { send(TasksApp.Command.Dismiss(row.id)) })
                } else if (row.standing == KindStatus.Standing.open) {
                    DropdownMenuItem(text = { Text(words.complete) }, onClick = { send(TasksApp.Command.Complete(row.id)) })
                    DropdownMenuItem(text = { Text(words.abandon) }, onClick = { send(TasksApp.Command.Abandon(row.id)) })
                } else {
                    DropdownMenuItem(text = { Text(words.reopen) }, onClick = { send(TasksApp.Command.Reopen(row.id)) })
                }
                if (row.editable) {
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
}

/** The app's Show Completed row, as the list's menu turns it. */
internal fun showCompleted(on: Boolean): dev.reamde.substrate.kit.bridge.SettingsCommand =
    dev.reamde.substrate.kit.bridge.SettingsCommand.Toggle(dev.reamde.substrate.kit.bridge.SettingsRowID.App("showCompleted"), on)
