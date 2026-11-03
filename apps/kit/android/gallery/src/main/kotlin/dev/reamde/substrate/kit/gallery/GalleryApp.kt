package dev.reamde.substrate.kit.gallery

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.Bridge
import dev.reamde.substrate.kit.StartResult
import dev.reamde.substrate.kit.bridge.GalleryPresentation
import dev.reamde.substrate.kit.ui.KitGallery
import dev.reamde.substrate.kit.ui.KitMark
import dev.reamde.substrate.kit.ui.KitSection
import dev.reamde.substrate.kit.ui.KitNavigationRow
import dev.reamde.substrate.kit.ui.KitRowDivider
import dev.reamde.substrate.kit.ui.screen
import kotlinx.coroutines.flow.emptyFlow

/**
 * The gallery: every fixture the `gallery` root presented, by screen, and
 * one of them drawn whole by kit-ui when it is chosen.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun GalleryApp(start: StartResult?, opened: String?, open: (String?) -> Unit) {
    if (start !is StartResult.Started) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            Text("The bridge did not start: $start", modifier = Modifier.padding(24.dp), color = MaterialTheme.colorScheme.error)
        }
        return
    }
    val handle = remember { Bridge.open("gallery").getOrNull() }
    DisposableEffect(handle) { onDispose { handle?.close() } }
    val presentations = remember(handle) { handle?.presentations(GalleryPresentation.serializer()) ?: emptyFlow() }
    val gallery by presentations.collectAsState(initial = null)
    val fixture = gallery?.fixtures?.firstOrNull { it.name == opened }
    if (fixture != null) {
        BackHandler { open(null) }
        Scaffold(topBar = {
            TopAppBar(
                title = { Text(fixture.name) },
                navigationIcon = { IconButton(onClick = { open(null) }) { Icon(KitMark.back.icon, contentDescription = "All screens") } },
            )
        }) { padding -> Box(Modifier.padding(padding)) { KitGallery.Render(fixture) } }
        return
    }
    Scaffold(topBar = { TopAppBar(title = { Text("Substrate Gallery") }) }) { padding ->
        val fixtures = gallery?.fixtures.orEmpty()
        LazyColumn(Modifier.padding(padding)) {
            if (gallery == null) item { ListItem(headlineContent = { Text("Waiting for Swift's fixtures…") }) }
            items(fixtures.groupBy { it.presentation.screen }.entries.toList(), key = { it.key.name }) { (screen, shown) ->
                KitSection(screen.name, footer = "${shown.size} states") {
                    shown.forEachIndexed { index, each ->
                        if (index > 0) KitRowDivider()
                        KitNavigationRow(each.name, onClick = { open(each.name) })
                    }
                }
            }
        }
    }
}
