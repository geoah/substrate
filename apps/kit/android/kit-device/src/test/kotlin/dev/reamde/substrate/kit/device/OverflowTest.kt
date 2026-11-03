package dev.reamde.substrate.kit.device

import dev.reamde.substrate.kit.BridgeException
import dev.reamde.substrate.kit.bridge.BridgeFailure
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** A callback's values past the queue end the flow with an overflow, never vanish. */
class OverflowTest {
    @Test
    fun aFullQueueEndsTheFlowWithAnOverflowAndKeepsWhatItHeld() = runBlocking {
        var unregistered = false
        val flow = callbackFlow {
            // A callback that fires five times before anyone reads.
            repeat(5) { sendOrOverflow(it, "too fast") }
            awaitClose { unregistered = true }
        }.buffer(2)
        val seen = mutableListOf<Int>()
        val failure = runCatching { flow.collect { seen += it } }.exceptionOrNull()
        // What fit (the queue, and one a waiting reader took), in order, none skipped.
        assertTrue("$seen", seen.size in 2..4 && seen == (0 until seen.size).toList())
        assertTrue("$failure", failure is BridgeException && failure.failure.code == BridgeFailure.Code.overflow)
        assertTrue(unregistered)
    }

    @Test
    fun aQueueWithRoomTakesEveryValueInOrder() = runBlocking {
        val flow = callbackFlow {
            repeat(5) { sendOrOverflow(it, "too fast") }
            close()
            awaitClose {}
        }.buffer(8)
        val seen = mutableListOf<Int>()
        flow.collect { seen += it }
        assertEquals(listOf(0, 1, 2, 3, 4), seen)
    }
}
