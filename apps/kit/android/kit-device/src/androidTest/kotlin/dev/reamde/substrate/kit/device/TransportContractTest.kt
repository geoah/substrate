package dev.reamde.substrate.kit.device

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import dev.reamde.substrate.kit.bridge.TransportChunk
import dev.reamde.substrate.kit.bridge.TransportOutcome
import dev.reamde.substrate.kit.bridge.TransportRequest
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.runBlocking
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/** No request follows a redirect, a streamed one included (app-kit.md §5.4). */
@RunWith(AndroidJUnit4::class)
class TransportContractTest {
    private val server = MockWebServer()
    private val transport = Transport(InstrumentationRegistry.getInstrumentation().targetContext)

    @Before fun start() = server.start()

    @After fun stop() = server.close()

    private fun redirectThenLanding() {
        server.enqueue(MockResponse.Builder().code(302).addHeader("Location", server.url("/landed").toString()).build())
        server.enqueue(MockResponse.Builder().code(200).body("landed").build())
    }

    private fun get(path: String) = TransportRequest(server.url(path).toString(), "GET", emptyList(), timeoutMillis = 5_000)

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
        val large = transport.fetch(get("/large")) as TransportOutcome.Response
        val small = transport.fetch(get("/small")) as TransportOutcome.Response
        assertTrue(large.response.bodyFile != null && large.response.body == null)
        assertEquals("c21hbGw=", small.response.body)
        java.io.File(large.response.bodyFile!!).delete()
        Unit
    }
}
