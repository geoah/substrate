import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// An install's registration with its publisher's push relay, kept in the
/// secure store beside the session: for one sign-in (its token's id), the
/// registration the server confirmed, the attempts it may hold, and the
/// generations of earlier ones still accepted. Nothing in it is a secret the
/// relay or the server lacks; it is kept beside the session so it goes where
/// the session goes.
public struct PushState: Codable, Equatable, Sendable {
    /// One registration: what was sent to the relay, what it sealed, and
    /// when.
    public struct Registration: Codable, Equatable, Sendable {
        /// Picked at random for this registration: every push of it carries
        /// it back, so a push meant for another opens and offers nothing.
        public var generation: String
        public var relay: String
        /// The public half of the key the server signs this sign-in's pushes
        /// with, as the server answered it.
        public var publicKey: String
        /// Apple's token for the install.
        public var deviceToken: String
        public var environment: PushEnvironment
        public var notifies: [String]
        public var registeredAt: Date
        /// What the relay sealed for it, once it answered: the device record's
        /// `pushKey`, so a put whose answer was lost is made again with the
        /// same key.
        public var pushKey: String?

        public init(
            generation: String, relay: String, publicKey: String, deviceToken: String, environment: PushEnvironment, notifies: [String],
            registeredAt: Date, pushKey: String? = nil
        ) {
            self.generation = generation
            self.relay = relay
            self.publicKey = publicKey
            self.deviceToken = deviceToken
            self.environment = environment
            self.notifies = notifies
            self.registeredAt = registeredAt
            self.pushKey = pushKey
        }
    }

    /// A generation of an earlier registration of this sign-in, accepted
    /// until `until`: an event id lives 24 hours, so a push sent under it may
    /// still be delivered, tapped or acted on that long.
    public struct Retired: Codable, Equatable, Sendable {
        public var generation: String
        public var until: Date

        public init(generation: String, until: Date) {
            self.generation = generation
            self.until = until
        }
    }

    /// How long a replaced or given-up generation stays accepted: the life
    /// of an event id.
    public static let retirement: TimeInterval = 24 * 3600
    /// How long after it was sent a put may still land on the server: a
    /// request the server accepted commits within its lifetime. A delete is
    /// final only once it was sent this long after the last put.
    public static let putLifetime: TimeInterval = 120
    /// How many unconfirmed attempts are kept: an older one goes to
    /// `previous`.
    static let attempts = 4

    /// The sign-in the registrations are for: its device record is named by
    /// this token's id.
    public var tokenID: String
    /// The registration whose put the server answered.
    public var current: Registration?
    /// Attempts made since, oldest first. One with a `pushKey` was put, or
    /// was about to be, and the server may hold it with the answer lost: its
    /// generation is accepted and the install counts as registered until a
    /// later put or a delete is confirmed.
    public var pending: [Registration]
    public var previous: [Retired]
    /// When the last put of this sign-in's device record was sent: saved
    /// before it is sent, so a put whose answer was lost is known.
    public var putAt: Date?
    /// A registration given up whose delete may not be final: a put sent
    /// less than `putLifetime` before it may still land and make the record
    /// again. Then it is deleted again on a sync from this time on.
    public var deleteAgainAt: Date?

    public init(
        tokenID: String, current: Registration? = nil, pending: [Registration] = [], previous: [Retired] = [], putAt: Date? = nil,
        deleteAgainAt: Date? = nil
    ) {
        self.tokenID = tokenID
        self.current = current
        self.pending = pending
        self.previous = previous
        self.putAt = putAt
        self.deleteAgainAt = deleteAgainAt
    }

    /// The server pushes, or may push, to this install. Not while a
    /// registration given up is only retiring: the app then posts its own
    /// notifications, and a late put may bring a push beside one for a
    /// moment.
    public var held: Bool { current != nil || pending.contains { $0.pushKey != nil } }

    /// The registration the server holds or may hold, the newest attempt
    /// first.
    public var latest: Registration? { pending.last { $0.pushKey != nil } ?? current }

