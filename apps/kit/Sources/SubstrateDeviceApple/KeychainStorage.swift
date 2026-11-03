#if canImport(Security)
import Foundation
import Security
import SubstrateDevice

/// The secure store on an Apple device: generic password items under one
/// service, each account one item, in an access group the app's targets
/// share where one is named.
public struct KeychainStorage: SessionStorage {
    public let service: String
    /// The `keychain-access-groups` entry the app's targets share, in full
    /// (`<team>.dev.reamde.substrate.sync`); nil is the app's default group.
    public let accessGroup: String?
    public let accessibility: SecureAccessibility
    /// On macOS, the data protection keychain, which an access group needs.
    /// Off keeps the item where a build without it left one.
    public let dataProtection: Bool

    public init(service: String, accessGroup: String? = nil, accessibility: SecureAccessibility = .afterFirstUnlockThisDeviceOnly,
                dataProtection: Bool = false)
    {
        self.service = service
        self.accessGroup = accessGroup
        self.accessibility = accessibility
        self.dataProtection = dataProtection
    }

    public func read(account: String) -> StoredItem {
        var q = query(account: account)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var item: CFTypeRef?
        let status = SecItemCopyMatching(q as CFDictionary, &item)
        switch status {
        case errSecSuccess: return (item as? Data).map(StoredItem.found) ?? .failed(status)
        case errSecItemNotFound: return .missing
        default: return .failed(status)
        }
    }

    /// Replaces the item in place, so a write that fails leaves the one
    /// before it rather than none.
    public func write(_ data: Data, account: String) throws {
        let match = query(account: account)
        let attributes: [String: Any] = [
            kSecAttrAccessible as String: attribute,
            kSecValueData as String: data,
        ]
        var status = SecItemUpdate(match as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            status = SecItemAdd(match.merging(attributes) { a, _ in a } as CFDictionary, nil)
        }
        guard status == errSecSuccess else { throw SecureStoreRefused(status: status) }
    }

    public func delete(account: String) {
        SecItemDelete(query(account: account) as CFDictionary)
    }

    func query(account: String) -> [String: Any] {
        var q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        if let accessGroup { q[kSecAttrAccessGroup as String] = accessGroup }
        #if os(macOS)
        if dataProtection { q[kSecUseDataProtectionKeychain as String] = true }
        #endif
        return q
    }

    var attribute: CFString {
        switch accessibility {
        case .afterFirstUnlock: kSecAttrAccessibleAfterFirstUnlock
        case .afterFirstUnlockThisDeviceOnly: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        }
    }
}
#endif
