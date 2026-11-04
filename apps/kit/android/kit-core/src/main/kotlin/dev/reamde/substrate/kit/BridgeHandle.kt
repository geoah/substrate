package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.BridgeFailure
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.Handle
import dev.reamde.substrate.kit.bridge.ListRow
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.mapNotNull
import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.JsonElement

/**
 * What a handle shows now: its newest presentation as it crossed, each keyed list with every delta so far applied and its revision, whether it is closed, and
 * the last failure Swift reported for it. `tree` is the presentation with its lists' rows put back (`KeyedListTree`), as of the last time the lists agreed with
 * it: what a screen draws.
 */
public data class HandleSnapshot(
    val presentation: JsonElement? = null,
    val lists: Map<String, List<ListRow>> = emptyMap(),
    val closed: Boolean = false,
    val problem: BridgeFailure? = null,
    val revisions: Map<String, Long> = emptyMap(),
    val tree: JsonElement? = null,
) {
    /** This snapshot with `tree` made again where the lists agree with the presentation. */
    internal fun rebuilt(): HandleSnapshot {
        val presentation = presentation ?: return this
        return KeyedListTree.resolve(presentation, lists, revisions)?.let { copy(tree = it) } ?: this
    }
}

/** One open root. Its state is a StateFlow: a slow collector sees the newest. */
public class BridgeHandle
internal constructor(
    public val handle: Handle,
    public val state: StateFlow<HandleSnapshot>,
    private val core: BridgeCore,
) {
    /** Sends a command, in order with every other sent through this handle. */
    public fun send(command: JsonElement): Result<Unit> = core.command(handle, command)

    public fun <T> send(serializer: KSerializer<T>, command: T): Result<Unit> = send(BridgeJson.encodeToJsonElement(serializer, command))

    /** Idempotent. Swift answers what this handle still waits on `cancelled`. */
    public fun close(): Result<Unit> = core.close(handle)

    /** Each presentation, its keyed lists' rows in place, decoded. */
    public fun <T> presentations(serializer: KSerializer<T>): Flow<T> =
        state.map { it.tree }.distinctUntilChanged().mapNotNull { tree -> tree?.let { BridgeJson.decodeFromJsonElement(serializer, it) } }
}
