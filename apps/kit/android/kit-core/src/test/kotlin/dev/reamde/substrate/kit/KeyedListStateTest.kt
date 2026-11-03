package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.KeyedListState.Applied
import dev.reamde.substrate.kit.bridge.ListMessage
import dev.reamde.substrate.kit.bridge.ListRow
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Test

class KeyedListStateTest {
    private fun row(id: String) = ListRow(id, JsonPrimitive(id))

    @Test
    fun deltasApplyInRevisionOrderAndOldOnesAreDropped() {
        val list = KeyedListState("rows")
        assertEquals(Applied.APPLIED, list.apply(ListMessage.Whole("rows", 5, listOf(row("a")))))
        assertEquals(Applied.DROPPED, list.apply(ListMessage.Delta("rows", 4, 5, listOf(row("x")), emptyList(), emptyList(), listOf("x"))))
        assertEquals(Applied.APPLIED, list.apply(ListMessage.Delta("rows", 5, 6, listOf(row("b")), emptyList(), emptyList(), listOf("b", "a"))))
        assertEquals(listOf("b", "a"), list.snapshot.map { it.id })
        assertEquals(6L, list.revision)
        // A whole list older than the one held is dropped too.
        assertEquals(Applied.DROPPED, list.apply(ListMessage.Whole("rows", 3, emptyList())))
    }

    @Test
    fun aDeltaNamingARowNeverSentAsksForTheWholeList() {
        val list = KeyedListState("rows")
        list.apply(ListMessage.Whole("rows", 1, listOf(row("a"))))
        assertEquals(Applied.NEEDS_WHOLE, list.apply(ListMessage.Delta("rows", 1, 2, emptyList(), emptyList(), emptyList(), listOf("a", "ghost"))))
        assertEquals(listOf("a"), list.snapshot.map { it.id })
        assertEquals(Applied.DROPPED, list.apply(ListMessage.Delta("rows", 2, 3, emptyList(), emptyList(), emptyList(), listOf("a"))))
        assertEquals(Applied.APPLIED, list.apply(ListMessage.Whole("rows", 3, listOf(row("a"), row("b")))))
    }
}
