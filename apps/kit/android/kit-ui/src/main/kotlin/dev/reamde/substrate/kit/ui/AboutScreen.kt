package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import dev.reamde.substrate.kit.bridge.AboutPresentation

/**
 * The build, its channel, an update offered and the privacy policy, as a section of a settings screen. A pre-release's version is long: it goes under its
 * label, whole.
 */
@Composable
public fun AboutScreen(presentation: AboutPresentation, modifier: Modifier = Modifier, onCheck: () -> Unit = {}, onInstall: () -> Unit = {}) {
    KitSection(presentation.title, modifier) {
        val rows = listOfNotNull(presentation.version, presentation.channel)
        rows.forEachIndexed { index, row ->
            if (index > 0) KitRowDivider()
            KitValueRow(row.label, row.value, verbatim = row.verbatim, stacked = true)
        }
        presentation.update?.let { update ->
            KitRowDivider()
            Column(Modifier.kitRowPadding(), verticalArrangement = Arrangement.spacedBy(KitSpacing.small)) {
                Text(update.title, style = KitText.heading.style)
                Text(update.message, style = KitText.secondary.style, color = MaterialTheme.colorScheme.onSurfaceVariant)
                FilledTonalButton(onClick = onInstall) { Text(update.install) }
            }
        }
        presentation.check?.let {
            KitRowDivider()
            KitRow(it, onClick = onCheck)
        }
        presentation.privacyPolicy?.let { policy ->
            val uris = LocalUriHandler.current
            KitRowDivider()
            KitRow(policy.title, onClick = { runCatching { uris.openUri(policy.url) } })
        }
    }
}