    /// Whether a push's generation is one of this sign-in's registrations, or
    /// one retired less than a day ago.
    public func accepts(_ generation: String, at now: Date) -> Bool {
        guard !generation.isEmpty else { return false }
        if current?.generation == generation || pending.contains(where: { $0.generation == generation }) { return true }
        return previous.contains { $0.generation == generation && $0.until > now }
    }

    /// Every registration made so far retired, each for a day, and none held.
    mutating func retireAll(at now: Date) {
        let until = now.addingTimeInterval(Self.retirement)
        previous += ([current].compactMap { $0 } + pending).map { Retired(generation: $0.generation, until: until) }
        current = nil
        pending = []
        prune(at: now)
    }

    /// `made` is what the server holds now: what it replaced is retired.
    mutating func confirm(_ made: Registration, at now: Date) {
        let until = now.addingTimeInterval(Self.retirement)
        previous += ([current].compactMap { $0 } + pending).filter { $0.generation != made.generation }
            .map { Retired(generation: $0.generation, until: until) }
        current = made
        pending = []
        prune(at: now)
    }

    mutating func prune(at now: Date) {
        previous.removeAll { $0.until <= now }
        while pending.count > Self.attempts {
            let oldest = pending.removeFirst()
            previous.append(Retired(generation: oldest.generation, until: now.addingTimeInterval(Self.retirement)))
        }
    }
}

/// Whether a push belongs to the sign-in in use.
public enum PushVerdict: Sendable, Equatable {
    /// Its generation is one of this sign-in's registrations.
    case accepted
    /// It is another sign-in's, a signed-out one's, or one this install no
    /// longer accepts.
    case refused
    /// The Keychain could not be read (before the first unlock after a
    /// restart): nothing can be said either way.
    case unknown
}

/// What the app's engine knows of its pushes, read without the network.
public struct PushFacts: Codable, Equatable, Sendable {
    /// Where the last attempt to register stands.
    public enum Standing: String, Codable, Sendable, CaseIterable {
        /// Registered: the server pushes to this install.
        case registered
        /// The server does not list the `push` feature.
        case notListed = "not_listed"
        /// The person turned both notification switches off.
        case switchedOff = "switched_off"
        /// The person has not allowed notifications.
        case notPermitted = "not_permitted"
        /// The build is not entitled to pushes (a local build, or the Mac).
        case notEntitled = "not_entitled"
        /// No relay: the build names none, the sign-in is no verified app's
        /// (a password sign-in), or the publisher's document names none.
        case noRelay = "no_relay"
        /// The operator turned push off.
        case off
        /// The build, the publisher's document and the server name
        /// different relays.
        case unmatched
        /// The relay or the server refused the registration for good.
        case refused
        /// Something could not be read or written now; the next launch or
        /// background refresh tries again.
        case unavailable
        /// Not asked yet in this launch.
        case unknown
        /// The Keychain could not be read (before the first unlock after a
        /// restart): nothing was changed, and nothing can be said of what
        /// is held. The app keeps what it last knew.
        case unreadable
    }

    public var standing: Standing
    /// A registration is held: the server may push to this install, so the
    /// app posts no notification of its own for what a push says.
    public var held: Bool
    public var environment: PushEnvironment?
    /// When the registration in use was made.
    public var since: Date?
    /// Apple's token, in a `development` build alone, so the relay's
    /// operator can send it one test push. Never in a diagnostics file.
    public var deviceToken: String?

    public init(standing: Standing, held: Bool = false, environment: PushEnvironment? = nil, since: Date? = nil, deviceToken: String? = nil) {
        self.standing = standing
        self.held = held
        self.environment = environment
        self.since = since
        self.deviceToken = deviceToken
    }
}

/// What the person and the server want, as the app last said it
/// (`PushRegistrar.want`): read by each attempt when it runs, never when it
/// was queued, so an attempt queued earlier does what a fresh one would.
struct PushWish: Sendable {
    /// The server lists the `push` feature.
    var listed: Bool
    /// The events the person wants pushed (`replies`, `requests`); none is
    /// both switches off.
    var notifies: [String]
    /// Whether the person allows notifications, asked when the attempt runs.
    var permitted: @Sendable () async -> Bool
}

/// A wish and a permission at once, for tests.
struct PushAsk: Equatable, Sendable {
    var listed: Bool
    var notifies: [String]
    var permitted: Bool
}

