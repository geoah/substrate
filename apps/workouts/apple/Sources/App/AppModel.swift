import Foundation
import Observation
import WorkoutsCore
import WorkoutsSync
import os

/// The five tabs (DESIGN.md §7.1).
enum AppTab: Hashable {
    case routines, history, stats, exercises, settings
}

/// What the tabs draw outside the session, republished on the main actor
/// from Workouts' root on the runtime (`WorkoutsApp`): the catalogue, the
/// finished sessions and the settings. It also holds where the owner is (the
/// tab, the session cover), which is the views' own. Every change goes to
/// the runtime as a command; History's month sections are worked out here
/// when the history, the catalogue or a totals setting changes rather than
/// on every render.
@MainActor
@Observable
final class AppModel {
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
            if persistsSettings {
                settings.save()
                root?.send(.settings(settings))
            }
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
    /// Moves on every change to `history` or `catalog`: what a presentation
    /// worked out from both (the stats) is kept against.
    private(set) var dataRevision = 0
    private(set) var historyIndex = HistoryIndex()
    private(set) var isHistoryLoaded = false
    /// History's month sections.
    private(set) var historySections: [HistorySection] = []
    /// Records that could not be read.
    private(set) var unreadable: [UnreadableRecord] = []
    /// Sessions deleted on the phone, which an import leaves out.
    private(set) var hiddenSessions: Set<String> = []

    /// `Application Support/Workouts/`; nil in previews, which write nothing.
    let directory: URL?
    @ObservationIgnored weak var root: WorkoutsRoot?
    @ObservationIgnored private var finished = 0
    private let persistsSettings: Bool

    init(directory: URL?, settings: WorkoutsSettings) {
        self.directory = directory
        self.settings = settings
        persistsSettings = true
    }

    /// A model over fixed data, for previews: nothing is read or written.
    init(catalog: Catalog, history: [HistoryEntry], settings: WorkoutsSettings = WorkoutsSettings()) {
        directory = nil
        self.settings = settings
        persistsSettings = false
        self.catalog = catalog
        self.history = history
        historyIndex = HistoryIndex(history.map(\.records))
        isHistoryLoaded = true
        refreshHistorySections()
    }

    static func workoutsDirectory() -> URL? {
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

    func apply(_ next: WorkoutsPresentation) {
        var moved = false
        if next.catalog != catalog {
            catalog = next.catalog
            moved = true
        }
        if next.history != history {
            history = next.history
            historyIndex = next.index
            moved = true
        }
        if next.origin != catalogOrigin { catalogOrigin = next.origin }
        if next.catalogFailure != catalogFailure { catalogFailure = next.catalogFailure }
        if next.historyLoaded != isHistoryLoaded { isHistoryLoaded = next.historyLoaded }
        if next.unreadable != unreadable { unreadable = next.unreadable }
        if next.hiddenSessions != hiddenSessions { hiddenSessions = next.hiddenSessions }
        if next.finished > finished {
            if tab != .history { historyBadge += next.finished - finished }
            finished = next.finished
        }
        if moved {
            dataRevision &+= 1
            refreshHistorySections()
        }
    }

    var presenterSettings: PresenterSettings { settings.presenter }

    /// The session in progress: the same, with the RPE pre-fill when the
    /// cover shows Rows.
    var sessionPresenterSettings: PresenterSettings { settings.sessionPresenter }

    /// Every exercise the catalogue knows.
    var exercises: [ExerciseID: Exercise] { catalog?.exercises ?? [:] }

    private func refreshHistorySections() {
        historySections = HistoryPresenter.sections(history, exercises: exercises, settings: presenterSettings)
    }

    // MARK: Changes

    /// Deletes a finished session's file, and the server's copy through the
    /// outbox. Its Apple Health workout stays. A delete whose tombstone
    /// cannot be kept is refused.
    func deleteSession(_ id: SessionID) async throws {
        try await root?.outcome { .deleteSession(id, receipt: $0) }
    }

    /// A history file the Health save rewrote (its status, its workout link).
    func replace(_ entry: HistoryEntry) {
        root?.send(.historyEntryChanged(entry))
    }

    /// Saves an exercise added on this phone (§7.6) and returns it as the
    /// catalogue now holds it.
    @discardableResult
    func addLocalExercise(_ exercise: Exercise) async throws -> Exercise {
        try await addLocalExercises([exercise])
        return catalog?.exercises[exercise.id] ?? exercise
    }

    /// Saves exercises added on this phone in one write, all or none.
    func addLocalExercises(_ exercises: [Exercise]) async throws {
        guard let root else {
            for exercise in exercises { catalog?.exercises[exercise.id] = exercise }
            return
        }
        try await root.outcome { .addLocalExercises(exercises, receipt: $0) }
    }

    /// Sessions from another app's export, and the exercises they name that
    /// the catalogue lacks, saved first. They join the history like a
    /// session from the server, never saved to Apple Health, and the next
    /// sync sends them as sessions the server never held. A file that
    /// cannot be written stops the import there, and the error says how many.
    func importSessions(_ sessions: [SessionRecords], exercises: [Exercise]) async throws {
        try await root?.outcome { .importSessions(sessions, exercises: exercises, receipt: $0) }
    }
}
