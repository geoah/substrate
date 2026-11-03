package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.ScreenTone
import dev.reamde.substrate.kit.bridge.StatusAction
import dev.reamde.substrate.kit.bridge.StatusPresentation

/**
 * The Status screen as sections of a settings screen: the sentence, why
 * where there is a reason, each package, what to do about it, and the
 * writes the server refused, each with Retry and Discard.
 */
@Composable
public fun StatusScreen(presentation: StatusPresentation, perform: (StatusAction) -> Unit, modifier: Modifier = Modifier) {
    Column(modifier) {
        KitSection(presentation.title, footer = presentation.footer) {
            KitRow(presentation.sentence, leading = {
                Icon(
                    presentation.symbol.icon,
                    contentDescription = null,
                    tint = if (presentation.tone == ScreenTone.quiet) MaterialTheme.colorScheme.onSurface else presentation.tone.tint,
                )
            })
            presentation.problem?.let {
                KitRowDivider()
                ProblemBox(it, Modifier.kitRowPadding())
            }
            for (line in presentation.packages) {
                KitRowDivider()
                KitValueRow(line.row.label, line.row.value, verbatim = line.row.verbatim)
            }
            for (button in presentation.actions) {
                KitRowDivider()
                val sending = button.action == StatusAction.SendNow && presentation.busy
                KitRow(
                    button.title,
                    onClick = if (sending) null else ({ perform(button.action) }),
                    trailing = if (sending) ({ CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) }) else null,
                )
            }
        }
        presentation.refused?.let { refused ->
            KitSection(refused.title, footer = refused.footer) {
                refused.rows.forEachIndexed { index, row ->
                    if (index > 0) KitRowDivider()
                    Column(Modifier.kitRowPadding(), verticalArrangement = Arrangement.spacedBy(KitSpacing.small)) {
                        Text(row.title, style = KitText.heading.style)
                        SelectionContainer {
                            Text(row.recordID, style = KitText.identifier.style, fontFamily = FontFamily.Monospace,
                                 color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        row.words?.let { Text(it, style = KitText.footnote.style, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                        Row(horizontalArrangement = Arrangement.spacedBy(KitSpacing.content)) {
                            TextButton(onClick = { perform(row.retry.action) }) { Text(row.retry.title) }
                            TextButton(onClick = { perform(row.discard.action) }) {
                                Text(row.discard.title, color = MaterialTheme.colorScheme.error)
                            }
                        }
                    }
                }
            }
        }
    }
}
