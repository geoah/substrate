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
    /// The turn settled: the whole reply, where the server gives it.
    case done(String?)
    /// It could not be asked, or the turn failed: why, in the app's words.
    case failed(String)
}

/// The app's own small client for the assistant: one chat turn with the
/// records it is about as context (at most eight), read as an ndjson
/// stream. A failure is an event said in place; nothing here retries a
/// POST, since a repeated one would run the turn again.
enum Assistant {
    static let agent = TodayKinds.assistant
    static let contextLimit = 8
    /// The stream is silent between heartbeats; past this it is dead.
    static let quiet: TimeInterval = 75

    static func body(message: String, context: [String], timezone: String, messageID: String) -> JSONValue {
        var body: JSONObject = ["thread": "", "message": .string(message), "messageId": .string(messageID), "timezone": .string(timezone)]
        if !context.isEmpty { body["context"] = .array(context.prefix(contextLimit).map(JSONValue.string)) }
        return .object(body)
    }

    static func request(_ client: SubstrateClient, message: String, context: [String], timezone: String, messageID: String) throws -> URLRequest {
        try client.request(
            "POST", "api/v1/substrate.reamde.dev/core/agent/\(segment(agent))/chat",
            body: .jsonValue(body(message: message, context: context, timezone: timezone, messageID: messageID)), accept: "application/x-ndjson",
            timeout: quiet)
    }

    /// One stream line as an event; nil for a heartbeat, a tool's line or
    /// anything this build does not name.
    static func parse(_ line: String) -> AssistantEvent? {
        guard let data = line.data(using: .utf8), let value = try? JSONValue(data: data), let kind = value["kind"]?.text else { return nil }
        switch kind {
        case "thread": return value["thread"]?.text.map(AssistantEvent.thread)
        case "delta": return value["text"]?.string.map(AssistantEvent.delta)
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
        case "error": return .failed(value["text"]?.text ?? value["message"]?.text ?? "Moss could not answer.")
        case "lagged": return .failed("The answer fell behind. It is in Chat, under Moss.")
        default: return nil
        }
    }

    /// Asks, and yields the turn's events until it settles. Every way it
    /// can fail ends in one `.failed`.
    static func ask(_ client: SubstrateClient, message: String, context: [String], timezone: String, messageID: String, app: String)
        -> AsyncStream<AssistantEvent>
    {
        AsyncStream { continuation in
            let task = Task {
                do {
                    let lines = try await client.transport.lines(
                        for: request(client, message: message, context: context, timezone: timezone, messageID: messageID))
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
        guard case let .meeting(id) = subject, let meeting = day.meetings.first(where: { $0.id == id }) else { return text }
        return
            "About the meeting \"\(meeting.title)\" at \(Words.clock(meeting.start, day.calendar)) \(Words.day(meeting.start, now: day.now, day.calendar)): \(text)"
    }
}
