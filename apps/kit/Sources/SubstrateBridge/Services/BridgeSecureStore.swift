import Foundation
import SubstrateCore
import SubstrateDevice

/// The secure store on Android: a Tink keyset wrapped by an Android Keystore
/// key, and DataStore holding only ciphertext, both under
/// `noBackupFilesDir`, through the bridge. A store whose key the device
/// dropped, or that would not decrypt, is cleared by Kotlin and reads as
/// missing, so the app signs in again.
///
/// `SessionStorage` is synchronous, so a call blocks its thread until Kotlin
/// answers, which it does off the main thread: never call it on Android's
/// main thread.
public struct BridgeSecureStore: SessionStorage {
    public let channel: DeviceChannel
    /// The store's name: one DataStore file and one keyset per name.
    public let store: String
    public var timeout: TimeInterval

    public init(channel: DeviceChannel, store: String, timeout: TimeInterval = 10) {
        self.channel = channel
        self.store = store
        self.timeout = timeout
    }

    /// The `failed` status of a store that did not answer, or answered
    /// unavailable: not a Keychain status, and never zero.
    public static let unavailableStatus: Int32 = -1

    public func read(account: String) -> StoredItem {
        switch wait(.secureRead(store: store, account: account), as: SecureItem.self) {
        case let .success(.found(value)):
            guard let data = Data(base64Encoded: value) else { return .failed(Self.unavailableStatus) }
            return .found(data)
        case .success(.missing), .success(.reset):
            return .missing
        case .success(.unavailable), .failure:
            return .failed(Self.unavailableStatus)
        }
    }

    public func write(_ data: Data, account: String) throws {
        let outcome = wait(.secureWrite(store: store, account: account, value: data.base64EncodedString()), as: SecureOutcome.self)
        guard case .success(.done) = outcome else { throw SecureStoreRefused(status: Self.unavailableStatus) }
    }

    public func delete(account: String) {
        _ = wait(.secureDelete(store: store, account: account), as: SecureOutcome.self)
    }

    private func wait<Answer: Decodable & Sendable>(_ request: DeviceRequest, as _: Answer.Type) -> Result<Answer, Error> {
        let box = ResultBox<Answer>()
        let task = Task.detached { [channel] in
            do {
                box.set(.success(try await channel.call(request, as: Answer.self)))
            } catch {
                box.set(.failure(error))
            }
        }
        guard box.wait(timeout: timeout) else {
            task.cancel()
            return .failure(BridgeFailure(.unavailable, "the secure store did not answer in time"))
        }
        return box.result ?? .failure(BridgeFailure(.unavailable, "the secure store did not answer"))
    }
}

private final class ResultBox<Value: Sendable>: @unchecked Sendable {
    private let semaphore = DispatchSemaphore(value: 0)
    private let lock = NSLock()
    private var value: Result<Value, Error>?

    func set(_ result: Result<Value, Error>) {
        lock.withLock { value = result }
        semaphore.signal()
    }

    func wait(timeout: TimeInterval) -> Bool {
        semaphore.wait(timeout: .now() + timeout) == .success
    }

    var result: Result<Value, Error>? { lock.withLock { value } }
}
