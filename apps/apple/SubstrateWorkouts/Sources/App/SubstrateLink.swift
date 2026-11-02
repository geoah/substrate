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

/// One signed-in account's sync: its session, API, outbox and catalogue
/// cache, all for one server and repository, and its own package check.
/// Signing out or signing in elsewhere replaces it; a sync run holds the one
/// it started with and checks it is still the current one after every
/// suspension before it publishes or writes anything.
@MainActor
final class SyncAccount {
    let session: Session
    let api: WorkoutsAPI
    /// One folder per server and repository, beside the journal.
    let directory: URL?
    let outbox: Outbox?
    let remote: RemoteCatalog?
    /// The session controller's writes into this account's outbox.
    private(set) var sink: OutboxSink<EventEnvelope, SessionState>?
    /// This account's package check, nil until it has run.
    var package: PackageState?

    init(session: Session, directory: URL?, localExercises: LocalExercises?, codec: RecordCodec) {
        self.session = session
        api = WorkoutsAPI(client: SubstrateClient(session), authority: session.repository)
        self.directory = directory
        outbox = directory.map { Outbox(directory: $0) }
        remote = directory.map { RemoteCatalog(directory: $0, localExercises: localExercises) }
        sink = outbox.map { outbox in
            OutboxSink(outbox: outbox, codec: codec, onQueued: { [weak self] in
                guard let self else { return }
                await SubstrateLink.shared.flush(self)
            }, onProblem: { [weak self] error in
                guard let self else { return }
                await SubstrateLink.shared.notKept(error, self)
            })
        }
    }

    var host: String { ServerAddress.display(session.server) }
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
    /// The writes the server refused, waiting for the owner to retry or
    /// discard them.
    private(set) var parked: [OutboxEntry] = []
    /// Why the outbox could not keep a change on disk, until one is kept.
    private(set) var notKept: String?
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

    @ObservationIgnored private var account: SyncAccount?
    @ObservationIgnored private var syncTask: (account: ObjectIdentifier, task: Task<Void, Never>)?
    @ObservationIgnored private let codec = RecordCodec()
    @ObservationIgnored private let logger = Logger(subsystem: "dev.reamde.substrate.workouts", category: "sync")

    private init() {
        if let session = SessionStore.workouts.load() { begin(session) }
        Task { await revokePending() }
    }

    var syncStatus: SyncStatus {
        if let problem, session != nil, package == nil { return .failed(problem) }
        return SyncStatus.derive(signedIn: session != nil, package: package, outbox: outboxState, waiting: waiting,
                                 known: packageManifest?.version ?? 0, issues: issues, notKept: notKept)
    }

    /// What the kit's check says that the owner can act on. A password
    /// session reaches everything and never ends.
    private var issues: [AppIssue] {
        guard session?.isApp == true else { return [] }
        return status.status?.issues ?? []
    }

    var host: String? { session.map { ServerAddress.display($0.server) } }

    /// Whether the app may write to its substrate now: signed in, and the
    /// training package checked at this build's version.
    var canWrite: Bool { session != nil && package?.writable == true }

    /// Why the app may not write now, in a sentence; nil when it may.
    var writeBlockedReason: String? {
        guard session != nil else { return "Sign in to your substrate first." }
        guard !canWrite else { return nil }
        return syncStatus.sentence
    }

    // MARK: Signing in

