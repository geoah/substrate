import Foundation
import SubstrateRuntime
import WorkoutsCore

public extension WorkoutsApp {
    enum Effect: Equatable, Sendable {
        case loadBundled
        case loadHistory
        case openAccount(AccountRef)
        /// Follows the account's outbox for its routine writes.
        case followOutbox(AccountRef)
        /// The package checked, the routines read and the sample's first run.
        case sync(AccountRef)
        /// The history exchange, from the history as it stands.
        case exchange(AccountRef, local: [HistoryEntry], hidden: Set<String>, exercises: [Exercise])
        /// The library seeded where the repository is behind this build's.
        case seedLibrary(AccountRef)
        case stopSync
        /// A change the session made, queued for the account's repository.
        case handOff(AccountRef, HandOff)
        case addSample(AccountRef)
        case deleteRoutine(AccountRef, RoutineID)
        case saveRoutine(AccountRef, RoutineEditor, receipt: UUID)
        case keepDraft(AccountRef, RoutineEditor)
        case dropDraft(AccountRef)
        case resolve(AccountRef, UUID, keepMine: Bool)
        /// Routines read again from the server: a conflicted one shows as
        /// the server holds it.
        case reread(AccountRef, [RoutineID])
        case deleteSession(AccountRef?, SessionID, receipt: UUID)
        case importSessions([HistoryEntry], exercises: [Exercise], origin: CatalogOrigin, receipt: UUID)
        case importRecords(AccountRef, routines: [Routine], measurements: [RecordWrite], exercises: [Exercise], receipt: UUID)
        case addLocalExercises([Exercise], origin: CatalogOrigin, receipt: UUID?)
        /// A change refused before any work: its receipt answered at once.
        case answer(UUID, String)
    }

    /// What the session hands the outbox.
    enum HandOff: Equatable, Sendable {
        case finished(SessionRecords)
        case discarded(SessionID)
        case exerciseAdded(Exercise)
        case healthLinked(SessionID, workout: String, revision: Int)
    }
}
