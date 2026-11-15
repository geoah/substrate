import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateTesting

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// A stream of numbers a seed fixes (SplitMix64): the same on every
/// platform, which the system's generators are not.
struct SeededNumbers: Sendable {
    private var state: UInt64

    init(seed: UInt64) { state = seed }

    mutating func next() -> UInt64 {
        state &+= 0x9E37_79B9_7F4A_7C15
        var z = state
        z = (z ^ (z >> 30)) &* 0xBF58_476D_1CE4_E5B9
        z = (z ^ (z >> 27)) &* 0x94D0_49BB_1331_11EB
        return z ^ (z >> 31)
    }

    /// In `0 ..< bound`.
    mutating func below(_ bound: Int) -> Int { Int(next() % UInt64(max(1, bound))) }

    /// In `0 ..< 1`.
    mutating func unit() -> Double { Double(next() >> 11) / Double(1 << 53) }

    mutating func chance(_ probability: Double) -> Bool { unit() < probability }

    mutating func pick<Value>(_ values: [Value]) -> Value { values[below(values.count)] }

    /// A number a text fixes, for a draw that must not depend on the order
    /// requests arrive in (FNV-1a).
    static func hash(_ text: String) -> UInt64 {
        var hash: UInt64 = 0xCBF2_9CE4_8422_2325
        for byte in text.utf8 {
            hash ^= UInt64(byte)
            hash = hash &* 0x0000_0100_0000_01B3
        }
        return hash
    }
}

/// A substrate's chat routes as a correct server keeps them, which then
/// misbehaves by a seed.
///
/// **The truth.** It holds threads and their message rows as #1403's server
/// would: a message under a `messageId` is written once, a request that
/// repeats the id writes nothing and runs nothing and is answered as an
/// attach is, an id that names another message is `409`, a thread runs one
/// turn at a time and a person four, a continue writes no message. A turn
/// runs detached from the request that started it and moves only when the
/// test says (`work`), so every interleaving is the test's to choose.
/// `rows(of:)` is what a transcript must end up equal to.
///
/// **The marks.** Where its `records` feature is at revision 8 it keeps the
/// person's mark of each thread as #1405's server does: one a thread, a
/// position that only moves forward through the thread's rows, a Mark as
/// Unread a position taken clears unless the same write sets it, and the
/// thread's `lastMessage`, the newest row it settled with, a message no
/// reply came after passed over (#1419). Another device
/// of the same person moves a mark when the test says (`elsewhere`), and
/// every mark that moved is a row on the change feed, in the order it
/// moved. Below that revision it does not know the kind.
///
/// **The flags and the lists.** At `records` 9 a mark also holds `pinned`
/// and `archived`, as #1459's server keeps them (decision 0322): two flags
/// only a person's write moves, which move nothing else. A list of threads
/// is narrowed by the marks that point at its rows (`referencedBy`), a
/// thread counts what waits on a person (`waitingOnPerson`, moved when the
/// test says, and told on the feed as a row of the thread), and every list
/// is cut into pages with a cursor. Below 9 it refuses the arm (`400`) and
/// the two properties (`422`). A search of the messages (`q`) answers the
/// rows that hold the words.
///
/// **The faults.** Each request draws its fault from the seed, the request's
/// own signature and how many like it came before, so the draw does not
/// turn on the order two requests of different kinds arrive in: a `500`, a
/// gateway's `502` page, a `429` with or without `Retry-After`, a `401`, a
/// request that hangs past its timeout or never reaches the server, a page
/// of HTML with a `200`, a stream that closes before its `thread` event, a
/// stream cut at any event, an event sent twice, and a delay, all on the
/// rig's clock. A write is taken or not before its answer is lost, by the
/// same draw: that is the case `messageId` exists for. An attach replays
/// its turn from the start, as the server does.
///
/// **A run of several calls.** A turn is one or more model calls: a reply's
/// pieces, then for some a tool call and another reply. A call's reply row
/// is written with its tool call, the tool's row at its own ordinal, and
/// the next call draws its reply's ordinal past everything written since.
///
/// **A message sent while a turn runs** (`agents` 6, #1419). A message
/// under an id into a running turn is admitted: its row is written at once
/// at the next ordinal, the request is answered with the turn's stream from
/// its start, and every stream of the turn says `messageQueued`. The next
/// call says `messageTaken` for each message that waits. A turn that ends
/// `ok` with one unread starts the next turn itself, which opens with
/// `messageTaken`; one stopped or failed starts nothing, and the message
/// stays in the thread for a continue or the next message. Below 6 the
/// same send is `409 thread_busy`.
///
/// **Questions and approvals.** A tool call may ask the person (`ask`,
/// which lands an `llm/interaction`) or wait for their approval (which
/// lands an `llm/approval`). Each is resolved by one write over the version
/// read, refused `409` once it moved, by the app or by another device of
/// the person (`resolve`). A resolution writes its `system` row into the
/// thread, tells the change feed, and resumes the thread: at once where no
/// turn runs, and after a turn that ends `ok` where one does. A stopped
/// thread is resumed by nothing. A typed message answers and dismisses
/// nothing. A thread's row counts what waits on it (`waitingOnPerson`).
final class SeededServer: @unchecked Sendable {
    /// One `llm/message` row.
    struct Row: Equatable, Sendable {
        var id: String
        var role: String
        var content: String
        var turn: Int
        var createdAt: Date
        /// An assistant row's tool call, and a tool row's: the call's id,
        /// the name the model saw, and the callable behind it.
        var call: String?
        var tool: String?
        var callable: String?
        /// A tool row's stamp: the record its call landed.
        var landed: Landed?

        struct Landed: Equatable, Sendable {
            var kind: String
            var id: String
        }

        /// What somebody said: a tool's row and the substrate's own are
        /// not that.
        var spoken: Bool { role == "user" || role == "assistant" }
    }

    /// Questions an agent asked, or a call that waits for approval: the
    /// record a tool call landed, which waits on the person.
    struct Card: Equatable, Sendable {
        static let interaction = "substrate.reamde.dev/llm/interaction"
        static let approval = "substrate.reamde.dev/llm/approval"

        struct Question: Equatable, Sendable {
            var id: String
            var prompt: String
            var options: [String]
            var multi = false
        }

        var id: String
        var kind: String
        var thread: String
        var state = "pending"
        var version = 1
        var questions: [Question] = []
        /// By question, the option values chosen.
        var answers: [String: [String]] = [:]
        var tool = ""
        var arguments: [String: String] = [:]
        var createdAt: Date
        var resolvedAt: Date?
        /// How many writes resolved it: one, ever.
        var resolutions = 0
        /// Whose hand resolved it, from `agents` 7: `me`, the person the
        /// app is signed in as, or another person of the repository.
        var by: String?
        /// The message questions were answered in, in the person's own
        /// words (`replied`, `agents` 7).
        var repliedIn: String?
        /// How often a write asked for it to be dismissed, taken or not.
        var dismissals = 0
        /// How often its resolution resumed its thread.
        var resumes = 0

        var pending: Bool { state == "pending" }
    }

    /// One reader of a running turn's events.
    private struct Reader {
        var id: Int
        var continuation: AsyncThrowingStream<String, any Error>.Continuation
        /// Lines delivered so far, the `thread` line included.
        var delivered = 0
        var fault: StreamFault
        var thread = ""
    }

    /// What a turn does next.
    enum Step: Equatable, Sendable {
        /// A piece of the reply being written, by its number.
        case piece(Int)
        /// The reply asks for a tool: its row is written with the call, the
        /// tool runs and its row is written.
        case tool(Tool)
        /// The next model call: it reads what was sent meanwhile, and its
        /// reply draws an ordinal past all of it.
        case call
    }

    enum Tool: Equatable, Sendable { case query, ask, approve }

    /// A turn running on a thread.
    private struct Turn {
        /// The ordinal the reply being written will carry.
        var ordinal: Int
        /// Every event emitted so far: what an attach replays.
        var emitted: [String] = []
        var remaining: [Step]
        var written = ""
        /// The code points of the reply being written so far.
        var before = 0
        /// The messages admitted into the turn that no call has read yet.
        var queued: [String] = []
        /// Resolutions that landed while the turn ran: it is resumed for
        /// them once it ends on its own.
        var resolutions = 0
        /// The turn ends in a provider's failure instead of its last piece.
        var fails: Bool
        var readers: [Reader] = []
        /// The server restarts once the turn has emitted this many events:
        /// every stream of it ends with no word, and the server takes the
        /// run up again.
        var restartsAfter: Int?
        /// The run was taken up again and this server process has no
        /// stream of it: an attach is told `idle` and `running`. From
        /// `agents` 5 it is given one, `unready` attaches later; below, it
        /// has none until it settles.
        var streamless = false
        var unready = 0
    }

    private struct Thread {
        var id: String
        var agent: String
        var status = "running"
        var cause: String?
        var version = 1
        var rows: [Row] = []
        var nextTurn = 0
        var startedAt: Date
        var updatedAt: Date
        var finishedAt: Date?
        var lastReply: String?
        /// The newest row when the thread last settled.
        var lastMessage: String?
        var turn: Turn?
        /// How many questions, suggestions and approvals wait on a person.
        var waiting = 0
    }

    /// The person's mark of a thread.
    struct Mark: Equatable, Sendable {
        var readThrough: String?
        /// Absent until set.
        var markedUnread: Bool?
        var version = 0
        var readAt: Date?
        /// Absent until set.
        var pinned: Bool?
        var archived: Bool?
    }

    /// A write of a mark the app sent, whatever became of it.
    struct MarkWrite: Equatable, Sendable {
        var thread: String
        var readThrough: String?
        var markedUnread: Bool?
        var at: Date
        var pinned: Bool?
        var archived: Bool?
    }

