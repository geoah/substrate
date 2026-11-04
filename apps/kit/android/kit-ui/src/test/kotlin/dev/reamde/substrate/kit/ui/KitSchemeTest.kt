package dev.reamde.substrate.kit.ui

import androidx.compose.material3.ColorScheme
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import java.io.File
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The scheme every accent makes, held to WCAG AA: each `on` role reads at 4.5:1 on what it is drawn on, and primary, the accent's text tone, reads at 4.5:1 as
 * text on every surface it is drawn on (a section's heading, a link in a card, a dialog's buttons), since accent text is as small as body text. The accents are
 * kit-ui's own and every app.json's, so an app whose accent cannot make a readable scheme fails here, not on a phone; the text tones of kit-ui and of every app
 * on Android are held here, and every app's on Apple's backgrounds by lint:apps.
 */
class KitSchemeTest {
    @Test
    fun primaryIsTheTextToneExactly() {
        for ((name, accent) in accents) {
            assertEquals("$name light", accent.textLight.toArgb(), scheme(accent, dark = false).primary.toArgb())
            assertEquals("$name dark", accent.textDark.toArgb(), scheme(accent, dark = true).primary.toArgb())
        }
    }

    @Test
    fun everyRoleReads() {
        val failures = mutableListOf<String>()
        for ((name, accent) in accents) {
            for (dark in listOf(false, true)) {
                for ((pair, ratio) in pairs(scheme(accent, dark))) {
                    val (text, on, floor) = pair
                    val measured = KitScheme.contrast(text, on)
                    if (measured < floor) failures += "$name ${if (dark) "dark" else "light"}: $ratio is ${"%.2f".format(measured)}:1, under $floor:1"
                }
            }
        }
        assertTrue(failures.joinToString("\n"), failures.isEmpty())
    }

    @Test
    fun theTextToneReadsOnEverySurface() {
        val failures = mutableListOf<String>()
        for ((name, accent) in accents.filterValues { it.android }) {
            for (dark in listOf(false, true)) {
                val scheme = scheme(accent, dark)
                for ((surface, colour) in KitScheme.textSurfaces(scheme)) {
                    val measured = KitScheme.contrast(scheme.primary, colour)
                    if (measured >= 4.5) continue
                    val derived = KitScheme.textTone(if (dark) accent.dark else accent.light, dark)
                    failures +=
                        "$name ${if (dark) "dark" else "light"}: the text tone ${hex(scheme.primary)} is ${"%.2f".format(measured)}:1 on $surface, under 4.5:1;" +
                            " ${hex(derived)} is the accent's hue at the nearest tone that reads on every surface"
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
            val surface = scheme(accent, dark = false).surface
            val channels = listOf(surface.red, surface.green, surface.blue).map { (it * 255).toInt() }
            assertTrue("$name's surface $channels is tinted", channels.max() - channels.min() <= 6)
        }
    }

    /** An accent and its text tone, and whether an Android build draws them. */
    private class Accent(val light: Color, val dark: Color, val textLight: Color, val textDark: Color, val android: Boolean)

    private fun scheme(accent: Accent, dark: Boolean) =
        if (dark) KitScheme.of(accent.dark, accent.textDark, dark = true) else KitScheme.of(accent.light, accent.textLight, dark = false)

    private fun hex(color: Color) = "#%06X".format(color.toArgb() and 0xFFFFFF)

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
        return out
    }

    private val accents: Map<String, Accent> by lazy {
        val out = linkedMapOf<String, Accent>()
        val res = File(System.getProperty("substrate.kitui.res"))
        fun resource(folder: String, name: String) =
            color(Regex("""<color name="$name">(#[0-9A-Fa-f]{6})</color>""").find(res.resolve("$folder/substrate_accent.xml").readText())!!.groupValues[1])
        out["kit-ui"] =
            Accent(
                resource("values", "substrate_accent"),
                resource("values-night", "substrate_accent"),
                resource("values", "substrate_accent_text"),
                resource("values-night", "substrate_accent_text"),
                android = true,
            )
        val apps = File(System.getProperty("substrate.apps"))
        for (json in apps.listFiles().orEmpty().map { it.resolve("app.json") }.filter { it.isFile }.sortedBy { it.path }) {
            val doc = Json.parseToJsonElement(json.readText()).jsonObject
            val accent = doc["accent"]?.jsonObject ?: continue
            val text = accent.getValue("text").jsonObject
            fun named(value: JsonObject, mode: String) = color(value.getValue(mode).jsonPrimitive.content)
            out[json.parentFile.name] =
                Accent(
                    named(accent, "light"),
                    named(accent, "dark"),
                    named(text, "light"),
                    named(text, "dark"),
                    android = doc["platforms"]?.jsonArray.orEmpty().any { it.jsonPrimitive.content == "android" },
                )
        }
        assertTrue("no app names an accent under $apps", out.size > 1)
        out
    }

    private fun color(value: String) = Color(0xFF000000 or value.removePrefix("#").toLong(16))
}