/// Registers an install with its publisher's relay and keeps its device
/// record, for the session in a store: the kit's half of pushes, which an
/// app runs at launch, at each background refresh and when its switches or
/// its permission change (`sync`), at sign-out (`signOut`), and when a
/// notification is opened or acted on (`verdict`, `client(accepting:)`).
///
/// It registers only where three name the same relay: the build
/// (`buildRelay`, stamped at release), the publisher's authority document at
/// the origin of the app's client id, and the server, which answers the
/// relay it will send this token's pushes to. A mismatch, or a document or
/// a server that says there is none, gives up a registration it holds.
public struct PushRegistrar: Sendable {
    public let sessions: SessionStore
    public let device: (any PushRegistration)?
    public let transport: any HTTPTransport
    /// The relay the build names; nil or `none` for a build that registers
    /// nowhere.
    public let buildRelay: String?
    /// The app's own client id: the publisher's document lists the app by
    /// it.
    public let clientID: String
    public let now: @Sendable () -> Date
    public let generation: @Sendable () -> String
    /// Shared by every copy of this value: the attempts in flight, and the
    /// sign-ins being signed out.
    let control = PushControl()

    /// How long a registration is used before it is made again: well inside
    /// the relay's 90 days, and how installs move to a new seal key.
    public static let renewal: TimeInterval = 30 * 24 * 3600

    public init(
        sessions: SessionStore, device: (any PushRegistration)?, transport: any HTTPTransport = URLSessionTransport.push,
        buildRelay: String?, clientID: String, now: @escaping @Sendable () -> Date = { Date() },
        generation: (@Sendable () -> String)? = nil
    ) {
        self.sessions = sessions
        self.device = device
        self.transport = transport
        self.buildRelay = buildRelay
        self.clientID = clientID
        self.now = now
        self.generation = generation ?? { randomBase64URL(bytes: 16) }
    }

    /// Where the state is kept, beside the session.
    public var account: String { sessions.account + ".push" }

    // MARK: Reading

    /// The state as kept; nil where none is, or it could not be read.
    public func state() async -> PushState? {
        guard case let .found(state) = await stored() else { return nil }
        return state
    }

    /// The state as kept, telling an item that is not there (or that no
    /// longer decodes, which is as good as none) from a Keychain that could
    /// not answer, which says nothing of what is there.
    enum Stored {
        case found(PushState)
        case missing
        case failed
    }

    func stored() async -> Stored {
        switch await sessions.data(account: account) {
        case let .found(data): (try? JSONDecoder().decode(PushState.self, from: data)).map(Stored.found) ?? .missing
        case .missing: .missing
        case .failed: .failed
        }
    }

    /// What is kept, for the engine, without the network.
    public func facts() async -> PushFacts {
        let session: Session
        switch await sessions.read() {
        case let .found(found): session = found
        case .missing: return PushFacts(standing: .unknown)
        case .failed: return PushFacts(standing: .unreadable)
        }
        switch await stored() {
        case let .found(state) where state.tokenID == session.tokenID && state.held: return facts(.registered, state)
        case .failed: return PushFacts(standing: .unreadable)
        case .found, .missing: return PushFacts(standing: .unknown)
        }
    }

    /// Whether a push's generation belongs to the sign-in in use: its
    /// current registration, an attempt the server may hold, or one retired
    /// less than a day ago. A push for an earlier registration of this
    /// install (another server, another sign-in, one signed out) opens
    /// nothing, fetches nothing and offers no action. Where the Keychain
    /// cannot be read (before the first unlock) it is `unknown`, which is not
    /// another sign-in's.
    public func verdict(_ generation: String) async -> PushVerdict {
        let session: Session
        switch await sessions.read() {
        case let .found(found): session = found
        case .missing: return .refused
        case .failed: return .unknown
        }
        switch await sessions.data(account: account) {
        case let .found(data):
            guard let state = try? JSONDecoder().decode(PushState.self, from: data), state.tokenID == session.tokenID,
                !control.isLeaving(session.tokenID)
            else { return .refused }
            return state.accepts(generation, at: now()) ? .accepted : .refused
        case .missing: return .refused
        case .failed: return .unknown
        }
    }

