package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.ChangeLine

/**
 * One change an approval would make: its mark, its words, and the fact
 * beside it, a blocked one's in full contrast, since that sentence is what
 * the owner acts on. Inside a card its words are set a size down (`style`).
 */
@Composable
public fun ChangeLineView(line: ChangeLine, modifier: Modifier = Modifier, style: TextStyle = KitText.body.style) {
    Row(modifier.semantics(mergeDescendants = true) {}, horizontalArrangement = Arrangement.spacedBy(KitSpacing.row)) {
        Icon(line.symbol.icon, contentDescription = null, tint = line.tone.tint, modifier = Modifier.size(20.dp))
        Column(verticalArrangement = Arrangement.spacedBy(KitSpacing.hairline)) {
            Text(line.words, style = style)
            line.detail?.let {
                Text(
                    it,
                    style = KitText.footnote.style,
                    color = if (line.detailStands) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}
