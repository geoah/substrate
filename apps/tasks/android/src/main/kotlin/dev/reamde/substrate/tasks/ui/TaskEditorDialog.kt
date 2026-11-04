package dev.reamde.substrate.tasks.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuAnchorType
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TimePicker
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.material3.rememberTimePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import dev.reamde.substrate.kit.ui.KitSpacing
import dev.reamde.substrate.kit.ui.KitText
import dev.reamde.substrate.kit.ui.style
import dev.reamde.substrate.tasks.bridge.DueKind
import dev.reamde.substrate.tasks.bridge.EditorChoice
import dev.reamde.substrate.tasks.bridge.EditorText
import dev.reamde.substrate.tasks.bridge.TaskEditor
import dev.reamde.substrate.tasks.bridge.TaskEditorPresentation
import dev.reamde.substrate.tasks.bridge.TaskPriority
import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.time.temporal.ChronoUnit

/** The editor over the app, the whole screen, as Material opens a form. */
@Composable
internal fun TaskEditorDialog(editor: TaskEditorPresentation, send: (TaskEditor.Command) -> Unit) {
    Dialog(onDismissRequest = { send(TaskEditor.Command.Cancel) }, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Surface(Modifier.fillMaxSize()) { TaskEditorForm(editor, send) }
    }
}

/** The edit form: each field under the label the read view gives it, every keystroke a command to the editor, Cancel and Save in the top bar. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun TaskEditorForm(editor: TaskEditorPresentation, send: (TaskEditor.Command) -> Unit, zone: ZoneId = ZoneId.systemDefault()) {
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(editor.title) },
                navigationIcon = {
                    IconButton(onClick = { send(TaskEditor.Command.Cancel) }) { Icon(Icons.Rounded.Close, contentDescription = editor.cancel) }
                },
                actions = { TextButton(onClick = { send(TaskEditor.Command.Save) }, enabled = editor.canSave) { Text(editor.save) } },
            )
        }
    ) { padding ->
        Column(
            Modifier.fillMaxSize().padding(padding).imePadding().verticalScroll(rememberScrollState()).padding(KitSpacing.content),
            verticalArrangement = Arrangement.spacedBy(KitSpacing.content),
        ) {
            TextField(editor.name, isError = editor.problem != null, supporting = editor.problem) { send(TaskEditor.Command.SetName(it)) }
            TextField(editor.notes, lines = true) { send(TaskEditor.Command.SetNotes(it)) }
            TextField(editor.link) { send(TaskEditor.Command.SetLink(it)) }
            Choice(editor.list) { send(TaskEditor.Command.SetList(it.ifEmpty { null })) }
            Due(editor, send, zone)
            Choice(editor.priority) { send(TaskEditor.Command.SetPriority(runCatching { TaskPriority.valueOf(it) }.getOrDefault(TaskPriority.none))) }
        }
    }
}

/** A text field under its label, the prompt inside it; what is typed is the field's and each change is sent. */
@Composable
private fun TextField(text: EditorText, lines: Boolean = false, isError: Boolean = false, supporting: String? = null, set: (String) -> Unit) {
    var value by remember { mutableStateOf(text.value) }
    OutlinedTextField(
        value = value,
        onValueChange = {
            value = it
            set(it)
        },
        label = { Text(text.label) },
        placeholder = { Text(text.prompt) },
        singleLine = !lines,
        minLines = if (lines) 3 else 1,
        isError = isError,
        supportingText = supporting?.let { { Text(it) } },
        modifier = Modifier.fillMaxWidth(),
    )
}

