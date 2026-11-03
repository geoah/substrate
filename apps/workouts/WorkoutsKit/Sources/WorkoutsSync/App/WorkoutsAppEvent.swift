import Foundation
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import WorkoutsCore

public extension WorkoutsApp {
    /// What the app's engine applies. A step of a signed-in repository's
    /// sync names the account it ran for, and one for an account no longer
    /// signed in changes nothing.
    enum Event: Codable, Equatable, Sendable {
        case opened
        case linked(LinkSnapshot)
        case settings(WorkoutsSettings)
        /// This build's routines and exercises, and the phone's own.
        case bundledLoaded(Catalog, failure: String?)
        case historyLoaded([HistoryEntry], unreadable: [UnreadableRecord], hidden: Set<String>)
        /// An account's folder read when it signed in: its cached
        /// catalogue, its draft, its sample's standing, what it could not read.
        case accountOpened(AccountRef, cached: Catalog?, draft: RoutineEditor?, sample: SampleInstall.State?,
                           unreadable: [UnreadableRecord], hidden: Set<String>)
        case syncStarted(AccountRef)
        case catalogRead(AccountRef, Catalog, sample: SampleInstall.State?, sampleProblem: String?)
        /// The history exchange's outcome on the phone: the history files
        /// imported, merged and removed, already written.
        case exchanged(AccountRef, imports: [HistoryEntry], merges: [HistoryEntry], removals: [SessionID], unreadable: [UnreadableRecord])
        /// The catalogue as the account's cache now holds it.
        case catalogShown(AccountRef, Catalog)
        case syncEnded(AccountRef, problem: String?)
        /// A request the server refused for the token.
        case refused(LinkReport)
        /// The outbox's routine writes as they wait: what the routine pages say.
        case outboxRead(AccountRef, waitingRoutines: Set<RoutineID>, conflicts: [OutboxEntry])
        case draft(RoutineEditor?)
        case sample(AccountRef, SampleInstall.State?, problem: String?)
        case addingSample(Bool)
        case sessionDeleted(SessionID)
        case sessionsImported([HistoryEntry])
        case entryChanged(HistoryEntry)
        case exercisesAdded([Exercise])
        case unreadable(UnreadableRecord)
        /// A change waited on by its receipt is done, or why it was not.
        case outcome(UUID, problem: String?)
        /// The session's own event, folded here for what it changes outside
        /// the session: a finished one joins History.
        case session(SessionController.Event)
        /// A command whose work is its effect's.
        case queued(Command)
    }
}
