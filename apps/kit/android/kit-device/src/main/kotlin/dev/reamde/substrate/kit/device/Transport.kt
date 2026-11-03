package dev.reamde.substrate.kit.device

import android.content.Context
import dev.reamde.substrate.kit.bridge.TransportChunk
import dev.reamde.substrate.kit.bridge.TransportFailureKind
import dev.reamde.substrate.kit.bridge.TransportHeader
import dev.reamde.substrate.kit.bridge.TransportOutcome
import dev.reamde.substrate.kit.bridge.TransportRequest
import dev.reamde.substrate.kit.bridge.TransportResponse
import java.io.File
import java.io.IOException
import java.io.InterruptedIOException
import java.net.ConnectException
import java.net.NoRouteToHostException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import java.util.Base64
import java.util.UUID
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import javax.net.ssl.SSLException
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.channelFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import okhttp3.Call
import okhttp3.Headers
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.asRequestBody
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response

/**
 * The transport: OkHttp, which follows no redirect on any path, a streamed
 * response's included (`URLSessionTransport`'s `RefusingRedirects`), so a
 * 3xx is the answer and the kit's client refuses it. No cache: every answer
 * is a repository's current state.
 *
 * A call is cancelled the moment its caller is, while it waits for the head
 * and while it reads the body alike: OkHttp's reads block, and only
 * cancelling the call ends them.
 *
 * Make one per process, before the bridge starts: it sweeps the spill
 * folder, where nothing is in flight yet and anything there was left by a
 * process that died.
 */
public class Transport(context: Context, base: OkHttpClient = OkHttpClient()) {
    /**
     * Bodies too large for a message, both ways: under `noBackupFilesDir`,
     * since one may hold the owner's records (Swift's `BridgeEnvironment.spill`).
     */
    public val folder: File = File(context.noBackupFilesDir, "bridge")

    init {
        folder.listFiles()?.forEach { it.delete() }
        // Where builds before this one spilled.
        File(context.cacheDir, "bridge").deleteRecursively()
    }

    public val client: OkHttpClient = base.newBuilder()
        .followRedirects(false)
        .followSslRedirects(false)
        .cache(null)
        .build()

    /** A body Swift wrote to a file is deleted once the call is done with it. */
    public suspend fun fetch(request: TransportRequest): TransportOutcome = try {
        send(request)
    } finally {
        request.bodyFile?.let { File(it).delete() }
    }

    /** Deletes what an outcome Swift never took owns: its body's file. */
    public fun discard(outcome: TransportOutcome) {
        if (outcome is TransportOutcome.Response) outcome.response.bodyFile?.let { File(it).delete() }
    }

    private suspend fun send(request: TransportRequest): TransportOutcome {
        val call = try {
            newCall(request)
        } catch (failure: IllegalArgumentException) {
            return TransportOutcome.Failed(TransportFailureKind.other, "not a request OkHttp takes: ${failure.message}")
        }
        return try {
            call.runCancellable({ response -> TransportOutcome.Response(read(response, request.credentials)) }, ::discard)
        } catch (failure: IOException) {
            failed(failure) { kind, message -> TransportOutcome.Failed(kind, message) }
        } catch (_: CredentialAnswerTooLarge) {
            TransportOutcome.Failed(
                TransportFailureKind.other,
                "the answer to a request carrying credentials is larger than $CREDENTIAL_LIMIT bytes",
            )
        }
    }

    /** The head, then each line of the body; the call is cancelled with the collector, mid-read included. */
    public fun lines(request: TransportRequest): Flow<TransportChunk> = channelFlow {
        val call = try {
            newCall(request)
        } catch (failure: IllegalArgumentException) {
            send(TransportChunk.Failed(TransportFailureKind.other, "not a request OkHttp takes: ${failure.message}"))
            request.bodyFile?.let { File(it).delete() }
            return@channelFlow
        }
        val reader = launch(Dispatchers.IO) {
            try {
                val response = try {
                    call.execute()
                } catch (failure: IOException) {
                    if (isActive) send(failed(failure) { kind, message -> TransportChunk.Failed(kind, message) })
                    return@launch
                }
                response.use {
                    send(TransportChunk.Head(head(it)))
                    val source = it.body.source()
                    try {
                        while (true) {
                            val line = source.readUtf8Line() ?: break
                            send(TransportChunk.Line(line))
                        }
                    } catch (failure: IOException) {
                        if (isActive) send(failed(failure) { kind, message -> TransportChunk.Failed(kind, message) })
                    }
                }
            } finally {
                request.bodyFile?.let { File(it).delete() }
            }
        }
        try {
            reader.join()
        } finally {
            // A cancelled collector lands here at once, while the reader may
            // be blocked on the socket: this is what ends the read.
            call.cancel()
        }
    }

