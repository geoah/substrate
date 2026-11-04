import Foundation
import SubstrateCore
import SubstrateDevice

/// The secure store on Android: a Tink keyset wrapped by an Android Keystore
/// key, and DataStore holding only ciphertext, both under
/// `noBackupFilesDir`, through the bridge. A store whose key the device
/// dropped, or that would not decrypt, is cleared by Kotlin and reads as
/// missing, so the app signs in again.
///
/// Every call suspends until Kotlin answers, holding no thread: a caller on
/// one of Swift's executors gives it back while it waits, so however many
/// wait at once, the answers never queue behind them.
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

    /// One DataStore file and keyset per store name, in this app's
    /// `noBackupFilesDir`: two storages of one name hold the same items.
    public var location: String { "android-secure-store:\(store)" }

    public func read(account: String) async -> StoredItem {
        switch await answer(.secureRead(store: store, account: account), as: SecureItem.self) {
        case let .success(.found(value)):
            guard let data = Data(base64Encoded: value) else { return .failed(Self.unavailableStatus) }
            return .found(data)
        case .success(.missing), .success(.reset):
            return .missing
        case .success(.unavailable), .failure:
            return .failed(Self.unavailableStatus)
        }
    }

    public func write(_ data: Data, account: String) async throws {
        let outcome = await answer(.secureWrite(store: store, account: account, value: data.base64EncodedString()), as: SecureOutcome.self)
        guard case .success(.done) = outcome else { throw SecureStoreRefused(status: Self.unavailableStatus) }
    }

    public func delete(account: String) async throws {
        let outcome = await answer(.secureDelete(store: store, account: account), as: SecureOutcome.self)
        guard case .success(.done) = outcome else { throw SecureStoreNotRemoved(status: Self.unavailableStatus) }
    }

    /// Kotlin's answer, or a failure once `timeout` passes without one: the
    /// call is cancelled on both sides then, as a cancelled caller's is.
    private func answer<Answer: Decodable & Sendable>(_ request: DeviceRequest, as _: Answer.Type) async -> Result<Answer, Error> {
        let (channel, timeout) = (channel, timeout)
        return await withTaskGroup(of: Result<Answer, Error>?.self) { group in
            group.addTask {
                do {
                    return .success(try await channel.call(request, as: Answer.self))
                } catch {
                    return .failure(error)
                }
            }
            group.addTask {
                try? await Task.sleep(for: .seconds(timeout))
                return nil
            }
            let first = await group.next() ?? nil
            group.cancelAll()
            return first ?? .failure(BridgeFailure(.unavailable, "the secure store did not answer in \(Int(timeout)) seconds"))
        }
    }
}
