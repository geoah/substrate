import Foundation
import Security

/// The session in the Keychain: one generic password item per app, readable
/// after the first unlock so background work (a sync, a refresh) can use it,
/// never synced off the device by default, and shared with the app's other
/// targets (a share sheet, a widget) through an access group.
public struct SessionStore: Sendable {
    public enum Accessibility: Sendable {
        case afterFirstUnlock
        case afterFirstUnlockThisDeviceOnly

        var attribute: CFString {
            switch self {
            case .afterFirstUnlock: kSecAttrAccessibleAfterFirstUnlock
            case .afterFirstUnlockThisDeviceOnly: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            }
        }
    }

    public let service: String
    /// The `keychain-access-groups` entry the app's targets share, in full
    /// (`<team>.dev.reamde.substrate.sync`); nil is the app's default group.
    public let accessGroup: String?
    public let account: String
    public let accessibility: Accessibility
    /// On macOS, the data protection keychain, which an access group needs.
    /// Off keeps the item where a build without it left one.
    public let dataProtection: Bool

    public init(service: String, accessGroup: String? = nil, account: String = "session",
                accessibility: Accessibility = .afterFirstUnlockThisDeviceOnly, dataProtection: Bool = false)
    {
        self.service = service
        self.accessGroup = accessGroup
        self.account = account
        self.accessibility = accessibility
        self.dataProtection = dataProtection
    }

    public enum Read: Sendable {
        case found(Session)
        case missing
        /// The Keychain refused (locked before the first unlock, say), or the
        /// item is not a session: not the same as signed out.
        case failed(OSStatus)
    }

    /// The session, saying whether it is absent or could not be read.
    public func read() -> Read {
        switch data(account: account) {
        case .found(let data):
            guard let session = try? JSONDecoder().decode(Session.self, from: data) else { return .failed(errSecDecode) }
            return .found(session)
        case .missing: return .missing
        case .failed(let status): return .failed(status)
        }
    }

    public func load() -> Session? {
        if case .found(let session) = read() { return session }
        return nil
    }

    /// A sign-in the Keychain did not keep is not a sign-in: the next launch,
    /// or a background run, would find none.
    public struct NotSaved: LocalizedError, Sendable {
        public let status: OSStatus
        public var errorDescription: String? {
            "This device could not keep your sign-in (Keychain status \(status)). Unlock it and sign in again."
        }
    }

    public func save(_ session: Session) throws {
        try write(try JSONEncoder().encode(session), account: account)
    }

    /// Forgets the session only; the app's other items stay.
    public func clear() {
        SecItemDelete(query(account: account) as CFDictionary)
    }

    // MARK: Revocations

    /// Where the tokens waiting to be revoked are kept, beside the session.
    var revocationsAccount: String { account + ".revocations" }

    /// Tokens this device stopped using whose revocation the server has not
    /// confirmed: kept, secret and id, since nothing else on the device
    /// holds them once a new session is saved over the old.
    public func pendingRevocations() -> [Session] {
        guard case .found(let data) = data(account: revocationsAccount) else { return [] }
        return (try? JSONDecoder().decode([Session].self, from: data)) ?? []
    }

    /// Keeps a token to revoke until `revoked(_:)` forgets it. Called before
    /// the session that replaces it is saved, so no moment holds neither.
    public func keepForRevocation(_ session: Session) throws {
        let pending = pendingRevocations().filter { $0.secret != session.secret } + [session]
        try write(try JSONEncoder().encode(pending), account: revocationsAccount)
    }

    /// Forgets a token the server revoked, or one still in use.
    public func revoked(_ session: Session) {
        let pending = pendingRevocations().filter { $0.secret != session.secret }
        if pending.isEmpty {
            SecItemDelete(query(account: revocationsAccount) as CFDictionary)
        } else if let data = try? JSONEncoder().encode(pending) {
            try? write(data, account: revocationsAccount)
        }
    }

    /// Revokes every token kept for revocation, forgetting each the server
    /// revoked, and answers the ones still live, which stay kept for the next
    /// try. The session in use is never revoked: one kept beside it is a
    /// replacement whose save never landed.
    public func revokePending(current: Session?, transport: any HTTPTransport = URLSessionTransport.shared) async -> [Session] {
        var live: [Session] = []
        for old in pendingRevocations() {
            if old.secret == current?.secret {
                revoked(old)
                continue
            }
            do {
                try await old.revoke(transport: transport)
                revoked(old)
            } catch {
                live.append(old)
            }
        }
        return live
    }

    /// Stops using `session` on this device: kept for revocation first, then
    /// forgotten, so a revocation cut short is tried again at the next
    /// launch (`revokePending`). Answers whether it was kept; one the
    /// Keychain would not keep is the caller's to revoke now or never.
    @discardableResult
    public func signOut(_ session: Session) -> Bool {
        let kept = (try? keepForRevocation(session)) != nil
        clear()
        return kept
    }

    // MARK: Other items

    public enum Item: Sendable {
        case found(Data)
        case missing
        case failed(OSStatus)
    }

    /// Another item of the app's beside the session, such as an install id
    /// that outlives a sign-out.
    public func data(account: String) -> Item {
        var q = query(account: account)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var item: CFTypeRef?
        let status = SecItemCopyMatching(q as CFDictionary, &item)
        switch status {
        case errSecSuccess: return (item as? Data).map(Item.found) ?? .failed(status)
        case errSecItemNotFound: return .missing
        default: return .failed(status)
        }
    }

    public func string(account: String) -> String? {
        if case .found(let data) = data(account: account) { return String(data: data, encoding: .utf8) }
        return nil
    }

    public func setString(_ value: String, account: String) throws {
        try write(Data(value.utf8), account: account)
    }

    /// Replaces the item in place, so a write that fails leaves the one
    /// before it rather than none.
    public func write(_ data: Data, account: String) throws {
        let match = query(account: account)
        let attributes: [String: Any] = [
            kSecAttrAccessible as String: accessibility.attribute,
            kSecValueData as String: data,
        ]
        var status = SecItemUpdate(match as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            status = SecItemAdd(match.merging(attributes) { a, _ in a } as CFDictionary, nil)
        }
        guard status == errSecSuccess else { throw NotSaved(status: status) }
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
}
