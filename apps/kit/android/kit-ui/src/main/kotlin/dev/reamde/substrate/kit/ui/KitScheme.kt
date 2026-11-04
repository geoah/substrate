package dev.reamde.substrate.kit.ui

import androidx.compose.material3.ColorScheme
import androidx.compose.ui.graphics.Color
import kotlin.math.atan2
import kotlin.math.cbrt
import kotlin.math.cos
import kotlin.math.hypot
import kotlin.math.max
import kotlin.math.pow
import kotlin.math.roundToInt
import kotlin.math.sin

/**
 * The Material 3 scheme an app's accent makes. Primary is the accent itself, exactly, so a button on Android is the colour iOS tints it; every other role is a
 * tone of a palette made from it.
 *
 * A tone is Material's: CIELAB lightness, 0 black to 100 white, which fixes a colour's luminance whatever its hue, so the contrast of two roles is a function
 * of their tones alone and holds for any accent. Hue and chroma are OKLCH's, which keeps a hue as it lightens (CIELAB's blue turns purple), and a tone the hue
 * cannot reach at the palette's chroma is drawn at the most chroma the sRGB gamut has there. The palettes are the accent's (primary), the accent's hue at a
 * third of its chroma (secondary), sixty degrees on at half (tertiary), the accent's hue barely tinted (neutral and neutral variant), and red (error). There is
 * no wallpaper colour: an app looks the same on every phone.
 */
public object KitScheme {
    /** The scheme for an accent, which is the light accent in a light scheme and the dark one in a dark one. */
    public fun of(accent: Color, dark: Boolean): ColorScheme {
        val hue = Oklch.of(accent)
        val p = Palette(hue.h, hue.c)
        val s = Palette(hue.h, hue.c / 3)
        val t = Palette(hue.h + 60, hue.c / 2)
        val n = Palette(hue.h, max(hue.c, 0.1) * 0.04)
        val nv = Palette(hue.h, max(hue.c, 0.1) * 0.08)
        val e = Palette(ERROR.h, ERROR.c)
        val onAccent = onColor(accent, p)
        return if (!dark) {
            ColorScheme(
                primary = accent,
                onPrimary = onAccent,
                primaryContainer = p(90),
                onPrimaryContainer = p(10),
                inversePrimary = p(80),
                secondary = s(40),
                onSecondary = s(100),
                secondaryContainer = s(90),
                onSecondaryContainer = s(10),
                tertiary = t(40),
                onTertiary = t(100),
                tertiaryContainer = t(90),
                onTertiaryContainer = t(10),
                // Tone 99, not Material's 98: the accent is drawn as text on the screen's surface (a section's heading), and a mid-tone accent
                // such as Tasks' rose reads at 4.5:1 only on a surface this light.
                background = n(99),
                onBackground = n(10),
                surface = n(99),
                onSurface = n(10),
                surfaceVariant = nv(90),
                onSurfaceVariant = nv(30),
                surfaceTint = accent,
                inverseSurface = n(20),
                inverseOnSurface = n(95),
                error = e(40),
                onError = e(100),
                errorContainer = e(90),
                onErrorContainer = e(10),
                outline = nv(50),
                outlineVariant = nv(80),
                scrim = Color.Black,
                surfaceBright = n(99),
                surfaceDim = n(87),
                surfaceContainer = n(94),
                surfaceContainerHigh = n(92),
                surfaceContainerHighest = n(90),
                surfaceContainerLow = n(96),
                surfaceContainerLowest = n(100),
                primaryFixed = p(90),
                primaryFixedDim = p(80),
                onPrimaryFixed = p(10),
                onPrimaryFixedVariant = p(30),
                secondaryFixed = s(90),
                secondaryFixedDim = s(80),
                onSecondaryFixed = s(10),
                onSecondaryFixedVariant = s(30),
                tertiaryFixed = t(90),
                tertiaryFixedDim = t(80),
                onTertiaryFixed = t(10),
                onTertiaryFixedVariant = t(30),
            )
        } else {
            ColorScheme(
                primary = accent,
                onPrimary = onAccent,
                primaryContainer = p(30),
                onPrimaryContainer = p(90),
                inversePrimary = p(40),
                secondary = s(80),
                onSecondary = s(20),
                secondaryContainer = s(30),
                onSecondaryContainer = s(90),
                tertiary = t(80),
                onTertiary = t(20),
                tertiaryContainer = t(30),
                onTertiaryContainer = t(90),
                background = n(6),
                onBackground = n(90),
                surface = n(6),
                onSurface = n(90),
                surfaceVariant = nv(30),
                onSurfaceVariant = nv(80),
                surfaceTint = accent,
                inverseSurface = n(90),
                inverseOnSurface = n(20),
                error = e(80),
                onError = e(20),
                errorContainer = e(30),
                onErrorContainer = e(90),
                outline = nv(60),
                outlineVariant = nv(30),
                scrim = Color.Black,
                surfaceBright = n(24),
                surfaceDim = n(6),
                surfaceContainer = n(12),
                surfaceContainerHigh = n(17),
                surfaceContainerHighest = n(22),
                surfaceContainerLow = n(10),
                surfaceContainerLowest = n(4),
                primaryFixed = p(90),
                primaryFixedDim = p(80),
                onPrimaryFixed = p(10),
                onPrimaryFixedVariant = p(30),
                secondaryFixed = s(90),
                secondaryFixedDim = s(80),
                onSecondaryFixed = s(10),
                onSecondaryFixedVariant = s(30),
                tertiaryFixed = t(90),
                tertiaryFixedDim = t(80),
                onTertiaryFixed = t(10),
                onTertiaryFixedVariant = t(30),
            )
        }
    }

