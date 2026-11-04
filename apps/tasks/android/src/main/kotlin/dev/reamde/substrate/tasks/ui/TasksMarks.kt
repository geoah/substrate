package dev.reamde.substrate.tasks.ui

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material.icons.rounded.Flag
import androidx.compose.material.icons.rounded.HighlightOff
import androidx.compose.material.icons.rounded.KeyboardArrowDown
import androidx.compose.material.icons.rounded.KeyboardDoubleArrowUp
import androidx.compose.material.icons.rounded.PriorityHigh
import androidx.compose.material.icons.rounded.RadioButtonUnchecked
import androidx.compose.material.icons.rounded.Remove
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import dev.reamde.substrate.kit.bridge.DueWords
import dev.reamde.substrate.kit.bridge.ScreenTone
import dev.reamde.substrate.kit.ui.tint
import dev.reamde.substrate.tasks.bridge.TaskPriority
import dev.reamde.substrate.tasks.bridge.TaskStatus

/** Tasks' own marks, as the Apple app's `TasksSymbols` draws them in SF Symbols. */
object TasksMarks {
    fun icon(priority: TaskPriority): ImageVector =
        when (priority) {
            TaskPriority.none -> Icons.Rounded.Remove
            TaskPriority.low -> Icons.Rounded.KeyboardArrowDown
            TaskPriority.medium -> Icons.Rounded.PriorityHigh
            TaskPriority.high -> Icons.Rounded.Flag
            TaskPriority.urgent -> Icons.Rounded.KeyboardDoubleArrowUp
        }

    @Composable
    @ReadOnlyComposable
    fun color(priority: TaskPriority): Color =
        when (priority) {
            TaskPriority.none,
            TaskPriority.low -> MaterialTheme.colorScheme.onSurfaceVariant
            TaskPriority.medium -> Color(0xFFC79200)
            TaskPriority.high -> ScreenTone.caution.tint
            TaskPriority.urgent -> ScreenTone.critical.tint
        }

    @Composable
    @ReadOnlyComposable
    fun color(tone: DueWords.Tone): Color =
        when (tone) {
            DueWords.Tone.quiet -> MaterialTheme.colorScheme.onSurfaceVariant
            DueWords.Tone.soon -> ScreenTone.caution.tint
            DueWords.Tone.overdue -> ScreenTone.critical.tint
        }

    fun mark(status: TaskStatus): ImageVector =
        when (status) {
            TaskStatus.open -> Icons.Rounded.RadioButtonUnchecked
            TaskStatus.done -> Icons.Rounded.CheckCircle
            TaskStatus.abandoned -> Icons.Rounded.HighlightOff
        }
}
