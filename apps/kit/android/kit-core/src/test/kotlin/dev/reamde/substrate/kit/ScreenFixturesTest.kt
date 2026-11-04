package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.ScreenFixture
import java.io.File
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.jsonArray
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The shared screens' fixtures Swift wrote (`fixtures/screens.json`), decoded with the generated serializers and encoded again: Kotlin writes what Swift wrote,
 * every key and value, and hands it back to Swift (`swiftDecodesWhatKotlinWroteOfEachScreen`).
 */
class ScreenFixturesTest {
    @Test
    fun everyScreenFixtureDecodesAndEncodesAsSwiftWroteIt() {
        val text = File(System.getProperty("substrate.screens.fixtures")).readText()
        val swift = BridgeJson.parseToJsonElement(text).jsonArray
        val fixtures = BridgeJson.decodeFromString(ListSerializer(ScreenFixture.serializer()), text)
        assertEquals(swift.size, fixtures.size)
        val kotlin = BridgeJson.encodeToJsonElement(ListSerializer(ScreenFixture.serializer()), fixtures) as JsonArray
        for ((index, fixture) in fixtures.withIndex()) {
            assertEquals(fixture.name, swift[index], kotlin[index])
        }
        val out = File(System.getProperty("substrate.screens.out"))
        out.parentFile.mkdirs()
        out.writeText(BridgeJson.encodeToString(JsonArray.serializer(), kotlin))
    }
}
