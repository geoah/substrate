import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime

/// The connection to one repository (§5.2), every app's root: the session
/// and its revocations, the status check, the versions of the app's
/// packages, connectivity and the outbox. It keeps the app's own root as its
/// one child, so one runtime runs both as one presentation tree: the link's
/// snapshot (`LinkSnapshot`, which carries no secret) reaches the app as
/// `event(forLink:)` inside the step that changed it, and what the app's
/// events report (`LinkReport`: a 401, a 403 `insufficient_scope`) comes
/// back up as the link's own event. The token stays in the session store:
/// the effects that need it read it there when they run.
///
/// The link opens on its own: its event at open loads the session, then
/// follows the network and the outbox, checks the status and reads the
/// packages. The outbox's reports move the Status screen, and a write the
/// app queues starts a flush wherever the link can send. A session the
/// store could not read is neither signed in nor signed out: the facts last
/// read stay, what needs the token waits, and the tokens kept for revocation
/// stay kept until the session in use is known, so none is revoked in its
/// place.
public struct LinkController<App: LinkedApp>: Controller {
    public enum Command: Bridged, Equatable {
        /// A sign-in stored a new session: it is loaded again.
        case signedIn
        /// The status check, at most every `checkInterval` unless forced.
        case refresh(force: Bool)
        case sendNow
        case retry(UUID)
        case discard(UUID)
        case signOut
        /// The session read again, after a read that failed.
        case readAgain
    }

    public enum Event: Codable, Equatable, Sendable {
        case loading
        /// The session as read, with the tokens still waiting to be revoked;
        /// nil `unrevoked` where the queue could not be read, which leaves
        /// what was known of it.
        case loaded(session: SessionFacts?, unrevoked: [SessionFacts]?, generation: Int)
        /// The session could not be read, in words: not signed out.
        case unreadable(String)
        case checkStarted
        case checked(AppStatus)
        case packagesRead([String: HeldPackage])
        case network(NetworkPath)
        case sendStarted
        case outboxChanged(OutboxFacts)
        case retryAsked(UUID)
        case discardAsked(UUID)
        case signOutStarted
        case signedOut(generation: Int, unrevoked: [SessionFacts])
        /// The session stays: the generation moved first where it moved.
        case signOutFailed(String, generation: Int?)
        /// The app said something about the connection.
        case reported(LinkReport)
    }

    public enum Effect: Equatable, Sendable {
        case load
        case followNetwork
        case followOutbox
        case check
        case readPackages
        case flush
        case retry(UUID)
        case discard(UUID)
        case signOut
    }

    public typealias State = LinkState
    public typealias Presentation = LinkPresentation<App.Presentation>
    public typealias Child = App

    /// The key the app's root is kept under, and the path a command for it
    /// takes (`Runtime.send(_:to:)`).
    public static var appKey: ChildKey { ChildKey("app") }
    public static var appPath: [ChildKey] { [appKey] }

    public let app: App
    public let manifest: AppManifest
    public let sessions: SessionStore
    public let outbox: Outbox?
    /// The outbox of the repository a session is signed in to, where an
    /// app keeps one per repository (Workouts' account folders); `outbox`
    /// where it answers none.
    public let outboxes: (@Sendable (SessionFacts) -> Outbox?)?
    /// The app's packages, each with the version this build needs.
    public let packages: [String: Int]
    public let words: StatusWords
    /// The actions the app offers on the Access banner.
    public let offers: Set<AccessAction.Offer>
    public let checkInterval: TimeInterval

    public init(
        app: App, manifest: AppManifest, sessions: SessionStore, outbox: Outbox? = nil, packages: [String: Int] = [:],
        words: StatusWords, offers: Set<AccessAction.Offer> = Set(AccessAction.Offer.allCases), checkInterval: TimeInterval = 300
    ) {
        self.app = app
        self.manifest = manifest
        self.sessions = sessions
        self.outbox = outbox
        outboxes = nil
        self.packages = packages
        self.words = words
        self.offers = offers
        self.checkInterval = checkInterval
    }

