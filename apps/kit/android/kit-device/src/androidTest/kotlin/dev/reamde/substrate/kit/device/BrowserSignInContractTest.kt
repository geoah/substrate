package dev.reamde.substrate.kit.device

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import dev.reamde.substrate.kit.BridgeCore
import dev.reamde.substrate.kit.bridge.BridgeFailure
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.BrowserAnswer
import dev.reamde.substrate.kit.bridge.BrowserPrepared
import dev.reamde.substrate.kit.bridge.BrowserRoute
import dev.reamde.substrate.kit.bridge.DeviceRequest
import dev.reamde.substrate.kit.bridge.Envelope
import dev.reamde.substrate.kit.bridge.Handle
import dev.reamde.substrate.kit.bridge.Op
import java.io.BufferedReader
import java.io.IOException
import java.io.InputStreamReader
import java.net.HttpURLConnection
import java.net.InetAddress
import java.net.NetworkInterface
import java.net.ServerSocket
import java.net.Socket
import java.net.URL
import java.util.Collections
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.CountDownLatch
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
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
 * The browser sign-in's half of the security parity list (app-kit.md §5.4). A local build's loopback listener is bound to `127.0.0.1` alone, answers only the
 * path Swift named, takes as the callback only the request carrying the attempt's `state` and hands its query back as it came (Swift checks `state` and `iss`),
 * survives whatever else a connection sends, gives up at the attempt's deadline, and is gone once the attempt ends. A release's claimed callback comes back
 * through `SignInCallback` alone, held to the attempt's address and `state` the same way, is refused where this app does not take it or Android has not
 * verified it, and is never answered through loopback. However an attempt ends, a cancel or its handle's close among them, it lets go of what it held without
 * Swift's `finish`.
 */
@RunWith(AndroidJUnit4::class)
class BrowserSignInContractTest {
    private val context: Context = InstrumentationRegistry.getInstrumentation().targetContext
    private val opened: MutableList<String> = Collections.synchronizedList(mutableListOf())
    private val browser = BrowserSignIn(context, { page: String -> opened.add(page) })
    private val page = "https://example.com/authorize?client_id=x&state=s%C3%A9"
    private val state = "s%C3%A9"

    /** The callback this test package's `SignInCallback` takes (src/androidTest/AndroidManifest.xml), which nobody verified for it. */
    private val callback = "https://example.com/kit/callback"

    /** A claimed route whose verification the test stands in for, opening no browser. */
    private fun claimed(limits: BrowserSignIn.Limits = BrowserSignIn.Limits()) =
        BrowserSignIn(context, { page: String -> opened.add(page) }, limits, verify = { null })

    /** [uri] as Android hands a verified App Link to this package: a VIEW that its `SignInCallback` takes, run to its end before this returns. */
    private fun follow(uri: String) {
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(uri)).addCategory(Intent.CATEGORY_BROWSABLE).setPackage(context.packageName)
        InstrumentationRegistry.getInstrumentation().startActivitySync(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        InstrumentationRegistry.getInstrumentation().waitForIdleSync()
    }

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
    fun aClaimedCallbackComesBackThroughItsActivityAsItCame() = runBlocking {
        val claimed = claimed()
        assertEquals(BrowserPrepared.Ready(callback), claimed.prepare(BrowserRoute.Claimed(callback)))
        val answer = async(Dispatchers.IO) { claimed.open(page, callback) }
        while (opened.isEmpty()) delay(10)
        assertEquals(listOf(page), opened)
        follow("$callback?code=abc%2B1&state=$state&iss=http%3A%2F%2F10.0.2.2%3A8098")
        val came = withTimeout(10_000) { answer.await() } as BrowserAnswer.Callback
        assertEquals(mapOf("code" to "abc+1", "state" to "sé", "iss" to "http://10.0.2.2:8098"), came.query)
        // The callback ended the attempt: nothing waits on it, and the same callback again ends nothing.
        assertEquals(0, claimed.attempting)
        assertEquals(0, BrowserSignIn.claimed.count)
        assertTrue(BrowserSignIn.claimed.deliver("$callback?code=again&state=$state") is ClaimedCallbacks.Delivered.Refused)
        claimed.finish(callback)
    }

    @Test
    fun aClaimedCallbackWithAnotherStateEndsNothingAndTheWaitGoesOn() = runBlocking {
        val claimed = claimed()
        claimed.prepare(BrowserRoute.Claimed(callback))
        val answer = async(Dispatchers.IO) { claimed.open(page, callback) }
        while (opened.isEmpty()) delay(10)
        follow("$callback?code=stolen&state=other")
        follow("$callback?code=stolen")
        follow("$callback?code=%zz&state=$state")
        assertTrue(answer.isActive)
        follow("$callback?code=abc&state=$state")
        assertEquals("abc", (withTimeout(10_000) { answer.await() } as BrowserAnswer.Callback).query["code"])
    }

