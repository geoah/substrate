#if DEBUG
import ChatKit
import Foundation
import SubstrateKit

/// A substrate that answers from ChatKit's fixtures, in the process: the
/// chat list, each chat's rows, the change feed held open, and a turn that
/// streams a reply at a model's pace and then stands in the rows. What a
/// measurement runs against and a gallery draws from, so neither needs a
/// server, a sign-in or a network.
final class FixtureServer: HTTPTransport, @unchecked Sendable {
    /// How a turn's reply is written: its text, the characters in each
    /// piece, and how long the server waits between two.
    struct Pace: Sendable {
        var piece = 12
        var interval: Duration = .milliseconds(30)

        var perSecond: Int {
            let parts = interval.components
            return Int(Double(piece) / (Double(parts.seconds) + Double(parts.attoseconds) / 1e18))
        }
    }

    private let lock = NSLock()
    /// The rows each chat holds, and the turns answered in it since.
    private var rows: [String: Int]
    private var answered: [String: [(message: String, reply: String)]] = [:]
    private var versions: [String: Int] = [:]
    private var reply: String
    private var pace: Pace
    /// The stream stops after this many characters and waits there.
    private var holdAfter: Int?
    /// The runs written a line at a time, by thread.
    private var scripts: [String: Script] = [:]
    /// The `agents` feature's revision the discovery document states: 0
    /// for a server from before it, as the measurement runs against.
    private let agents: Int
    /// A chat's rows where a picture sets them whole, by thread; and the
    /// records served by path under `/api/v1/` (a question's, an
    /// approval's), each with what a write of it leaves.
    private var pages: [String: String] = [:]
    private var records: [String: String] = [:]
    private var written: [String: (record: String, thread: String, page: String)] = [:]

    /// A run a test writes a line at a time (`step`), so every screen
    /// between its first piece and its stored rows can be looked at: the
    /// lines of its stream, which a send streams and an attach replays from
    /// the start, and the rows the thread holds once so many are out.
    struct Script {
        var lines: [String]
        /// The transcript's page once `after` lines are out, later ones
        /// replacing earlier ones.
        var rows: [(after: Int, page: String)]
        var out = 0
        /// The streams reading it end as a connection that dropped does,
        /// and none opens until it is cleared.
        var dropped = false

        var done: Bool { out >= lines.count }
        var page: String? { rows.last { $0.after <= out }?.page }
    }

    init(reply: String = ChatFixtures.reply(bytes: 16 * 1024), pace: Pace = Pace(), agents: Int = 0) {
        rows = [ChatFixtures.long: 200, ChatFixtures.full: 500]
        self.reply = reply
        self.pace = pace
        self.agents = agents
    }

    /// A chat that holds exactly this page of rows.
    func rows(_ thread: String, page: String) {
        lock.withLock { pages[thread] = page }
    }

    /// A chat whose reply asked the person something: its rows, and the
    /// card's record, which a write resolves as the fixture says, with the
    /// rows the thread holds from then. `resolved` serves it as ended from
    /// the start.
    func ask(_ asked: ChatFixtures.Asked, resolved: Bool = false) {
        lock.withLock {
            pages[asked.thread] = resolved ? asked.rowsResolved : asked.rows
            records[asked.path] = resolved ? asked.resolved : asked.pending
            written[asked.path] = (asked.resolved, asked.thread, asked.rowsResolved)
        }
    }

    /// A chat that holds one settled turn: `message`, answered with `reply`.
    func seed(_ thread: String, message: String, reply: String) {
        lock.withLock {
            rows[thread] = 0
            answered[thread] = [(message, reply)]
        }
    }

    /// A chat with nothing in it yet.
    func empty(_ thread: String) {
        lock.withLock { rows[thread] = 0 }
    }

    /// The next turn answers with `reply`, stopping part way where `holdAfter`
    /// says so, for a picture of a reply caught as it is written.
    func answer(with reply: String, holdAfter: Int? = nil) {
        lock.withLock {
            self.reply = reply
            self.holdAfter = holdAfter
        }
    }

