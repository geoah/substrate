package dev.reamde.substrate.kit.ui

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onRoot
import com.github.takahirom.roborazzi.captureRoboImage
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.ScreenFixture
import dev.reamde.substrate.kit.bridge.ScreenKind
import java.io.File
import kotlinx.serialization.builtins.ListSerializer
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.ParameterizedRobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * The gallery as a check (docs/plans/app-kit.md §7.4): every fixture Swift wrote (`fixtures/screens.json`, from `ScreenFixtures.all`) drawn by its screen's
 * composable on the JVM, one test each, at the size SubstrateUI's gallery draws an iPhone in (393 by 852, twice over), in light mode. A screen without a
 * composable fails here. Each is written as `<fixture>.png` under the folder `-Psubstrate.gallery` names, else under build/. The clock stands still, so a busy
 * screen's spinner is drawn rather than waited out.
 */
@RunWith(ParameterizedRobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(qualifiers = "w393dp-h852dp-xhdpi")
class KitGalleryTest(@Suppress("unused") private val name: String, private val fixture: ScreenFixture) {
    @get:Rule val compose = createComposeRule()

    @Test
    fun theFixtureDraws() {
        assertNotNull("${fixture.name} has no composable", KitGallery.renderers[fixture.presentation.screen])
        val folder = File(System.getProperty("substrate.gallery") ?: "build/gallery/android").apply { mkdirs() }
        compose.mainClock.autoAdvance = false
        compose.setContent { KitTheme(dark = false) { KitGallery.Render(fixture) } }
        compose.mainClock.advanceTimeBy(400)
        compose.onRoot().captureRoboImage(File(folder, "${fixture.name}.png").path)
    }

    @Test
    fun everyScreenHasAComposable() {
        for (screen in ScreenKind.entries) assertTrue("$screen has no composable in KitGallery", screen in KitGallery.renderers)
    }

    companion object {
        @JvmStatic
        @ParameterizedRobolectricTestRunner.Parameters(name = "{0}")
        fun fixtures(): List<Array<Any>> =
            BridgeJson.decodeFromString(ListSerializer(ScreenFixture.serializer()), File(System.getProperty("substrate.screens.fixtures")).readText()).map {
                arrayOf(it.name, it)
            }
    }
}
