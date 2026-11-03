package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.ProblemFix
import dev.reamde.substrate.kit.bridge.ProblemPresentation

/**
 * A problem in a sentence, where a status would be: its mark, a short
 * title and the server's or the system's own words, which a person may
 * copy. It sits in a card of its own or inside one, never as a box in a box.
 */
@Composable
public fun ProblemBox(problem: ProblemPresentation, modifier: Modifier = Modifier) {
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(KitSpacing.row)) {
        Icon(problem.symbol.icon, contentDescription = null, tint = problem.tone.tint, modifier = Modifier.size(20.dp))
        Column(verticalArrangement = Arrangement.spacedBy(KitSpacing.hairline)) {
            problem.title?.let { Text(it, style = KitText.heading.style, modifier = Modifier.semantics { heading() }) }
            SelectionContainer {
                Text(
                    problem.message,
                    style = KitText.secondary.style,
                    color = if (problem.title == null) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

/** A problem and the one thing that fixes it, as its presentation words it. */
@Composable
public fun ProblemView(problem: ProblemPresentation, modifier: Modifier = Modifier, fix: (ProblemFix.Action) -> Unit = {}) {
    Column(modifier, verticalArrangement = Arrangement.spacedBy(KitSpacing.row)) {
        ProblemBox(problem)
        problem.fix?.let { button ->
            OutlinedButton(onClick = { fix(button.action) }, modifier = Modifier.padding(start = 30.dp)) { Text(button.title) }
        }
    }
}
