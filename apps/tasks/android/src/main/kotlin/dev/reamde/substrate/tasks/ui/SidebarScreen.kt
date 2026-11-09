package dev.reamde.substrate.tasks.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Add
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import dev.reamde.substrate.kit.bridge.AccessBanner
import dev.reamde.substrate.kit.bridge.ActionButton
import dev.reamde.substrate.kit.ui.KitSpacing
import dev.reamde.substrate.kit.ui.KitText
import dev.reamde.substrate.kit.ui.style
import dev.reamde.substrate.tasks.bridge.ListAction
import dev.reamde.substrate.tasks.bridge.ListSelection
import dev.reamde.substrate.tasks.bridge.SidebarPresentation
import dev.reamde.substrate.tasks.bridge.SidebarRow
import dev.reamde.substrate.tasks.bridge.TasksApp
import dev.reamde.substrate.tasks.bridge.TasksPresentation

/**
 * The lists: the pinned groups, then a section per group kind, headed by its label with its full reference under it, listing the groups with open tasks, the
 * tasks in no group, those whose group is gone, and how many more hold nothing open; then the collections that group by nothing. Wide, Needs you, Suggestions
 * and No date come first, and each kind shows its busiest groups with "All N…". Each row says how many of its tasks are open; a long press on one of the app's
 * own lists offers its rename and delete.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun SidebarScreen(
    sidebar: SidebarPresentation,
    app: TasksPresentation,
    banner: AccessBanner?,
    actions: TasksActions,
    name: (ListNaming) -> Unit,
    showsNeedsYou: Boolean,
    selected: ListSelection? = null,
) {
    var opened by remember { mutableStateOf(setOf<String>()) }
    val show = { row: SidebarRow ->
        if (showsNeedsYou) actions.send(TasksApp.Command.Select(if (row.selection == ListSelection.NeedsYou) MENU_NEEDS else "lists"))
        actions.send(TasksApp.Command.Show(row.selection))
    }
    val rowActions = { row: SidebarRow ->
        { action: ActionButton<ListAction> ->
            when (val chosen = action.action) {
                is ListAction.Rename -> name(ListNaming(chosen.value, row.title))
                is ListAction.Delete -> actions.send(TasksApp.Command.DeleteList(chosen.value))
            }
        }
    }
    Scaffold(
        topBar = { TopAppBar(title = { Text(sidebar.title) }) },
        floatingActionButton = {
            ExtendedFloatingActionButton(
                onClick = { name(ListNaming(null, "")) },
                icon = { Icon(Icons.Rounded.Add, contentDescription = null) },
                text = { Text(sidebar.newList) },
            )
        },
    ) { padding ->
        PullToRefreshBox(isRefreshing = false, onRefresh = { actions.send(TasksApp.Command.Refresh) }, modifier = Modifier.padding(padding)) {
            LazyColumn(Modifier.fillMaxSize()) {
                item { Notices(banner, app.notices, actions) }
                if (showsNeedsYou) {
                    items(sidebar.fixed, key = { it.id }) { row -> SidebarRowView(row, row.selection == selected, onOpen = { show(row) }, onAction = {}) }
                }
                if (sidebar.pinned.isNotEmpty()) {
                    item { Heading(sidebar.pinnedTitle, null) }
                    items(sidebar.pinned, key = { "pin-" + it.id }) { row ->
                        SidebarRowView(row, row.selection == selected, onOpen = { show(row) }, onAction = {})
                    }
                }
                for (section in sidebar.sections) {
                    item(key = "section-" + section.id) { Heading(section.title, section.reference) }
                    val open = section.id in opened
                    val rows =
                        when {
                            open -> section.rows + section.quiet
                            showsNeedsYou -> section.rows.take(section.busiest.toInt())
                            else -> section.rows
                        }
                    items(rows, key = { section.id + "-" + it.id }) { row ->
                        SidebarRowView(row, row.selection == selected, onOpen = { show(row) }, onAction = rowActions(row))
                    }
                    val more = if (showsNeedsYou) section.all else section.more
                    if (!open && more != null) {
                        item(key = "more-" + section.id) {
                            ListItem(
                                headlineContent = { Text(more, color = MaterialTheme.colorScheme.onSurfaceVariant) },
                                modifier = Modifier.combinedClickable(onClick = { opened = opened + section.id }),
                            )
                        }
                    }
                    if (section.reorders) {
                        sidebar.noLists?.let { none ->
                            item {
                                Text(
                                    none,
                                    style = KitText.footnote.style,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    modifier = Modifier.padding(horizontal = KitSpacing.content),
                                )
                            }
                        }
                    }
                }
                if (sidebar.ungrouped.isNotEmpty()) {
                    item { Heading(sidebar.ungroupedTitle, null) }
                    items(sidebar.ungrouped, key = { it.id }) { row -> SidebarRowView(row, row.selection == selected, onOpen = { show(row) }, onAction = {}) }
                }
                // Room for the button floating over the last row.
                item { Box(Modifier.padding(KitSpacing.block * 2)) }
            }
        }
    }
}

/** A section's heading: a group kind's plural label, and its full reference on a line of its own. */
@Composable
private fun Heading(title: String, reference: String?) {
    androidx.compose.foundation.layout.Column(Modifier.padding(start = KitSpacing.content, top = KitSpacing.screen, bottom = KitSpacing.medium)) {
        Text(title, style = KitText.heading.style, color = MaterialTheme.colorScheme.primary)
        reference?.let { Text(it, style = referenceStyle, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

/** A list as a row: its mark, its name, and how many of its tasks are open. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun SidebarRowView(row: SidebarRow, chosen: Boolean, onOpen: () -> Unit, onAction: (ActionButton<ListAction>) -> Unit) {
    var menu by remember { mutableStateOf(false) }
    Box {
        ListItem(
            headlineContent = { Text(row.title) },
            supportingContent =
                row.detail
                    ?.takeIf { it.isNotEmpty() }
                    ?.let { detail ->
                        { Text(detail, style = if (detail.count { it == '/' } == 2) referenceStyle else KitText.secondary.style) }
                    },
            leadingContent = { Icon(TasksMarks.symbol(row.symbol), contentDescription = null) },
            trailingContent = { Text("${row.open}", style = KitText.body.style, color = MaterialTheme.colorScheme.onSurfaceVariant) },
            colors = ListItemDefaults.colors(containerColor = if (chosen) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surface),
            modifier =
                Modifier.combinedClickable(onClick = onOpen, onLongClick = { if (row.actions.isNotEmpty()) menu = true }).semantics {
                    contentDescription = "${row.title}, ${row.openWords}"
                },
        )
        DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
            for (action in row.actions) {
                val delete = action.action is ListAction.Delete
                DropdownMenuItem(
                    text = { Text(action.title, color = if (delete) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface) },
                    onClick = {
                        menu = false
                        onAction(action)
                    },
                )
            }
        }
    }
}
