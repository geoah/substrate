package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.bridgedSerializers
import java.io.File
import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The round trip's middle leg (app-kit.md §5.5): every value Swift wrote to
 * the fixtures, decoded with the generated serializer and encoded again.
 * What Kotlin writes goes back to Swift (`KotlinRoundTrip`), which must read
 * each as the value it started from.
 */
class RoundTripTest {
    @Test
    fun everyFixtureDecodesAndEncodesStably() {
        val fixtures = BridgeJson.parseToJsonElement(File(System.getProperty("substrate.roundtrip.fixtures")).readText()).jsonArray
        val written = buildJsonArray {
            for (entry in fixtures) {
                val type = entry.jsonObject.getValue("type").jsonPrimitive.content
                @Suppress("UNCHECKED_CAST")
                val serializer = bridgedSerializers[type] as KSerializer<Any>? ?: error("no serializer for $type")
                val value = BridgeJson.decodeFromJsonElement(serializer, entry.jsonObject.getValue("json"))
                val encoded = BridgeJson.encodeToJsonElement(serializer, value)
                // Decoding what Kotlin wrote gives the same value again.
                assertEquals(type, value, BridgeJson.decodeFromJsonElement(serializer, encoded))
                add(buildJsonObject {
                    put("type", type)
                    put("json", encoded)
                })
            }
        }
        val out = File(System.getProperty("substrate.roundtrip.out"))
        out.parentFile.mkdirs()
        out.writeText(BridgeJson.encodeToString(JsonArray.serializer(), written))
        assertEquals(fixtures.size, written.size)
    }

    @Test
    fun aNullOptionalIsLeftOutAsSwiftLeavesItOut() {
        val fixtures = BridgeJson.parseToJsonElement(File(System.getProperty("substrate.roundtrip.fixtures")).readText()).jsonArray
        val withNull = fixtures.map { it.jsonObject }.first { it.getValue("json").toString().contains("\"body\":null") }
        @Suppress("UNCHECKED_CAST")
        val serializer = bridgedSerializers.getValue("TransportRequest") as KSerializer<Any>
        val encoded = BridgeJson.encodeToJsonElement(serializer, BridgeJson.decodeFromJsonElement(serializer, withNull.getValue("json")))
        assertEquals(false, (encoded as JsonObject).containsKey("body"))
    }
}
