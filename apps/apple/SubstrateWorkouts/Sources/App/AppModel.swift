import Foundation
import Observation
import os
import WorkoutsCore

/// Where the catalogue comes from.
enum CatalogOrigin: Hashable {
    case bundled
    case server(String)
}

/// The four tabs (DESIGN.md §7.1).
enum AppTab: Hashable {
    case routines, history, exercises, settings
}

/// What the app holds outside the session: the catalogue, the finished
/// sessions, the settings, and where the owner is (the tab, the session
/// cover). One instance, shared with `SessionController`, which reads the
/// catalogue, the history index and the settings for every presentation.
@MainActor
@Observable
final class AppModel {
    /// Created on first use: at launch, or when a Live Activity intent
    /// launches the app in the background.
    static let shared = AppModel(directory: AppModel.workoutsDirectory())

    var tab: AppTab = .routines {
        didSet { if tab == .history { historyBadge = 0 } }
    }

    /// The session cover is on screen.
    var isSessionPresented = false
    /// Sessions finished since History was last opened (§7.7).
    var historyBadge = 0

    var settings: WorkoutsSettings {
        didSet {
            guard settings != oldValue else { return }
            if persistsSettings { settings.save() }
            if settings.presenter != oldValue.presenter { refreshHistorySections() }
        }
    }

    /// Nil until the catalogue decodes (normally at once).
    private(set) var catalog: Catalog?
    /// Where the routines and exercises come from: this build, or the
    /// substrate the app is signed in to (its host).
    private(set) var catalogOrigin: CatalogOrigin = .bundled
    /// Set when a bundled file is missing, a build fault.
    private(set) var catalogFailure: String?
    /// Finished sessions, newest first.
    private(set) var history: [HistoryEntry] = []
    private(set) var historyIndex = HistoryIndex()
    private(set) var historyUnreadable: [UnreadableRecord] = []
    /// A session journal that did not load at launch, set aside (§2.3).
    private(set) var sessionUnreadable: [UnreadableRecord] = []
    private(set) var isHistoryLoaded = false
    /// History's month sections, rebuilt when the history, the catalogue or a
    /// totals setting changes rather than on every render: each row sums a
    /// session's volume.
    private(set) var historySections: [HistorySection] = []

    /// `Application Support/Workouts/`; nil in previews, which write nothing.
    let directory: URL?
    private let catalogSource: BundledCatalog
    private let historyStore: HistoryStore?
    private let persistsSettings: Bool
    private var historyTask: Task<Void, Never>?
    private let logger = Logger(subsystem: "dev.reamde.substrate.workouts", category: "app")

    init(directory: URL?, defaults: UserDefaults = .standard) {
        self.directory = directory
        settings = WorkoutsSettings.load(from: defaults)
        persistsSettings = true
        catalogSource = BundledCatalog(localExercises: directory.map { LocalExercises(directory: $0) })
        historyStore = directory.map { HistoryStore(directory: $0) }
    }

    /// A model over fixed data, for previews: nothing is read or written.
    init(catalog: Catalog, history: [HistoryEntry], settings: WorkoutsSettings = WorkoutsSettings()) {
        directory = nil
        self.settings = settings
        persistsSettings = false
        catalogSource = BundledCatalog()
        historyStore = nil
        self.catalog = catalog
        self.history = history
        historyIndex = HistoryIndex(history.map(\.records))
        isHistoryLoaded = true
        refreshHistorySections()
    }

    private static func workoutsDirectory() -> URL? {
        do {
            return try WorkoutsFiles.applicationSupportDirectory()
        } catch {
            // A full disk at launch: keep the path. Every write creates the
            // directory again, so a session finished once space is free is
            // saved rather than kept only in memory.
            Logger(subsystem: "dev.reamde.substrate.workouts", category: "app")
                .fault("Application Support directory not created: \(error.localizedDescription, privacy: .public)")
            return WorkoutsFiles.applicationSupportPath()
        }
    }

    var presenterSettings: PresenterSettings { settings.presenter }

    /// The session in progress: the same, with the RPE pre-fill when the
    /// cover shows Rows.
    var sessionPresenterSettings: PresenterSettings { settings.sessionPresenter }

    /// Every exercise the catalogue knows.
    var exercises: [ExerciseID: Exercise] { catalog?.exercises ?? [:] }

    /// Records that could not be read: the bundled catalogue as a whole, its
    /// records, the history files and a session journal set aside.
    var unreadable: [UnreadableRecord] {
        let bundle = catalogFailure.map { [UnreadableRecord(source: "The routines and exercises in this build", reason: $0)] } ?? []
        return bundle + (catalog?.unreadable ?? []) + historyUnreadable + sessionUnreadable
    }

    /// The journal of the session in progress did not load and was set aside.
    func noteUnreadableSession(_ record: UnreadableRecord) {
        sessionUnreadable.append(record)
    }

    // MARK: - Loading

