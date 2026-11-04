package dev.reamde.substrate.kit.device

import java.util.concurrent.CopyOnWriteArrayList
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * A release's callback, matched to its attempt as the loopback listener matches a request: its own address and its page's `state`, and nothing else. What
 * matches no attempt ends none, and what matched once is over.
 */
class ClaimedCallbacksTest {
    private val redirect = "https://apps.example.com/apps/tasks/android/callback"

    /** An attempt's wait, keeping each query it was handed; the first ends it, as `BrowserSignIn`'s answer does. */
    private class Attempt {
        val taken = CopyOnWriteArrayList<Map<String, String>>()

        fun take(query: Map<String, String>): Boolean = taken.isEmpty() && taken.add(query)
    }

    private fun ClaimedCallbacks.waiting(state: String, at: String = redirect): Pair<Attempt, ClaimedCallbacks.Waiting> {
        val attempt = Attempt()
        return attempt to wait(at, state, attempt::take)
    }

    private fun refused(delivered: ClaimedCallbacks.Delivered) = delivered is ClaimedCallbacks.Delivered.Refused

    @Test
    fun theCallbackCarryingItsAttemptsStateEndsItWithTheQueryAsItCame() {
        val callbacks = ClaimedCallbacks()
        val (attempt, _) = callbacks.waiting("sé")
        val delivered = callbacks.deliver("$redirect?code=abc%2B1&state=s%C3%A9&iss=https%3A%2F%2Fsubstrate.example.com")
        assertEquals(ClaimedCallbacks.Delivered.Taken, delivered)
        assertEquals(listOf(mapOf("code" to "abc+1", "state" to "sé", "iss" to "https://substrate.example.com")), attempt.taken.toList())
    }

    @Test
    fun anotherStateOrNoneEndsNothingAndTheWaitGoesOn() {
        val callbacks = ClaimedCallbacks()
        val (attempt, _) = callbacks.waiting("s")
        for (uri in listOf("$redirect?code=stolen&state=other", "$redirect?code=stolen", "$redirect?code=stolen&state=", "$redirect?code=stolen&state=S")) {
            assertTrue(uri, refused(callbacks.deliver(uri)))
        }
        assertTrue(attempt.taken.isEmpty())
        assertEquals(ClaimedCallbacks.Delivered.Taken, callbacks.deliver("$redirect?code=abc&state=s"))
        assertEquals("abc", attempt.taken.single()["code"])
    }

    @Test
    fun aCallbackToAnotherAddressEndsNothing() {
        val callbacks = ClaimedCallbacks()
        val (attempt, _) = callbacks.waiting("s")
        for (uri in
            listOf(
                "http://apps.example.com/apps/tasks/android/callback?code=a&state=s",
                "https://elsewhere.example.com/apps/tasks/android/callback?code=a&state=s",
                "https://apps.example.com.evil.example/apps/tasks/android/callback?code=a&state=s",
                "https://apps.example.com:8443/apps/tasks/android/callback?code=a&state=s",
                "https://apps.example.com/apps/tasks/android/callback/more?code=a&state=s",
                "https://apps.example.com/apps/tasks/callback?code=a&state=s",
                "https://apps.example.com/apps/tasks/android/Callback?code=a&state=s",
            )) {
            assertTrue(uri, refused(callbacks.deliver(uri)))
        }
        assertTrue(attempt.taken.isEmpty())
        // A host is the same whatever its case, as every browser reads it.
        assertEquals(ClaimedCallbacks.Delivered.Taken, callbacks.deliver("https://Apps.Example.COM/apps/tasks/android/callback?code=a&state=s"))
    }

    @Test
    fun aCallbackThatCannotBeReadEndsNothing() {
        val callbacks = ClaimedCallbacks()
        val (attempt, _) = callbacks.waiting("s")
        for (uri in
            listOf(
                "$redirect?code=%&state=s",
                "$redirect?code=%zz&state=s",
                "$redirect?code=a&state=s&state=s",
                "$redirect?code=a b&state=s",
                "not an address at all",
            )) {
            assertTrue(uri, refused(callbacks.deliver(uri)))
        }
        assertTrue(attempt.taken.isEmpty())
        assertEquals(1, callbacks.count)
    }

    @Test
    fun eachAttemptTakesItsOwnCallbackOnceAndAForgottenOneNone() {
        val callbacks = ClaimedCallbacks()
        val (first, _) = callbacks.waiting("one")
        val (second, wait) = callbacks.waiting("two")
        assertEquals(ClaimedCallbacks.Delivered.Taken, callbacks.deliver("$redirect?code=b&state=two"))
        assertEquals(ClaimedCallbacks.Delivered.Taken, callbacks.deliver("$redirect?code=a&state=one"))
        assertEquals("a", first.taken.single()["code"])
        assertEquals("b", second.taken.single()["code"])
        // The same callback again is over: its attempt took the first.
        assertTrue(refused(callbacks.deliver("$redirect?code=b2&state=two")))
        assertEquals("b", second.taken.single()["code"])
        // An attempt that stopped waiting (it ended: its deadline, a cancel) takes nothing.
        callbacks.forget(wait)
        callbacks.forget(wait)
        val (third, _) = callbacks.waiting("three")
        callbacks.forget(callbacks.wait(redirect, "four") { error("a forgotten wait took a callback") })
        assertTrue(refused(callbacks.deliver("$redirect?code=d&state=four")))
        assertTrue(third.taken.isEmpty())
        assertEquals(2, callbacks.count)
    }
}
