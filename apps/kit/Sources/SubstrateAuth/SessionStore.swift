import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice

/// The session in the device's secure store, under one account of it. Where
/// and how the items are kept (the Keychain's service, access group and
/// accessibility on an Apple device) is the storage's alone
/// (`KeychainStorage`), so two stores over one storage share its items.
/// Every call suspends while the storage answers: Android's answers over the
/// bridge, and a caller waiting on it holds no thread. An Apple app's code
/// that cannot suspend reads the Keychain on its own thread through
/// SubstrateUI's synchronous reads.
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

    public enum Read: Equatable, Sendable {
        case found(Session)
        case missing
        /// The Keychain refused (locked before the first unlock, say), or the
        /// item is not a session: not the same as signed out.
        case failed(Int32)

        /// The session an item holds, or why it holds none.
        public init(_ item: StoredItem) {
            switch item {
            case .found(let data):
                guard let session = try? JSONDecoder().decode(Session.self, from: data) else {
                    self = .failed(SessionStore.undecodable)
                    return
                }
                self = .found(session)
            case .missing: self = .missing
            case .failed(let status): self = .failed(status)
            }
        }
    }

    /// The status a stored item that is not a session reads as: the
    /// Keychain's `errSecDecode`, whatever the store.
    public static let undecodable: Int32 = -26275

    /// The session, saying whether it is absent or could not be read.
    public func read() async -> Read {
        Read(await data(account: account))
    }

    /// The session, nil both when there is none and when it could not be
    /// read: never for a decision a failed read must not reach, such as
    /// whether the device is signed out (`read()`).
    public func load() async -> Session? {
        if case .found(let session) = await read() { return session }
        return nil
    }

    public func save(_ session: Session) async throws {
        try await write(try JSONEncoder().encode(session), account: account)
    }

    /// Forgets the session only; the app's other items stay. Throws, the
    /// session still kept, where the storage could not remove it.
    public func clear() async throws {
        try await delete(account: account)
    }

    // MARK: Revocations

    /// Where the tokens waiting to be revoked are kept, beside the session.
    public var revocationsAccount: String { account + ".revocations" }

    /// Tokens this device stopped using whose revocation the server has not
    /// confirmed: kept, secret and id, since nothing else on the device
    /// holds them once a new session is saved over the old. An empty list
    /// only where none are kept: a queue the storage could not read, or one
    /// that does not decode, throws (`RevocationsUnreadable`).
    public func pendingRevocations() async throws -> [Session] {
        try Self.revocations(in: await data(account: revocationsAccount))
    }

    /// The tokens a stored queue holds, or why it holds none that can be
    /// read.
    public static func revocations(in item: StoredItem) throws -> [Session] {
        switch item {
        case .found(let data):
            guard let pending = try? JSONDecoder().decode([Session].self, from: data) else {
                throw RevocationsUnreadable(status: undecodable)
            }
            return pending
        case .missing: return []
        case .failed(let status): throw RevocationsUnreadable(status: status)
        }
    }

    /// Keeps a token to revoke until `revoked(_:)` forgets it. Called before
    /// the session that replaces it is saved, so no moment holds neither.
    /// Throws, keeping nothing, where the queue cannot be read: written
    /// over, the tokens in it would stay live with nothing left to revoke
    /// them.
    public func keepForRevocation(_ session: Session) async throws {
        try await SessionTransitions.shared.run(queueKey) {
            let pending = try await pendingRevocations()
            try await write(try JSONEncoder().encode(pending.filter { $0.secret != session.secret } + [session]), account: revocationsAccount)
        }
    }

    /// Forgets a token the server revoked, or one still in use. Throws,
    /// forgetting nothing, where the queue cannot be read, written or
    /// removed.
    public func revoked(_ session: Session) async throws {
        try await SessionTransitions.shared.run(queueKey) {
            let pending = try await pendingRevocations()
            let left = pending.filter { $0.secret != session.secret }
            guard left.count < pending.count else { return }
            if left.isEmpty {
                try await delete(account: revocationsAccount)
            } else {
                try await write(try JSONEncoder().encode(left), account: revocationsAccount)
            }
        }
    }

    /// Revokes every token kept for revocation, forgetting each the server
    /// revoked, and answers the ones still live, which stay kept for the next
    /// try. The session in use is never revoked: one kept beside it is a
    /// replacement whose save never landed, or a sign-out whose removal
    /// failed, and it is forgotten from the queue instead. Which session is
    /// in use is read here, and decided between transitions, so a sign-in
    /// saving over it meanwhile keeps it again: a session that could not be
    /// read throws (`SessionUnreadable`) before anything is revoked, as does
    /// a queue that could not be read (`RevocationsUnreadable`).
    public func revokePending(transport: any HTTPTransport = URLSessionTransport.shared) async throws -> [Session] {
        var live: [Session] = []
        for old in try await pendingRevocations() {
            let inUse = try await SessionTransitions.shared.run(key) {
                switch await read() {
                case let .found(current) where current.secret == old.secret:
                    try await revoked(old)
                    return true
                case let .failed(status) where status != Self.undecodable:
                    throw SessionUnreadable(status: status)
                case .found, .missing, .failed:
                    return false
                }
            }
            if inUse { continue }
            do {
                try await old.revoke(transport: transport)
            } catch {
                live.append(old)
                continue
            }
            // One the server revoked and the queue still holds is revoked
            // again at the next try, which the server answers as done.
            try? await revoked(old)
        }
        return live
    }

    /// Saves `session` in place of `old`, the session in use where there is
    /// one. `old` is kept for revocation first, since the save overwrites the
    /// only other copy of its token; where the Keychain will not keep it, it
    /// is revoked here, and one neither kept nor revoked stays the session in
    /// use (`NotRetired`). With no `old`, a stored session this store could
    /// not read stays, unwritten over (`SessionUnreadable`): it may be the
    /// one in use, its token held nowhere else. A session this store does
    /// not save is revoked, since nothing else holds its token. A kept `old`
    /// waits for `revokePending`. Where the Keychain refuses both writes,
    /// `old` was revoked to retire it and stays the session in use: its next
    /// request is refused and the app signs in again, which loses nothing.
    public func save(
        _ session: Session, replacing old: Session?,
        transport: any HTTPTransport = URLSessionTransport.shared
    ) async throws {
        try await SessionTransitions.shared.run(key) {
            if let old {
                do {
                    try await retire(old, transport: transport)
                } catch {
                    await discard(session, transport: transport)
                    throw error
                }
            } else if case let .failed(status) = await read(), status != Self.undecodable {
                await discard(session, transport: transport)
                throw SessionUnreadable(status: status)
            }
            do {
                try await save(session)
            } catch {
                // `old` stays the session in use: a queue that will not
                // forget it leaves it to `revokePending`, which forgets the
                // session in use rather than revoke it.
                if let old { try? await revoked(old) }
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
    /// its token. Signed out only once the session is gone: a removal the
    /// storage refuses (`SecureStoreNotRemoved`), or a stored session it
    /// could not read (`SessionUnreadable`), throws with the session still
    /// kept, and its token still kept for revocation.
    public func signOut(_ session: Session, transport: any HTTPTransport = URLSessionTransport.shared) async throws {
        try await SessionTransitions.shared.run(key) {
            try await retire(session, transport: transport)
            switch await read() {
            // Only the session this sign-out retired: one saved in its
            // place is another transition's, kept or revoked by it.
            case let .found(stored) where stored.secret != session.secret:
                return
            case let .failed(status) where status != Self.undecodable:
                throw SessionUnreadable(status: status)
            case .found, .missing, .failed:
                try await clear()
            }
        }
    }

    /// A token this device never came to use, such as a replacement whose
    /// save failed: kept for revocation before it is revoked, so a server
    /// that does not answer leaves it for `revokePending` rather than
    /// nowhere.
    private func discard(_ session: Session, transport: any HTTPTransport) async {
        let kept = (try? await keepForRevocation(session)) != nil
        guard (try? await session.revoke(transport: transport)) != nil else { return }
        if kept { try? await revoked(session) }
    }

    /// What the store's transitions are serialised on: one item, wherever
    /// the store value was made.
    private var key: String { "\(storage.location)|\(account)" }

    /// What every read-modify-write of the revocation queue is serialised
    /// on, whoever makes it: a keep, a forget, a sign-in's rejected token.
    private var queueKey: String { "\(storage.location)|\(revocationsAccount)" }

    /// Kept for revocation, or else revoked now: never neither.
    private func retire(_ session: Session, transport: any HTTPTransport) async throws {
        do {
            try await keepForRevocation(session)
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
    public func data(account: String) async -> Item {
        await storage.read(account: account)
    }

    public func string(account: String) async -> String? {
        if case .found(let data) = await data(account: account) { return String(data: data, encoding: .utf8) }
        return nil
    }

    public func setString(_ value: String, account: String) async throws {
        try await write(Data(value.utf8), account: account)
    }

    /// Replaces the item in place, so a write that fails leaves the one
    /// before it rather than none.
    public func write(_ data: Data, account: String) async throws {
        try await storage.write(data, account: account)
    }

    public func delete(account: String) async throws {
        try await storage.delete(account: account)
    }
}

/// The session could not be read (a Keychain locked before the first
/// unlock, an I/O error): not the same as signed out, so nothing that would
/// save over it, sign it out or revoke beside it goes ahead.
public struct SessionUnreadable: LocalizedError, Sendable {
    public let status: Int32

    public init(status: Int32) {
        self.status = status
    }

    public var errorDescription: String? {
        "This device couldn't read your sign-in (Keychain status \(status)). Unlock it and try again."
    }
}

/// The tokens kept to revoke could not be read: none is kept, forgotten or
/// revoked until they can be, since a queue written over would lose them.
public struct RevocationsUnreadable: LocalizedError, Sendable {
    public let status: Int32

    public init(status: Int32) {
        self.status = status
    }

    public var errorDescription: String? {
        "This device couldn't read the sign-ins it keeps to revoke (Keychain status \(status)). Unlock it and try again."
    }
}

/// Runs the changes to one item of a store one after another: a session's
/// transitions, where a sign-out that waits on the server to revoke would
/// otherwise clear a session a replacement saved meanwhile, and each
/// read-modify-write of a revocation queue, where two at once would each
/// write the queue without the other's token.
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
