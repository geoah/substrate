import Foundation
import SubstrateCore
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import SubstrateChatCore

/// A reply's text on a screen.
private func text(_ screen: ConversationScreen?) -> String {
    for turn in (screen?.turns ?? []).reversed() {
        if case let .assistant(reply) = turn.body { return reply.text ?? "" }
    }
    return ""
}

/// What the stream said before it broke: the turn was admitted.
private let admitted = [#"{"kind":"thread","thread":"t1"}"#, #"{"kind":"delta","text":"Hel","turn":1,"at":0}"#]

/// The thread as it stands once its run ended as `status`, with the cause
/// and the words the server keeps beside a failure.
private func settled(_ status: String, cause: String? = nil, reason: String? = nil) -> String {
    var extra = ""
    if let cause { extra += #","cause":"\#(cause)""# }
    if let reason { extra += #","reason":"\#(reason)""# }
    return threadJSON(version: 4, status: status).replacingOccurrences(of: #""status":"\#(status)""#, with: #""status":"\#(status)"\#(extra)"#)
}

/// A stream that breaks after the server took the turn (a timeout, the
/// connection lost, the app suspended) is not a failed run: the thread is
/// read, attached to again while it runs, and only a run the server itself
/// settled as failed says so (the owner's report of 2026-11-17, thread
/// 01m7kt1p11egyvty9v9hcbx2zf, whose run finished `ok` minutes after the
/// app said it had failed).
@Suite("A stream that broke after its turn was admitted", .timeLimit(.minutes(10)))
struct RecoveryTests {
    /// Signed in at `agents`, a new chat, and "Hi" sent into a stream that
    /// breaks with `code` once the turn was admitted, or with no `code`
    /// closes without an error, as a proxy that went away closes it. The thread's read is
    /// held, so the test says what it finds; the id of the message's row
    /// comes back: the one it went under, or the server's below `agents` 3.
    private func broken(_ code: URLError.Code?, _ routes: [ChatServer.Route] = [], agents: Int = 5) async throws -> (
        ChatRig, ChildKey, Frames, Task<Void, Never>, String
    ) {
        let rig = ChatRig(baseRoutes() + routes + [.init(method: "POST", path: "/chat", answer: code.map { .broken(admitted, $0) } ?? .stream(admitted))])
        try await rig.signIn(agents: agents)
        let key = try await rig.newChat()
        let (frames, watching) = await rig.frames(key)
        rig.server.hold("/llm/thread/t1")
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        // Between the break and the read, the composer says it is
        // reconnecting, and nothing is called a failure.
        try await rig.harness.until { _ in await rig.live(key)?.reconnecting == true }
        let body = try #require(JSONValue.parse(rig.server.body("POST", "/chat")))
        #expect((body["messageId"] != nil) == (agents >= 3))
        let id = body["messageId"]?.string ?? "m1"
        let screen = try #require(await rig.conversation(key))
        #expect(screen.composer.status == ComposerStatus(kind: .reconnecting, words: "Reconnecting…"))
        #expect(screen.failures.strip == nil && screen.failures.turns.isEmpty)
        return (rig, key, frames, watching, id)
    }

    /// No screen the conversation drew called anything a failure.
    private func noFailure(_ frames: Frames) {
        for screen in frames.all {
            #expect(screen.failures.strip == nil, "a strip: \(String(describing: screen.failures.strip?.sentence))")
            #expect(screen.failures.turns.isEmpty, "a card: \(screen.failures.turns.values.map(\.sentence))")
        }
    }

    @Test(arguments: [(URLError.Code.timedOut, 5), (.networkConnectionLost, 5), (nil, 5), (.timedOut, 2)] as [(URLError.Code?, Int)])
    func aTurnStillRunningIsAttachedToAgainAndItsReplyLands(code: URLError.Code?, agents: Int) async throws {
        let attached = [
            #"{"kind":"thread","thread":"t1"}"#, #"{"kind":"delta","text":"Hello wor","turn":1,"at":0}"#,
            #"{"kind":"delta","text":"ld","turn":1,"at":9}"#,
            #"{"kind":"done","result":{"reply":"Hello world","thread":"t1","status":"ok","turns":2}}"#,
        ]
        let (rig, key, frames, watching, id) = try await broken(
            code, [.init(method: "GET", path: "/events", answer: .stream(attached, holdAfter: 2))], agents: agents)
        defer { watching.cancel() }
        // The thread still runs, and holds the message.
        rig.server.route("GET", "/llm/thread/t1", .json(200, threadJSON(version: 3, status: "running")))
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, page([messageRow(id, "user", "Hi", turn: 0)])))
        rig.server.letGo("/llm/thread/t1")
        try await rig.harness.until(advancing: 0.1) { _ in await text(rig.conversation(key)) == "Hello wor" }
        #expect(await rig.live(key)?.reconnecting == false)
        // The run ends, and its rows are read.
        rig.server.route("GET", "/llm/thread/t1", .json(200, threadJSON(version: 4)))
        rig.server.route(
            "GET", "/api/v1/records", query: "llm/message",
            .json(200, page([messageRow(id, "user", "Hi", turn: 0), messageRow("m2", "assistant", "Hello world", turn: 1)])))
        rig.server.release()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        await rig.harness.settle()
        #expect(await rig.turns(key) == ["Hi", "Hello world"])
        #expect(await rig.card(key) == nil)
        noFailure(frames)
        // The message was sent once: the attach is what read the rest.
        #expect(rig.server.count("/chat") == 1 && rig.server.count("/events") == 1)
    }

    @Test func aTurnThatFinishedWhileTheStreamWasDownShowsItsReply() async throws {
        let (rig, key, frames, watching, id) = try await broken(
            .timedOut, [.init(method: "GET", path: "/events", answer: .stream([#"{"kind":"idle","thread":"t1","status":"ok"}"#]))])
        defer { watching.cancel() }
        rig.server.route("GET", "/llm/thread/t1", .json(200, threadJSON(version: 4)))
        rig.server.route(
            "GET", "/api/v1/records", query: "llm/message",
            .json(200, page([messageRow(id, "user", "Hi", turn: 0), messageRow("m2", "assistant", "Hello", turn: 1)])))
        rig.server.letGo("/llm/thread/t1")
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        await rig.harness.settle()
        #expect(await rig.turns(key) == ["Hi", "Hello"])
        #expect(await rig.card(key) == nil)
        noFailure(frames)
        #expect(rig.server.count("/chat") == 1)
    }

    @Test func aTurnTheServerSettledAsFailedSaysWhyAndOffersTryAgain() async throws {
        let (rig, key, _, watching, id) = try await broken(
            .networkConnectionLost, [.init(method: "GET", path: "/events", answer: .stream([#"{"kind":"idle","thread":"t1","status":"error"}"#]))])
        defer { watching.cancel() }
        let failed = settled("error", cause: "provider_overloaded", reason: "llm: overloaded_error")
        rig.server.route("GET", "/llm/thread/t1", .json(200, failed))
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, page([messageRow(id, "user", "Hi", turn: 0)])))
        rig.server.letGo("/llm/thread/t1")
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        try await rig.harness.until { _ in await rig.card(key) != nil }
        let card = try #require(await rig.card(key))
        // The server's account of the run, never the connection's.
        #expect(card.sentence == "The provider is having trouble.")
        #expect(card.button == FailureButton(title: "Try Again", action: .resume))
        #expect(card.details == FailureDetails(title: "Details", label: "The server said", text: "llm: overloaded_error"))
        #expect(rig.server.count("/chat") == 1)
    }

    @Test func aStreamThatWillNotOpenAgainKeepsReconnectingAndNeverCallsTheRunFailed() async throws {
        let (rig, key, frames, watching, _) = try await broken(.timedOut, [.init(method: "GET", path: "/events", answer: .fail(.timedOut))])
        defer { watching.cancel() }
        // The thread cannot be read either at first: the server is not
        // there. Reconnecting is said all along.
        rig.server.route("GET", "/llm/thread/t1", once: true, .fail(.cannotConnectToHost))
        rig.server.letGo("/llm/thread/t1")
        try await rig.harness.until(advancing: 15) { _ in rig.server.count("/llm/thread/t1") >= 2 }
        rig.server.route("GET", "/llm/thread/t1", .json(200, threadJSON(version: 3, status: "running")))
        // It backs off and asks again, up to 15 seconds apart, for as long
        // as it takes.
        try await rig.harness.until(advancing: 15) { _ in rig.server.count("/events") >= 6 }
        let live = try #require(await rig.live(key))
        #expect(live.reconnecting && live.turnFailure == nil && live.chatFailure == nil)
        noFailure(frames)
        // From the break on, every screen said it was reconnecting.
        let since = frames.all.drop { $0.composer.status?.kind != .reconnecting }
        #expect(since.allSatisfy { $0.composer.status?.kind == .reconnecting }, "\(since.map { $0.composer.status?.words ?? "-" })")
        #expect(rig.server.count("/chat") == 1)
    }

    @Test func aTryAgainWhoseAnswerWasLostButWhoseRunFinishedLeavesNoFailure() async throws {
        let failed = threadJSON(version: 3, status: "error").replacingOccurrences(of: #""lastReply":"Hello""#, with: #""reason":"llm: overloaded""#)
        let threads = #"{"head":10,"generation":"g","records":[\#(failed)]}"#
        let rig = ChatRig(
            [.init(method: "GET", path: "/llm/thread/t1", answer: .json(200, failed))] + baseRoutes(threads: threads, status: "error") + [
                .init(method: "POST", path: "/continue", answer: .fail(.timedOut)),
                .init(method: "GET", path: "/events", answer: .stream([#"{"kind":"idle","thread":"t1","status":"ok"}"#])),
            ])
        try await rig.signIn(agents: 5)
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        #expect(await rig.card(key)?.button == FailureButton(title: "Try Again", action: .resume))
        // The continue's answer never came; the run it started finished.
        rig.server.route("GET", "/llm/thread/t1", .json(200, threadJSON(version: 5)))
        rig.server.route(
            "GET", "/api/v1/records", query: "llm/message",
            .json(
                200,
                page([
                    messageRow("m1", "user", "Hi", turn: 0), messageRow("m2", "assistant", "Hello", turn: 1),
                    messageRow("m3", "assistant", "Hello again", turn: 2),
                ])))
        try await rig.send(.continueTurn, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        await rig.harness.settle()
        #expect(await rig.turns(key).last == "Hello again")
        #expect(await rig.live(key)?.turnFailure == nil)
        #expect(await rig.card(key) == nil)
        #expect(rig.server.count("/continue") == 1)
    }

    @Test func aTryAgainWhoseAnswerWasLostAndWhoseRunFailedShowsTheServersFailure() async throws {
        let failed = threadJSON(version: 3, status: "error").replacingOccurrences(of: #""lastReply":"Hello""#, with: #""reason":"llm: overloaded""#)
        let threads = #"{"head":10,"generation":"g","records":[\#(failed)]}"#
        let rig = ChatRig(
            [.init(method: "GET", path: "/llm/thread/t1", answer: .json(200, failed))] + baseRoutes(threads: threads, status: "error") + [
                .init(method: "POST", path: "/continue", answer: .fail(.timedOut)),
                .init(method: "GET", path: "/events", answer: .stream([#"{"kind":"idle","thread":"t1","status":"error"}"#])),
            ])
        try await rig.signIn(agents: 5)
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        // The continue's answer never came; its run failed again, and the
        // server says why.
        rig.server.route("GET", "/llm/thread/t1", .json(200, settled("error", cause: "provider_billing", reason: "llm: credit balance is too low")))
        try await rig.send(.continueTurn, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        try await rig.harness.until { _ in await rig.card(key)?.sentence == "The provider's account is out of credit." }
        let card = try #require(await rig.card(key))
        #expect(card.details?.text == "llm: credit balance is too low" && card.button == FailureButton(title: "Try Again", action: .resume))
        #expect(await rig.live(key)?.turnFailure == nil)
    }

    @Test func aProbeThatClosesBeforeItsFirstEventLeavesTheWaitQuiet() async throws {
        let running = threadJSON(version: 3, status: "running")
        let rig = ChatRig(
            baseRoutes(threads: #"{"head":10,"generation":"g","records":[\#(running)]}"#, status: "running")
                + [.init(method: "GET", path: "/events", answer: .stream([]))])
        rig.server.route("GET", "/events", once: true, .stream([#"{"kind":"idle","thread":"t1","status":"running"}"#]))
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, page([messageRow("m1", "user", "Hi", turn: 0)])))
        try await rig.signIn(agents: 5)
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        let (frames, watching) = await rig.frames(key)
        defer { watching.cancel() }
        try await rig.harness.until { _ in await rig.live(key)?.phase == .background }
        // The wait asks for a stream again; it closes with nothing said.
        try await rig.harness.until(advancing: 6) { _ in rig.server.count("/events") >= 3 }
        await rig.harness.settle()
        let live = try #require(await rig.live(key))
        #expect(live.phase == .background && !live.reconnecting && live.lastFailure == nil)
        #expect(frames.all.allSatisfy { $0.composer.status?.kind != .reconnecting })
    }
}
