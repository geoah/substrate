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
    /// The folder holds these unsent messages, by id, as of its last write.
    case unsentSaved(Set<String>, failed: String?)
    case unsentRetryDue
    case permissionRead(NotificationPermission)
    case usageRead([AreaUsage])
    case transcriptsCleared
    case backgroundRun(BackgroundRun)
    case backgroundRefreshed(BackgroundRun, threads: Threads?, cards: Cards?)
    /// The system took the run back, or its budget ran out.
    case backgroundExpired(String)
    case continued(ChildKey, ContinuedProcessing.Update)
    case notificationOpened(NotificationResponse)
    /// An action of a notification: `key` and `message` are ids minted for
    /// it, the conversation a reply is sent from where its thread has none
    /// yet, and the id the reply is sent under.
    case notificationActed(NotificationResponse, key: ChildKey, message: String, at: Date)
    /// The time an action from a notification may take has passed.
    case actionDue(String)
    /// A card's notification was held as long as it may be.
    case cardHoldDue(String)
    /// The person's own resolved cards of a thread, of the ones asked
    /// about; `whole` where the answer is all of them. Nil where no read
    /// was answered.
    case ownRead(thread: String, asked: [String], mine: [String]?, whole: Bool)
    case phase(AppPhase)
    case paced(Bool)
    /// A reader took a transcript from its end: said in the diagnostics
    /// file, and nothing else.
    case followEnded(key: ChildKey, TranscriptFollow.Release)
    /// Share Diagnostics was opened or closed.
    case diagnosticsAsked(Bool)
    /// The diagnostics file's lines, oldest first.
    case diagnosticsRead([String])

    // Reads
    case refreshAsked
    case refreshed(Refresh)
    case refreshFailed(Failure)
    /// The `agents` and the `records` features' revisions, as the
    /// discovery document says them.
    case featuresRead(agents: Int, records: Int)
    /// The person's marks of some threads, read on their own.
    case marksRead(Marks)
    /// The mark of a thread the person spoke in, read after they did.
    case markConfirmed(thread: String, Marks)
    /// That read was given up: nothing answered it.
    case markUnconfirmed(thread: String)
    case threadsRead(Threads)
    case agentsRead(agents: [Record], providers: [Record]?, models: [Record]?, keys: ProviderKeys?)
    case cardsRead(Cards)
    case readFailed(Failure)
    case openingsRead([String: String])
    case rowsRead(thread: String, epoch: Int, version: Int, rows: [Record], full: Bool)
    case rowsFailed(thread: String, epoch: Int, Failure)
    case rowsAsked(String)
    case cachedLoaded(thread: String, epoch: Int, CachedTranscript?)
    case prefetched(thread: String, epoch: Int, version: Int, rows: [Record])
    case threadFetched(Record)
    /// A thread's row, asked for by name, was not read: the thread is
    /// gone (no failure), or the read failed.
    case threadUnfetched(thread: String, Failure?)
    /// The row read again for a mark ahead of it, nil where the thread is
    /// gone: a read that started after that mark was learned.
    case rowReread(thread: String, Record?, mark: Int)
    /// A page of a list past its first, or the archived chats' first:
    /// `after` is the cursor it was asked from.
    case pageRead(part: ChatLists.Part, after: String?, rows: [Record], cursor: String?, marks: Marks?)
    /// `compacted`: the server can no longer go on from the cursor, and
    /// the list is read again from its start.
    case pageFailed(part: ChatLists.Part, after: String?, Failure, compacted: Bool)
    /// The rows of a thread before the row whose ordinal is `before`;
    /// `more` where the read filled its window.
    case olderRowsRead(thread: String, epoch: Int, before: Int, rows: [Record], more: Bool)
    case olderRowsFailed(thread: String, epoch: Int)
    /// The messages a window's search found, the rows of the chats they
    /// are in where no list holds them, and the person's marks of those.
    case searchRead(scene: String, query: String, hits: [Record], threads: [Record], marks: Marks?)
    /// `unserved`: the server takes no search from this sign-in.
    case searchFailed(scene: String, query: String, Failure, unserved: Bool)

    // The watch
    /// `kinds`: the kinds the watch that opened follows. A watch opened
    /// again for one more kind is asked for while the one before it may
    /// still be opening, and that one's word says nothing of the kind.
    case watchOpened(fresh: Bool, kinds: [String] = [])
    /// `history`: the generation of the history the watch followed when it
    /// said the row, which the cache's bookmark must still be of when the
    /// row is noted there.
    case watchRow(WatchRow, history: String? = nil)
    /// The watch's position was refused as `compacted`, at this instant on
    /// the app's clock: the list is read again, once a minute
    /// (`WatchFollower.recovers`).
    case watchRelist(at: Date)
    /// The feed dropped, or came back.
    case watchInterrupted(Bool)
    case watchEnded(String?, Failure?)
    case feedDue
    case feedApplied(Feed)

    // Scenes
    /// `lands`: the window opens to something (`ChatApp.land`), and
    /// `restoring` is the chat it was left in a short while ago.
    case sceneAppeared(scene: String, restoring: String?, key: ChildKey, lands: Bool = false)
    case sceneActive(scene: String, active: Bool)
    case sceneGone(scene: String)
    /// `message`: the row the chat is opened at, from a search.
    case show(thread: String, scene: String, key: ChildKey, message: String? = nil)
    case newChat(agent: String?, listen: Bool, scene: String, key: ChildKey)
    case back(scene: String)
    case searched(scene: String, query: String)
    case draftKept(key: ChildKey, thread: String?, text: String)
    case routed(ChatRoute)
    case routeClaimed(scene: String, key: ChildKey)

    // Reading a chat
    /// A window's view said whether somebody looking at it sees the end of
    /// the conversation it shows.
    case endShown(scene: String, key: ChildKey, shown: Bool)
    /// The end of a chat has been on screen for a second.
    case sighted(thread: String, message: String, at: Date)
    /// The wait before a mark's write ended.
    case marksDue(at: Date)
    case markWritten(thread: String, sent: MarkChange, Record)
    /// The failure is said where the write moved a flag and the server
    /// refused it for good.
    case markWriteFailed(thread: String, sent: MarkChange, MarkRefusal, Failure? = nil)
    /// The offer to notify was answered.
    case notificationsAnswered(allow: Bool)

    // What the owner does
    case markedRead(String)
    case markedUnread(String)
    case pinAsked(thread: String, pinned: Bool)
    case archiveAsked(thread: String, archived: Bool, at: Date)
    case undoAsked
    /// Undo's few seconds passed.
    case undoPassed(String)
    /// A window lists the archived chats, or the chats again.
    case archivedShown(scene: String, shown: Bool)
    /// A list was scrolled to its end.
    case moreAsked(archived: Bool)
    /// A transcript was scrolled to the first row held.
    case olderRowsAsked(String)
    case renameAsked(thread: String, subject: String)
    case renamed(Record)
    case renameFailed(thread: String, before: Record?, Failure)
    case renameDismissed
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
    case cardWriteFailed(String, Failure)
    case cardRetried(CardReference)
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
        var unsent: [String: UnsentMessage] = [:]
        var preferences: ChatPreferences
        /// What the watch marked to read again that was not read before the
        /// app last stopped.
        var stale: [WatchKey]
    }

    typealias WatchKey = ChatAppState.WatchKey

    struct Threads: Codable, Equatable, Sendable {
        var chats: [Record]
        var automated: [Record]
        /// The person's marks of these threads, read with them; nil where
        /// the server keeps none, and where their read failed, which keeps
        /// what is held.
        var marks: Marks?
        /// Where the server's list goes on past `chats`; nil where it ends.
        var cursor: String?
        /// The pinned chats and the ones that wait on a person, each read
        /// whole beside the list; nil where the server keeps no flags.
        var pinned: [Record]?
        var waiting: [Record]?
        /// The archived chats' first page, read while a window lists them.
        var archived: [Record]?
        var archivedCursor: String?
        /// Threads held that the feed named and no list read holds, read
        /// one at a time, and the ones of them the server no longer holds.
        var named: [Record] = []
        var gone: [String] = []
        /// The server refused the filter arm the flags are read by.
        var flagsUnserved = false
    }

    /// An answered read of the person's marks.
    struct Marks: Codable, Equatable, Sendable {
        /// The threads asked about: one with no record here has no mark.
        var threads: [String]
        var records: [Record]
        /// The server answered that it does not know the kind.
        var unserved = false
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
        /// The server said it does not serve the continue route.
        var continueUnserved = false
        /// The message the server has not taken, as the folder keeps it.
        var unsent: UnsentMessage?
        /// The server refused `messageId` as a field it does not know.
        var idsUnserved = false
        /// `named` came from a read that found the pending message on the
        /// thread, not from a run.
        var adopted = false
        /// The draft holds a message the conversation put back.
        var gaveBack = false
        /// The person spoke in the chat: a message, Send Again, Try Again
        /// or Continue.
        var spoke = false
        /// The questions the server now holds an answer to in the person's
        /// own words: dismissed, so they do not wait for good.
        var answered: Conversation.WordsAnswer?
        /// What became of a reply typed into a notification.
        var replied: Conversation.ReplyOutcome?
        /// What the conversation's step says of itself, for the
        /// diagnostics file.
        var notes: [Diagnostic] = []
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
