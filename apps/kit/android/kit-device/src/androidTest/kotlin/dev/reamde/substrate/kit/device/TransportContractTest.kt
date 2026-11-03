package dev.reamde.substrate.kit.device

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import dev.reamde.substrate.kit.bridge.TransportChunk
import dev.reamde.substrate.kit.bridge.TransportOutcome
import dev.reamde.substrate.kit.bridge.TransportRequest
import dev.reamde.substrate.kit.BridgeCore
import dev.reamde.substrate.kit.bridge.BridgeFailure
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.DeviceRequest
import dev.reamde.substrate.kit.bridge.Envelope
import dev.reamde.substrate.kit.bridge.Op
import java.io.File
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import mockwebserver3.SocketEffect
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * No request follows a redirect, a streamed one included (app-kit.md §5.4);
 * a cancel reaches the socket while the head or the body is still coming;
 * and no body is left on disk, nor a credential answer ever written there.
 */
@RunWith(AndroidJUnit4::class)
class TransportContractTest {
    private val context = InstrumentationRegistry.getInstrumentation().targetContext
    private val server = MockWebServer()
    private val transport = Transport(context)

    @Before fun start() = server.start()

    @After fun stop() = server.close()

    private fun redirectThenLanding() {
        server.enqueue(MockResponse.Builder().code(302).addHeader("Location", server.url("/landed").toString()).build())
        server.enqueue(MockResponse.Builder().code(200).body("landed").build())
    }

    private fun get(path: String, credentials: Boolean = false) =
        TransportRequest(server.url(path).toString(), "GET", emptyList(), timeoutMillis = 30_000, credentials = credentials)

    private fun spilled(): List<String> = transport.folder.list().orEmpty().toList()

    /** Runs [body] until it is cancelled after [after] ms; how long the cancel took to land. */
    private fun cancelledAfter(after: Long, body: suspend () -> Unit): Long = runBlocking {
        val job = launch(Dispatchers.Default) { body() }
        delay(after)
        val began = System.nanoTime()
        job.cancelAndJoin()
        (System.nanoTime() - began) / 1_000_000
    }

    private val large = "x".repeat(Transport.INLINE_LIMIT * 4)

    @Test
    fun aFetchAnswersTheRedirectItself() = runBlocking {
        redirectThenLanding()
        val outcome = transport.fetch(get("/start"))
        assertTrue("$outcome", outcome is TransportOutcome.Response && outcome.response.status == 302L)
        assertEquals(1, server.requestCount)
    }

    @Test
    fun aLineStreamAnswersTheRedirectItself() = runBlocking {
        redirectThenLanding()
        val chunks = transport.lines(get("/stream")).toList()
        val head = chunks.first()
        assertTrue("$head", head is TransportChunk.Head && head.response.status == 302L)
        assertEquals(1, server.requestCount)
    }

    @Test
    fun aLargeBodyComesBackAsAFileAndASmallOneInline() = runBlocking {
        server.enqueue(MockResponse.Builder().code(200).body("x".repeat(Transport.INLINE_LIMIT + 1)).build())
        server.enqueue(MockResponse.Builder().code(200).body("small").build())
        val spilled = transport.fetch(get("/large")) as TransportOutcome.Response
        val small = transport.fetch(get("/small")) as TransportOutcome.Response
        assertTrue(spilled.response.bodyFile != null && spilled.response.body == null)
        assertEquals("c21hbGw=", small.response.body)
        File(spilled.response.bodyFile!!).delete()
        Unit
    }

    @Test
    fun aCancelEndsAFetchWaitingOnItsHead() {
        server.enqueue(MockResponse.Builder().onResponseStart(SocketEffect.Stall).build())
        val took = cancelledAfter(500) { transport.fetch(get("/stalled-head")) }
        assertTrue("the cancel took $took ms", took < 2_000)
    }

    @Test
    fun aCancelEndsAFetchReadingItsBodyAndLeavesNoFile() {
        server.enqueue(MockResponse.Builder().body(large).onResponseBody(SocketEffect.Stall).build())
        val took = cancelledAfter(1_000) { transport.fetch(get("/stalled-body")) }
        assertTrue("the cancel took $took ms", took < 2_000)
        assertEquals(emptyList<String>(), spilled())
    }

