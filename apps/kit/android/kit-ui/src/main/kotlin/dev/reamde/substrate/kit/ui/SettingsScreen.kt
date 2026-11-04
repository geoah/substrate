package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import dev.reamde.substrate.kit.bridge.ScreenTone
import dev.reamde.substrate.kit.bridge.SettingsCommand
import dev.reamde.substrate.kit.bridge.SettingsPresentation
import dev.reamde.substrate.kit.bridge.SettingsRow

/** A settings presentation as sections: each row drawn by its control, each change a command. */
@Composable
public fun SettingsScreen(presentation: SettingsPresentation, send: (SettingsCommand) -> Unit, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        for (section in presentation.sections) {
            KitSection(section.title, footer = section.footer) {
                section.rows.forEachIndexed { index, row ->
                    if (index > 0) KitRowDivider()
                    SettingsRowView(row, send)
                }
            }
        }
    }
}

/** One settings row, by its control. */
@Composable
public fun SettingsRowView(row: SettingsRow, send: (SettingsCommand) -> Unit, modifier: Modifier = Modifier) {
    when (val control = row.control) {
        is SettingsRow.Control.Toggle ->
            KitRow(
                row.label,
                modifier,
                supporting = row.detail,
                onClick = { send(SettingsCommand.Toggle(row.id, !control.value)) },
                trailing = { Switch(checked = control.value, onCheckedChange = { send(SettingsCommand.Toggle(row.id, it)) }) },
            )
        is SettingsRow.Control.Choice -> {
            var open by remember { mutableStateOf(false) }
            Box(modifier) {
                KitRow(
                    row.label,
                    supporting = control.options.firstOrNull { it.id == control.selected }?.label,
                    onClick = { open = true },
                )
                DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
                    for (option in control.options) {
                        DropdownMenuItem(
                            text = { Text(option.label) },
                            trailingIcon = if (option.id == control.selected) ({ Icon(KitMark.check.icon, contentDescription = null) }) else null,
                            onClick = {
                                open = false
                                send(SettingsCommand.Choose(row.id, option.id))
                            },
                        )
                    }
                }
            }
        }
        is SettingsRow.Control.Value -> KitRow(row.label, modifier, supporting = row.detail, trailing = { KitValueText(control.value, control.verbatim) })
        SettingsRow.Control.Link -> KitNavigationRow(row.label, onClick = { send(SettingsCommand.Open(row.id)) }, modifier, supporting = row.detail)
        is SettingsRow.Control.Action ->
            if (control.destructive) {
                KitDestructiveRow(
                    row.label,
                    onClick = { send(SettingsCommand.Perform(row.id)) },
                    modifier,
                    enabled = control.enabled,
                    confirmation = row.confirmation,
                )
            } else {
                KitRow(row.label, modifier, supporting = row.detail, onClick = if (control.enabled) ({ send(SettingsCommand.Perform(row.id)) }) else null)
            }
        is SettingsRow.Control.Note ->
            KitRow(
                row.label,
                modifier,
                supporting = row.detail,
                leading = mark(control.value)?.let { icon -> { Icon(icon, contentDescription = null, tint = control.value.tint) } },
            )
        is SettingsRow.Control.Problem -> ProblemBox(control.value, modifier.kitRowPadding())
    }
}

/** A note's mark by its tone: allowed or done, something to know, something to act on or that stopped; none for quiet words. */
private fun mark(tone: ScreenTone) =
    when (tone) {
        ScreenTone.positive -> KitMark.check.icon
        ScreenTone.informative -> KitMark.info.icon
        ScreenTone.caution,
        ScreenTone.critical -> dev.reamde.substrate.kit.bridge.ScreenSymbol.warning.icon
        ScreenTone.quiet,
        ScreenTone.supporting -> null
    }
