import Foundation
import Observation
import os
import SubstrateKit
import WorkoutsCore
import WorkoutsSync

extension SessionStore {
    /// The session, kept where every build keeps it: readable after the
    /// first unlock, so a Live Activity button or a background refresh can
    /// use it, and never synced off this device.
    static let workouts = SessionStore(service: "dev.reamde.substrate.workouts")
}

/// The app's substrate: the sign-in, the training package it keeps there,
/// the routines it reads from it and the outbox it writes through
/// (README.md, "Signing in and sync"). Without a session the app runs on the
/// routines it carries and keeps every session on the phone; signing in
/// later sends them.
@MainActor
@Observable
final class SubstrateLink {
    static let shared = SubstrateLink()

    private(set) var session: Session?
    private(set) var package: PackageState?
    private(set) var outboxState: OutboxState = .idle
    private(set) var waiting = 0
    /// Why the last package check or catalogue read did not finish.
    private(set) var problem: String?
    private(set) var syncing = false
    private(set) var copyingStarter = false
    /// Tokens this device stopped using that the server has not revoked yet.
    private(set) var unrevoked: [Session] = []
    var signOutProblem: String?
    /// The browser sheet is open for a change of access.
    private(set) var authorizing = false

    let manifest = WorkoutsManifest.current
    @ObservationIgnored let packageManifest: PackageManifest? = try? PackageManifest.bundled()
    /// Whether an app session is still set up with its server: the kit's
    /// check, at launch and on return to the foreground.
    @ObservationIgnored private(set) lazy var status = AppStatusModel { [weak self] in self?.statusCheck() }

    @ObservationIgnored private var outbox: Outbox?
    @ObservationIgnored private var remote: RemoteCatalog?
    @ObservationIgnored private var syncTask: Task<Void, Never>?
    @ObservationIgnored private let codec = RecordCodec()
    @ObservationIgnored private let logger = Logger(subsystem: "dev.reamde.substrate.workouts", category: "sync")

    private init() {
        if let session = SessionStore.workouts.load() { begin(session) }
        Task { await revokePending() }
    }

    var syncStatus: SyncStatus {
        if let problem, session != nil, package == nil { return .failed(problem) }
        return SyncStatus.derive(signedIn: session != nil, package: package, outbox: outboxState, waiting: waiting,
                                 known: packageManifest?.version ?? 0)
    }

    var host: String? { session.map { ServerAddress.display($0.server) } }

    private var api: WorkoutsAPI? {
        session.map { WorkoutsAPI(client: SubstrateClient($0), authority: $0.repository) }
    }

    // MARK: Signing in

    /// The app authorization flow on a server the sign-in screen probed: the
    /// server's own sign-in and approval page in the system sheet, the code
    /// exchanged with its PKCE verifier on a loopback port. Approving sets up
    /// the health catalogue; the app then installs its own package with the
    /// `declare:` scope the owner approved.
    func signIn(_ probe: ServerProbe.Result) async throws {
        guard let oauth = OAuthClient(manifest: manifest, probe: probe) else { throw URLError(.badServerResponse) }
        try await adopt(try await oauth.signIn(server: probe.origin))
    }

    /// The flow again with the whole request: Change Access, a renewal, a
    /// scope the owner left out. The old access works until the new one is
    /// kept.
    func reauthorize() async {
        guard let old = session, !authorizing else { return }
        authorizing = true
        defer { authorizing = false }
        do {
            let probe = try await ServerProbe().probe(old.server, requirement: manifest.requirement, app: manifest.appName)
            guard manifest.door(for: probe) == .app, let oauth = OAuthClient(manifest: manifest, probe: probe) else {
                problem = "\(ServerAddress.display(old.server)) can\u{2019}t give \(manifest.appName) access of its own: it needs an update first."
                return
            }
            try await adopt(try await oauth.signIn(server: old.server), replacing: old)
        } catch let error where error is WebAuthenticator.Cancelled || error is CancellationError {
            // The sheet was closed: the access stays as it was.
        } catch {
            problem = explain(error, app: manifest.appName, server: old.server)
        }
    }