    /// The next turn in `thread` is written as `lines`, one at each `step`.
    func script(_ thread: String, lines: [String], rows: [(after: Int, page: String)]) {
        lock.withLock {
            self.rows[thread] = 0
            scripts[thread] = Script(lines: lines, rows: rows)
        }
    }

    /// Lets the next `count` lines of the thread's run out, to every stream
    /// reading it.
    func step(_ thread: String, _ count: Int = 1) {
        lock.withLock {
            guard var script = scripts[thread] else { return }
            script.out = min(script.lines.count, script.out + count)
            if script.done { versions[thread] = (versions[thread] ?? 3) + 1 }
            scripts[thread] = script
        }
    }

    /// Drops every stream reading the thread's run, as a network that went
    /// away does, and keeps the next from opening.
    func drop(_ thread: String) {
        lock.withLock { scripts[thread]?.dropped = true }
    }

    /// A scripted run's lines from its first, each as it is let out, to
    /// its last.
    private func scripted(_ thread: String) -> AsyncThrowingStream<String, any Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                var sent = 0
                while !Task.isCancelled {
                    guard let script = self.lock.withLock({ self.scripts[thread] }) else { break }
                    if script.dropped {
                        continuation.finish(throwing: URLError(.networkConnectionLost))
                        return
                    }
                    while sent < script.out {
                        continuation.yield(script.lines[sent])
                        sent += 1
                    }
                    if sent >= script.lines.count { break }
                    try? await Task.sleep(for: .milliseconds(2))
                }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let stream = try await lines(for: request)
        return (try await stream.body(limit: .max), stream.head)
    }

    func lines(for request: URLRequest) async throws -> LineStream {
        let url = request.url ?? ChatFixtures.origin
        let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        let path = components?.percentEncodedPath.removingPercentEncoding ?? url.path
        let query = components?.query ?? ""
        let method = request.httpMethod ?? "GET"
        func head(_ status: Int, _ type: String = "application/json") -> HTTPURLResponse {
            HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": type])!
        }
        func json(_ body: String, status: Int = 200) -> LineStream { LineStream(head: head(status), body: Data(body.utf8)) }
        let missing = json(#"{"error":{"code":"not_found","message":"The fixture holds nothing there."}}"#, status: 404)

        if path.hasSuffix("/.well-known/substrate/server.json") { return json(Self.discovery(agents: agents)) }
        // A card's record: read, and resolved by the one write a card makes.
        if let range = path.range(of: "/api/v1/"), let held = lock.withLock({ records[String(path[range.upperBound...])] }) {
            let key = String(path[range.upperBound...])
            guard method == "PATCH" else { return json(held) }
            let next = lock.withLock { () -> String in
                guard let after = written[key] else { return held }
                records[key] = after.record
                pages[after.thread] = after.page
                versions[after.thread] = (versions[after.thread] ?? 3) + 1
                return after.record
            }
            return json(next)
        }
        // A run whose stream was dropped: the thread and its stream are
        // out of reach, the rest answers.
        if path.contains("/llm/thread/"), lock.withLock({ scripts.values.contains(where: \.dropped) }) {
            throw URLError(.networkConnectionLost)
        }
        if path.hasSuffix("/api/v1/changes") {
            // The bookmark, then nothing: the feed stays open and quiet.
            let lines = AsyncThrowingStream<String, any Error> { continuation in
                continuation.yield(#"{"bookmark":10,"generation":"g"}"#)
                let task = Task {
                    while !Task.isCancelled { try? await Task.sleep(for: .seconds(20)) }
                    continuation.finish()
                }
                continuation.onTermination = { _ in task.cancel() }
            }
            return LineStream(head: head(200, "application/x-ndjson"), lines: lines)
        }
        if method == "POST", path.hasSuffix("/chat") {
            let body = request.httpBody.flatMap { JSONValue.parse(String(decoding: $0, as: UTF8.self)) }
            if let thread = body?["thread"]?.string, lock.withLock({ scripts[thread] != nil }) {
                // A message into the run while it is written joins it: the
                // script's `$Q` is the id it was sent under.
                if let id = body?["messageId"]?.string, lock.withLock({ scripts[thread].map { $0.out > 0 && !$0.done } ?? false }) {
                    lock.withLock {
                        guard var script = scripts[thread] else { return }
                        script.lines = script.lines.map { $0.replacingOccurrences(of: "$Q", with: id) }
                        script.rows = script.rows.map { ($0.after, $0.page.replacingOccurrences(of: "$Q", with: id)) }
                        scripts[thread] = script
                    }
                }
                return LineStream(head: head(200, "application/x-ndjson"), lines: scripted(thread))
            }
            return LineStream(
                head: head(200, "application/x-ndjson"),
                lines: turn(thread: body?["thread"]?.string ?? "", message: body?["message"]?.string ?? ""))
        }
        if method == "GET", path.hasSuffix("/api/v1/records") {
            // The person's own resolved cards (`me` in the filter): the
            // fixture has one person, so every card that ended is theirs.
            if query.contains(#""eq":"me""#) {
                let own = lock.withLock { records.filter { query.contains($0.key.split(separator: "/").dropLast().joined(separator: "/")) }.map(\.value) }
                return json(ChatFixtures.page(own.filter { !$0.contains(#""state":"pending""#) }))
            }
            if query.contains("llm/message") {
                // Every chat has a subject, so no read asks for opening lines.
                guard !query.contains(#""in""#), let thread = Self.named(in: query) else { return json(ChatFixtures.page([])) }
                return json(transcript(thread))
            }
            if query.contains("llm/thread") { return json(query.contains(#""chat""#) ? ChatFixtures.threads() : ChatFixtures.page([])) }
            if query.contains("core/agent") { return json(ChatFixtures.agents()) }
            if query.contains("llm/provider") { return json(ChatFixtures.providers()) }
            return json(ChatFixtures.page([]))
        }
        if method == "GET", let range = path.range(of: "/llm/thread/") {
            let id = String(path[range.upperBound...])
            if id.hasSuffix("/events") {
                // An attach: the run from its start while one is being
                // written, else the one line that says none is.
                let thread = String(id.dropLast("/events".count))
                let running = lock.withLock { scripts[thread].map { $0.out > 0 && !$0.done } ?? false }
                let idle = AsyncThrowingStream<String, any Error> { continuation in
                    continuation.yield(#"{"kind":"idle","thread":"\#(thread)","status":"ok"}"#)
                    continuation.finish()
                }
                return LineStream(head: head(200, "application/x-ndjson"), lines: running ? scripted(thread) : idle)
            }
            guard !id.contains("/") else { return missing }
            let (version, running, stopped) = lock.withLock {
                // A run whose last line says it was stopped left its thread so.
                (
                    versions[id] ?? 3, scripts[id].map { $0.out > 0 && !$0.done } ?? false,
                    scripts[id].map { $0.done && $0.lines.last?.contains(#""status":"stopped""#) == true } ?? false
                )
            }
            return json(ChatFixtures.thread(id, version: version, status: running ? "running" : stopped ? "stopped" : "ok"))
        }
        return missing
    }

    /// The thread a transcript read names: the `eq` of its filter.
    private static func named(in query: String) -> String? {
        guard let start = query.range(of: #""eq":""#) else { return nil }
        let rest = query[start.upperBound...]
        return rest.firstIndex(of: "\"").map { String(rest[..<$0]) }
    }

    private func transcript(_ thread: String) -> String {
        if let page = lock.withLock({ pages[thread] }) { return page }
        if let script = lock.withLock({ scripts[thread] }) { return script.page ?? ChatFixtures.page([]) }
        let (held, turns) = lock.withLock { (rows[thread] ?? 4, answered[thread] ?? []) }
        guard let last = turns.last else { return ChatFixtures.transcript(thread, rows: held) }
        // One answered turn is as far as a measurement goes in a chat.
        return ChatFixtures.transcript(thread, rows: held + (turns.count - 1) * 2, message: last.message, reply: last.reply)
    }

    private func turn(thread: String, message: String) -> AsyncThrowingStream<String, any Error> {
        let (text, pace, hold, held) = lock.withLock { (reply, self.pace, holdAfter, rows[thread] ?? 4) }
        let lines = ChatFixtures.stream(thread: thread, text: text, turn: held + 1, piece: pace.piece)
        return AsyncThrowingStream { continuation in
            let task = Task {
                var written = 0
                for line in lines {
                    if let hold, written >= hold {
                        while !Task.isCancelled { try? await Task.sleep(for: .seconds(20)) }
                    }
                    if Task.isCancelled { break }
                    if line.hasPrefix(#"{"kind":"done""#) {
                        // The rows hold the turn before the stream says so.
                        self.lock.withLock {
                            self.answered[thread, default: []].append((message, text))
                            self.versions[thread] = (self.versions[thread] ?? 3) + 1
                        }
                    }
                    continuation.yield(line)
                    if line.hasPrefix(#"{"kind":"delta""#) {
                        written += pace.piece
                        try? await Task.sleep(for: pace.interval)
                    }
                }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    /// A released server's discovery document, with no app authorization:
    /// the session is a password one. Its `agents` feature is listed where
    /// a picture needs what a revision of it brought.
    private static func discovery(agents: Int) -> String {
        let listed = agents > 0 ? #",{"name":"agents","stability":"experimental","revision":\#(agents),"surfaces":["rest"]}"# : ""
        return #"""
            {"versions":[{"name":"v1","status":"served"}],
             "server":{"version":"v0.123.1","build":"0123456789abcdef0123456789abcdef01234567"},
             "changelog":{"horizon":0},"vocabulary":{"dialect":3},
             "features":[{"name":"records","stability":"stable","revision":1,"surfaces":["rest"]}\#(listed)],
             "registration":{"inviteRequired":false,"totpRequired":false}}
            """#
    }
}

/// A session kept in memory, for a fixture: nothing reaches the Keychain.
final class MemorySessions: ImmediateSessionStorage, @unchecked Sendable {
    private let lock = NSLock()
    private var items: [String: Data] = [:]
    let location = "memory|" + UUID().uuidString

    func read(account: String) -> StoredItem { lock.withLock { items[account].map(StoredItem.found) ?? .missing } }
    func write(_ data: Data, account: String) throws { lock.withLock { items[account] = data } }
    func delete(account: String) throws { lock.withLock { items[account] = nil } }
}

/// A network that is there, whatever the device's is.
struct AlwaysOnline: Connectivity {
    func current() async -> NetworkPath { NetworkPath(online: true) }
    func paths() -> AsyncStream<NetworkPath> {
        AsyncStream { continuation in continuation.yield(NetworkPath(online: true)) }
    }
}

/// A folder of the fixture's own under the temporary directory.
struct ScratchFolders: AppFolders {
    let root: URL

    init() {
        root = FileManager.default.temporaryDirectory.appendingPathComponent("chat-fixture-\(UUID().uuidString)", isDirectory: true)
    }

    func data() throws -> URL {
        let url = root.appendingPathComponent("data", isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    func cache() throws -> URL {
        let url = root.appendingPathComponent("cache", isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    var writingOptions: Data.WritingOptions { [] }
}

extension ChatHost.Parts {
    /// A host signed in to the fixture: its session in memory, its folder a
    /// scratch one, no notifications and no background work.
    static func fixture(_ server: FixtureServer) -> ChatHost.Parts {
        let storage = MemorySessions()
        let session = Session(server: ChatFixtures.origin, repository: ChatFixtures.repository, secret: "fixture", tokenID: "fixture")
        if let data = try? JSONEncoder().encode(session) { try? storage.write(data, account: "session") }
        return ChatHost.Parts(
            sessions: SessionStore(storage: storage), transport: server, folders: ScratchFolders(),
            preferences: MemoryPreferenceStore(), notifications: nil, background: nil, connectivity: AlwaysOnline())
    }
}
#endif
