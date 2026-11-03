import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice

/// The session in the device's secure store, under one account of it. Where
/// and how the items are kept (the Keychain's service, access group and
/// accessibility on an Apple device) is the storage's alone
/// (`KeychainStorage`), so two stores over one storage share its items.
public struct SessionStore: Sendable {
    public typealias Accessibility = SecureAccessibility
    public typealias Item = StoredItem
    public typealias NotSaved = SecureStoreRefused

    public let account: String
    /// Where the items are.
    public let storage: any SessionStorage

    public init(account: String = "session", storage: any SessionStorage) {
        self.account = account
        self.storage = storage
    }

    public enum Read: Sendable {
        case found(Session)
        case missing
        /// The Keychain refused (locked before the first unlock, say), or the
        /// item is not a session: not the same as signed out.
        case failed(Int32)
    }

    /// The status a stored item that is not a session reads as: the
    /// Keychain's `errSecDecode`, whatever the store.
    static let undecodable: Int32 = -26275

    /// The session, saying whether it is absent or could not be read.
    public func read() -> Read {
        switch data(account: account) {
        case .found(let data):
            guard let session = try? JSONDecoder().decode(Session.self, from: data) else { return .failed(Self.undecodable) }
            return .found(session)
        case .missing: return .missing
        case .failed(let status): return .failed(status)
        }
    }

    public func load() -> Session? {
        if case .found(let session) = read() { return session }
        return nil
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
        try await SessionTransitions.shared.run(key) {
            if let old {
                do {
                    try await retire(old, transport: transport)
                } catch {
                    await discard(session, transport: transport)
                    throw error
                }
            }
            do {
                try save(session)
            } catch {
                if let old { revoked(old) }
                await discard(session, transport: transport)
                throw error
            }
        }
    }

    /// Stops using `session` on this device: kept for revocation, then
    /// forgotten, so a revocation cut short is tried again at the next launch
    /// (`revokePending`). Where the Keychain will not keep it, it is revoked
    /// first and forgotten only once the server says so: one neither kept nor
    /// revoked stays signed in (`NotRetired`), since nothing else would hold
    /// its token.
    public func signOut(_ session: Session, transport: any HTTPTransport = URLSessionTransport.shared) async throws {
        try await SessionTransitions.shared.run(key) {
            try await retire(session, transport: transport)
            // Only the session this sign-out retired: one saved in its
            // place is another transition's, kept or revoked by it.
            if case .found(let stored) = read(), stored.secret != session.secret { return }
            clear()
        }
    }

    /// A token this device never came to use, such as a replacement whose
    /// save failed: kept for revocation before it is revoked, so a server
    /// that does not answer leaves it for `revokePending` rather than
    /// nowhere.
    private func discard(_ session: Session, transport: any HTTPTransport) async {
        let kept = (try? keepForRevocation(session)) != nil
        guard (try? await session.revoke(transport: transport)) != nil else { return }
        if kept { revoked(session) }
    }

    /// What the store's transitions are serialised on: one item, wherever
    /// the store value was made.
    private var key: String { "\(storage.location)|\(account)" }

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

    /// Another item of the app's beside the session, such as an install id
    /// that outlives a sign-out.
    public func data(account: String) -> Item {
        storage.read(account: account)
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
        try storage.write(data, account: account)
    }

    public func delete(account: String) {
        storage.delete(account: account)
    }
}

/// Runs one store's session transitions one after another. A sign-out that
/// waits on the server to revoke would otherwise clear a session a
/// replacement saved meanwhile.
actor SessionTransitions {
    static let shared = SessionTransitions()

    private var tails: [String: Task<Void, Never>] = [:]

    func run<T: Sendable>(_ key: String, _ operation: @escaping @Sendable () async throws -> T) async throws -> T {
        let previous = tails[key]
        let task = Task<T, Error> {
            await previous?.value
            return try await operation()
        }
        tails[key] = Task { _ = try? await task.value }
        return try await task.value
    }
}
