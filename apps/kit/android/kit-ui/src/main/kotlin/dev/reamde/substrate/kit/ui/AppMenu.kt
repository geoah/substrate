package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.NavigationRail
import androidx.compose.material3.NavigationRailItem
import androidx.compose.material3.PermanentDrawerSheet
import androidx.compose.material3.PermanentNavigationDrawer
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.AppMenuPresentation
import dev.reamde.substrate.kit.bridge.AppMenuSection

/** How the menu is drawn: by the window's width, or as one of the three. */
public enum class AppMenuLayout { Automatic, Bar, Rail, Drawer }

/**
 * The app's menu as its presentation words it: a navigation bar of its
 * sections in a compact window, a rail in a medium one, and a drawer under
 * the app's name in an expanded one, the section shown beside it. Choosing
 * one is the app's to send; the menu keeps no state. A section's mark is
 * its symbol's name in the design tokens (`menuIcon`).
 */
@Composable
public fun AppMenu(
    menu: AppMenuPresentation,
    select: (String) -> Unit,
    modifier: Modifier = Modifier,
    title: String? = null,
    layout: AppMenuLayout = AppMenuLayout.Automatic,
    content: @Composable (AppMenuSection) -> Unit,
) {
    val items = menu.sections
    val selected = items.firstOrNull { it.id == menu.selected }
    val choose = { item: AppMenuSection -> select(item.id) }
    BoxWithConstraints(modifier.fillMaxSize()) {
        val drawn = when {
            layout != AppMenuLayout.Automatic -> layout
            maxWidth < 600.dp -> AppMenuLayout.Bar
            maxWidth < 1200.dp -> AppMenuLayout.Rail
            else -> AppMenuLayout.Drawer
        }
        when (drawn) {
            AppMenuLayout.Bar, AppMenuLayout.Automatic -> Scaffold(bottomBar = {
                NavigationBar {
                    for (item in items) {
                        NavigationBarItem(
                            selected = item.id == menu.selected,
                            onClick = { choose(item) },
                            icon = { MenuIcon(item) },
                            label = { Text(item.title) },
                        )
                    }
                }
            }) { padding ->
                // Consumed, so a section's own top bar does not count the
                // status bar the scaffold already made room for.
                Box(Modifier.padding(padding).consumeWindowInsets(padding)) { selected?.let { content(it) } }
            }
            AppMenuLayout.Rail -> Row {
                NavigationRail {
                    for (item in items) {
                        NavigationRailItem(
                            selected = item.id == menu.selected,
                            onClick = { choose(item) },
                            icon = { MenuIcon(item) },
                            label = { Text(item.title) },
                        )
                    }
                }
                Box(Modifier.fillMaxHeight().weight(1f)) { selected?.let { content(it) } }
            }
            AppMenuLayout.Drawer -> PermanentNavigationDrawer(drawerContent = {
                PermanentDrawerSheet(Modifier.width(300.dp)) {
                    title?.let { Text(it, style = KitText.screenTitle.style, modifier = Modifier.padding(KitSpacing.screen)) }
                    for (item in items) {
                        NavigationDrawerItem(
                            label = { Text(item.title) },
                            selected = item.id == menu.selected,
                            onClick = { choose(item) },
                            icon = { Icon(menuIcon(item.symbol), contentDescription = null) },
                            badge = item.badge?.let { { Text("$it") } },
                            modifier = Modifier.padding(horizontal = KitSpacing.row),
                        )
                    }
                }
            }) { selected?.let { content(it) } }
        }
    }
}

@Composable
private fun MenuIcon(item: AppMenuSection) {
    BadgedBox(badge = { item.badge?.let { Badge { Text("$it") } } }) {
        Icon(menuIcon(item.symbol), contentDescription = null)
    }
}
