import Foundation
import Security

/// The session: the server, the bearer secret, the token's id (so sign-out
/// revokes the very token it forgets) and the repository's authority (the
/// webhook path names it). Kept in the Keychain, readable after the first
/// unlock so a background sync can use it.
struct Session: Codable, Equatable, Sendable {
    var server: URL
    var repository: String
    var secret: String
    var tokenID: String

    /// Which repository on which server: what the local state is bound to.
    var identity: String { server.absoluteString + "|" + repository }
}

enum Keychain {
    private static let service = "dev.reamde.substrate.sync"
    private static let account = "session"

    static func load() -> Session? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess, let data = item as? Data else {
            return nil
        }
        return try? JSONDecoder().decode(Session.self, from: data)
    }

    /// A sign-in the Keychain did not keep is not a sign-in: the next
    /// launch, or a background sync, would find none.
    struct NotSaved: LocalizedError {
        let status: OSStatus
        var errorDescription: String? { "This phone could not keep your sign-in (Keychain status \(status)). Unlock the phone and sign in again." }
    }

    static func save(_ session: Session) throws {
        try write(try JSONEncoder().encode(session), account: account)
    }

    /// Replaces the item in place, so a write that fails leaves the one
    /// before it rather than none.
    private static func write(_ data: Data, account: String) throws {
        let match: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        let attributes: [String: Any] = [
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
            kSecValueData as String: data,
        ]
        var status = SecItemUpdate(match as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            status = SecItemAdd(match.merging(attributes) { a, _ in a } as CFDictionary, nil)
        }
        guard status == errSecSuccess else { throw NotSaved(status: status) }
    }

    enum Read {
        case found(Data)
        case missing
        case failed(OSStatus)
    }

    /// One item, saying whether it is absent or could not be read.
    static func read(account: String) -> Read {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        switch status {
        case errSecSuccess: return (item as? Data).map(Read.found) ?? .failed(status)
        case errSecItemNotFound: return .missing
        default: return .failed(status)
        }
    }

    /// A plain string item of this app's, beside the session.
    static func string(account: String) -> String? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess, let data = item as? Data else {
            return nil
        }
        return String(data: data, encoding: .utf8)
    }

    static func setString(_ value: String, account: String) throws {
        try write(Data(value.utf8), account: account)
    }

    /// Forgets the session only; the install id stays, so signing in again
    /// keeps naming the same address book, event store and Health account.
    static func clear() {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        SecItemDelete(query as CFDictionary)
    }
}