    /// What a stream does wrong once it is open.
    struct StreamFault: Equatable, Sendable {
        /// The stream ends after this many lines.
        var cutAfter: Int?
        /// It ends with the connection's error, not quietly.
        var loud = false
        /// This line is sent twice.
        var duplicate: Int?
        /// The cut is the server's own, of a reader that fell behind, and
        /// it says so first (`lagged`, `agents` 5).
        var lagged = false

        static let none = StreamFault()
    }

    /// What a request meets before any answer.
    enum Fault: Equatable, Sendable {
        case none
        /// The server's own failure, with its envelope.
        case status(Int, code: String, retryAfter: String?)
        /// A gateway's page.
        case gateway
        case unauthorized
        /// No answer until the request's own timeout.
        case hang
        /// The request never reaches the server.
        case unreachable
        /// The connection drops before a byte of the answer.
        case dropped
        /// A captive portal's page, with a `200`.
        case page
        case delay(Double)
        case stream(StreamFault)
    }

    /// Every sentence the server says in its own words: an envelope's
    /// message, a thread's reason, a failed turn's error. The app shows
    /// them under Details and writes them nowhere, the diagnostics file
    /// least of all, which a seeded run holds it to (`SeededRun.discreet`).
    static let said = ["the server could not answer", "the token was revoked", "the run ended", "llm: overloaded_error", "unknown type"]

    let seed: UInt64
    let clock: TestClock
    /// The `agents` feature's revision the discovery document states: below
    /// 3 the server refuses `messageId` and has no continue route.
    let revision: Int
    /// The `records` feature's revision: 8 keeps the person's marks.
    let records: Int
    let agent: String
    private let lock = NSLock()
    private var marks: [String: Mark] = [:]
    private var written: [MarkWrite] = []
    /// The change feed: a row for each mark and each card that moved, and
    /// who follows which kinds.
    private var feedSeq = 1
    private var ordered = Date.distantPast
    private var feed: [(seq: Int, kind: String, line: String)] = []
    private var watchers: [Int: (port: Int, kinds: Set<String>, to: AsyncThrowingStream<String, any Error>.Continuation)] = [:]
    private var cards: [String: Card] = [:]
    private var cardOrder: [String] = []
    /// The messages a model call was said to read, by id.
    private var taken: Set<String> = []
    /// The messages admitted into a running turn, by id.
    private var joined: Set<String> = []
    /// What the next turns do, where a test says so in place of the seed:
    /// each runs to its end unless `failing`, with no restart.
    private var planned: [(steps: [Step], fails: Bool)] = []
    /// The faults the next requests draw, where a test says so in place of
    /// the seed: each for the first request whose signature starts so.
    private var forced: [(prefix: String, fault: Fault, lands: Bool)] = []
    private var threads: [String: Thread] = [:]
    private var order: [String] = []
    private var minted = 0
    private var readers = 0
    private var counts: [String: Int] = [:]
    /// The chance a request is faulted, 0 for a server that behaves.
    private var rate = 0.0
    private var allows401 = false
    private var port = 0
    private var open: [String: Int] = [:]
    private var seen = 0
    private var refused401 = false
    private var faults = 0
    private var counted: [String: Int] = [:]
    private var inside: [Int: String] = [:]
    private var sleeping: [Int: Date] = [:]
    /// Every chat request that reached the server, by its message's text.
    private var chats: [String: Int] = [:]
    /// The threads a stream of which sent an event twice.
    private var twice: Set<String> = []
    /// The seeded thread the server was made on, nil when on none, and
    /// where the run was met on any other: work of a seeded run that left
    /// the run's thread, which the seed no longer orders.
    private let home = SeededThread.current
    private var strays: [String] = []

    init(seed: UInt64, clock: TestClock, revision: Int = 3, records: Int = 8, agent: String = helper) {
        self.seed = seed
        self.clock = clock
        self.revision = revision
        self.records = records
        self.agent = agent
    }

    // MARK: What the test reads and moves

    /// The rows a correct server holds of a thread, in order.
    func rows(of thread: String) -> [Row] { lock.withLock { threads[thread]?.rows ?? [] } }

    /// What somebody said in a thread, in order: the rows a transcript
    /// draws as messages.
    func spoken(in thread: String) -> [Row] { rows(of: thread).filter(\.spoken) }

    /// A message sent while a turn ran, as the server holds it: admitted
    /// into a turn, and whether a model call was said to read it.
    func wasQueued(_ message: String) -> Bool { lock.withLock { joined.contains(message) } }
    func wasTaken(_ message: String) -> Bool { lock.withLock { taken.contains(message) } }

    /// The messages a running turn holds unread, by thread.
    func queued(in thread: String) -> [String] { lock.withLock { threads[thread]?.turn?.queued ?? [] } }

    /// A card as the server holds it.
    func card(_ id: String) -> Card? { lock.withLock { cards[id] } }

    /// Every card, in the order they were asked.
    var allCards: [Card] { lock.withLock { cardOrder.compactMap { cards[$0] } } }

    /// The next turn does `steps`, whatever the seed would have drawn: a
    /// test's own run. `.piece` numbers each reply's pieces from 0.
    func plan(_ steps: [Step], failing: Bool = false) { lock.withLock { planned.append((steps, failing)) } }

    /// The next request whose signature starts with `prefix` (`chat|`,
    /// `decide|`, `events|`) draws `fault`; a write then lands or not.
    func fail(_ prefix: String, with fault: Fault, lands: Bool = false) { lock.withLock { forced.append((prefix, fault, lands)) } }

    /// Another device of the same person sends a message into a thread
    /// under an id of its own: taken into the turn running there, or
    /// starting one. False where the server refused it.
    @discardableResult func elsewhere(send text: String, to thread: String, id: String) -> Bool {
        lock.withLock {
            if case .stream = chat(agent, ["thread": thread, "message": text, "messageId": id]) { return true }
            return false
        }
    }

    /// Another device of the same person resolves a card: answers its
    /// questions with each one's first option, or dismisses them; approves
    /// a call, or declines it. False where it no longer waits.
    ///
    /// - Parameter person: `me` for a device of the same person, anything
    ///   else for another person of the repository.
    @discardableResult func elsewhere(card id: String, accept: Bool, by person: String = "me") -> Bool {
        lock.withLock {
            guard let card = cards[id], card.pending else { return false }
            let state = card.kind == Card.approval ? (accept ? "approved" : "denied") : (accept ? "answered" : "dismissed")
            let answers = Dictionary(card.questions.map { ($0.id, Array($0.options.prefix(1))) }) { first, _ in first }
            if case .json(200, _) = resolve(id, to: state, answers: accept ? answers : [:], ifVersion: card.version, by: person) {
                note("a card resolved on another device")
                return true
            }
            return false
        }
    }

    var threadIDs: [String] { lock.withLock { order } }

    func version(of thread: String) -> Int? { lock.withLock { threads[thread]?.version } }

    func running(_ thread: String) -> Bool { lock.withLock { threads[thread]?.turn != nil } }

    var anyRunning: Bool { lock.withLock { threads.values.contains { $0.turn != nil } } }

    var keepsMarks: Bool { records >= 8 }
    var keepsFlags: Bool { records >= 9 }

    /// How many things wait on a person in a thread.
    func waiting(_ thread: String) -> Int { lock.withLock { threads[thread].map(waits) ?? 0 } }

    /// Called under the lock: what a thread's row counts as waiting on a
    /// person. Its own cards that wait, and what a test or a step said
    /// waits in it beside them (`wait(_:on:)`), which no card stands for.
    private func waits(_ thread: Thread) -> Int {
        thread.waiting + cards.values.filter { $0.thread == thread.id && $0.pending }.count
    }

    /// Called under the lock: what waits in a thread moved with a card of
    /// its own, so its row moved, and the feed tells of it.
    private func counted(_ thread: inout Thread) {
        thread.version += 1
        thread.updatedAt = clock.now
        tell("substrate.reamde.dev/llm/thread", thread.id)
    }

    /// A thread comes to wait on a person, or is answered: its row moves,
    /// and the feed tells of it. False where nothing moved.
    @discardableResult func wait(_ thread: String, on count: Int) -> Bool {
        lock.withLock {
            guard var held = threads[thread], held.waiting != count else { return false }
            held.waiting = count
            held.version += 1
            held.updatedAt = clock.now
            threads[thread] = held
            tell("substrate.reamde.dev/llm/thread", thread)
            return true
        }
    }

    /// Another device of the same person pins or archives a thread, or
    /// takes either back. False where it had nothing to move.
    @discardableResult func elsewhere(_ thread: String, pinned: Bool? = nil, archived: Bool? = nil) -> Bool {
        lock.withLock {
            guard keepsFlags, threads[thread] != nil else { return false }
            return put(thread, readThrough: nil, markedUnread: nil, pinned: pinned, archived: archived)
        }
    }

    /// The threads the list of chats answers, in its order: every one, or
    /// where the server keeps the flags the ones not archived.
    var listed: [String] { lock.withLock { ordered(threads: order.compactMap { threads[$0] }).filter { marks[$0.id]?.archived != true }.map(\.id) } }

    /// Called under the lock: a row on the change feed, for whoever
    /// follows its kind.
    private func tell(_ kind: String, _ id: String) {
        feedSeq += 1
        let row = line(["seq": feedSeq, "ts": stamp(clock.now), "kind": kind, "recordId": id, "op": "patch"])
        feed.append((feedSeq, kind, row))
        for reader in watchers.keys.sorted() where watchers[reader]?.port == port && watchers[reader]?.kinds.contains(kind) == true {
            watchers[reader]?.to.yield(row)
        }
    }

    /// The server's order of a list of threads: most recently moved first,
    /// and of those moved at one instant the one made last.
    private func ordered(threads held: [Thread]) -> [Thread] {
        held.sorted { a, b in
            a.updatedAt == b.updatedAt ? (order.firstIndex(of: a.id) ?? 0) > (order.firstIndex(of: b.id) ?? 0) : a.updatedAt > b.updatedAt
        }
    }

