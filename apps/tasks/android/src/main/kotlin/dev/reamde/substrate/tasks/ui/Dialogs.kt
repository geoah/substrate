package dev.reamde.substrate.tasks.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import dev.reamde.substrate.kit.bridge.ConfirmationPresentation
import dev.reamde.substrate.kit.bridge.ProblemPresentation
import dev.reamde.substrate.kit.ui.KitSpacing
import dev.reamde.substrate.kit.ui.ProblemBox
import dev.reamde.substrate.tasks.bridge.Confirmation
import dev.reamde.substrate.tasks.bridge.TasksApp

/** The question asked before something is given up: deleting a list and its tasks. */
@Composable
internal fun ConfirmationDialog(confirmation: ConfirmationPresentation<Confirmation>, actions: TasksActions) {
    AlertDialog(
        onDismissRequest = { actions.send(TasksApp.Command.DismissConfirmation) },
        title = { Text(confirmation.title) },
        text = { Text(confirmation.message) },
        confirmButton = {
            TextButton(onClick = { actions.send(TasksApp.Command.Confirm) }) {
                Text(confirmation.confirm, color = MaterialTheme.colorScheme.error)
            }
        },
        dismissButton = { TextButton(onClick = { actions.send(TasksApp.Command.DismissConfirmation) }) { Text(confirmation.cancel) } },
    )
}

/** A problem the app reports, in kit-ui's words box, until the owner dismisses it. */
@Composable
internal fun ProblemDialog(problem: ProblemPresentation, dismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = dismiss,
        text = { ProblemBox(problem) },
        confirmButton = { TextButton(onClick = dismiss) { Text("OK") } },
    )
}

/** A list being named: a new one, or one renamed. What is typed is the field's until it is sent. */
data class ListNaming(val id: String?, val name: String)

@Composable
internal fun ListNamingDialog(naming: ListNaming, dismiss: () -> Unit, actions: TasksActions) {
    var name by remember(naming) { mutableStateOf(naming.name) }
    val add = naming.id == null
    AlertDialog(
        onDismissRequest = dismiss,
        title = { Text(if (add) "New List" else "Rename List") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(KitSpacing.medium)) {
                Text("A task list's name, such as Groceries or Home.")
                OutlinedTextField(value = name, onValueChange = { name = it }, label = { Text("Name") }, singleLine = true)
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    val id = naming.id
                    actions.send(if (id == null) TasksApp.Command.CreateList(name) else TasksApp.Command.RenameList(id, name))
                    dismiss()
                }
            ) {
                Text(if (add) "Add" else "Rename")
            }
        },
        dismissButton = { TextButton(onClick = dismiss) { Text("Cancel") } },
    )
}
