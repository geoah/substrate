package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.AccessAction
import dev.reamde.substrate.kit.bridge.AccessBanner
import dev.reamde.substrate.kit.bridge.AccessEndedPresentation
import dev.reamde.substrate.kit.bridge.IssueLine
import dev.reamde.substrate.kit.bridge.ScreenTone

/** The Access screen's banner: one line per issue, with its action, in a card tinted by the worst of them. */
@Composable
public fun AccessBannerView(banner: AccessBanner, perform: (AccessAction) -> Unit, modifier: Modifier = Modifier) {
    KitCard(modifier, tint = banner.tone.tint, spacing = KitSpacing.card) {
        banner.lines.forEachIndexed { index, line ->
            if (index > 0) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            IssueLineView(line, perform)
        }
    }
}

/** One issue's line: its mark, title, words, changes and action. */
@Composable
public fun IssueLineView(line: IssueLine, perform: (AccessAction) -> Unit, modifier: Modifier = Modifier) {
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(KitSpacing.row)) {
        Icon(line.symbol.icon, contentDescription = null, tint = line.tone.tint, modifier = Modifier.size(20.dp))
        Column(verticalArrangement = Arrangement.spacedBy(KitSpacing.medium)) {
            Column(verticalArrangement = Arrangement.spacedBy(KitSpacing.hairline)) {
                Text(line.title, style = KitText.heading.style, modifier = Modifier.semantics { heading() })
                Text(line.message, style = KitText.secondary.style, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (line.changes.isNotEmpty()) {
                Column(verticalArrangement = Arrangement.spacedBy(KitSpacing.small)) {
                    for (change in line.changes) ChangeLineView(change, style = KitText.secondary.style)
                    line.more?.let { Text(it, style = KitText.secondary.style, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
            }
            line.action?.let { button -> OutlinedButton(onClick = { perform(button.action) }) { Text(button.title) } }
        }
    }
}

/** The access ended or was revoked, as its presentation words it: what happened, and the one way out. */
@Composable
public fun AccessEndedScreen(presentation: AccessEndedPresentation, onSignInAgain: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier.widthIn(max = 440.dp).padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(KitSpacing.section),
    ) {
        Icon(presentation.symbol.icon, contentDescription = null, tint = ScreenTone.caution.tint, modifier = Modifier.size(48.dp))
        Text(presentation.title, style = KitText.screenTitle.style, textAlign = TextAlign.Center, modifier = Modifier.semantics { heading() })
        Text(presentation.message, style = KitText.body.style, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
        Button(onClick = onSignInAgain, modifier = Modifier.widthIn(min = 160.dp).heightIn(min = 48.dp)) { Text(presentation.signInAgain) }
    }
}