    /// The newest row a thread settled with.
    func lastMessage(of thread: String) -> String? { lock.withLock { threads[thread]?.lastMessage } }

    /// The person's mark of a thread as the server holds it.
    func mark(of thread: String) -> Mark? { lock.withLock { marks[thread] } }

    /// Every write of a mark the app sent, in the order they came.
    var markWrites: [MarkWrite] { lock.withLock { written } }

    /// Another device of the same person moves a mark: reads the thread to
    /// its newest row, or marks it unread. False where it had nothing to
    /// move.
    @discardableResult func elsewhere(_ thread: String, unread: Bool) -> Bool {
        lock.withLock {
            guard keepsMarks, let held = threads[thread] else { return false }
            if unread { return put(thread, readThrough: nil, markedUnread: true) }
            guard let last = held.lastMessage else { return false }
            return put(thread, readThrough: last, markedUnread: nil)
        }
    }

    /// The server's own time for what it orders by, a thread settling and
    /// a position taken: the clock's, and never the same instant twice,
    /// since the run's clock stands still between its steps and a real
    /// server's does not. A position is taken after the thread settled
    /// with its message, and the stamps say so.
    private func instant() -> Date {
        ordered = max(clock.now, ordered.addingTimeInterval(0.001))
        return ordered
    }

    /// Called under the lock: the rules of a mark's write. True when the
    /// mark moved.
    private func put(_ thread: String, readThrough: String?, markedUnread: Bool?, pinned: Bool? = nil, archived: Bool? = nil) -> Bool {
        guard let rows = threads[thread]?.rows else { return false }
        var mark = marks[thread] ?? Mark()
        var changed = marks[thread] == nil
        if let readThrough, let place = rows.firstIndex(where: { $0.id == readThrough }) {
            let stored = mark.readThrough.flatMap { id in rows.firstIndex { $0.id == id } }
            // Forward only: an earlier row leaves the position alone.
            if stored.map({ place >= $0 }) ?? true {
                let cleared = mark.markedUnread == true && markedUnread != true
                if mark.readThrough != readThrough || cleared { changed = true }
                // Taken after the thread settled with it, never at the
                // same instant: the reader had to see it first.
                if mark.readThrough != readThrough { mark.readAt = instant() }
                mark.readThrough = readThrough
                if cleared { mark.markedUnread = false }
            }
        }
        if let markedUnread, mark.markedUnread != markedUnread {
            mark.markedUnread = markedUnread
            changed = true
        }
        // A flag moves nothing else, and nothing else moves a flag.
        if let pinned, mark.pinned != pinned {
            mark.pinned = pinned
            changed = true
        }
        if let archived, mark.archived != archived {
            mark.archived = archived
            changed = true
        }
        guard changed else { return false }
        mark.version += 1
        marks[thread] = mark
        tell("substrate.reamde.dev/llm/threadmark", "\(thread).me")
        return true
    }

    private func record(_ mark: Mark, thread: String) -> [String: Any] {
        var properties: [String: Any] = [
            "thread": ["ref": "substrate.reamde.dev/llm/thread/\(thread)"], "readBy": ["ref": "substrate.reamde.dev/core/actor/me"],
        ]
        if let read = mark.readThrough { properties["readThrough"] = ["ref": "substrate.reamde.dev/llm/message/\(read)"] }
        if let at = mark.readAt { properties["readAt"] = stamp(at) }
        if let unread = mark.markedUnread { properties["markedUnread"] = unread }
        if let pinned = mark.pinned { properties["pinned"] = pinned }
        if let archived = mark.archived { properties["archived"] = archived }
        return [
            "id": "\(thread).me", "kind": "substrate.reamde.dev/llm/threadmark", "version": mark.version,
            "createdAt": "2026-10-30T10:00:00Z", "updatedAt": stamp(mark.readAt ?? clock.now), "properties": properties,
        ]
    }

    /// How many user rows hold exactly this text, in every thread.
    func userRows(saying text: String) -> Int {
        lock.withLock { threads.values.reduce(0) { $0 + $1.rows.filter { $0.role == "user" && $0.content == text }.count } }
    }

    /// Whether a user row holds this text, alone or inside a longer
    /// message: a draft kept beside what was typed is sent as one.
    func holds(_ text: String) -> Bool {
        lock.withLock { threads.values.contains { $0.rows.contains { $0.role == "user" && $0.content.contains(text) } } }
    }

    /// Every row id two rows share, in any thread: none, ever.
    var sharedRowIDs: [String] {
        lock.withLock {
            var seen: Set<String> = []
            var shared: Set<String> = []
            for row in threads.values.flatMap(\.rows) where !seen.insert(row.id).inserted { shared.insert(row.id) }
            return shared.sorted()
        }
    }

    /// The thread whose rows hold a user message with this text.
    func thread(saying text: String) -> String? {
        lock.withLock { order.first { threads[$0]?.rows.contains { $0.role == "user" && $0.content == text } ?? false } }
    }

    /// Requests still at the server about a thread, or a new chat's
    /// message: waiting on a turn, on a hang or on a delay.
    func exchanges(about tag: String) -> Int { lock.withLock { open[tag, default: 0] } }

    /// When each request that hangs or is delayed at the server stops
    /// waiting on the clock.
    var sleepDeadlines: [Date] { lock.withLock { Array(sleeping.values) } }

    private func sleep(for seconds: Double, _ serial: Int) async throws {
        let deadline = clock.now.addingTimeInterval(seconds)
        lock.withLock { sleeping[serial] = deadline }
        defer { lock.withLock { sleeping[serial] = nil } }
        try await clock.sleep(until: deadline)
    }

    /// Every tag a request is still at the server about, and every request
    /// not yet answered, by what it waits on.
    var openExchanges: [String] {
        lock.withLock { open.filter { $0.value != 0 }.map { "\($0.key)×\($0.value)" }.sorted() + inside.values.sorted() }
    }

    /// What the server did, by name: how far a run reached.
    var tally: [String: Int] { lock.withLock { counted } }

    private func note(_ name: String) { counted[name, default: 0] += 1 }

    /// How many requests came, and how many of them drew a fault.
    var requests: Int { lock.withLock { seen } }
    var faulted: Int { lock.withLock { faults } }
    var strayed: [String] { lock.withLock { strays } }

    /// Says where the run is, at a place it passes: a request as it reaches
    /// the server and as its answer goes back, a wait on the clock as it
    /// begins and as it ends, an id as it is minted, a stream's cleanup, a
    /// step of the driver. Off the run's thread there, the run strayed.
    var watch: @Sendable (String) -> Void {
        { [self] place in if SeededThread.current !== home { lock.withLock { strays.append(place) } } }
    }

    /// Whether a stream of the thread ever sent an event twice.
    func repeated(_ thread: String) -> Bool { lock.withLock { twice.contains(thread) } }

    /// How often a chat request with this text reached the server.
    func chatRequests(saying text: String) -> Int { lock.withLock { chats[text, default: 0] } }

    /// The chance each request from here on is faulted.
    func misbehave(_ rate: Double, with401: Bool = false) {
        lock.withLock {
            self.rate = rate
            allows401 = with401
        }
    }

    /// Whether a `401` was answered since this was last asked.
    func took401() -> Bool {
        lock.withLock {
            defer { refused401 = false }
            return refused401
        }
    }

    /// The connection a launch of the app speaks through. Opening one ends
    /// every one before it: a request a killed app still has on its way
    /// reaches nothing.
    func connect() -> Port {
        lock.withLock {
            port += 1
            return Port(server: self, number: port)
        }
    }

    /// Every running turn emits up to `events` more events, and ends when
    /// it has none left.
    func work(events: Int) {
        lock.withLock {
            for id in order {
                for _ in 0 ..< events where threads[id]?.turn != nil { step(id) }
            }
        }
    }

    /// Every running turn runs to its end.
    func finishTurns() {
        lock.withLock {
            for id in order {
                while threads[id]?.turn != nil { step(id) }
            }
        }
    }

    // MARK: Turns

    private func stamp(_ date: Date) -> String { Timestamp.format(date, precision: .milliseconds) }

    private func line(_ object: [String: Any]) -> String {
        String(decoding: (try? JSONSerialization.data(withJSONObject: object, options: [.sortedKeys])) ?? Data(), as: UTF8.self)
    }

    /// Starts a turn on a thread: its reply is fixed by the seed and the
    /// thread's place, never by when it was asked for.
    ///
    /// - Parameter resumed: nobody sent a message for it: the thread is
    ///   resumed after a resolution, or for messages a run left unread.
    ///   Below `agents` 5 such a run has no stream.
    private func start(_ id: String, resumed: Bool = false) {
        guard var thread = threads[id] else { return }
        var numbers = SeededNumbers(seed: seed ^ SeededNumbers.hash("turn|\(id)|\(thread.nextTurn)"))
        let ordinal = thread.nextTurn
        thread.nextTurn += 1
        let pieces = 1 + numbers.below(4)
        thread.turn = Turn(ordinal: ordinal, remaining: (0 ..< pieces).map(Step.piece), fails: numbers.chance(0.12))
        // Drawn after everything a turn drew before these were known, so
        // what a seed's turns say is what they said.
        if numbers.chance(0.15) { thread.turn?.restartsAfter = numbers.below(pieces + 1) }
        thread.turn?.unready = numbers.below(2)
        // Three turns in eight make a second call: a tool, then another
        // reply. One in eight asks the person, and one waits for their
        // approval.
        let more = numbers.below(8)
        if more >= 5 {
            let tool: Tool = more == 6 ? .ask : more == 7 ? .approve : .query
            thread.turn?.remaining += [.tool(tool), .call] + (0 ... numbers.below(3)).map(Step.piece)
        }
        if !planned.isEmpty {
            let plan = planned.removeFirst()
            thread.turn = Turn(ordinal: ordinal, remaining: plan.steps, fails: plan.fails)
        }
        if resumed, revision < 5 { thread.turn?.streamless = true }
        thread.status = "running"
        thread.cause = nil
        thread.finishedAt = nil
        thread.version += 1
        thread.updatedAt = clock.now
        threads[id] = thread
    }

