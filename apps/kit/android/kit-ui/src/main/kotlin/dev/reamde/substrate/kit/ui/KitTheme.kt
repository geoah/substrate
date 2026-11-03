package dev.reamde.substrate.kit.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import dev.reamde.substrate.kit.bridge.ScreenTone

/**
 * Material 3 as the shared screens draw in it: the system's own scheme for
 * surfaces and controls, light or dark as the device is. What a tone says
 * by its colour comes from the design tokens, so a caution is the same
 * orange on both systems.
 */
@Composable
public fun KitTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = if (dark) darkColorScheme() else lightColorScheme(), content = content)
}

/** Whether the scheme in force is a dark one. */
public val isKitDark: Boolean
    @Composable @ReadOnlyComposable
    get() = MaterialTheme.colorScheme.surface.luminance() < 0.5f

/** A tone as a colour here: a quiet one is the scheme's secondary text, the rest the tokens'. */
public val ScreenTone.tint: Color
    @Composable @ReadOnlyComposable
    get() = if (this == ScreenTone.quiet) MaterialTheme.colorScheme.onSurfaceVariant else color(isKitDark)
