package dev.reamde.substrate.bridgecheck

import java.io.BufferedReader
import java.io.InputStreamReader
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.util.concurrent.atomic.AtomicInteger
import kotlin.concurrent.thread

/**
 * An HTTP server on a loopback port, so the transport checks need no network: `/bytes?n=`, `/lines?n=`, `/redirect` (a 302 to `/landed`, whose hits are
 * counted, so a followed redirect shows).
 */
class LocalServer : AutoCloseable {
    private val socket = ServerSocket(0, 50, InetAddress.getByName("127.0.0.1"))
    val landed = AtomicInteger(0)
    val origin = "http://127.0.0.1:${socket.localPort}"

    init {
        thread(isDaemon = true, name = "bridge-check-server") {
            while (!socket.isClosed) {
                val client =
                    try {
                        socket.accept()
                    } catch (_: Exception) {
                        return@thread
                    }
                thread(isDaemon = true) { serve(client) }
            }
        }
    }

    private fun serve(client: Socket) {
        client.use { serveOne(it) }
    }

    private fun serveOne(it: Socket) {
        val reader = BufferedReader(InputStreamReader(it.getInputStream()))
        val line = reader.readLine() ?: return
        while (reader.readLine()?.isNotEmpty() == true) continue
        val target = line.split(" ").getOrElse(1) { "/" }
        val path = target.substringBefore("?")
        val n = target.substringAfter("n=", "0").toIntOrNull() ?: 0
        val out = it.getOutputStream().buffered(64 * 1024)
        fun head(status: String, extra: String = "", length: Long? = null) {
            val size = length?.let { "Content-Length: $it\r\n" } ?: ""
            out.write("HTTP/1.1 $status\r\nConnection: close\r\n$size$extra\r\n".toByteArray())
        }
        try {
            when (path) {
                "/bytes" -> {
                    head("200 OK", "Content-Type: application/octet-stream\r\n", n.toLong())
                    val chunk = ByteArray(64 * 1024) { 'x'.code.toByte() }
                    var left = n
                    while (left > 0) {
                        val size = minOf(left, chunk.size)
                        out.write(chunk, 0, size)
                        left -= size
                    }
                }
                "/lines" -> {
                    head("200 OK", "Content-Type: application/x-ndjson\r\n")
                    for (i in 1..n) out.write("{\"n\":$i}\n".toByteArray())
                }
                "/redirect" -> head("302 Found", "Location: $origin/landed\r\n", 0)
                "/landed" -> {
                    landed.incrementAndGet()
                    head("200 OK", "", 6)
                    out.write("landed".toByteArray())
                }
                else -> head("404 Not Found", "", 0)
            }
            out.flush()
        } catch (_: Exception) {
            // The reader went away: a cancelled stream.
        }
    }

    override fun close() {
        socket.close()
    }
}
