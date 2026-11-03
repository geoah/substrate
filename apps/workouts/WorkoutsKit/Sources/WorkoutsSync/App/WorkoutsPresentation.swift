import Foundation
import SubstrateLocal
import WorkoutsCore

/// What the app's screens draw outside the session, and the session's own
/// presentation under it.
public struct WorkoutsPresentation: Codable, Equatable, Sendable {
    public var catalog: Catalog?
    public var origin: CatalogOrigin
    public var catalogFailure: String?
    public var history: [HistoryEntry]
    public var index: HistoryIndex
    public var historyLoaded: Bool
    /// Records that could not be read: the bundled catalogue as a whole, its
    /// records, the history files, the sync's files and a journal set aside.
    public var unreadable: [UnreadableRecord]
    public var settings: WorkoutsSettings
    public var host: String?
    public var signedIn: Bool
    public var canWrite: Bool
    /// Why the app may not write now, in a sentence; nil when it may.
    public var writeBlockedReason: String?
    /// Whether a routine may be deleted: signed in, and a package the app
    /// may write or not checked yet.
    public var canDeleteRoutines: Bool
    public var syncing: Bool
    public var problem: String?
    public var sampleState: SampleInstall.State?
    public var sampleProblem: String?
    public var addingSample: Bool
    public var waitingRoutines: Set<RoutineID>
    public var conflicts: [OutboxEntry]
    public var draft: RoutineEditor?
    public var finished: Int
    public var outcomes: [UUID: String?]
    /// Sessions deleted on the phone while signed in here, by id: an import
    /// leaves them out, as the history exchange does.
    public var hiddenSessions: Set<String>
    public var session: SessionScreenPresentation?

    /// The edit of a routine the server refused because the routine changed
    /// there since the edit began, waiting for the owner's choice.
    public func conflict(for routine: RoutineID) -> OutboxEntry? {
        conflicts.first { $0.recordID == routine.rawValue }
    }

    public var exercises: [ExerciseID: Exercise] { catalog?.exercises ?? [:] }

    public static let conflictFirst
        = "This routine has an edit waiting for your choice after a conflict. Choose Keep My Edit or Use Theirs on its page first."
}
