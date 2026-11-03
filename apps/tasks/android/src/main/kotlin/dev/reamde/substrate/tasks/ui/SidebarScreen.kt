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
import dev.reamde.substrate.kit.ui.menuIcon
import dev.reamde.substrate.kit.ui.style
import dev.reamde.substrate.tasks.bridge.ListAction
import dev.reamde.substrate.tasks.bridge.ListSelection
import dev.reamde.substrate.tasks.bridge.SidebarPresentation
import dev.reamde.substrate.tasks.bridge.SidebarRow
import dev.reamde.substrate.tasks.bridge.TasksApp
import dev.reamde.substrate.tasks.bridge.TasksPresentation

/**
 * The lists: what is due and the tasks in no list, then the lists in the
 * owner's order, each with how many of its tasks are open, a long press
 * offering its rename and delete. On a phone Today is a section of its own
 * in the App menu, so it is left out here.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun SidebarScreen(
    sidebar: SidebarPresentation,
    app: TasksPresentation,
    banner: AccessBanner?,
    actions: TasksActions,
    name: (ListNaming) -> Unit,
    showsToday: Boolean,
    selected: ListSelection? = null,
) {
    val show = { row: SidebarRow ->
        if (showsToday) actions.send(TasksApp.Command.Select(if (row.selection == ListSelection.Today) MENU_TODAY else "lists"))
        actions.send(TasksApp.Command.Show(row.selection))
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
                items(sidebar.fixed.filter { showsToday || it.selection != ListSelection.Today }, key = { it.id }) { row ->
                    SidebarRowView(row, row.selection == selected, onOpen = { show(row) }, onAction = {})
                }
                item {
                    Text(sidebar.listsTitle, style = KitText.heading.style, color = MaterialTheme.colorScheme.primary,
                         modifier = Modifier.padding(start = KitSpacing.content, top = KitSpacing.screen, bottom = KitSpacing.medium))
                }
                items(sidebar.lists.rows, key = { "list-" + it.id }) { row ->
                    SidebarRowView(row, row.selection == selected, onOpen = { show(row) }) { action ->
                        when (val chosen = action.action) {
                            is ListAction.Rename -> name(ListNaming(chosen.value, row.title))
                            is ListAction.Delete -> actions.send(TasksApp.Command.DeleteList(chosen.value))
                        }
                    }
                }
                sidebar.noLists?.let { none ->
                    item {
                        Text(none, style = KitText.footnote.style, color = MaterialTheme.colorScheme.onSurfaceVariant,
                             modifier = Modifier.padding(horizontal = KitSpacing.content))
                    }
                }
                // Room for the button floating over the last row.
                item { Box(Modifier.padding(KitSpacing.block * 2)) }
            }
        }
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
            leadingContent = { Icon(menuIcon(row.symbol), contentDescription = null) },
            trailingContent = { Text("${row.open}", style = KitText.body.style, color = MaterialTheme.colorScheme.onSurfaceVariant) },
            colors = ListItemDefaults.colors(
                containerColor = if (chosen) MaterialTheme.colorScheme.secondaryContainer else MaterialTheme.colorScheme.surface,
            ),
            modifier = Modifier
                .combinedClickable(onClick = onOpen, onLongClick = { if (row.actions.isNotEmpty()) menu = true })
                .semantics { contentDescription = "${row.title}, ${row.openWords}" },
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
