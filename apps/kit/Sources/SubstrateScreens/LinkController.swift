import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime

/// The connection to one repository (§5.2), every app's root: the session
/// and its revocations, the status check, the versions of the app's
/// packages, connectivity and the outbox's state. Its presentation is the
/// Account, Access and Status screens, and its snapshot (`LinkSnapshot`),
/// which carries no secret, is what the runtime tells every other
/// controller as `linkChanged`. The token stays in the session store: the
/// effects that need it read it there when they run.
public struct LinkController: Controller {
    public enum Command: Codable, Equatable, Sendable {
        /// At launch: the session, then the network, the check, the
        /// packages and the outbox.
        case open
        /// A sign-in stored a new session.
        case signedIn
        /// The status check, at most every `checkInterval` unless forced.
        case refresh(force: Bool)
        case sendNow
        case retry(UUID)
        case discard(UUID)
        case signOut
    }

    public enum Event: Codable, Equatable, Sendable {
        case loading
        case loaded(session: SessionFacts?, unrevoked: [SessionFacts], generation: Int)
        case checkStarted
        case checked(AppStatus)
        case packagesRead([String: HeldPackage])
        case network(NetworkPath)
        case sendStarted
        case outboxRead(state: OutboxState, waiting: Int, parked: [ParkedWrite])
        case retryAsked(UUID)
        case discardAsked(UUID)
        case signOutStarted
        case signedOut(generation: Int, unrevoked: [SessionFacts])
        case signOutFailed(String)
    }

    public enum Effect: Equatable, Sendable {
        case load
        case followNetwork
        case check
        case readPackages
        case readOutbox
        case flush
        case retry(UUID)
        case discard(UUID)
        case signOut
    }

    public typealias State = LinkState
    public typealias Presentation = LinkPresentation

    public let manifest: AppManifest
    public let sessions: SessionStore
    public let outbox: Outbox?
    /// The app's packages, each with the version this build needs.
    public let packages: [String: Int]
    public let words: StatusWords
    /// The actions the app offers on the Access banner.
    public let offers: Set<AccessAction.Offer>
    public let checkInterval: TimeInterval

    public init(manifest: AppManifest, sessions: SessionStore, outbox: Outbox? = nil, packages: [String: Int] = [:],
                words: StatusWords, offers: Set<AccessAction.Offer> = Set(AccessAction.Offer.allCases), checkInterval: TimeInterval = 300)
    {
        self.manifest = manifest
        self.sessions = sessions
        self.outbox = outbox
        self.packages = packages
        self.words = words
        self.offers = offers
        self.checkInterval = checkInterval
    }

    public var initialState: LinkState { LinkState() }

    // MARK: The controller

    public func events(for command: Command, in context: Enrichment<LinkController>) -> [Event] {
        let state = context.state
        switch command {
        case .open, .signedIn:
            return [.loading]
        case let .refresh(force):
            guard state.session != nil, !state.checking else { return [] }
            if !force, let last = state.status?.checkedAt, context.now.timeIntervalSince(last) < checkInterval { return [] }
            return [.checkStarted]
        case .sendNow:
            return state.session == nil || state.sending ? [] : [.sendStarted]
        case let .retry(id):
            return [.retryAsked(id)]
        case let .discard(id):
            return [.discardAsked(id)]
        case .signOut:
            return state.session == nil || state.signingOut ? [] : [.signOutStarted]
        }
    }

    // MARK: The engine

    public func apply(_ event: Event, to state: LinkState) throws -> LinkState {
        var next = state
        switch event {
        case .loading:
            break
        case let .loaded(session, unrevoked, generation):
            if session?.identity != state.session?.identity {
                next.status = nil
                next.packages = [:]
            }
            next.session = session
            next.unrevoked = unrevoked
            next.generation = generation
        case .checkStarted:
            next.checking = true
        case let .checked(status):
            next.checking = false
            next.status = status
        case let .packagesRead(packages):
            next.packages = packages
        case let .network(path):
            next.network = path
        case .sendStarted:
            next.sending = true
        case let .outboxRead(outbox, waiting, parked):
            next.sending = false
            next.outbox = outbox
            next.waiting = waiting
            next.parked = parked
        case .retryAsked, .discardAsked:
            break
        case .signOutStarted:
            next.signingOut = true
            next.signOutProblem = nil
        case let .signedOut(generation, unrevoked):
            next = LinkState()
            next.network = state.network
            next.generation = generation
            next.unrevoked = unrevoked
        case let .signOutFailed(problem):
            next.signingOut = false
            next.signOutProblem = problem
        }
        return next
    }

    // MARK: The effects

    public func effects(of event: Event, on state: LinkState, presented presentation: LinkPresentation) -> [Effect] {
        switch event {
        case .loading:
            return [.load]
        case let .loaded(session, _, _):
            return [.followNetwork] + (session == nil ? [] : [.check, .readPackages, .readOutbox])
        case .checkStarted:
            return [.check]
        case let .network(path):
            // Back online: what waits goes.
            return path.online && state.session != nil && state.waiting > 0 ? [.flush] : []
        case .sendStarted:
            return [.flush]
        case let .retryAsked(id):
            return [.retry(id)]
        case let .discardAsked(id):
            return [.discard(id)]
        case .signOutStarted:
            return [.signOut]
        default:
            return []
        }
    }

    public func key(of effect: Effect) -> String? {
        switch effect {
        case .load, .signOut: "session"
        case .followNetwork: "network"
        case .check: "check"
        case .readPackages: "packages"
        case .readOutbox, .flush, .retry, .discard: "outbox"
        }
    }