    /// One more event of the thread's turn, or its end.
    private func step(_ id: String) {
        guard var thread = threads[id], var turn = thread.turn else { return }
        if let at = turn.restartsAfter, turn.emitted.count >= at {
            // The server process goes and comes back: every stream of the
            // turn ends with no word, and the run is taken up again with
            // none.
            turn.restartsAfter = nil
            turn.streamless = true
            for reader in turn.readers { reader.continuation.finish() }
            turn.readers = []
            thread.turn = turn
            threads[id] = thread
            note("a server restart under a running turn")
            return
        }
        guard let next = turn.remaining.first, !(turn.fails && turn.remaining.count == 1) else {
            return settle(id, status: turn.fails ? "error" : "ok")
        }
        turn.remaining.removeFirst()
        var said: [String] = []
        switch next {
        case let .piece(number):
            // From `agents` 5 a piece says where it belongs: the code
            // points of the reply before it. Below, it says nothing.
            let piece = "r\(turn.ordinal).\(number) "
            var event: [String: Any] = ["kind": "delta", "text": piece, "turn": turn.ordinal]
            if revision >= 5 { event["at"] = turn.before }
            turn.before += piece.unicodeScalars.count
            turn.written += piece
            said = [line(event)]
        case let .tool(tool):
            // The reply's row is written with its call, then the tool
            // runs: its row draws the next ordinal.
            minted += 1
            let call = "c\(minted)"
            let ordinal = thread.nextTurn
            thread.nextTurn += 1
            let (name, callable): (String, String) =
                switch tool {
                case .query: ("query", "substrate.reamde.dev/core/function/substrate.reamde.dev/core/query")
                case .ask: ("ask", "substrate.reamde.dev/core/function/substrate.reamde.dev/core/ask")
                case .approve: ("gmail_send", "substrate.reamde.dev/core/function/providers.substrate.reamde.dev/google/sendmail")
                }
            var landed: Row.Landed?
            var output = #"{"records":[],"total":0}"#
            if tool != .query {
                let card = ask(tool, thread: id, call: call)
                counted(&thread)
                landed = Row.Landed(kind: card.kind, id: card.id)
                output =
                    tool == .ask
                    ? #"{"id":"\#(card.id)"}"# : #"{"output":{"approval":"\#(card.id)","status":"waiting for the owner's approval"}}"#
            }
            minted += 1
            add(
                Row(
                    id: "a\(minted)", role: "assistant", content: turn.written, turn: turn.ordinal, createdAt: clock.now, call: call,
                    tool: name, callable: callable), to: &thread)
            add(
                Row(
                    id: "o\(minted)", role: "tool", content: output, turn: ordinal, createdAt: clock.now, call: call, tool: name,
                    callable: callable, landed: landed), to: &thread)
            thread.lastReply = turn.written
            turn.written = ""
            let event: [String: Any] = ["id": call, "tool": name, "callable": callable, "turn": ordinal]
            said = [
                line(event.merging(["kind": "toolStarted", "args": "{}"]) { first, _ in first }),
                line(event.merging(["kind": "toolFinished", "ok": true, "output": output]) { first, _ in first }),
            ]
            if tool != .query { note(tool == .ask ? "a question asked" : "an approval asked") }
        case .call:
            // The next call reads what was sent while the one before
            // worked, and says so; its reply's ordinal is past all of it.
            said = turn.queued.map { message in
                taken.insert(message)
                note("a queued message taken by the next call")
                return line(["kind": "messageTaken", "id": message, "turn": thread.rows.first { $0.id == message }?.turn ?? 0])
            }
            turn.queued = []
            turn.ordinal = thread.nextTurn
            thread.nextTurn += 1
            turn.before = 0
        }
        turn.emitted += said
        thread.turn = turn
        threads[id] = thread
        for text in said { emit(id, text) }
    }

    /// Puts a row among a thread's, which are kept in the order of their
    /// ordinals: a message sent while a reply is written is stored before
    /// that reply's row, and stands after it.
    private func add(_ row: Row, to thread: inout Thread) {
        thread.rows.insert(row, at: thread.rows.firstIndex { $0.turn > row.turn } ?? thread.rows.count)
    }

    /// A tool call lands the record that waits on the person: questions
    /// with their options, or a call to approve with what it would run
    /// with. All of it follows from the seed and the call.
    private func ask(_ tool: Tool, thread: String, call: String) -> Card {
        var numbers = SeededNumbers(seed: seed ^ SeededNumbers.hash("card|\(thread)|\(call)"))
        minted += 1
        var card = Card(id: "\(tool == .ask ? "i" : "p")\(minted)", kind: tool == .ask ? Card.interaction : Card.approval, thread: thread, createdAt: clock.now)
        if tool == .ask {
            // Mostly one question of two or three options: what a
            // notification can answer. Some have more of either.
            let questions = numbers.below(4) == 0 ? 2 : 1
            card.questions = (1 ... questions).map { number in
                Card.Question(
                    id: "q\(number)", prompt: "Question \(number) of \(card.id)?",
                    options: (0 ..< 2 + numbers.below(3)).map { "o\($0)" }, multi: numbers.below(6) == 0)
            }
        } else {
            card.tool = "gmail_send"
            card.arguments = ["to": "ada@example.com", "subject": "Subject \(card.id)"]
            // Some say more than a notification can show whole.
            if numbers.below(4) == 0 { card.arguments["body"] = String(repeating: "A long body. ", count: 40) }
        }
        cards[card.id] = card
        cardOrder.append(card.id)
        tell(card.kind, card.id)
        return card
    }

    /// Called under the lock: the one write that resolves a card, over the
    /// version read. It writes the resolution's `system` row into the
    /// thread, tells the change feed, and resumes the thread where nothing
    /// runs on it and it was not stopped.
    private func resolve(_ id: String, to state: String, answers: [String: [String]], ifVersion: Int?, by person: String = "me") -> Outcome {
        guard var card = cards[id] else { return refuse(404, "not_found", "substrate: not found: \(id)") }
        // Only a message that names the questions marks them replied.
        if state == "replied" { return refuse(403, "forbidden", "substrate: only a message replies to \(id)") }
        if let ifVersion, ifVersion != card.version {
            return refuse(409, "conflict", "substrate: version conflict: \(id) is at version \(card.version), not \(ifVersion)")
        }
        guard card.pending else { return refuse(409, "conflict", "substrate: version conflict: \(id) is no longer pending") }
        let allowed = card.kind == Card.approval ? ["approved", "denied"] : ["answered", "dismissed"]
        guard allowed.contains(state) else { return refuse(422, "validation", "\(id) has no transition from pending to \(state)") }
        if state == "answered" {
            for question in card.questions {
                let chosen = answers[question.id] ?? []
                let fits = question.multi ? !chosen.isEmpty : chosen.count == 1
                guard fits, chosen.allSatisfy(question.options.contains) else {
                    return refuse(422, "validation", "question \(question.id) of \(id) is not answered with its options")
                }
            }
            card.answers = answers
        }
        // An approved call runs at once here, and works.
        card.state = state == "approved" ? "done" : state
        card.version += 1
        card.resolvedAt = instant()
        card.resolutions += 1
        // Who resolved it is linked from `agents` 7.
        if revision >= 7 { card.by = person }
        cards[id] = card
        tell(card.kind, id)
        guard var thread = threads[card.thread] else { return .json(200, line(record(card))) }
        var envelope: [String: Any]
        if card.kind == Card.approval {
            envelope = ["event": "approvalDecided", "approval": "\(Card.approval)/\(id)", "decision": state, "tool": card.tool]
            if state == "approved" { envelope["ok"] = true }
        } else {
            envelope = ["event": state == "answered" ? "interactionAnswered" : "interactionDismissed", "interaction": "\(Card.interaction)/\(id)"]
            if state == "answered" {
                envelope["answers"] = card.questions.map { ["question": $0.id, "selected": answers[$0.id] ?? []] as [String: Any] }
            }
        }
        minted += 1
        add(Row(id: "s\(minted)", role: "system", content: line(envelope), turn: thread.nextTurn, createdAt: clock.now), to: &thread)
        thread.nextTurn += 1
        counted(&thread)
        if thread.turn != nil {
            // A turn runs: it is resumed for this once it ends on its own.
            thread.turn?.resolutions += 1
            threads[card.thread] = thread
        } else {
            threads[card.thread] = thread
            // A stopped thread holds until its person writes again.
            if thread.status != "stopped" {
                note("a thread resumed by a resolution")
                cards[id]?.resumes += 1
                start(card.thread, resumed: true)
            }
        }
        return .json(200, line(record(cards[id] ?? card)))
    }

    private func record(_ card: Card) -> [String: Any] {
        var properties: [String: Any] = ["thread": ["ref": "substrate.reamde.dev/llm/thread/\(card.thread)"], "state": card.state]
        if card.kind == Card.approval {
            properties["agent"] = ["ref": "substrate.reamde.dev/core/agent/\(agent)"]
            properties["tool"] = card.tool
            properties["arguments"] = card.arguments
            if let at = card.resolvedAt { properties["decidedAt"] = stamp(at) }
            if let by = card.by { properties["decidedBy"] = ["ref": "substrate.reamde.dev/core/actor/\(by)"] }
        } else {
            properties["questions"] = card.questions.map { question in
                [
                    "id": question.id, "prompt": question.prompt, "multi": question.multi,
                    "options": question.options.map { ["value": $0, "label": "Option \($0)"] },
                ] as [String: Any]
            }
            if !card.answers.isEmpty {
                properties["answers"] = card.questions.map { ["question": $0.id, "selected": card.answers[$0.id] ?? []] as [String: Any] }
            }
            if let at = card.resolvedAt { properties["resolvedAt"] = stamp(at) }
            if let by = card.by { properties["resolvedBy"] = ["ref": "substrate.reamde.dev/core/actor/\(by)"] }
            if let message = card.repliedIn { properties["repliedIn"] = ["ref": "substrate.reamde.dev/llm/message/\(message)"] }
        }
        return [
            "id": card.id, "kind": card.kind, "version": card.version, "createdAt": stamp(card.createdAt),
            "updatedAt": stamp(card.resolvedAt ?? card.createdAt), "properties": properties,
        ]
    }