    public func accepts(_ generation: String) async -> Bool { await verdict(generation) == .accepted }

    /// The client of the session a push belongs to, for a write a push's
    /// action makes: read once, and only while the push's generation is that
    /// session's, so a decision accepted under one sign-in is never sent with
    /// another's token.
    public func client(accepting generation: String) async -> SubstrateClient? {
        guard case let .found(session) = await sessions.read(), let state = await state(), state.tokenID == session.tokenID,
            !control.isLeaving(session.tokenID), state.accepts(generation, at: now())
        else { return nil }
        return SubstrateClient(session, transport: transport)
    }

    /// The repository a push belongs to, as `Session.identity` names it;
    /// nil where the push is not this sign-in's.
    public func identity(accepting generation: String) async -> String? {
        guard case let .found(session) = await sessions.read(), let state = await state(), state.tokenID == session.tokenID,
            state.accepts(generation, at: now())
        else { return nil }
        return session.identity
    }

    // MARK: Registering

    /// What is wanted from now on: the app says it whenever it changes (a
    /// switch, the permission, the server's document), before it asks for a
    /// sync. Every attempt reads the newest when it runs.
    public func want(listed: Bool, notifies: [String], permitted: @escaping @Sendable () async -> Bool) {
        control.want(PushWish(listed: listed, notifies: notifies, permitted: permitted))
    }

    /// Brings the server and the Keychain into line with what is wanted
    /// when the attempt runs (`want`), for the sign-in in use now, and
    /// answers where it stands. The attempt is queued behind the registrar's
    /// other work and runs to its end, whether or not its caller still waits.
    /// It acts for this sign-in alone: run once another is in use, it does
    /// nothing; and it saves and puts nothing for one being signed out.
    public func sync() async -> PushFacts {
        let tokenID: String?
        switch await sessions.read() {
        case let .found(session): tokenID = session.tokenID
        case .missing: tokenID = nil
        case .failed: return PushFacts(standing: .unreadable)
        }
        return await control.enqueue { await register(for: tokenID) }.value
    }

    /// `want` and `sync` at once, for tests.
    func sync(_ ask: PushAsk) async -> PushFacts {
        want(listed: ask.listed, notifies: ask.notifies, permitted: { ask.permitted })
        return await sync()
    }

    /// The client the registrar's own requests go with: each ends within the
    /// transport's limit, and a busy server is not waited out, so an attempt
    /// always ends.
    private func pushClient(_ session: Session) -> SubstrateClient {
        var client = SubstrateClient(session, transport: transport)
        client.pause = { _ in throw URLError(.timedOut) }
        return client
    }

