import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import WorkoutsCore

public extension WorkoutsApp {
    /// What the app holds outside the session: the catalogue, the finished
    /// sessions, the settings, and the signed-in repository's standing.
    struct State: Codable, Equatable, Sendable {
        public var link: LinkSnapshot?
        public var settings: WorkoutsSettings
        /// Nil until this build's catalogue is read (at once).
        public var catalog: Catalog?
        public var origin: CatalogOrigin = .bundled
        /// Set when a bundled file is missing, a build fault.
        public var catalogFailure: String?
        /// This build's routines and exercises, the phone's own merged in:
        /// what a sign-out shows again.
        public var bundled: Catalog?
        /// Finished sessions, newest first.
        public var history: [HistoryEntry] = []
        public var index = HistoryIndex()
        public var historyLoaded = false
        public var historyUnreadable: [UnreadableRecord] = []
        /// Sessions deleted on the phone: signed out (never imported again)
        /// and while signed in here (purged on this repository alone).
        public var hidden: Set<String> = []
        /// Files of the substrate sync that could not be read, set aside.
        public var syncUnreadable: [UnreadableRecord] = []
        /// The account the facts below are about.
        public var account: AccountRef?
        public var package: PackageState?
        public var syncing = false
        /// Why the last package check or catalogue read did not finish.
        public var problem: String?
        public var sampleState: SampleInstall.State?
        /// Why the sample could not be written at the last sync; tried again
        /// at the next. Shown where the sample would be, never in the way.
        public var sampleProblem: String?
        public var addingSample = false
        /// Routines whose edit, create or delete waits for the server.
        public var waitingRoutines: Set<RoutineID> = []
        /// Routine edits the server refused because the routine changed there.
        public var conflicts: [OutboxEntry] = []
        /// The routine edit not saved yet, kept on disk.
        public var draft: RoutineEditor?
        /// The last snapshot signed in to another account, or none.
        public var accountMoved = false
        /// The last snapshot made the training package writable.
        public var becameWritable = false
        /// The catalogue was read before the history loaded: the exchange
        /// waits for it.
        public var awaitingHistory = false
        /// Conflicts the last outbox read found that were not there before:
        /// their routines are read again.
        public var newConflicts: [RoutineID] = []
        /// Sessions finished here since launch: History's badge counts them.
        public var finished = 0
        /// How each change waited on by its receipt went: nil is done.
        public var outcomes: [UUID: String?] = [:]

        public init(settings: WorkoutsSettings) {
            self.settings = settings
        }

        public var host: String? { link.flatMap(\.server).map { ServerAddress.display($0) } }

        /// The signed-in repository, while signed in.
        public var signedIn: AccountRef? {
            guard let link, link.phase != .signedOut, let server = link.server, let repository = link.repository else { return nil }
            return AccountRef(server: server, repository: repository)
        }

        /// Whether the app may write to its substrate now: signed in, and
        /// the training package checked at this build's version.
        public var canWrite: Bool { signedIn != nil && package?.writable == true }
    }
}
