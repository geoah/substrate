package dev.reamde.substrate.kit.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.spring
import androidx.compose.animation.fadeIn
import androidx.compose.animation.scaleIn
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import dev.reamde.substrate.kit.bridge.ConnectController
import dev.reamde.substrate.kit.bridge.ConnectPresentation
import dev.reamde.substrate.kit.bridge.LabeledValue
import dev.reamde.substrate.kit.bridge.ScreenSymbol
import dev.reamde.substrate.kit.bridge.ScreenTone
import kotlinx.serialization.json.JsonElement

/** The app's name and what it is for, above the address field. */
@Composable
public fun ConnectHeader(appName: String, modifier: Modifier = Modifier, tagline: String? = null, icon: (@Composable () -> Unit)? = null) {
    Column(modifier, horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(KitSpacing.card)) {
        icon?.invoke()
        Text(appName, style = KitText.appTitle.style, textAlign = TextAlign.Center)
        tagline?.let { Text(it, style = KitText.body.style, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center) }
    }
}

/**
 * The connect screen as its presentation words it: the field, the door the server found takes, the problem, the button and the footer. Every change is a
 * command; the screen keeps only what is typed, which the controller writes over when it moves the field's revision (the name a server was found from, an empty
 * field for another server).
 *
 * @param passwordDoor the app's own password form, drawn for the server found (the probe's whole answer, which the app hands back to Swift).
 */
@Composable
public fun ConnectScreen(
    presentation: ConnectPresentation,
    send: (ConnectController.Command) -> Unit,
    modifier: Modifier = Modifier,
    header: @Composable () -> Unit = {},
    passwordDoor: @Composable (JsonElement) -> Unit = {},
) {
    val field = presentation.field
    var text by remember(field.revision) { mutableStateOf(field.text) }
    Box(modifier.fillMaxWidth().verticalScroll(rememberScrollState()), contentAlignment = Alignment.TopCenter) {
        Column(
            Modifier.widthIn(max = 440.dp).padding(KitSpacing.screen),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(KitSpacing.block),
        ) {
            Box(Modifier.padding(top = 48.dp)) { header() }
            OutlinedTextField(
                value = text,
                onValueChange = {
                    text = it
                    if (it != field.text) send(ConnectController.Command.Edit(it))
                },
                label = { Text(field.label) },
                placeholder = { Text(field.placeholder) },
                leadingIcon = { Icon(ScreenSymbol.server.icon, contentDescription = null) },
                singleLine = true,
                enabled = !field.disabled,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false, imeAction = ImeAction.Go),
                keyboardActions = KeyboardActions(onGo = { send(ConnectController.Command.Proceed) }),
                modifier = Modifier.fillMaxWidth().semantics { contentDescription = field.label },
            )
            val tooOld = presentation.tooOld
            if (tooOld != null) {
                ServerTooOldScreen(
                    tooOld,
                    onTryAgain = { send(ConnectController.Command.CheckAgain) },
                    onChangeServer = {
                        send(ConnectController.Command.ChangeServer)
                        text = ""
                    },
                )
            } else {
                val found = presentation.found
                if (presentation.door == ConnectPresentation.Door.password && found != null) passwordDoor(found)
                presentation.problem?.let { problem -> KitCard(tint = problem.tone.tint) { ProblemBox(problem) } }
                presentation.button?.let { button ->
                    Button(
                        onClick = { send(ConnectController.Command.Proceed) },
                        enabled = button.enabled,
                        modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp),
                    ) {
                        if (button.busy) {
                            CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            Spacer(Modifier.width(KitSpacing.medium))
                        }
                        Text(button.title)
                    }
                }
                presentation.cancel?.let { cancel -> TextButton(onClick = { send(ConnectController.Command.CancelSignIn) }) { Text(cancel) } }
                presentation.footer?.let { ConnectFooter(it) }
            }
            UnrevokedNotices(presentation.unrevoked)
        }
    }
}

/** A label and its value, the value as the identifier it is. */
@Composable
private fun ConnectFact(fact: LabeledValue, color: Color) {
    Row(horizontalArrangement = Arrangement.spacedBy(KitSpacing.small)) {
        Text(fact.label, style = KitText.footnote.style, color = color, fontWeight = FontWeight.SemiBold)
        SelectionContainer { Text(fact.value, style = KitText.footnote.style, color = color, fontFamily = FontFamily.Monospace) }
    }
}

@Composable
private fun ConnectFooter(footer: ConnectPresentation.Footer) {
    val words = MaterialTheme.colorScheme.onSurfaceVariant
    Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(KitSpacing.small)) {
        // The check lands while the sign-in waits to open: it is what says the name was found.
        val found = footer.found
        AnimatedVisibility(
            visible = found != null,
            enter = fadeIn() + scaleIn(spring(dampingRatio = Spring.DampingRatioMediumBouncy), initialScale = 0.4f),
            exit = ExitTransition.None,
        ) {
            if (found != null) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(KitSpacing.small)) {
                    Icon(ScreenSymbol.found.icon, contentDescription = null, tint = ScreenTone.positive.tint, modifier = Modifier.size(16.dp))
                    ConnectFact(found, words)
                }
            }
        }
        footer.repository?.let { ConnectFact(it, words) }
        Text(footer.note, style = KitText.footnote.style, color = words, textAlign = TextAlign.Center)
    }
}