    private func adopt(_ session: Session, replacing old: Session? = nil) async throws {
        try await SessionStore.workouts.save(session, replacing: old)
        begin(session)
        if old != nil { await revokePending() }
        await sync()
    }

    /// Forgets the session on this phone and revokes the token in the
    /// background. What still waits in the outbox stays with that repository
    /// and goes when it signs in again; the app returns to its own routines.
    func signOut() {
        guard let session else { return }
        signOutProblem = nil
        Task {
            do {
                try await SessionStore.workouts.signOut(session)
            } catch {
                signOutProblem = explain(error, app: self.manifest.appName, server: session.server)
                return
            }
            end()
            AppModel.shared.showBundledCatalog()
            await revokePending()
        }
    }

    func revokePending() async {
        unrevoked = await SessionStore.workouts.revokePending(current: SessionStore.workouts.load())
    }

    private func begin(_ session: Session) {
        self.session = session
        package = nil
        problem = nil
        outboxState = .idle
        let directory = Self.directory(for: session)
        outbox = directory.map { Outbox(directory: $0) }
        remote = directory.map { RemoteCatalog(directory: $0, localExercises: AppModel.shared.directory.map { LocalExercises(directory: $0) }) }
        if let remote, remote.hasCache, let cached = try? remote.loadNow() {
            AppModel.shared.showServerCatalog(cached, host: ServerAddress.display(session.server))
        }
        status.reset()
        Task { await status.refresh(force: true) }
        Task { await countWaiting() }
    }

    private func end() {
        session = nil
        package = nil
        outbox = nil
        remote = nil
        outboxState = .idle
        waiting = 0
        status.reset()
    }

    /// One folder per server and repository, beside the journal.
    private static func directory(for session: Session) -> URL? {
        guard let base = AppModel.shared.directory else { return nil }
        let host = session.server.host() ?? "server"
        let port = session.server.port.map { "-\($0)" } ?? ""
        return base.appendingPathComponent("substrate", isDirectory: true)
            .appendingPathComponent("\(host)\(port)-\(session.repository)", isDirectory: true)
    }

    private func statusCheck() -> AppStatusCheck? {
        guard let session, session.isApp else { return nil }
        return AppStatusCheck(manifest: manifest, session: session)
    }

    // MARK: Sync

    /// At launch, on return to the foreground and after a sign-in: the
    /// package checked and installed or upgraded, the routines read, the
    /// sessions another writer left fetched, and everything waiting sent.
    /// One at a time; a call while one runs waits for it.
    func sync() async {
        if let syncTask { return await syncTask.value }
        let task = Task { await runSync() }
        syncTask = task
        await task.value
        syncTask = nil
    }

    private func runSync() async {
        guard let api, let packageManifest else { return }
        syncing = true
        defer { syncing = false }
        do {
            let installer = PackageInstaller(api: api, manifest: packageManifest)
            let state = try await installer.ensure()
            package = state
            problem = nil
            logger.info("training package: \(String(describing: state), privacy: .public)")
            guard state.writable else { return }
            if let remote {
                let catalog = try await remote.refresh(api)
                AppModel.shared.showServerCatalog(catalog, host: ServerAddress.display(api.client.server))
            }
            await exchangeHistory(api)
            await flush()
        } catch {
            note(error)
        }
    }

    /// Sends what waits, when the package is in place.
    func flush() async {
        guard let api, let outbox, package?.writable == true else {
            await countWaiting()
            return
        }
        outboxState = await outbox.flush(api)
        await countWaiting()
        if outboxState == .signedOut || outboxState == .needsApproval { Task { await status.refresh(force: true) } }
    }

