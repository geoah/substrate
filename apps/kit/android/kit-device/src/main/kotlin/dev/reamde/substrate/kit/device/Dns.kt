package dev.reamde.substrate.kit.device

import android.net.DnsResolver
import android.os.Build
import android.os.CancellationSignal
import dev.reamde.substrate.kit.bridge.DNSAnswer
import java.util.concurrent.Executors
import kotlin.coroutines.resume
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeoutOrNull

/**
 * TXT lookups through the system resolver, `DnsResolver.rawQuery` (Android 10 and later), which honours Private DNS and the active network. The answer goes
 * back raw: its response code and each TXT record's character-strings. Joining, bounding and telling the outcomes apart is Swift's (`BridgeTXTResolver`), as on
 * Apple systems.
 */
public class Dns {
    private val executor = Executors.newSingleThreadExecutor()

    public suspend fun txt(name: String, timeoutMillis: Long): DNSAnswer {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) {
            return DNSAnswer.Failed("TXT lookups need Android 10 or later")
        }
        return withTimeoutOrNull(timeoutMillis.coerceAtLeast(1)) { query(name) } ?: DNSAnswer.Failed("no answer in ${timeoutMillis} ms")
    }

    private suspend fun query(name: String): DNSAnswer = suspendCancellableCoroutine { continuation ->
        val signal = CancellationSignal()
        continuation.invokeOnCancellation { signal.cancel() }
        // `DnsResolver(context, looper)` replaces it from API 37 only.
        @Suppress("DEPRECATION") val resolver = DnsResolver.getInstance()
        resolver.rawQuery(
            null,
            name,
            DnsResolver.CLASS_IN,
            TYPE_TXT,
            DnsResolver.FLAG_EMPTY,
            executor,
            signal,
            object : DnsResolver.Callback<ByteArray> {
                override fun onAnswer(answer: ByteArray, rcode: Int) {
                    val parsed =
                        try {
                            DNSAnswer.Answered(rcode.toLong(), DnsMessage.txtRecords(answer))
                        } catch (failure: IllegalArgumentException) {
                            DNSAnswer.Failed("the answer could not be read: ${failure.message}")
                        }
                    continuation.resume(parsed)
                }

                override fun onError(error: DnsResolver.DnsException) {
                    continuation.resume(DNSAnswer.Failed("the resolver failed (code ${error.code})"))
                }
            },
        )
    }

    private companion object {
        const val TYPE_TXT = 16
    }
}

/** Just enough of RFC 1035 to read a TXT answer: each record's character-strings. */
public object DnsMessage {
    public fun txtRecords(message: ByteArray): List<List<String>> {
        val reader = Reader(message)
        reader.skip(4)
        val questions = reader.u16()
        val answers = reader.u16()
        reader.skip(4)
        repeat(questions) {
            reader.name()
            reader.skip(4)
        }
        val records = mutableListOf<List<String>>()
        repeat(answers) {
            reader.name()
            val type = reader.u16()
            val klass = reader.u16()
            reader.skip(4)
            val length = reader.u16()
            val end = reader.position + length
            require(end <= message.size) { "a record runs past the message" }
            if (type == 16 && klass == 1) {
                val segments = mutableListOf<String>()
                while (reader.position < end) {
                    val size = reader.u8()
                    require(reader.position + size <= end) { "a character-string runs past its record" }
                    segments += String(message, reader.position, size, Charsets.UTF_8)
                    reader.skip(size)
                }
                records += segments
            }
            reader.position = end
        }
        return records
    }

    private class Reader(private val bytes: ByteArray) {
        var position = 0

        fun u8(): Int {
            require(position < bytes.size) { "the message ends early" }
            return bytes[position++].toInt() and 0xff
        }

        fun u16(): Int = (u8() shl 8) or u8()

        fun skip(count: Int) {
            require(position + count <= bytes.size) { "the message ends early" }
            position += count
        }

        /** Skips a name, compressed or not. */
        fun name() {
            while (true) {
                val length = u8()
                when {
                    length == 0 -> return
                    length and 0xc0 == 0xc0 -> {
                        u8()
                        return
                    }
                    else -> skip(length)
                }
            }
        }
    }
}