    private func register(for bound: String?) async -> PushFacts {
        let session: Session
        switch await sessions.read() {
        case let .found(found): session = found
        case .missing:
            // Nobody is signed in: what is kept is no sign-in's any more.
            if bound == nil { try? await clear() }
            return PushFacts(standing: .unknown)
        case .failed: return PushFacts(standing: .unreadable)
        }
        // Queued for another sign-in than the one in use now: nothing is
        // done for this one with what was meant for that.
        guard session.tokenID == bound, !control.isLeaving(session.tokenID), let wish = control.wanted else {
            return PushFacts(standing: .unknown)
        }
        let client = pushClient(session)
        // A Keychain that cannot answer is no absent registration: nothing
        // is made over what it may hold.
        var state: PushState?
        switch await stored() {
        case let .found(found): state = found
        case .missing: state = nil
        case .failed: return PushFacts(standing: .unreadable)
        }
        // Another sign-in's registration: its device goes with its token.
        if let held = state, held.tokenID != session.tokenID {
            try? await clear()
            state = nil
        }
        guard wish.listed else { return await giveUp(.notListed, state, client) }
        guard !wish.notifies.isEmpty else { return await giveUp(.switchedOff, state, client) }
        guard await wish.permitted() else { return await giveUp(.notPermitted, state, client) }
        guard let device, let environment = device.environment() else { return await giveUp(.notEntitled, state, client) }
        guard let build = buildRelay, build != "none", PushRelay.endpoint(build, path: PushRelay.pushPath) != nil,
            let tokenID = session.tokenID, session.isApp
        else { return await giveUp(.noRelay, state, client) }

        let key: PushKey
        do {
            guard let published = try await PushRelay.published(clientID: clientID, transport: transport) else {
                return await giveUp(.noRelay, state, client)
            }
            switch try await client.pushKey() {
            case let .success(answer): key = answer
            case .failure(.noRelay): return await giveUp(.noRelay, state, client)
            case .failure(.off): return await giveUp(.off, state, client)
            }
            guard PushRelay.same(build, published), PushRelay.same(build, key.relay) else { return await giveUp(.unmatched, state, client) }
        } catch is SessionExpired {
            return await giveUp(.unavailable, state, client)
        } catch {
            return facts(.unavailable, state)
        }

        let deviceToken: String
        do {
            deviceToken = try await device.deviceToken()
        } catch {
            return facts(.unavailable, state)
        }
        let notifies = wish.notifies.sorted()
        func wanted(_ made: PushState.Registration) -> Bool {
            !Self.due(made, relay: build, publicKey: key.publicKey, deviceToken: deviceToken, environment: environment, notifies: notifies, now: now())
        }
        var next = state ?? PushState(tokenID: tokenID)

        // An attempt the server may hold, and still the one wanted: put again
        // with the key it was sealed, which lands the same record whether or
        // not the first put did.
        if let attempt = next.pending.last(where: { $0.pushKey != nil }), wanted(attempt) {
            return await put(attempt, &next, device: device, client: client)
        }
        // The confirmed one, still wanted, with nothing newer the server may
        // hold. Its record may be gone (deleted by the server after the
        // relay refused a push, or by hand): then it is made again.
        if let current = next.current, !next.pending.contains(where: { $0.pushKey != nil }), wanted(current) {
            do {
                if try await client.record(SubstrateClient.deviceKind, tokenID) != nil { return facts(.registered, next) }
            } catch {
                return facts(.registered, next)
            }
        }

        var made = PushState.Registration(
            generation: generation(), relay: build, publicKey: key.publicKey, deviceToken: deviceToken, environment: environment,
            notifies: notifies, registeredAt: now())
        next.pending.append(made)
        // What `held` is read from on every way out: the state last saved,
        // never an attempt only this run holds.
        guard await save(next, for: tokenID) else { return facts(.unavailable, state) }
        let saved = next
        do {
            made.pushKey = try await PushRelay.register(
                PushRelay.RegisterRequest(
                    platform: "apns", token: deviceToken, topic: device.topic, environment: environment.rawValue, publicKey: key.publicKey,
                    generation: made.generation),
                relay: build, transport: transport)
        } catch let refusal as PushRelay.Refusal where refusal.stops {
            return await giveUp(.refused, next, client)
        } catch {
            // It never reached the server: only it goes, and every earlier
            // attempt the server may hold stays accepted.
            next.pending.removeAll { $0.generation == made.generation }
            return facts(.unavailable, await save(next, for: tokenID) ? next : saved)
        }
        next.pending[next.pending.count - 1] = made
        // No put is sent for a key the Keychain did not keep.
        guard await save(next, for: tokenID) else { return facts(.unavailable, saved) }
        return await put(made, &next, device: device, client: client)
    }

    /// The device record put with `made`'s key, and `made` current once the
    /// server answers. A put that may have landed leaves it held. When it was
    /// sent is saved before it is sent: a delete is final only once a put can
    /// no longer land after it.
    private func put(
        _ made: PushState.Registration, _ next: inout PushState, device: any PushRegistration, client: SubstrateClient
    ) async -> PushFacts {
        guard let pushKey = made.pushKey else { return facts(.unavailable, next) }
        // A sign-out under way: no put is started for it. One already sent
        // ends before the sign-out's delete starts, which is queued after.
        guard !control.isLeaving(next.tokenID) else { return PushFacts(standing: .unknown) }
        let unsent = next
        next.putAt = now()
        guard await save(next, for: next.tokenID) else { return facts(.unavailable, unsent) }
        do {
            try await client.putDevice(tokenID: next.tokenID, name: device.deviceName, platform: device.platform, pushKey: pushKey, notifies: made.notifies)
        } catch let refused as ServerError where refused.isPermanent {
            return await giveUp(.refused, next, client)
        } catch {
            return facts(.unavailable, next)
        }
        let before = next
        next.confirm(made, at: now())
        // The record is this registration's now: nothing is left to delete.
        next.deleteAgainAt = nil
        guard await save(next, for: next.tokenID) else { return facts(.unavailable, before) }
        return facts(.registered, next)
    }

