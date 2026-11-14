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
final class SeededServer: @unchecked Sendable {
    /// One `llm/message` row.
    struct Row: Equatable, Sendable {
        var id: String
        var role: String
        var content: String
        var turn: Int
        var createdAt: Date
    }

    /// One reader of a running turn's events.
    private struct Reader {
        var id: Int
        var continuation: AsyncThrowingStream<String, any Error>.Continuation
        /// Lines delivered so far, the `thread` line included.
        var delivered = 0
        var fault: StreamFault
    }

    /// A turn running on a thread.
    private struct Turn {
        /// The ordinal its reply row will carry.
        var ordinal: Int
        /// Every event emitted so far: what an attach replays.
        var emitted: [String] = []
        var remaining: [String]
        var written = ""
        /// The turn ends in a provider's failure instead of its last piece.
        var fails: Bool
        var readers: [Reader] = []
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
        var turn: Turn?
    }

    /// What a stream does wrong once it is open.
    struct StreamFault: Equatable, Sendable {
        /// The stream ends after this many lines.
        var cutAfter: Int?
        /// It ends with the connection's error, not quietly.
        var loud = false
        /// This line is sent twice.
        var duplicate: Int?

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

    let seed: UInt64
    let clock: TestClock
    /// The `agents` feature's revision the discovery document states: below
    /// 3 the server refuses `messageId` and has no continue route.
    let revision: Int
    let agent: String
    private let lock = NSLock()
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
    private var lines = 0
    private var refused401 = false
    private var faults = 0
    private var closed = 0
    private var counted: [String: Int] = [:]
    private var inside: [Int: String] = [:]
    private var sleeping: [Int: Date] = [:]
    /// Every chat request that reached the server, by its message's text.
    private var chats: [String: Int] = [:]

    init(seed: UInt64, clock: TestClock, revision: Int = 3, agent: String = helper) {
        self.seed = seed
        self.clock = clock
        self.revision = revision
        self.agent = agent
    }

    // MARK: What the test reads and moves

    /// The rows a correct server holds of a thread, in order.
    func rows(of thread: String) -> [Row] { lock.withLock { threads[thread]?.rows ?? [] } }

    var threadIDs: [String] { lock.withLock { order } }

    func version(of thread: String) -> Int? { lock.withLock { threads[thread]?.version } }

    func running(_ thread: String) -> Bool { lock.withLock { threads[thread]?.turn != nil } }

    var anyRunning: Bool { lock.withLock { threads.values.contains { $0.turn != nil } } }

