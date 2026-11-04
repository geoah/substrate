package dev.reamde.substrate.kit.device

import android.content.Context
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import dev.reamde.substrate.kit.bridge.BrowserAnswer
import dev.reamde.substrate.kit.bridge.BrowserPrepared
import dev.reamde.substrate.kit.bridge.BrowserRoute
import java.io.BufferedReader
import java.io.IOException
import java.io.InputStreamReader
import java.net.HttpURLConnection
import java.net.InetAddress
import java.net.NetworkInterface
import java.net.Socket
import java.net.URL
import java.util.Collections
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The browser sign-in's half of the security parity list (app-kit.md §5.4): a local build's loopback listener is bound to `127.0.0.1` alone, answers only the
 * path Swift named, takes as the callback only the request carrying the attempt's `state` and hands its query back as it came (Swift checks `state` and `iss`),
 * survives whatever else a connection sends, gives up at the attempt's deadline, and is gone once the attempt ends; a release's claimed route is refused, never
 * answered through loopback.
 */
@RunWith(AndroidJUnit4::class)
class BrowserSignInContractTest {
    private val context: Context = InstrumentationRegistry.getInstrumentation().targetContext
    private val opened: MutableList<String> = Collections.synchronizedList(mutableListOf())
    private val browser = BrowserSignIn(context, { page: String -> opened.add(page) })
    private val page = "https://example.com/authorize?client_id=x&state=s%C3%A9"
    private val state = "s%C3%A9"

    /** [request] written raw to the listener on [port]: the status it answered, or -1 where the connection closed or reset first. */
    private fun raw(port: Int, request: String): Int =
        try {
            Socket(InetAddress.getByName("127.0.0.1"), port).use { socket ->
                socket.soTimeout = 5_000
                socket.getOutputStream().apply {
                    write(request.toByteArray(Charsets.ISO_8859_1))
                    flush()
                }
                val line = BufferedReader(InputStreamReader(socket.getInputStream(), Charsets.ISO_8859_1)).readLine()
                line?.split(' ')?.getOrNull(1)?.toIntOrNull() ?: -1
            }
        } catch (_: IOException) {
            -1
        }

    private fun port(redirect: String) = URL(redirect).port

    private fun listening(redirect: String) = runCatching { Socket(InetAddress.getByName("127.0.0.1"), port(redirect)).close() }.isSuccess

    private fun get(url: String): Pair<Int, String> {
        val connection = URL(url).openConnection() as HttpURLConnection
        connection.instanceFollowRedirects = false
        return try {
            connection.responseCode to (runCatching { connection.inputStream.readBytes().decodeToString() }.getOrNull() ?: "")
        } finally {
            connection.disconnect()
        }
    }

    private suspend fun ready(path: String = "/oauth/callback"): String =
        (browser.prepare(BrowserRoute.Loopback("Substrate Tasks", path)) as BrowserPrepared.Ready).redirect

    @Test
    fun theCallbacksQueryComesBackAsItCame() = runBlocking {
        val redirect = ready()
        assertTrue(redirect, redirect.matches(Regex("""http://127\.0\.0\.1:\d+/oauth/callback""")))
        val answer = async(Dispatchers.IO) { browser.open(page, redirect) }
        while (opened.isEmpty()) delay(10)
        assertEquals(listOf(page), opened)
        // Another path is not the callback, and the wait goes on.
        assertEquals(404, get(redirect.replace("/oauth/callback", "/favicon.ico")).first)
        val (status, body) = get("$redirect?code=abc%2B1&state=$state&iss=http%3A%2F%2F10.0.2.2%3A8098")
        assertEquals(200, status)
        assertTrue(body, body.contains("Substrate Tasks"))
        val callback = withTimeout(10_000) { answer.await() } as BrowserAnswer.Callback
        assertEquals(mapOf("code" to "abc+1", "state" to "sé", "iss" to "http://10.0.2.2:8098"), callback.query)
        // The callback ends the attempt: nothing listens after it, finished or not.
        assertFalse(listening(redirect))
        browser.finish(redirect)
    }

