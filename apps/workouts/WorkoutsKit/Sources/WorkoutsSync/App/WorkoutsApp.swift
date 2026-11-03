import Foundation
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import WorkoutsCore

/// Substrate Workouts' root under the kit's link (`LinkController`): the
/// catalogue, the finished sessions and the settings, the signed-in
/// repository's sync (the package checked, the routines read, the sample's
/// first run, the history exchange, the library seeded) and the routine
/// edits, with the session as its one child. The link owns the connection:
/// the session and its revocations, the status check, connectivity and the
/// outbox's flush, and its snapshot arrives here as `.linked`.
///
/// A sync run belongs to the account it started for: every step names that
/// account, a step for one no longer signed in changes nothing, and a
/// sign-out or a sign-in elsewhere stops the run under its key.
public struct WorkoutsApp: LinkedApp {
    public typealias Child = SessionController
    public typealias Presentation = WorkoutsPresentation

    /// `Application Support/Workouts/`; nil in a preview, which reads and
    /// writes nothing.
    public var directory: URL?
    /// The session record's adapter, `substrate-workouts@<version>`.
    public var adapter: String
    public var settings: WorkoutsSettings
    /// What a preview shows, read nowhere else.
    public var preview: Preview?

    public struct Preview: Equatable, Sendable {
        public var catalog: Catalog
        public var history: [HistoryEntry]
        public var session: SessionState?

        public init(catalog: Catalog, history: [HistoryEntry], session: SessionState? = nil) {
            self.catalog = catalog
            self.history = history
            self.session = session
        }
    }

    public init(directory: URL?, adapter: String, settings: WorkoutsSettings, preview: Preview? = nil) {
        self.directory = directory
        self.adapter = adapter
        self.settings = settings
        self.preview = preview
    }

    public static let sessionKey = ChildKey("session")
    /// The path a session command takes from the link.
    public static var sessionPath: [ChildKey] { LinkController<WorkoutsApp>.appPath + [sessionKey] }

    var codec: RecordCodec { RecordCodec() }

    public var initialState: State {
        var state = State(settings: settings)
        if let preview {
            state.catalog = preview.catalog
            state.history = preview.history
            state.index = HistoryIndex(preview.history.map(\.records))
            state.historyLoaded = true
        }
        return state
    }

    // MARK: Composition

    public func children(in state: State) -> [ChildKey: SessionController] {
        [Self.sessionKey: SessionController(catalog: state.catalog ?? Catalog(), history: state.index, settings: state.settings,
                                            adapter: adapter, directory: directory, preview: preview?.session)]
    }

    public func event(forLink link: LinkSnapshot) -> Event? { .linked(link) }

    public func event(fromChild key: ChildKey, _ event: SessionController.Event) -> Event? {
        switch event {
        case .finished, .discard, .engine: .session(event)
        default: nil
        }
    }

    public func report(of event: Event) -> LinkReport? {
        if case let .refused(report) = event { return report }
        return nil
    }

    public func event(atOpen replayed: Bool) -> Event? { preview == nil ? .opened : nil }

    // MARK: Commands

    public func events(for command: Command, in context: Enrichment<WorkoutsApp>) -> [Event] {
        switch command {
        case let .settings(settings):
            return context.state.settings == settings ? [] : [.settings(settings)]
        case let .historyEntryChanged(entry):
            return [.entryChanged(entry)]
        default:
            // The rest is work for the effects: the command is its own event.
            return [.queued(command)]
        }
    }

    // MARK: The engine

