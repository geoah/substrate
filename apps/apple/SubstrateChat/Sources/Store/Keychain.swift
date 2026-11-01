import Foundation
import Security
import SubstrateKit

/// The session in the Keychain, readable after the first unlock so a
/// background refresh can use it, and never synced off this device.
enum Keychain {
    private static let service = "dev.reamde.substrate.chat"
    private static let account = "session"

    private static var base: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }

    static func load() -> Session? {
        var query = base
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess, let data = item as? Data else {
            return nil
        }
        return try? JSONDecoder().decode(Session.self, from: data)
    }

    struct NotSaved: LocalizedError {
        let status: OSStatus
        var errorDescription: String? {
            "This device could not keep your sign-in (Keychain status \(status)). Unlock it and sign in again."
        }
    }

    /// Replaces the item in place, so a write that fails leaves the one
    /// before it rather than none.
    static func save(_ session: Session) throws {
        let attributes: [String: Any] = [
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
            kSecValueData as String: try JSONEncoder().encode(session),
        ]
        var status = SecItemUpdate(base as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            status = SecItemAdd(base.merging(attributes) { a, _ in a } as CFDictionary, nil)
        }
        guard status == errSecSuccess else { throw NotSaved(status: status) }
    }

    static func clear() {
        SecItemDelete(base as CFDictionary)
    }
}
