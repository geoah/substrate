import Foundation
import SubstrateDevice

@testable import ChatKit

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// A server that keeps marks the way the real one does (decision 0313), for
/// one person on any number of devices at once: the chats with the newest
/// message each settled with, the person's mark of each, held to the
/// server's rules on every write (a position only moves forward, Mark as
/// Unread is a flag a position taken clears), and a change feed every
/// device's watch reads the same rows from. With `records` below 8 it is a
/// server from before marks: it does not know the kind and its threads name
/// no newest message. At 9 it keeps `pinned` and `archived` on a mark as
/// #1459's does (decision 0322): two flags only the person's write moves,
/// and a list of threads narrowed by the marks that point at them
/// (`referencedBy`), cut into pages with a cursor. Below 9 it refuses the
/// arm (`400`) and the two properties (`422`), as a server before them
/// does.
final class MarkServer: HTTPTransport, @unchecked Sendable {
    struct Message {
        var id: String
        var role: String
        var content: String
    }

    struct Chat {
        var id: String
        var version = 1
        var status = "ok"
        var messages: [Message] = []
        var lastMessage: String?
        var waiting = 0
        /// Seconds past the server's first instant: when it last settled.
        var finished = 0
    }

    struct Mark {
        var readThrough: String?
        var markedUnread: Bool?
        var version = 0
        /// The place in the feed where the position was taken.
        var readAt: Int?
        /// Absent until set.
        var pinned: Bool?
        var archived: Bool?
    }

    private let lock = NSLock()
    private var features: (agents: Int, records: Int)
    private var chats: [String: Chat] = [:]
    private var marks: [String: Mark] = [:]
    private var seq = 10
    /// Every row the feed carried, for a watch that resumes from a cursor.
    private var rows: [(seq: Int, kind: String, line: String)] = []
    private var watchers: [UUID: (kinds: Set<String>, to: AsyncThrowingStream<String, any Error>.Continuation)] = [:]
    private var kept: [String] = []
    private var written: [[String: Any]] = []
    private var failing = 0
    private var refusing: (status: Int, code: String)?
    private var held: Set<String> = []
    private var afterThreads: (@Sendable () -> Void)?

    init(records: Int = 8, agents: Int = 2) {
        features = (agents, records)
    }

    private var keepsMarks: Bool { features.records >= 8 }
    private var keepsFlags: Bool { features.records >= 9 }
    /// The next `count` searches are answered 500; `refusesSearch` answers
    /// every one as an app's token is (`403`).
    private var failingSearches = 0
    private var refusesSearch = false

    // MARK: What a test does to it

    /// Another server takes its place: an upgrade, or a rollback.
    func become(records: Int) { lock.withLock { features.records = records } }

    /// A chat with a question and its settled reply: `m<n>` are its
    /// messages, in order.
    func chat(_ id: String, messages count: Int = 2, readThrough: String? = nil, waiting: Int = 0) {
        lock.withLock {
            seq += 1
            var chat = Chat(id: id, waiting: waiting, finished: seq)
            for n in 1 ... max(1, count) {
                chat.messages.append(Message(id: "\(id)-m\(n)", role: n % 2 == 1 ? "user" : "assistant", content: "Message \(n) of \(id)"))
            }
            chat.lastMessage = chat.messages.last?.id
            chats[id] = chat
            // A position from before the thread last settled, unless it
            // is that of its newest message.
            if let readThrough { marks[id] = Mark(readThrough: readThrough, version: 1, readAt: readThrough == chat.lastMessage ? seq : 0) }
        }
    }

    /// A reply settles in a chat, written by a turn no device here streams.
    @discardableResult
    func settle(_ id: String, reply: String = "A new reply", status: String = "ok") -> String {
        lock.withLock {
            guard var chat = chats[id] else { return "" }
            seq += 1
            let message = Message(id: "\(id)-m\(chat.messages.count + 1)", role: "assistant", content: reply)
            chat.messages.append(message)
            chat.lastMessage = message.id
            chat.status = status
            chat.version += 1
            chat.finished = seq
            chats[id] = chat
            push(Kinds.message, message.id)
            push(Kinds.thread, id)
            return message.id
        }
    }

