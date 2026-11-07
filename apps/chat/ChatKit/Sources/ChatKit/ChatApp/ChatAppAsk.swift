import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime

/// What an event the chat list applied asks the runtime to do.
enum ChatAppAsk: Codable, Equatable, Sendable {
    typealias Lease = ChatStorage.Lease

    /// Answered with the session's lease on the repository's folder: every
    /// ask that reaches the folder carries it, and reaches nothing once a
    /// sign-out retired it.
    case openDisk(String)
    /// The repository's folder is done with: the link generation moves, and
    /// with `deleting` the rest of the folder goes, and so do the choices
    /// kept for it.
    case closeDisk(String, deleting: Bool)
    case prune(Lease)
    case readUsage(Lease)
    case clearTranscripts(Lease)
    case saveState(Lease, CachedState)
    case saveDrafts(Lease, [String: String])
    case savePreferences(Lease, ChatPreferences)
    case saveTranscript(Lease, thread: String, CachedTranscript)
    /// Rules 3 and 4 hold its answer as they hold a read's: it carries the
    /// thread's epoch.
    case loadCached(Lease, thread: String, epoch: Int)

    case refresh(Plan)
    case cards(Plan, threads: [String])
    case openings([String])
    case read(thread: String, first: Int, epoch: Int, version: Int)
    case prefetch(Lease, [Prefetch])
    case fetchThread(String)
    case watch(Lease, head: WatchCursor?)
    case feedWait(seconds: Double)
    case applyFeed(Plan, Set<ChatAppState.WatchKey>, threads: [String])
    case markRefreshed(Lease, [ChatAppState.WatchKey])

    case rename(thread: String, subject: String, before: Record?)
    case setKey(provider: String, key: String)
    case readCard(CardReference, target: Bool)
    case writeCard(String, kind: String, properties: [String: JSONValue], ifVersion: Int)
    case decideUnread(String, accept: Bool, shown: [String: JSONValue])

    case readGuidance
    /// Each carries the record as it stood before, which a refusal puts back.
    case writeGuidance(RecordReference, properties: [String: JSONValue], before: Record)
    case deleteGuidance(RecordReference, before: Record)

    case notify(NotificationRequest)
    case cancelGroup(String)
    case cancelAllNotifications
    case badge(Int)
    case askPermission
    case readPermission
    case followRuns
    /// What the owner does with the app's notifications.
    case followResponses
    /// A run of the refresh job the lease scheduled; its result lands only
    /// while the lease holds its generation, and within the run's budget.
    case backgroundRefresh(BackgroundRun, Lease, Plan, threads: [String])
    case finishRun(BackgroundRun, success: Bool)
    /// The refresh job, naming the lease's repository and generation.
    case scheduleRefresh(Lease)
    case continueRun(ChildKey, ContinuedProcessing)
    case endContinued(ChildKey)

    /// Ends what runs under a key: its late result is dropped.
    case cancel(String)

    /// What a read may reach, by the session's grants.
    struct Plan: Codable, Equatable, Sendable {
        var showAutomated: Bool
        var canChat: Bool
        var canDecide: Bool
        var canReadProviders: Bool
        /// Only a full-access token reads which keys the operator supplies.
        var readsOperator: Bool
        /// An app's token lists the cards only under the threads it names.
        var namesThreads: Bool
    }

    /// A chat to read ahead, from the disk first.
    struct Prefetch: Codable, Equatable, Sendable {
        var thread: String
        var version: Int
        var epoch: Int
        /// The rows are in memory already, so the disk is not asked.
        var held: Bool
    }
}