    /// Reads the bundled catalogue and the phone's own exercises. Synchronous:
    /// a few dozen records.
    func loadCatalog() {
        guard catalog == nil else { return }
        do {
            catalog = try catalogSource.loadNow()
        } catch {
            catalogFailure = String(describing: error)
            catalog = Catalog()
            logger.fault("Bundled catalogue failed: \(String(describing: error), privacy: .public)")
        }
        refreshHistorySections()
    }

    /// Reads every history file off the main actor, once; later calls wait
    /// for the first.
    func loadHistory() async {
        if let historyTask { return await historyTask.value }
        guard let historyStore else { return }
        let task = Task {
            let load = await Task.detached(priority: .userInitiated) { historyStore.loadAll() }.value
            apply(load)
        }
        historyTask = task
        await task.value
    }

    /// Re-reads every history file: after a Health save changed one.
    func reloadHistory() async {
        guard let historyStore else { return }
        let load = await Task.detached(priority: .utility) { historyStore.loadAll() }.value
        apply(load)
    }

    private func apply(_ load: HistoryLoad) {
        history = load.entries
        historyIndex = HistoryIndex(load.entries.map(\.records))
        historyUnreadable = load.unreadable
        isHistoryLoaded = true
        refreshHistorySections()
        for record in load.unreadable {
            logger.error("Unreadable history file \(record.source, privacy: .public): \(record.reason, privacy: .public)")
        }
    }

    // MARK: - Changes

    /// Writes a finished session's file and adds it to the history. Only a
    /// preview keeps a session in memory alone.
    func save(_ entry: HistoryEntry) throws {
        guard historyStore != nil || !persistsSettings else { throw CocoaError(.fileNoSuchFile) }
        try historyStore?.write(entry)
        history.removeAll { $0.id == entry.id }
        history.insert(entry, at: history.firstIndex { $0.records.session.at < entry.records.session.at } ?? history.endIndex)
        historyIndex.insert(entry.records)
        refreshHistorySections()
        if tab != .history { historyBadge += 1 }
    }

    /// A history file the Health save rewrote (its status, its workout link).
    func replace(_ entry: HistoryEntry) {
        guard let index = history.firstIndex(where: { $0.id == entry.id }) else { return }
        history[index] = entry
        historyIndex.remove(entry.id)
        historyIndex.insert(entry.records)
        refreshHistorySections()
    }

    /// Deletes a finished session's file, and the server's copy through the
    /// outbox. Its Apple Health workout stays.
    func deleteSession(_ id: SessionID) throws {
        try historyStore?.delete(id)
        history.removeAll { $0.id == id }
        historyIndex.remove(id)
        refreshHistorySections()
        if persistsSettings { Task { await SubstrateLink.shared.deleted(id) } }
    }

    // MARK: - The substrate

    /// The routines and exercises of the substrate the app is signed in to,
    /// cached or just read, with the phone's own exercises merged in.
    func showServerCatalog(_ server: Catalog, host: String) {
        catalog = server
        catalogFailure = nil
        catalogOrigin = .server(host)
        refreshHistorySections()
    }

    /// Back to this build's routines: a sign-out.
    func showBundledCatalog() {
        catalog = nil
        catalogOrigin = .bundled
        loadCatalog()
    }

    /// Finished sessions the server holds that this phone does not: another
    /// writer's, or this phone's from before a reinstall. They join the
    /// history, which PREVIOUS and the suggestions read, and are never saved
    /// to Apple Health again.
    func importServerSessions(_ sessions: [SessionRecords]) {
        guard let historyStore else { return }
        let held = Set(history.map(\.id))
        var added = false
        for records in sessions where !held.contains(records.id) {
            let health = HealthSaveState(status: records.session.healthKitWorkout == nil ? .notAvailable : .saved)
            let entry = HistoryEntry(records: records, local: HistoryLocal(health: health))
            do {
                try historyStore.write(entry)
                history.append(entry)
                historyIndex.insert(records)
                added = true
            } catch {
                logger.error("A session from the server was not kept: \(error.localizedDescription, privacy: .public)")
            }
        }
        guard added else { return }
        history.sort { $0.records.session.at > $1.records.session.at }
        refreshHistorySections()
    }

    private func refreshHistorySections() {
        historySections = HistoryPresenter.sections(history, exercises: exercises, settings: presenterSettings)
    }

    /// Saves an exercise added on this phone (§7.6) and returns it as the
    /// catalogue now holds it.
    @discardableResult
    func addLocalExercise(_ exercise: Exercise) throws -> Exercise {
        var local = exercise
        local.isLocal = true
        if directory != nil, catalogOrigin == .bundled {
            catalog = try catalogSource.addLocalExercise(local)
        } else if let directory {
            try LocalExercises(directory: directory).add(local)
            catalog?.exercises[local.id] = local
        } else {
            catalog?.exercises[local.id] = local
        }
        refreshHistorySections()
        return catalog?.exercises[local.id] ?? local
    }
}
