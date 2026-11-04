package dev.reamde.substrate.kit.device

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import dev.reamde.substrate.kit.bridge.BrowserAnswer
import dev.reamde.substrate.kit.bridge.BrowserPrepared
import dev.reamde.substrate.kit.bridge.BrowserRoute
import java.io.BufferedReader
import java.io.InputStreamReader
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.net.SocketException
import java.net.SocketTimeoutException
import java.net.URLDecoder
import java.util.concurrent.ConcurrentHashMap
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * The owner's browser on the sign-in page, and the callback it comes back
 * with (docs/plans/app-kit.md §5.4). A local build's loopback route listens
 * on `127.0.0.1` alone, on a port the system picks, for the one path Swift
 * named; the query is handed back unchecked, since Swift holds it to its
 * request (`state`, `iss`, PKCE). A release's claimed route is refused
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
public class BrowserSignIn internal constructor(context: Context, private val launch: ((String) -> Boolean)?) {
    public constructor(context: Context) : this(context, null)

    private val app = context.applicationContext
    private val listeners = ConcurrentHashMap<String, Listener>()

    /** A loopback listener: its socket, the path it answers, and the callback once it came. */
    private class Listener(val socket: ServerSocket, val path: String, val appName: String) {
        val callback = CompletableDeferred<Map<String, String>>()
    }

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
                listeners[redirect] = Listener(socket, route.path, route.appName)
                BrowserPrepared.Ready(redirect)
            }
        }
    }

    /**
     * Opens [page] and waits for the callback on [redirect]. A cancelled
     * caller (the connect screen's Cancel, the handle closed) stops waiting;
     * the listener stays until `finish`.
     */
    public suspend fun open(page: String, redirect: String): BrowserAnswer {
        val listener = listeners[redirect] ?: return BrowserAnswer.Failed("The sign-in was not ready to come back. Try again.")
        if (launch == null) SignInWait.start(ForegroundActivity.current ?: app)
        val opened = launch?.invoke(page) ?: withContext(Dispatchers.Main) { openBrowser(page) }
        if (!opened) {
            SignInWait.stop(app)
            return BrowserAnswer.Failed("No browser on this device could open the sign-in page.")
        }
        val accepting = Thread({ accept(listener) }, "substrate-signin").apply { isDaemon = true }
        accepting.start()
        val query = listener.callback.await()
        if (launch == null) withContext(Dispatchers.Main) { bringAppForward() }
        return BrowserAnswer.Callback(query)
    }

    /** Lets go of what `prepare` took, whatever `open` came to. */
    public fun finish(redirect: String) {
        if (launch == null) SignInWait.stop(app)
        listeners.remove(redirect)?.let { listener ->
            listener.callback.cancel()
            runCatching { listener.socket.close() }
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
     * Answers each connection until the callback comes: the expected path
     * with a query is the callback, anything else (a favicon, a prefetch)
     * a 404. Only the loopback address is bound, so nothing off the device
     * reaches it.
     */
    private fun accept(listener: Listener) {
        while (!listener.callback.isCompleted) {
            val connection = try {
                listener.socket.accept()
            } catch (_: SocketException) {
                return
            }
            connection.use { answer(it, listener) }
        }
    }

    private fun answer(connection: Socket, listener: Listener) {
        connection.soTimeout = 10_000
        val line = try {
            BufferedReader(InputStreamReader(connection.getInputStream(), Charsets.ISO_8859_1)).readLine()
        } catch (_: SocketTimeoutException) {
            null
        } ?: return
        val target = line.split(' ').getOrNull(1) ?: return respond(connection, 400, "This is not a sign-in.")
        val path = target.substringBefore('?')
        if (!line.startsWith("GET ") || path != listener.path) return respond(connection, 404, "This is not a sign-in.")
        val query = target.substringAfter('?', "").split('&').filter { it.isNotEmpty() }.associate { pair ->
            decode(pair.substringBefore('=')) to decode(pair.substringAfter('=', ""))
        }
        respond(connection, 200, "You can go back to ${listener.appName}.", listener.appName)
        listener.callback.complete(query)
    }

    private fun decode(text: String): String = URLDecoder.decode(text, Charsets.UTF_8)

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