    @Test
    fun aClaimedAttemptNoCallbackComesToExpiresAndALateOneEndsNothing() = runBlocking {
        val claimed = claimed(BrowserSignIn.Limits(attempt = 1_500))
        claimed.prepare(BrowserRoute.Claimed(callback))
        assertEquals(BrowserAnswer.Expired, withTimeout(10_000) { claimed.open(page, callback) })
        assertEquals(0, claimed.attempting)
        assertEquals(0, BrowserSignIn.claimed.count)
        follow("$callback?code=late&state=$state")
        assertTrue(claimed.open(page, callback) is BrowserAnswer.Failed)
    }

    /** A newer attempt on the same claimed callback ends the one before, which answers `cancelled`, and takes the callback its own page names. */
    @Test
    fun aNewerClaimedAttemptEndsTheOneBefore() = runBlocking {
        val claimed = claimed()
        claimed.prepare(BrowserRoute.Claimed(callback))
        val first = async(Dispatchers.IO) { claimed.open(page, callback) }
        while (opened.isEmpty()) delay(10)
        claimed.prepare(BrowserRoute.Claimed(callback))
        assertEquals(BrowserAnswer.Cancelled, withTimeout(10_000) { first.await() })
        val newer = "https://example.com/authorize?client_id=x&state=newer"
        val second = async(Dispatchers.IO) { claimed.open(newer, callback) }
        while (opened.size < 2) delay(10)
        follow("$callback?code=old&state=$state")
        assertTrue(second.isActive)
        follow("$callback?code=new&state=newer")
        assertEquals("new", (withTimeout(10_000) { second.await() } as BrowserAnswer.Callback).query["code"])
    }

    @Test
    fun aClaimedRouteThisAppDoesNotTakeOrAndroidHasNotVerifiedIsRefusedNeverAnsweredThroughLoopback() = runBlocking {
        for (url in listOf("https://example.com/elsewhere", "http://example.com/kit/callback", "https://example.com/kit/callback?x=1", "https://example.com")) {
            val prepared = browser.prepare(BrowserRoute.Claimed(url))
            assertTrue("$url: $prepared", prepared is BrowserPrepared.Failed)
        }
        // This package's own, which nobody verified for it: Android 12 and later say so; before them, the system's verification at install is all there is.
        val unverified = browser.prepare(BrowserRoute.Claimed(callback))
        if (Build.VERSION.SDK_INT >= 31) {
            assertTrue("$unverified", unverified is BrowserPrepared.Failed)
        } else {
            assertEquals(BrowserPrepared.Ready(callback), unverified)
        }
        browser.finish(callback)
        assertEquals(0, browser.attempting)
    }

    /** Cancelled while `prepare` runs, after it made its listener: the listener ends with the call, and nothing waits on `finish`. */
    @Test
    fun aCancelDuringPrepareLetsGoOfWhatItMade() = runBlocking {
        val made = CompletableDeferred<ServerSocket>()
        val go = CountDownLatch(1)
        val slow =
            BrowserSignIn(
                context,
                { page: String -> opened.add(page) },
                listen = { address ->
                    ServerSocket(0, 8, address).also {
                        made.complete(it)
                        go.await()
                    }
                },
            )
        val preparing = async(Dispatchers.IO) { slow.prepare(BrowserRoute.Loopback("Substrate Tasks", "/oauth/callback")) }
        val socket = withTimeout(10_000) { made.await() }
        preparing.cancel()
        go.countDown()
        preparing.join()
        assertTrue(preparing.isCancelled)
        assertTrue(socket.isClosed)
        assertEquals(0, slow.attempting)
    }

    /** Cancelled once the browser opened, on either route: the wait ends, and so does the attempt, its socket or its wait, without `finish`. */
    @Test
    fun aCancelAfterTheBrowserOpenedLetsGoOfTheAttempt() = runBlocking {
        for (route in listOf(BrowserRoute.Loopback("Substrate Tasks", "/oauth/callback"), BrowserRoute.Claimed(callback))) {
            opened.clear()
            val signIn = claimed()
            val redirect = (signIn.prepare(route) as BrowserPrepared.Ready).redirect
            val waiting = async(Dispatchers.IO) { signIn.open(page, redirect) }
            while (opened.isEmpty()) delay(10)
            waiting.cancel()
            waiting.join()
            assertEquals("$route", 0, signIn.attempting)
            assertEquals("$route", 0, BrowserSignIn.claimed.count)
            if (route is BrowserRoute.Loopback) assertFalse(listening(redirect)) else follow("$callback?code=late&state=$state")
            assertTrue(signIn.open(page, redirect) is BrowserAnswer.Failed)
        }
    }

