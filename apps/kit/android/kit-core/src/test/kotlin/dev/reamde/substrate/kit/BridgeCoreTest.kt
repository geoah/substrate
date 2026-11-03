@file:OptIn(ExperimentalCoroutinesApi::class)

package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.BridgeFailure
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.DNSAnswer
import dev.reamde.substrate.kit.bridge.DeviceRequest
import dev.reamde.substrate.kit.bridge.Envelope
import dev.reamde.substrate.kit.bridge.EnvironmentUpdate
import dev.reamde.substrate.kit.bridge.Handle
import dev.reamde.substrate.kit.bridge.ListMessage
import dev.reamde.substrate.kit.bridge.ListRow
import dev.reamde.substrate.kit.bridge.Op
import java.util.concurrent.CopyOnWriteArrayList
import kotlin.concurrent.thread
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** BridgeCore against a Swift that is a list of what it was posted. */
class BridgeCoreTest {
    private class FakeSwift {
        val posted = CopyOnWriteArrayList<Envelope>()
        var refuse: (Envelope) -> Boolean = { false }
        var onPost: (Envelope) -> Unit = {}

        fun post(text: String): String {
            val envelope = BridgeJson.decodeFromString(Envelope.serializer(), text)
            posted += envelope
            onPost(envelope)
            val reply = when {
                refuse(envelope) -> Envelope(1, Op.error, body = BridgeJson.encodeToJsonElement(BridgeFailure.serializer(), BridgeFailure(BridgeFailure.Code.unknownCall, "no")))
                envelope.op == Op.open -> Envelope(1, Op.opened, handle = Handle(1, 1))
                else -> Envelope(1, Op.accepted)
            }
            return BridgeJson.encodeToString(Envelope.serializer(), reply)
        }
    }

    private fun deliver(core: BridgeCore, envelope: Envelope) = core.receive(BridgeJson.encodeToString(Envelope.serializer(), envelope))

    private fun list(message: ListMessage): JsonElement = BridgeJson.encodeToJsonElement(ListMessage.Serializer, message)

    private fun row(id: String, text: String) = ListRow(id, JsonPrimitive(text))

    private fun request(request: DeviceRequest): JsonElement = BridgeJson.encodeToJsonElement(DeviceRequest.Serializer, request)

    private fun TestScope.core(swift: FakeSwift, services: DeviceServices = NoDeviceServices) =
        BridgeCore(swift::post, services, this)

    @Test
    fun presentationsAndListsBecomeTheHandlesState() = runTest {
        val swift = FakeSwift()
        val core = core(swift)
        val handle = core.open("check").getOrThrow()
        deliver(core, Envelope(1, Op.presentation, handle = handle.handle, body = buildJsonObject { put("step", 1) }))
        assertEquals(buildJsonObject { put("step", 1) }, handle.state.value.presentation)
        deliver(core, Envelope(1, Op.list, handle = handle.handle, body = list(ListMessage.Whole("rows", 1, listOf(row("a", "A"), row("b", "B"))))))
        deliver(core, Envelope(1, Op.list, handle = handle.handle, body = list(ListMessage.Delta("rows", 1, 2, listOf(row("c", "C")), listOf(row("a", "a!")), listOf("b"), listOf("c", "a")))))
        assertEquals(listOf(row("c", "C"), row("a", "a!")), handle.state.value.lists["rows"])
        deliver(core, Envelope(1, Op.closed, handle = handle.handle))
        assertTrue(handle.state.value.closed)
        // After `closed`, nothing changes the handle's state.
        deliver(core, Envelope(1, Op.presentation, handle = handle.handle, body = JsonPrimitive("late")))
        assertEquals(buildJsonObject { put("step", 1) }, handle.state.value.presentation)
    }

    @Test
    fun aDeltaFromAnotherRevisionAsksForTheListWhole() = runTest {
        val swift = FakeSwift()
        val core = core(swift)
        val handle = core.open("check").getOrThrow()
        deliver(core, Envelope(1, Op.list, handle = handle.handle, body = list(ListMessage.Whole("rows", 1, listOf(row("a", "A"))))))
        deliver(core, Envelope(1, Op.list, handle = handle.handle, body = list(ListMessage.Delta("rows", 2, 3, emptyList(), emptyList(), emptyList(), listOf("a")))))
        assertEquals(Op.resync, swift.posted.last().op)
        // Deltas until the whole list are dropped; the whole one applies.
        deliver(core, Envelope(1, Op.list, handle = handle.handle, body = list(ListMessage.Delta("rows", 3, 4, listOf(row("z", "Z")), emptyList(), emptyList(), listOf("a", "z")))))
        assertEquals(listOf(row("a", "A")), handle.state.value.lists["rows"])
        deliver(core, Envelope(1, Op.list, handle = handle.handle, body = list(ListMessage.Whole("rows", 4, listOf(row("a", "A"), row("z", "Z"))))))
        assertEquals(2, handle.state.value.lists.getValue("rows").size)
    }

