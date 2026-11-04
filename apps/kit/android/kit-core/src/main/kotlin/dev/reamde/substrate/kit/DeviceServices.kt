package dev.reamde.substrate.kit

import dev.reamde.substrate.kit.bridge.DeviceRequest
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.serialization.json.JsonElement

/**
 * What answers Swift's device calls: kit-device's services on a device,
 * fakes in a test. kit-core holds no Android service itself
 * (`mise run lint:apps`).
 */
public fun interface DeviceServices {
    public fun answer(request: DeviceRequest): DeviceAnswer
}

/** How a call is answered, and on which thread Android wants it run. */
public sealed interface DeviceAnswer {
    /**
     * One value, `reply`; a throw is an `error` of kind `kotlinException`,
     * or the failure a `BridgeException` carries. `discard` gets a value
     * made that Swift never took (the call was cancelled, or Swift refused
     * the reply): what the value owns, a file, is then this side's to delete.
     */
    public class Value(
        public val dispatcher: CoroutineDispatcher = Dispatchers.IO,
        public val discard: (JsonElement) -> Unit = {},
        public val block: suspend () -> JsonElement,
    ) : DeviceAnswer

    /**
     * Values in order, `chunk` by `chunk`, then `end`; a throw is an `error`,
     * the failure a `BridgeException` carries (an `overflow`) or else a
     * `kotlinException`.
     */
    public class Chunks(
        public val dispatcher: CoroutineDispatcher = Dispatchers.IO,
        public val values: Flow<JsonElement>,
    ) : DeviceAnswer

    /** No service here takes the call: an `error` of kind `unsupported`. */
    public class Unsupported(public val reason: String) : DeviceAnswer
}

/** Answers nothing: the services before kit-device installs its own. */
public val NoDeviceServices: DeviceServices = DeviceServices { request ->
    DeviceAnswer.Unsupported("no device service answers ${request::class.simpleName}")
}