    /// Ends the thread's turn: the reply so far is its row, the thread
    /// says how it ended, and every reader hears it and is let go.
    private func settle(_ id: String, status: String) {
        guard var thread = threads[id], let turn = thread.turn else { return }
        if !turn.written.isEmpty {
            minted += 1
            add(Row(id: "a\(minted)", role: "assistant", content: turn.written, turn: turn.ordinal, createdAt: clock.now), to: &thread)
            thread.lastReply = turn.written
        }
        thread.status = status
        thread.cause = status == "error" ? "provider_overloaded" : status == "stopped" ? "requested" : nil
        thread.version += 1
        thread.updatedAt = clock.now
        thread.finishedAt = instant()
        thread.turn = nil
        // The newest row, passing over a message of the person's that no
        // reply came after (#1419): after a call that wrote no reply it
        // still names the reply before, and names the message only in a
        // chat that holds nothing else. It never moves back.
        let named =
            thread.rows.lastIndex { $0.role == "assistant" || $0.role == "tool" } ?? (thread.lastMessage == nil ? thread.rows.indices.last : nil)
        let held = thread.lastMessage.flatMap { id in thread.rows.firstIndex { $0.id == id } }
        if let named, held.map({ named > $0 }) ?? true { thread.lastMessage = thread.rows[named].id }
        threads[id] = thread
        let last: String
        if status == "error" {
            last = line(["kind": "error", "error": Self.said[3], "cause": "provider_overloaded"])
        } else {
            var result: [String: Any] = ["reply": turn.written, "thread": id, "status": status, "turns": 1]
            if status == "stopped" { result["cause"] = "requested" }
            last = line(["kind": "done", "result": result])
        }
        for reader in turn.readers {
            var reader = reader
            if deliver(last, to: &reader) { reader.continuation.finish() }
        }
        // A run that ended on its own starts the turn of the messages it
        // never read, which opens by saying each one taken; or is resumed
        // for a resolution that landed while it ran. One that was stopped
        // or that failed starts nothing: the messages stay in the thread.
        guard status == "ok" else {
            if !turn.queued.isEmpty { note("a run ended by a stop or a failure with a message unread") }
            return
        }
        if !turn.queued.isEmpty {
            note("a turn started by the messages a run left unread")
            start(id, resumed: true)
            let said = turn.queued.map { message in
                taken.insert(message)
                return line(["kind": "messageTaken", "id": message, "turn": thread.rows.first { $0.id == message }?.turn ?? 0])
            }
            threads[id]?.turn?.emitted = said
        } else if turn.resolutions > 0 {
            note("a thread resumed by a resolution that landed mid-turn")
            start(id, resumed: true)
        }
    }

    /// Sends a turn's event to everyone reading it.
    private func emit(_ id: String, _ text: String) {
        guard var turn = threads[id]?.turn else { return }
        var kept: [Reader] = []
        for reader in turn.readers {
            var reader = reader
            if deliver(text, to: &reader) { kept.append(reader) }
        }
        turn.readers = kept
        threads[id]?.turn = turn
    }

    /// One line to one reader, with what its stream does wrong; false once
    /// the stream is over.
    private func deliver(_ text: String, to reader: inout Reader) -> Bool {
        if let cut = reader.fault.cutAfter, reader.delivered >= cut {
            if reader.fault.lagged { reader.continuation.yield(line(["kind": "lagged"])) }
            reader.continuation.finish(throwing: reader.fault.loud ? URLError(.networkConnectionLost) : nil)
            return false
        }
        reader.continuation.yield(text)
        if reader.fault.duplicate == reader.delivered {
            reader.continuation.yield(text)
            twice.insert(reader.thread)
        }
        reader.delivered += 1
        return true
    }

    // MARK: Requests

    /// A launch's connection to the server.
    struct Port: HTTPTransport {
        fileprivate let server: SeededServer
        fileprivate let number: Int

        func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
            let stream = try await lines(for: request)
            return (try await stream.body(limit: .max), stream.head)
        }

