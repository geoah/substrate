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
    /// `atOnce` writes with no wait for more: the app is leaving the front,
    /// and may not be running by the end of one.
    case saveDrafts(Lease, [String: String], atOnce: Bool)
    /// The unsent messages and, after them, the drafts as they stand with
    /// them: written at once, and answered with the ids the folder holds.
    case saveUnsent(Lease, [String: UnsentMessage], drafts: [String: String])
    /// The unsent messages' write failed: after this long, again.
    case retryUnsent(after: Double)
    case savePreferences(Lease, ChatPreferences)
    case saveTranscript(Lease, thread: String, CachedTranscript)
    /// Rules 3 and 4 hold its answer as they hold a read's: it carries the
    /// thread's epoch.
    case loadCached(Lease, thread: String, epoch: Int)

    /// `held`: the threads beside the list whose cards are read with it.
    /// `beside`: the threads held that no list may answer, whose rows and
    /// marks are read with it.
    case refresh(Plan, held: [String], beside: [String] = [])
    /// The server's discovery document, for the revision of what the chat
    /// may call.
    case readFeatures
    case cards(Plan, threads: [String])
    case openings([String])
    case read(thread: String, first: Int, epoch: Int, version: Int)
    case prefetch(Lease, [Prefetch])
    case fetchThread(String)
    /// The row of a thread whose mark is ahead of it, read again: `mark` is
    /// the version of the mark it is read for.
    case rereadBehind(thread: String, mark: Int)
    case watch(Lease, head: WatchCursor?, kinds: [String])
    case feedWait(seconds: Double)
    /// `named`: the threads held that the batch names, by a row of their
    /// own or of a mark: each is read on its own where no list read for
    /// the batch holds it.
    case applyFeed(Plan, Set<ChatAppState.WatchKey>, threads: [String], named: [String] = [])
    /// The page of a list after `after`; with none, the archived chats'
    /// first.
    case page(Plan, part: ChatLists.Part, after: String?)
    /// The rows of a thread before the row whose ordinal is `before`.
    case readOlder(thread: String, before: Int, epoch: Int)
    /// The messages that hold a window's search, asked for once the typing
    /// has paused: `threads` are the ones an app's token names.
    case search(Plan, scene: String, query: String, threads: [String])
    /// Wakes the engine when Undo's few seconds have passed.
    case undoWait(thread: String, until: Date)
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

    /// The person's marks of these threads.
    case readMarks([String])
    /// Wakes the engine once the end of a chat has been on screen a second.
    case sight(thread: String, message: String)
    /// Wakes the engine when the next mark's write is due.
    case marksWait(until: Date)
    case writeMark(thread: String, MarkChange)

    case notify(NotificationRequest)
    /// A notification whose actions are its own category's: the categories
    /// are registered first, in the same piece of work, since an action's
    /// title is read from its category when the notification is shown.
    case notifyCard(NotificationRequest, categories: [NotificationCategory])
    /// The categories the app's notifications name, as they stand now.
    case categories([NotificationCategory])
    /// Wakes the engine when an action from a notification has had its time.
    case actionWait(String, seconds: Double)
    /// An action from a notification is over: its own notification is
    /// taken back or what it could not do is said, and only then is the
    /// device told, which may suspend the app at that word.
    case finishAction(NotificationResponse, cancel: String?, notify: NotificationRequest?)
    /// A reply's notification, shown once `seconds` passed: a mark that
    /// arrives first takes it back, so a reply read on a device beside this
    /// one does not sound here.
    case notifyHeld(NotificationRequest, seconds: Double)
    case cancelGroup(String)
    /// Takes back the one notification of this id.
    case cancelNotification(String)
    case cancelAllNotifications
    case badge(Int)
    case askPermission
    case readPermission
    case followRuns
    /// What the owner does with the app's notifications.
    case followResponses
    /// A run of the refresh job the lease scheduled; its result lands only
    /// while the lease holds its generation, and within the run's budget.
    case backgroundRefresh(BackgroundRun, Lease, Plan, threads: [String], beside: [String])
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
        var canReadModels: Bool
        /// Only a full-access token reads which keys the operator supplies.
        var readsOperator: Bool
        /// An app's token lists the cards only under the threads it names.
        var namesThreads: Bool
        /// The server keeps marks: the person's are read with every list of
        /// threads, and each thread row counts what waits on a person, so
        /// the cards are read only under the threads that say something
        /// does.
        var readsMarks = false
        /// The server keeps `pinned` and `archived`: the list is the chats
        /// not archived, and the pinned ones and the ones that wait are
        /// read beside it.
        var flags = false
        /// A window lists the archived chats: their first page is read
        /// with the list.
        var archived = false
        /// How many chats one read of a list asks for.
        var page = ChatLists.page
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
