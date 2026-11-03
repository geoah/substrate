package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextAlign

/**
 * One section of a settings screen, as a grouped form's section is on an
 * Apple system: its title over its rows, which sit together on one
 * surface, and its footer under them. An app's own settings use it as the
 * kit's screens do.
 */
@Composable
public fun KitSection(
    title: String?,
    modifier: Modifier = Modifier,
    footer: String? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(modifier.fillMaxWidth().padding(horizontal = KitSpacing.content, vertical = KitSpacing.medium)) {
        if (title != null) {
            Text(
                title,
                style = KitText.heading.style,
                color = MaterialTheme.colorScheme.primary,
                modifier = Modifier.padding(start = KitSpacing.content, bottom = KitSpacing.medium),
            )
        }
        Surface(shape = RoundedCornerShape(KitRadius.card), color = MaterialTheme.colorScheme.surfaceContainerLow) {
            Column(Modifier.fillMaxWidth(), content = content)
        }
        if (footer != null) {
            Text(
                footer,
                style = KitText.footnote.style,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(start = KitSpacing.content, end = KitSpacing.content, top = KitSpacing.small),
            )
        }
    }
}

/** The rule between two rows of a section. */
@Composable
public fun KitRowDivider() {
    HorizontalDivider(Modifier.padding(start = KitSpacing.content), color = MaterialTheme.colorScheme.outlineVariant)
}

/** A row of a section: its label, the words under it, and what stands at its end. */
@Composable
public fun KitRow(
    label: String,
    modifier: Modifier = Modifier,
    supporting: String? = null,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable () -> Unit)? = null,
    onClick: (() -> Unit)? = null,
) {
    ListItem(
        headlineContent = { Text(label) },
        supportingContent = supporting?.let { { Text(it) } },
        leadingContent = leading,
        trailingContent = trailing,
        colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
        modifier = if (onClick != null) modifier.clickable(onClick = onClick) else modifier,
    )
}

/**
 * A label and its value, read the same in a read view and an edit view. An
 * identifier a person may copy is set as one, and a long value goes under
 * its label rather than being squeezed beside it.
 */
@Composable
public fun KitValueRow(label: String, value: String, modifier: Modifier = Modifier, verbatim: Boolean = false, stacked: Boolean = false) {
    val shown: @Composable () -> Unit = { KitValueText(value, verbatim, if (stacked) TextAlign.Start else TextAlign.End) }
    if (stacked) {
        ListItem(
            overlineContent = { Text(label) },
            headlineContent = shown,
            colors = ListItemDefaults.colors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow),
            modifier = modifier,
        )
    } else {
        KitRow(label, modifier, trailing = shown)
    }
}

/** A row that opens a screen of its own. */
@Composable
public fun KitNavigationRow(label: String, onClick: () -> Unit, modifier: Modifier = Modifier, supporting: String? = null) {
    KitRow(label, modifier, supporting = supporting, onClick = onClick, trailing = {
        Icon(KitMark.disclosure.icon, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
    })
}

/** The padding a row of a section takes when it is not a `ListItem`. */
public fun Modifier.kitRowPadding(): Modifier = padding(horizontal = KitSpacing.content, vertical = KitSpacing.row)

/** A whole row that answers a tap. */
public fun Modifier.clickableRow(onClick: () -> Unit): Modifier = clickable(onClick = onClick)

/**
 * A row's value: in the body's size, secondary; an identifier in a
 * monospaced face a size down, so a long one wraps rarely.
 */
@Composable
public fun KitValueText(value: String, verbatim: Boolean = false, textAlign: TextAlign = TextAlign.End) {
    Text(
        value,
        style = if (verbatim) KitText.secondary.style else KitText.body.style,
        fontFamily = if (verbatim) FontFamily.Monospace else null,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        textAlign = textAlign,
    )
}
