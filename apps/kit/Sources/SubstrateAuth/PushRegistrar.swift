import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// An install's registration with its publisher's push relay, kept in the
/// secure store beside the session: for one sign-in (its token's id), the
/// registration in use, and the one being made, until the server took its
/// device record. Nothing in it is a secret the relay or the server lacks;
/// it is kept beside the session so it goes where the session goes.
public struct PushState: Codable, Equatable, Sendable {
    /// One registration: what was sent to the relay, and when.
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

        public init(
            generation: String, relay: String, publicKey: String, deviceToken: String, environment: PushEnvironment, notifies: [String],
            registeredAt: Date
        ) {
            self.generation = generation
            self.relay = relay
            self.publicKey = publicKey
            self.deviceToken = deviceToken
            self.environment = environment
            self.notifies = notifies
            self.registeredAt = registeredAt
        }
    }

    /// The sign-in the registrations are for: its device record is named by
    /// this token's id.
    public var tokenID: String
    public var current: Registration?
    /// Made and not yet answered by the server's put: a push carrying its
    /// generation is accepted beside the current one's, since the put may
    /// have landed with its answer lost.
    public var pending: Registration?

    public init(tokenID: String, current: Registration? = nil, pending: Registration? = nil) {
        self.tokenID = tokenID
        self.current = current
        self.pending = pending
    }

    /// Whether a push's generation is one of this sign-in's registrations.
    public func accepts(_ generation: String) -> Bool {
        !generation.isEmpty && (current?.generation == generation || pending?.generation == generation)
    }
}

/// What the app's engine knows of its pushes, read without the network.
public struct PushFacts: Codable, Equatable, Sendable {
    /// Where the last attempt to register stands.
    public enum Standing: String, Codable, Sendable, CaseIterable {
        /// Registered: the server pushes to this install.
        case registered
        /// The server does not list the `push` feature.
        case notListed
        /// The person turned both notification switches off.
        case switchedOff
        /// The person has not allowed notifications.
        case notPermitted
        /// The build is not entitled to pushes (a local build, or the Mac).
        case notEntitled
        /// No relay: the build names none, the sign-in is no verified app's
        /// (a password sign-in), or the publisher's document names none.
        case noRelay
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

/// What the person and the server allow, for one attempt to register.
public struct PushAsk: Codable, Equatable, Sendable {
    /// The server lists the `push` feature.
    public var listed: Bool
    /// The events the person wants pushed (`replies`, `requests`); none is
    /// both switches off.
    public var notifies: [String]
    /// The person allows notifications.
    public var permitted: Bool

    public init(listed: Bool, notifies: [String], permitted: Bool) {
        self.listed = listed
        self.notifies = notifies
        self.permitted = permitted
    }
}

/// Registers an install with its publisher's relay and keeps its device
/// record, for the session in a store: the kit's half of pushes, which an
/// app runs at launch, at each background refresh and when its switches or
/// its permission change (`sync`), at sign-out (`signOut`), and when a
/// notification is opened or acted on (`accepts`).
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

    /// How long a registration is used before it is made again: well inside
    /// the relay's 90 days, and how installs move to a new seal key.
    public static let renewal: TimeInterval = 30 * 24 * 3600

