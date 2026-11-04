package dev.reamde.substrate.kit.device

import java.net.URI
import java.net.URISyntaxException
import java.net.URLDecoder
import java.security.MessageDigest
import java.util.concurrent.CopyOnWriteArrayList

/**
 * The release sign-ins waiting on their claimed callback, which Android hands [SignInCallback] from whichever browser followed it: one table for the process,
 * since the activity reaches no `BrowserSignIn` of its own. A callback is matched as the loopback listener matches a request: to the attempt whose address
 * (scheme, host, port and path) it is, carrying the `state` that attempt's page named, compared in constant time. One that matches none (an old page's, a
 * forged link, one that reached a process started fresh for it) ends nothing and every attempt goes on, so it never ends a sign-in or starts one; the first
 * that matches ends its attempt, its query otherwise unchecked, since Swift holds it to its request (`state`, `iss`, PKCE).
 */
internal class ClaimedCallbacks {
    /** One attempt's wait: the address its callback comes to, its page's `state`, and [take], which hands the query on and says whether it was the first. */
    class Waiting(val redirect: URI, val state: String, val take: (Map<String, String>) -> Boolean)

    /** What one callback came to. */
    sealed interface Delivered {
        /** It ended the attempt waiting on it. */
        data object Taken : Delivered

        /** It ended nothing, for [reason], which repeats nothing the address carried. */
        data class Refused(val reason: String) : Delivered
    }

    private val waiting = CopyOnWriteArrayList<Waiting>()

    /** Waits on the callback to [redirect], a claimed callback `prepare` held to what a release claims, carrying [state]. */
    fun wait(redirect: String, state: String, take: (Map<String, String>) -> Boolean): Waiting = Waiting(URI(redirect), state, take).also { waiting += it }

    /** Stops waiting; safe to call again. */
    fun forget(wait: Waiting) {
        waiting.remove(wait)
    }

    /** Hands [uri], the address Android started [SignInCallback] with, to the attempt it belongs to, if one waits on it. */
    fun deliver(uri: String): Delivered {
        val callback =
            try {
                URI(uri)
            } catch (_: URISyntaxException) {
                return Delivered.Refused("the callback is not an address")
            }
        val query =
            try {
                callbackQuery(callback.rawQuery ?: "")
            } catch (_: Malformed) {
                return Delivered.Refused("the callback's query cannot be read")
            }
        val came = query["state"]?.toByteArray(Charsets.UTF_8) ?: return Delivered.Refused("the callback names no state")
        val wait =
            waiting.firstOrNull { it.redirect.sameAddress(callback) && MessageDigest.isEqual(came, it.state.toByteArray(Charsets.UTF_8)) }
                ?: return Delivered.Refused("no sign-in waits on this callback")
        return if (wait.take(query)) Delivered.Taken else Delivered.Refused("this sign-in is over")
    }

    /** The attempts waiting now. */
    val count: Int
        get() = waiting.size

    /** The callback's address is this one's, as Apple's claimed callback is held to its redirect: the host's case aside, every part but the query. */
    private fun URI.sameAddress(other: URI): Boolean =
        scheme.equals(other.scheme, ignoreCase = true) && host != null && host.equals(other.host, ignoreCase = true) && port == other.port && path == other.path
}

/** A request or a callback that cannot be read: refused, and the attempt goes on. */
internal class Malformed(message: String) : Exception(message)

/** A callback's query, each pair decoded; a bad escape, or a name given twice, is [Malformed]. */
internal fun callbackQuery(text: String): Map<String, String> {
    val pairs = mutableMapOf<String, String>()
    for (pair in text.split('&').filter { it.isNotEmpty() }) {
        val name = decode(pair.substringBefore('='))
        if (pairs.put(name, decode(pair.substringAfter('=', ""))) != null) throw Malformed("$name is given twice")
    }
    return pairs
}

private fun decode(text: String): String =
    try {
        URLDecoder.decode(text, Charsets.UTF_8)
    } catch (failure: IllegalArgumentException) {
        throw Malformed("a bad escape: ${failure.message}")
    }