    /// A link whose outbox is the signed-in repository's own, made or
    /// found by `outboxes` each time an effect needs it.
    public init(
        app: App, manifest: AppManifest, sessions: SessionStore, outboxes: @escaping @Sendable (SessionFacts) -> Outbox?,
        packages: [String: Int] = [:], words: StatusWords, offers: Set<AccessAction.Offer> = Set(AccessAction.Offer.allCases),
        checkInterval: TimeInterval = 300
    ) {
        self.app = app
        self.manifest = manifest
        self.sessions = sessions
        outbox = nil
        self.outboxes = outboxes
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
        case .signedIn, .readAgain:
            return [.loading]
        case let .refresh(force):
            // Coming forward reads again a session that could not be read.
            if state.sessionProblem != nil { return [.loading] }
            guard state.session != nil, !state.checking else { return [] }
            if !force, let last = state.status?.checkedAt, context.now.timeIntervalSince(last) < checkInterval { return [] }
            return [.checkStarted]
        case .sendNow:
            return state.session == nil || state.sessionProblem != nil || state.sending ? [] : [.sendStarted]
        case let .retry(id):
            return [.retryAsked(id)]
        case let .discard(id):
            return [.discardAsked(id)]
        case .signOut:
            return state.session == nil || state.signingOut ? [] : [.signOutStarted]
        }
    }

    public func event(atOpen replayed: Bool) -> Event? { .loading }

    public func event(fromChild key: ChildKey, _ event: App.Event) -> Event? {
        app.report(of: event).map(Event.reported)
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
            if let unrevoked { next.unrevoked = unrevoked }
            next.generation = generation
            next.sessionProblem = nil
            // A token loaded again is a new one: what the old one was
            // refused no longer holds.
            next.tokenRefused = false
            next.scopeRefused = false
        case let .unreadable(problem):
            next.sessionProblem = problem
            next.checking = false
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
        case let .outboxChanged(outbox):
            next.flushDue = outbox.revision != state.seenRevision && outbox.sendable > 0
            next.seenRevision = outbox.revision
            next.outbox = outbox
            next.sending = outbox.flushing
        case .retryAsked, .discardAsked:
            break
        case .signOutStarted:
            next.signingOut = true
            next.signOutProblem = nil
        case let .signedOut(generation, unrevoked):
            next = LinkState()
            next.network = state.network
            next.outbox = state.outbox
            next.seenRevision = state.seenRevision
            next.generation = generation
            next.unrevoked = unrevoked
        case let .signOutFailed(problem, generation):
            next.signingOut = false
            next.signOutProblem = problem
            if let generation { next.generation = generation }
        case .reported(.accessEnded):
            next.tokenRefused = true
        case .reported(.needsApproval):
            next.scopeRefused = true
            next.checking = true
        }
        return next
    }

    /// The outbox reports often while a flush runs: its counts are drawn
    /// within a window, never once per landed write.
    public func coalescing(of event: Event) -> Coalescing {
        if case let .outboxChanged(outbox) = event, outbox.flushing { return .within(.milliseconds(100)) }
        return .now
    }

    // MARK: The effects

    public func effects(of event: Event, on state: LinkState, presented presentation: Presentation) -> [Effect] {
        switch event {
        case .loading:
            return [.load]
        case let .loaded(session, _, _):
            // A session loaded (at launch, or a sign-in) sends what waits.
            let flush: [Effect] = state.canSend && state.outbox.sendable > 0 ? [.flush] : []
            return [.followNetwork, .followOutbox] + (session == nil ? [] : [.check, .readPackages] + flush)
        case .checkStarted, .reported(.needsApproval):
            return [.check]
        case .checked:
            // The check carries the build's floors, and the server upgrades
            // a package behind them at once where it may: read them again.
            return state.session == nil || state.sessionProblem != nil || packages.isEmpty ? [] : [.readPackages]
        case let .network(path):
            // Back online: what waits goes.
            return path.online && state.canSend && state.outbox.sendable > 0 ? [.flush] : []
        case .outboxChanged:
            return state.flushDue && state.canSend ? [.flush] : []
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

    /// The flush, a retry and a discard run under no key: the outbox runs
    /// one flush at a time itself, and a write on the wire is never
    /// cancelled for a second one.
    public func key(of effect: Effect) -> String? {
        switch effect {
        case .load, .signOut: "session"
        case .followNetwork: "network"
        case .followOutbox: "outbox"
        case .check: "check"
        case .readPackages: "packages"
        case .flush, .retry, .discard: nil
        }
    }

    public func perform(_ effect: Effect, in environment: AppEnvironment) -> Work<Event> {
        let (sessions, manifest, packages, outboxes, fixed) = (sessions, manifest, packages, outboxes, outbox)
        let transport = environment.devices.transport
        let folder = environment.folder
        let now = environment.clock
        @Sendable func client(_ session: Session) -> SubstrateClient {
            SubstrateClient(server: session.server, token: session.secret, transport: transport)
        }
        /// The outbox of the repository the stored session is signed in to,
        /// found when the work runs: none while the session cannot be read.
        @Sendable func current() async -> Outbox? {
            guard let outboxes else { return fixed }
            return await sessions.load().flatMap { outboxes(SessionFacts($0)) }
        }
        @Sendable func unreadable(_ status: Int32) -> Event {
            .unreadable(SessionUnreadable(status: status).localizedDescription)
        }
        let keepsOutbox = outboxes != nil || fixed != nil
        switch effect {
        case .load:
            return .once {
                let session: Session?
                switch await sessions.read() {
                case let .found(found): session = found
                case .missing: session = nil
                case let .failed(status): return unreadable(status)
                }
                // A revocation cut short at the last launch is tried again,
                // now that the session in use is known.
                let left: [Session]?
                do {
                    left = try await sessions.revokePending(transport: transport)
                } catch let error as SessionUnreadable {
                    return unreadable(error.status)
                } catch {
                    left = nil
                }
                let generation = folder.map { LinkGeneration(folder: $0).current } ?? 0
                return .loaded(
                    session: session.map(SessionFacts.init), unrevoked: left.map { $0.map(SessionFacts.init) }, generation: generation)
            }
        case .followNetwork:
            guard let connectivity = environment.devices.connectivity else { return .none }
            return .sequence { Self.relay(connectivity.paths(), as: Event.network) }
        case .followOutbox:
            guard keepsOutbox else { return .none }
            return .sequence {
                let (stream, continuation) = AsyncStream<Event>.makeStream(bufferingPolicy: .bufferingNewest(1))
                let task = Task {
                    if let outbox = await current() {
                        for await summary in await outbox.changes() { continuation.yield(.outboxChanged(OutboxFacts(summary))) }
                    }
                    continuation.finish()
                }
                continuation.onTermination = { _ in task.cancel() }
                return stream
            }
        case .check:
            return .once {
                switch await sessions.read() {
                case let .found(session):
                    let check = AppStatusCheck(manifest: manifest, session: session, client: client(session), now: { now.now })
                    return .checked(await check.run())
                case .missing:
                    return .checked(AppStatus(checkedAt: now.now))
                case let .failed(status):
                    return unreadable(status)
                }
            }
        case .readPackages:
            return .once {
                guard let session = await sessions.load() else { return nil }
                var held: [String: HeldPackage] = [:]
                for (package, needed) in packages {
                    held[package] = try? await HeldPackage.read(package, needing: needed, from: client(session))
                }
                return .packagesRead(held)
            }
        case .flush:
            // The outbox reports what the flush did; the result is its
            // state once it ends, so a flush that found one running reads
            // that one's.
            guard keepsOutbox else { return .none }
            return .once {
                guard let outbox = await current() else { return nil }
                if let session = await sessions.load() { await outbox.flush(client(session)) }
                return .outboxChanged(OutboxFacts(await outbox.summary))
            }
        case let .retry(id):
            guard keepsOutbox else { return .none }
            return .once {
                guard let outbox = await current() else { return nil }
                try? await outbox.retry(id)
                if let session = await sessions.load() { await outbox.flush(client(session)) }
                return .outboxChanged(OutboxFacts(await outbox.summary))
            }
        case let .discard(id):
            guard keepsOutbox else { return .none }
            return .once {
                try? await current()?.discard(id)
                return nil
            }
        case .signOut:
            return .once {
                let session: Session
                switch await sessions.read() {
                case let .found(found): session = found
                case .missing: return .signedOut(generation: folder.map { LinkGeneration(folder: $0).current } ?? 0, unrevoked: [])
                case let .failed(status):
                    return .signOutFailed(SessionUnreadable(status: status).localizedDescription, generation: nil)
                }
                var generation: Int?
                do {
                    // The generation moves first: work scheduled before this
                    // writes nothing from here on.
                    generation = try folder.map { try LinkGeneration(folder: $0).bump() } ?? 0
                    try await sessions.signOut(session, transport: transport)
                } catch {
                    return .signOutFailed(explain(error, app: manifest.appName, server: session.server), generation: generation)
                }
                // The sign-out only kept the token for revocation: it is
                // revoked now, and only one the server did not confirm waits.
                // A queue or a session that cannot be read leaves them all
                // for the next load.
                var left: [Session] = []
                if let live = try? await sessions.revokePending(transport: transport) {
                    left = live
                } else if let pending = try? await sessions.pendingRevocations() {
                    left = pending
                }
                return .signedOut(generation: generation ?? 0, unrevoked: left.map(SessionFacts.init))
            }
        }
    }

    private static func relay<Element: Sendable>(
        _ source: AsyncStream<Element>,
        as event: @escaping @Sendable (Element) -> Event
    ) -> AsyncStream<Event> {
        AsyncStream { continuation in
            let task = Task {
                for await element in source { continuation.yield(event(element)) }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    // MARK: Composition

    public func children(in state: LinkState) -> [ChildKey: App] { [Self.appKey: app] }

    public func link(in state: LinkState) -> LinkSnapshot? { snapshot(of: state) }

    public func snapshot(of state: LinkState) -> LinkSnapshot {
        LinkSnapshot(
            phase: state.session == nil ? .signedOut : state.accessEnded ? .accessEnded : .signedIn,
            server: state.session?.server, repository: state.session?.repository, scopes: state.session?.scopes,
            accessEnds: state.session?.expiresAt, generation: state.generation, online: state.network?.online,
            packages: state.packages.compactMapValues(\.held), waiting: state.outbox.waiting, refused: state.outbox.parked.count)
    }

    // MARK: The presenter

    /// Each package the app needs, where the repository stands with it.
    public func standings(in state: LinkState) -> [PackageStanding] {
        packages.keys.sorted().map { id in
            PackageStanding(id: id, name: words.packageName(id), held: state.packages[id], needs: packages[id] ?? 1)
        }
    }

    public func present(_ input: PresenterInput<LinkController>) -> Presentation {
        let state = input.state
        let host = state.session.map { ServerAddress.display($0.server) }
        let issues = state.status?.issues ?? []
        // What the app reported stands until the check or a new session
        // says otherwise.
        var standing = issues
        if state.tokenRefused { standing.append(.accessEnded) }
        if state.scopeRefused, !issues.contains(where: { if case .needsApproval = $0 { true } else { false } }) {
            standing.append(.needsApproval(changes: []))
        }
        let standings = standings(in: state)
        let status = LinkStatus.derive(
            signedIn: state.session != nil, packages: standings, outbox: state.outbox.state,
            waiting: state.outbox.waiting, issues: standing,
            notKept: state.outbox.problem ?? input.journalProblem ?? input.childJournalProblem, unreadable: state.sessionProblem)
        let statusPresentation = StatusPresenter.present(
            status, words: words, host: host, packages: standings, sending: state.sending,
            waiting: state.outbox.waiting, parked: state.outbox.parked,
            kindLabels: manifest.kindLabels.mapValues(\.singular),
            locale: input.locale, timeZone: input.timeZone)
        let repository = state.session.map {
            RepositoryPresenter.present(
                session: $0, manifest: manifest, serverVersion: state.status?.serverVersion, unrevoked: state.unrevoked,
                offersChangeAccess: offers.contains(.review), signOutMessage: words.signOut, locale: input.locale,
                timeZone: input.timeZone)
        }
        return LinkPresentation(
            snapshot: snapshot(of: state), repository: repository,
            banner: host.flatMap {
                AccessPresenter.banner(
                    issues: issues.filter { $0 != .accessEnded }, appName: manifest.appName, host: $0,
                    offers: offers, now: input.now, locale: input.locale, timeZone: input.timeZone)
            },
            ended: state.accessEnded ? AccessPresenter.ended(appName: manifest.appName, host: host ?? "Your server") : nil,
            status: statusPresentation,
            signOutProblem: state.signOutProblem.map {
                ProblemPresentation(title: "Couldn't sign out", message: $0, fix: ProblemFix(title: "Try Again", action: .tryAgain))
            },
            app: input.children[Self.appKey])
    }
}

public extension LinkController where App == NoApp {
    /// A link with no app under it: a test, a preview.
    init(
        manifest: AppManifest, sessions: SessionStore, outbox: Outbox? = nil, packages: [String: Int] = [:], words: StatusWords,
        offers: Set<AccessAction.Offer> = Set(AccessAction.Offer.allCases), checkInterval: TimeInterval = 300
    ) {
        self.init(
            app: NoApp(), manifest: manifest, sessions: sessions, outbox: outbox, packages: packages, words: words, offers: offers,
            checkInterval: checkInterval)
    }
}
