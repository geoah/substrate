import Foundation
import SubstrateCore
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

/// What a test watched a conversation's screen go through.
final class Frames: @unchecked Sendable {
    private let lock = NSLock()
    private var kept: [ConversationScreen] = []
    func add(_ screen: ConversationScreen) { lock.withLock { if kept.last != screen { kept.append(screen) } } }
    var all: [ConversationScreen] { lock.withLock { kept } }
}

extension ChatRig {
    /// Every screen the conversation presents from here on.
    func frames(_ key: ChildKey) async -> (Frames, Task<Void, Never>) {
        let presentations = await runtime.presentations()
        let frames = Frames()
        let watching = Task {
            for await tree in presentations {
                if let screen = tree.conversations[key] { frames.add(screen) }
            }
        }
        return (frames, watching)
    }
}

/// A reply's text on a screen.
private func text(_ screen: ConversationScreen?) -> String {
    for turn in (screen?.turns ?? []).reversed() {
        if case let .assistant(reply) = turn.body { return reply.text ?? "" }
    }
    return ""
}

private let running = threadJSON(version: 3, status: "running")
/// A page of rows as the transcript read answers it: newest first.
func page(_ rows: [Record]) -> String {
    let records = rows.reversed().map { String(decoding: (try? ValueCoding.encode($0))?.data() ?? Data(), as: UTF8.self) }
    return #"{"head":10,"generation":"g","records":[\#(records.joined(separator: ","))]}"#
}

@Suite("A stream joined again", .timeLimit(.minutes(10)))
struct StreamTests {
    /// A reply held part way, the app sent away and back, and the stream
    /// joined again: from a server that says where text belongs, and from
    /// one that does not.
    @Test(arguments: [5, 2])
    func aReturnToTheFrontEmptiesNothingAndTheReplyGoesOnWhereItWas(agents: Int) async throws {
        let at = { (position: Int) in agents >= 5 ? #","at":\#(position)"# : "" }
        let sent = [
            #"{"kind":"thread","thread":"t1"}"#, #"{"kind":"reasoning","text":"Let me think.","turn":1\#(at(0))}"#,
            #"{"kind":"delta","text":"Hel","turn":1\#(at(0))}"#,
            // What the first stream would have said had the app stayed: it
            // was let go, and nothing of it may land.
            #"{"kind":"delta","text":"LOST","turn":1\#(at(3))}"#,
            #"{"kind":"done","result":{"reply":"HelLOST","thread":"t1","status":"ok","turns":2}}"#,
        ]
        // The attach replays the turn from its start, then goes on live.
        let attached = [
            #"{"kind":"thread","thread":"t1"}"#, #"{"kind":"reasoning","text":"Let me think.","turn":1\#(at(0))}"#,
            #"{"kind":"delta","text":"Hello wor","turn":1\#(at(0))}"#, #"{"kind":"delta","text":"ld","turn":1\#(at(9))}"#,
            #"{"kind":"done","result":{"reply":"Hello world","thread":"t1","status":"ok","turns":2}}"#,
        ]
        let rig = ChatRig(
            baseRoutes() + [
                .init(method: "POST", path: "/chat", answer: .stream(sent, holdAfter: 3)),
                .init(method: "GET", path: "/events", answer: .stream(attached, holdAfter: 3)),
            ])
        try await rig.signIn(agents: agents)
        let key = try await rig.newChat()
        let (frames, watching) = await rig.frames(key)
        defer { watching.cancel() }
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until(advancing: 0.1) { _ in await text(rig.conversation(key)) == "Hel" }
        let before = try #require(await rig.conversation(key))
        // Where the server takes a message's id (`agents` 3), the message
        // is drawn under it from the press and its row has it; below, the
        // row has an id of the server's and is found by its words.
        let minted = rig.server.bodies("POST", "/chat").first.flatMap { body in
            (try? JSONSerialization.jsonObject(with: Data(body.utf8)) as? [String: Any])?["messageId"] as? String
        }
        #expect((minted != nil) == (agents >= 3))
        let message = minted ?? "sent-1"
        let row = minted ?? "m1"
        #expect(before.turns.map(\.id) == [message, "turn-1"])

        // Away and back: the thread runs, and its rows hold the message.
        rig.server.route("GET", "/llm/thread/t1", .json(200, running))
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, page([messageRow(row, "user", "Hi", turn: 0)])))
        await rig.harness.send(.phase(.background))
        await rig.harness.send(.phase(.active))
        try await rig.harness.until(advancing: 0.1) { _ in await text(rig.conversation(key)) == "Hello wor" }
        #expect(rig.server.count("/events") == 1)
        // The message's row took its turn's place, and its id: the same
        // id where the message was sent under one, so nothing is kept to
        // say which is which.
        #expect(await rig.live(key)?.aliases == (minted == nil ? ["m1": "sent-1"] : [:]))
        #expect(await rig.live(key)?.provisional.turns.map(\.id) == ["turn-1"])

