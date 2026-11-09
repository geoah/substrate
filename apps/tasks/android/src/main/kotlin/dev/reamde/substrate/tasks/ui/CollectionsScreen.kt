package dev.reamde.substrate.tasks.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowBack
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import dev.reamde.substrate.kit.ui.KitMark
import dev.reamde.substrate.kit.ui.KitRowDivider
import dev.reamde.substrate.kit.ui.KitSection
import dev.reamde.substrate.kit.ui.KitSpacing
import dev.reamde.substrate.kit.ui.KitText
import dev.reamde.substrate.kit.ui.KitValueRow
import dev.reamde.substrate.kit.ui.brandAccent
import dev.reamde.substrate.kit.ui.kitRowPadding
import dev.reamde.substrate.kit.ui.style
import dev.reamde.substrate.tasks.bridge.ApprovalLine
import dev.reamde.substrate.tasks.bridge.ApprovalPresentation
import dev.reamde.substrate.tasks.bridge.ChoicePresentation
import dev.reamde.substrate.tasks.bridge.CollectionPresentation
import dev.reamde.substrate.tasks.bridge.CollectionsPresentation
import dev.reamde.substrate.tasks.bridge.DetailField
import dev.reamde.substrate.tasks.bridge.SettingsPage
import dev.reamde.substrate.tasks.bridge.TaskField
import dev.reamde.substrate.tasks.bridge.TasksApp

/** Settings → Collections: every candidate with its label, its full reference, what it holds and a switch; a row opens its page; where new tasks go. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun CollectionsScreen(collections: CollectionsPresentation, actions: TasksActions, onBack: (() -> Unit)?) {
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(collections.title) },
                navigationIcon = { onBack?.let { IconButton(onClick = it) { Icon(Icons.AutoMirrored.Rounded.ArrowBack, contentDescription = "Back") } } },
            )
        }
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState())) {
            Text(
                collections.intro,
                style = KitText.secondary.style,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = KitSpacing.screen, vertical = KitSpacing.small),
            )
            KitSection(collections.showTitle, footer = collections.footer) {
                collections.rows.forEachIndexed { index, row ->
                    if (index > 0) KitRowDivider()
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(KitSpacing.content),
                        modifier =
                            Modifier.fillMaxWidth().clickable { actions.send(TasksApp.Command.ShowSettings(SettingsPage.Collection(row.id))) }.kitRowPadding(),
                    ) {
                        Column(Modifier.weight(1f).padding(vertical = KitSpacing.small)) {
                            Text(row.title, style = KitText.body.style)
                            Text(row.reference, style = referenceStyle, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            row.meta?.let { Text(it, style = KitText.secondary.style, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                        }
                        Switch(
                            checked = row.on,
                            onCheckedChange = { actions.send(TasksApp.Command.SetCollection(row.id, it)) },
                            modifier = Modifier.semantics { contentDescription = row.title },
                        )
                    }
                }
            }
            if (collections.newTasks.options.isNotEmpty()) {
                KitSection(collections.newTasksTitle) {
                    FactRow(
                        DetailField(
                            field = TaskField.list,
                            label = collections.newTasks.label,
                            value = collections.newTasks.options.firstOrNull { it.id == collections.newTasks.selected }?.title ?: "",
                            choice = collections.newTasks,
                        )
                    ) {
                        actions.send(TasksApp.Command.SetNewTasks(it))
                    }
                }
            }
        }
    }
}

/** One collection: what it is grouped by, how its states read, and the property each field is read from. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun CollectionScreen(collection: CollectionPresentation, actions: TasksActions, onBack: (() -> Unit)?) {
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(collection.title) },
                navigationIcon = { onBack?.let { IconButton(onClick = it) { Icon(Icons.AutoMirrored.Rounded.ArrowBack, contentDescription = "Back") } } },
            )
        }
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState())) {
            Text(
                collection.reference,
                style = referenceStyle,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = KitSpacing.screen),
            )
            collection.approval?.let { ApprovalLineRow(it, actions) }
            KitSection(collection.groupTitle) {
                collection.groups.forEachIndexed { index, choice ->
                    if (index > 0) KitRowDivider()
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier =
                            Modifier.fillMaxWidth()
                                .clickable { actions.send(TasksApp.Command.SetGrouping(collection.kind, choice.id.ifEmpty { null })) }
                                .kitRowPadding(),
                    ) {
                        Column(Modifier.weight(1f).padding(vertical = KitSpacing.small)) {
                            Text(choice.title, style = KitText.body.style)
                            Text(
                                choice.detail,
                                style = if (choice.id.isEmpty()) KitText.secondary.style else referenceStyle,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        if (choice.selected) Icon(KitMark.check.icon, contentDescription = "Chosen", tint = MaterialTheme.brandAccent)
                    }
                }
            }
            KitSection(collection.statesTitle) {
                collection.states.forEachIndexed { index, state ->
                    if (index > 0) KitRowDivider()
                    KitValueRow(state.label, state.value, verbatim = state.verbatim)
                }
            }
            KitSection(collection.fieldsTitle) {
                collection.fields.forEachIndexed { index, field ->
                    if (index > 0) KitRowDivider()
                    KitValueRow(field.label, field.value, verbatim = field.verbatim)
                }
            }
        }
    }
}

/** A page's approval line and its Approve (`TasksActions.request`). */
@Composable
internal fun ApprovalLineRow(line: ApprovalLine, actions: TasksActions) {
    KitSection(null) {
        Text(line.message, style = KitText.body.style, modifier = Modifier.kitRowPadding().padding(vertical = KitSpacing.small))
        KitRowDivider()
        TextButton(onClick = { actions.request(line) }, modifier = Modifier.kitRowPadding()) { Text(line.approve) }
    }
}

/** A decision with several values: each by its declared label, and Cancel. */
@Composable
internal fun ChoiceDialog(choice: ChoicePresentation, actions: TasksActions) {
    AlertDialog(
        onDismissRequest = { actions.send(TasksApp.Command.DismissChoice) },
        title = { Text(choice.title) },
        text = {
            Column {
                for (option in choice.options) {
                    TextButton(onClick = { actions.send(TasksApp.Command.Decide(choice.task, choice.decision, option.id)) }) { Text(option.title) }
                }
            }
        },
        confirmButton = {},
        dismissButton = { TextButton(onClick = { actions.send(TasksApp.Command.DismissChoice) }) { Text(choice.cancel) } },
    )
}

/** The approval the app asks for to change a collection (`TasksActions.approve`), or that it was not given. */
@Composable
internal fun ApprovalDialog(approval: ApprovalPresentation, actions: TasksActions) {
    if (approval.waiting) return
    AlertDialog(
        onDismissRequest = { actions.send(TasksApp.Command.DismissApproval) },
        text = { Text(approval.refused ?: approval.message) },
        confirmButton = {
            if (approval.refused == null) TextButton(onClick = { actions.approve(approval) }) { Text(approval.approve) }
        },
        dismissButton = { TextButton(onClick = { actions.send(TasksApp.Command.DismissApproval) }) { Text(approval.cancel) } },
    )
}
