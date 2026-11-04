package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.DeviceRequest
import dev.reamde.substrate.kit.bridge.Envelope
import dev.reamde.substrate.kit.bridge.Handle
import dev.reamde.substrate.kit.bridge.Op
import dev.reamde.substrate.kit.bridge.TransportHeader
import dev.reamde.substrate.kit.bridge.TransportRequest
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RedactionTest {
    private fun text(envelope: Envelope) = BridgeJson.encodeToString(Envelope.serializer(), envelope)

    @Test
    fun aTransportOrSecureStoreBodyIsNeverRecorded() {
        val fetch =
            DeviceRequest.TransportFetch(
                TransportRequest(
                    "https://example.com",
                    "GET",
                    listOf(TransportHeader("Authorization", "Bearer s3cret")),
                    timeoutMillis = 1,
                    credentials = false,
                )
            )
        val request = text(Envelope(1, Op.request, id = 1, body = BridgeJson.encodeToJsonElement(DeviceRequest.Serializer, fetch)))
        assertFalse(Redaction.redact(request).contains("s3cret"))
        val reply = text(Envelope(1, Op.reply, id = 1, body = JsonPrimitive("dG9rZW4=")))
        assertFalse(Redaction.redact(reply, Redaction.Call.SECRET).contains("dG9rZW4="))
        val dns = text(Envelope(1, Op.request, id = 2, body = BridgeJson.encodeToJsonElement(DeviceRequest.Serializer, DeviceRequest.DnsTXT("example.com", 1))))
        assertEquals(dns, Redaction.redact(dns))
        assertEquals("<a message that is not an envelope, 13 bytes>", Redaction.redact("Bearer s3cret"))
    }

    @Test
    fun anAnswerForACallNothingWaitsOnAndAnUnreadableRequestKeepNoBody() {
        for (op in listOf(Op.reply, Op.chunk, Op.error)) {
            val late = text(Envelope(1, op, id = 9, body = JsonPrimitive("dG9rZW4=")))
            val recorded = Redaction.redact(late)
            assertFalse("$op: $recorded", recorded.contains("dG9rZW4="))
            assertTrue(recorded.contains("<redacted: unknown call>"))
        }
        val dnsReply = text(Envelope(1, Op.reply, id = 3, body = JsonPrimitive("rcode 0")))
        assertEquals(dnsReply, Redaction.redact(dnsReply, Redaction.Call.PUBLIC))
        val odd = text(Envelope(1, Op.request, id = 4, body = buildJsonObject { put("transportFetch", buildJsonObject { put("token", "s3cret") }) }))
        assertTrue(Redaction.redact(odd).contains("<redacted: unreadable request>"))
        // A handle's error names no call, and is kept.
        val handleError = text(Envelope(1, Op.error, handle = Handle(1, 1), body = JsonPrimitive("the command failed")))
        assertEquals(handleError, Redaction.redact(handleError))
    }
}
