package dev.reamde.substrate.kit.device

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
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
import java.net.URLDecoder
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Semaphore
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * The owner's browser on the sign-in page, and the callback it comes back
 * with (docs/plans/app-kit.md §5.4). A local build's loopback route listens
 * on `127.0.0.1` alone, on a port the system picks, for the one path Swift
 * named, until the attempt's deadline; the callback is the request carrying
 * the attempt's `state`, and its query is handed back otherwise unchecked,
 * since Swift holds it to its request (`state`, `iss`, PKCE). Every request
 * is bounded in size and time, and one it cannot read is answered 400
 * without ending the attempt. A release's claimed route is refused
 * until the app takes its verified App Link: a release never falls back to
 * loopback.
 *
 * The page opens in the owner's default browser, as on a Mac, so the
 * connect screen offers its own Cancel; cancelling the call stops the
 * listener. While the browser is in front, `SignInWait` keeps the process
 * from being frozen, which would leave the callback unanswered. The page
 * the browser lands on tells the person to go back to the app and links to
 * it, and the app is brought forward where Android lets it.
 */
public class BrowserSignIn internal constructor(
    context: Context,
    private val launch: ((String) -> Boolean)?,
    private val limits: Limits = Limits(),
) {
    public constructor(context: Context) : this(context, null)

    private val app = context.applicationContext
    private val listeners = ConcurrentHashMap<String, Listener>()

    /**
     * What one attempt may take. [attempt] is the server's: an authorization
     * request lives ten minutes, so no callback after that could be
     * exchanged. [connection] is the whole request head's, however slowly
     * it trickles in, and [connections] how many are read at once: a browser
     * opens a preconnection it may never write to, so one at a time would
     * hold the callback behind it.
     */
    internal data class Limits(
        val attempt: Long = 10 * 60_000L,
        val connection: Long = 10_000L,
        val connections: Int = 4,
        val line: Int = 8 * 1024,
        val headers: Int = 64,
        val head: Int = 32 * 1024,
    )

    /** A loopback listener: its socket, the path it answers, and how the attempt ended once it did. */
    private class Listener(val socket: ServerSocket, val path: String, val appName: String, limits: Limits) {
        val answer = CompletableDeferred<BrowserAnswer>()
        val opened = AtomicBoolean(false)
        val called = AtomicBoolean(false)
        val slots = Semaphore(limits.connections)
        val connections: MutableSet<Socket> = ConcurrentHashMap.newKeySet()

        /** The listener and every connection still open, closed; safe to call again. */
        fun close() {
            runCatching { socket.close() }
            connections.forEach { runCatching { it.close() } }
        }
    }

    /** A request the listener cannot read: answered 400, and the attempt goes on. */
    private class Malformed(message: String) : Exception(message)

    public suspend fun prepare(route: BrowserRoute): BrowserPrepared = withContext(Dispatchers.IO) {
        when (route) {
            is BrowserRoute.Claimed -> BrowserPrepared.Failed(
                "This build signs in through its verified link (${route.url}), which this Android build does not take yet.",
            )
            is BrowserRoute.Loopback -> {
                if (!route.path.startsWith("/")) return@withContext BrowserPrepared.Failed("The sign-in's path ${route.path} is not a path.")
                val socket = try {
                    ServerSocket(0, 8, InetAddress.getByName(LOOPBACK))
                } catch (failure: Exception) {
                    return@withContext BrowserPrepared.Failed("The sign-in could not listen on this device: ${failure.message}")
                }
                val redirect = "http://$LOOPBACK:${socket.localPort}${route.path}"
                listeners[redirect] = Listener(socket, route.path, route.appName, limits)
                BrowserPrepared.Ready(redirect)
            }
        }
    }

    /**
     * Opens [page] and waits for the callback on [redirect], until the
     * attempt's deadline, which answers `expired`. The callback is the
     * request on the path carrying the `state` [page] names, which the
     * server echoes; one with another `state` is refused and the wait goes
     * on, so a stray request cannot end the attempt. Swift still checks
     * `state`, `iss` and PKCE. A cancelled caller (the connect screen's
     * Cancel, the handle closed) stops waiting; the listener stays until
     * `finish`.
     */
    public suspend fun open(page: String, redirect: String): BrowserAnswer {
        val listener = listeners[redirect] ?: return BrowserAnswer.Failed("The sign-in was not ready to come back. Try again.")
        val state = Uri.parse(page).getQueryParameter("state")?.takeIf { it.isNotEmpty() }
            ?: return BrowserAnswer.Failed("The sign-in page names no state to hold its callback to.")
        if (!listener.opened.compareAndSet(false, true)) return BrowserAnswer.Failed("This sign-in is already open in the browser.")
        if (launch == null) SignInWait.start(ForegroundActivity.current ?: app)
        val opened = launch?.invoke(page) ?: withContext(Dispatchers.Main) { openBrowser(page) }
        if (!opened) {
            SignInWait.stop(app)
            return BrowserAnswer.Failed("No browser on this device could open the sign-in page.")
        }
        val deadline = SystemClock.elapsedRealtime() + limits.attempt
        Thread({ accept(listener, state, deadline) }, "substrate-signin").apply { isDaemon = true }.start()
        val answer = listener.answer.await()
        if (launch == null) {
            SignInWait.stop(app)
            if (answer is BrowserAnswer.Callback) withContext(Dispatchers.Main) { bringAppForward() }
        }
        return answer
    }

    /** Lets go of what `prepare` took, whatever `open` came to. */
    public fun finish(redirect: String) {
        if (launch == null) SignInWait.stop(app)
        listeners.remove(redirect)?.let { listener ->
            listener.answer.cancel()
            listener.close()
        }
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
     * Takes connections until the attempt ends: the callback, the deadline,
     * `finish` closing the socket, or a failure, each closing the listener
     * and every connection it took. Each connection is read on its own
     * thread, at most `limits.connections` at once, so a silent one holds
     * no other up. Only the loopback address is bound, so nothing off the
     * device reaches it.
     */
    private fun accept(listener: Listener, state: String, deadline: Long) {
        try {
            while (!listener.answer.isCompleted) {
                val remaining = deadline - SystemClock.elapsedRealtime()
                if (remaining <= 0) {
                    listener.answer.complete(BrowserAnswer.Expired)
                    break
                }
                if (!listener.slots.tryAcquire(remaining, TimeUnit.MILLISECONDS)) continue
                val connection = try {
                    listener.socket.soTimeout = remaining.coerceIn(1, Int.MAX_VALUE.toLong()).toInt()
                    listener.socket.accept()
                } catch (_: SocketTimeoutException) {
                    listener.slots.release()
                    continue
                } catch (failure: IOException) {
                    listener.slots.release()
                    // Closed by `finish`, or the socket failed under it.
                    listener.answer.complete(BrowserAnswer.Failed("The sign-in stopped listening: ${failure.message}"))
                    break
                }
                listener.connections += connection
                val until = minOf(SystemClock.elapsedRealtime() + limits.connection, deadline)
                Thread({ serve(connection, listener, state, until) }, "substrate-signin-request").apply { isDaemon = true }.start()
            }
        } catch (failure: Throwable) {
            listener.answer.complete(BrowserAnswer.Failed("The sign-in stopped listening: ${failure.message}"))
        } finally {
            listener.close()
        }
    }

    /**
     * Answers one connection, whatever it sends: the expected path with
     * this attempt's `state` is the callback; another path (a favicon, a
     * prefetch) a 404; a request that cannot be read, or the path with
     * another `state`, a 400. Only the callback ends the attempt. A reset,
     * a timeout or anything else thrown here ends this connection alone.
     */
    private fun serve(connection: Socket, listener: Listener, state: String, until: Long) {
        try {
            val target = try {
                requestTarget(connection, until) ?: return
            } catch (refused: Malformed) {
                return respond(connection, 400, "This is not a sign-in.")
            }
            val path = target.substringBefore('?')
            if (path != listener.path) return respond(connection, 404, "This is not a sign-in.")
            val query = try {
                query(target.substringAfter('?', ""))
            } catch (refused: Malformed) {
                return respond(connection, 400, "This is not a sign-in.")
            }
            val came = query["state"]?.toByteArray(Charsets.UTF_8)
            if (came == null || !MessageDigest.isEqual(came, state.toByteArray(Charsets.UTF_8))) {
                return respond(connection, 400, "This is not the sign-in ${listener.appName} is waiting for.")
            }
            if (!listener.called.compareAndSet(false, true)) return respond(connection, 400, "This sign-in is over.")
            respond(connection, 200, "You can go back to ${listener.appName}.", listener.appName)
            listener.answer.complete(BrowserAnswer.Callback(query))
            // The page is written: the listener stops taking connections.
            runCatching { listener.socket.close() }
        } catch (_: Exception) {
        } finally {
            listener.connections -= connection
            runCatching { connection.close() }
            listener.slots.release()
        }
    }

    /**
     * The request line's target, once the head is read to its blank line:
     * null where the connection closed before a request line, [Malformed]
     * where the head breaks a limit or is not `GET <target> HTTP/1.x`.
     * Every read is held to [until], so a head that trickles in a byte at a
     * time still ends then.
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

    /** The query's pairs, decoded; a bad escape or a name given twice is [Malformed]. */
    private fun query(text: String): Map<String, String> {
        val pairs = mutableMapOf<String, String>()
        for (pair in text.split('&').filter { it.isNotEmpty() }) {
            val name = decode(pair.substringBefore('='))
            if (pairs.put(name, decode(pair.substringAfter('=', ""))) != null) throw Malformed("$name is given twice")
        }
        return pairs
    }

    private fun decode(text: String): String = try {
        URLDecoder.decode(text, Charsets.UTF_8)
    } catch (failure: IllegalArgumentException) {
        throw Malformed("a bad escape: ${failure.message}")
    }

    /**
     * Reads a request head's lines, each bounded, the whole head bounded,
     * every socket read given only what is left until the connection's
     * deadline.
     */
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
        val back = appName?.let {
            val intent = ReturnToApp.link(app.packageName)
            "<p><a href=\"${escape(intent)}\" style=\"color:#0a84ff\">Return to ${escape(it)}</a></p>"
        } ?: ""
        val body = """
            <!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
            <body style="font:17px system-ui,sans-serif;display:flex;flex-direction:column;align-items:center;justify-content:center;height:90vh;color:#444">
            <p>${escape(message)}</p>$back</body></html>
        """.trimIndent().toByteArray(Charsets.UTF_8)
        val reason = when (status) {
            200 -> "OK"
            404 -> "Not Found"
            else -> "Bad Request"
        }
        val head = "HTTP/1.1 $status $reason\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: ${body.size}\r\n" +
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
    }
}