    public func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        func current(_ account: AccountRef) -> Bool { state.signedIn == account }
        switch event {
        case .opened:
            break
        case let .linked(link):
            next.link = link
            let held = link.packages[WorkoutsKinds.packageID]
            next.package = next.signedIn == nil ? nil : held.map { PackageState.of(held: $0, needed: WorkoutsManifest.trainingVersion) }
            next.accountMoved = next.signedIn != state.account
            next.becameWritable = next.canWrite && !state.canWrite
            if next.signedIn != state.account {
                // Another account, or none: its facts start afresh.
                next.account = next.signedIn
                next.problem = nil
                next.syncing = false
                next.sampleState = nil
                next.sampleProblem = nil
                next.waitingRoutines = []
                next.conflicts = []
                next.draft = nil
                next.addingSample = false
                if next.signedIn == nil, let bundled = state.bundled {
                    next.catalog = bundled
                    next.origin = .bundled
                }
            }
        case let .settings(settings):
            next.settings = settings
        case let .bundledLoaded(catalog, failure):
            next.bundled = catalog
            next.catalogFailure = failure
            if state.origin == .bundled { next.catalog = catalog }
        case let .historyLoaded(entries, unreadable, hidden):
            next.history = entries
            next.index = HistoryIndex(entries.map(\.records))
            next.historyUnreadable = unreadable
            next.hidden.formUnion(hidden)
            next.historyLoaded = true
        case let .accountOpened(account, cached, draft, sample, unreadable, hidden):
            guard current(account) else { return state }
            if let cached { show(cached, in: &next) }
            next.draft = draft
            next.sampleState = sample
            next.hidden.formUnion(hidden)
            for record in unreadable where !next.syncUnreadable.contains(record) { next.syncUnreadable.append(record) }
        case let .syncStarted(account):
            guard current(account) else { return state }
            next.syncing = true
        case let .catalogRead(account, catalog, sample, sampleProblem):
            guard current(account) else { return state }
            show(catalog, in: &next)
            next.sampleState = sample
            next.sampleProblem = sampleProblem
            next.problem = nil
            next.awaitingHistory = !state.historyLoaded
        case let .exchanged(account, imports, merges, removals, unreadable):
            guard current(account) else { return state }
            next.awaitingHistory = false
            var history = state.history
            let removed = Set(removals)
            history.removeAll { removed.contains($0.id) }
            for entry in merges {
                if let index = history.firstIndex(where: { $0.id == entry.id }) { history[index] = entry }
            }
            let held = Set(history.map(\.id))
            history += imports.filter { !held.contains($0.id) }
            set(history, in: &next)
            for record in unreadable where !next.syncUnreadable.contains(record) { next.syncUnreadable.append(record) }
        case let .catalogShown(account, catalog):
            guard current(account) else { return state }
            show(catalog, in: &next)
        case let .syncEnded(account, problem):
            guard current(account) else { return state }
            next.syncing = false
            if let problem { next.problem = problem }
        case .refused:
            next.syncing = false
        case let .outboxRead(account, waiting, conflicts):
            guard current(account) else { return state }
            next.waitingRoutines = waiting
            let known = Set(state.conflicts.map(\.id))
            next.newConflicts = conflicts.filter { !known.contains($0.id) }.map { RoutineID($0.recordID) }
            next.conflicts = conflicts
        case let .draft(draft):
            next.draft = draft
        case let .sample(account, sample, problem):
            guard current(account) else { return state }
            next.sampleState = sample
            next.sampleProblem = problem
            next.addingSample = false
        case let .addingSample(adding):
            next.addingSample = adding
        case let .sessionDeleted(id):
            set(state.history.filter { $0.id != id }, in: &next)
            next.hidden.insert(id.rawValue)
        case let .sessionsImported(entries):
            let added = Set(entries.map(\.id))
            set(state.history.filter { !added.contains($0.id) } + entries, in: &next)
        case let .entryChanged(entry):
            guard let index = state.history.firstIndex(where: { $0.id == entry.id }) else { return state }
            var history = state.history
            history[index] = entry
            set(history, in: &next)
        case let .exercisesAdded(exercises):
            var catalog = state.catalog ?? Catalog()
            var bundled = state.bundled
            for exercise in exercises {
                var local = exercise
                local.isLocal = true
                catalog.exercises[local.id] = local
                if state.origin == .bundled { bundled?.exercises[local.id] = local }
            }
            next.catalog = catalog
            next.bundled = bundled
        case let .unreadable(record):
            if !next.syncUnreadable.contains(record) { next.syncUnreadable.append(record) }
        case let .outcome(receipt, problem):
            next.outcomes[receipt] = .some(problem)
        case let .session(event):
            if case let .finished(finished) = event {
                set(state.history.filter { $0.id != finished.id } + [finished.entry], in: &next)
                next.finished += 1
            }
        case let .queued(command):
            switch command {
            case .sync: if state.signedIn != nil { next.syncing = true }
            case .addSampleRoutine: if state.canWrite { next.addingSample = true }
            case let .saveRoutine(editor, _): if state.draft?.routine.id == editor.routine.id { next.draft = nil }
            case .dropDraft: next.draft = nil
            case let .keepDraft(editor): next.draft = editor
            default: break
            }
        }
        return next
    }

    /// The catalogue of the signed-in repository, as its cache holds it.
    private func show(_ catalog: Catalog, in state: inout State) {
        state.catalog = catalog
        state.catalogFailure = nil
        state.origin = state.host.map(CatalogOrigin.server) ?? .bundled
    }

    /// The history newest first, and its index.
    private func set(_ history: [HistoryEntry], in state: inout State) {
        let sorted = history.sorted { $0.records.session.at > $1.records.session.at }
        state.history = sorted
        state.index = HistoryIndex(sorted.map(\.records))
    }

    // MARK: The presenter

    public func present(_ input: PresenterInput<WorkoutsApp>) -> WorkoutsPresentation {
        let state = input.state
        let session = input.children[Self.sessionKey]
        let bundle = state.catalogFailure.map { [UnreadableRecord(source: "The routines and exercises in this build", reason: $0)] } ?? []
        let unreadable = bundle + (state.catalog?.unreadable ?? []) + state.historyUnreadable + (session?.setAside ?? [])
            + state.syncUnreadable
        let blocked: String? = if state.signedIn == nil {
            "Sign in to your substrate first."
        } else if state.canWrite {
            nil
        } else if let problem = state.problem {
            "The training package could not be checked: \(problem)"
        } else if case let .behind(held)? = state.package {
            "Your substrate has version \(held) of the Training package and this app needs version \(WorkoutsManifest.trainingVersion) or later. The update waits for your approval on your substrate; sessions wait on this iPhone until then."
        } else {
            "Checking the training package on your substrate."
        }
        return WorkoutsPresentation(
            catalog: state.catalog, origin: state.origin, catalogFailure: state.catalogFailure, history: state.history, index: state.index,
            historyLoaded: state.historyLoaded, unreadable: unreadable, settings: state.settings, host: state.host,
            signedIn: state.signedIn != nil, canWrite: state.canWrite, writeBlockedReason: blocked,
            canDeleteRoutines: state.signedIn != nil && (state.package.map(\.writable) ?? true), syncing: state.syncing,
            problem: state.problem, sampleState: state.sampleState, sampleProblem: state.sampleProblem, addingSample: state.addingSample,
            waitingRoutines: state.waitingRoutines, conflicts: state.conflicts, draft: state.draft, finished: state.finished,
            outcomes: state.outcomes, hiddenSessions: state.hidden, session: session)
    }
}