    @Test
    fun aRequestWithAnotherStateIsRefusedAndTheWaitGoesOn() = runBlocking {
        val redirect = ready()
        val answer = async(Dispatchers.IO) { browser.open(page, redirect) }
        while (opened.isEmpty()) delay(10)
        assertEquals(400, get("$redirect?code=stolen&state=other").first)
        assertEquals(400, get("$redirect?code=stolen").first)
        assertTrue(answer.isActive)
        assertEquals(200, get("$redirect?code=abc&state=$state").first)
        assertEquals("abc", (withTimeout(10_000) { answer.await() } as BrowserAnswer.Callback).query["code"])
        browser.finish(redirect)
    }

    @Test
    fun aMalformedRequestIsA400AndTheWaitGoesOn() = runBlocking {
        val redirect = ready()
        val answer = async(Dispatchers.IO) { browser.open(page, redirect) }
        while (opened.isEmpty()) delay(10)
        val port = port(redirect)
        for (request in
            listOf(
                "GET /oauth/callback?code=%&state=$state HTTP/1.1\r\n\r\n",
                "GET /oauth/callback?code=%zz&state=$state HTTP/1.1\r\n\r\n",
                "GET /oauth/callback?code=a&state=$state&state=$state HTTP/1.1\r\n\r\n",
                "POST /oauth/callback?code=a&state=$state HTTP/1.1\r\n\r\n",
                "HELLO\r\n\r\n",
                "GET http://127.0.0.1/oauth/callback?code=a&state=$state HTTP/1.1\r\n\r\n",
            )) {
            assertEquals(request, 400, raw(port, request))
        }
        assertTrue(answer.isActive)
        assertEquals(200, get("$redirect?code=abc&state=$state").first)
        assertEquals("abc", (withTimeout(10_000) { answer.await() } as BrowserAnswer.Callback).query["code"])
        browser.finish(redirect)
    }

    /**
     * A line or a head past its bound is refused once the bound is read; the rest is never read, so the refusal may arrive as a reset. Either way the attempt
     * goes on.
     */
    @Test
    fun anOversizedLineOrHeadIsRefusedAndTheWaitGoesOn() = runBlocking {
        val redirect = ready()
        val answer = async(Dispatchers.IO) { browser.open(page, redirect) }
        while (opened.isEmpty()) delay(10)
        val port = port(redirect)
        val long = "GET /oauth/callback?code=${"a".repeat(64 * 1024)}&state=$state HTTP/1.1\r\n\r\n"
        assertTrue(raw(port, long) in setOf(400, -1))
        val noisy = "GET /oauth/callback?code=a&state=$state HTTP/1.1\r\n" + (1..200).joinToString("") { "X-$it: y\r\n" } + "\r\n"
        assertTrue(raw(port, noisy) in setOf(400, -1))
        val heavy = "GET /oauth/callback?code=a&state=$state HTTP/1.1\r\n" + (1..10).joinToString("") { "X-$it: ${"y".repeat(4000)}\r\n" } + "\r\n"
        assertTrue(raw(port, heavy) in setOf(400, -1))
        assertTrue(answer.isActive)
        assertEquals(200, get("$redirect?code=abc&state=$state").first)
        assertEquals("abc", (withTimeout(10_000) { answer.await() } as BrowserAnswer.Callback).query["code"])
        browser.finish(redirect)
    }

    @Test
    fun aResetConnectionEndsItselfAlone() = runBlocking {
        val redirect = ready()
        val answer = async(Dispatchers.IO) { browser.open(page, redirect) }
        while (opened.isEmpty()) delay(10)
        repeat(3) {
            Socket(InetAddress.getByName("127.0.0.1"), port(redirect)).apply {
                setSoLinger(true, 0)
                getOutputStream().write("GET /oauth/cal".toByteArray())
                close()
            }
        }
        Socket(InetAddress.getByName("127.0.0.1"), port(redirect)).apply {
            setSoLinger(true, 0)
            close()
        }
        assertTrue(answer.isActive)
        assertEquals(200, get("$redirect?code=abc&state=$state").first)
        assertEquals("abc", (withTimeout(10_000) { answer.await() } as BrowserAnswer.Callback).query["code"])
        browser.finish(redirect)
    }

