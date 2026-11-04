import Foundation
import WorkoutsCore

public extension WorkoutsApp {
    /// What the app's screens ask of it outside the session. A change whose
    /// sheet waits for its outcome names a receipt, and the presentation
    /// says how it went under it (`WorkoutsPresentation.outcomes`).
    enum Command: Codable, Equatable, Sendable {
        /// The owner changed a setting; the app keeps it on the phone.
        case settings(WorkoutsSettings)
        /// At launch, on return to the foreground and after a sign-in: the
        /// package checked, the routines read, the sessions exchanged, the
        /// library seeded. One at a time.
        case sync
        /// Writes the sample routine back after the owner deleted it.
        case addSampleRoutine
        case deleteRoutine(RoutineID)
        /// Saves an edited or duplicated routine through the outbox.
        case saveRoutine(RoutineEditor, receipt: UUID)
        /// Keeps the edit in progress on disk.
        case keepDraft(RoutineEditor)
        case dropDraft
        /// The owner's answer to a conflicting routine edit.
        case resolve(UUID, keepMine: Bool)
        /// Deletes a finished session, and the server's copy through the outbox.
        case deleteSession(SessionID, receipt: UUID)
        /// Sessions from another app's export, and the exercises they name.
        case importSessions([SessionRecords], exercises: [Exercise], receipt: UUID)
        /// What an import writes beside the sessions: routines and body
        /// measurements, after every new exercise they name.
        case importRecords(routines: [Routine], measurements: [RecordWrite], exercises: [Exercise], receipt: UUID)
        /// Exercises added on this phone, saved in one write, all or none.
        case addLocalExercises([Exercise], receipt: UUID)
        /// A history file the Health save rewrote (its status, its workout link).
        case historyEntryChanged(HistoryEntry)
        /// The Health save's workout on the session, queued for the server.
        case healthLinked(SessionID, workout: String, revision: Int)
        /// An exercise the routine editor made, queued for the server.
        case exerciseAdded(Exercise)
    }
}
