package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.BridgeFailure
import dev.reamde.substrate.kit.bridge.BridgeJson
import dev.reamde.substrate.kit.bridge.Handle
import dev.reamde.substrate.kit.bridge.ListRow
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.mapNotNull
import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.JsonElement

/**
 * What a handle shows now: its newest presentation, each keyed list with
 * every delta so far applied, whether it is closed, and the last failure
 * Swift reported for it.
 */
public data class HandleSnapshot(
    val presentation: JsonElement? = null,
    val lists: Map<String, List<ListRow>> = emptyMap(),
    val closed: Boolean = false,
    val problem: BridgeFailure? = null,
)

/** One open root. Its state is a StateFlow: a slow collector sees the newest. */
public class BridgeHandle internal constructor(
    public val handle: Handle,
    public val state: StateFlow<HandleSnapshot>,
    private val core: BridgeCore,
) {
    /** Sends a command, in order with every other sent through this handle. */
    public fun send(command: JsonElement): Result<Unit> = core.command(handle, command)

    public fun <T> send(serializer: KSerializer<T>, command: T): Result<Unit> =
        send(BridgeJson.encodeToJsonElement(serializer, command))

    /** Idempotent. Swift answers what this handle still waits on `cancelled`. */
    public fun close(): Result<Unit> = core.close(handle)

    /** Each presentation, decoded. */
    public fun <T> presentations(serializer: KSerializer<T>): Flow<T> =
        state.mapNotNull { snapshot -> snapshot.presentation?.let { BridgeJson.decodeFromJsonElement(serializer, it) } }
}
