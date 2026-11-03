package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.Envelope
import dev.reamde.substrate.kit.bridge.Handle
import dev.reamde.substrate.kit.bridge.ListMessage
import dev.reamde.substrate.kit.bridge.ListRow
import dev.reamde.substrate.kit.bridge.Op
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** A presentation that crossed without its keyed lists' rows, put back together. */
class KeyedListTreeTest {
    private fun reference(name: String, revision: Long) = buildJsonObject {
        put("\$list", name)
        put("revision", revision)
    }

    private fun row(id: String) = ListRow(id, buildJsonObject {
        put("id", id)
        put("text", id.uppercase())
    })

    private fun tree(revision: Long) = buildJsonObject {
        put("count", "$revision")
        put("app", buildJsonObject { put("rows", reference("app/rows", revision)) })
    }

    @Test
    fun aReferenceIsReplacedByItsRowsAtTheRevisionItNames() {
        val resolved = KeyedListTree.resolve(tree(2), mapOf("app/rows" to listOf(row("a"), row("b"))), mapOf("app/rows" to 2L))
        val expected = buildJsonObject {
            put("count", "2")
            put("app", buildJsonObject {
                put("rows", buildJsonObject {
                    put("revision", 2)
                    put("rows", buildJsonArray {
                        add(row("a").value)
                        add(row("b").value)
                    })
                })
            })
        }
        assertEquals(expected, resolved)
        assertEquals(mapOf("app/rows" to 2L), KeyedListTree.references(tree(2)))
    }

    @Test
    fun aTreeWaitsForTheListsItNames() {
        assertNull(KeyedListTree.resolve(tree(3), mapOf("app/rows" to listOf(row("a"))), mapOf("app/rows" to 2L)))
        assertNull(KeyedListTree.resolve(tree(1), emptyMap(), emptyMap()))
        // An object that only looks like part of one is left as it is.
        val plain = buildJsonObject {
            put("\$list", "x")
            put("revision", 1)
            put("more", true)
        }
        assertEquals(plain, KeyedListTree.resolve(plain, emptyMap(), emptyMap()))
    }

    @Test
    fun theHandleDrawsATreeOnlyOnceItsListsAgreeAndKeepsTheLastOneBetween() = runTest {
        val core = BridgeCore({ text ->
            val envelope = BridgeJson.decodeFromString(Envelope.serializer(), text)
            val reply = if (envelope.op == Op.open) Envelope(1, Op.opened, handle = Handle(1, 1)) else Envelope(1, Op.accepted)
            BridgeJson.encodeToString(Envelope.serializer(), reply)
        }, NoDeviceServices, this)
        val handle = core.open("ledger").getOrThrow()
        fun deliver(op: Op, body: JsonElement) =
            core.receive(BridgeJson.encodeToString(Envelope.serializer(), Envelope(1, op, handle = handle.handle, body = body)))
        fun list(message: ListMessage) = deliver(Op.list, BridgeJson.encodeToJsonElement(ListMessage.Serializer, message))

        list(ListMessage.Whole("app/rows", 1, listOf(row("a"))))
        deliver(Op.presentation, tree(1))
        val first = handle.state.value.tree as JsonObject
        assertEquals(listOf(row("a").value), BridgeJson.parseToJsonElement(first["app"].toString()).let {
            ((it as JsonObject)["rows"] as JsonObject)["rows"]?.let { rows -> (rows as kotlinx.serialization.json.JsonArray).toList() }
        })
        // The next frame's delta arrives before its tree: the last tree stands.
        list(ListMessage.Delta("app/rows", 1, 2, listOf(row("b")), emptyList(), emptyList(), listOf("a", "b")))
        assertEquals(first, handle.state.value.tree)
        deliver(Op.presentation, tree(2))
        assertEquals("2", ((handle.state.value.tree as JsonObject)["count"] as kotlinx.serialization.json.JsonPrimitive).content)
    }
}