/** A choice under its label, as Material's exposed menu. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Choice(choice: EditorChoice, choose: (String) -> Unit) {
    var open by remember { mutableStateOf(false) }
    ExposedDropdownMenuBox(expanded = open, onExpandedChange = { open = it }) {
        OutlinedTextField(
            value = choice.options.firstOrNull { it.id == choice.selected }?.title.orEmpty(),
            onValueChange = {},
            readOnly = true,
            label = { Text(choice.label) },
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = open) },
            modifier = Modifier.fillMaxWidth().menuAnchor(ExposedDropdownMenuAnchorType.PrimaryNotEditable),
        )
        ExposedDropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            for (option in choice.options) {
                DropdownMenuItem(
                    text = { Text(option.title) },
                    onClick = {
                        open = false
                        choose(option.id)
                    },
                )
            }
        }
    }
}

/** When it is due: none, a day, or a day and time, as segments under the label, then the day (and the time) the pickers change. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Due(editor: TaskEditorPresentation, send: (TaskEditor.Command) -> Unit, zone: ZoneId) {
    val due = editor.due
    var picking by remember { mutableStateOf<String?>(null) }
    Column(verticalArrangement = Arrangement.spacedBy(KitSpacing.medium)) {
        Text(due.label, style = KitText.secondary.style, color = MaterialTheme.colorScheme.onSurfaceVariant)
        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
            due.kinds.forEachIndexed { index, kind ->
                SegmentedButton(
                    selected = kind.id == due.kind.name,
                    onClick = { send(TaskEditor.Command.SetDueKind(DueKind.valueOf(kind.id))) },
                    shape = SegmentedButtonDefaults.itemShape(index, due.kinds.size),
                ) {
                    Text(kind.title)
                }
            }
        }
        val instant = due.date?.let { runCatching { Instant.parse(it) }.getOrNull() }
        val label = due.dateLabel
        if (instant != null && label != null) {
            val local = instant.atZone(zone)
            Text(label, style = KitText.secondary.style, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Row(horizontalArrangement = Arrangement.spacedBy(KitSpacing.medium)) {
                OutlinedButton(onClick = { picking = DAY }) { Text(local.format(DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM))) }
                if (due.kind == DueKind.time) {
                    OutlinedButton(onClick = { picking = TIME }) { Text(local.format(DateTimeFormatter.ofLocalizedTime(FormatStyle.SHORT))) }
                }
            }
            when (picking) {
                DAY -> {
                    val state = rememberDatePickerState(local.toLocalDate().atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli())
                    DatePickerDialog(
                        onDismissRequest = { picking = null },
                        confirmButton = {
                            TextButton(
                                onClick = {
                                    picking = null
                                    state.selectedDateMillis?.let { millis ->
                                        val day = Instant.ofEpochMilli(millis).atZone(ZoneOffset.UTC).toLocalDate()
                                        send(TaskEditor.Command.SetDueDate(rfc3339(day, local.toLocalTime(), zone)))
                                    }
                                }
                            ) {
                                Text("OK")
                            }
                        },
                        dismissButton = { TextButton(onClick = { picking = null }) { Text("Cancel") } },
                    ) {
                        DatePicker(state)
                    }
                }
                TIME -> {
                    val state = rememberTimePickerState(local.hour, local.minute)
                    AlertDialog(
                        onDismissRequest = { picking = null },
                        text = { TimePicker(state) },
                        confirmButton = {
                            TextButton(
                                onClick = {
                                    picking = null
                                    send(TaskEditor.Command.SetDueDate(rfc3339(local.toLocalDate(), LocalTime.of(state.hour, state.minute), zone)))
                                }
                            ) {
                                Text("OK")
                            }
                        },
                        dismissButton = { TextButton(onClick = { picking = null }) { Text("Cancel") } },
                    )
                }
            }
        }
    }
}

/** A day and a time in the device's zone, as the instant Swift reads (RFC 3339, whole seconds). */
internal fun rfc3339(day: LocalDate, time: LocalTime, zone: ZoneId): String =
    day.atTime(time).atZone(zone).toInstant().truncatedTo(ChronoUnit.SECONDS).toString()

private const val DAY = "day"
private const val TIME = "time"
