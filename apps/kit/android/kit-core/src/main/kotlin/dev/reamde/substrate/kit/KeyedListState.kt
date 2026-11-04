package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.ListMessage
import dev.reamde.substrate.kit.bridge.ListRow

/**
 * One keyed list as Kotlin holds it. Deltas apply in revision order, each onto the revision it was made from; one whose base is not this list's revision asks
 * for the list whole, and deltas until the whole one arrives, and any older than it, are dropped.
 */
public class KeyedListState(public val name: String) {
    public enum class Applied {
        APPLIED,
        DROPPED,
        NEEDS_WHOLE,
    }

    public var revision: Long = 0
        private set

    private var rows = LinkedHashMap<String, ListRow>()
    private var awaitingWhole = false

    public val snapshot: List<ListRow>
        get() = rows.values.toList()

    public fun apply(message: ListMessage): Applied =
        when (message) {
            is ListMessage.Whole -> {
                if (message.revision < revision && !awaitingWhole) {
                    Applied.DROPPED
                } else {
                    rows = LinkedHashMap(message.rows.associateBy { it.id })
                    revision = message.revision
                    awaitingWhole = false
                    Applied.APPLIED
                }
            }
            is ListMessage.Delta ->
                when {
                    message.revision <= revision -> Applied.DROPPED
                    awaitingWhole -> Applied.DROPPED
                    message.base != revision -> {
                        awaitingWhole = true
                        Applied.NEEDS_WHOLE
                    }
                    else -> {
                        val changed = (message.inserted + message.changed).associateBy { it.id }
                        val next = LinkedHashMap<String, ListRow>()
                        var complete = true
                        for (id in message.order) {
                            val row = changed[id] ?: rows[id]
                            if (row == null) complete = false else next[id] = row
                        }
                        if (!complete) {
                            awaitingWhole = true
                            Applied.NEEDS_WHOLE
                        } else {
                            rows = next
                            revision = message.revision
                            Applied.APPLIED
                        }
                    }
                }
        }
}