    @Test
    fun aValueCallIsAnsweredOnceOnItsDispatcher() = runTest {
        val swift = FakeSwift()
        val dispatcher = StandardTestDispatcher(testScheduler)
        val core = core(swift) { DeviceAnswer.Value(dispatcher) { BridgeJson.encodeToJsonElement(DNSAnswer.Serializer, DNSAnswer.Answered(0, emptyList())) } }
        deliver(core, Envelope(1, Op.request, id = 7, body = request(DeviceRequest.DnsTXT("example.com", 1))))
        advanceUntilIdle()
        val replies = swift.posted.filter { it.id == 7L }
        assertEquals(listOf(Op.reply), replies.map { it.op })
        assertEquals(0, core.runningCalls)
    }

    @Test
    fun aStreamGoesOutInOrderThenEndsAndStopsWhenSwiftRefusesAChunk() = runTest {
        val swift = FakeSwift()
        val dispatcher = StandardTestDispatcher(testScheduler)
        val core = core(swift) { DeviceAnswer.Stream(dispatcher, flow { repeat(5) { emit(JsonPrimitive(it)) } }) }
        deliver(core, Envelope(1, Op.request, id = 1, body = request(DeviceRequest.ConnectivityWatch)))
        advanceUntilIdle()
        assertEquals(listOf(0L, 1L, 2L, 3L, 4L), swift.posted.filter { it.op == Op.chunk }.map { it.seq })
        assertEquals(Op.end, swift.posted.last().op)

        swift.posted.clear()
        swift.refuse = { it.op == Op.chunk && it.seq == 2L }
        deliver(core, Envelope(1, Op.request, id = 2, body = request(DeviceRequest.ConnectivityWatch)))
        advanceUntilIdle()
        assertEquals(listOf(Op.chunk, Op.chunk, Op.chunk), swift.posted.map { it.op })
        assertEquals(0, core.runningCalls)
    }

    @Test
    fun swiftsCancelStopsTheCallAndNothingAnswersIt() = runTest {
        val swift = FakeSwift()
        val dispatcher = StandardTestDispatcher(testScheduler)
        val started = CompletableDeferred<Unit>()
        val core = core(swift) {
            DeviceAnswer.Value(dispatcher) {
                started.complete(Unit)
                awaitCancellation()
            }
        }
        deliver(core, Envelope(1, Op.request, id = 3, body = request(DeviceRequest.ConnectivityCurrent)))
        advanceUntilIdle()
        assertTrue(started.isCompleted)
        assertEquals(1, core.runningCalls)
        deliver(core, Envelope(1, Op.cancel, id = 3))
        advanceUntilIdle()
        assertEquals(0, core.runningCalls)
        assertTrue(swift.posted.none { it.id == 3L })
    }

    @Test
    fun anUnsupportedOrThrowingServiceIsAStructuredErrorThatHidesSecrets() = runTest {
        val swift = FakeSwift()
        val dispatcher = StandardTestDispatcher(testScheduler)
        val core = core(swift) { request ->
            when (request) {
                is DeviceRequest.SecureRead -> DeviceAnswer.Value(dispatcher) { throw IllegalStateException("the token is s3cret") }
                else -> DeviceAnswer.Unsupported("not here")
            }
        }
        deliver(core, Envelope(1, Op.request, id = 4, body = request(DeviceRequest.ConnectivityCurrent)))
        deliver(core, Envelope(1, Op.request, id = 5, body = request(DeviceRequest.SecureRead("s", "a"))))
        advanceUntilIdle()
        val unsupported = BridgeJson.decodeFromJsonElement(BridgeFailure.serializer(), swift.posted.first { it.id == 4L }.body!!)
        assertEquals(BridgeFailure.Code.unsupported, unsupported.code)
        val thrown = BridgeJson.decodeFromJsonElement(BridgeFailure.serializer(), swift.posted.first { it.id == 5L }.body!!)
        assertEquals(BridgeFailure.Code.kotlinException, thrown.code)
        assertFalse(thrown.message.contains("s3cret"))
    }