    /// The sessions on the server that this phone lacks go into its
    /// history; the sessions and exercises this phone has that the server
    /// lacks, from before a sign-in or another repository, are queued.
    private func exchangeHistory(_ api: WorkoutsAPI) async {
        guard let outbox else { return }
        do {
            let server = try await ServerHistory(api: api, codec: codec).sessions()
            AppModel.shared.importServerSessions(server)
            let held = Set(server.map(\.id))
            let queued = Set(await outbox.entries.compactMap(\.session))
            let exercises = Set(try await api.list([codec.kinds.exercise]).map(\.id))
            var plan: [OutboxEntry] = []
            for exercise in AppModel.shared.exercises.values where exercise.isLocal && !exercises.contains(exercise.id.rawValue) {
                plan.append(OutboxPlan.exerciseAdded(exercise, codec: codec))
            }
            for entry in AppModel.shared.history.reversed() where !held.contains(entry.id) && !queued.contains(entry.id.rawValue) {
                plan += OutboxPlan.finished(entry.records, codec: codec)
            }
            if !plan.isEmpty { await outbox.enqueue(plan) }
        } catch {
            note(error)
        }
    }

    /// Copies the routines this build carries, with their exercises, into a
    /// repository that has none, then reads the catalogue again.
    func copyStarterRoutines() async {
        guard let api, let remote, !copyingStarter else { return }
        copyingStarter = true
        defer { copyingStarter = false }
        do {
            let bundled = try BundledCatalog().loadNow()
            _ = try await remote.copyStarter(api, from: bundled)
            let catalog = try await remote.refresh(api)
            AppModel.shared.showServerCatalog(catalog, host: ServerAddress.display(api.client.server))
        } catch {
            note(error)
        }
    }

    private func note(_ error: any Error) {
        switch error {
        case is SessionExpired: outboxState = .signedOut
        case is InsufficientScope: outboxState = .needsApproval
        case let server as ServerError where server.status == 403: outboxState = .needsApproval
        default: problem = explain(error, app: manifest.appName, server: session?.server)
        }
        logger.error("sync: \(String(describing: error), privacy: .public)")
    }

    private func countWaiting() async {
        waiting = await outbox?.entries.count ?? 0
    }

    // MARK: The session controller's seam

    func finished(_ records: SessionRecords) async {
        guard let outbox, session != nil else { return }
        await outbox.enqueue(OutboxPlan.finished(records, codec: codec))
        await flush()
    }

    func discarded(_ id: SessionID) async {
        guard let outbox else { return }
        if await outbox.drop(session: id.rawValue, kind: codec.kinds.session) {
            await outbox.enqueue([OutboxPlan.deleted(id, codec: codec)])
        }
        await flush()
    }

    func deleted(_ id: SessionID) async {
        guard let outbox else { return }
        await outbox.drop(session: id.rawValue, kind: codec.kinds.session)
        await outbox.enqueue([OutboxPlan.deleted(id, codec: codec)])
        await flush()
    }

    func healthLinked(_ id: SessionID, healthKitWorkout: String, revision: Int) async {
        guard let outbox, session != nil else { return }
        await outbox.enqueue([OutboxPlan.healthLinked(id, healthKitWorkout: healthKitWorkout, revision: revision, codec: codec)])
        await flush()
    }

    func exerciseAdded(_ exercise: Exercise) async {
        guard let outbox, session != nil else { return }
        await outbox.enqueue([OutboxPlan.exerciseAdded(exercise, codec: codec)])
        await flush()
    }
}

/// The session controller's and the Health save's sync seam: every change
/// goes to `SubstrateLink`, which queues it when signed in and leaves it to
/// the next sign-in's exchange otherwise.
struct LinkSink: SessionSyncSink {
    nonisolated init() {}

    nonisolated func applied(_: EventEnvelope, state _: SessionState) async {}
    nonisolated func finished(_ records: SessionRecords) async { await SubstrateLink.shared.finished(records) }
    nonisolated func discarded(_ sessionID: SessionID) async { await SubstrateLink.shared.discarded(sessionID) }
    nonisolated func healthLinked(_ sessionID: SessionID, healthKitWorkout: String, revision: Int) async {
        await SubstrateLink.shared.healthLinked(sessionID, healthKitWorkout: healthKitWorkout, revision: revision)
    }
    nonisolated func exerciseAdded(_ exercise: Exercise) async { await SubstrateLink.shared.exerciseAdded(exercise) }
}
