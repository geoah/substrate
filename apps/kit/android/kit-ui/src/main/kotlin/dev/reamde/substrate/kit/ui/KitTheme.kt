package dev.reamde.substrate.kit.ui

import android.content.res.Configuration
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.remember
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import dev.reamde.substrate.kit.bridge.ScreenTone

/**
 * Material 3 as the shared screens and an app's own draw in it, light or dark as the device is: the whole scheme made from the app's accent ([KitScheme]), the
 * `substrate_accent` colour its build writes from its app.json, so every app is its own colour with no code of its own. What a tone says by its colour comes
 * from the design tokens, so a caution is the same orange on both systems.
 */
@Composable
public fun KitTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    val accent = accent(dark)
    val scheme = remember(accent, dark) { KitScheme.of(accent, dark) }
    MaterialTheme(colorScheme = scheme, content = content)
}

/** The accent resource in the appearance [dark] asks for, which may not be the configuration's: a gallery draws light on a dark device. */
@Composable
private fun accent(dark: Boolean): Color {
    val context = LocalContext.current
    val configuration = LocalConfiguration.current
    return remember(context, configuration, dark) {
        val night = if (dark) Configuration.UI_MODE_NIGHT_YES else Configuration.UI_MODE_NIGHT_NO
        val asked = Configuration(configuration).apply { uiMode = (uiMode and Configuration.UI_MODE_NIGHT_MASK.inv()) or night }
        Color(context.createConfigurationContext(asked).getColor(R.color.substrate_accent))
    }
}

/** Whether the scheme in force is a dark one. */
public val isKitDark: Boolean
    @Composable @ReadOnlyComposable get() = MaterialTheme.colorScheme.surface.luminance() < 0.5f

/** A tone as a colour here: a quiet one is the scheme's secondary text, the rest the tokens'. */
public val ScreenTone.tint: Color
    @Composable @ReadOnlyComposable get() = if (this == ScreenTone.quiet) MaterialTheme.colorScheme.onSurfaceVariant else color(isKitDark)