    @Test
    fun anUnreadableMessageIsCountedNotThrown() = runTest {
        val core = core(FakeSwift())
        core.receive("not an envelope")
        core.receive("""{"v":1,"op":"presentation","handle":{"id":9,"generation":1},"body":1}""")
        assertEquals(2L, core.failureCount)
        assertNull(null)
    }

    @Test
    fun aPresentationSwiftDeliversBeforeOpenReturnsIsKept() = runTest {
        val swift = FakeSwift()
        val core = core(swift)
        var delivering: Thread? = null
        // Swift's outbox delivers the root's first presentation from its own
        // thread while `post(open)` has not yet returned, as an idle screen
        // that presents only at open does.
        swift.onPost = { envelope ->
            if (envelope.op == Op.open) {
                delivering = thread {
                    deliver(core, Envelope(1, Op.presentation, handle = Handle(1, 1), body = JsonPrimitive("first")))
                }
                delivering!!.join(200)
            }
        }
        val handle = core.open("idle").getOrThrow()
        delivering!!.join(5_000)
        assertEquals(JsonPrimitive("first"), handle.state.value.presentation)
        assertEquals(0L, core.failureCount)
    }

    @Test
    fun aStreamsOwnFailureCrossesAsItsCodeAndAValueSwiftRefusedIsDiscarded() = runTest {
        val swift = FakeSwift()
        val dispatcher = StandardTestDispatcher(testScheduler)
        val discarded = CopyOnWriteArrayList<JsonElement>()
        val core = core(swift) { request ->
            when (request) {
                DeviceRequest.ConnectivityWatch -> DeviceAnswer.Stream(dispatcher, flow {
                    emit(JsonPrimitive(1))
                    throw BridgeException(BridgeFailure(BridgeFailure.Code.overflow, "too fast"))
                })
                else -> DeviceAnswer.Value(dispatcher, discard = { discarded += it }) { JsonPrimitive("a file") }
            }
        }
        deliver(core, Envelope(1, Op.request, id = 1, body = request(DeviceRequest.ConnectivityWatch)))
        advanceUntilIdle()
        val error = swift.posted.last { it.id == 1L }
        assertEquals(Op.error, error.op)
        assertEquals(BridgeFailure.Code.overflow, BridgeJson.decodeFromJsonElement(BridgeFailure.serializer(), error.body!!).code)

        // Swift refuses the reply (its call was cancelled first): what it owned is this side's.
        swift.refuse = { it.op == Op.reply }
        deliver(core, Envelope(1, Op.request, id = 2, body = request(DeviceRequest.ConnectivityCurrent)))
        advanceUntilIdle()
        assertEquals(listOf<JsonElement>(JsonPrimitive("a file")), discarded.toList())
    }

    @Test
    fun aValueMadeAsItsCallIsCancelledIsDiscarded() = runTest {
        val swift = FakeSwift()
        val dispatcher = StandardTestDispatcher(testScheduler)
        val discarded = CopyOnWriteArrayList<JsonElement>()
        val made = CompletableDeferred<Unit>()
        val core = core(swift) {
            DeviceAnswer.Value(dispatcher, discard = { discarded += it }) {
                made.complete(Unit)
                JsonPrimitive("made")
            }
        }
        deliver(core, Envelope(1, Op.request, id = 5, body = request(DeviceRequest.ConnectivityCurrent)))
        // Cancelled between the block's return and the reply.
        testScheduler.runCurrent()
        deliver(core, Envelope(1, Op.cancel, id = 5))
        advanceUntilIdle()
        assertTrue(made.isCompleted)
        if (swift.posted.none { it.id == 5L && it.op == Op.reply }) {
            assertEquals(listOf<JsonElement>(JsonPrimitive("made")), discarded.toList())
        }
        assertEquals(0, core.runningCalls)
    }

    @Test
    fun anEnvironmentUpdateIsPosted() = runTest {
        val swift = FakeSwift()
        val core = core(swift)
        core.environment(EnvironmentUpdate("el-GR", "Europe/Athens")).getOrThrow()
        val posted = swift.posted.last()
        assertEquals(Op.environment, posted.op)
        assertEquals(EnvironmentUpdate("el-GR", "Europe/Athens"), BridgeJson.decodeFromJsonElement(EnvironmentUpdate.serializer(), posted.body!!))
    }
}
