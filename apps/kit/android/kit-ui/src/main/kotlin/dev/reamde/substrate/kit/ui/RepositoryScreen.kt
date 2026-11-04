package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.ConfirmationPresentation
import dev.reamde.substrate.kit.bridge.RepositoryPresentation
import dev.reamde.substrate.kit.bridge.ScreenTone

/**
 * The Repository screen as a section of a settings screen, as its presentation words it: the server, the repository and the server's version, what the app was
 * approved to do and until when, Change Access, the tokens not revoked yet, and Sign Out.
 */
@Composable
public fun RepositoryScreen(
    presentation: RepositoryPresentation,
    onSignOut: () -> Unit,
    modifier: Modifier = Modifier,
    onChangeAccess: (() -> Unit)? = null,
) {
    KitSection(presentation.title, modifier, footer = presentation.footer) {
        val rows = listOfNotNull(presentation.server, presentation.repository, presentation.serverVersion)
        rows.forEachIndexed { index, row ->
            if (index > 0) KitRowDivider()
            KitValueRow(row.label, row.value, verbatim = row.verbatim)
        }
        presentation.approved?.let { approved ->
            KitRowDivider()
            Column(Modifier.kitRowPadding(), verticalArrangement = Arrangement.spacedBy(KitSpacing.small)) {
                Text(approved.title, style = KitText.body.style)
                for (scope in approved.scopes) {
                    Row(horizontalArrangement = Arrangement.spacedBy(KitSpacing.medium)) {
                        Icon(KitMark.check.icon, contentDescription = null, tint = ScreenTone.positive.tint, modifier = Modifier.size(16.dp))
                        Text(scope.words, style = KitText.footnote.style, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
            KitRowDivider()
            KitValueRow(approved.until.label, approved.until.value, verbatim = approved.until.verbatim)
        }
        val changeAccess = presentation.changeAccess
        if (changeAccess != null && onChangeAccess != null) {
            KitRowDivider()
            KitRow(changeAccess, onClick = onChangeAccess)
        }
        presentation.access?.let {
            KitRowDivider()
            KitValueRow(it.label, it.value, verbatim = it.verbatim)
        }
        if (presentation.unrevoked.isNotEmpty()) {
            KitRowDivider()
            UnrevokedNotices(presentation.unrevoked, asRows = true)
        }
        KitRowDivider()
        KitDestructiveRow(presentation.signOut, onClick = onSignOut, confirmation = presentation.signOutConfirmation)
    }
}

/**
 * A row whose action gives something up, in the scheme's error colour. With a `confirmation`, a tap asks it first, in a menu anchored to the row, so the
 * question stands by what it asks about.
 */
@Composable
public fun KitDestructiveRow(
    label: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    confirmation: ConfirmationPresentation<*>? = null,
) {
    var asking by remember { mutableStateOf(false) }
    Box(modifier) {
        androidx.compose.material3.ListItem(
            headlineContent = {
                Text(label, color = if (enabled) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.38f))
            },
            colors = androidx.compose.material3.ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
            modifier = if (enabled) Modifier.clickableRow { if (confirmation == null) onClick() else asking = true } else Modifier,
        )
        if (confirmation != null) {
            DropdownMenu(expanded = asking, onDismissRequest = { asking = false }) {
                Column(
                    Modifier.widthIn(max = 280.dp).padding(horizontal = KitSpacing.content, vertical = KitSpacing.small),
                    verticalArrangement = Arrangement.spacedBy(KitSpacing.small),
                ) {
                    Text(confirmation.title, style = KitText.heading.style)
                    Text(confirmation.message, style = KitText.footnote.style, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                DropdownMenuItem(
                    text = { Text(confirmation.confirm, color = MaterialTheme.colorScheme.error) },
                    onClick = {
                        asking = false
                        onClick()
                    },
                )
                DropdownMenuItem(text = { Text(confirmation.cancel) }, onClick = { asking = false })
            }
        }
    }
}
