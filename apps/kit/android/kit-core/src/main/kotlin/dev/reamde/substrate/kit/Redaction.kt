package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.BridgeQuery
import dev.reamde.substrate.kit.bridge.DeviceRequest
import dev.reamde.substrate.kit.bridge.Envelope
import dev.reamde.substrate.kit.bridge.Op
import kotlinx.serialization.json.JsonPrimitive

/**
 * What of a message may be recorded: all of it but a body that may carry a
 * secret (docs/plans/app-kit.md §5.4). A body is kept only when what it
 * belongs to is known to carry none, so an answer for a call nobody waits
 * on, or a request that does not read, is recorded without its body; a
 * message that is not an envelope is recorded as its length. Swift's
 * `Redaction` is the same rule.
 */
public object Redaction {
    /** What the recorder knows of the call a message answers. */
    public enum class Call {
        /** A transport or secure store call's: its body may hold a token. */
        SECRET,

        /** A call whose answers carry none: DNS, connectivity, the bridge's counters. */
        PUBLIC,

        /** Nothing known waits on the id. */
        UNKNOWN,
    }

    public fun redact(message: String, call: Call = Call.UNKNOWN): String {
        val envelope = try {
            BridgeJson.decodeFromString(Envelope.serializer(), message)
        } catch (_: Exception) {
            return "<a message that is not an envelope, ${message.toByteArray().size} bytes>"
        }
        val body = envelope.body ?: return message
        val label: String? = when (envelope.op) {
            Op.request -> {
                val request = runCatching { BridgeJson.decodeFromJsonElement(DeviceRequest.Serializer, body) }.getOrNull()
                when {
                    request != null -> if (carriesSecrets(request)) "request" else null
                    runCatching { BridgeJson.decodeFromJsonElement(BridgeQuery.Serializer, body) }.isSuccess -> null
                    else -> "unreadable request"
                }
            }
            Op.reply, Op.chunk, Op.end, Op.error, Op.cancel -> when {
                envelope.id == null -> null
                call == Call.PUBLIC -> null
                call == Call.SECRET -> "secret"
                else -> "unknown call"
            }
            else -> null
        }
        label ?: return message
        return BridgeJson.encodeToString(Envelope.serializer(), envelope.copy(body = JsonPrimitive("<redacted: $label>")))
    }

    public fun carriesSecrets(request: DeviceRequest): Boolean = when (request) {
        is DeviceRequest.TransportFetch, is DeviceRequest.TransportLines,
        is DeviceRequest.SecureRead, is DeviceRequest.SecureWrite, is DeviceRequest.SecureDelete,
        -> true
        is DeviceRequest.DnsTXT, DeviceRequest.ConnectivityCurrent, DeviceRequest.ConnectivityWatch -> false
    }
}