    @Test
    fun aCancelEndsALineStreamOnItsHeadOrItsBody() {
        server.enqueue(MockResponse.Builder().onResponseStart(SocketEffect.Stall).build())
        val head = cancelledAfter(500) { transport.lines(get("/stalled-head")).collect {} }
        assertTrue("the cancel took $head ms", head < 2_000)
        server.enqueue(MockResponse.Builder().body("one\ntwo\n" + large).onResponseBody(SocketEffect.Stall).build())
        val body = cancelledAfter(1_000) { transport.lines(get("/stalled-body")).collect {} }
        assertTrue("the cancel took $body ms", body < 2_000)
    }

    @Test
    fun aBodyCutShortLeavesNoFile() = runBlocking {
        server.enqueue(MockResponse.Builder().body(large).onResponseBody(SocketEffect.ShutdownConnection).build())
        val outcome = transport.fetch(get("/cut"))
        assertTrue("$outcome", outcome is TransportOutcome.Failed)
        assertEquals(emptyList<String>(), spilled())
    }

    @Test
    fun aCredentialAnswerStaysInlineAndIsRefusedPastItsBound() = runBlocking {
        server.enqueue(MockResponse.Builder().body(large).build())
        val token = transport.fetch(get("/oauth/token", credentials = true)) as TransportOutcome.Response
        assertTrue(token.response.bodyFile == null && token.response.body != null)
        server.enqueue(MockResponse.Builder().body("x".repeat(Transport.CREDENTIAL_LIMIT + 1)).build())
        val tooLarge = transport.fetch(get("/oauth/token", credentials = true))
        assertTrue("$tooLarge", tooLarge is TransportOutcome.Failed)
        assertEquals(emptyList<String>(), spilled())
    }

    @Test
    fun aSpilledAnswerSwiftNeverTookIsDeleted() = runBlocking {
        server.enqueue(MockResponse.Builder().body(large).build())
        val outcome = transport.fetch(get("/large")) as TransportOutcome.Response
        val file = File(outcome.response.bodyFile!!)
        assertTrue(file.path.startsWith(context.noBackupFilesDir.path) && file.exists())
        transport.discard(outcome)
        assertFalse(file.exists())
    }

    @Test
    fun aRefusedReplyThroughTheBridgeLeavesNoFile() = runBlocking {
        server.enqueue(MockResponse.Builder().body(large).build())
        // A Swift whose call was cancelled before the reply came: it refuses it.
        val core = BridgeCore(
            post = { text ->
                val envelope = BridgeJson.decodeFromString(Envelope.serializer(), text)
                val refused = BridgeFailure(BridgeFailure.Code.unknownCall, "nothing waits on it")
                BridgeJson.encodeToString(
                    Envelope.serializer(),
                    if (envelope.op == Op.reply) {
                        Envelope(1, Op.error, id = envelope.id, body = BridgeJson.encodeToJsonElement(BridgeFailure.serializer(), refused))
                    } else {
                        Envelope(1, Op.accepted)
                    },
                )
            },
            services = KitDevice.services(context),
            scope = CoroutineScope(Dispatchers.IO),
        )
        val request = BridgeJson.encodeToJsonElement(DeviceRequest.Serializer, DeviceRequest.TransportFetch(get("/large")))
        core.receive(BridgeJson.encodeToString(Envelope.serializer(), Envelope(1, Op.request, id = 1, body = request)))
        withTimeout(10_000) {
            while (core.runningCalls > 0 || server.requestCount == 0) delay(10)
        }
        assertEquals(emptyList<String>(), File(context.noBackupFilesDir, "bridge").list().orEmpty().toList())
    }

    @Test
    fun startingSweepsWhatADeadProcessLeft() {
        val folder = File(context.noBackupFilesDir, "bridge").apply { mkdirs() }
        File(folder, "response-left").writeText("records")
        val legacy = File(context.cacheDir, "bridge").apply { mkdirs() }
        File(legacy, "response-old").writeText("records")
        Transport(context)
        assertEquals(emptyList<String>(), folder.list().orEmpty().toList())
        assertFalse(legacy.exists())
    }
}
