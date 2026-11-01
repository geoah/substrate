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
    /// Where the items are: the Keychain under the attributes above, unless
    /// the store was made over storage of the caller's.
    private let storage: (any SessionStorage)?

    public init(service: String, accessGroup: String? = nil, account: String = "session",
                accessibility: Accessibility = .afterFirstUnlockThisDeviceOnly, dataProtection: Bool = false)
    {
        self.service = service
        self.accessGroup = accessGroup
        self.account = account
        self.accessibility = accessibility
        self.dataProtection = dataProtection
        storage = nil
    }

    /// A store over `storage` instead of the Keychain, such as a test's that
    /// refuses to write.
    public init(service: String, account: String = "session", storage: any SessionStorage) {
        self.service = service
        accessGroup = nil
        self.account = account
        accessibility = .afterFirstUnlockThisDeviceOnly
        dataProtection = false
        self.storage = storage
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
        delete(account: account)
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
            delete(account: revocationsAccount)
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

    /// Saves `session` in place of `old`, the session in use where there is
    /// one. `old` is kept for revocation first, since the save overwrites the
    /// only other copy of its token; where the Keychain will not keep it, it
    /// is revoked here, and one neither kept nor revoked stays the session in
    /// use (`NotRetired`). A session this store does not save is revoked,
    /// since nothing else holds its token. A kept `old` waits for
    /// `revokePending`. Where the Keychain refuses both writes, `old` was
    /// revoked to retire it and stays the session in use: its next request
    /// is refused and the app signs in again, which loses nothing.
    public func save(_ session: Session, replacing old: Session?,
                     transport: any HTTPTransport = URLSessionTransport.shared) async throws
    {
        if let old {
            do {
                try await retire(old, transport: transport)
            } catch {
                try? await session.revoke(transport: transport)
                throw error
            }
        }
        do {
            try save(session)
        } catch {
            if let old { revoked(old) }
            try? await session.revoke(transport: transport)
            throw error
        }
    }

    /// Stops using `session` on this device: kept for revocation, then
    /// forgotten, so a revocation cut short is tried again at the next launch
    /// (`revokePending`). Where the Keychain will not keep it, it is revoked
    /// first and forgotten only once the server says so: one neither kept nor
    /// revoked stays signed in (`NotRetired`), since nothing else would hold
    /// its token.
    public func signOut(_ session: Session, transport: any HTTPTransport = URLSessionTransport.shared) async throws {
        try await retire(session, transport: transport)
        clear()
    }

    /// Kept for revocation, or else revoked now: never neither.
    private func retire(_ session: Session, transport: any HTTPTransport) async throws {
        do {
            try keepForRevocation(session)
        } catch {
            do {
                try await session.revoke(transport: transport)
            } catch let refused {
                throw NotRetired(session: session, keeping: error, revoking: refused)
            }
        }
    }

    /// A token this device would stop using that it could neither keep to
    /// revoke later nor revoke now: it stays in use, since nothing else would
    /// hold it.
    public struct NotRetired: LocalizedError, Sendable {
        public let session: Session
        /// Why the Keychain did not keep it.
        public let keeping: Error
        /// Why the server did not confirm revoking it.
        public let revoking: Error

        public var errorDescription: String? {
            let host = ServerAddress.display(session.server)
            return "This device couldn't keep the sign-in to \(host) to revoke later, and \(host) didn't confirm revoking it now, "
                + "so it stays in use here. Try again once \(host) answers."
        }
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
        if let storage { return storage.read(account: account) }
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
        if let storage { return try storage.write(data, account: account) }
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

    public func delete(account: String) {
        if let storage { return storage.delete(account: account) }
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
}

/// Where a `SessionStore` keeps its items in place of the Keychain. A write
/// that fails throws and leaves the item before it, as the Keychain's does.
public protocol SessionStorage: Sendable {
    func read(account: String) -> SessionStore.Item
    func write(_ data: Data, account: String) throws
    func delete(account: String)
}