    /// What waits on a person in a chat moved.
    func wait(_ id: String, on count: Int) {
        lock.withLock {
            chats[id]?.waiting = count
            chats[id]?.version += 1
            push(Kinds.thread, id)
        }
    }

    /// The person's mark as it stands.
    func mark(_ thread: String) -> Mark? { lock.withLock { marks[thread] } }

    /// A device not in the test moves the person's mark, held to the same
    /// rules.
    func write(_ thread: String, readThrough: String? = nil, markedUnread: Bool? = nil, pinned: Bool? = nil, archived: Bool? = nil) {
        lock.withLock { _ = put(thread, readThrough: readThrough, markedUnread: markedUnread, pinned: pinned, archived: archived) }
    }

    /// A chat starts running, or settles as it was, by a turn no device
    /// here streams.
    func run(_ id: String, running: Bool = true) {
        lock.withLock {
            seq += 1
            chats[id]?.status = running ? "running" : "ok"
            chats[id]?.version += 1
            chats[id]?.finished = seq
            push(Kinds.thread, id)
        }
    }

    /// A message somebody said in a chat long ago: before every row it holds.
    func older(_ id: String, messages count: Int) {
        lock.withLock {
            guard var chat = chats[id] else { return }
            let old = (0 ..< count).map { n in Message(id: "\(id)-o\(n)", role: n % 2 == 0 ? "user" : "assistant", content: "Older message \(n) of \(id)") }
            chat.messages = old + chat.messages
            chats[id] = chat
        }
    }

    /// The thread ids in the order the list answers them.
    var listed: [String] { lock.withLock { chats.values.sorted { ($0.finished, $0.id) > ($1.finished, $1.id) }.map(\.id) } }

    func failSearches(_ count: Int) { lock.withLock { failingSearches = count } }
    func refuseSearch(_ refuses: Bool = true) { lock.withLock { refusesSearch = refuses } }

    /// The next `count` writes of a mark are answered 500.
    func failWrites(_ count: Int) { lock.withLock { failing = count } }
    /// Every write of a mark is refused so, until told otherwise.
    func refuseWrites(_ status: Int?, code: String = "validation") { lock.withLock { refusing = status.map { ($0, code) } } }

    /// Runs once, right after the next list of threads is answered and
    /// before anything else is: what happens on the server between a
    /// device's read of the threads and its read of their marks.
    func afterNextThreadList(_ act: @escaping @Sendable () -> Void) { lock.withLock { afterThreads = act } }

    /// The requests naming `text` wait until `letGo(_:)`.
    func hold(_ text: String) { lock.withLock { _ = held.insert(text) } }
    func letGo(_ text: String) { lock.withLock { _ = held.remove(text) } }

    var requests: [String] { lock.withLock { kept } }
    func count(_ text: String, and other: String? = nil) -> Int {
        lock.withLock { kept.count { $0.contains(text) && (other.map($0.contains) ?? true) } }
    }
    /// The bodies of the mark writes received, in order.
    var writes: [[String: Any]] { lock.withLock { written } }

    // MARK: The rules