    public init(
        sessions: SessionStore, device: (any PushRegistration)?, transport: any HTTPTransport = URLSessionTransport.shared,
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

    /// What every change to the state is serialised on.
    private var transitions: String { "\(sessions.storage.location)|\(account)" }

    // MARK: Reading

    /// The state as kept; nil where none is, or it could not be read.
    public func state() async -> PushState? {
        guard case let .found(data) = await sessions.data(account: account) else { return nil }
        return try? JSONDecoder().decode(PushState.self, from: data)
    }

    /// What is kept, for the engine, without the network.
    public func facts() async -> PushFacts {
        guard let session = await sessions.load(), let state = await state(), state.tokenID == session.tokenID, let current = state.current
        else { return PushFacts(standing: .unknown) }
        return facts(.registered, current)
    }

    /// Whether a push's generation is the current or the pending one of the
    /// sign-in in use. A push for an earlier registration of this install
    /// (another server, another sign-in, one signed out) is not: it opens
    /// nothing, fetches nothing and offers no action.
    public func accepts(_ generation: String) async -> Bool {
        guard let session = await sessions.load(), let state = await state() else { return false }
        return state.tokenID == session.tokenID && state.accepts(generation)
    }

    // MARK: Registering

    /// Registers where it is due, gives up a registration that no longer
    /// holds, and answers where it stands. One attempt at a time, and never
    /// beside a sign-out: one that saved after a sign-out forgot the
    /// registrations would bring a signed-out one back.
    public func sync(_ ask: PushAsk) async -> PushFacts {
        (try? await SessionTransitions.shared.run(transitions) { await register(ask) }) ?? PushFacts(standing: .unavailable)
    }

    private func register(_ ask: PushAsk) async -> PushFacts {
        let session: Session
        switch await sessions.read() {
        case let .found(found): session = found
        case .missing:
            try? await clear()
            return PushFacts(standing: .unknown)
        case .failed: return await facts()
        }
        let client = SubstrateClient(session, transport: transport)
        var state = await state()
        // Another sign-in's registration: its device goes with its token.
        if let held = state, held.tokenID != session.tokenID {
            try? await clear()
            state = nil
        }
        guard ask.listed else { return await giveUp(.notListed, state, client) }
        guard !ask.notifies.isEmpty else { return await giveUp(.switchedOff, state, client) }
        guard ask.permitted else { return await giveUp(.notPermitted, state, client) }
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
            return unavailable(state)
        }

        let deviceToken: String
        do {
            deviceToken = try await device.deviceToken()
        } catch {
            return unavailable(state)
        }
        let notifies = ask.notifies.sorted()
        if let current = state?.current, !Self.due(current, relay: build, publicKey: key.publicKey, deviceToken: deviceToken,
            environment: environment, notifies: notifies, now: now())
        {
            // The record may be gone (deleted by the server after the relay
            // refused a push, or by hand): it is put again.
            do {
                if try await client.record(SubstrateClient.deviceKind, tokenID) != nil { return facts(.registered, current) }
            } catch {
                return facts(.registered, current)
            }
        }

        let made = PushState.Registration(
            generation: generation(), relay: build, publicKey: key.publicKey, deviceToken: deviceToken, environment: environment,
            notifies: notifies, registeredAt: now())
        var next = state ?? PushState(tokenID: tokenID)
        next.pending = made
        do {
            try await save(next)
        } catch {
            return unavailable(state)
        }
        let pushKey: String
        do {
            pushKey = try await PushRelay.register(
                PushRelay.RegisterRequest(
                    platform: "apns", token: deviceToken, topic: device.topic, environment: environment.rawValue, publicKey: key.publicKey,
                    generation: made.generation),
                relay: build, transport: transport)
        } catch let refusal as PushRelay.Refusal where refusal.stops {
            return await giveUp(.refused, next, client)
        } catch {
            next.pending = nil
            try? await save(next)
            return unavailable(next)
        }
        do {
            try await client.putDevice(tokenID: tokenID, name: device.deviceName, platform: device.platform, pushKey: pushKey, notifies: notifies)
        } catch let refused as ServerError where refused.isPermanent {
            return await giveUp(.refused, next, client)
        } catch {
            // It may have landed with its answer lost: the pending generation
            // stays accepted beside the current one until the next attempt.
            return unavailable(next)
        }
        next.current = made
        next.pending = nil
        do {
            try await save(next)
        } catch {
            return unavailable(next)
        }
        return facts(.registered, made)
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

    /// Signing out: the device record is deleted first, while the token can
    /// still delete it, and then every registration of the sign-in is
    /// forgotten, the current and the pending one alike, so every push
    /// already delivered or still queued for it no longer matches. A
    /// delete that fails is left to the server, which deletes the device
    /// with its token.
    public func signOut(_ session: Session) async {
        _ = try? await SessionTransitions.shared.run(transitions) { await forget(session) }
    }

    private func forget(_ session: Session) async {
        if let state = await state(), state.tokenID == session.tokenID, state.current != nil || state.pending != nil, let tokenID = session.tokenID {
            var client = SubstrateClient(session, transport: transport)
            client.pause = { _ in throw CancellationError() }
            try? await client.delete(SubstrateClient.deviceKind, tokenID)
        }
        try? await clear()
    }

    /// Stops using a registration: its device record deleted, then the
    /// registration forgotten. Where the delete could not be made now the
    /// registration stays held, since the server may still push to it, and
    /// the next attempt deletes it.
    private func giveUp(_ standing: PushFacts.Standing, _ state: PushState?, _ client: SubstrateClient) async -> PushFacts {
        guard let state, state.current != nil || state.pending != nil else { return PushFacts(standing: standing) }
        do {
            try await client.delete(SubstrateClient.deviceKind, state.tokenID)
        } catch is SessionExpired {
            // The token is gone, and its device with it.
        } catch {
            return PushFacts(standing: standing, held: state.current != nil, environment: state.current?.environment, since: state.current?.registeredAt)
        }
        try? await clear()
        return PushFacts(standing: standing)
    }

    private func unavailable(_ state: PushState?) -> PushFacts {
        guard let current = state?.current else { return PushFacts(standing: .unavailable) }
        var out = facts(.registered, current)
        out.standing = .unavailable
        return out
    }

    private func facts(_ standing: PushFacts.Standing, _ current: PushState.Registration) -> PushFacts {
        PushFacts(
            standing: standing, held: true, environment: current.environment, since: current.registeredAt,
            deviceToken: current.environment == .development ? current.deviceToken : nil)
    }

    private func save(_ state: PushState) async throws {
        try await sessions.write(try JSONEncoder().encode(state), account: account)
    }

    private func clear() async throws {
        try await sessions.delete(account: account)
    }
}
