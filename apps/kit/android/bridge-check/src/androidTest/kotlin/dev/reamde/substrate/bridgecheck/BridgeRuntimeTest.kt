package dev.reamde.substrate.bridgecheck

import android.util.Log
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The bridge's runtime checks (app-kit.md §8) against the debug build, in
 * the app's process with the bridge its Application started. `android:smoke`
 * runs the same checks against the minified release build.
 */
@RunWith(AndroidJUnit4::class)
class BridgeRuntimeTest {
    @Test
    fun everyRuntimeCheckPasses() = runBlocking {
        val results = BridgeChecks(InstrumentationRegistry.getInstrumentation().targetContext) { Log.i("bridge-check", it) }.runAll()
        val failed = results.filter { !it.passed }
        assertEquals(failed.joinToString("\n") { "${it.name}: ${it.detail}" }, emptyList<BridgeChecks.Result>(), failed)
    }
}
