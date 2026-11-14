import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime

/// Each event the chat list's engine applies.
enum ChatAppStep: Codable, Equatable, Sendable {
    // The link and the device
    case linked(LinkSnapshot)
    case diskOpened(Disk)
    case permissionRead(NotificationPermission)
    case usageRead([AreaUsage])
    case transcriptsCleared
    case backgroundRun(BackgroundRun)
    case backgroundRefreshed(BackgroundRun, threads: Threads?, cards: Cards?)
    /// The system took the run back, or its budget ran out.
    case backgroundExpired(String)
    case continued(ChildKey, ContinuedProcessing.Update)
    case notificationOpened(NotificationResponse)
    case phase(AppPhase)

    // Reads
    case refreshAsked
    case refreshed(Refresh)
    case refreshFailed(Failure)
    case threadsRead(Threads)
    case agentsRead(agents: [Record], providers: [Record]?, models: [Record]?, keys: ProviderKeys?)
    case cardsRead(Cards)
    case readFailed(Failure)
    case openingsRead([String: String])
    case rowsRead(thread: String, epoch: Int, version: Int, rows: [Record], full: Bool)
    case rowsFailed(thread: String, epoch: Int, Failure)
    case cachedLoaded(thread: String, epoch: Int, CachedTranscript?)
    case prefetched(thread: String, epoch: Int, version: Int, rows: [Record])
    case threadFetched(Record)

    // The watch
    case watchOpened(fresh: Bool)
    case watchRow(WatchRow)
    /// The watch's position was refused as `compacted`, at this instant on
    /// the app's clock: the list is read again, once a minute
    /// (`WatchFollower.recovers`).
    case watchRelist(at: Date)
    case watchEnded(String?, Failure?)
    case feedDue
    case feedApplied(Feed)

    // Scenes
    case sceneAppeared(scene: String, restoring: String?, key: ChildKey)
    case sceneActive(scene: String, active: Bool)
    case sceneGone(scene: String)
    case show(thread: String, scene: String, key: ChildKey)
    case newChat(agent: String?, listen: Bool, scene: String, key: ChildKey)
    case back(scene: String)
    case searched(scene: String, query: String)
    case routed(ChatRoute)
    case routeClaimed(scene: String, key: ChildKey)

    // What the owner does
    case markedRead(String)
    case markedUnread(String)
    case renameAsked(thread: String, subject: String)
    case renamed(Record)
    case renameFailed(thread: String, before: Record?, Failure)
    case keyAsked(provider: String, key: String)
    case keySet(Record)
    case keyFailed(Failure)
    case cardAsked(CardReference)
    case cardRead(String, Record?, target: Record?)
    case cardFailed(String, Failure)
    case answerAsked(String, picks: [String: [String]])
    case questionsDismissed(String)
    case decideAsked(String, accept: Bool, shown: [String: JSONValue])
    case approveAsked(String, approve: Bool)
    case cardWritten(String, Record)
    case cardWriteFailed(String, Failure, conflict: String)
    case setting(row: String, toggle: Bool?, choice: String?)
    case settingsOpened
    case clearAsked
    case forget(Bool)
    case guidance(GuidanceStep)

    // A conversation's report
    case conversation(ChildKey, Report)

    /// The owner's memories and instructions: read, and written one record
    /// at a time.
    enum GuidanceStep: Codable, Equatable, Sendable {
        case asked
        case read(memories: [Record], instructions: [Record])
        case readFailed(Failure)
        case changeAsked(RecordReference, GuidanceChange)
        case written(Record)
        case deleted(RecordReference)
        case writeFailed(RecordReference, Failure, before: Record)
    }

    /// What the device held for a repository when it was opened.
    struct Disk: Codable, Equatable, Sendable {
        var lease: ChatStorage.Lease
        var state: CachedState?
        var drafts: [String: String]
        var preferences: ChatPreferences
        /// What the watch marked to read again that was not read before the
        /// app last stopped.
        var stale: [WatchKey]
    }

    typealias WatchKey = ChatAppState.WatchKey

    struct Threads: Codable, Equatable, Sendable {
        var chats: [Record]
        var automated: [Record]
    }

    struct Cards: Codable, Equatable, Sendable {
        var interactions: [Record]
        var requests: [Record]
        /// Nil where the server refused the read: a server older than the
        /// kind, or a token that does not reach it.
        var approvals: [Record]?
    }

    struct Refresh: Codable, Equatable, Sendable {
        var agents: [Record]
        var providers: [Record]?
        /// The `llm/model` records; nil where the session may not read them.
        var models: [Record]?
        /// Whose key each provider row runs on; nil where that read failed.
        var keys: ProviderKeys?
        var threads: Threads
        var cards: Cards
        /// Where the watch resumes so nothing written between the reads is
        /// missed: the earliest head of one history.
        var head: WatchCursor?
    }

    /// What a batch of the watch's rows read again, and what failed.
    struct Feed: Codable, Equatable, Sendable {
        var threads: Threads?
        var agents: [Record]?
        var providers: [Record]?
        var models: [Record]?
        var keys: ProviderKeys?
        var cards: Cards?
        var failed: Set<WatchKey>
        var failure: Failure?
    }

    /// What a conversation's applied event tells the list.
    struct Report: Codable, Equatable, Sendable {
        var thread: String?
        var agent: String?
        var owns: Bool
        var active: Bool
        var streaming: Bool
        var outcomes: Int
        var name: String
        var settles: Int
        /// The `thread` event named its thread.
        var named: String?
        /// The rows the conversation read for itself.
        var read: Read?
        var draft: String?
        var sent: Bool
        var failure: Failure?
    }

    struct Read: Codable, Equatable, Sendable {
        var thread: String
        var record: Record?
        var rows: [Record]
        /// A delivery check found the message on a thread that is not
        /// running: its rows are read whole.
        var reload: Bool
    }
}
