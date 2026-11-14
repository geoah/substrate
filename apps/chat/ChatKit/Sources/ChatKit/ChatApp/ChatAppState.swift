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
        /// The read filled its window: there may be older rows.
        var full: Bool
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
        var name = "the assistant"
        /// The conversation's settles as last reported.
        var settles = 0
        var pollNudge = 0
        var resumeNudge = 0
        var continuing: String?
    }

    /// One window: the conversation it shows, whether it is in front, and
    /// what its list is searched for.
    struct Scene: Codable, Equatable, Sendable {
        var current: ChildKey?
        var active = true
        var query = ""
    }

    /// A question's, a suggestion's or an approval's card, as last read and
    /// acted on.
    struct Card: Codable, Equatable, Sendable {
        var record: Record?
        /// A suggestion's target, for its values before.
        var target: Record?
        var busy: String?
        var problem: String?
    }

    /// The owner's memories and instructions, as Settings last read them:
    /// what their assistants know about them and how they should work.
    struct Guidance: Codable, Equatable, Sendable {
        /// Newest first, each list as read.
        var memories: [Record] = []
        var instructions: [Record] = []
        var read = false
        var reading = false
        var problem: String?
        /// The records a write is out for, by path: one at a time each, so
        /// no write is made over a version another is moving.
        var busy: Set<String> = []
        /// What a write that did not go through left to say, by path.
        var failed: [String: String] = [:]

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
    var refreshing = false
    var refreshError: String?
    var feedProblem: String?
    var watching = false
    /// When the last read again for a `compacted` watch started: another
    /// refusal within a minute of it is a failure shown as one, never
    /// another refresh (`WatchFollower.recovers`). Nothing but the minute
    /// passing lets the next one start.
    var recoveryStarted: Date?

    var agents: [Record] = []
    var providers: [String: Record] = [:]
    /// The live `llm/model` records, by id; nil until read, or where the
    /// session may not read them, when no `runsOn` entry is said to fail.
    var models: [String: Record]?
    /// Whose key each provider row runs on; nil until read, when no row is
    /// said to lack one.
    var keys: ProviderKeys?
    var chats: [Record] = []
    var automated: [Record] = []
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
    var seen: [String: String] = [:]
    var notified: Set<String> = []
    var baselined = false
    var lastAgent: String?
    var preferences = ChatPreferences()
    var drafts: [String: String] = [:]

    var transcripts: [String: Held] = [:]
    /// Rule 4: every thread's read epoch. A read records the one it started
    /// under, and its rows are dropped when it moved.
    var readEpochs: [String: Int] = [:]
    /// The thread version each read in flight started under.
    var reading: [String: Int] = [:]

    /// The background run whose refresh is under way.
    var backgroundRun: BackgroundRun?
    /// A run granted before the folder was read, which names the
    /// generation the run is checked against.
    var waitingRun: BackgroundRun?

    var conversations: [ChildKey: Entry] = [:]
    var scenes: [String: Scene] = [:]
    var route: ChatRoute?
    var appActive = true

    var feedBatch: Set<WatchKey> = []
    var feedApplying: Set<WatchKey>?
    var cards: [String: Card] = [:]
    var renameProblem: String?
    var keyProblem: String?
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
