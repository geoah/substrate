package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.unit.dp

/** A short state word in a capsule: the colour darkened (lightened in dark mode) on its own pale tint, so it reads on it. */
@Composable
public fun StatusBadge(text: String, color: Color, modifier: Modifier = Modifier) {
    val dark = isKitDark
    Surface(modifier, shape = androidx.compose.foundation.shape.CircleShape, color = color.copy(alpha = 0.16f)) {
        Text(
            text,
            style = KitText.badge.style,
            color = lerp(color, if (dark) Color.White else Color.Black, if (dark) 0.2f else 0.3f),
            maxLines = 1,
            modifier = Modifier.padding(horizontal = KitSpacing.medium, vertical = 3.dp),
        )
    }
}
