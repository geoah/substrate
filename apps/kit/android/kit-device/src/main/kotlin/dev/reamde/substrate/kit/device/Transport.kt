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
import javax.net.ssl.SSLException
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import kotlinx.coroutines.Job
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.suspendCancellableCoroutine
import okhttp3.Call
import okhttp3.Callback
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
 */
public class Transport(context: Context, base: OkHttpClient = OkHttpClient()) {
    private val folder = File(context.cacheDir, "bridge")

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

    private suspend fun send(request: TransportRequest): TransportOutcome {
        val call = try {
            newCall(request)
        } catch (failure: IllegalArgumentException) {
            return TransportOutcome.Failed(TransportFailureKind.other, "not a request OkHttp takes: ${failure.message}")
        }
        val response = try {
            call.await()
        } catch (failure: IOException) {
            return failed(failure) { kind, message -> TransportOutcome.Failed(kind, message) }
        }
        return response.use {
            try {
                TransportOutcome.Response(read(it))
            } catch (failure: IOException) {
                failed(failure) { kind, message -> TransportOutcome.Failed(kind, message) }
            }
        }
    }

    /** The head, then each line of the body; the call is cancelled with the collector. */
    public fun lines(request: TransportRequest): Flow<TransportChunk> = flow {
        val call = try {
            newCall(request)
        } catch (failure: IllegalArgumentException) {
            emit(TransportChunk.Failed(TransportFailureKind.other, "not a request OkHttp takes: ${failure.message}"))
            return@flow
        }
        val cancel = currentCoroutineContext()[Job]?.invokeOnCompletion { call.cancel() }
        try {
            val response = try {
                call.execute()
            } catch (failure: IOException) {
                if (currentCoroutineContext().isActive) emit(failed(failure) { kind, message -> TransportChunk.Failed(kind, message) })
                return@flow
            }
            response.use {
                emit(TransportChunk.Head(head(it)))
                val source = it.body.source()
                try {
                    while (true) {
                        val line = source.readUtf8Line() ?: break
                        emit(TransportChunk.Line(line))
                    }
                } catch (failure: IOException) {
                    if (currentCoroutineContext().isActive) emit(failed(failure) { kind, message -> TransportChunk.Failed(kind, message) })
                }
            }
        } finally {
            cancel?.dispose()
            request.bodyFile?.let { File(it).delete() }
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

    /** The body inline up to the inline limit, else in a file Swift reads and deletes. */
    private fun read(response: Response): TransportResponse {
        val source = response.body.source()
        val limit = INLINE_LIMIT
        // False when the body ended first: then all of it is in the buffer.
        if (!source.request(limit + 1L)) {
            val bytes = source.readByteArray()
            return head(response).copy(body = Base64.getEncoder().encodeToString(bytes))
        }
        folder.mkdirs()
        val file = File(folder, "response-${UUID.randomUUID()}")
        file.outputStream().use { output -> source.inputStream().copyTo(output) }
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
    }
}

private suspend fun Call.await(): Response = suspendCancellableCoroutine { continuation ->
    continuation.invokeOnCancellation { cancel() }
    enqueue(
        object : Callback {
            override fun onResponse(call: Call, response: Response) {
                continuation.resume(response) { _, value, _ -> value.close() }
            }

            override fun onFailure(call: Call, e: IOException) {
                continuation.resumeWithException(e)
            }
        },
    )
}
