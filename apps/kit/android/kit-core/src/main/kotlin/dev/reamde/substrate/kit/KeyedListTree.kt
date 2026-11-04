package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.ListRow
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.longOrNull

/**
 * A presentation that crossed without its keyed lists' rows (Swift's `KeyedListReference`, `{"$list": name, "revision": n}` where the list stood), put back
 * together from the lists the handle holds, so the generated serializers decode the tree Swift presented: a `KeyedList` as `{"revision": n, "rows": [...]}`.
 */
public object KeyedListTree {
    private const val LIST = "\$list"
    private const val REVISION = "revision"

    /** Every list the tree names, with the revision it was drawn with. */
    public fun references(tree: JsonElement): Map<String, Long> = buildMap { collect(tree, this) }

    private fun collect(element: JsonElement, into: MutableMap<String, Long>) {
        when (element) {
            is JsonObject -> reference(element)?.let { (name, revision) -> into[name] = revision } ?: element.values.forEach { collect(it, into) }
            is JsonArray -> element.forEach { collect(it, into) }
            else -> Unit
        }
    }

    private fun reference(element: JsonObject): Pair<String, Long>? {
        if (element.size != 2) return null
        val name = (element[LIST] as? JsonPrimitive)?.takeIf { it.isString }?.content ?: return null
        val revision = (element[REVISION] as? JsonPrimitive)?.longOrNull ?: return null
        return name to revision
    }

    /**
     * The tree with each reference replaced by its list's rows, or null while a list it names is missing or at another revision: a frame's lists come before
     * its tree, so they agree once the tree arrives, and a delta that arrived since belongs to the next tree.
     */
    public fun resolve(tree: JsonElement, lists: Map<String, List<ListRow>>, revisions: Map<String, Long>): JsonElement? {
        for ((name, revision) in references(tree)) {
            if (revisions[name] != revision || name !in lists) return null
        }
        return put(tree, lists)
    }

    private fun put(element: JsonElement, lists: Map<String, List<ListRow>>): JsonElement =
        when (element) {
            is JsonObject ->
                reference(element)?.let { (name, revision) ->
                    JsonObject(mapOf(REVISION to JsonPrimitive(revision), "rows" to JsonArray(lists.getValue(name).map { it.value })))
                } ?: JsonObject(element.mapValues { put(it.value, lists) })
            is JsonArray -> JsonArray(element.map { put(it, lists) })
            else -> element
        }
}
