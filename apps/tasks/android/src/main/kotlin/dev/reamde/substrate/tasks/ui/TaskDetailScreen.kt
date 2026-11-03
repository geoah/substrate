package dev.reamde.substrate.tasks.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowBack
import androidx.compose.material.icons.rounded.ArrowDropDown
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDecoration
import dev.reamde.substrate.kit.bridge.ActionButton
import dev.reamde.substrate.kit.ui.KitDestructiveRow
import dev.reamde.substrate.kit.ui.KitMark
import dev.reamde.substrate.kit.ui.KitRow
import dev.reamde.substrate.kit.ui.KitRowDivider
import dev.reamde.substrate.kit.ui.KitSection
import dev.reamde.substrate.kit.ui.KitSpacing
import dev.reamde.substrate.kit.ui.KitText
import dev.reamde.substrate.kit.ui.KitValueRow
import dev.reamde.substrate.kit.ui.kitRowPadding
import dev.reamde.substrate.kit.ui.style
import dev.reamde.substrate.tasks.bridge.DetailField
import dev.reamde.substrate.tasks.bridge.TaskAction
import dev.reamde.substrate.tasks.bridge.TaskDetailPresentation
import dev.reamde.substrate.tasks.bridge.TaskField
import dev.reamde.substrate.tasks.bridge.TaskPriority
import dev.reamde.substrate.tasks.bridge.TaskStatus
import dev.reamde.substrate.tasks.bridge.TasksApp

/**
 * The open task: its name, then each fact once under the label the editor
 * gives it, the list and the priority as the choices that change them where
 * they stand, then what can be done to it. Edit is the top bar's.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun TaskDetailScreen(detail: TaskDetailPresentation, actions: TasksActions, onBack: (() -> Unit)?) {
    Scaffold(topBar = {
        TopAppBar(
            title = {},
            navigationIcon = { onBack?.let { IconButton(onClick = it) { Icon(Icons.AutoMirrored.Rounded.ArrowBack, contentDescription = "Back") } } },
            actions = { TextButton(onClick = { actions.send(TasksApp.Command.Edit(detail.id)) }) { Text(detail.edit) } },
        )
    }) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState())) {
            Text(
                detail.title,
                style = KitText.screenTitle.style,
                textDecoration = if (detail.status == TaskStatus.abandoned) TextDecoration.LineThrough else null,
                modifier = Modifier.padding(horizontal = KitSpacing.screen, vertical = KitSpacing.medium),
            )
            KitSection(null) {
                detail.fields.forEachIndexed { index, field ->
                    if (index > 0) KitRowDivider()
                    FactRow(field) { id -> choose(detail.id, field.field, id, actions) }
                }
            }
            KitSection(null) {
                detail.actions.forEachIndexed { index, action ->
                    if (index > 0) KitRowDivider()
                    ActionRow(action) { perform(detail.id, action.action, actions) }
                }
            }
        }
    }
}

/** A fact under its label: a choice as the menu that changes it, a link to open, or a value in its tone. */
@Composable
private fun FactRow(field: DetailField, choose: (String) -> Unit) {
    val choice = field.choice
    val uri = LocalUriHandler.current
    when {
        choice != null -> {
            var open by remember { mutableStateOf(false) }
            Box {
                KitRow(field.label, onClick = { open = true }, trailing = {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(choice.options.firstOrNull { it.id == choice.selected }?.title ?: field.value, style = KitText.body.style,
                             color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Icon(Icons.Rounded.ArrowDropDown, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                })
                DropdownMenu(expanded = open, onDismissRequest = { open = false }, modifier = Modifier.align(Alignment.TopEnd)) {
                    for (option in choice.options) {
                        DropdownMenuItem(
                            text = { Text(option.title) },
                            trailingIcon = { if (option.id == choice.selected) Icon(KitMark.check.icon, contentDescription = null) },
                            onClick = {
                                open = false
                                choose(option.id)
                            },
                        )
                    }
                }
            }
        }
        field.url != null -> FactLine(field.label, field.value, MaterialTheme.colorScheme.primary,
                                      Modifier.clickable { runCatching { uri.openUri(field.url!!) } })
        field.tone != null -> FactLine(field.label, field.value, TasksMarks.color(field.tone!!))
        else -> KitValueRow(field.label, field.value, stacked = field.field == TaskField.notes)
    }
}

/** A label and a value that may run long: the label keeps its words whole, and the value wraps beside it. */
@Composable
private fun FactLine(label: String, value: String, color: Color, modifier: Modifier = Modifier) {
    Row(modifier.fillMaxWidth().kitRowPadding().padding(vertical = KitSpacing.small), verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(KitSpacing.content)) {
        Text(label, style = KitText.body.style, softWrap = false)
        Text(value, style = KitText.body.style, color = color, textAlign = TextAlign.End, modifier = Modifier.weight(1f))
    }
}

@Composable
private fun ActionRow(action: ActionButton<TaskAction>, perform: () -> Unit) {
    if (action.action == TaskAction.Delete) {
        KitDestructiveRow(action.title, onClick = perform)
    } else {
        KitRow(action.title, onClick = perform)
    }
}

private fun choose(task: String, field: TaskField, id: String, actions: TasksActions) {
    when (field) {
        TaskField.list -> actions.send(TasksApp.Command.Move(task, id.ifEmpty { null }))
        TaskField.priority -> actions.send(TasksApp.Command.SetPriority(task, runCatching { TaskPriority.valueOf(id) }.getOrDefault(TaskPriority.none)))
        else -> Unit
    }
}

private fun perform(task: String, action: TaskAction, actions: TasksActions) {
    actions.send(
        when (action) {
            TaskAction.Complete -> TasksApp.Command.Complete(task)
            TaskAction.Reopen -> TasksApp.Command.Reopen(task)
            TaskAction.Abandon -> TasksApp.Command.Abandon(task)
            TaskAction.Delete -> TasksApp.Command.DeleteTask(task)
        },
    )
}
