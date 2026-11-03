import Foundation
import SubstrateDevice

/// A secure store in memory that refuses to write the accounts in
/// `refusing`, as a Keychain locked before the first unlock does. Every field
/// is read and written under `lock`.
public final class MemoryStorage: SessionStorage, @unchecked Sendable {
    /// The Keychain's `errSecInteractionNotAllowed`, which a locked device
    /// answers a write with.
    public static let locked: Int32 = -25308

    private let lock = NSLock()
    private var items: [String: Data] = [:]
    public let refusing: Set<String>
    /// Each instance's own: two never share an item.
    public let location = "memory|" + UUID().uuidString

    public init(refusing: Set<String> = []) {
        self.refusing = refusing
    }

    public func read(account: String) -> StoredItem {
        lock.withLock { items[account].map(StoredItem.found) ?? .missing }
    }

    public func write(_ data: Data, account: String) throws {
        guard !refusing.contains(account) else { throw SecureStoreRefused(status: Self.locked) }
        lock.withLock { items[account] = data }
    }

    /// An item as an earlier run left it, whatever this storage refuses now.
    public func writeUnchecked(_ data: Data, account: String) {
        lock.withLock { items[account] = data }
    }

    public func delete(account: String) {
        lock.withLock { items[account] = nil }
    }
}
