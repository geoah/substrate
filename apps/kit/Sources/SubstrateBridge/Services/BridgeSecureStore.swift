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
/// main thread. The answer wakes it from the thread that delivers it
/// (DeviceChannel.callBlocking), so callers blocked on Swift's executors
/// never wait for one of those executors to come free.
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
        channel.callBlocking(request, timeout: timeout).flatMap { body in
            Result { try BridgeCodec.decode(Answer.self, from: body) }
        }
    }
}