    private fun newCall(request: TransportRequest): Call {
        val headers = Headers.Builder()
        for (header in request.headers) headers.add(header.name, header.value)
        val built = headers.build()
        val type = built["Content-Type"]?.toMediaTypeOrNull()
        val file = request.bodyFile
        val inline = request.body
        val body: RequestBody? = when {
            file != null -> File(file).asRequestBody(type)
            inline != null -> Base64.getDecoder().decode(inline).toRequestBody(type)
            request.method in setOf("POST", "PUT", "PATCH") -> ByteArray(0).toRequestBody(type)
            else -> null
        }
        val timeout = request.timeoutMillis.coerceAtLeast(1)
        val client = client.newBuilder()
            .connectTimeout(timeout, TimeUnit.MILLISECONDS)
            .readTimeout(timeout, TimeUnit.MILLISECONDS)
            .writeTimeout(timeout, TimeUnit.MILLISECONDS)
            .build()
        return client.newCall(Request.Builder().url(request.url).headers(built).method(request.method, body).build())
    }

    private fun head(response: Response) = TransportResponse(
        status = response.code.toLong(),
        url = response.request.url.toString(),
        headers = response.headers.map { (name, value) -> TransportHeader(name, value) },
    )

    /**
     * The body inline up to the inline limit, else in a file Swift reads and
     * deletes. An answer to a request carrying credentials never goes to a
     * file: inline up to [CREDENTIAL_LIMIT], refused past it. A file this
     * read does not finish is deleted before the failure leaves it.
     */
    private fun read(response: Response, credentials: Boolean): TransportResponse {
        val source = response.body.source()
        val limit = if (credentials) CREDENTIAL_LIMIT else INLINE_LIMIT
        // False when the body ended first: then all of it is in the buffer.
        if (!source.request(limit + 1L)) {
            val bytes = source.readByteArray()
            return head(response).copy(body = Base64.getEncoder().encodeToString(bytes))
        }
        if (credentials) throw CredentialAnswerTooLarge()
        folder.mkdirs()
        val file = File(folder, "response-${UUID.randomUUID()}")
        try {
            file.outputStream().use { output -> source.inputStream().copyTo(output) }
        } catch (failure: Throwable) {
            file.delete()
            throw failure
        }
        return head(response).copy(bodyFile = file.path)
    }

    private fun <T> failed(failure: IOException, make: (TransportFailureKind, String) -> T): T {
        val kind = when (failure) {
            is SocketTimeoutException -> TransportFailureKind.timedOut
            is UnknownHostException -> TransportFailureKind.cannotConnect
            is NoRouteToHostException -> TransportFailureKind.offline
            is ConnectException -> TransportFailureKind.cannotConnect
            is SSLException -> TransportFailureKind.secureConnectionFailed
            is InterruptedIOException -> if (failure.message == "timeout") TransportFailureKind.timedOut else TransportFailureKind.cancelled
            else -> if (failure.message == "Canceled") TransportFailureKind.cancelled else TransportFailureKind.other
        }
        // The exception's own words name the host at most; never a header.
        return make(kind, "${failure::class.java.simpleName}: ${failure.message ?: "no detail"}")
    }

    public companion object {
        /** Swift's `TransportRequest.inlineLimit`. */
        public const val INLINE_LIMIT: Int = 256 * 1024

        /** Swift's `TransportRequest.credentialLimit`. */
        public const val CREDENTIAL_LIMIT: Int = 1024 * 1024
    }
}

private class CredentialAnswerTooLarge : Exception()

private const val PENDING = 0
private const val MADE = 1
private const val ABANDONED = 2

/**
 * Runs the call and [read] on an IO thread, and cancels the call the moment
 * the caller is cancelled, through the head and the body alike. What [read]
 * makes after the caller gave up (a spilled file) goes to [discard], once,
 * whichever side finds it abandoned.
 */
private suspend fun <T : Any> Call.runCancellable(read: (Response) -> T, discard: (T) -> Unit): T = coroutineScope {
    val made = AtomicReference<T?>(null)
    val state = AtomicInteger(PENDING)
    val running = async(Dispatchers.IO) {
        runCatching { execute().use(read) }.onSuccess { value ->
            made.set(value)
            if (!state.compareAndSet(PENDING, MADE)) discard(value)
        }
    }
    try {
        running.await().getOrThrow()
    } catch (cancelled: CancellationException) {
        cancel()
        if (!state.compareAndSet(PENDING, ABANDONED)) made.get()?.let(discard)
        throw cancelled
    }
}
