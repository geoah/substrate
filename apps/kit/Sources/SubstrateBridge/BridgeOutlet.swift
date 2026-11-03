import Foundation

/// Where Swift's messages go: Kotlin's `deliver` through JNI on Android, a
/// list in a test.
public protocol BridgeOutlet: Sendable {
    /// Hands one message over, and keeps the call short. Never throws: a
    /// message that could not be handed over is the failure returned, which
    /// the bridge records.
    func deliver(_ message: String) -> BridgeFailure?
}