    /// The app authorization flow on a server the sign-in screen probed: the
    /// server's own sign-in and approval page in the system sheet, the code
    /// exchanged with its PKCE verifier on a loopback port. Approving sets up
    /// the health catalogue; the app then installs its own package with the
    /// `declare:` scope the owner approved. A session this replaces (Sign In
    /// Again after the access ended) is revoked like any replaced one.
    func signIn(_ probe: ServerProbe.Result) async throws {
        guard let oauth = OAuthClient(manifest: manifest, probe: probe) else { throw URLError(.badServerResponse) }
        try await adopt(try await oauth.signIn(server: probe.origin), replacing: session)
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

    private func adopt(_ session: Session, replacing old: Session?) async throws {
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

    /// A new account: its own folder, outbox, catalogue and package check. A
    /// sync still running for the account before stops at its next step.
    private func begin(_ session: Session) {
        syncTask?.task.cancel()
        syncTask = nil
        let account = SyncAccount(session: session, directory: Self.directory(for: session),
                                  localExercises: AppModel.shared.directory.map { LocalExercises(directory: $0) }, codec: codec)
        self.account = account
        self.session = session
        package = nil
        problem = nil
        notKept = nil
        outboxState = .idle
        parked = []
        waiting = 0
        syncing = false
        if let remote = account.remote, remote.hasCache, let cached = try? remote.loadNow() {
            AppModel.shared.showServerCatalog(cached, host: account.host)
        }
        status.reset()
        Task { await status.refresh(force: true) }
        Task {
            if let unreadable = await account.outbox?.unreadable { AppModel.shared.noteUnreadable(unreadable) }
            await refreshOutbox(account)
        }
    }

    private func end() {
        syncTask?.task.cancel()
        syncTask = nil
        account = nil
        session = nil
        package = nil
        problem = nil
        notKept = nil
        outboxState = .idle
        waiting = 0
        parked = []
        syncing = false
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

    /// Whether `account` is still the one signed in, and the run that holds
    /// it was not stopped.
    private func isCurrent(_ account: SyncAccount) -> Bool {
        self.account === account && !Task.isCancelled
    }

    // MARK: Sync

    /// At launch, on return to the foreground and after a sign-in: the
    /// package checked and installed or upgraded, the routines read, the
    /// sessions exchanged, and everything waiting sent. One at a time per
    /// account; a call while one runs for the same account waits for it.
    func sync() async {
        guard let account else { return }
        if let running = syncTask, running.account == ObjectIdentifier(account) { return await running.task.value }
        await run(account, overwritingEdits: false)
    }

    /// The upgrade the owner chose to let apply over their edits to the
    /// package's kinds. The server still refuses one that would remove
    /// anything the package holds.
    func upgradeOverEdits() async {
        guard let account else { return }
        if let running = syncTask, running.account == ObjectIdentifier(account) { await running.task.value }
        guard isCurrent(account) else { return }
        await run(account, overwritingEdits: true)
    }

    private func run(_ account: SyncAccount, overwritingEdits: Bool) async {
        let id = ObjectIdentifier(account)
        let task = Task { await runSync(account, overwritingEdits: overwritingEdits) }
        syncTask = (id, task)
        await task.value
        if syncTask?.account == id { syncTask = nil }
    }

    private func runSync(_ account: SyncAccount, overwritingEdits: Bool) async {
        guard let packageManifest else { return }
        syncing = true
        defer { if self.account === account { syncing = false } }
        do {
            let installer = PackageInstaller(api: account.api, manifest: packageManifest, baselineDirectory: account.directory)
            let state = try await installer.ensure(overwritingEdits: overwritingEdits)
            guard isCurrent(account) else { return }
            account.package = state
            package = state
            problem = nil
            logger.info("training package: \(String(describing: state), privacy: .public)")
            guard state.writable else {
                await refreshOutbox(account)
                return
            }
            if let remote = account.remote {
                let catalog = try await remote.refresh(account.api)
                guard isCurrent(account) else { return }
                AppModel.shared.showServerCatalog(catalog, host: account.host)
            }
            await exchangeHistory(account)
            guard isCurrent(account) else { return }
            await flush(account)
        } catch {
            if isCurrent(account) { note(error) }
        }
    }

    /// Sends what waits, when the package is in place.
    func flush() async {
        guard let account else { return }
        await flush(account)
    }

    /// Sends what waits in `account`'s outbox, when its package is in place
    /// and it is still the account signed in.
    func flush(_ account: SyncAccount) async {
        guard isCurrent(account), let outbox = account.outbox, account.package?.writable == true else {
            await refreshOutbox(account)
            return
        }
        let state = await outbox.flush(account.api)
        guard isCurrent(account) else { return }
        outboxState = state
        await refreshOutbox(account)
        if state == .signedOut || state == .needsApproval { Task { await status.refresh(force: true) } }
    }

    /// The history exchange (`HistoryExchange`): sessions on the server the
    /// phone lacks are imported and server edits merged; sessions deleted on
    /// the server after this phone sent them are removed here; sessions and
    /// exercises the server never held are queued, and sessions deleted on
    /// the phone purged there.
    private func exchangeHistory(_ account: SyncAccount) async {
        guard let outbox = account.outbox else { return }
        do {
            let snapshot = try await ServerHistory(api: account.api, codec: codec).snapshot()
            let exercises = Set(try await account.api.list([codec.kinds.exercise]).map(\.id))
            let queued = Set(await outbox.entries.compactMap(\.session))
            let versions = await outbox.versions
            guard isCurrent(account) else { return }
            let app = AppModel.shared
            let plan = HistoryExchange.plan(server: snapshot, local: app.history.map(\.records), queued: queued, versions: versions,
                                            tombstones: app.tombstones?.all ?? [], codec: codec)
            for record in plan.unreadable { app.noteUnreadable(record) }
            app.importServerSessions(plan.imports)
            app.mergeServerSessions(plan.merges)
            app.removeSessionsDeletedOnServer(plan.removals)
            try await outbox.saw(plan.seen)
            for id in plan.removals { try await outbox.forget(codec.kinds.session, id.rawValue) }
            var entries: [OutboxEntry] = []
            for exercise in app.exercises.values.sorted(by: { $0.id.rawValue < $1.id.rawValue })
                where exercise.isLocal && !exercises.contains(exercise.id.rawValue)
            {
                entries.append(OutboxPlan.exerciseAdded(exercise, codec: codec))
            }
            for records in plan.uploads.sorted(by: { $0.session.at < $1.session.at }) {
                entries += OutboxPlan.finished(records, codec: codec)
            }
            entries += plan.finishes
            entries += plan.purges.map { OutboxPlan.deleted($0, codec: codec) }
            guard isCurrent(account) else { return }
            if !entries.isEmpty { try await outbox.enqueue(entries) }
        } catch {
            if isCurrent(account) { note(error) }
        }
    }

    /// Copies the routines this build carries, with their exercises, into a
    /// repository that has none, then reads the catalogue again. Only into
    /// a package checked at this build's version, like every write.
    func copyStarterRoutines() async {
        guard let account, account.package?.writable == true, let remote = account.remote, !copyingStarter else { return }
        copyingStarter = true
        defer { copyingStarter = false }
        do {
            let bundled = try BundledCatalog().loadNow()
            _ = try await remote.copyStarter(account.api, from: bundled)
            guard isCurrent(account) else { return }
            let catalog = try await remote.refresh(account.api)
            guard isCurrent(account) else { return }
            AppModel.shared.showServerCatalog(catalog, host: account.host)
        } catch {
            if isCurrent(account) { note(error) }
        }
    }

    // MARK: Refused writes

    /// Sends a parked write again.
    func retry(_ entry: OutboxEntry) async {
        guard let account, let outbox = account.outbox else { return }
        do {
            try await outbox.retry(entry.id)
        } catch {
            notKept(error, account)
            return
        }
        await flush(account)
    }

    /// Gives up a parked write and the writes of its session waiting behind
    /// it. The session stays on this iPhone.
    func discard(_ entry: OutboxEntry) async {
        guard let account, let outbox = account.outbox else { return }
        do {
            try await outbox.discard(entry.id)
        } catch {
            notKept(error, account)
            return
        }
        await flush(account)
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

    /// The outbox could not keep a change of `account` on disk.
    func notKept(_ error: any Error, _ account: SyncAccount) {
        logger.error("outbox: \(String(describing: error), privacy: .public)")
        guard isCurrent(account) else { return }
        notKept = error.localizedDescription
    }

    /// The outbox's count, parked writes and disk state, for `account` when
    /// it is still current.
    private func refreshOutbox(_ account: SyncAccount) async {
        let entries = await account.outbox?.entries ?? []
        let saveProblem = await account.outbox?.saveProblem
        guard isCurrent(account) else { return }
        waiting = entries.count
        parked = entries.filter(\.parked)
        notKept = saveProblem
    }

    // MARK: The session controller's seam

    func finished(_ records: SessionRecords) async {
        await account?.sink?.finished(records)
    }

    func discarded(_ id: SessionID) async {
        await account?.sink?.discarded(id)
    }

    /// A finished session deleted from History, after its tombstone is kept.
    func deleted(_ id: SessionID) async {
        await account?.sink?.deleted(id)
    }

    func healthLinked(_ id: SessionID, healthKitWorkout: String, revision: Int) async {
        await account?.sink?.healthLinked(id, healthKitWorkout: healthKitWorkout, revision: revision)
    }

    func exerciseAdded(_ exercise: Exercise) async {
        await account?.sink?.exerciseAdded(exercise)
    }
}

/// The session controller's and the Health save's sync seam: every change
/// goes to `SubstrateLink`, which queues it in the signed-in account's outbox
/// and leaves it to the next sign-in's exchange otherwise.
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
