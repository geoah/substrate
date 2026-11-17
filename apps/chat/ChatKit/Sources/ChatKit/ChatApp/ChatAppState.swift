import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// Everything one signed-in repository's chats hold, for the whole process:
/// every window reads this one tree. Its rules are DESIGN.md's ("The store
/// and its concurrency rules"), each named where the engine keeps it.
public struct ChatAppState: Codable, Equatable, Sendable {
    /// One thread's rows as last published, their fold, and what they were
    /// read under.
    struct Held: Codable, Equatable, Sendable {
        var records: [Record]
        var turns: [Turn]
        var version: Int
        /// There may be rows before the ones held: the read filled its
        /// window, and no read of the rows before it has reached the
        /// thread's first.
        var full: Bool
        /// Rows were read in above the ones held: the turn that stood first
        /// before the last of them arrived, by its row, and how often that
        /// has happened. What a transcript on screen is kept at.
        var above: String?
        var grown = 0
        /// The server said these rows to this launch. Rows from the folder
        /// are as old as the launch that wrote them, whatever version they
        /// carry, until a list read in this launch says the thread has not
        /// moved since.
        var heard = false
    }

    /// One conversation the list keeps, and what it knows of it.
    struct Entry: Codable, Equatable, Sendable {
        var seed: Conversation.Seed
        var thread: String?
        var agent: String?
        var owns = false
        var active = false
        var streaming = false
        var outcomes = 0
        /// The conversation's settles as last reported.
        var settles = 0
        var pollNudge = 0
        var resumeNudge = 0
        /// The id of the message the conversation last said the server has
        /// not taken.
        var unsent: String?
        /// The draft the folder kept for the thread a new chat turned out
        /// to be, until the conversation has it in its composer under what
        /// was typed there.
        var draftFound: String?
        /// Where what the person had not read began when the chat was
        /// opened: the "New" line stands there while the chat stays open.
        var newFrom: NewFrom?
        /// The message the chat was opened at, from a search: its row, and
        /// how often the chat was asked to be put at a row.
        var opensAt: String?
        /// The "New" line is above the rows held, and is read toward.
        var line: LineSeek?
        /// The first row held when that read gave up: where the line that
        /// says the new messages begin above stands. It stays on that row
        /// when rows arrive above it, as a line a reader was shown does.
        var lineAt: String?
        var visits = 0
        /// A reply typed into a notification, for the conversation to send:
        /// kept until it says what became of it.
        var reply: Conversation.Reply?
    }

    /// One action of a notification under way: answered in the background,
    /// within `ChatApp.actionSeconds`, and then said finished to the
    /// device, with what became of it.
    struct Act: Codable, Equatable, Sendable {
        var response: NotificationResponse
        var thread: String
        /// A reply's: the conversation that sends it.
        var reply: ChildKey?
        /// A card's: the record it resolves.
        var card: CardReference?
    }

    /// An action heard before the app could act on it: the folder is not
    /// read yet, or the server's document, which says how a reply is sent.
    struct Deferred: Codable, Equatable, Sendable {
        var response: NotificationResponse
        var key: ChildKey
        var message: String
        var at: Date
    }

    /// Where a chat's unread part begins.
    enum NewFrom: Codable, Equatable, Sendable {
        /// After the message read through: the latest in the rows of
        /// these, the server's position and what this device saw since.
        case after([String])
        /// The person never opened the chat: everything held is new.
        case start
    }

    /// One window: the conversation it shows, whether it is in front, and
    /// what its list is searched for.
    struct Scene: Codable, Equatable, Sendable {
        var current: ChildKey?
        var active = true
        var query = ""
        /// The conversation whose end this window shows to somebody looking
        /// at it, as its view last said.
        var atEnd: ChildKey?
        /// The window's list is the archived chats.
        var archived = false
        /// The window drew before the folder was read: what it opens to is
        /// decided once the chats are held (`ChatApp.land`).
        var arriving: Arrival?
    }

    /// What a window that just appeared may go back to: the chat it showed
    /// when it last ran, where it was left within `ChatApp.freshFor`.
    struct Arrival: Codable, Equatable, Sendable {
        var restoring: String?
        var key: ChildKey
    }

