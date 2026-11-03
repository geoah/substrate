package dev.reamde.substrate.kit.device

import android.content.Context
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import dev.reamde.substrate.kit.bridge.BrowserAnswer
import dev.reamde.substrate.kit.bridge.BrowserPrepared
import dev.reamde.substrate.kit.bridge.BrowserRoute
import java.net.HttpURLConnection
import java.net.InetAddress
import java.net.NetworkInterface
import java.net.Socket
import java.net.URL
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The browser sign-in's half of the security parity list (app-kit.md §5.4):
 * a local build's loopback listener is bound to `127.0.0.1` alone, answers
 * only the path Swift named, hands the callback's query back as it came
 * (Swift checks `state` and `iss`), and is gone once finished; a release's
 * claimed route is refused, never answered through loopback.
 */
@RunWith(AndroidJUnit4::class)
class BrowserSignInContractTest {
    private val context: Context = InstrumentationRegistry.getInstrumentation().targetContext
    private val opened = mutableListOf<String>()
    private val browser = BrowserSignIn(context) { page -> opened.add(page) }

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
        val answer = async { browser.open("https://example.com/authorize", redirect) }
        while (opened.isEmpty()) kotlinx.coroutines.delay(10)
        assertEquals(listOf("https://example.com/authorize"), opened)
        // Another path is not the callback, and the wait goes on.
        assertEquals(404, get(redirect.replace("/oauth/callback", "/favicon.ico")).first)
        val (status, page) = get("$redirect?code=abc%2B1&state=s%C3%A9&iss=http%3A%2F%2F10.0.2.2%3A8098")
        assertEquals(200, status)
        assertTrue(page, page.contains("Substrate Tasks"))
        val callback = withTimeout(10_000) { answer.await() } as BrowserAnswer.Callback
        assertEquals(mapOf("code" to "abc+1", "state" to "sé", "iss" to "http://10.0.2.2:8098"), callback.query)
        browser.finish(redirect)
    }

    @Test
    fun theListenerIsOnTheLoopbackAddressAlone() = runBlocking {
        val redirect = ready()
        val port = URL(redirect).port
        val outside = NetworkInterface.getNetworkInterfaces().toList().flatMap { it.inetAddresses.toList() }
            .filter { !it.isLoopbackAddress && it is java.net.Inet4Address }
        for (address in outside) {
            try {
                Socket(address, port).close()
                fail("the listener answered on $address")
            } catch (_: Exception) {
            }
        }
        Socket(InetAddress.getByName("127.0.0.1"), port).close()
        browser.finish(redirect)
    }

    @Test
    fun aFinishedSignInListensNoLonger() = runBlocking {
        val redirect = ready()
        browser.finish(redirect)
        assertTrue(runCatching { Socket(InetAddress.getByName("127.0.0.1"), URL(redirect).port).close() }.isFailure)
        assertTrue(browser.open("https://example.com/authorize", redirect) is BrowserAnswer.Failed)
    }

    @Test
    fun aClaimedCallbackIsRefusedNeverAnsweredThroughLoopback() = runBlocking {
        val prepared = browser.prepare(BrowserRoute.Claimed("https://apps.example.com/tasks/callback"))
        assertTrue("$prepared", prepared is BrowserPrepared.Failed)
    }
}
