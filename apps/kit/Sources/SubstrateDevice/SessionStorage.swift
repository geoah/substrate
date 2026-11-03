import Foundation

/// One item read from a secure store, telling an item that is not there from
/// a store that could not answer.
public enum StoredItem: Sendable {
    case found(Data)
    case missing
    /// The store refused (a Keychain locked before the first unlock, say),
    /// or the item is not what was asked for: not the same as missing.
    case failed(Int32)
}

/// When a secure store's items can be read, and whether they may leave the
/// device.
public enum SecureAccessibility: Sendable {
    case afterFirstUnlock
    case afterFirstUnlockThisDeviceOnly
}

/// A secure store refused to keep an item. Without it the next launch, or a
/// background run, would find no sign-in.
public struct SecureStoreRefused: LocalizedError, Sendable {
    public let status: Int32

    public init(status: Int32) {
        self.status = status
    }

    public var errorDescription: String? {
        "This device could not keep your sign-in (Keychain status \(status)). Unlock it and sign in again."
    }
}

/// Where secrets are kept by account: the Keychain on an Apple device, memory
/// in a test. A write that fails throws (`SecureStoreRefused`) and leaves the item
/// before it, as the Keychain's does.
public protocol SessionStorage: Sendable {
    func read(account: String) -> StoredItem
    func write(_ data: Data, account: String) throws
    func delete(account: String)
}
