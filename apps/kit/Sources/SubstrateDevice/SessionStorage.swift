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

/// A secure store could not remove an item, which is still there: a sign-out
/// that reported success would leave the token for the next launch to find.
public struct SecureStoreNotRemoved: LocalizedError, Sendable {
    public let status: Int32

    public init(status: Int32) {
        self.status = status
    }

    public var errorDescription: String? {
        "This device could not remove your sign-in (Keychain status \(status)), so you are still signed in. Unlock it and try again."
    }
}

/// Where secrets are kept by account: the Keychain on an Apple device, the
/// bridge's store on Android, memory in a test. Every call may suspend, so a
/// store that answers from another thread (Android's, over the bridge) never
/// holds one of Swift's executors while it waits. A write that fails throws
/// (`SecureStoreRefused`) and leaves the item before it, as the Keychain's
/// does; a removal that fails throws (`SecureStoreNotRemoved`) and leaves the
/// item, and one of an item already missing succeeds.
public protocol SessionStorage: Sendable {
    /// Where the items are, as text: two storages at one location hold the
    /// same items, so a session store serialises its transitions on it.
    var location: String { get }
    func read(account: String) async -> StoredItem
    func write(_ data: Data, account: String) async throws
    func delete(account: String) async throws
}

/// A secure store that answers on the calling thread, never waiting on
/// another thread to answer it: the Keychain, and memory in a test. Code
/// that cannot suspend (an Apple app's view, a lock's critical section)
/// reads one there. Android's store answers over the bridge and is not one.
public protocol ImmediateSessionStorage: SessionStorage {
    func read(account: String) -> StoredItem
    func write(_ data: Data, account: String) throws
    func delete(account: String) throws
}
