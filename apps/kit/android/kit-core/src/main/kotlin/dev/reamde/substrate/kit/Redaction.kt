package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.DeviceRequest
import dev.reamde.substrate.kit.bridge.Envelope
import dev.reamde.substrate.kit.bridge.Op
import kotlinx.serialization.json.JsonPrimitive

/**
 * What of a message may be recorded: all of it but the body of a transport
 * or secure store message, which may carry a token (docs/plans/app-kit.md
 * §5.4). Swift's `Redaction` is the same rule; a message that is not an
 * envelope is recorded as its length.
 */
public object Redaction {
    public fun redact(message: String, sensitive: Boolean = false): String {
        val envelope = try {
            BridgeJson.decodeFromString(Envelope.serializer(), message)
        } catch (_: Exception) {
            return "<a message that is not an envelope, ${message.toByteArray().size} bytes>"
        }
        val body = envelope.body ?: return message
        var secret = sensitive
        if (envelope.op == Op.request) {
            secret = secret || runCatching { carriesSecrets(BridgeJson.decodeFromJsonElement(DeviceRequest.Serializer, body)) }.getOrDefault(true)
        }
        if (!secret) return message
        return BridgeJson.encodeToString(Envelope.serializer(), envelope.copy(body = JsonPrimitive("<redacted>")))
    }

    public fun carriesSecrets(request: DeviceRequest): Boolean = when (request) {
        is DeviceRequest.TransportFetch, is DeviceRequest.TransportLines,
        is DeviceRequest.SecureRead, is DeviceRequest.SecureWrite, is DeviceRequest.SecureDelete,
        -> true
        is DeviceRequest.DnsTXT, DeviceRequest.ConnectivityCurrent, DeviceRequest.ConnectivityWatch -> false
    }
}
