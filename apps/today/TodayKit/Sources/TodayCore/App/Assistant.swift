import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// One line of an assistant turn, as the root folds it.
public enum AssistantEvent: Codable, Equatable, Sendable {
    /// The thread the turn opened.
    case thread(String)
    /// More of the answer.
    case delta(String)
    /// The assistant began a step of work: what it said before it is a
    /// paragraph of its own.
    case step
    /// The turn settled: the whole reply, where the server gives it.
    case done(String?)
    /// It could not be asked, or the turn failed: why, in the app's words.
    case failed(String)
}

/// The app's own small client for the assistant: one chat turn in the
/// thread of its conversation, with the records it is about as context
/// (at most eight), read as an ndjson stream. A failure is an event said
/// in place. Nothing here sends a turn that ran a second time: the one
/// POST it repeats is one the server refused before it wrote anything.
enum Assistant {
    static let agent = TodayKinds.assistant
    static let contextLimit = 8
    /// The stream is silent between heartbeats; past this it is dead.
    static let quiet: TimeInterval = 75

    /// How many times, and how far apart, the message asked before is
    /// looked for: its row is written a moment after it is sent.
    static let lookups = 3
    static let lookupGap: UInt64 = 400_000_000

    /// - Parameter thread: the thread the message continues; nil opens one.
    static func body(message: String, context: [String], timezone: String, messageID: String, thread: String? = nil) -> JSONValue {
        var body: JSONObject = [
            "thread": .string(thread ?? ""), "message": .string(message), "messageId": .string(messageID), "timezone": .string(timezone),
        ]
        if !context.isEmpty { body["context"] = .array(context.prefix(contextLimit).map(JSONValue.string)) }
        return .object(body)
    }

    static func request(
        _ client: SubstrateClient, message: String, context: [String], timezone: String, messageID: String, thread: String? = nil
    ) throws -> URLRequest {
        try client.request(
            "POST", "api/v1/substrate.reamde.dev/core/agent/\(segment(agent))/chat",
            body: .jsonValue(body(message: message, context: context, timezone: timezone, messageID: messageID, thread: thread)),
            accept: "application/x-ndjson", timeout: quiet)
    }

    /// The thread a message the app sent is in, read from the message's
    /// own row, which the server writes under the id the app chose. Nil
    /// where it never arrived, or cannot be read.
    static func thread(of messageID: String, _ client: SubstrateClient) async -> String? {
        for attempt in 0 ..< lookups {
            if attempt > 0 { try? await Task.sleep(nanoseconds: lookupGap) }
            guard !Task.isCancelled else { return nil }
            if let record = try? await client.record(TodayKinds.message, messageID) { return record["thread"]?.referenceID }
        }
        return nil
    }

    /// One stream line as an event; nil for a heartbeat, a tool's result or
    /// anything this build does not name.
    static func parse(_ line: String) -> AssistantEvent? {
        guard let data = line.data(using: .utf8), let value = try? JSONValue(data: data), let kind = value["kind"]?.text else { return nil }
        switch kind {
        case "thread": return value["thread"]?.text.map(AssistantEvent.thread)
        case "delta": return value["text"]?.string.map(AssistantEvent.delta)
        case "toolStarted": return .step
        case "done":
            if let result = value["result"] {
                let status = result["status"]?.text
                if let status, status != "completed", status != "done", status != "ok", let reason = result["reason"]?.text {
                    return .failed("Moss stopped before it finished: \(reason)")
                }
                return .done(result["reply"]?.string)
            }
            if let text = value["text"]?.text { return .failed(text) }
            return .done(nil)
        // The server says why in `error`; a line with its words elsewhere is read too.
        case "error": return .failed(value["error"]?.text ?? value["text"]?.text ?? value["message"]?.text ?? "Moss could not answer.")
        case "lagged": return .failed("The answer fell behind. It is in Chat, under Moss.")
        default: return nil
        }
    }

    /// Asks, and yields the turn's events until it settles. Every way it
    /// can fail ends in one `.failed`.
    /// - Parameters:
    ///   - thread: the conversation's thread, which the message continues.
    ///   - after: with no thread, the id of what was last asked in the
    ///     conversation: the thread that message is in is continued.
    static func ask(
        _ client: SubstrateClient, message: String, context: [String], timezone: String, messageID: String, thread: String? = nil,
        after: String? = nil, app: String
    ) -> AsyncStream<AssistantEvent> {
        AsyncStream { continuation in
            let task = Task {
                do {
                    var thread = thread
                    if thread == nil, let after { thread = await Self.thread(of: after, client) }
                    func send(_ thread: String?) async throws -> LineStream {
                        try await client.transport.lines(
                            for: request(client, message: message, context: context, timezone: timezone, messageID: messageID, thread: thread))
                    }
                    var lines = try await send(thread)
                    // The thread is gone: the conversation opens a new one. The server refuses a thread it does not
                    // hold before it writes the message, so nothing ran and the same send is made once more.
                    if lines.head.statusCode == 404, thread != nil {
                        for try await _ in lines.lines {}
                        lines = try await send(nil)
                    }
                    guard lines.head.statusCode == 200 else {
                        var body = Data()
                        for try await line in lines.lines { body.append(contentsOf: line.utf8) }
                        throw SubstrateClient.failure(SubstrateClient.Response(data: body, response: lines.head))
                    }
                    var settled = false
                    for try await line in lines.lines {
                        guard let event = parse(line) else { continue }
                        continuation.yield(event)
                        if case .done = event { settled = true }
                        if case .failed = event { settled = true }
                        if settled { break }
                    }
                    if !settled { continuation.yield(.failed("The answer stopped before it was finished. It may be in Chat, under Moss.")) }
                } catch is CancellationError {
                } catch {
                    continuation.yield(.failed("Moss could not be reached. " + explain(error, app: app, server: client.server)))
                }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    /// The records that go with a question, by what it is about.
    static func context(_ subject: MossSubject, day: Day) -> [String] {
        switch subject {
        case let .task(id): return [day.task(id)?.path].compactMap { $0 }
        case let .project(id): return [day.list(id)?.path].compactMap { $0 }
        case let .person(path): return day.person(path) == nil ? [] : [path]
        // A meeting's occurrence is computed and holds no row of its own:
        // the question names it in words instead.
        case .meeting, .day: return []
        }
    }

    /// The message as it is sent: the owner's words, and for a meeting what
    /// it is, since no record goes with it.
    static func message(_ subject: MossSubject, _ text: String, day: Day) -> String {
        guard case let .meeting(id) = subject, let meeting = day.meeting(id) else { return text }
        return
            "About the meeting \"\(meeting.title)\" at \(Words.clock(meeting.start, day.calendar)) \(Words.day(meeting.start, now: day.now, day.calendar)): \(text)"
    }
}