    /// Where the server's list of chats goes on past the ones held.
    struct ListWindow: Codable, Equatable, Sendable {
        /// The cursor after the last chat read; nil where the list ended
        /// there, and until its first page is read.
        var cursor: String?
        /// Which list the cursor is of: the server takes a cursor back only
        /// with the filter it was cut for, so a window read as every chat
        /// starts over when it is read as the chats not archived.
        var part: ChatLists.Part?
        /// When the last chat of the last page read was last active: a
        /// chat older than it is past the window. Nil with no cursor.
        var end: Date?
        /// The first page was read from the server since the folder was
        /// opened: before that the chats held are the folder's.
        var read = false
        /// A read of the next page is out.
        var reading = false
        /// The next first page starts the window over, whatever it shares
        /// with the rows held: the person asked for the list anew, or the
        /// server could not go on from the cursor.
        var restart = false
    }

    /// The archived chats, read when a window lists them.
    struct Archive: Codable, Equatable, Sendable {
        var rows: [Record] = []
        var window = ListWindow()
    }

    /// The chat archived a moment ago, which Undo takes back until then.
    struct Undo: Codable, Equatable, Sendable {
        var thread: String
        var until: Date
    }

    /// One window's search of the server's messages.
    struct Search: Codable, Equatable, Sendable {
        var query: String
        /// The answer for `query` is not in yet: the pause, then the read.
        var reading = true
        /// The messages found, best first.
        var hits: [Record] = []
    }

    /// A chat opened at a message its rows do not hold yet: the rows
    /// before are read until they do, a few pages at most.
    struct Seek: Codable, Equatable, Sendable {
        var message: String
        var reads = 0
        /// The messages the person read through, where the rows are read
        /// toward the chat's "New" line and not toward a search's hit:
        /// the seek ends at the first of them the rows hold.
        var line: [String] = []
    }

    /// A chat opened with more than its person had read, whose rows held
    /// do not reach the message they read through: the rows before are
    /// read toward it, or were, and did not reach it.
    enum LineSeek: String, Codable, Sendable {
        case reading, missed
    }

    /// The end of a chat on screen in a window in front: `message` is the
    /// newest one the thread settled with. Read once it has been there a
    /// second, and then kept until the chat is left, so a chat marked
    /// unread while it is open stays unread until it is opened again or
    /// says more.
    struct Sight: Codable, Equatable, Sendable {
        var message: String
        var read = false
    }

    /// A question's, a suggestion's or an approval's card, as last read and
    /// acted on.
    struct Card: Codable, Equatable, Sendable {
        /// The card as it was asked for: kept from the moment its record
        /// is, before any read of it is answered, so a row of the feed
        /// that says the record moved while that read was out reads it
        /// again (`watchRow`). A read answered before the move and landing
        /// after it would otherwise be the last word: a card waiting here
        /// that was ended elsewhere.
        var asked: CardReference?
        var record: Record?
        /// A suggestion's target, for its values before.
        var target: Record?
        var busy: String?
        var problem: Failure?
        /// What the last write set and what the card said while it was
        /// out, kept so Try Again asks the same again over the version the
        /// card holds now.
        var tried: Tried?

        struct Tried: Codable, Equatable, Sendable {
            var properties: [String: JSONValue]
            var busy: String
        }

        /// `busy` while the dismissal is out that an answer in the person's
        /// own words makes: the card stays aside for it.
        static let words = "words"
    }

    /// A rename the server did not take: what was asked, so Try Again asks
    /// it again.
    struct Rename: Codable, Equatable, Sendable {
        var thread: String
        var subject: String
        var failure: Failure
    }

    /// The owner's memories and instructions, as Settings last read them:
    /// what their assistants know about them and how they should work.
    struct Guidance: Codable, Equatable, Sendable {
        /// Newest first, each list as read.
        var memories: [Record] = []
        var instructions: [Record] = []
        var read = false
        var reading = false
        var problem: Failure?
        /// The records a write is out for, by path: one at a time each, so
        /// no write is made over a version another is moving.
        var busy: Set<String> = []
        /// What a write that did not go through left to say, by path.
        var failed: [String: Failure] = [:]

        func record(_ reference: RecordReference) -> Record? {
            (reference.kind == Kinds.memory ? memories : instructions).first { $0.id == reference.recordID }
        }

        /// The record in its list, where it stood, or back in it, newest
        /// first.
        mutating func put(_ record: Record) {
            var list = record.kind == Kinds.memory ? memories : instructions
            if let at = list.firstIndex(where: { $0.id == record.id }) {
                list[at] = record
            } else {
                list.append(record)
                list.sort { $0.createdDate > $1.createdDate }
            }
            if record.kind == Kinds.memory { memories = list } else { instructions = list }
        }