    /**
     * Swift's side of a handle closed under a sign-in, as BridgeCore hears it: each call the handle waits on is cancelled, a reply after that is refused, and
     * no `finish` comes, since it would go through the handle that closed.
     */
    private class ClosingSwift {
        val posted = CopyOnWriteArrayList<Envelope>()

        @Volatile var closed = false

        fun post(text: String): String {
            val envelope = BridgeJson.decodeFromString(Envelope.serializer(), text)
            posted += envelope
            val reply =
                if (closed && (envelope.op == Op.reply || envelope.op == Op.error)) {
                    val failure = BridgeFailure(BridgeFailure.Code.unknownCall, "the handle closed")
                    Envelope(1, Op.error, body = BridgeJson.encodeToJsonElement(BridgeFailure.serializer(), failure))
                } else {
                    Envelope(1, Op.accepted)
                }
            return BridgeJson.encodeToString(Envelope.serializer(), reply)
        }
    }

    private val handle = Handle(1, 1)

    private fun request(id: Long, request: DeviceRequest) =
        BridgeJson.encodeToString(
            Envelope.serializer(),
            Envelope(1, Op.request, handle = handle, id = id, body = BridgeJson.encodeToJsonElement(DeviceRequest.Serializer, request)),
        )

    private fun cancel(id: Long) = BridgeJson.encodeToString(Envelope.serializer(), Envelope(1, Op.cancel, handle = handle, id = id))

    /** The redirect a `prepare` answered, whether or not Swift took it. */
    private fun prepared(swift: ClosingSwift, id: Long): String? =
        swift.posted
            .firstOrNull { it.op == Op.reply && it.id == id }
            ?.body
            ?.let { BridgeJson.decodeFromJsonElement(BrowserPrepared.Serializer, it) as? BrowserPrepared.Ready }
            ?.redirect

    /** A handle closed while `prepare` runs: closed before it made its listener answers, or before Swift took the answer, the listener ends either way. */
    @Test
    fun aHandleClosedDuringPrepareLetsGoOfWhatItMade() = runBlocking {
        for (whileRunning in listOf(true, false)) {
            val made = CompletableDeferred<ServerSocket>()
            val go = CountDownLatch(1)
            val signIn =
                BrowserSignIn(
                    context,
                    { page: String -> opened.add(page) },
                    listen = { address ->
                        ServerSocket(0, 8, address).also {
                            made.complete(it)
                            go.await()
                        }
                    },
                )
            val swift = ClosingSwift()
            val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
            val core = BridgeCore(swift::post, KitDevice.services(context, "Contract", signIn), scope)
            core.receive(request(1, DeviceRequest.BrowserPrepare(BrowserRoute.Loopback("Substrate Tasks", "/oauth/callback"))))
            val socket = withTimeout(10_000) { made.await() }
            swift.closed = true
            // The close's cancel, while the listener is being made; or none, and the answer is refused.
            if (whileRunning) core.receive(cancel(1))
            go.countDown()
            withTimeout(10_000) { while (core.runningCalls > 0 || signIn.attempting > 0 || !socket.isClosed) delay(10) }
            prepared(swift, 1)?.let { assertFalse("whileRunning: $whileRunning", listening(it)) }
            scope.cancel()
        }
    }

    /** A handle closed once the browser opened, on either route: its cancel ends the wait and the attempt, and no `finish` is needed. */
    @Test
    fun aHandleClosedAfterTheBrowserOpenedLetsGoOfTheAttempt() = runBlocking {
        for (route in listOf(BrowserRoute.Loopback("Substrate Tasks", "/oauth/callback"), BrowserRoute.Claimed(callback))) {
            opened.clear()
            val signIn = claimed()
            val swift = ClosingSwift()
            val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
            val core = BridgeCore(swift::post, KitDevice.services(context, "Contract", signIn), scope)
            core.receive(request(1, DeviceRequest.BrowserPrepare(route)))
            val redirect =
                withTimeout(10_000) {
                    while (prepared(swift, 1) == null) delay(10)
                    prepared(swift, 1)!!
                }
            core.receive(request(2, DeviceRequest.BrowserOpen(page, redirect)))
            withTimeout(10_000) { while (opened.isEmpty()) delay(10) }
            swift.closed = true
            core.receive(cancel(2))
            withTimeout(10_000) { while (core.runningCalls > 0 || signIn.attempting > 0) delay(10) }
            assertEquals("$route", 0, BrowserSignIn.claimed.count)
            if (route is BrowserRoute.Loopback) assertFalse(listening(redirect))
            // Swift took no answer to the open, and was sent none it could not take.
            assertTrue(swift.posted.none { it.id == 2L && it.op == Op.reply })
            scope.cancel()
        }
    }
}
