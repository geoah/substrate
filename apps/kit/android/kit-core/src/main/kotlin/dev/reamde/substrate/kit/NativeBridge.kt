package dev.reamde.substrate.kit

/**
 * The JNI surface (docs/plans/app-kit.md §5.5): two natives into Swift and the one method Swift calls back. The names are the contract with SubstrateBridge's
 * `Android/JNI.swift`; consumer-rules.pro keeps them.
 */
internal object NativeBridge {
    @JvmStatic external fun start(config: String): String?

    @JvmStatic external fun post(message: String): String?

    /** Every message from Swift, on whatever thread Swift is on. Never throws. */
    @JvmStatic
    fun deliver(message: String) {
        try {
            Bridge.receive(message)
        } catch (failure: Throwable) {
            // A throw here would be pending in Swift's JNI call; the bridge
            // records the failure instead, by type, never by message.
            Bridge.recordFailure("deliver threw ${failure::class.java.name}")
        }
    }
}
