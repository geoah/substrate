package dev.reamde.substrate.kit.device

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.verify.domain.DomainVerificationManager
import android.content.pm.verify.domain.DomainVerificationUserState
import android.net.Uri
import android.os.Build
import android.os.SystemClock
import dev.reamde.substrate.kit.bridge.BrowserAnswer
import dev.reamde.substrate.kit.bridge.BrowserPrepared
import dev.reamde.substrate.kit.bridge.BrowserRoute
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.net.SocketTimeoutException
import java.net.URI
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Semaphore
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull

/**
 * The owner's browser on the sign-in page, and the callback it comes back with (docs/plans/app-kit.md §5.4), on one of two routes. A local build's loopback
 * route listens on `127.0.0.1` alone, on a port the system picks, for the one path Swift named; the callback is the request carrying the attempt's `state`,
 * every request is bounded in size and time, and one it cannot read is answered 400 without ending the attempt. A release's claimed route is the https callback
 * it claims: Android hands it to [SignInCallback], which only a release's manifest declares, once the system has verified the site's `assetlinks.json` names
 * the app, and a callback Android has not verified for this app is refused, never answered through loopback. Either way the query is handed back otherwise
 * unchecked, since Swift holds it to its request (`state`, `iss`, PKCE).
 *
 * An attempt lives from `prepare` until the first of its callback, its deadline, a cancel, `finish`, or a newer attempt on the same claimed callback, and lets
 * go of what it holds as it ends: a cancel, or a handle closed under it, needs nothing more from Swift. The page opens in the owner's default browser, as on a
 * Mac, so the connect screen offers its own Cancel. While the browser is in front, `SignInWait` keeps the process from being frozen, which would leave a
 * loopback callback unanswered, or killed, which would lose the attempt a claimed callback ends. The page a loopback callback lands on tells the person to go
 * back to the app and links to it, and the app is brought forward where Android lets it; a claimed callback's activity brings the app forward itself.
 */