    /// Whether a registration is made again: Apple's token, the relay, the
    /// public key, the environment or the switches changed, or it is 30 days
    /// old. A device record that is gone is the other reason, which `sync`
    /// reads from the server.
    public static func due(
        _ current: PushState.Registration, relay: String, publicKey: String, deviceToken: String, environment: PushEnvironment,
        notifies: [String], now: Date
    ) -> Bool {
        current.deviceToken != deviceToken || !PushRelay.same(current.relay, relay) || current.publicKey != publicKey
            || current.environment != environment || current.notifies.sorted() != notifies.sorted()
            || now.timeIntervalSince(current.registeredAt) >= renewal
    }

    // MARK: Giving up

    /// Signing out: the sign-in is marked as leaving, so no attempt saves or
    /// puts for it from now on, and its forgetting is queued behind the work
    /// in flight, which is not stopped: the device record deleted while the
    /// token may still delete it, then every registration of the sign-in
    /// forgotten, the current, the pending and the retired alike, so every
    /// push already delivered or still queued for it no longer matches. The
    /// sign-out waits for that at most `PushControl.signOutBound`, then
    /// returns, and the forgetting runs when its turn comes; nothing queued
    /// before it runs after it.
    ///
    /// The caller revokes the token after this (`LinkController.signOut`),
    /// and the server deletes a token's device with it. That covers what no
    /// order here can: a delete that fails, one that runs after the
    /// revocation, and a put whose answer the app gave up on that lands on
    /// the server after the delete. Once the revocation goes through no
    /// device is left; where it fails the token is still live, and so is a
    /// device it put.
    public func signOut(_ session: Session) async {
        control.leave(session.tokenID)
        // What was wanted was this sign-in's: the next says its own.
        control.forgetWish()
        let forgetting = control.enqueue { await forget(session) }
        _ = await PushControl.within(control.signOutBound) { await forgetting.value }
    }

    /// What a sign-out leaves to the queue, for its own sign-in alone: the
    /// device and the state kept for its token, never another's. Where the
    /// sign-out failed meanwhile (`signOutFailed`), nothing: the person is
    /// still signed in.
    private func forget(_ session: Session) async {
        guard control.isLeaving(session.tokenID), let tokenID = session.tokenID,
            case let .found(state) = await stored(), state.tokenID == tokenID
        else { return }
        if state.held || state.deleteAgainAt != nil {
            try? await pushClient(session).delete(SubstrateClient.deviceKind, tokenID)
        }
        try? await clear()
    }

    /// The sign-out did not go through (the session could not be removed):
    /// the person is still signed in, so attempts may start and save again
    /// for the sign-in, and the next one registers it anew.
    public func signOutFailed(_ session: Session) {
        control.stay(session.tokenID)
    }

    /// Stops using a registration: its device record deleted, then each
    /// generation retired for a day. Where the delete could not be made now
    /// the registration stays held, since the server may still push to it,
    /// and the next attempt deletes it. A delete sent less than
    /// `PushState.putLifetime` after the last put is not final, since that put
    /// may still land and make the record again: the state is kept retiring,
    /// and a sync from `deleteAgainAt` on deletes again. What it answers is
    /// what was saved: where the retirement could not be saved, the stored
    /// state, still held, and the next sync deletes again.
    private func giveUp(_ standing: PushFacts.Standing, _ state: PushState?, _ client: SubstrateClient) async -> PushFacts {
        guard let stored = state else { return PushFacts(standing: standing) }
        var next = stored
        let retiring = !stored.held && stored.deleteAgainAt != nil
        if retiring, let again = stored.deleteAgainAt, now() < again { return facts(standing, stored) }
        if stored.held || retiring {
            let sent = now()
            do {
                try await client.delete(SubstrateClient.deviceKind, stored.tokenID)
                if let putAt = stored.putAt, sent.timeIntervalSince(putAt) < PushState.putLifetime {
                    next.deleteAgainAt = putAt.addingTimeInterval(PushState.putLifetime)
                } else {
                    next.deleteAgainAt = nil
                }
            } catch is SessionExpired {
                // The token is gone, and its device with it.
                next.deleteAgainAt = nil
            } catch {
                return facts(standing, stored)
            }
        }
        next.retireAll(at: now())
        guard next != stored else { return facts(standing, stored) }
        guard await save(next, for: next.tokenID) else { return facts(standing, stored) }
        return facts(standing, next)
    }