    /// Called under the lock. Answers the record's JSON and whether the
    /// write created it; nil for a thread the server does not hold.
    private func put(_ thread: String, readThrough: String?, markedUnread: Bool?, pinned: Bool? = nil, archived: Bool? = nil)
        -> (json: [String: Any], created: Bool)?
    {
        guard let chat = chats[thread] else { return nil }
        let created = marks[thread] == nil
        var mark = marks[thread] ?? Mark()
        var changed = created
        if let readThrough, let place = chat.messages.firstIndex(where: { $0.id == readThrough }) {
            let stored = mark.readThrough.flatMap { id in chat.messages.firstIndex { $0.id == id } }
            // Forward only: an earlier message leaves the position alone.
            if stored.map({ place >= $0 }) ?? true {
                let cleared = mark.markedUnread == true && markedUnread != true
                if mark.readThrough != readThrough || cleared { changed = true }
                if mark.readThrough != readThrough { mark.readAt = seq + 1 }
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
        if changed {
            mark.version += 1
            marks[thread] = mark
            push(Kinds.threadMark, "\(thread).me")
        }
        return (markJSON(thread, mark), created)
    }

    /// Called under the lock. Each row takes the next place in the feed.
    private func push(_ kind: String, _ id: String) {
        seq += 1
        let row = #"{"seq":\#(seq),"ts":"\#(stamp(seq))","kind":"\#(kind)","recordId":"\#(id)","op":"patch"}"#
        rows.append((seq, kind, row))
        for watcher in watchers.values where watcher.kinds.contains(kind) { watcher.to.yield(row) }
    }

    private func stamp(_ seconds: Int) -> String {
        String(format: "2026-10-31T10:%02d:%02dZ", seconds / 60, seconds % 60)
    }

    private func markJSON(_ thread: String, _ mark: Mark) -> [String: Any] {
        var properties: [String: Any] = [
            "thread": ["ref": "\(Kinds.thread)/\(thread)"], "readBy": ["ref": "\(Kinds.core)/actor/me"],
        ]
        if let read = mark.readThrough {
            properties["readThrough"] = ["ref": "\(Kinds.message)/\(read)"]
            properties["readAt"] = stamp(mark.readAt ?? seq)
        }
        if let unread = mark.markedUnread { properties["markedUnread"] = unread }
        if let pinned = mark.pinned { properties["pinned"] = pinned }
        if let archived = mark.archived { properties["archived"] = archived }
        return record("\(thread).me", Kinds.threadMark, mark.version, at: seq, properties)
    }

    private func record(_ id: String, _ kind: String, _ version: Int, at: Int, _ properties: [String: Any]) -> [String: Any] {
        ["id": id, "kind": kind, "version": version, "createdAt": stamp(0), "updatedAt": stamp(at), "properties": properties]
    }

    private func threadJSON(_ chat: Chat) -> [String: Any] {
        var properties: [String: Any] = [
            "mode": "chat", "status": chat.status, "agent": ["ref": "\(Kinds.agent)/\(helper)"], "subject": "Chat \(chat.id)",
        ]
        if chat.status != "running" {
            properties["finishedAt"] = stamp(chat.finished)
            if let reply = chat.messages.last(where: { $0.role == "assistant" }) { properties["lastReply"] = reply.content }
        }
        if keepsMarks {
            if let last = chat.lastMessage { properties["lastMessage"] = ["ref": "\(Kinds.message)/\(last)"] }
            if chat.waiting > 0 { properties["waitingOnPerson"] = chat.waiting }
        }
        return record(chat.id, Kinds.thread, chat.version, at: chat.finished, properties)
    }

    private func messageJSON(_ chat: Chat, _ index: Int) -> [String: Any] {
        let message = chat.messages[index]
        return record(
            message.id, Kinds.message, 1, at: index,
            ["role": message.role, "content": message.content, "turn": index, "thread": ["ref": "\(Kinds.thread)/\(chat.id)"]])
    }

    // MARK: Requests

    private enum Answer {
        case json(Int, Any)
        case watch(Set<String>, from: Int?)
    }

    private func page(_ records: [[String: Any]]) -> Answer {
        .json(200, ["head": seq, "generation": "g", "records": records])
    }

    private func refusal(_ status: Int, _ code: String, _ message: String) -> Answer {
        .json(status, ["error": ["code": code, "message": message]])
    }

    /// Called under the lock. A list of threads is narrowed by the
    /// person's marks where the filter says (`referencedBy`), then cut
    /// into a page: `first` rows after the cursor, which is the last row's
    /// place in the order.
    private func list(_ filter: [String: Any], _ query: [String: String]) -> Answer {
        let kind = (filter["kinds"] as? [String])?.first ?? ""
        let properties = filter["properties"] as? [String: [String: Any]] ?? [:]
        let named = (properties["thread"]?["in"] as? [String]) ?? (properties["thread"]?["eq"] as? String).map { [$0] }
        let first = query["first"].flatMap(Int.init) ?? 100
        if let words = query["q"] {
            if failingSearches > 0 {
                failingSearches -= 1
                return refusal(500, "internal", "the search could not be read")
            }
            if refusesSearch { return refusal(403, "forbidden", "an app's token reads \(kind) through a list") }
            guard kind == Kinds.message else { return page([]) }
            let wanted = words.lowercased().split(separator: " ").map(String.init)
            var found: [[String: Any]] = []
            for chat in chats.values.sorted(by: { ($0.finished, $0.id) > ($1.finished, $1.id) }) {
                for index in chat.messages.indices where wanted.allSatisfy(chat.messages[index].content.lowercased().contains) {
                    found.append(messageJSON(chat, index))
                }
            }
            return .json(200, ["records": Array(found.prefix(first)), "scores": [String: Any](), "pending": 0])
        }
        switch kind {
        case Kinds.agent:
            return page([record(helper, Kinds.agent, 1, at: 0, ["description": "Helps."])])
        case Kinds.thread:
            let modes = (properties["mode"]?["in"] as? [String]) ?? (properties["mode"]?["eq"] as? String).map { [$0] } ?? ["chat"]
            guard modes.contains("chat") else { return page([]) }
            var held = chats.values.sorted { ($0.finished, $0.id) > ($1.finished, $1.id) }
            if let waits = properties["waitingOnPerson"]?["gt"] as? Int { held = held.filter { $0.waiting > waits } }
            if let arm = filter["referencedBy"] as? [String: Any] {
                guard keepsFlags else { return refusal(400, "bad_request", #"filter: unknown field "referencedBy""#) }
                let conditions = arm["properties"] as? [String: [String: Any]] ?? [:]
                let exists = arm["exists"] as? Bool ?? true
                held = held.filter { chat in
                    var points = marks[chat.id] != nil
                    if conditions["archived"]?["eq"] as? Bool == true, marks[chat.id]?.archived != true { points = false }
                    if conditions["pinned"]?["eq"] as? Bool == true, marks[chat.id]?.pinned != true { points = false }
                    return points == exists
                }
            }
            // A cursor is taken back only with the filter it was cut for.
            let bound = SeededNumbers.hash(query["filter"] ?? "")
            if let after = query["after"] {
                let parts = after.split(separator: "|", maxSplits: 2).map(String.init)
                guard parts.count == 3, let finished = Int(parts[1]) else { return refusal(422, "validation", "after: not a cursor") }
                guard parts[0] == "\(bound)" else { return refusal(422, "validation", "cursor does not match this filter and orderBy") }
                held = held.filter { ($0.finished, $0.id) < (finished, parts[2]) }
            }
            let rows = Array(held.prefix(first))
            var body: [String: Any] = ["head": seq, "generation": "g", "records": rows.map(threadJSON)]
            if held.count > first, let last = rows.last { body["cursor"] = "\(bound)|\(last.finished)|\(last.id)" }
            return .json(200, body)
        case Kinds.message:
            let threads = (named ?? []).compactMap { chats[$0] }
            if properties["role"] != nil {
                return page(threads.compactMap { chat in chat.messages.indices.first.map { messageJSON(chat, $0) } })
            }
            // Newest first, as the transcript asks, from the ordinal named
            // down where it names one.
            let before = properties["turn"]?["lte"] as? Int
            let rows = threads.flatMap { chat in
                chat.messages.indices.reversed().filter { index in before.map { index <= $0 } ?? true }.map { messageJSON(chat, $0) }
            }
            return page(Array(rows.prefix(first)))
        case Kinds.threadMark:
            guard keepsMarks else { return refusal(404, "not_found", "unknown kind \(kind)") }
            return page((named ?? []).compactMap { thread in marks[thread].map { markJSON(thread, $0) } })
        default:
            return page([])
        }
    }

    private func answer(_ request: URLRequest) -> Answer {
        let url = request.url!
        let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        let path = components?.percentEncodedPath ?? url.path
        let query = components?.queryItems ?? []
        let method = request.httpMethod ?? "GET"
        var asked: [String: String] = [:]
        for item in query { asked[item.name] = item.value ?? "" }
        return lock.withLock {
            kept.append("\(method) \(path)?\(components?.query ?? "")")
            if path.hasSuffix("/.well-known/substrate/server.json") {
                return .json(
                    200,
                    [
                        "server": ["version": "dev"],
                        "features": [
                            ["name": "agents", "stability": "experimental", "revision": features.agents],
                            ["name": "records", "stability": "stable", "revision": features.records],
                        ],
                    ])
            }
            if method == "GET", path.hasSuffix("/api/v1/records") {
                let text = query.first { $0.name == "filter" }?.value ?? "{}"
                let filter = (try? JSONSerialization.jsonObject(with: Data(text.utf8))) as? [String: Any] ?? [:]
                return list(filter, asked)
            }
            if method == "POST", path.hasSuffix("/api/v1/records") {
                let body = request.httpBody.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [String: Any] ?? [:]
                guard body["kind"] as? String == Kinds.threadMark, keepsMarks else {
                    return refusal(404, "not_found", "unknown kind \(body["kind"] as? String ?? "")")
                }
                let properties = body["properties"] as? [String: Any] ?? [:]
                written.append(properties)
                if failing > 0 {
                    failing -= 1
                    return refusal(500, "internal", "the write could not be kept")
                }
                if let refusing { return refusal(refusing.status, refusing.code, "refused") }
                let thread = properties["thread"] as? String ?? ""
                let read = properties["readThrough"] as? String
                if let read, chats[thread]?.messages.contains(where: { $0.id == read }) == false {
                    return refusal(422, "validation", "readThrough names no message of thread \(thread)")
                }
                // A server from before the flags does not declare them.
                if !keepsFlags, properties["pinned"] != nil || properties["archived"] != nil {
                    return refusal(422, "validation", "unknown property \"pinned\" or \"archived\"")
                }
                guard
                    let put = put(
                        thread, readThrough: read, markedUnread: properties["markedUnread"] as? Bool, pinned: properties["pinned"] as? Bool,
                        archived: properties["archived"] as? Bool)
                else {
                    return refusal(404, "not_found", "thread \(thread)")
                }
                return .json(put.created ? 201 : 200, put.json)
            }
            if method == "GET", path.hasSuffix("/api/v1/changes") {
                let kinds = Set(query.filter { $0.name == "kinds" }.flatMap { ($0.value ?? "").split(separator: ",").map(String.init) })
                return .watch(kinds, from: query.first { $0.name == "from" }?.value.flatMap(Int.init))
            }
            if method == "GET", let id = path.components(separatedBy: "/llm/thread/").dropFirst().first, let chat = chats[id] {
                return .json(200, threadJSON(chat))
            }
            return refusal(404, "not_found", "no route")
        }
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let stream = try await lines(for: request)
        return (try await stream.body(limit: .max), stream.head)
    }

    func lines(for request: URLRequest) async throws -> LineStream {
        let url = request.url!
        let text = url.absoluteString.removingPercentEncoding ?? url.absoluteString
        while lock.withLock({ held.contains { text.contains($0) } }), !Task.isCancelled {
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
        switch answer(request) {
        case let .json(status, body):
            let head = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
            let data = try JSONSerialization.data(withJSONObject: body)
            if request.httpMethod ?? "GET" == "GET", text.contains("\"\(Kinds.thread)\""), text.contains("\"mode\":{\"eq\"") {
                let act = lock.withLock { () -> (@Sendable () -> Void)? in
                    defer { afterThreads = nil }
                    return afterThreads
                }
                act?()
            }
            return LineStream(head: head, body: data)
        case let .watch(kinds, from):
            let head = HTTPURLResponse(
                url: url, statusCode: 200, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/x-ndjson"])!
            let id = UUID()
            let stream = AsyncThrowingStream<String, any Error> { continuation in
                lock.withLock {
                    continuation.yield(#"{"bookmark":\#(from ?? seq),"generation":"g"}"#)
                    // The real server refuses a watch naming a kind it does
                    // not declare, whole.
                    if kinds.contains(Kinds.threadMark), !keepsMarks {
                        continuation.yield(#"{"error":{"code":"validation","message":"unknown type \"\#(Kinds.threadMark)\""}}"#)
                        continuation.finish()
                        return
                    }
                    // A watch resumed from a cursor is told what it missed.
                    if let from {
                        for row in rows where row.seq > from && kinds.contains(row.kind) { continuation.yield(row.line) }
                    }
                    watchers[id] = (kinds, continuation)
                }
                // Not on the caller's thread: a stream ending while a row
                // is pushed to it holds its own lock, and `push` holds this
                // server's.
                continuation.onTermination = { [weak self] _ in
                    Task.detached { self?.lock.withLock { self?.watchers[id] = nil } }
                }
            }
            return LineStream(head: head, lines: stream)
        }
    }
}