        // The reply ends, and its rows are read.
        rig.server.route("GET", "/llm/thread/t1", .json(200, threadJSON(version: 4)))
        rig.server.route(
            "GET", "/api/v1/records", query: "llm/message",
            .json(200, page([messageRow(row, "user", "Hi", turn: 0), messageRow("m2", "assistant", "Hello world", turn: 1, reasoning: "Let me think.")])))
        rig.server.release()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        await rig.harness.settle()
        let after = try #require(await rig.conversation(key))
        #expect(await rig.turns(key) == ["Hi", "Hello world"])
        #expect(await rig.live(key)?.provisional.isEmpty == true)
        #expect(await rig.live(key)?.aliases == (minted == nil ? ["m1": "sent-1", "m2": "turn-1"] : ["m2": "turn-1"]))

        // On no screen between did a turn go, change its id, or lose text.
        var shown = ""
        var thought = ""
        var ids: [String] = []
        for screen in frames.all {
            #expect(Array(screen.turns.map(\.id).prefix(ids.count)) == ids, "a turn went or changed its id")
            ids = screen.turns.map(\.id)
            let now = text(screen)
            #expect(now.hasPrefix(shown), "the reply lost text: “\(shown)” then “\(now)”")
            shown = now
            if case let .assistant(reply)? = screen.turns.last?.body, let reasoning = reply.reasoning {
                #expect(reasoning.text.hasPrefix(thought))
                thought = reasoning.text
            }
        }
        #expect(ids == [message, "turn-1"] && after.turns.map(\.id) == ids && shown == "Hello world")
    }

    @Test func aTurnThatSettlesBetweenTheReadAndTheStreamIsReadAgainAndNothingIsLost() async throws {
        let rig = ChatRig(
            baseRoutes() + [
                .init(method: "POST", path: "/chat", answer: .stream(Array(replyStream.prefix(2)), holdAfter: 2)),
                .init(method: "GET", path: "/events", answer: .stream([#"{"kind":"idle","thread":"t1","status":"ok"}"#])),
            ])
        try await rig.signIn()
        let key = try await rig.newChat()
        let (frames, watching) = await rig.frames(key)
        defer { watching.cancel() }
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until(advancing: 0.1) { _ in await text(rig.conversation(key)) == "Hel" }
        // The attach's read sees the thread still running and only the
        // message's row; by the time its stream opens the turn has settled.
        rig.server.route("GET", "/llm/thread/t1", once: true, .json(200, running))
        rig.server.route("GET", "/api/v1/records", query: "llm/message", once: true, .json(200, page([messageRow("m1", "user", "Hi", turn: 0)])))
        await rig.harness.send(.phase(.background))
        await rig.harness.send(.phase(.active))
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        await rig.harness.settle()
        #expect(await rig.turns(key) == ["Hi", "Hello"])
        #expect(await rig.conversation(key)?.turns.map(\.id) == ["sent-1", "turn-1"])
        // What the stream had drawn stood until the rows that hold it landed.
        var shown = ""
        var count = 0
        for screen in frames.all {
            #expect(text(screen).hasPrefix(shown) && screen.turns.count >= count)
            (shown, count) = (text(screen), screen.turns.count)
        }
    }

    @Test func aReaderThatFellBehindAttachesAgainAtOnceAndNothingFailed() async throws {
        let lagged = [
            #"{"kind":"thread","thread":"t1"}"#, #"{"kind":"delta","text":"Hello wor","turn":1,"at":0}"#,
            #"{"kind":"lagged","thread":"t1"}"#,
        ]
        let rest = [
            #"{"kind":"thread","thread":"t1"}"#, #"{"kind":"delta","text":"Hello world","turn":1,"at":0}"#,
            #"{"kind":"done","result":{"reply":"Hello world","thread":"t1","status":"ok","turns":2}}"#,
        ]
        let rig = ChatRig(
            baseRoutes(
                threads: #"{"head":10,"generation":"g","records":[\#(running)]}"#, status: "running")
                + [.init(method: "GET", path: "/events", answer: .stream(rest, holdAfter: 2))])
        rig.server.route("GET", "/events", once: true, .stream(lagged))
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, page([messageRow("m1", "user", "Hi", turn: 0)])))
        try await rig.signIn(agents: 5)
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        let (frames, watching) = await rig.frames(key)
        defer { watching.cancel() }
        // No clock moves: the second attach follows the first at once.
        try await rig.harness.until { _ in
            let held = await rig.live(key)?.provisional.writing?.content
            return rig.server.count("/events") == 2 && held == "Hello world"
        }
        try await rig.harness.until(advancing: 0.1) { _ in await text(rig.conversation(key)) == "Hello world" }
        let live = try #require(await rig.live(key))
        #expect(live.phase == .streaming && live.lastFailure == nil && live.turnFailure == nil && live.chatFailure == nil)
        #expect(frames.all.allSatisfy { $0.composer.status?.kind != .reconnecting && $0.failures.strip == nil && $0.failures.turns.isEmpty })
        rig.server.release()
    }

    /// A server at `agents` 6 says of a message sent while a turn runs
    /// that it was queued and then taken (`messageQueued`,
    /// `messageTaken`). This build draws no queued message: the two lines
    /// are kinds it does not know, read as no event at all, and the reply
    /// around them is what it is without them.
    @Test func aQueuedMessagesEventsAreReadAsNoneAndTheReplyIsWhole() async throws {
        let lines = [
            #"{"kind":"thread","thread":"t1"}"#, #"{"kind":"delta","text":"Hello ","turn":1,"at":0}"#,
            #"{"kind":"messageQueued","id":"q1","turn":2,"text":"And the mint?"}"#, #"{"kind":"delta","text":"world","turn":1,"at":6}"#,
            #"{"kind":"messageTaken","id":"q1","turn":2}"#,
            #"{"kind":"done","result":{"reply":"Hello world","thread":"t1","status":"ok","turns":2}}"#,
        ]
        #expect(AgentEvent.parse(lines[2]) == nil && AgentEvent.parse(lines[4]) == nil)
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(lines, holdAfter: 5))])
        try await rig.signIn(agents: 6)
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until(advancing: 0.1) { _ in await text(rig.conversation(key)) == "Hello world" }
        let live = try #require(await rig.live(key))
        #expect(live.provisional.turns.map(\.role) == [.user, .assistant] && live.provisional.turns.last?.content == "Hello world")
        #expect(live.phase == .streaming && live.lastFailure == nil && live.turnFailure == nil)
        rig.server.release()
    }

    @Test func aStreamMayBeQuietForTwoOfItsServersHeartbeats() async throws {
        for (agents, quiet) in [(5, 25.0), (4, 75.0), (0, 75.0)] {
            let conversation = Conversation(
                key: ChildKey("k"), seed: .init(thread: "t1", agent: helper, draft: "", listen: false), facts: nil, online: true,
                keyless: nil, newestTurn: nil, visible: true, pollNudge: 0, resumeNudge: 0, continuing: nil, agents: agents)
            #expect(conversation.quiet == quiet)
        }
        // And the request says so.
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        try await rig.signIn(agents: 5)
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(rig.server.timeout("POST", "/chat") == 25)
    }

    /// A chat whose thread runs with no stream for it yet: a resumed run
    /// waiting for a place, as a server at `agents` 5 has them.
    private func waiting(agents: Int) async throws -> (ChatRig, ChildKey) {
        let rig = ChatRig(
            baseRoutes(threads: #"{"head":10,"generation":"g","records":[\#(running)]}"#, status: "running")
                + [.init(method: "GET", path: "/events", answer: .stream(Array(replyStream.dropLast()), holdAfter: 3))])
        rig.server.route("GET", "/events", once: true, .stream([#"{"kind":"idle","thread":"t1","status":"running"}"#]))
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, page([messageRow("m1", "user", "Hi", turn: 0)])))
        try await rig.signIn(agents: agents)
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.live(key)?.phase == .background }
        return (rig, key)
    }

    @Test func aChatsRunWithNoStreamYetIsAskedForAgainAndStreamsFromRevisionFive() async throws {
        let (rig, key) = try await waiting(agents: 5)
        #expect(rig.server.count("/events") == 1)
        // The wait asks for the stream again, and this time there is one.
        try await rig.harness.until(advancing: 6) { _ in await rig.live(key)?.phase == .streaming }
        #expect(rig.server.count("/events") == 2)
        try await rig.harness.until(advancing: 0.1) { _ in await text(rig.conversation(key)) == "Hello" }
        #expect(await rig.conversation(key)?.streaming == true)
        rig.server.release()
    }

    @Test func belowRevisionFiveItsRowsAreReadAsBefore() async throws {
        let (rig, key) = try await waiting(agents: 4)
        let reads = rig.server.count("/llm/thread/t1")
        try await rig.harness.until(advancing: 6) { _ in rig.server.count("/llm/thread/t1") > reads }
        #expect(rig.server.count("/events") == 1)
        #expect(await rig.live(key)?.phase == .background)
    }

    @Test func anAutomatedRunIsNeverAskedForAStream() async throws {
        let manual = running.replacingOccurrences(of: #""mode":"chat""#, with: #""mode":"schedule""#)
        let rig = ChatRig(
            baseRoutes(threads: emptyPage, status: "running") + [
                .init(method: "GET", path: "/events", answer: .stream([#"{"kind":"idle","thread":"t1","status":"running"}"#]))
            ])
        rig.server.route("GET", "/llm/thread/t1", .json(200, manual))
        try await rig.signIn(agents: 5)
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.live(key)?.phase == .background && rig.server.count("/events") == 1 }
        let reads = rig.server.count("/llm/thread/t1")
        try await rig.harness.until(advancing: 6) { _ in rig.server.count("/llm/thread/t1") > reads }
        #expect(rig.server.count("/events") == 1)
    }

    @Test func theComposerSaysWhatIsHappeningFromTheSendToTheEnd() async throws {
        let stream = [
            #"{"kind":"thread","thread":"t1"}"#, #"{"kind":"reasoning","text":"Let me look.","turn":1,"at":0}"#,
            #"{"kind":"delta","text":"I'll check. ","turn":1,"at":0}"#,
            #"{"kind":"toolStarted","id":"c1","tool":"web_search","turn":2,"args":"{}"}"#,
            #"{"kind":"toolFinished","id":"c1","tool":"web_search","turn":2,"ok":true,"output":"{}"}"#,
            #"{"kind":"delta","text":"Hello","turn":3,"at":0}"#,
            #"{"kind":"done","result":{"reply":"Hello","thread":"t1","status":"ok","turns":4}}"#,
        ]
        let rig = ChatRig(baseRoutes())
        try await rig.signIn()
        let key = try await rig.newChat()
        let app = rig.runtime.controller
        let conversation = try #require(app.children(in: await rig.harness.state)[key])
        var live = conversation.initialState
        let now = Date(timeIntervalSince1970: 1000)
        func status() -> ComposerStatus? {
            ChatApp.status(conversation.present(PresenterInput(state: live, now: now)), agent: nil)
        }
        #expect(status() == nil)
        live = try conversation.apply(.init(.sent(message: "Hi", agent: helper, at: now, baseline: nil, fromDraft: true, id: nil)), to: live)
        #expect(status() == ComposerStatus(kind: .connecting, words: "Connecting…"))
        var said: [String] = []
        for line in stream {
            let event = try #require(AgentEvent.parse(line))
            live = try conversation.apply(.init(event.kind == .thread ? .named("t1") : .streamed(event, at: now)), to: live)
            said.append(status()?.words ?? "")
        }
        #expect(said == ["Thinking…", "Thinking…", "Writing…", "Running web search…", "Thinking…", "Writing…", "Writing…"])
        // The substrate's own functions are said by what they do, and a
        // sub-agent by its name.
        let query = ToolCall(id: "q", name: "query", callable: "\(Kinds.function)/\(HostFunction.query)", arguments: "{}")
        #expect(TurnPresenter.runningWords(query, agent: nil) == "Looking through your records…")
        let librarian = ToolCall(id: "s", name: "librarian", callable: "\(Kinds.agent)/substrate.reamde.dev/assistant/librarian", arguments: "{}")
        #expect(TurnPresenter.runningWords(librarian, agent: nil) == "Asking Librarian…")
        // Stop is said while it is asked; and once the reply ended nothing is.
        var stopping = live
        stopping.stopping = true
        live = stopping
        #expect(status()?.kind == .stopping)
        live.stopping = false
        live = try conversation.apply(.init(.streamEnded(nil, outcome: UUID())), to: live)
        #expect(live.phase == .handingOver && status() == nil)

        // A stream that dropped is opening again until it is open.
        var dropped = conversation.initialState
        dropped = try conversation.apply(.init(.sent(message: "Hi", agent: helper, at: now, baseline: nil, fromDraft: true, id: nil)), to: dropped)
        dropped = try conversation.apply(.init(.named("t1")), to: dropped)
        dropped = try conversation.apply(.init(.streamEnded(Failure(.send, .timeout), outcome: UUID())), to: dropped)
        live = dropped
        #expect(status() == ComposerStatus(kind: .reconnecting, words: "Reconnecting…"))
        live = try conversation.apply(.init(.attachRead(thread: Record(id: "t1", kind: Kinds.thread, properties: [:]), rows: [])), to: live)
        live = try conversation.apply(.init(.streamed(AgentEvent(kind: .thread), at: now)), to: live)
        #expect(status()?.kind == .thinking)
    }

    @Test func everyPieceOfAReplyKeepsItsIdentityFromItsFirstTokenToItsStoredRow() async throws {
        let stream = [
            #"{"kind":"thread","thread":"t1"}"#, #"{"kind":"reasoning","text":"Let me look.","turn":1,"at":0}"#,
            #"{"kind":"delta","text":"I'll check. ","turn":1,"at":0}"#,
            #"{"kind":"toolStarted","id":"c1","tool":"query","turn":2,"args":"{}"}"#,
            #"{"kind":"toolFinished","id":"c1","tool":"query","turn":2,"ok":true,"output":"{}"}"#,
            #"{"kind":"toolStarted","id":"c2","tool":"query","turn":4,"args":"{}"}"#,
            #"{"kind":"toolFinished","id":"c2","tool":"query","turn":4,"ok":true,"output":"{}"}"#,
            #"{"kind":"delta","text":"Hello","turn":5,"at":0}"#,
            #"{"kind":"done","result":{"reply":"Hello","thread":"t1","status":"ok","turns":6}}"#,
        ]
        let rows = [
            messageRow("m0", "user", "Hi", turn: 0), messageRow("m1", "assistant", "I'll check. ", turn: 1, calls: ["c1"], reasoning: "Let me look."),
            messageRow("m2", "tool", "{}", turn: 2, answers: "c1"), messageRow("m3", "assistant", "", turn: 3, calls: ["c2"]),
            messageRow("m4", "tool", "{}", turn: 4, answers: "c2"), messageRow("m5", "assistant", "Hello", turn: 5),
        ]
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(stream))])
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, page(rows)))
        try await rig.signIn()
        let key = try await rig.newChat()
        let (frames, watching) = await rig.frames(key)
        defer { watching.cancel() }
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        await rig.harness.settle()
        // Each turn, each call's row and each block of text, by its id.
        func pieces(_ screen: ConversationScreen) -> [String] {
            screen.turns.flatMap { turn -> [String] in
                guard case let .assistant(reply) = turn.body else { return [turn.id] }
                return [turn.id] + reply.tools.map(\.id) + (reply.reasoning == nil ? [] : ["\(turn.id)/thinking"])
            }
        }
        var held: [String] = []
        for screen in frames.all {
            let now = pieces(screen)
            #expect(held.allSatisfy(now.contains), "a piece went: \(held.filter { !now.contains($0) })")
            #expect(now.filter(held.contains) == held, "pieces changed places")
            held = now
        }
        #expect(held == ["sent-1", "turn-1", "turn-1#0", "turn-1/thinking", "call-c2", "call-c2#0", "turn-5"])
        // And they are the rows now.
        let last = try #require(frames.all.last)
        #expect(last.turns.count == 4 && last.streaming == false)
        #expect(await rig.harness.state.transcripts["t1"]?.turns.map(\.row) == ["m0", "m1", "m3", "m5"])
    }
}
