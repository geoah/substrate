package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.DeviceRequest
import dev.reamde.substrate.kit.bridge.Envelope
import dev.reamde.substrate.kit.bridge.Op
import dev.reamde.substrate.kit.bridge.TransportHeader
import dev.reamde.substrate.kit.bridge.TransportRequest
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class RedactionTest {
    private fun text(envelope: Envelope) = BridgeJson.encodeToString(Envelope.serializer(), envelope)

    @Test
    fun aTransportOrSecureStoreBodyIsNeverRecorded() {
        val fetch = DeviceRequest.TransportFetch(TransportRequest("https://example.com", "GET", listOf(TransportHeader("Authorization", "Bearer s3cret")), timeoutMillis = 1))
        val request = text(Envelope(1, Op.request, id = 1, body = BridgeJson.encodeToJsonElement(DeviceRequest.Serializer, fetch)))
        assertFalse(Redaction.redact(request).contains("s3cret"))
        val reply = text(Envelope(1, Op.reply, id = 1, body = JsonPrimitive("dG9rZW4=")))
        assertFalse(Redaction.redact(reply, sensitive = true).contains("dG9rZW4="))
        val dns = text(Envelope(1, Op.request, id = 2, body = BridgeJson.encodeToJsonElement(DeviceRequest.Serializer, DeviceRequest.DnsTXT("example.com", 1))))
        assertEquals(dns, Redaction.redact(dns))
        assertEquals("<a message that is not an envelope, 13 bytes>", Redaction.redact("Bearer s3cret"))
    }
}
