import Foundation
import SubstrateDevice

/// The device's push registration as a test sets it: Apple's token, or the
/// refusal the system answers with, and the environment the build's
/// signature names. Every field is read and written under `lock`.
public final class ScriptedPush: PushRegistration, @unchecked Sendable {
    private let lock = NSLock()
    private var token: String?
    private var named: PushEnvironment?
    private var asked = 0

    public let deviceName: String
    public let platform = "ios"
    public let topic: String

    public init(
        token: String? = String(repeating: "ab", count: 32), environment: PushEnvironment? = .development, deviceName: String = "iPhone",
        topic: String = "dev.reamde.substrate.chat"
    ) {
        self.token = token
        named = environment
        self.deviceName = deviceName
        self.topic = topic
    }

    /// Apple answers this token from now on; nil refuses.
    public func set(token: String?) { lock.withLock { self.token = token } }

    public func set(environment: PushEnvironment?) { lock.withLock { named = environment } }

    /// How often the app asked for Apple's token.
    public var asks: Int { lock.withLock { asked } }

    public func deviceToken() async throws -> String {
        try lock.withLock {
            asked += 1
            guard let token else { throw URLError(.notConnectedToInternet) }
            return token
        }
    }

    public func environment() -> PushEnvironment? { lock.withLock { named } }
}
