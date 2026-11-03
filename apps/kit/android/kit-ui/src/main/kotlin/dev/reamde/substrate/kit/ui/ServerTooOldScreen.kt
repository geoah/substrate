package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.ScreenTone
import dev.reamde.substrate.kit.bridge.ServerTooOldPresentation

/**
 * A server that cannot do what the app needs, as its presentation words
 * it: which release it runs and which the app needs, what it cannot do
 * yet, who can change that, and the two ways on where the app offers them.
 */
@Composable
public fun ServerTooOldScreen(
    presentation: ServerTooOldPresentation,
    modifier: Modifier = Modifier,
    onTryAgain: (() -> Unit)? = null,
    onChangeServer: (() -> Unit)? = null,
) {
    Column(
        modifier.widthIn(max = 440.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(KitSpacing.section),
    ) {
        Icon(presentation.symbol.icon, contentDescription = null, tint = ScreenTone.caution.tint, modifier = Modifier.size(44.dp))
        Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(KitSpacing.medium)) {
            Text(presentation.title, style = KitText.screenTitle.style, textAlign = TextAlign.Center, modifier = Modifier.semantics { heading() })
            Text(presentation.summary, style = KitText.body.style, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
        }
        presentation.cannotTitle?.let { cannot ->
            KitCard {
                Text(cannot, style = KitText.heading.style)
                Column(verticalArrangement = Arrangement.spacedBy(KitSpacing.medium), modifier = Modifier.fillMaxWidth()) {
                    for (line in presentation.missing) ChangeLineView(line, style = KitText.secondary.style)
                }
            }
        }
        Text(presentation.note, style = KitText.footnote.style, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
        val tryAgain = presentation.tryAgain?.let { title -> onTryAgain?.let { title to it } }
        val changeServer = presentation.changeServer?.let { title -> onChangeServer?.let { title to it } }
        if (tryAgain != null || changeServer != null) {
            Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(KitSpacing.row)) {
                tryAgain?.let { (title, action) ->
                    Button(onClick = action, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) { Text(title) }
                }
                changeServer?.let { (title, action) -> TextButton(onClick = action) { Text(title) } }
            }
        }
    }
}
