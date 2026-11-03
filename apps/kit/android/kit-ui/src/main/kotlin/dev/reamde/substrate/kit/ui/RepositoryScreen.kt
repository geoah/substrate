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
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.RepositoryPresentation
import dev.reamde.substrate.kit.bridge.ScreenTone

/**
 * The Repository screen as a section of a settings screen, as its
 * presentation words it: the server, the repository and the server's
 * version, what the app was approved to do and until when, Change Access,
 * the tokens not revoked yet, and Sign Out.
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
        KitDestructiveRow(presentation.signOut, onClick = onSignOut)
    }
}

/** A row whose action gives something up, in the scheme's error colour. */
@Composable
public fun KitDestructiveRow(label: String, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true) {
    androidx.compose.material3.ListItem(
        headlineContent = {
            Text(label, color = if (enabled) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.38f))
        },
        colors = androidx.compose.material3.ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
        modifier = modifier.then(if (enabled) Modifier.clickableRow(onClick) else Modifier),
    )
}
