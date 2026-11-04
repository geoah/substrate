package dev.reamde.substrate.kit.ui

import androidx.compose.material3.ColorScheme
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The scheme every accent makes, held to WCAG AA: each `on` role reads at 4.5:1 on what it is drawn on, the accent reads at 4.5:1 as text on the screen's
 * surface (a section's heading, the completed count), and at 3:1 on the containers it marks (a link in a card, a dialog's buttons). The accents are kit-ui's
 * own and every app.json's, so an app whose accent cannot make a readable scheme fails here, not on a phone.
 */
class KitSchemeTest {
    @Test
    fun primaryIsTheAccentExactly() {
        for ((name, accent) in accents) {
            assertEquals("$name light", accent.light.toArgb(), KitScheme.of(accent.light, dark = false).primary.toArgb())
            assertEquals("$name dark", accent.dark.toArgb(), KitScheme.of(accent.dark, dark = true).primary.toArgb())
        }
    }

    @Test
    fun everyRoleReads() {
        val failures = mutableListOf<String>()
        for ((name, accent) in accents) {
            for (dark in listOf(false, true)) {
                val scheme = KitScheme.of(if (dark) accent.dark else accent.light, dark)
                for ((pair, ratio) in pairs(scheme)) {
                    val (text, on, floor) = pair
                    val measured = KitScheme.contrast(text, on)
                    if (measured < floor) failures += "$name ${if (dark) "dark" else "light"}: $ratio is ${"%.2f".format(measured)}:1, under $floor:1"
                }
            }
        }
        assertTrue(failures.joinToString("\n"), failures.isEmpty())
    }

    @Test
    fun theSurfacesAreTheAccentsNotLavender() {
        // Material's baseline surface is #FEF7FF, tinted violet whatever the app: a neutral tone here carries the accent's hue so faintly that each
        // channel stays within a few steps of grey.
        for ((name, accent) in accents) {
            val surface = KitScheme.of(accent.light, dark = false).surface
            val channels = listOf(surface.red, surface.green, surface.blue).map { (it * 255).toInt() }
            assertTrue("$name's surface $channels is tinted", channels.max() - channels.min() <= 6)
        }
    }

    private class Accent(val light: Color, val dark: Color)

    /** Each pair: the text's colour, what it is drawn on, and its floor; named for the failure message. */
    private fun pairs(s: ColorScheme): Map<Triple<Color, Color, Double>, String> {
        val out = linkedMapOf<Triple<Color, Color, Double>, String>()
        fun text(text: Color, on: Color, name: String, floor: Double = 4.5) {
            out[Triple(text, on, floor)] = name
        }
        text(s.onPrimary, s.primary, "onPrimary on primary")
        text(s.onPrimaryContainer, s.primaryContainer, "onPrimaryContainer on primaryContainer")
        text(s.onSecondary, s.secondary, "onSecondary on secondary")
        text(s.onSecondaryContainer, s.secondaryContainer, "onSecondaryContainer on secondaryContainer")
        text(s.onTertiary, s.tertiary, "onTertiary on tertiary")
        text(s.onTertiaryContainer, s.tertiaryContainer, "onTertiaryContainer on tertiaryContainer")
        text(s.onError, s.error, "onError on error")
        text(s.onErrorContainer, s.errorContainer, "onErrorContainer on errorContainer")
        text(s.onBackground, s.background, "onBackground on background")
        text(s.inverseOnSurface, s.inverseSurface, "inverseOnSurface on inverseSurface")
        text(s.inversePrimary, s.inverseSurface, "inversePrimary on inverseSurface")
        text(s.onPrimaryFixed, s.primaryFixed, "onPrimaryFixed on primaryFixed")
        text(s.onPrimaryFixedVariant, s.primaryFixed, "onPrimaryFixedVariant on primaryFixed")
        text(s.onSecondaryFixed, s.secondaryFixed, "onSecondaryFixed on secondaryFixed")
        text(s.onSecondaryFixedVariant, s.secondaryFixed, "onSecondaryFixedVariant on secondaryFixed")
        text(s.onTertiaryFixed, s.tertiaryFixed, "onTertiaryFixed on tertiaryFixed")
        text(s.onTertiaryFixedVariant, s.tertiaryFixed, "onTertiaryFixedVariant on tertiaryFixed")
        val surfaces =
            mapOf(
                "surface" to s.surface,
                "surfaceVariant" to s.surfaceVariant,
                "surfaceDim" to s.surfaceDim,
                "surfaceBright" to s.surfaceBright,
                "surfaceContainerLowest" to s.surfaceContainerLowest,
                "surfaceContainerLow" to s.surfaceContainerLow,
                "surfaceContainer" to s.surfaceContainer,
                "surfaceContainerHigh" to s.surfaceContainerHigh,
                "surfaceContainerHighest" to s.surfaceContainerHighest,
                // A chosen row: its words are the surface's.
                "secondaryContainer" to s.secondaryContainer,
            )
        for ((name, surface) in surfaces) {
            text(s.onSurface, surface, "onSurface on $name")
            text(s.onSurfaceVariant, surface, "onSurfaceVariant on $name")
            text(s.error, surface, "error on $name")
        }
        text(s.primary, s.surface, "primary on surface")
        text(s.primary, s.background, "primary on background")
        text(s.primary, s.surfaceContainerLowest, "primary on surfaceContainerLowest")
        for (name in listOf("surfaceContainerLow", "surfaceContainer", "surfaceContainerHigh", "surfaceContainerHighest")) {
            text(s.primary, surfaces.getValue(name), "primary on $name", floor = 3.0)
        }
        return out
    }

    private val accents: Map<String, Accent> by lazy {
        val out = linkedMapOf<String, Accent>()
        val res = File(System.getProperty("substrate.kitui.res"))
        fun resource(folder: String) =
            Regex("""<color name="substrate_accent">(#[0-9A-Fa-f]{6})</color>""").find(res.resolve("$folder/substrate_accent.xml").readText())!!.groupValues[1]
        out["kit-ui"] = Accent(hex(resource("values")), hex(resource("values-night")))
        val apps = File(System.getProperty("substrate.apps"))
        for (json in apps.listFiles().orEmpty().map { it.resolve("app.json") }.filter { it.isFile }.sortedBy { it.path }) {
            val accent = Json.parseToJsonElement(json.readText()).jsonObject["accent"]?.jsonObject ?: continue
            out[json.parentFile.name] = Accent(hex(accent.getValue("light").jsonPrimitive.content), hex(accent.getValue("dark").jsonPrimitive.content))
        }
        assertTrue("no app names an accent under $apps", out.size > 1)
        out
    }

    private fun hex(value: String) = Color(0xFF000000 or value.removePrefix("#").toLong(16))
}