        mutating func remove(_ reference: RecordReference) {
            if reference.kind == Kinds.memory {
                memories.removeAll { $0.id == reference.recordID }
            } else {
                instructions.removeAll { $0.id == reference.recordID }
            }
        }
    }

    /// Where asking for the permission to notify stands.
    enum NotificationOffer: String, Codable, Sendable {
        case checking, offered
    }

    /// What the watch reported, to read again once its batch is due.
    struct WatchKey: Codable, Hashable, Sendable {
        var kind: String
        var id: String
    }

    var session: ChatSession?
    var accessEnded = false
    var online = true
    /// The session's lease on the device's folder, once it was read.
    var disk: ChatStorage.Lease?
    /// The device's folder was read: what it held is drawn.
    var opened: Bool { disk != nil }
    /// Something to draw: the cache, or a first read.
    var loaded = false
    /// The diagnostics file's lines while Share Diagnostics shows them; nil
    /// while it is closed.
    var diagnostics: [String]?
    /// The server answered a refresh in this launch: the chats' rows are
    /// its own, and no longer only the folder's.
    var refreshed = false
    var refreshing = false
    /// The list's one failure, drawn at its top: a refresh that failed, or
    /// the change feed ended. A second replaces the first.
    var listFailure: Failure?
    /// The change feed dropped and is being opened again: a state the
    /// composer's status line may read, never drawn as a failure.
    var feedReconnecting = false
    var watching = false
    /// When the last read again for a `compacted` watch started: another
    /// refusal within a minute of it is a failure shown as one, never
    /// another refresh (`WatchFollower.recovers`). Nothing but the minute
    /// passing lets the next one start.
    var recoveryStarted: Date?
    /// The renames out, by thread, each with the subject it asked for.
    var renaming: [String: String] = [:]
    var renameFailure: Rename?
    /// A chat's rows could not be read when it was opened, by thread.
    var rowsFailed: [String: Failure] = [:]
    /// The `agents` feature's revision, from the server's discovery
    /// document; nil until it is read, when the chat calls nothing a
    /// revision added.
    var agentsRevision: Int?
    /// The server said it serves the continue route and then answered that
    /// it does not: believed until the document is read again.
    var continueUnserved = false

    /// The `agents` revision that has the continue route.
    static let continueRevision = 3
    /// The `records` revision that has the person's marks: `me` in a
    /// filter, and the mark kind.
    static let marksRevision = 8
    /// The `records` revision that has `pinned` and `archived` on a mark,
    /// and the `referencedBy` filter arm the lists are narrowed by.
    static let flagsRevision = 9
    /// Try Again and Continue may call the continue route. Asked of the
    /// server's document, never found out by calling: an older server
    /// answers 404, and then they go on as before the route.
    var continues: Bool { (agentsRevision ?? 0) >= Self.continueRevision && !continueUnserved }
    /// The server said it takes a `messageId` and then refused the field:
    /// believed until the document is read again.
    var idsUnserved = false
    /// The `agents` revision that takes a `messageId` on a chat message.
    static let messageIDRevision = 3
    /// Every message goes under an id of its own, so a send whose answer
    /// was lost is sent again. Asked of the server's document, never found
    /// out by sending one: an older server refuses the field.
    var sendsIDs: Bool { (agentsRevision ?? 0) >= Self.messageIDRevision && !idsUnserved }

    var agents: [Record] = []
    var providers: [String: Record] = [:]
    /// The live `llm/model` records, by id; nil until read, or where the
    /// session may not read them, when no `runsOn` entry is said to fail.
    var models: [String: Record]?
    /// Whose key each provider row runs on; nil until read, when no row is
    /// said to lack one.
    var keys: ProviderKeys?
    /// The chats, most recently active first: the server's list from its
    /// start down to `window`, each page as it was read, with the chats
    /// opened on their own beside them. Where the server keeps the flags it
    /// is the list of the chats not archived.
    var chats: [Record] = []
    var window = ListWindow()
    /// How many chats one read of the list asks for (`ChatApp.page`).
    var page = ChatLists.page
    var automated: [Record] = []
    /// The pinned chats, however old, and the ones that wait on a person,
    /// archived or not: each read whole beside the list, where the server
    /// keeps the flags. The pinned read also answers a pinned chat its
    /// person archived, which the mark tells.
    var pinned: [Record] = []
    var waiting: [Record] = []
    var archive = Archive()
    var undo: Undo?
    /// The pin or the archive the server refused for good, which Try Again
    /// on the list's strip writes again.
    var flagRefused: FlagRefused?

