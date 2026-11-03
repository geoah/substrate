import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime

/// What an event the chat list applied asks the runtime to do.
enum ChatAppAsk: Codable, Equatable, Sendable {
    case openDisk(String)
    /// The repository's folder is done with; with `deleting` it goes, and so
    /// do the choices kept for it.
    case closeDisk(String, deleting: Bool)
    case prune(String)
    case readUsage(String)
    case clearTranscripts(String)
    case saveState(String, CachedState)
    case saveDrafts(String, [String: String])
    case savePreferences(String, ChatPreferences)
    case saveTranscript(String, thread: String, CachedTranscript)
    case loadCached(String, thread: String)

    case refresh(Plan)
    case cards(Plan, threads: [String])
    case openings([String])
    case read(thread: String, first: Int, epoch: Int, version: Int)
    case prefetch(String, [Prefetch])
    case fetchThread(String)
    case watch(String, head: WatchCursor?)
    case feedWait(seconds: Double)
    case applyFeed(Plan, Set<ChatAppState.WatchKey>, threads: [String])
    case markRefreshed(String, [ChatAppState.WatchKey])

    case rename(thread: String, subject: String, before: Record?)
    case setKey(provider: String, key: String)
    case readCard(CardReference, target: Bool)
    case writeCard(String, kind: String, properties: [String: JSONValue], ifVersion: Int)
    case decideUnread(String, accept: Bool, shown: [String: JSONValue])

    /// Its instant is stamped when it is sent.
    case notify(NotificationRequest)
    case cancelGroup(String)
    case cancelAllNotifications
    case badge(Int)
    case askPermission
    case readPermission
    case followRuns
    case backgroundRefresh(BackgroundRun, Plan, threads: [String])
    case finishRun(BackgroundRun)
    case scheduleRefresh
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