        func lines(for request: URLRequest) async throws -> LineStream {
            server.watch("a request reaching the server")
            defer { server.watch("a request's answer going back") }
            return try await server.answer(request, port: number)
        }
    }

    private enum Call {
        case discovery
        case list(kind: String, filter: [String: Any], query: [String: String])
        case thread(String)
        case message(String)
        case chat(agent: String, body: [String: Any])
        case events(String)
        case stop(String)
        case resume(String)
        case watch(kinds: Set<String>, from: Int?)
        case mark([String: Any])
        /// A question's or an approval's record, read.
        case card(String)
        /// The write that resolves one.
        case decide(String, body: [String: Any])
        case other

        var writes: Bool {
            switch self {
            case .chat, .stop, .resume, .mark, .decide: true
            default: false
            }
        }

        var streams: Bool {
            switch self {
            case .chat, .events, .resume: true
            default: false
            }
        }
    }

    private func call(_ request: URLRequest) -> (Call, signature: String, tag: String?) {
        let url = request.url!
        let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        let path = components?.percentEncodedPath ?? url.path
        let method = request.httpMethod ?? "GET"
        var query: [String: String] = [:]
        for item in components?.queryItems ?? [] { query[item.name] = item.value ?? "" }
        func after(_ marker: String) -> String? {
            guard let range = path.range(of: marker) else { return nil }
            return String(path[range.upperBound...]).removingPercentEncoding
        }
        if path.hasSuffix("/.well-known/substrate/server.json") { return (.discovery, "discovery", nil) }
        if path.hasSuffix("/api/v1/changes") {
            let items = components?.queryItems ?? []
            let kinds = Set(items.filter { $0.name == "kinds" }.flatMap { ($0.value ?? "").split(separator: ",").map(String.init) })
            return (.watch(kinds: kinds, from: query["from"].flatMap(Int.init)), "watch", nil)
        }
        if method == "POST", path.hasSuffix("/api/v1/records") {
            let body = (request.httpBody.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [String: Any]) ?? [:]
            let properties = body["properties"] as? [String: Any] ?? [:]
            return (.mark(body), "mark|\(properties["thread"] as? String ?? "")", nil)
        }
        if method == "GET", path.hasSuffix("/api/v1/records") {
            let filter = (query["filter"].flatMap { try? JSONSerialization.jsonObject(with: Data($0.utf8)) } as? [String: Any]) ?? [:]
            let kind = (filter["kinds"] as? [String])?.first ?? ""
            let properties = filter["properties"] as? [String: Any] ?? [:]
            let thread = (properties["thread"] as? [String: Any])?["eq"] as? String
            return (.list(kind: kind, filter: filter, query: query), "list|\(kind)|\(thread ?? "")", thread)
        }
        if method == "POST", path.hasSuffix("/chat"), let agent = after("/core/agent/") {
            let body = (request.httpBody.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [String: Any]) ?? [:]
            let thread = body["thread"] as? String ?? ""
            let text = body["message"] as? String ?? ""
            return (.chat(agent: String(agent.dropLast("/chat".count)), body: body), "chat|\(thread)|\(text)", thread.isEmpty ? "new:\(text)" : thread)
        }
        if let rest = after("/llm/thread/") {
            let parts = rest.split(separator: "/").map(String.init)
            guard let id = parts.first else { return (.other, "other", nil) }
            switch (method, parts.dropFirst().first) {
            case ("GET", nil): return (.thread(id), "thread|\(id)", id)
            case ("GET", "events"?): return (.events(id), "events|\(id)", id)
            case ("POST", "stop"?): return (.stop(id), "stop|\(id)", id)
            case ("POST", "continue"?): return (.resume(id), "continue|\(id)", id)
            default: return (.other, "other", nil)
            }
        }
        if method == "GET", let id = after("/llm/message/") { return (.message(id), "message|\(id)", "message:\(id)") }
        if let id = after("/llm/interaction/") ?? after("/llm/approval/") {
            if method == "GET" { return (.card(id), "card|\(id)", nil) }
            if method == "PATCH" {
                let body = (request.httpBody.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [String: Any]) ?? [:]
                let properties = body["properties"] as? [String: Any] ?? [:]
                return (.decide(id, body: body), "decide|\(id)|\(properties["state"] as? String ?? "")", nil)
            }
        }
        return (.other, "other|\(method)|\(path)", nil)
    }

    /// The fault this request draws: fixed by the seed, the request's
    /// signature and how many like it came before.
    private func fault(for call: Call, signature: String) -> (Fault, lands: Bool) {
        let count = counts[signature, default: 0]
        counts[signature] = count + 1
        if let index = forced.firstIndex(where: { signature.hasPrefix($0.prefix) }) {
            let chosen = forced.remove(at: index)
            return (chosen.fault, chosen.lands)
        }
        guard rate > 0 else { return (.none, true) }
        var numbers = SeededNumbers(seed: seed ^ SeededNumbers.hash("\(signature)#\(count)"))
        // A send, a continue and an attach are what a chat is made of, and
        // a few among many reads: they meet trouble half again as often.
        guard numbers.chance(call.streams ? min(0.9, rate * 1.5) : rate) else { return (.none, true) }
        if case .watch = call { return (.none, true) }
        var faults: [Fault] = [
            .status(500, code: "internal", retryAfter: nil), .status(503, code: "unavailable", retryAfter: "3"), .gateway,
            .status(429, code: "rate_limited", retryAfter: "2"), .status(429, code: "rate_limited", retryAfter: nil), .hang,
            .unreachable, .dropped, .page, .delay(Double(1 + numbers.below(12))), .delay(0.2),
        ]
        if allows401 { faults.append(.unauthorized) }
        if call.streams {
            // Half of a stream's faults are a stream's own: it opens, and
            // then goes wrong.
            let at = numbers.below(5)
            let streams: [Fault] = [
                .stream(StreamFault(cutAfter: 0)), .stream(StreamFault(cutAfter: at, loud: true)), .stream(StreamFault(cutAfter: at)),
                .stream(StreamFault(duplicate: at)), .stream(StreamFault(cutAfter: 1 + at, loud: true, duplicate: at)),
            ]
            // A reader that fell behind is cut by the server itself, which
            // from `agents` 5 says so.
            let lagged: [Fault] = revision >= 5 ? [.stream(StreamFault(cutAfter: 1 + at, lagged: true))] : []
            if numbers.chance(0.5) { faults = streams + lagged } else { faults += streams + lagged }
        }
        let fault = numbers.pick(faults)
        self.faults += 1
        switch fault {
        case .none: break
        case let .status(status, _, retryAfter): note("fault: \(status)\(retryAfter == nil ? "" : " with Retry-After")")
        case .gateway: note("fault: a gateway's 502 page")
        case .unauthorized: note("fault: 401")
        case .hang: note("fault: a hang past the timeout")
        case .unreachable: note("fault: never reached the server")
        case .dropped: note("fault: the connection dropped")
        case .page: note("fault: a page of HTML")
        case .delay: note("fault: a delay")
        case let .stream(broken):
            if broken.lagged {
                note("fault: a reader cut off as lagged")
            } else if broken.cutAfter == 0 {
                note("fault: closed before the thread event")
            } else if broken.cutAfter != nil {
                note("fault: a stream cut at an event")
            }
            if broken.duplicate != nil { note("fault: an event sent twice") }
        }
        // The server's own refusals took nothing; anything else may have
        // lost only the answer.
        let lands: Bool
        switch fault {
        case .none, .delay, .stream: lands = true
        case .unreachable, .unauthorized, .status(429, _, _): lands = false
        case .status, .gateway, .hang, .dropped, .page: lands = numbers.chance(0.5)
        }
        return (fault, lands)
    }

    private func enter(_ tag: String?) { if let tag { lock.withLock { open[tag, default: 0] += 1 } } }
    private func leave(_ tag: String?) { if let tag { lock.withLock { open[tag, default: 0] -= 1 } } }

    fileprivate func answer(_ request: URLRequest, port number: Int) async throws -> LineStream {
        let (call, signature, tag) = call(request)
        let drawn: (Fault, lands: Bool)? = lock.withLock {
            guard number == port else { return nil }
            seen += 1
            if case let .chat(_, body) = call, let text = body["message"] as? String { chats[text, default: 0] += 1 }
            if case let .mark(body) = call, let properties = body["properties"] as? [String: Any] {
                written.append(
                    MarkWrite(
                        thread: properties["thread"] as? String ?? "", readThrough: properties["readThrough"] as? String,
                        markedUnread: properties["markedUnread"] as? Bool, at: clock.now, pinned: properties["pinned"] as? Bool,
                        archived: properties["archived"] as? Bool))
            }
            return fault(for: call, signature: signature)
        }
        // A launch that was killed: what it still had on its way is lost.
        guard let (fault, lands) = drawn else { throw CancellationError() }
        let url = request.url!
        let serial = lock.withLock { () -> Int in
            inside[seen] = "\(signature) \(fault)"
            return seen
        }
        enter(tag)
        var entered = true
        defer {
            if entered { leave(tag) }
            lock.withLock { inside[serial] = nil }
        }
        func alive() throws { if lock.withLock({ number != port }) { throw CancellationError() } }

        switch fault {
        case .unreachable:
            throw URLError(.cannotConnectToHost)
        case let .delay(seconds):
            try await sleep(for: seconds, serial)
            try alive()
        default:
            break
        }
        // What the request does at the server, whatever becomes of its answer.
        var outcome: Outcome?
        if lands || !call.writes {
            outcome = lock.withLock {
                let done = perform(call, url: url)
                // A write the server took whose answer the fault then eats.
                if case .mark = call, case .json(200 ... 201, _) = done {
                    switch fault {
                    case .status, .gateway, .hang, .dropped, .page: note("a mark taken and its answer lost")
                    default: break
                    }
                }
                if case .decide = call, case .json(200, _) = done {
                    switch fault {
                    case .status, .gateway, .hang, .dropped, .page: note("a card's write taken and its answer lost")
                    default: break
                    }
                }
                if call.writes, case .stream = done {
                    switch fault {
                    case .status, .gateway, .hang, .dropped, .page: note("a write taken and its answer lost")
                    case let .stream(broken) where broken.cutAfter != nil: note("a write taken and its stream cut")
                    default: break
                    }
                }
                return done
            }
        }
        switch fault {
        case .none, .delay, .unreachable:
            break
        case let .stream(broken):
            // Only a stream that opened can go wrong as one.
            if case let .stream(thread, opening, _)? = outcome { outcome = .stream(thread: thread, opening: opening, fault: broken) }
        case let .status(status, code, retryAfter):
            return json(status, #"{"error":{"code":"\#(code)","message":"\#(Self.said[0])"}}"#, url: url, retryAfter: retryAfter)
        case .gateway:
            return page(502, url: url)
        case .unauthorized:
            lock.withLock { refused401 = true }
            return json(401, #"{"error":{"code":"unauthorized","message":"\#(Self.said[1])"}}"#, url: url)
        case .page:
            return page(200, url: url)
        case .dropped:
            throw URLError(.networkConnectionLost)
        case .hang:
            try await sleep(for: max(1, request.timeoutInterval), serial)
            throw URLError(.timedOut)
        }
        switch outcome ?? .json(500, "{}") {
        case let .json(status, body):
            return json(status, body, url: url)
        case let .watching(kinds, from):
            // The change feed: its bookmark, what a watch resumed from a
            // cursor missed, then each mark and each card as it moves, and
            // each thread as what waits on a person in it moves, of the
            // kinds the watch names, for as long as the app listens.
            // Nothing else is told on it: the app hears of a thread's runs
            // from its reads.
            return ndjson(url: url) { [self] continuation in
                lock.withLock {
                    continuation.yield(line(["bookmark": from ?? feedSeq, "generation": "g"]))
                    // The server refuses, whole, a watch that names a kind
                    // it does not declare.
                    if kinds.contains("substrate.reamde.dev/llm/threadmark"), !keepsMarks {
                        continuation.yield(#"{"error":{"code":"validation","message":"unknown type \"substrate.reamde.dev/llm/threadmark\""}}"#)
                        continuation.finish()
                        return nil as Int?
                    }
                    if let from {
                        for row in feed where row.seq > from && kinds.contains(row.kind) { continuation.yield(row.line) }
                    }
                    readers += 1
                    watchers[readers] = (number, kinds, continuation)
                    return readers
                }
            } ended: { [self] reader in
                Task.detached { [self] in
                    watch("a stream's cleanup")
                    lock.withLock { if let reader { watchers[reader] = nil } }
                }
            }
        case let .stream(thread, opening, broken):
            entered = false
            return ndjson(url: url) { [self] continuation in
                lock.withLock {
                    readers += 1
                    var reader = Reader(id: readers, continuation: continuation, fault: broken, thread: thread)
                    for text in opening {
                        guard deliver(text, to: &reader) else { return nil as Int? }
                    }
                    // What the turn emitted before this reader came: an
                    // attach's replay, from the turn's start.
                    // A turn this process has no stream of gives none: the
                    // opening said so.
                    guard var turn = threads[thread]?.turn, !turn.streamless else {
                        continuation.finish()
                        return nil
                    }
                    for text in turn.emitted {
                        guard deliver(text, to: &reader) else { return nil }
                    }
                    turn.readers.append(reader)
                    threads[thread]?.turn = turn
                    return reader.id
                }
            } ended: { [self] reader in
                // Never under the stream's own lock, which a cancelled
                // reader holds while it runs this and a line on its way to
                // that reader waits for: the reader is let go a moment later.
                Task.detached { [self] in
                    watch("a stream's cleanup")
                    lock.withLock {
                        if let reader { threads[thread]?.turn?.readers.removeAll { $0.id == reader } }
                        if let tag { open[tag, default: 0] -= 1 }
                    }
                }
            }
        }
    }

    private func json(_ status: Int, _ body: String, url: URL, retryAfter: String? = nil) -> LineStream {
        var headers = ["Content-Type": "application/json"]
        if let retryAfter { headers["Retry-After"] = retryAfter }
        return LineStream(head: HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: headers)!, body: Data(body.utf8))
    }

    private func page(_ status: Int, url: URL) -> LineStream {
        let head = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "text/html"])!
        return LineStream(head: head, body: Data("<!doctype html>\n<html><body><h1>Sign in to this network</h1></body></html>\n".utf8))
    }

    private func ndjson(
        url: URL, _ begin: @escaping @Sendable (AsyncThrowingStream<String, any Error>.Continuation) -> Int?,
        ended: (@Sendable (Int?) -> Void)? = nil
    ) -> LineStream {
        let head = HTTPURLResponse(
            url: url, statusCode: 200, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/x-ndjson"])!
        let stream = AsyncThrowingStream<String, any Error> { continuation in
            // Set before the first line: a stream cut at once ends inside
            // `begin`, and a handler set after that is never called.
            let reader = Held()
            continuation.onTermination = { _ in ended?(reader.id) }
            reader.id = begin(continuation)
        }
        return LineStream(head: head, lines: stream)
    }

    /// The reader a stream was given, once it has one.
    private final class Held: @unchecked Sendable {
        private let lock = NSLock()
        private var value: Int?

        var id: Int? {
            get { lock.withLock { value } }
            set { lock.withLock { value = newValue } }
        }
    }

    // MARK: What a correct server does with each request

    private enum Outcome {
        case json(Int, String)
        /// The stream's first lines, then the thread's running turn.
        case stream(thread: String, opening: [String], fault: StreamFault)
        case watching(kinds: Set<String>, from: Int?)
    }

    private func refuse(_ status: Int, _ code: String, reason: String? = nil, _ message: String) -> Outcome {
        var payload: [String: Any] = ["code": code, "message": message]
        if let reason { payload["reason"] = reason }
        return .json(status, line(["error": payload]))
    }

    private func record(_ thread: Thread) -> [String: Any] {
        var properties: [String: Any] = [
            "mode": "chat", "status": thread.status, "agent": ["ref": "substrate.reamde.dev/core/agent/\(thread.agent)"],
            "startedAt": stamp(thread.startedAt),
        ]
        if let finished = thread.finishedAt { properties["finishedAt"] = stamp(finished) }
        if let reply = thread.lastReply { properties["lastReply"] = reply }
        if let cause = thread.cause {
            properties["cause"] = cause
            properties["reason"] = "\(Self.said[2]): \(cause)"
        }
        if keepsMarks, let last = thread.lastMessage { properties["lastMessage"] = ["ref": "substrate.reamde.dev/llm/message/\(last)"] }
        // What waits on a person, counted where the server keeps marks.
        if keepsMarks, waits(thread) > 0 { properties["waitingOnPerson"] = waits(thread) }
        return [
            "id": thread.id, "kind": "substrate.reamde.dev/llm/thread", "version": thread.version, "createdAt": stamp(thread.startedAt),
            "updatedAt": stamp(thread.updatedAt), "properties": properties,
        ]
    }

    private func record(_ row: Row, thread: String) -> [String: Any] {
        var properties: [String: Any] = [
            "role": row.role, "content": row.content, "turn": row.turn, "thread": ["ref": "substrate.reamde.dev/llm/thread/\(thread)"],
        ]
        if let call = row.call, let tool = row.tool {
            if row.role == "assistant" {
                properties["toolCalls"] = [["id": call, "name": tool, "callable": row.callable ?? "", "arguments": "{}"] as [String: Any]]
            } else {
                properties["toolCallId"] = call
                properties["name"] = tool
                properties["callable"] = row.callable ?? ""
                properties["ok"] = true
                if let landed = row.landed { properties["changes"] = [["seq": 1, "op": "put", "kind": landed.kind, "id": landed.id] as [String: Any]] }
            }
        }
        return [
            "id": row.id, "kind": "substrate.reamde.dev/llm/message", "version": 1, "createdAt": stamp(row.createdAt),
            "updatedAt": stamp(row.createdAt), "properties": properties,
        ]
    }

    private func page(_ records: [[String: Any]]) -> Outcome {
        // The feed's place as of this read: a watch opened from it is told
        // what moved since.
        .json(200, line(["head": feedSeq, "generation": "g", "records": records]))
    }

    private func perform(_ call: Call, url: URL) -> Outcome {
        switch call {
        case .discovery:
            return .json(
                200,
                line([
                    "server": ["version": "seeded"],
                    "features": [
                        ["name": "agents", "stability": "experimental", "revision": revision],
                        ["name": "records", "stability": "stable", "revision": records],
                    ],
                ]))
        case let .watch(kinds, from):
            return .watching(kinds: kinds, from: from)
        case let .mark(body):
            let kind = body["kind"] as? String ?? ""
            guard keepsMarks, kind == "substrate.reamde.dev/llm/threadmark" else { return refuse(404, "not_found", "unknown kind \(kind)") }
            let properties = body["properties"] as? [String: Any] ?? [:]
            let thread = properties["thread"] as? String ?? ""
            guard let held = threads[thread] else { return refuse(404, "not_found", "substrate: not found: thread \(thread)") }
            let read = properties["readThrough"] as? String
            if let read, !held.rows.contains(where: { $0.id == read }) {
                return refuse(422, "validation", "readThrough names no message of thread \(thread)")
            }
            let (pinned, archived) = (properties["pinned"] as? Bool, properties["archived"] as? Bool)
            if !keepsFlags, pinned != nil || archived != nil {
                return refuse(422, "validation", #"substrate: validation failed: unknown property "pinned" or "archived""#)
            }
            let created = marks[thread] == nil
            if put(thread, readThrough: read, markedUnread: properties["markedUnread"] as? Bool, pinned: pinned, archived: archived) {
                note(pinned != nil || archived != nil ? "a flag moved by the app" : "a mark moved by the app")
            }
            return .json(created ? 201 : 200, line(record(marks[thread] ?? Mark(), thread: thread)))
        case let .thread(id):
            guard let thread = threads[id] else { return refuse(404, "not_found", "substrate: not found: thread \(id)") }
            return .json(200, line(record(thread)))
        case let .message(id):
            for thread in order {
                if let row = threads[thread]?.rows.first(where: { $0.id == id }) { return .json(200, line(record(row, thread: thread))) }
            }
            return refuse(404, "not_found", "substrate: not found: message \(id)")
        case let .card(id):
            guard let card = cards[id] else { return refuse(404, "not_found", "substrate: not found: \(id)") }
            return .json(200, line(record(card)))
        case let .decide(id, body):
            let properties = body["properties"] as? [String: Any] ?? [:]
            var answers: [String: [String]] = [:]
            for answer in properties["answers"] as? [[String: Any]] ?? [] {
                if let question = answer["question"] as? String { answers[question] = answer["selected"] as? [String] ?? [] }
            }
            if properties["state"] as? String == "dismissed" { cards[id]?.dismissals += 1 }
            let done = resolve(id, to: properties["state"] as? String ?? "", answers: answers, ifVersion: body["ifVersion"] as? Int)
            if case .json(200, _) = done { note("a card resolved by the app") }
            return done
        case let .list(kind, filter, query):
            return list(kind, filter, query)
        case let .chat(agent, body):
            return chat(agent, body)
        case let .events(id):
            guard let thread = threads[id] else { return refuse(404, "not_found", "substrate: not found: thread \(id)") }
            guard let turn = thread.turn else {
                return .stream(thread: id, opening: [line(["kind": "idle", "thread": id, "status": thread.status])], fault: .none)
            }
            if turn.streamless {
                // A run taken up again after a restart: from `agents` 5 it
                // streams, once the server has given it a stream, and the
                // replay is the turn from its start; below, an attach is
                // told it runs for as long as it does.
                if revision >= 5, turn.unready == 0 {
                    threads[id]?.turn?.streamless = false
                    note("a resumed run joined by its stream")
                    return .stream(thread: id, opening: [], fault: .none)
                }
                if revision >= 5 { threads[id]?.turn?.unready -= 1 }
                note("an attach told its run has no stream")
                return .stream(thread: id, opening: [line(["kind": "idle", "thread": id, "status": "running"])], fault: .none)
            }
            return .stream(thread: id, opening: [], fault: .none)
        case let .stop(id):
            guard threads[id] != nil else { return refuse(404, "not_found", "substrate: not found: thread \(id)") }
            guard threads[id]?.turn != nil else { return refuse(409, "conflict", "substrate: thread \(id) has no running chat turn") }
            note("a stop")
            settle(id, status: "stopped")
            return .json(200, line(["thread": id, "status": "stopped"]))
        case let .resume(id):
            guard revision >= 3 else { return refuse(404, "not_found", "no such API path: \(url.path)") }
            guard let thread = threads[id] else { return refuse(404, "not_found", "substrate: not found: thread \(id)") }
            if thread.turn != nil { return refuse(409, "conflict", reason: "thread_busy", "a turn is running on the thread") }
            // From `agents` 6 a finished thread whose newest message no
            // reply came after is taken up: a message a run left unread.
            let unanswered = revision >= 6 && thread.rows.last(where: \.spoken)?.role == "user"
            if thread.status == "ok", !unanswered {
                return refuse(409, "conflict", reason: "nothing_to_continue", "the thread's last turn finished")
            }
            if let capped = capped() { return capped }
            note("a continue")
            start(id)
            return .stream(thread: id, opening: [line(["kind": "thread", "thread": id])], fault: .none)
        case .other:
            return refuse(404, "not_found", "no such API path: \(url.path)")
        }
    }

    /// Called under the lock, in the write of a message that names the
    /// questions it answers: they are marked replied in it, with no row of
    /// their own in the thread and no resume, so the turn that reads the
    /// message is the only one. Resolved meanwhile, they are left as they
    /// are, and the message is an ordinary one.
    private func replied(_ id: String, in message: String, _ thread: inout Thread) {
        guard var card = cards[id], card.pending else {
            note("a reply in words to questions resolved meanwhile")
            return
        }
        card.state = "replied"
        card.repliedIn = message
        card.version += 1
        card.resolvedAt = instant()
        card.resolutions += 1
        card.by = "me"
        cards[id] = card
        tell(card.kind, id)
        counted(&thread)
        note("questions replied to in a message")
    }

    /// A person runs four chat turns at once.
    private func capped() -> Outcome? {
        threads.values.filter { $0.turn != nil }.count >= 4 ? refuse(409, "conflict", "substrate: 4 chat turns are running: wait for one to settle") : nil
    }

    private func chat(_ agent: String, _ body: [String: Any]) -> Outcome {
        let named = body["thread"] as? String ?? ""
        let text = body["message"] as? String ?? ""
        let id = body["messageId"] as? String
        if id != nil, revision < 3 { return refuse(400, "bad_request", #"unknown field "messageId""#) }
        // A message that answers questions in the person's own words
        // names them (`agents` 7): a server below refuses the field.
        let repliesTo = body["repliesTo"] as? String
        if repliesTo != nil, revision < 7 { return refuse(400, "bad_request", #"unknown field "repliesTo""#) }
        guard agent == self.agent else { return refuse(404, "not_found", "substrate: not found: agent \(agent)") }
        if let repliesTo {
            guard !named.isEmpty, id != nil else { return refuse(422, "validation", "repliesTo needs thread and messageId") }
            guard let card = cards[repliesTo], card.kind == Card.interaction, card.thread == named else {
                return refuse(422, "validation", "repliesTo names no pending interaction of the thread")
            }
        }
        if let id {
            // The id's record, wherever it is: this message, or another.
            for thread in order {
                guard let row = threads[thread]?.rows.first(where: { $0.id == id }) else { continue }
                let same = row.role == "user" && row.content == text && (named.isEmpty ? row.turn == 0 : named == thread)
                guard same else {
                    note("an id refused as taken")
                    // A server at `errors` 6 names the reason and one
                    // before it does not: by the seed, so the app's reading
                    // of the reason and of the words are both run.
                    return refuse(
                        409, "conflict", reason: seed % 2 == 0 ? "message_id_taken" : nil,
                        "substrate: version conflict: the messageId is taken: messageId \"\(id)\": send the message under a new id")
                }
                // Nothing written, nothing run: the `thread` event, then
                // what an attach answers.
                note("a repeat answered by its id")
                var opening = [line(["kind": "thread", "thread": thread])]
                if threads[thread]?.turn == nil {
                    opening.append(line(["kind": "idle", "thread": thread, "status": threads[thread]?.status ?? "ok"]))
                } else if threads[thread]?.turn?.streamless == true {
                    opening.append(line(["kind": "idle", "thread": thread, "status": "running"]))
                }
                return .stream(thread: thread, opening: opening, fault: .none)
            }
        }
        guard !text.isEmpty else { return refuse(422, "validation", "a chat turn needs a message") }
        var thread: Thread
        if named.isEmpty {
            if let capped = capped() { return capped }
            minted += 1
            thread = Thread(id: "t\(minted)", agent: agent, startedAt: clock.now, updatedAt: clock.now)
            order.append(thread.id)
        } else {
            guard var held = threads[named] else { return refuse(404, "not_found", "substrate: not found: thread \(named)") }
            if held.turn != nil {
                // A message under its id joins the turn (`agents` 6): its
                // row is written at once, every stream of the turn hears of
                // it, and the request is answered with the turn's stream.
                guard revision >= 6, let id else {
                    note("a send refused: thread_busy")
                    return refuse(409, "conflict", reason: "thread_busy", "a turn is running on the thread")
                }
                note("a message taken into a running turn")
                let ordinal = held.nextTurn
                held.nextTurn += 1
                add(Row(id: id, role: "user", content: text, turn: ordinal, createdAt: clock.now), to: &held)
                held.turn?.queued.append(id)
                joined.insert(id)
                let said = line(["kind": "messageQueued", "id": id, "turn": ordinal, "text": text])
                held.turn?.emitted.append(said)
                if let repliesTo { replied(repliesTo, in: id, &held) }
                threads[named] = held
                emit(named, said)
                var opening = [line(["kind": "thread", "thread": named])]
                if held.turn?.streamless == true { opening.append(line(["kind": "idle", "thread": named, "status": "running"])) }
                return .stream(thread: named, opening: opening, fault: .none)
            }
            if let capped = capped() { return capped }
            thread = held
        }
        minted += 1
        note(id == nil ? "a message written without an id" : "a message written under its id")
        add(Row(id: id ?? "u\(minted)", role: "user", content: text, turn: thread.nextTurn, createdAt: clock.now), to: &thread)
        thread.nextTurn += 1
        if let repliesTo, let id { replied(repliesTo, in: id, &thread) }
        threads[thread.id] = thread
        start(thread.id)
        return .stream(thread: thread.id, opening: [line(["kind": "thread", "thread": thread.id])], fault: .none)
    }

    private func list(_ kind: String, _ filter: [String: Any], _ query: [String: String]) -> Outcome {
        let properties = filter["properties"] as? [String: Any] ?? [:]
        let first = query["first"].flatMap(Int.init) ?? 100
        func equal(_ name: String) -> String? { (properties[name] as? [String: Any])?["eq"] as? String }
        if let words = query["q"] {
            // The ranked read: the messages that hold the words, newest
            // thread first.
            guard kind == "substrate.reamde.dev/llm/message" else { return page([]) }
            let wanted = words.lowercased()
            var found: [[String: Any]] = []
            for thread in order.reversed() {
                for row in threads[thread]?.rows ?? [] where row.content.lowercased().contains(wanted) { found.append(record(row, thread: thread)) }
            }
            note("a search of the messages")
            return .json(200, line(["records": Array(found.prefix(first)), "scores": [String: Any](), "pending": 0]))
        }
        switch kind {
        case "substrate.reamde.dev/core/agent":
            return page([
                [
                    "id": agent, "kind": kind, "version": 1, "createdAt": "2026-10-30T10:00:00Z", "updatedAt": "2026-10-30T10:00:00Z",
                    "properties": ["description": "Helps."],
                ]
            ])
        case "substrate.reamde.dev/llm/thread":
            // The chats; an automated run's list is another mode's, and empty.
            if let mode = properties["mode"] as? [String: Any] {
                let modes = (mode["in"] as? [String]) ?? (mode["eq"] as? String).map { [$0] } ?? []
                if !modes.contains("chat") { return page([]) }
            }
            var held = order.compactMap { threads[$0] }
            if let agent = equal("agent") { held = held.filter { "substrate.reamde.dev/core/agent/\($0.agent)" == agent } }
            if let more = (properties["waitingOnPerson"] as? [String: Any])?["gt"] as? Int { held = held.filter { waits($0) > more } }
            // Narrowed by the person's marks that point at each thread,
            // before the page is cut.
            if let arm = filter["referencedBy"] as? [String: Any] {
                guard keepsFlags else { return refuse(400, "bad_request", #"filter: unknown field "referencedBy""#) }
                let conditions = arm["properties"] as? [String: Any] ?? [:]
                func asks(_ name: String) -> Bool { (conditions[name] as? [String: Any])?["eq"] as? Bool == true }
                let exists = arm["exists"] as? Bool ?? true
                held = held.filter { thread in
                    var points = marks[thread.id] != nil
                    if asks("archived"), marks[thread.id]?.archived != true { points = false }
                    if asks("pinned"), marks[thread.id]?.pinned != true { points = false }
                    return points == exists
                }
            }
            let newest = query["orderBy"]?.hasPrefix("startedAt") == true
            // Stable: threads moved at the same instant keep their order.
            let placed = held.map { thread in (thread, at: newest ? thread.startedAt : thread.updatedAt, made: order.firstIndex(of: thread.id) ?? 0) }
                .sorted { a, b in a.at == b.at ? a.made > b.made : a.at > b.at }
            // A page: the rows after the cursor, which is the last row's
            // place in the order as it stood.
            // A cursor is taken back only with the filter it was cut for.
            let bound = SeededNumbers.hash(query["filter"] ?? "")
            var rest = placed[...]
            if let after = query["after"] {
                let parts = after.split(separator: "|").map(String.init)
                guard parts.count == 3, let time = Double(parts[1]), let made = Int(parts[2]) else {
                    return refuse(422, "validation", "after: not a cursor of this list")
                }
                guard parts[0] == "\(bound)" else {
                    return refuse(422, "validation", "substrate: validation failed: cursor does not match this filter and orderBy")
                }
                let at = Date(timeIntervalSinceReferenceDate: time)
                rest = rest.filter { $0.at < at || ($0.at == at && $0.made < made) }[...]
                note("a page of a list past its first")
            }
            let rows = rest.prefix(first)
            var body: [String: Any] = ["head": feedSeq, "generation": "g", "records": rows.map { record($0.0) }]
            if rest.count > first, let last = rows.last { body["cursor"] = "\(bound)|\(last.at.timeIntervalSinceReferenceDate)|\(last.made)" }
            return .json(200, line(body))
        case "substrate.reamde.dev/llm/message":
            var rows: [(Row, String)] = []
            if let thread = equal("thread") {
                rows = (threads[thread]?.rows ?? []).map { ($0, thread) }
            } else if let named = (properties["thread"] as? [String: Any])?["in"] as? [String] {
                rows = named.flatMap { thread in (threads[thread]?.rows ?? []).map { ($0, thread) } }
            }
            if let role = equal("role") { rows = rows.filter { $0.0.role == role } }
            if query["orderBy"]?.hasPrefix("turn:desc") == true { rows.reverse() }
            return page(rows.prefix(first).map { record($0.0, thread: $0.1) })
        case Card.interaction, Card.approval:
            // The ones that wait, under the threads named where any are.
            var held = cardOrder.compactMap { cards[$0] }.filter { $0.kind == kind }
            if let state = equal("state") { held = held.filter { $0.state == state } }
            if let thread = equal("thread") { held = held.filter { $0.thread == thread } }
            // The person's own resolutions, asked with `me` (`agents` 7):
            // a server below does not know the property.
            let link = kind == Card.interaction ? "resolvedBy" : "decidedBy"
            if properties["resolvedBy"] != nil || properties["decidedBy"] != nil {
                guard revision >= 7, properties[link] != nil else { return refuse(422, "validation", "\(kind) declares no such property") }
                held = held.filter { $0.by == equal(link) }
                note("a read of the person's own resolutions")
            }
            if let named = (properties["thread"] as? [String: Any])?["in"] as? [String] { held = held.filter { named.contains($0.thread) } }
            return page(held.prefix(first).map(record))
        case "substrate.reamde.dev/llm/threadmark":
            guard keepsMarks else { return refuse(404, "not_found", "unknown kind \(kind)") }
            let named = (properties["thread"] as? [String: Any])?["in"] as? [String] ?? []
            return page(named.compactMap { thread in marks[thread].map { record($0, thread: thread) } }.prefix(first).map { $0 })
        default:
            return page([])
        }
    }
}