    struct FlagRefused: Codable, Equatable, Sendable {
        var thread: String
        var change: MarkChange
    }
    /// The server said it keeps the flags and then refused the filter arm
    /// they are read by: believed until the document is read again.
    var flagsUnserved = false
    /// Each window's search of the messages, by scene.
    var searches: [String: Search] = [:]
    /// The server refused the search: believed until the document is read
    /// again, and the device's own rows are searched alone.
    var searchUnserved = false
    /// Threads read one at a time that no list read holds: an old chat, or
    /// an automated run while those are hidden.
    var others: [String: Record] = [:]
    var openings: [String: String] = [:]
    var interactions: [Record] = []
    var requests: [Record] = []
    /// The calls waiting on the owner's approval. Never kept on the device:
    /// one decided elsewhere must not wait here after a relaunch.
    var approvals: [Record] = []
    /// The server answered the approvals read: it declares the kind and the
    /// token reaches it, so the watch may follow it. A watch naming a kind
    /// an older server lacks is refused whole.
    var approvalsKnown = false
    /// The kinds the watch was opened again to follow as well, until it is
    /// open: each is read once more then (`ChatApp.widen`).
    var watchGains: Set<String> = []
    var seen: [String: String] = [:]
    /// The `records` feature's revision, from the server's discovery
    /// document; nil until it is read.
    var recordsRevision: Int?
    /// The server said it keeps marks and then answered that it does not
    /// know the kind: believed until the document is read again.
    var marksUnserved = false
    /// The person's marks by thread, as the server last said them: what is
    /// unread. Nil while the server keeps none (one older than marks), when
    /// `seen` says it, as before marks.
    var marks: [String: ThreadMark]?
    /// A read of the marks was answered since the folder was opened: the
    /// server declares the kind and the token reaches it, so the watch may
    /// follow it. A watch naming a kind an older server lacks is refused
    /// whole.
    var marksKnown = false
    /// The marks this device moved and has not sent yet, by thread, and the
    /// ones on their way: each reads as written here at once.
    var marking: [String: MarkChange] = [:]
    var markSending: [String: MarkChange] = [:]
    /// The threads a reply was notified of since the app was launched:
    /// where the server keeps marks, each is taken back once its mark says
    /// the reply was read, however the row and the mark came to agree, and
    /// whether or not the marks were known when it was notified.
    var announced: Set<String> = []
    /// The threads whose mark was learned without their row and is ahead
    /// of the row held (`ThreadMark.ahead`): another device read a reply
    /// this one has not read the thread's row for. Each reads as it did
    /// until its row catches up, or a read of the row that started after
    /// the mark was learned has answered.
    var rowsBehind: Set<String> = []
    /// The threads whose mark is being read beside a message their person
    /// just sent (`ChatAppAsk.confirmMark`): until that read is answered
    /// or given up, a mark learned by any read that says the chat is
    /// archived says what it was when they spoke, as the read's own
    /// answer would (`ChatApp.followAgain`).
    var confirming: Set<String> = []
    /// The threads shown with no row held, whose row is being read by
    /// name (`ChatAppAsk.fetchThread`): their rows are read once it is,
    /// under the version it says, and not before under none, a read that
    /// the row's arrival would only replace.
    var rowAwaited: Set<String> = []
    /// When the wait before the next write ends.
    var marksDue: Date?
    /// The chats whose end somebody is looking at, or looked at since they
    /// were opened, by thread.
    var sights: [String: Sight] = [:]
    /// The permission to notify: about to be asked for, once the system
    /// says whether it ever was (`checking`), or offered in the app's own
    /// words (`offered`), which the system's prompt follows.
    var notificationOffer: NotificationOffer?
    var notified: Set<String> = []
    var baselined = false
    var lastAgent: String?
    var preferences = ChatPreferences()
    var drafts: [String: String] = [:]
    /// The messages sent under an id of their own that the server has not
    /// taken, by chat as `drafts` names it: kept in the folder beside the
    /// drafts, so a kill never loses one or the id to send it again under.
    var unsent: [String: UnsentMessage] = [:]
    /// The ids the folder held when it was last written, or read: a
    /// message's first copy leaves only once its id is among them.
    var keptUnsent: Set<String> = []
    /// The last write of the unsent messages failed: the ids the folder
    /// does not hold, the device's words for why, and how often the write
    /// has failed in a row. Their messages go all the same, kept only
    /// while the app runs, and the write is made again.
    var unsaved: Unsaved?

