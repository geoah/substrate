package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.compositeOver
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/** A card of the sign-in and status screens: a plain fill tinted by what it says, never elevated, the same card SubstrateUI's `KitCard` draws. */
@Composable
public fun KitCard(
    modifier: Modifier = Modifier,
    tint: Color? = null,
    spacing: Dp = KitSpacing.medium,
    content: @Composable ColumnScope.() -> Unit,
) {
    val surface = MaterialTheme.colorScheme.surface
    val base = tint ?: MaterialTheme.colorScheme.onSurfaceVariant
    Card(
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(KitRadius.card),
        colors = CardDefaults.cardColors(containerColor = base.copy(alpha = if (tint == null) 0.08f else 0.10f).compositeOver(surface)),
        border = BorderStroke(0.5.dp, base.copy(alpha = 0.18f)),
    ) {
        Column(Modifier.padding(KitSpacing.card), verticalArrangement = Arrangement.spacedBy(spacing), content = content)
    }
}
