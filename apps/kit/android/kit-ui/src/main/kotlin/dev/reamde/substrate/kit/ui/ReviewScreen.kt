package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.ReviewPresentation
import dev.reamde.substrate.kit.bridge.ReviewSection

/**
 * The review as its presentation words it, before the browser opens: the
 * heading and why, each list of what the app asks for and sets up, and the
 * way on, with Cancel where a sheet's is.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
public fun ReviewScreen(presentation: ReviewPresentation, onContinue: () -> Unit, onCancel: () -> Unit, modifier: Modifier = Modifier) {
    Scaffold(
        modifier = modifier,
        topBar = {
            TopAppBar(title = {}, navigationIcon = { TextButton(onClick = onCancel) { Text(presentation.cancelTitle) } })
        },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState())) {
            Column(
                Modifier.padding(horizontal = KitSpacing.screen, vertical = KitSpacing.medium),
                verticalArrangement = Arrangement.spacedBy(KitSpacing.small),
            ) {
                Text(presentation.title, style = KitText.screenTitle.style, modifier = Modifier.semantics { heading() })
                Text(presentation.intro, style = KitText.body.style, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            ReviewSections(presentation.sections)
            Column(
                Modifier.padding(horizontal = KitSpacing.screen, vertical = KitSpacing.content),
                verticalArrangement = Arrangement.spacedBy(KitSpacing.row),
            ) {
                Button(onClick = onContinue, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) { Text(presentation.continueTitle) }
                Text(presentation.footer, style = KitText.footnote.style, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/** A review's lists, as sections. A sign-in screen shows the same sections the review does. */
@Composable
public fun ReviewSections(sections: List<ReviewSection>, modifier: Modifier = Modifier) {
    Column(modifier) {
        for (section in sections) {
            KitSection(section.title, footer = section.footer) {
                section.rows.forEachIndexed { index, row ->
                    if (index > 0) KitRowDivider()
                    ChangeLineView(row, Modifier.kitRowPadding())
                }
            }
        }
    }
}