    public func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        let (sessions, manifest, outbox, packages) = (sessions, manifest, outbox, packages)
        let transport = environment.devices.transport
        let folder = environment.folder
        let now = environment.clock
        @Sendable func client(_ session: Session) -> SubstrateClient {
            SubstrateClient(server: session.server, token: session.secret, transport: transport)
        }
        switch effect {
        case .load:
            return .once {
                let session = sessions.load()
                // A revocation cut short at the last launch is tried again.
                let left = await sessions.revokePending(current: session, transport: transport)
                let generation = folder.map { LinkGeneration(folder: $0).current } ?? 0
                return .loaded(session: session.map(SessionFacts.init), unrevoked: left.map(SessionFacts.init), generation: generation)
            }
        case .followNetwork:
            guard let connectivity = environment.devices.connectivity else { return .none }
            return .sequence {
                let paths = connectivity.paths()
                return AsyncStream { continuation in
                    let task = Task {
                        for await path in paths { continuation.yield(.network(path)) }
                        continuation.finish()
                    }
                    continuation.onTermination = { _ in task.cancel() }
                }
            }
        case .check:
            return .once {
                guard let session = sessions.load() else { return .checked(AppStatus(checkedAt: now.now)) }
                let check = AppStatusCheck(manifest: manifest, session: session, client: client(session), now: { now.now })
                return .checked(await check.run())
            }
        case .readPackages:
            return .once {
                guard let session = sessions.load() else { return nil }
                var held: [String: HeldPackage] = [:]
                for (package, needed) in packages {
                    held[package] = try? await HeldPackage.read(package, needing: needed, from: client(session))
                }
                return .packagesRead(held)
            }
        case .readOutbox:
            guard let outbox else { return .none }
            return .once { await Self.read(outbox, state: nil) }
        case .flush:
            guard let outbox else { return .none }
            return .once {
                guard let session = sessions.load() else { return await Self.read(outbox, state: nil) }
                let state = await outbox.flush(client(session))
                return await Self.read(outbox, state: state)
            }
        case let .retry(id):
            guard let outbox else { return .none }
            return .once {
                try? await outbox.retry(id)
                guard let session = sessions.load() else { return await Self.read(outbox, state: nil) }
                return await Self.read(outbox, state: await outbox.flush(client(session)))
            }
        case let .discard(id):
            guard let outbox else { return .none }
            return .once {
                try? await outbox.discard(id)
                return await Self.read(outbox, state: nil)
            }
        case .signOut:
            return .once {
                guard let session = sessions.load() else { return .signedOut(generation: 0, unrevoked: []) }
                do {
                    // The generation moves first: work scheduled before this
                    // writes nothing from here on.
                    let generation = try folder.map { try LinkGeneration(folder: $0).bump() } ?? 0
                    try await sessions.signOut(session, transport: transport)
                    return .signedOut(generation: generation, unrevoked: sessions.pendingRevocations().map(SessionFacts.init))
                } catch {
                    return .signOutFailed(explain(error, app: manifest.appName, server: session.server))
                }
            }
        }
    }

    private static func read(_ outbox: Outbox, state: OutboxState?) async -> Event {
        let entries = await outbox.entries
        let current = await outbox.state
        return .outboxRead(state: state ?? current, waiting: entries.count, parked: entries.filter(\.parked).map(ParkedWrite.init))
    }

    // MARK: The presenter

    public func snapshot(of state: LinkState) -> LinkSnapshot {
        LinkSnapshot(phase: state.session == nil ? .signedOut : state.accessEnded ? .accessEnded : .signedIn,
                     server: state.session?.server, repository: state.session?.repository, scopes: state.session?.scopes,
                     accessEnds: state.session?.expiresAt, generation: state.generation, online: state.network?.online,
                     packages: state.packages.compactMapValues(\.held), waiting: state.waiting, refused: state.parked.count)
    }

    public func present(_ input: PresenterInput<LinkController>) -> LinkPresentation {
        let state = input.state
        let host = state.session.map { ServerAddress.display($0.server) }
        let issues = state.status?.issues ?? []
        let known = packages.values.max() ?? 0
        let held = packages.keys.sorted().first.flatMap { state.packages[$0] }
        let status = LinkStatus.derive(signedIn: state.session != nil, package: packages.isEmpty ? .current(held: 0) : held,
                                       outbox: state.outbox, waiting: state.waiting, known: known, issues: issues)
        var statusPresentation = StatusPresenter.present(status, words: words, host: host, package: held, known: known, sending: state.sending,
                                                         waiting: state.waiting, parked: state.parked,
                                                         kindLabels: manifest.kindLabels.mapValues(\.singular),
                                                         locale: input.locale, timeZone: input.timeZone)
        if packages.isEmpty { statusPresentation.rows = [] }
        let account = state.session.map {
            AccountPresenter.present(session: $0, manifest: manifest, serverVersion: state.status?.serverVersion, unrevoked: state.unrevoked,
                                     offersChangeAccess: offers.contains(.review), locale: input.locale, timeZone: input.timeZone)
        }
        return LinkPresentation(
            snapshot: snapshot(of: state), account: account,
            banner: host.flatMap { AccessPresenter.banner(issues: issues.filter { $0 != .accessEnded }, appName: manifest.appName, host: $0,
                                                           offers: offers, now: input.now, locale: input.locale, timeZone: input.timeZone) },
            ended: state.accessEnded ? AccessPresenter.ended(appName: manifest.appName, host: host ?? "Your server") : nil,
            status: statusPresentation,
            signOutProblem: state.signOutProblem.map { ProblemPresentation(title: "Couldn't sign out", message: $0) })
    }
}