public class BrowserSignIn
internal constructor(
    context: Context,
    private val launch: ((String) -> Boolean)?,
    private val limits: Limits = Limits(),
    private val verify: ((String) -> String?)? = null,
    private val listen: (InetAddress) -> ServerSocket = { ServerSocket(0, 8, it) },
) {
    public constructor(context: Context) : this(context, null)

    private val app = context.applicationContext
    private val attempts = ConcurrentHashMap<String, Attempt>()

    /**
     * What one attempt may take. [attempt] is the server's: an authorization request lives ten minutes, so no callback after that could be exchanged.
     * [connection] is the whole request head's, however slowly it trickles in, and [connections] how many are read at once: a browser opens a preconnection it
     * may never write to, so one at a time would hold the callback behind it.
     */
    internal data class Limits(
        val attempt: Long = 10 * 60_000L,
        val connection: Long = 10_000L,
        val connections: Int = 4,
        val line: Int = 8 * 1024,
        val headers: Int = 64,
        val head: Int = 32 * 1024,
    )

    /** One sign-in, from `prepare` until [end]: the redirect it was prepared for, when it gives up, and how it ended. */
    private sealed class Attempt(val redirect: String, val deadline: Long) {
        val answer = CompletableDeferred<BrowserAnswer>()
        val opened = AtomicBoolean(false)

        /** Lets go of what the attempt holds; safe to call again. */
        abstract fun close()
    }

    /** A loopback listener: its socket, the path it answers, the `state` its page named once `open` gave it, and every connection still open. */
    private class Listener(val socket: ServerSocket, val path: String, val appName: String, redirect: String, deadline: Long, limits: Limits) :
        Attempt(redirect, deadline) {
        val state = AtomicReference<String?>(null)
        val called = AtomicBoolean(false)
        val slots = Semaphore(limits.connections)
        val connections: MutableSet<Socket> = ConcurrentHashMap.newKeySet()

        override fun close() {
            runCatching { socket.close() }
            connections.forEach { runCatching { it.close() } }
        }
    }

    /** A claimed callback's attempt: its wait in [claimed], from `open` on. */
    private class Claimed(redirect: String, deadline: Long) : Attempt(redirect, deadline) {
        @Volatile var waiting: ClaimedCallbacks.Waiting? = null

        override fun close() {
            waiting?.let(claimed::forget)
        }
    }

    public suspend fun prepare(route: BrowserRoute): BrowserPrepared {
        // What a cancel or a throw lands on before Swift hears of it is let go of here: nothing else will name it.
        var made: Attempt? = null
        try {
            return withContext(Dispatchers.IO) {
                when (route) {
                    is BrowserRoute.Claimed -> {
                        (verify ?: ::claimProblem)(route.url)?.let {
                            return@withContext BrowserPrepared.Failed(it)
                        }
                        val attempt = Claimed(route.url, SystemClock.elapsedRealtime() + limits.attempt)
                        made = attempt
                        // One attempt at a time waits on a claimed callback: the one before, whose `finish` was lost with its handle, ends here.
                        attempts.put(route.url, attempt)?.let(::end)
                        BrowserPrepared.Ready(route.url)
                    }
                    is BrowserRoute.Loopback -> {
                        if (!route.path.startsWith("/")) return@withContext BrowserPrepared.Failed("The sign-in's path ${route.path} is not a path.")
                        val socket =
                            try {
                                listen(InetAddress.getByName(LOOPBACK))
                            } catch (failure: Exception) {
                                return@withContext BrowserPrepared.Failed("The sign-in could not listen on this device: ${failure.message}")
                            }
                        val redirect = "http://$LOOPBACK:${socket.localPort}${route.path}"
                        val listener = Listener(socket, route.path, route.appName, redirect, SystemClock.elapsedRealtime() + limits.attempt, limits)
                        made = listener
                        attempts[redirect] = listener
                        // Listening from here, so a listener no `open` follows still ends at its deadline.
                        Thread({ accept(listener) }, "substrate-signin").apply { isDaemon = true }.start()
                        BrowserPrepared.Ready(redirect)
                    }
                }
            }
        } catch (failure: Throwable) {
            made?.let(::end)
            throw failure
        }
    }

    /**
     * Opens [page] and waits for the callback on [redirect], until the attempt's deadline, which answers `expired`. The callback carries the `state` [page]
     * names, which the server echoes; one with another `state` is refused and the wait goes on, so a stray request or link cannot end the attempt. Swift still
     * checks `state`, `iss` and PKCE. However the wait ends (the callback, the deadline, a cancelled caller), the attempt ends with it, so nothing waits on
     * `finish` to let go of it.
     */
    public suspend fun open(page: String, redirect: String): BrowserAnswer {
        val attempt = attempts[redirect] ?: return BrowserAnswer.Failed("The sign-in was not ready to come back. Try again.")
        val state =
            Uri.parse(page).getQueryParameter("state")?.takeIf { it.isNotEmpty() }
                ?: return BrowserAnswer.Failed("The sign-in page names no state to hold its callback to.")
        if (!attempt.opened.compareAndSet(false, true)) return BrowserAnswer.Failed("This sign-in is already open in the browser.")
        try {
            when (attempt) {
                is Listener -> attempt.state.set(state)
                is Claimed -> attempt.waiting = claimed.wait(redirect, state) { query -> attempt.answer.complete(BrowserAnswer.Callback(query)) }
            }
            // Ended as it was found (its deadline, a newer attempt): no browser to open.
            if (attempt.answer.isCompleted) return attempt.answer.await()
            if (launch == null) SignInWait.start(ForegroundActivity.current ?: app)
            val opened = launch?.invoke(page) ?: withContext(Dispatchers.Main) { openBrowser(page) }
            if (!opened) return BrowserAnswer.Failed("No browser on this device could open the sign-in page.")
            val answer = withTimeoutOrNull(attempt.deadline - SystemClock.elapsedRealtime()) { attempt.answer.await() } ?: BrowserAnswer.Expired
            // A claimed callback's activity brought the app forward itself.
            if (launch == null && attempt is Listener && answer is BrowserAnswer.Callback) withContext(Dispatchers.Main) { bringAppForward() }
            return answer
        } finally {
            if (launch == null) SignInWait.stop(app)
            end(attempt)
        }
    }

    /**
     * Lets go of what `prepare` took, whatever `open` came to. `KitDevice` calls it as Swift's request arrives, before any request after it: a claimed
     * callback's `redirect` is every attempt's on it, so a finish acted on later could end the attempt a later `prepare` made there.
     */
    public fun finish(redirect: String) {
        attempts[redirect]?.let(::end)
    }

    /**
     * Lets go of a loopback route Swift never took (its `prepare` was cancelled, or its handle closed, before the answer reached it), whose socket would
     * otherwise wait out its deadline. A claimed one holds nothing until it is opened, and the next `prepare` on its callback replaces it.
     */
    internal fun discard(redirect: String) {
        (attempts[redirect] as? Listener)?.let(::end)
    }

    /** The attempts not ended yet. */
    internal val attempting: Int
        get() = attempts.size

    /**
     * Ends [attempt] however it stands, once: a wait still going is answered `cancelled`, its socket or its wait is let go of, and it is forgotten. Every way
     * an attempt ends comes here, and every call after the first changes nothing.
     */
    private fun end(attempt: Attempt) {
        attempts.remove(attempt.redirect, attempt)
        attempt.answer.complete(BrowserAnswer.Cancelled)
        attempt.close()
    }

    /**
     * Why this app cannot take the callback Android hands for [url], or null where it can: [url] is a callback a release may claim, this app's [SignInCallback]
     * takes it (a release's manifest declares it for the stamped callback alone), and from Android 12 on the system has verified its host against the site's
     * `assetlinks.json` and the owner lets the app open it. A failed verification is a failure, never a fallback (§5.4); before Android 12 the system verifies
     * at install and says nothing an app can read.
     */
    private fun claimProblem(url: String): String? {
        val host = claimedHost(url) ?: return "The sign-in's callback ($url) is not an https address with a host and a path."
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url)).addCategory(Intent.CATEGORY_BROWSABLE).setPackage(app.packageName)
        if (taker(intent) != SignInCallback::class.java.name) return "This build does not take its sign-in's callback ($url), so it cannot sign in."
        if (Build.VERSION.SDK_INT < 31) return null
        val state =
            runCatching { app.getSystemService(DomainVerificationManager::class.java)?.getDomainVerificationUserState(app.packageName) }.getOrNull()
                ?: return "Android could not say whether $host is verified for this app, so your browser cannot hand the sign-in back to it."
        if (!state.isLinkHandlingAllowed) {
            return "Opening supported links is turned off for this app in Settings, so your browser cannot hand the sign-in back to it."
        }
        if (state.hostToStateMap[host] != DomainVerificationUserState.DOMAIN_STATE_VERIFIED) {
            return "Android has not verified $host for this app, so your browser cannot hand the sign-in back to it."
        }
        return null
    }

    /** [url]'s host where it is a callback a release may claim: https, with a host and a path, and no query, fragment or user, as Swift's `AppManifest`. */
    private fun claimedHost(url: String): String? {
        val uri = runCatching { URI(url) }.getOrNull() ?: return null
        if (!uri.scheme.equals("https", ignoreCase = true) || uri.host.isNullOrEmpty() || (uri.path?.length ?: 0) <= 1) return null
        if (uri.rawQuery != null || uri.rawFragment != null || uri.rawUserInfo != null) return null
        return uri.host.lowercase()
    }

    /** The activity of this app that [intent] reaches, by class name. */
    private fun taker(intent: Intent): String? =
        if (Build.VERSION.SDK_INT >= 33) {
            app.packageManager.resolveActivity(intent, PackageManager.ResolveInfoFlags.of(0))?.activityInfo?.name
        } else {
            @Suppress("DEPRECATION") app.packageManager.resolveActivity(intent, 0)?.activityInfo?.name
        }

    private fun openBrowser(page: String): Boolean {
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(page)).addCategory(Intent.CATEGORY_BROWSABLE)
        val from = ForegroundActivity.current
        return try {
            if (from != null) from.startActivity(intent) else app.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            true
        } catch (_: ActivityNotFoundException) {
            false
        }
    }

    private fun bringAppForward() {
        val launch = app.packageManager.getLaunchIntentForPackage(app.packageName) ?: return
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_REORDER_TO_FRONT)
        runCatching { app.startActivity(launch) }
    }

    /**
     * Takes connections until the attempt ends: the callback, the deadline, the attempt ended under it (its socket closed), or a failure, then ends the
     * attempt. Each connection is read on its own thread, at most `limits.connections` at once, so a silent one holds no other up. Only the loopback address is
     * bound, so nothing off the device reaches it.
     */
    private fun accept(listener: Listener) {
        try {
            while (!listener.answer.isCompleted) {
                val remaining = listener.deadline - SystemClock.elapsedRealtime()
                if (remaining <= 0) {
                    listener.answer.complete(BrowserAnswer.Expired)
                    break
                }
                if (!listener.slots.tryAcquire(remaining, TimeUnit.MILLISECONDS)) continue
                val connection =
                    try {
                        listener.socket.soTimeout = remaining.coerceIn(1, Int.MAX_VALUE.toLong()).toInt()
                        listener.socket.accept()
                    } catch (_: SocketTimeoutException) {
                        listener.slots.release()
                        continue
                    } catch (failure: IOException) {
                        listener.slots.release()
                        // Ended under it, or the socket failed under it.
                        listener.answer.complete(BrowserAnswer.Failed("The sign-in stopped listening: ${failure.message}"))
                        break
                    }
                listener.connections += connection
                val until = minOf(SystemClock.elapsedRealtime() + limits.connection, listener.deadline)
                Thread({ serve(connection, listener, until) }, "substrate-signin-request").apply { isDaemon = true }.start()
            }
        } catch (failure: Throwable) {
            listener.answer.complete(BrowserAnswer.Failed("The sign-in stopped listening: ${failure.message}"))
        } finally {
            end(listener)
        }
    }

    /**
     * Answers one connection, whatever it sends: the expected path with this attempt's `state` is the callback; another path (a favicon, a prefetch) a 404; a
     * request that cannot be read, or the path with another `state` or before `open` named one, a 400. Only the callback ends the attempt. A reset, a timeout
     * or anything else thrown here ends this connection alone.
     */
    private fun serve(connection: Socket, listener: Listener, until: Long) {
        try {
            val target =
                try {
                    requestTarget(connection, until) ?: return
                } catch (refused: Malformed) {
                    return respond(connection, 400, "This is not a sign-in.")
                }
            val path = target.substringBefore('?')
            if (path != listener.path) return respond(connection, 404, "This is not a sign-in.")
            val query =
                try {
                    callbackQuery(target.substringAfter('?', ""))
                } catch (refused: Malformed) {
                    return respond(connection, 400, "This is not a sign-in.")
                }
            val came = query["state"]?.toByteArray(Charsets.UTF_8)
            val state = listener.state.get()?.toByteArray(Charsets.UTF_8)
            if (came == null || state == null || !MessageDigest.isEqual(came, state)) {
                return respond(connection, 400, "This is not the sign-in ${listener.appName} is waiting for.")
            }
            if (!listener.called.compareAndSet(false, true)) return respond(connection, 400, "This sign-in is over.")
            respond(connection, 200, "You can go back to ${listener.appName}.", listener.appName)
            listener.answer.complete(BrowserAnswer.Callback(query))
            // The page is written: the listener stops taking connections.
            runCatching { listener.socket.close() }
        } catch (_: Exception) {} finally {
            listener.connections -= connection
            runCatching { connection.close() }
            listener.slots.release()
        }
    }

    /**
     * The request line's target, once the head is read to its blank line: null where the connection closed before a request line, [Malformed] where the head
     * breaks a limit or is not `GET <target> HTTP/1.x`. Every read is held to [until], so a head that trickles in a byte at a time still ends then.
     */
    private fun requestTarget(connection: Socket, until: Long): String? {
        val head = HeadReader(connection, until, limits)
        val line = head.line() ?: return null
        var headers = 0
        while (true) {
            val header = head.line() ?: throw Malformed("the head ended before its blank line")
            if (header.isEmpty()) break
            if (++headers > limits.headers) throw Malformed("too many headers")
        }
        val parts = line.split(' ')
        if (parts.size != 3 || parts[0] != "GET" || !parts[2].startsWith("HTTP/1.") || !parts[1].startsWith("/")) {
            throw Malformed("not a GET request line")
        }
        return parts[1]
    }

    /** Reads a request head's lines, each bounded, the whole head bounded, every socket read given only what is left until the connection's deadline. */
    private class HeadReader(private val connection: Socket, private val until: Long, private val limits: Limits) {
        private val input = connection.getInputStream()
        private val buffer = ByteArray(1024)
        private var start = 0
        private var end = 0
        private var read = 0

        /** The next line without its CRLF, or null at the end of the stream before any byte of it. */
        fun line(): String? {
            val line = ByteArrayOutputStream()
            while (true) {
                if (start == end && !fill()) {
                    if (line.size() == 0) return null
                    throw Malformed("the head ended inside a line")
                }
                val byte = buffer[start++].toInt()
                if (++read > limits.head) throw Malformed("the head is too long")
                if (byte == '\n'.code) {
                    val bytes = line.toByteArray()
                    val size = if (bytes.lastOrNull() == '\r'.code.toByte()) bytes.size - 1 else bytes.size
                    return String(bytes, 0, size, Charsets.ISO_8859_1)
                }
                if (line.size() >= limits.line) throw Malformed("a line is too long")
                line.write(byte)
            }
        }

        private fun fill(): Boolean {
            val remaining = until - SystemClock.elapsedRealtime()
            if (remaining <= 0) throw SocketTimeoutException("the request took too long")
            connection.soTimeout = remaining.coerceIn(1, Int.MAX_VALUE.toLong()).toInt()
            val count = input.read(buffer)
            if (count <= 0) return false
            start = 0
            end = count
            return true
        }
    }

    private fun respond(connection: Socket, status: Int, message: String, appName: String? = null) {
        val back =
            appName?.let {
                val intent = ReturnToApp.link(app.packageName)
                "<p><a href=\"${escape(intent)}\" style=\"color:#0a84ff\">Return to ${escape(it)}</a></p>"
            } ?: ""
        val body =
            """
            <!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
            <body style="font:17px system-ui,sans-serif;display:flex;flex-direction:column;align-items:center;justify-content:center;height:90vh;color:#444">
            <p>${escape(message)}</p>$back</body></html>
        """
                .trimIndent()
                .toByteArray(Charsets.UTF_8)
        val reason =
            when (status) {
                200 -> "OK"
                404 -> "Not Found"
                else -> "Bad Request"
            }
        val head =
            "HTTP/1.1 $status $reason\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: ${body.size}\r\n" +
                "Cache-Control: no-store\r\nConnection: close\r\n\r\n"
        runCatching {
            connection.getOutputStream().apply {
                write(head.toByteArray(Charsets.ISO_8859_1))
                write(body)
                flush()
            }
        }
    }

    private fun escape(text: String) = text.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace("\"", "&quot;")

    internal companion object {
        const val LOOPBACK = "127.0.0.1"

        /** The claimed callbacks every attempt in the process waits on: how [SignInCallback], which Android starts on its own, reaches them. */
        val claimed = ClaimedCallbacks()
    }
}