    private func facts(_ standing: PushFacts.Standing, _ state: PushState?) -> PushFacts {
        guard let state, state.held, let latest = state.latest else { return PushFacts(standing: standing) }
        return PushFacts(
            standing: standing, held: true, environment: latest.environment, since: latest.registeredAt,
            deviceToken: latest.environment == .development ? latest.deviceToken : nil)
    }

    /// Saves unless the sign-in is being signed out: a save after the
    /// sign-out forgot the registrations would bring one back.
    private func save(_ state: PushState, for tokenID: String) async -> Bool {
        guard !control.isLeaving(tokenID), let data = try? JSONEncoder().encode(state) else { return false }
        do {
            try await sessions.write(data, account: account)
            return true
        } catch {
            return false
        }
    }

    private func clear() async throws {
        try await sessions.delete(account: account)
    }
}

/// The registrar's work on the server and in the Keychain, what is wanted
/// of it, and the sign-ins being signed out: shared by every copy of one
/// registrar.
///
/// Every attempt and every sign-out's forgetting runs on one queue, in the
/// order it was queued, each to its end: nothing is cancelled or overtaken,
/// and a caller that stops waiting does not stop the work. What bounds a
/// wait is each request's own limit (`URLSessionTransport.push`), so the
/// work before always ends and the queue moves on. An attempt carries no
/// wish of its own: it reads `wanted` when it runs.
final class PushControl: @unchecked Sendable {
    /// How long a sign-out waits for its turn before it returns, leaving
    /// its delete to run when the work before it ends.
    static let signOutWait: Duration = .seconds(7)

    private let lock = NSLock()
    private var tail: Task<Void, Never>?
    private var leaving: Set<String> = []
    private var wish: PushWish?
    private var bound = PushControl.signOutWait

    var signOutBound: Duration {
        get { lock.withLock { bound } }
        set { lock.withLock { bound = newValue } }
    }

    /// Queues `operation` after everything queued before it; it runs to its
    /// end whoever waits for it.
    func enqueue<T: Sendable>(_ operation: @escaping @Sendable () async -> T) -> Task<T, Never> {
        lock.withLock {
            let previous = tail
            let task = Task<T, Never> {
                await previous?.value
                return await operation()
            }
            tail = Task { _ = await task.value }
            return task
        }
    }

    func want(_ wish: PushWish) { lock.withLock { self.wish = wish } }

    /// What is wanted now; nil before the app has said, and after a
    /// sign-out until the next sign-in says its own.
    var wanted: PushWish? { lock.withLock { wish } }

    func forgetWish() { lock.withLock { wish = nil } }

    /// The sign-in of `tokenID` is being signed out: no attempt queued or
    /// still to come saves or puts for it. One under way is not stopped; the
    /// sign-out's delete is queued after it.
    func leave(_ tokenID: String?) {
        guard let tokenID else { return }
        _ = lock.withLock { leaving.insert(tokenID) }
    }

    /// A sign-out of `tokenID` failed: it is no longer leaving.
    func stay(_ tokenID: String?) {
        guard let tokenID else { return }
        _ = lock.withLock { leaving.remove(tokenID) }
    }

    func isLeaving(_ tokenID: String?) -> Bool {
        guard let tokenID else { return false }
        return lock.withLock { leaving.contains(tokenID) }
    }

    /// Waits for `work` at most `limit`: true where it finished in time.
    /// The work is not stopped at the limit; only the wait is.
    static func within(_ limit: Duration, _ work: @escaping @Sendable () async -> Void) async -> Bool {
        await Deadline.race(until: .now + limit, or: false) {
            await work()
            return true
        }
    }
}