    /** WCAG's contrast ratio of two colours, 1 to 21. */
    public fun contrast(a: Color, b: Color): Double {
        val la = luminance(a)
        val lb = luminance(b)
        return (max(la, lb) + 0.05) / (minOf(la, lb) + 0.05)
    }

    /**
     * What is drawn on the accent: white or the palette's tone 10, whichever reads the better, and black where neither reaches 4.5:1 (an accent of middle
     * luminance, which black alone clears).
     */
    private fun onColor(accent: Color, p: Palette): Color {
        val best = listOf(Color.White, p(10)).maxBy { contrast(it, accent) }
        return if (contrast(best, accent) >= 4.5) best else Color.Black
    }

    private val ERROR = Oklch.of(Color(0xFFB3261E))

    private class Palette(val h: Double, val c: Double) {
        private val tones = HashMap<Int, Color>()

        operator fun invoke(tone: Int): Color = tones.getOrPut(tone) { Oklch.tone(tone.toDouble(), c, h) }
    }
}

/** The relative luminance WCAG's contrast is made of: linear sRGB weighted, which is CIE Y. */
private fun luminance(color: Color): Double = 0.2126 * linear(color.red) + 0.7152 * linear(color.green) + 0.0722 * linear(color.blue)

private fun linear(channel: Float): Double = channel.toDouble().let { if (it <= 0.04045) it / 12.92 else ((it + 0.055) / 1.055).pow(2.4) }

private fun encoded(channel: Double): Float = (if (channel <= 0.0031308) channel * 12.92 else 1.055 * channel.pow(1 / 2.4) - 0.055).toFloat()

/** OKLab in polar form: lightness, chroma, hue in degrees. */
private class Oklch(val l: Double, val c: Double, val h: Double) {
    companion object {
        fun of(color: Color): Oklch {
            val r = linear(color.red)
            val g = linear(color.green)
            val b = linear(color.blue)
            val l = cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b)
            val m = cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b)
            val s = cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b)
            val ll = 0.2104542553 * l + 0.7936177850 * m - 0.0040720468 * s
            val a = 1.9779984951 * l - 2.4285922050 * m + 0.4505937099 * s
            val bb = 0.0259040371 * l + 0.7827717662 * m - 0.8086757660 * s
            return Oklch(ll, hypot(a, bb), Math.toDegrees(atan2(bb, a)))
        }

        /** Linear sRGB of an OKLCH colour, which may lie outside [0, 1] where the gamut ends. */
        private fun rgb(l: Double, c: Double, h: Double): DoubleArray {
            val a = c * cos(Math.toRadians(h))
            val b = c * sin(Math.toRadians(h))
            val l3 = (l + 0.3963377774 * a + 0.2158037573 * b).pow(3)
            val m3 = (l - 0.1055613458 * a - 0.0638541728 * b).pow(3)
            val s3 = (l - 0.0894841775 * a - 1.2914855480 * b).pow(3)
            return doubleArrayOf(
                4.0767416621 * l3 - 3.3077115913 * m3 + 0.2309699292 * s3,
                -1.2684380046 * l3 + 2.6097574011 * m3 - 0.3413193965 * s3,
                -0.0041960863 * l3 - 0.7034186147 * m3 + 1.7076147010 * s3,
            )
        }

        private fun y(rgb: DoubleArray) = 0.2126 * rgb[0] + 0.7152 * rgb[1] + 0.0722 * rgb[2]

        /** The OKLab lightness at which hue and chroma have luminance [target]. */
        private fun lightness(target: Double, c: Double, h: Double): Double {
            var lo = 0.0
            var hi = 1.0
            repeat(40) {
                val mid = (lo + hi) / 2
                if (y(rgb(mid, c, h)) < target) lo = mid else hi = mid
            }
            return (lo + hi) / 2
        }

        private fun inGamut(rgb: DoubleArray) = rgb.all { it in -1e-4..1.0001 }

        /** Tone [tone] of a hue at chroma [c], or at the most chroma sRGB holds at that tone. */
        fun tone(tone: Double, c: Double, h: Double): Color {
            if (tone >= 100) return Color.White
            if (tone <= 0) return Color.Black
            val fy = (tone + 16) / 116
            val target = if (tone > 8) fy * fy * fy else tone / 903.2962962
            var rgb = rgb(lightness(target, c, h), c, h)
            if (!inGamut(rgb)) {
                var lo = 0.0
                var hi = c
                repeat(30) {
                    val mid = (lo + hi) / 2
                    if (inGamut(rgb(lightness(target, mid, h), mid, h))) lo = mid else hi = mid
                }
                rgb = rgb(lightness(target, lo, h), lo, h)
            }
            val (r, g, b) = rgb.map { (encoded(it.coerceIn(0.0, 1.0)) * 255).roundToInt() / 255f }
            return Color(r, g, b)
        }
    }
}
