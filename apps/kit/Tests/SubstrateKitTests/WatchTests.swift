import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateTesting
import Testing

private let taskKind = "example.com/tasks/task"

private func row(_ seq: Int, _ id: String, _ op: String = "put") -> String {
    #"{"seq":\#(seq),"ts":"2026-11-03T09:00:00Z","kind":"\#(taskKind)","recordId":"\#(id)","op":"\#(op)"}"#
}

private func follower(_ server: ScriptedLines) -> WatchFollower {
    WatchFollower(client: SubstrateClient(server: URL(string: "https://ada.example.com")!, token: "t", transport: server),
                  kinds: [taskKind], app: "Substrate Tasks", pause: { _ in })
}

private func query(_ request: URLRequest) -> [String: String] {
    let items = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems ?? []
    return Dictionary(items.map { ($0.name, $0.value ?? "") }) { first, _ in first }
}

/// Reads events until `count` have come or the stream ends.
private func take(_ count: Int, from stream: AsyncStream<WatchEvent>) async -> [WatchEvent] {
    var out: [WatchEvent] = []
    for await event in stream {
        out.append(event)
        if out.count == count { break }
    }
    return out
}

@Suite("The watch")
struct WatchTests {
    @Test func aLineIsABookmarkARowOrATerminalFrameAndAHeartbeatIsNothing() {
        #expect(WatchLine.parse(#"{"bookmark":41,"generation":"g1"}"#) == .bookmark(WatchCursor(seq: 41, generation: "g1")))
        #expect(WatchLine.parse(row(42, "a", "delete")) == .row(WatchRow(seq: 42, kind: taskKind, recordID: "a", op: "delete")))
        #expect(WatchLine.parse(#"{"error":{"code":"compacted","message":"gone"}}"#) == .failed(code: "compacted", message: "gone"))
        #expect(WatchLine.parse("{}") == nil)
        #expect(WatchLine.parse("") == nil)
        #expect(WatchLine.parse("not json") == nil)
    }

    @Test func itFollowsFromTheLiveHeadThroughHeartbeats() async throws {
        let server = ScriptedLines()
        server.answer(lines: [#"{"bookmark":40,"generation":"g1"}"#, "{}", row(41, "a"), "{}", row(42, "b", "delete")], then: .open)
        let events = await take(3, from: follower(server).follow(from: nil))
        #expect(events == [
            .opened(WatchCursor(seq: 40, generation: "g1")),
            .row(WatchRow(seq: 41, kind: taskKind, recordID: "a", op: "put")),
            .row(WatchRow(seq: 42, kind: taskKind, recordID: "b", op: "delete")),
        ])
        let sent = try #require(server.requests.first)
        #expect(sent.value(forHTTPHeaderField: "Accept") == "application/x-ndjson")
        #expect(query(sent) == ["kinds": taskKind, "watch": "1"])
    }

    @Test func aDroppedConnectionOpensAgainFromTheLastRowUnderItsGeneration() async throws {
        let server = ScriptedLines()
        server.answer(lines: [#"{"bookmark":40,"generation":"g1"}"#, row(41, "a")], then: .fail(.networkConnectionLost))
        server.answer(lines: [#"{"bookmark":41,"generation":"g1"}"#, row(42, "b")], then: .open)
        let events = await take(5, from: follower(server).follow(from: WatchCursor(seq: 39, generation: "g1")))
        #expect(events.count == 5)
        guard case .interrupted = events[2] else { Issue.record("no interruption: \(events)"); return }
        #expect(events.last == .row(WatchRow(seq: 42, kind: taskKind, recordID: "b", op: "put")))
        #expect(query(server.requests[0])["from"] == "39")
        #expect(query(server.requests[1])["from"] == "41")
        #expect(query(server.requests[1])["generation"] == "g1")
    }

    @Test func aGoneCursorOrACompactedFrameAsksForARelist() async throws {
        let gone = ScriptedLines()
        gone.answer(status: 410, lines: [#"{"error":{"code":"gone","message":"below the retention horizon"}}"#])
        #expect(await take(5, from: follower(gone).follow(from: WatchCursor(seq: 1, generation: "old"))) == [.relist])

        let compacted = ScriptedLines()
        compacted.answer(lines: [#"{"bookmark":3,"generation":"g"}"#, #"{"error":{"code":"compacted","message":"x"}}"#])
        #expect(await take(5, from: follower(compacted).follow(from: nil)) == [.opened(WatchCursor(seq: 3, generation: "g")), .relist])
    }

    @Test func anotherTerminalFrameOrARefusedTokenEndsTheFollow() async throws {
        let stopped = ScriptedLines()
        stopped.answer(lines: [#"{"error":{"code":"internal","message":"The feed stopped."}}"#])
        #expect(await take(5, from: follower(stopped).follow(from: nil)) == [.ended(.refused("The feed stopped."))])

        let expired = ScriptedLines()
        expired.answer(status: 401, lines: [#"{"error":{"code":"auth","message":"invalid token"}}"#])
        #expect(await take(5, from: follower(expired).follow(from: nil)) == [.ended(.signedOut)])
    }

    @Test func aRedirectIsRefusedNeverFollowed() async throws {
        let server = ScriptedLines()
        server.answer(status: 307, lines: [])
        let events = await take(5, from: follower(server).follow(from: nil))
        guard case let .ended(.refused(words))? = events.first else { Issue.record("not refused: \(events)"); return }
        #expect(words.contains("never follows"))
        #expect(server.requests.count == 1)
    }

    @Test func aBusyServerIsTriedAgain() async throws {
        let server = ScriptedLines()
        server.answer(status: 503, lines: [#"{"error":{"code":"unavailable","message":"shutting down"}}"#])
        server.answer(lines: [#"{"bookmark":7,"generation":"g"}"#], then: .open)
        let events = await take(2, from: follower(server).follow(from: nil))
        guard case .interrupted = events.first else { Issue.record("not interrupted: \(events)"); return }
        #expect(events.last == .opened(WatchCursor(seq: 7, generation: "g")))
    }

    @Test func aScriptedServerAnswersItsLinesWhole() async throws {
        let stub = StubTransport().on("GET", "/api/v1/changes", json: "a\nb\n\nc")
        let stream = try await stub.lines(for: URLRequest(url: URL(string: "https://ada.example.com/api/v1/changes")!))
        var lines: [String] = []
        for try await line in stream.lines { lines.append(line) }
        #expect(lines == ["a", "b", "c"])
    }
}
