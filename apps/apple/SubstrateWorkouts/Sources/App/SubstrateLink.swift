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
    /// Sessions deleted on the phone while signed in here: purged on this
    /// server and repository only, and never imported from it again.
    let tombstones: SessionTombstones?
    let remote: RemoteCatalog?
    /// Whether the sample routine was written here, so a deleted one never
    /// comes back (decision S1).
    let sample: SampleInstall?
    /// The routine edit not saved yet, kept on every change.
    let drafts: RoutineDrafts?
    /// Where the exercise library stands in this repository.
    let library: LibraryInstall?
    /// The session controller's writes into this account's outbox.
    private(set) var sink: OutboxSink<EventEnvelope, SessionState>?
    /// This account's package check, nil until it has run.
    var package: PackageState?

    init(session: Session, directory: URL?, localExercises: LocalExercises?, codec: RecordCodec) {
        self.session = session
        api = WorkoutsAPI(client: SubstrateClient(session), authority: session.repository)
        self.directory = directory
        outbox = directory.map { Outbox(directory: $0) }
        tombstones = directory.map { SessionTombstones(directory: $0) }
        remote = directory.map { RemoteCatalog(directory: $0, localExercises: localExercises) }
        sample = directory.map { SampleInstall(directory: $0) }
        drafts = directory.map { RoutineDrafts(directory: $0) }
        library = directory.map { LibraryInstall(directory: $0) }
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
    /// Routines whose edit, create or delete waits for the server, by id.
    private(set) var waitingRoutines: Set<RoutineID> = []
    /// The routine edit not saved yet, kept on disk (`keepDraft`).
    private(set) var draft: RoutineEditor?
    /// Why the outbox could not keep a change on disk, until one is kept.
    private(set) var notKept: String?
    /// Why the last package check or catalogue read did not finish.
    private(set) var problem: String?
    private(set) var syncing = false
    private(set) var addingSample = false
    /// The signed-in repository's sample: nil until its first run is settled.
    private(set) var sampleState: SampleInstall.State?
    /// Why the sample could not be written at the last sync; tried again at
    /// the next. Shown where the sample would be, never in the way of sync.
    private(set) var sampleProblem: String?
    /// Tokens this device stopped using that the server has not revoked yet.
    private(set) var unrevoked: [Session] = []
    var signOutProblem: String?
    /// The browser sheet is open for a change of access.
    private(set) var authorizing = false

    let manifest = WorkoutsManifest.current
    @ObservationIgnored let packageManifest: PackageManifest? = try? PackageManifest.bundled()
    @ObservationIgnored let coachManifest: CoachManifest? = try? CoachManifest.bundled()
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
                                 known: packageManifest?.version ?? 0, issues: issues, notKept: notKept,
                                 server: host ?? "your substrate")
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
    /// exchanged with its PKCE verifier. Approving sets up
    /// the health catalogue; the app then installs its own package with the
    /// `declare:` scope the owner approved. A session this replaces (Sign In
    /// Again after the access ended) is revoked like any replaced one.
    func signIn(_ probe: ServerProbe.Result) async throws {
        guard let oauth = OAuthClient(manifest: manifest, probe: probe) else { throw URLError(.badServerResponse) }
        try await adopt(try await oauth.signIn(server: probe.origin, repository: probe.repository,
                                                        keepingRejectedIn: .workouts), replacing: session)
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
        waitingRoutines = []
        waiting = 0
        syncing = false
        sampleState = account.sample?.state
        sampleProblem = nil
        draft = loadDraft(account)
        if let remote = account.remote, remote.hasCache, let cached = try? remote.loadNow() {
            AppModel.shared.showServerCatalog(cached, host: account.host)
        }
        status.reset()
        Task { await status.refresh(force: true) }
        Task {
            if let unreadable = await account.outbox?.unreadable { AppModel.shared.noteUnreadable(unreadable) }
            if let unreadable = account.tombstones?.unreadable { AppModel.shared.noteUnreadable(unreadable) }
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
        waitingRoutines = []
        syncing = false
        sampleState = nil
        sampleProblem = nil
        draft = nil
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
        await run(account)
    }

    /// A sync that starts once any running one ends: a change made while
    /// one runs (an import) was not in the history it read.
    func syncAfterRunning() async {
        guard let account else { return }
        if let running = syncTask, running.account == ObjectIdentifier(account) { await running.task.value }
        await sync()
    }

    private func run(_ account: SyncAccount) async {
        let id = ObjectIdentifier(account)
        let task = Task { await runSync(account) }
        syncTask = (id, task)
        await task.value
        if syncTask?.account == id { syncTask = nil }
    }

    private func runSync(_ account: SyncAccount) async {
        guard let packageManifest else { return }
        syncing = true
        defer { if self.account === account { syncing = false } }
        do {
            let installer = PackageInstaller(api: account.api, manifest: packageManifest, baselineDirectory: account.directory,
                                             clientID: manifest.clientID)
            let state = try await installer.ensure()
            guard isCurrent(account) else { return }
            account.package = state
            package = state
            problem = nil
            logger.info("training package: \(String(describing: state), privacy: .public)")
            guard state.writable else {
                await refreshOutbox(account)
                return
            }
            await ensureCoach(account, training: state)
            guard isCurrent(account) else { return }
            if let remote = account.remote {
                let read = try await CatalogSync.read(account.api, remote: remote, outbox: account.outbox, sample: account.sample,
                                                      bundled: { try BundledCatalog().loadNow() })
                guard isCurrent(account) else { return }
                sampleState = account.sample?.state
                sampleProblem = read.sampleProblem
                if let problem = read.sampleProblem {
                    logger.error("sample routine: \(problem, privacy: .public)")
                }
                AppModel.shared.showServerCatalog(read.catalog, host: account.host)
            }
            await exchangeHistory(account)
            guard isCurrent(account) else { return }
            await flush(account)
            guard isCurrent(account) else { return }
            await seedLibrary(account)
        } catch {
            if isCurrent(account) { note(error) }
        }
    }

    /// Installs or updates the workout coach (decision 0207) when the token
    /// approves it and training is current. Quiet: whatever it meets is
    /// logged, never shown, and never stops the sync.
    private func ensureCoach(_ account: SyncAccount, training: PackageState) async {
        guard let coachManifest else { return }
        let approval = CoachInstaller.approval(in: account.session.details, authority: account.api.rehome.authority)
        let installer = CoachInstaller(api: account.api, manifest: coachManifest, approval: approval, clientID: manifest.clientID)
        do {
            let state = try await installer.ensure(training: training)
            logger.info("workout coach: \(String(describing: state), privacy: .public)")
        } catch {
            logger.error("workout coach: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// Seeds the exercise library (`LibraryInstall`) where the repository is
    /// behind this build's, after the sessions are sent so a first seed of a
    /// thousand records never holds them back, then reads the catalogue
    /// again. Quiet like the coach: what it meets is logged and tried again
    /// at the next sync.
    private func seedLibrary(_ account: SyncAccount) async {
        guard let installer = account.library, let library = try? ExerciseLibrary.bundled(), let remote = account.remote else { return }
        do {
            let outcome = try await installer.ensure(account.api, library: library)
            logger.info("exercise library \(library.version, privacy: .public): \(outcome.written) written, \(outcome.filled) filled")
            guard !outcome.current, isCurrent(account) else { return }
            let outbox = account.outbox
            let codec = codec
            let catalog = try await remote.refresh(account.api, hiding: { await outbox?.waitingDeletes ?? [] },
                                                   waiting: { await outbox?.waitingRoutines(codec: codec) ?? [:] })
            guard isCurrent(account) else { return }
            AppModel.shared.showServerCatalog(catalog, host: account.host)
        } catch {
            logger.error("exercise library: \(error.localizedDescription, privacy: .public)")
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
        let shown = Set(parked.filter(\.conflict).map(\.id))
        let state = await outbox.flush(account.api)
        guard isCurrent(account) else { return }
        outboxState = state
        await refreshOutbox(account)
        // A conflict new in this flush: the routine as the server holds it.
        let conflicts = parked.filter { $0.conflict && $0.kind == codec.kinds.routine && !shown.contains($0.id) }
        if !conflicts.isEmpty { await reread(conflicts.map { RoutineID($0.recordID) }, account) }
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
                                            tombstones: account.tombstones?.all ?? [], hidden: app.tombstones?.all ?? [],
                                            codec: codec)
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
            // The exercises a session to upload names that the repository
            // lacks: the sample's, for a session logged signed out.
            let queuedExercises = Set(entries.map(\.recordID))
            let bundled = (try? BundledCatalog().loadNow())?.exercises ?? [:]
            let known = bundled.merging(app.exercises) { _, phone in phone }
            entries += OutboxPlan.exercisesFor(plan.uploads, server: exercises.union(queuedExercises), known: known, codec: codec)
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

    /// Writes the sample routine back, with any of its exercises the
    /// repository lacks, after the owner deleted it; then reads the
    /// catalogue again. Only into a package checked at this build's version,
    /// like every write.
    func addSampleRoutine() async {
        guard let account, account.package?.writable == true, let remote = account.remote, !addingSample else { return }
        addingSample = true
        defer { addingSample = false }
        do {
            let outbox = account.outbox
            try await account.sample?.addBack(account.api, remote: remote, outbox: outbox, bundled: BundledCatalog().loadNow())
            guard isCurrent(account) else { return }
            sampleState = account.sample?.state
            sampleProblem = nil
            let codec = codec
            let catalog = try await remote.refresh(account.api, hiding: { await outbox?.waitingDeletes ?? [] },
                                                   waiting: { await outbox?.waitingRoutines(codec: codec) ?? [:] })
            guard isCurrent(account) else { return }
            AppModel.shared.showServerCatalog(catalog, host: account.host)
        } catch {
            if isCurrent(account) { note(error) }
        }
    }

    /// Whether a routine may be deleted from the app: signed in, with an
    /// outbox to send the delete through, and a package the app may write.
    /// While the package is unchecked the delete is queued like any write;
    /// one known not writable (newer, changed outside the app) would hold the
    /// delete back with no word of why, so Delete is not offered, and the
    /// Routines banner says what stops the writes.
    var canDeleteRoutines: Bool {
        guard account?.outbox != nil, account?.remote != nil else { return false }
        return package.map(\.writable) ?? true
    }

    /// Deletes a routine the owner chose: out of the cached catalogue at
    /// once, and purged on the server through the outbox. The exercises it
    /// names stay, since sessions name them too. A delete the outbox cannot
    /// keep on disk changes nothing and is said.
    func deleteRoutine(_ id: RoutineID) async {
        guard let account, let outbox = account.outbox, let remote = account.remote else { return }
        do {
            try await outbox.delete(routine: id, codec: codec)
        } catch {
            notKept(error, account)
            return
        }
        guard isCurrent(account) else { return }
        do {
            let catalog = try remote.remove(routine: id)
            if remote.hasCache { AppModel.shared.showServerCatalog(catalog, host: account.host) }
        } catch {
            // The cache could not be rewritten; the delete still goes, and
            // the next refresh leaves the routine out until it lands.
            note(error)
        }
        await flush(account)
    }

    // MARK: Editing routines

    /// The routine edit kept on disk and not saved, read when the account
    /// begins; one that cannot be read is set aside and listed in Settings.
    private func loadDraft(_ account: SyncAccount) -> RoutineEditor? {
        guard let drafts = account.drafts else { return nil }
        do {
            return try drafts.load()
        } catch let error as UnreadableDraft {
            AppModel.shared.noteUnreadable(error.record)
        } catch {
            logger.error("routine draft: \(error.localizedDescription, privacy: .public)")
        }
        return nil
    }

    /// Keeps the edit in progress on disk: an edit survives the app being
    /// killed before Save.
    func keepDraft(_ editor: RoutineEditor) {
        draft = editor
        do {
            try account?.drafts?.save(editor)
        } catch {
            logger.error("routine draft not kept: \(error.localizedDescription, privacy: .public)")
        }
    }

    func dropDraft() {
        draft = nil
        try? account?.drafts?.clear()
    }

    /// Saves an edited or duplicated routine: queued in the outbox (a
    /// guarded PATCH of what changed, or a PUT of a new routine), into the
    /// cached catalogue at once, the draft dropped, and sent. An edit the
    /// outbox cannot keep on disk changes nothing and throws, and the draft
    /// stays.
    func saveRoutine(_ edited: RoutineEditor) async throws {
        guard let account, let outbox = account.outbox, let remote = account.remote else { throw OutboxNotSaved(reason: "the app is not signed in to your substrate.") }
        guard conflict(for: edited.routine.id) == nil else { throw OutboxNotSaved(reason: Self.conflictFirst) }
        let editor = edited.finished
        if editor.isNew {
            try await outbox.enqueue([OutboxPlan.routineCreated(editor.routine, codec: codec)])
        } else {
            try await outbox.edit(routine: editor.routine.id, patch: editor.patch(codec: codec), baseVersion: editor.baseVersion,
                                  codec: codec)
        }
        try? account.drafts?.clear()
        guard isCurrent(account) else { return }
        draft = nil
        do {
            let catalog = try remote.store(routine: editor.routine)
            AppModel.shared.showServerCatalog(catalog, host: account.host)
        } catch {
            // The cache could not be rewritten; the write still goes, and the
            // next read lays it over the server's routine until it lands.
            note(error)
        }
        await refreshOutbox(account)
        // The sheet closes once the edit is queued; sending is the outbox's.
        Task { await flush(account) }
    }

    /// What an import (Hevy's) writes beside the sessions: routines, queued
    /// as new routines (a create that refuses an id another writer took
    /// meanwhile) after every new exercise they name, since a routine's slots
    /// must find theirs, and into the cached catalogue at once; and body
    /// measurements, each a PUT at the source's id, so an import again
    /// rewrites the same record. Then sent.
    func importRecords(routines: [Routine], measurements: [FlatRecord], exercises: [Exercise]) async throws {
        guard let account, let outbox = account.outbox, let remote = account.remote else {
            throw OutboxNotSaved(reason: "the app is not signed in to your substrate.")
        }
        let named = Set(routines.flatMap { $0.slots.map(\.exercise) })
        var entries = exercises.filter { named.contains($0.id) }.map { OutboxPlan.exerciseAdded($0, codec: codec) }
        entries += routines.map { OutboxPlan.routineCreated($0, codec: codec) }
        entries += measurements.map { OutboxEntry(method: .put, kind: $0.kind, recordID: $0.id, properties: $0.properties) }
        try await outbox.enqueue(entries)
        guard isCurrent(account) else { return }
        do {
            var catalog: Catalog?
            for routine in routines { catalog = try remote.store(routine: routine) }
            if let catalog { AppModel.shared.showServerCatalog(catalog, host: account.host) }
        } catch {
            note(error)
        }
        await refreshOutbox(account)
        Task { await flush(account) }
    }

    static let conflictFirst = "This routine has an edit waiting for your choice after a conflict. Choose Keep My Edit or Use Theirs on its page first."

    /// Reads routines again from the server, one by one, into the cache: a
    /// conflicted one shows as the server holds it, which is what Use
    /// Theirs keeps, and one Keep My Edit just wrote shows the merge.
    private func reread(_ ids: [RoutineID], _ account: SyncAccount) async {
        guard let remote = account.remote else { return }
        for id in ids {
            do {
                let record = try await account.api.get(codec.kinds.routine, id.rawValue)
                guard isCurrent(account) else { return }
                let catalog = try remote.store(record: record, removing: id)
                AppModel.shared.showServerCatalog(catalog, host: account.host)
            } catch {
                logger.error("routine \(id.rawValue, privacy: .public) not read again: \(error.localizedDescription, privacy: .public)")
            }
        }
    }

    /// The edit of a routine the server refused because the routine changed
    /// there since the edit began, waiting for the owner's choice.
    func conflict(for routine: RoutineID) -> OutboxEntry? {
        parked.first { $0.conflict && $0.kind == codec.kinds.routine && $0.recordID == routine.rawValue }
    }

    /// The owner's answer to a conflict: keep their edit (it is sent
    /// without a version, replacing what it changed), or take the server's
    /// routine (the edit is dropped and the routines read again).
    func resolve(_ entry: OutboxEntry, keepMine: Bool) async {
        guard let account, let outbox = account.outbox else { return }
        do {
            try await outbox.resolve(entry.id, keepMine: keepMine)
        } catch {
            notKept(error, account)
            return
        }
        await refreshOutbox(account)
        let id = RoutineID(entry.recordID)
        if keepMine {
            await flush(account)
            if !waitingRoutines.contains(id), conflict(for: id) == nil { await reread([id], account) }
        } else {
            await reread([id], account)
            await flush(account)
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
        waitingRoutines = Set(entries.filter { $0.kind == codec.kinds.routine && $0.session == nil && !$0.parked }
            .map { RoutineID($0.recordID) })
        notKept = saveProblem
    }

    // MARK: The session controller's seam

    func finished(_ records: SessionRecords) async {
        await account?.sink?.finished(records)
    }

    func discarded(_ id: SessionID) async {
        await account?.sink?.discarded(id)
    }

    /// Keeps a delete's tombstone where it belongs: in the signed-in
    /// account's folder, so only that server and repository purge it. False
    /// when signed out (or in previews): the caller keeps it on the phone.
    /// Sessions deleted on the phone while signed in here, by id: an import
    /// leaves them out, as the history exchange does.
    var tombstoneIDs: Set<String> { account?.tombstones?.all ?? [] }

    func keepTombstone(_ id: SessionID) throws -> Bool {
        guard let tombstones = account?.tombstones else { return false }
        try tombstones.add(id)
        return true
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
