package dev.reamde.substrate.tasks.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.ui.KitRowDivider
import dev.reamde.substrate.kit.ui.KitSection
import dev.reamde.substrate.kit.ui.KitSpacing
import dev.reamde.substrate.kit.ui.KitText
import dev.reamde.substrate.kit.ui.KitValueRow
import dev.reamde.substrate.kit.ui.brandAccent
import dev.reamde.substrate.kit.ui.style
import dev.reamde.substrate.tasks.bridge.QueuePresentation
import dev.reamde.substrate.tasks.bridge.SuggestionPresentation
import dev.reamde.substrate.tasks.bridge.TaskAction
import dev.reamde.substrate.tasks.bridge.TasksApp
import dev.reamde.substrate.tasks.bridge.TasksPresentation
import kotlinx.serialization.json.JsonPrimitive

/** A full reference as a line under what it names. */
internal val referenceStyle
    @Composable get() = KitText.identifier.style.copy(fontFamily = FontFamily.Monospace)

/**
 * The suggestions: each row with Dismiss and Accept, swiped right to accept and left to dismiss, and Select to pick several for one decision, which the bar at
 * the bottom then makes.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun SuggestionsScreen(queue: QueuePresentation, app: TasksPresentation, actions: TasksActions, onBack: (() -> Unit)?, selected: String? = null) {
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(queue.title) },
                navigationIcon = { onBack?.let { IconButton(onClick = it) { Icon(Icons.AutoMirrored.Rounded.ArrowBack, contentDescription = "Back") } } },
                actions = { if (queue.empty == null) TextButton(onClick = { actions.send(TasksApp.Command.Pick(!queue.picking)) }) { Text(queue.select) } },
            )
        },
        bottomBar = {
            val picked = queue.picked
            if (queue.picking && picked != null) {
                Surface(tonalElevation = 3.dp) {
                    Row(
                        horizontalArrangement = Arrangement.spacedBy(KitSpacing.content),
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier.fillMaxWidth().navigationBarsPadding().padding(KitSpacing.content),
                    ) {
                        Text(picked, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
                        queue.dismissPicked?.let { FilledTonalButton(onClick = { actions.send(TasksApp.Command.DismissPicked) }) { Text(it) } }
                        queue.acceptPicked?.let { Button(onClick = { actions.send(TasksApp.Command.AcceptPicked) }) { Text(it) } }
                    }
                }
            }
        },
    ) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            LazyColumn(Modifier.fillMaxSize()) {
                item {
                    Text(
                        queue.intro,
                        style = KitText.secondary.style,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(horizontal = KitSpacing.content, vertical = KitSpacing.small),
                    )
                }
                items(queue.rows.rows, key = { it.id }) { row ->
                    val words = app.list?.rowMenu
                    val swipe =
                        rememberSwipeToDismissBoxState(
                            confirmValueChange = { value ->
                                when (value) {
                                    SwipeToDismissBoxValue.StartToEnd -> actions.send(TasksApp.Command.Accept(row.id))
                                    SwipeToDismissBoxValue.EndToStart -> actions.send(TasksApp.Command.Dismiss(row.id))
                                    SwipeToDismissBoxValue.Settled -> Unit
                                }
                                false
                            }
                        )
                    SwipeToDismissBox(
                        swipe,
                        enableDismissFromStartToEnd = !row.pending && row.decisions.any { it.action == TaskAction.Accept },
                        enableDismissFromEndToStart = !row.pending && row.decisions.any { it.action == TaskAction.Dismiss },
                        backgroundContent = {},
                    ) {
                        TaskRowView(row, row.id == selected, words ?: menuWords, app, actions)
                    }
                }
            }
            queue.empty?.let { Placeholder(it.title, it.message) }
        }
    }
}

/** The row menu's words where the suggestions are shown with no list beside them. */
private val menuWords =
    dev.reamde.substrate.tasks.bridge.TaskMenuWords(
        complete = "Mark as Done",
        abandon = "Won't Do",
        reopen = "Reopen",
        edit = "Edit",
        priority = "Priority",
        priorities = emptyList(),
        move = "Move to List",
        delete = "Delete Task",
        swipeDelete = "Delete",
        accept = "Accept",
        dismiss = "Dismiss",
        undo = "Undo",
    )

/**
 * One suggestion: its collection and when it was written, its title, the fields the owner sets before accepting, its description under its label, every other
 * property under Details, and Dismiss and Accept docked at the bottom.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun SuggestionScreen(suggestion: SuggestionPresentation, actions: TasksActions, onBack: (() -> Unit)?) {
    Scaffold(
        topBar = {
            TopAppBar(
                title = {},
                navigationIcon = { onBack?.let { IconButton(onClick = it) { Icon(Icons.AutoMirrored.Rounded.ArrowBack, contentDescription = "Back") } } },
                actions = { suggestion.place?.let { Text(it, modifier = Modifier.padding(end = KitSpacing.content)) } },
            )
        },
        bottomBar = {
            Surface(tonalElevation = 3.dp) {
                Row(
                    horizontalArrangement = Arrangement.spacedBy(KitSpacing.content),
                    modifier = Modifier.fillMaxWidth().navigationBarsPadding().padding(KitSpacing.content),
                ) {
                    for (action in suggestion.actions) {
                        if (action.action == TaskAction.Accept) {
                            Button(onClick = { actions.send(command(action.action, suggestion.id)) }, modifier = Modifier.weight(1f)) { Text(action.title) }
                        } else {
                            FilledTonalButton(onClick = { actions.send(command(action.action, suggestion.id)) }, modifier = Modifier.weight(1f)) {
                                Text(action.title)
                            }
                        }
                    }
                }
            }
        },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState())) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(KitSpacing.small),
                modifier = Modifier.padding(horizontal = KitSpacing.screen),
            ) {
                Icon(TasksMarks.symbol("suggestion"), contentDescription = null, tint = MaterialTheme.brandAccent)
                Text(suggestion.header, style = KitText.secondary.style, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Text(suggestion.title, style = KitText.screenTitle.style, modifier = Modifier.padding(horizontal = KitSpacing.screen, vertical = KitSpacing.medium))
            if (suggestion.pending) {
                suggestion.pendingWords?.let {
                    Text(it, color = MaterialTheme.brandAccent, modifier = Modifier.padding(horizontal = KitSpacing.screen))
                }
            }
            suggestion.approval?.let { ApprovalLineRow(it, actions) }
            if (suggestion.fields.isNotEmpty()) {
                KitSection(null) {
                    suggestion.fields.forEachIndexed { index, field ->
                        if (index > 0) KitRowDivider()
                        FactRow(field) { id ->
                            field.property?.let { property ->
                                actions.send(TasksApp.Command.SetSuggestion(suggestion.id, property, if (id.isEmpty()) null else JsonPrimitive(id)))
                            }
                        }
                    }
                }
            }
            suggestion.description?.let { description -> KitSection(description.label) { KitValueRow(description.label, description.value, stacked = true) } }
            KitSection(suggestion.detailsTitle) {
                suggestion.details.forEachIndexed { index, fact ->
                    if (index > 0) KitRowDivider()
                    FactRow(fact)
                }
            }
        }
    }
}