    /** A head that trickles in is cut at the connection's deadline, however often a byte arrives, and holds no other connection up meanwhile. */
    @Test
    fun aTricklingConnectionIsCutAtItsDeadline() = runBlocking {
        val quick = BrowserSignIn(context, { page: String -> opened.add(page) }, BrowserSignIn.Limits(connection = 1_000))
        val redirect = (quick.prepare(BrowserRoute.Loopback("Substrate Tasks", "/oauth/callback")) as BrowserPrepared.Ready).redirect
        val answer = async(Dispatchers.IO) { quick.open(page, redirect) }
        while (opened.isEmpty()) delay(10)
        val trickled =
            async(Dispatchers.IO) {
                val started = System.nanoTime()
                Socket(InetAddress.getByName("127.0.0.1"), port(redirect)).use { socket ->
                    val out = socket.getOutputStream()
                    try {
                        for (byte in "GET /oauth/callback?code=a&state=$state HTTP/1.1\r\nHost: x\r\n".toByteArray()) {
                            out.write(byte.toInt())
                            out.flush()
                            Thread.sleep(150)
                        }
                    } catch (_: IOException) {}
                }
                (System.nanoTime() - started) / 1_000_000
            }
        delay(200)
        // Another connection is answered while the trickle is still coming.
        assertEquals(404, get(redirect.replace("/oauth/callback", "/favicon.ico")).first)
        val took = withTimeout(10_000) { trickled.await() }
        assertTrue("the trickle went on for $took ms", took < 3_000)
        assertTrue(answer.isActive)
        assertEquals(200, get("$redirect?code=abc&state=$state").first)
        assertEquals("abc", (withTimeout(10_000) { answer.await() } as BrowserAnswer.Callback).query["code"])
        quick.finish(redirect)
    }

    @Test
    fun anAttemptNoCallbackComesToExpiresAndStopsListening() = runBlocking {
        val quick = BrowserSignIn(context, { page: String -> opened.add(page) }, BrowserSignIn.Limits(attempt = 1_500))
        val redirect = (quick.prepare(BrowserRoute.Loopback("Substrate Tasks", "/oauth/callback")) as BrowserPrepared.Ready).redirect
        val answer = async(Dispatchers.IO) { quick.open(page, redirect) }
        // A connection that never writes does not hold the deadline back.
        val silent = Socket(InetAddress.getByName("127.0.0.1"), port(redirect))
        assertEquals(BrowserAnswer.Expired, withTimeout(10_000) { answer.await() })
        assertTrue(silent.getInputStream().runCatching { read() }.let { it.isFailure || it.getOrNull() == -1 })
        silent.close()
        assertFalse(listening(redirect))
        quick.finish(redirect)
    }

    @Test
    fun aPageWithoutAStateIsNotOpened() = runBlocking {
        val redirect = ready()
        assertTrue(browser.open("https://example.com/authorize", redirect) is BrowserAnswer.Failed)
        assertTrue(opened.isEmpty())
        browser.finish(redirect)
    }

    @Test
    fun theListenerIsOnTheLoopbackAddressAlone() = runBlocking {
        val redirect = ready()
        val port = URL(redirect).port
        val outside =
            NetworkInterface.getNetworkInterfaces()
                .toList()
                .flatMap { it.inetAddresses.toList() }
                .filter { !it.isLoopbackAddress && it is java.net.Inet4Address }
        for (address in outside) {
            try {
                Socket(address, port).close()
                fail("the listener answered on $address")
            } catch (_: Exception) {}
        }
        Socket(InetAddress.getByName("127.0.0.1"), port).close()
        browser.finish(redirect)
    }

    @Test
    fun aFinishedSignInListensNoLonger() = runBlocking {
        val redirect = ready()
        browser.finish(redirect)
        assertTrue(runCatching { Socket(InetAddress.getByName("127.0.0.1"), URL(redirect).port).close() }.isFailure)
        assertTrue(browser.open(page, redirect) is BrowserAnswer.Failed)
    }

    @Test
    fun aClaimedCallbackIsRefusedNeverAnsweredThroughLoopback() = runBlocking {
        val prepared = browser.prepare(BrowserRoute.Claimed("https://apps.example.com/tasks/callback"))
        assertTrue("$prepared", prepared is BrowserPrepared.Failed)
    }
}
