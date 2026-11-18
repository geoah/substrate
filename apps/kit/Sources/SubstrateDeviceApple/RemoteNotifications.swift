#if os(iOS)
import Foundation
import SubstrateDevice
import UIKit

/// Pushes on iPhone and iPad: Apple's token through
/// `registerForRemoteNotifications`, whose answer arrives at the app
/// delegate, which hands it here (`registered(_:)`, `failed(_:)`); the
/// environment from the build's own signature, read at run time, never a
/// build setting, since an export re-signs the app and replaces the
/// entitlement.
public final class RemoteNotifications: PushRegistration, @unchecked Sendable {
    /// The one instance the app delegate answers into.
    @MainActor public static var shared: RemoteNotifications?

    public let topic: String
    public let platform = "ios"
    public let deviceName: String

    private let lock = NSLock()
    private var waiting: [UUID: CheckedContinuation<String, any Error>] = [:]

    /// How long Apple's answer is waited for.
    static let wait: Duration = .seconds(30)

    @MainActor
    public init(topic: String) {
        self.topic = topic
        // The system's own word for the device: an app without Apple's
        // entitlement for the person's name reads "iPhone" or "iPad".
        deviceName = UIDevice.current.name
        Self.shared = self
    }

    public func deviceToken() async throws -> String {
        try await withThrowingTaskGroup(of: String.self) { group in
            group.addTask {
                let id = UUID()
                return try await withTaskCancellationHandler {
                    try await withCheckedThrowingContinuation { continuation in
                        self.lock.withLock { self.waiting[id] = continuation }
                        // Cancelled before it was kept: the handler found
                        // nothing to answer.
                        if Task.isCancelled {
                            self.lock.withLock { self.waiting.removeValue(forKey: id) }?.resume(throwing: CancellationError())
                            return
                        }
                        Task { @MainActor in UIApplication.shared.registerForRemoteNotifications() }
                    }
                } onCancel: {
                    self.lock.withLock { self.waiting.removeValue(forKey: id) }?.resume(throwing: CancellationError())
                }
            }
            group.addTask {
                try await Task.sleep(for: Self.wait)
                throw URLError(.timedOut)
            }
            defer { group.cancelAll() }
            guard let token = try await group.next() else { throw URLError(.timedOut) }
            return token
        }
    }

    /// The app delegate's `didRegisterForRemoteNotificationsWithDeviceToken`.
    public func registered(_ token: Data) {
        answer(.success(token.map { String(format: "%02x", $0) }.joined()))
    }

    /// The app delegate's `didFailToRegisterForRemoteNotificationsWithError`.
    public func failed(_ error: any Error) {
        answer(.failure(error))
    }

    private func answer(_ result: Result<String, any Error>) {
        let continuations = lock.withLock {
            defer { waiting = [:] }
            return Array(waiting.values)
        }
        for continuation in continuations { continuation.resume(with: result) }
    }

    public func environment() -> PushEnvironment? {
        #if targetEnvironment(simulator)
        PushEnvironment.of(profile: nil, simulator: true)
        #else
        PushEnvironment.of(
            profile: Bundle.main.url(forResource: "embedded", withExtension: "mobileprovision").flatMap { try? Data(contentsOf: $0) },
            simulator: false)
        #endif
    }
}
#endif
