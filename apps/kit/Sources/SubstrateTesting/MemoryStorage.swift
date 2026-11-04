import Foundation
import SubstrateDevice

/// A secure store in memory that refuses to write the accounts in
/// `refusing`, as a Keychain locked before the first unlock does, and fails
/// what a test tells it to (`failReads`, `failWrites`, `failDeletes`). Every
/// field is read and written under `lock`.
public final class MemoryStorage: ImmediateSessionStorage, @unchecked Sendable {
    /// The Keychain's `errSecInteractionNotAllowed`, which a locked device
    /// answers a write with.
    public static let locked: Int32 = -25308

    private let lock = NSLock()
    private var items: [String: Data] = [:]
    private var failingReads: [String: Int32] = [:]
    private var failingWrites: [String: Int32] = [:]
    private var failingDeletes: [String: Int32] = [:]
    public let refusing: Set<String>
    /// Each instance's own: two never share an item.
    public let location = "memory|" + UUID().uuidString

    public init(refusing: Set<String> = []) {
        self.refusing = refusing
    }

    public func read(account: String) -> StoredItem {
        lock.withLock {
            if let status = failingReads[account] { return .failed(status) }
            return items[account].map(StoredItem.found) ?? .missing
        }
    }

    public func write(_ data: Data, account: String) throws {
        guard !refusing.contains(account) else { throw SecureStoreRefused(status: Self.locked) }
        try lock.withLock {
            if let status = failingWrites[account] { throw SecureStoreRefused(status: status) }
            items[account] = data
        }
    }

    /// An item as an earlier run left it, whatever this storage refuses now.
    public func writeUnchecked(_ data: Data, account: String) {
        lock.withLock { items[account] = data }
    }

    public func delete(account: String) throws {
        try lock.withLock {
            if let status = failingDeletes[account] { throw SecureStoreNotRemoved(status: status) }
            items[account] = nil
        }
    }

    /// The item as it is held, whatever this storage fails now.
    public func stored(account: String) -> Data? {
        lock.withLock { items[account] }
    }

    /// Every read of `account` answers `failed(status)` until this is called
    /// again with nil: a Keychain locked, an item another build wrote.
    public func failReads(of account: String, with status: Int32?) {
        lock.withLock { failingReads[account] = status }
    }

    /// Every write of `account` is refused with `status` until this is
    /// called again with nil.
    public func failWrites(of account: String, with status: Int32?) {
        lock.withLock { failingWrites[account] = status }
    }

    /// Every removal of `account` fails with `status`, leaving the item,
    /// until this is called again with nil.
    public func failDeletes(of account: String, with status: Int32?) {
        lock.withLock { failingDeletes[account] = status }
    }
}