    struct Unsaved: Codable, Equatable, Sendable {
        var ids: Set<String>
        var seen: String
        var failures: Int
    }

    var transcripts: [String: Held] = [:]
    /// Rule 4: every thread's read epoch. A read records the one it started
    /// under, and its rows are dropped when it moved.
    var readEpochs: [String: Int] = [:]
    /// The thread version each read in flight started under.
    var reading: [String: Int] = [:]
    /// The threads a read of the rows before the ones held is out for.
    var olderReading: Set<String> = []
    /// The chats opened at a message their rows do not hold yet, by thread.
    var seeking: [String: Seek] = [:]

    /// The background run whose refresh is under way.
    var backgroundRun: BackgroundRun?
    /// A run granted before the folder was read, which names the
    /// generation the run is checked against.
    var waitingRun: BackgroundRun?

    var conversations: [ChildKey: Entry] = [:]
    var scenes: [String: Scene] = [:]
    var route: ChatRoute?
    var appActive = true
    /// A reply being written is revealed at a steady rate (`TextReveal`).
    /// The device says so; until it does, and where it asks for less
    /// motion, each piece is drawn as it arrives.
    var paces = false
    /// The app is leaving the front or has left it: from the first word of
    /// it (inactive comes before background) until it is in front again.
    /// What a field says meanwhile is written with no wait.
    var leaving = false

    /// The actions of notifications under way, by the response's stamp
    /// (`ChatNotifications.stamp`), and every response acted on since the
    /// launch: the same one heard twice acts once.
    var acting: [String: Act] = [:]
    var acted: Set<String> = []
    var deferred: [Deferred] = []
    /// The resolved cards the server said are the person's own, and the
    /// ones a whole answer of theirs did not hold (`ChatAppHands.swift`).
    var ownCards: Set<String> = []
    var othersCards: Set<String> = []
    /// The threads whose read of the person's own cards is out, and the
    /// cards the last one answered was asked about, by thread.
    var ownReading: Set<String> = []
    var ownAsked: [String: Set<String>] = [:]
    /// The message each question was answered by in the person's own
    /// words, by the question's id: a card read as replied in it is theirs.
    var wordsSent: [String: String] = [:]
    /// The cards whose notification waits for their turn to settle, by id
    /// (`ChatAppAttention.swift`).
    var heldCards: [String: Record] = [:]
    /// The card notifications that stand, by thread: taken back once
    /// nothing waits in the chat.
    var attention: [String: [String]] = [:]
    /// The settle each card's notification stands for, by the card's id:
    /// its thread's `finishedAt`. A later reply in the chat is its own.
    var cardSaid: [String: String] = [:]
    /// The threads a reply's own notification stands for, of the ones in
    /// `announced`: a read takes it back where a card's must stay.
    var replySaid: Set<String> = []
    /// The threads a notification says an action could not be done in:
    /// each stands until its chat is opened here.
    var failedActions: Set<String> = []
    /// A reply typed into a notification that could be neither sent nor
    /// written down in its time, by thread: the folder was not read yet.
    /// It goes into the thread's draft when the folder is.
    var unkept: [String: String] = [:]

    var feedBatch: Set<WatchKey> = []
    var feedApplying: Set<WatchKey>?
    var cards: [String: Card] = [:]
    var keyProblem: Failure?
    /// A provider's key was saved: its sheet closes.
    var keySaved = Signal<String>()
    /// A conversation's run settled: announced, where its chat is on screen.
    var answered = Signal<ChatAnswer>()
    var usage: [AreaUsage] = []
    var permission: NotificationPermission?
    var guidance = Guidance()
    /// Whether the next sign-out forgets the saved chats: Sign Out does,
    /// Sign In Again after a refused token does not.
    var forgetOnSignOut = true

    /// What the last applied event asked for.
    var asks: [ChatAppAsk] = []
    /// The last event started the chats over: another repository, or none.
    var restarted = false

    public init() {}
}
