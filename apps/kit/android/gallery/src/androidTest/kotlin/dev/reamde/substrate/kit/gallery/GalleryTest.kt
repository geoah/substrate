package dev.reamde.substrate.kit.gallery

import androidx.test.ext.junit.runners.AndroidJUnit4
import dev.reamde.substrate.kit.Bridge
import dev.reamde.substrate.kit.StartResult
import dev.reamde.substrate.kit.bridge.GalleryPresentation
import dev.reamde.substrate.kit.bridge.ScreenKind
import dev.reamde.substrate.kit.ui.KitGallery
import dev.reamde.substrate.kit.ui.screen
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The Gallery app on a device: every fixture the screens' presenters make in Swift, here, crosses the bridge and decodes with the generated Kotlin, every
 * shared screen is among them, and each has its composable.
 */
@RunWith(AndroidJUnit4::class)
class GalleryTest {
    @Test
    fun everyFixtureSwiftMakesOnTheDeviceDecodesAndHasAComposable() = runBlocking {
        assertTrue("the bridge did not start: ${GalleryApplication.start}", GalleryApplication.start is StartResult.Started)
        val handle = Bridge.open("gallery").getOrThrow()
        try {
            val gallery = withTimeout(60_000) { handle.presentations(GalleryPresentation.serializer()).first() }
            assertTrue("no fixtures", gallery.fixtures.isNotEmpty())
            assertEquals("a fixture's name twice", gallery.fixtures.size, gallery.fixtures.map { it.name }.toSet().size)
            assertEquals(ScreenKind.entries.toSet(), gallery.fixtures.map { it.presentation.screen }.toSet())
            for (fixture in gallery.fixtures) assertNotNull(fixture.name, KitGallery.renderers[fixture.presentation.screen])
        } finally {
            handle.close()
        }
    }
}