    /// How many user rows hold exactly this text, in every thread.
    func userRows(saying text: String) -> Int {
        lock.withLock { threads.values.reduce(0) { $0 + $1.rows.filter { $0.role == "user" && $0.content == text }.count } }
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

    /// Moves whenever a request arrives or a line goes out: a test waits
    /// for it to stand still.
    var activity: Int { lock.withLock { seen + lines + closed } }

    /// What the server did, by name: how far a run reached.
    var tally: [String: Int] { lock.withLock { counted } }

    private func note(_ name: String) { counted[name, default: 0] += 1 }

    /// How many requests came, and how many of them drew a fault.
    var requests: Int { lock.withLock { seen } }
    var faulted: Int { lock.withLock { faults } }

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
    private func start(_ id: String) {
        guard var thread = threads[id] else { return }
        var numbers = SeededNumbers(seed: seed ^ SeededNumbers.hash("turn|\(id)|\(thread.nextTurn)"))
        let ordinal = thread.nextTurn
        thread.nextTurn += 1
        let pieces = (0 ... numbers.below(4)).map { "r\(ordinal).\($0) " }
        thread.turn = Turn(
            ordinal: ordinal, remaining: pieces.map { line(["kind": "delta", "text": $0, "turn": ordinal]) },
            fails: numbers.chance(0.12))
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
        if let next = turn.remaining.first, !(turn.fails && turn.remaining.count == 1) {
            turn.remaining.removeFirst()
            turn.emitted.append(next)
            if case let .object(event)? = JSONValue.parse(next), case let .string(text)? = event["text"] { turn.written += text }
            thread.turn = turn
            threads[id] = thread
            emit(id, next)
            return
        }
        settle(id, status: turn.fails ? "error" : "ok")
    }

    /// Ends the thread's turn: the reply so far is its row, the thread
    /// says how it ended, and every reader hears it and is let go.
    private func settle(_ id: String, status: String) {
        guard var thread = threads[id], let turn = thread.turn else { return }
        if !turn.written.isEmpty {
            minted += 1
            thread.rows.append(Row(id: "a\(minted)", role: "assistant", content: turn.written, turn: turn.ordinal, createdAt: clock.now))
            thread.lastReply = turn.written
        }
        thread.status = status
        thread.cause = status == "error" ? "provider_overloaded" : status == "stopped" ? "requested" : nil
        thread.version += 1
        thread.updatedAt = clock.now
        thread.finishedAt = clock.now
        thread.turn = nil
        threads[id] = thread
        let last: String
        if status == "error" {
            last = line(["kind": "error", "error": "llm: overloaded_error", "cause": "provider_overloaded"])
        } else {
            var result: [String: Any] = ["reply": turn.written, "thread": id, "status": status, "turns": 1]
            if status == "stopped" { result["cause"] = "requested" }
            last = line(["kind": "done", "result": result])
        }
        for reader in turn.readers {
            var reader = reader
            if deliver(last, to: &reader) { reader.continuation.finish() }
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
            reader.continuation.finish(throwing: reader.fault.loud ? URLError(.networkConnectionLost) : nil)
            return false
        }
        reader.continuation.yield(text)
        lines += 1
        if reader.fault.duplicate == reader.delivered { reader.continuation.yield(text) }
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
            try await server.answer(request, port: number)
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
        case watch
        case other

        var writes: Bool {
            switch self {
            case .chat, .stop, .resume: true
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
        if path.hasSuffix("/api/v1/changes") { return (.watch, "watch", nil) }
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
        return (.other, "other|\(method)|\(path)", nil)
    }

    /// The fault this request draws: fixed by the seed, the request's
    /// signature and how many like it came before.
    private func fault(for call: Call, signature: String) -> (Fault, lands: Bool) {
        let count = counts[signature, default: 0]
        counts[signature] = count + 1
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
            if numbers.chance(0.5) { faults = streams } else { faults += streams }
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
            if broken.cutAfter == 0 { note("fault: closed before the thread event") } else if broken.cutAfter != nil { note("fault: a stream cut at an event") }
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
            return json(status, #"{"error":{"code":"\#(code)","message":"the server could not answer"}}"#, url: url, retryAfter: retryAfter)
        case .gateway:
            return page(502, url: url)
        case .unauthorized:
            lock.withLock { refused401 = true }
            return json(401, #"{"error":{"code":"unauthorized","message":"the token was revoked"}}"#, url: url)
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
        case .parked:
            // The change feed: its bookmark, then nothing, for as long as
            // the app listens.
            return ndjson(url: url) { continuation in
                continuation.yield(#"{"bookmark":1,"generation":"g"}"#)
                return nil
            }
        case let .stream(thread, opening, broken):
            entered = false
            return ndjson(url: url) { [self] continuation in
                lock.withLock {
                    readers += 1
                    var reader = Reader(id: readers, continuation: continuation, fault: broken)
                    for text in opening {
                        guard deliver(text, to: &reader) else { return nil as Int? }
                    }
                    // What the turn emitted before this reader came: an
                    // attach's replay, from the turn's start.
                    guard var turn = threads[thread]?.turn else {
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
                    lock.withLock {
                        if let reader { threads[thread]?.turn?.readers.removeAll { $0.id == reader } }
                        if let tag { open[tag, default: 0] -= 1 }
                        closed += 1
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
        case parked
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
            properties["reason"] = "the run ended: \(cause)"
        }
        return [
            "id": thread.id, "kind": "substrate.reamde.dev/llm/thread", "version": thread.version, "createdAt": stamp(thread.startedAt),
            "updatedAt": stamp(thread.updatedAt), "properties": properties,
        ]
    }

    private func record(_ row: Row, thread: String) -> [String: Any] {
        [
            "id": row.id, "kind": "substrate.reamde.dev/llm/message", "version": 1, "createdAt": stamp(row.createdAt),
            "updatedAt": stamp(row.createdAt),
            "properties": [
                "role": row.role, "content": row.content, "turn": row.turn, "thread": ["ref": "substrate.reamde.dev/llm/thread/\(thread)"],
            ],
        ]
    }

    private func page(_ records: [[String: Any]]) -> Outcome {
        .json(200, line(["head": seen, "generation": "g", "records": records]))
    }

    private func perform(_ call: Call, url: URL) -> Outcome {
        switch call {
        case .discovery:
            return .json(
                200,
                line([
                    "server": ["version": "seeded"],
                    "features": [["name": "agents", "stability": "experimental", "revision": revision]],
                ]))
        case .watch:
            return .parked
        case let .thread(id):
            guard let thread = threads[id] else { return refuse(404, "not_found", "substrate: not found: thread \(id)") }
            return .json(200, line(record(thread)))
        case let .message(id):
            for thread in order {
                if let row = threads[thread]?.rows.first(where: { $0.id == id }) { return .json(200, line(record(row, thread: thread))) }
            }
            return refuse(404, "not_found", "substrate: not found: message \(id)")
        case let .list(kind, filter, query):
            return list(kind, filter, query)
        case let .chat(agent, body):
            return chat(agent, body)
        case let .events(id):
            guard let thread = threads[id] else { return refuse(404, "not_found", "substrate: not found: thread \(id)") }
            guard thread.turn != nil else {
                return .stream(thread: id, opening: [line(["kind": "idle", "thread": id, "status": thread.status])], fault: .none)
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
            if thread.status == "ok" { return refuse(409, "conflict", reason: "nothing_to_continue", "the thread's last turn finished") }
            if let capped = capped() { return capped }
            note("a continue")
            start(id)
            return .stream(thread: id, opening: [line(["kind": "thread", "thread": id])], fault: .none)
        case .other:
            return refuse(404, "not_found", "no such API path: \(url.path)")
        }
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
        guard agent == self.agent else { return refuse(404, "not_found", "substrate: not found: agent \(agent)") }
        if let id {
            // The id's record, wherever it is: this message, or another.
            for thread in order {
                guard let row = threads[thread]?.rows.first(where: { $0.id == id }) else { continue }
                let same = row.role == "user" && row.content == text && (named.isEmpty ? row.turn == 0 : named == thread)
                guard same else {
                    note("an id refused as taken")
                    return refuse(409, "conflict", "substrate: version conflict: the messageId is taken: messageId \"\(id)\": send the message under a new id")
                }
                // Nothing written, nothing run: the `thread` event, then
                // what an attach answers.
                note("a repeat answered by its id")
                var opening = [line(["kind": "thread", "thread": thread])]
                if threads[thread]?.turn == nil {
                    opening.append(line(["kind": "idle", "thread": thread, "status": threads[thread]?.status ?? "ok"]))
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
            guard let held = threads[named] else { return refuse(404, "not_found", "substrate: not found: thread \(named)") }
            if held.turn != nil {
                note("a send refused: thread_busy")
                return refuse(409, "conflict", reason: "thread_busy", "a turn is running on the thread")
            }
            if let capped = capped() { return capped }
            thread = held
        }
        minted += 1
        note(id == nil ? "a message written without an id" : "a message written under its id")
        thread.rows.append(Row(id: id ?? "u\(minted)", role: "user", content: text, turn: thread.nextTurn, createdAt: clock.now))
        thread.nextTurn += 1
        threads[thread.id] = thread
        start(thread.id)
        return .stream(thread: thread.id, opening: [line(["kind": "thread", "thread": thread.id])], fault: .none)
    }

    private func list(_ kind: String, _ filter: [String: Any], _ query: [String: String]) -> Outcome {
        let properties = filter["properties"] as? [String: Any] ?? [:]
        let first = query["first"].flatMap(Int.init) ?? 100
        func equal(_ name: String) -> String? { (properties[name] as? [String: Any])?["eq"] as? String }
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
            if let mode = properties["mode"] as? [String: Any], mode["eq"] as? String != "chat" { return page([]) }
            var held = order.compactMap { threads[$0] }
            if let agent = equal("agent") { held = held.filter { "substrate.reamde.dev/core/agent/\($0.agent)" == agent } }
            let newest = query["orderBy"]?.hasPrefix("startedAt") == true
            // Stable: threads moved at the same instant keep their order.
            held = held.enumerated().sorted { a, b in
                let (x, y) = newest ? (a.element.startedAt, b.element.startedAt) : (a.element.updatedAt, b.element.updatedAt)
                return x == y ? a.offset > b.offset : x > y
            }.map(\.element)
            return page(held.prefix(first).map(record))
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
        default:
            return page([])
        }
    }
}
