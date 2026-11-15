import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

/// The app against a server that keeps the truth and does nothing wrong
/// (`SeededServer`, faulted only where a test says): its turns move when
/// the test moves them, so every screen between is the test's to look at.
struct Answering {
    let server: SeededServer
    let rig: ChatRig
    let agents: Int
    /// The ids of this world: a launch after another mints none again.
    let ids = SequentialIDs()

    /// - Parameter records: 9 for a server that keeps a person's pins and
    ///   archives, 8 for one that keeps their marks alone.
    init(agents: Int = 7, records: Int = 8) {
        let clock = TestClock()
        server = SeededServer(seed: 1, clock: clock, revision: agents, records: records)
        rig = ChatRig([], transport: server.connect(), clock: clock, ids: ids)
        self.agents = agents
    }

    /// The app launched again over the same folder.
    func relaunched() -> ChatRig { ChatRig([], base: rig.base, transport: server.connect(), clock: rig.clock, ids: ids) }

    func signIn() async throws {
        try await rig.signIn()
        try await rig.harness.until { harness in await harness.state.agentsRevision == agents }
    }

    /// A new chat whose first message is sent and taken: its turn does
    /// `steps`, of which none is out yet.
    func chat(_ steps: [SeededServer.Step], failing: Bool = false, saying message: String = "Hi") async throws -> ChildKey {
        server.plan(steps, failing: failing)
        let key = try await rig.newChat()
        try await say(message, key)
        try await rig.harness.until { _ in server.threadIDs == ["t1"] && server.running("t1") }
        try await until { _, live in live.thread == "t1" }
        return key
    }

    func say(_ message: String, _ key: ChildKey) async throws {
        try await rig.send(.draft(message), to: key)
        try await rig.send(.send, to: key)
    }

    /// The server's turns move by `events`, and the app hears of it.
    func work(_ events: Int = 1) { server.work(events: events) }

    /// Waits for a screen, with the conversation's state beside it, moving
    /// the clock past the window a reply's pieces share.
    func until(
        advancing: TimeInterval = 0.1, _ condition: @escaping @Sendable (ConversationScreen, ConversationState) -> Bool, _ key: ChildKey? = nil
    ) async throws {
        try await rig.harness.until(advancing: advancing) { harness in
            let state = await harness.state
            guard let key = key ?? state.scenes["s"]?.current, let screen = await rig.conversation(key), let live = await rig.live(key) else {
                return false
            }
            return condition(screen, live)
        }
    }

    /// The run is over and its rows are the chat's. The clock moves past
    /// the waits a chat makes on its way there.
    func settled(_ key: ChildKey) async throws {
        try await rig.harness.until(advancing: 6) { harness in
            guard let live = await rig.live(key), let thread = live.thread else { return false }
            let state = await harness.state
            return live.phase == .idle && !server.running(thread) && state.transcripts[thread]?.version == server.version(of: thread)
                && state.reading[thread] == nil
        }
    }

    func screen(_ key: ChildKey) async throws -> ConversationScreen { try #require(await rig.conversation(key)) }

    /// What the chat draws as said, in order: each message, with whether it
    /// is drawn as queued.
    func said(_ key: ChildKey) async -> [String] {
        (await rig.conversation(key)?.turns ?? []).compactMap { turn in
            switch turn.body {
            case let .user(user): user.text + (user.queued == nil ? "" : " (queued)")
            case let .assistant(reply): reply.text ?? ""
            default: nil
            }
        }
    }

    /// The tool rows a chat draws, in order.
    func tools(_ key: ChildKey) async -> [ToolRowPresentation] {
        (await rig.conversation(key)?.turns ?? []).flatMap { turn -> [ToolRowPresentation] in
            if case let .assistant(reply) = turn.body { reply.tools } else { [] }
        }
    }

    /// The card's view asks for its record, as it does when it appears.
    func show(_ card: CardReference) async { await rig.harness.send(.loadCard(card)) }
}

@Suite("A message sent while the assistant works", .timeLimit(.minutes(10)))
struct QueuedMessages {
    @Test func itIsQueuedUntilACallReadsItAndThenAnOrdinaryMessage() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1), .tool(.query), .call, .piece(0)])
        app.work()
        try await app.until { screen, _ in screen.turns.count == 2 && screen.streaming }
        // The composer sends while the reply is written, and Stop stays.
        var screen = try await app.screen(key)
        #expect(screen.composer.canSend && screen.composer.queues && screen.composer.blocked == nil && screen.composer.status != nil)
        let first = screen.turns.map(\.id)

        try await app.say("And the mint?", key)
        try await app.until { screen, live in live.queued.count == 1 && screen.turns.count == 3 }
        screen = try await app.screen(key)
        let message = try #require(app.server.queued(in: "t1").first)
        // Drawn once, under the id it was sent with, after the reply that
        // was being written, and said to wait.
        #expect(screen.turns.map(\.id) == first + [message])
        #expect(await app.said(key) == ["Hi", "r1.0 ", "And the mint? (queued)"])
        #expect(screen.composer.queued == "1 message is queued. If you stop Helper first, it stays unread until you continue.")
        // The reply above it is not its answer: nothing is awaited for it.
        #expect(!screen.typing && screen.streaming && screen.composer.canSend)

        // The reply goes on, and its tool is its own: no turn of the call's
        // comes to stand under the message.
        app.work(2)
        try await app.until { screen, _ in
            if case let .assistant(reply) = screen.turns[1].body { reply.tools.count == 1 && reply.text == "r1.0 r1.1 " } else { false }
        }
        #expect(await app.said(key) == ["Hi", "r1.0 r1.1 ", "And the mint? (queued)"])
        #expect(try await app.screen(key).turns.map(\.id) == first + [message])

        // The next call reads it.
        app.work()
        try await app.until { _, live in live.queued.isEmpty }
        #expect(await app.said(key) == ["Hi", "r1.0 r1.1 ", "And the mint?"])
        #expect(try await app.screen(key).composer.queued == nil)
        app.work()
        try await app.until { screen, _ in screen.turns.count == 4 }
        app.server.finishTurns()
        try await app.settled(key)
        // The rows are the chat, each under the id it was drawn by.
        #expect(await app.said(key) == app.server.spoken(in: "t1").map(\.content))
        #expect(try await app.screen(key).turns.map(\.id) == first + [message, "turn-4"])
        #expect(app.server.rows(of: "t1").filter { $0.role == "user" }.map(\.id) == [first[0], message])
        #expect(app.server.chatRequests(saying: "And the mint?") == 1)
    }

    @Test func aRunThatEndsWithOneUnreadStartsItsTurnAndTheChatFollowsIt() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1)])
        app.work()
        try await app.until { screen, _ in screen.turns.count == 2 }
        try await app.say("And the mint?", key)
        try await app.until { _, live in live.queued.count == 1 }
        let (frames, watching) = await app.rig.frames(key)
        defer { watching.cancel() }
        // The run ends with the message unread: the server starts the next
        // turn itself, and the chat is told it took the message.
        app.server.plan([.piece(0), .piece(1)])
        app.work(2)
        try await app.until { _, live in live.queued.isEmpty && live.phase == .streaming && live.settles == 1 }
        #expect(app.server.running("t1") && app.server.wasTaken(app.server.rows(of: "t1")[2].id))
        #expect(await app.said(key) == ["Hi", "r1.0 r1.1 ", "And the mint?"])
        app.server.finishTurns()
        try await app.settled(key)
        #expect(await app.said(key) == app.server.spoken(in: "t1").map(\.content))
        #expect(await app.said(key).count == 4)
        // Never, between, was a message drawn queued in a chat nothing ran in.
        for frame in frames.all where !frame.streaming {
            #expect(frame.turns.allSatisfy { if case let .user(message) = $0.body { message.queued == nil } else { true } })
        }
    }

    @Test func aStopLeavesItUnreadAndTheCardSaysSoWithContinue() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1), .piece(2)])
        app.work()
        try await app.until { screen, _ in screen.turns.count == 2 }
        try await app.say("And the mint?", key)
        try await app.until { _, live in live.queued.count == 1 }
        try await app.rig.send(.stop, to: key)
        try await app.settled(key)
        // Stopped: nothing starts its turn, and it is queued no more.
        #expect(!app.server.running("t1"))
        #expect(await app.said(key) == ["Hi", "r1.0 ", "And the mint?"])
        let card = try #require(await app.rig.card(key))
        #expect(card.sentence == "Helper was stopped before it finished.")
        #expect(card.note == "It hadn't read your last message. Continue has it read.")
        #expect(card.button?.title == "Continue" && card.button?.action == .resume)
        // Continue has it read: a turn with no new message.
        app.server.plan([.piece(0)])
        try await app.rig.send(.continueTurn, to: key)
        try await app.rig.harness.until { _ in app.server.running("t1") }
        app.server.finishTurns()
        try await app.settled(key)
        #expect(await app.said(key) == app.server.spoken(in: "t1").map(\.content))
        #expect(app.server.spoken(in: "t1").map(\.role) == ["user", "assistant", "user", "assistant"])
        #expect(await app.rig.card(key) == nil)
    }

    @Test func aRunThatFailsLeavesItUnreadToo() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1)], failing: true)
        app.work()
        try await app.until { screen, _ in screen.turns.count == 2 }
        try await app.say("And the mint?", key)
        try await app.until { _, live in live.queued.count == 1 }
        app.work()
        try await app.settled(key)
        #expect(await app.said(key) == ["Hi", "r1.0 ", "And the mint?"])
        #expect(await app.rig.live(key)?.unread == 1)
        #expect(await app.rig.card(key)?.button?.title == "Try Again")
    }

    /// A chat opened at a message, as a search's hit opens it, while that
    /// message is queued: it is drawn at its place, and says so.
    @Test func aChatOpenedAtAQueuedMessageDrawsItQueued() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1), .piece(2)])
        app.work(2)
        try await app.until { screen, _ in screen.streaming && screen.composer.queues }
        try await app.say("And the mint?", key)
        try await app.until { screen, _ in screen.composer.queued != nil }
        let message = try #require(app.server.rows(of: "t1").first { $0.content == "And the mint?" })
        await app.rig.harness.send(.open(thread: "t1", message: message.id, scene: "s"))
        try await app.until { screen, _ in screen.opens != .end }
        let screen = try await app.screen(key)
        #expect(screen.opens == .turn(message.id))
        #expect(await app.said(key).contains("And the mint? (queued)") && screen.turns.contains { $0.id == message.id })
        app.server.finishTurns()
        try await app.settled(key)
    }

    @Test func oneSentFromAnotherDeviceIsDrawnTooAndSeveralKeepTheirOrder() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1), .tool(.query), .call, .piece(0)])
        app.work()
        try await app.until { screen, _ in screen.turns.count == 2 }
        // Another device's, which this one hears of from the turn's stream.
        #expect(app.server.elsewhere(send: "From the phone", to: "t1", id: "phone-1"))
        try await app.until { _, live in live.queued.map(\.id) == ["phone-1"] }
        try await app.say("From here", key)
        try await app.until { _, live in live.queued.count == 2 }
        #expect(await app.said(key) == ["Hi", "r1.0 ", "From the phone (queued)", "From here (queued)"])
        #expect(try await app.screen(key).composer.queued?.hasPrefix("2 messages are queued.") == true)
        // Away and back: the stream is joined again, said from its start,
        // and nothing is drawn twice. What it says is queued is how the
        // turn stood, and a call may have read either while nothing was
        // heard: neither is called queued until the stream has caught up,
        // which the server's heartbeat says. Then both are, as they are.
        await app.rig.harness.send(.phase(.background))
        await app.rig.harness.send(.phase(.active))
        try await app.until { _, live in if case .attach? = live.run { live.connected } else { false } }
        #expect(await app.said(key) == ["Hi", "r1.0 ", "From the phone", "From here"])
        #expect(try await app.screen(key).composer.queued == nil)
        app.server.beat()
        try await app.until { _, live in live.queued.count == 2 }
        #expect(await app.said(key) == ["Hi", "r1.0 ", "From the phone (queued)", "From here (queued)"])
        app.work(3)
        try await app.until { _, live in live.queued.isEmpty }
        // Joined once more after both were taken: neither is queued again.
        await app.rig.harness.send(.phase(.background))
        await app.rig.harness.send(.phase(.active))
        try await app.until { _, live in if case .attach? = live.run { live.connected } else { false } }
        #expect(await app.said(key) == ["Hi", "r1.0 r1.1 ", "From the phone", "From here"])
        app.server.finishTurns()
        try await app.settled(key)
        #expect(await app.said(key) == app.server.spoken(in: "t1").map(\.content))
    }

    /// What the seeded runs found (seed 9435 at 39 steps, `agents` 7): a
    /// conversation that joins a run knowing nothing of it (a launch, a
    /// sign-in again) hears the turn from its start, where a message a
    /// call read long ago is first said queued. A stream cut between that
    /// and the word that it was taken left the message drawn as waiting
    /// until the next stream. A stream that may be saying its turn again
    /// says how the turn stood before now, which is not drawn; and a
    /// conversation with no stream calls nothing queued (seed 4487 at 40
    /// steps: a call read a message while nothing could say so).
    @Test func aChatThatJoinsARunAnewDrawsNoMessageQueuedThatACallHasRead() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1), .tool(.query), .call, .piece(0), .piece(1), .piece(2)])
        app.work()
        try await app.until { screen, _ in screen.turns.count == 2 }
        try await app.say("And the mint?", key)
        try await app.until { _, live in live.queued.count == 1 }
        app.work(3)
        try await app.until { _, live in live.queued.isEmpty }
        let message = try #require(app.server.rows(of: "t1").first { $0.content == "And the mint?" }?.id)
        #expect(app.server.wasTaken(message) && app.server.running("t1"))
        await app.rig.runtime.close()

        // Launched again while the run goes on. Its first stream ends
        // after `thread`, the reply's first piece and `messageQueued`.
        app.server.fail("events|t1", with: .stream(.init(cutAfter: 3)))
        let again = app.relaunched()
        try await again.signIn()
        await again.harness.send(.show(thread: "t1", scene: "s"))
        let shown = try #require(await again.harness.state.scenes["s"]?.current)
        let (frames, watching) = await again.frames(shown)
        defer { watching.cancel() }
        // The stream dropped, and the chat waits to ask for it again: by
        // the wait it asked for, since a chat that has not heard its first
        // stream yet is not connected either.
        try await again.harness.until { harness in
            guard let live = await again.live(shown) else { return false }
            let waits = harness.trace.contains { entry in
                guard case let .effect(_, effect) = entry, case .wait? = (effect as? Conversation.Effect)?.ask else { return false }
                return true
            }
            return waits && !live.connected && live.phase == .streaming && app.server.exchanges(about: "t1") == 0
        }
        // With no stream it knows nothing of the turn's queue, and says
        // nothing of one.
        let waiting = try #require(await again.live(shown))
        #expect(waiting.queued.isEmpty && waiting.recalled.isEmpty && !waiting.hears)
        #expect(await again.conversation(shown)?.composer.queued == nil)
        // The next stream says the rest: a call read it.
        try await again.harness.until(advancing: 1) { _ in
            guard let live = await again.live(shown) else { return false }
            return live.taken.contains(message) && live.connected
        }
        #expect(await again.live(shown)?.recalled.isEmpty == true)
        app.server.finishTurns()
        try await again.harness.until(advancing: 6) { _ in await again.live(shown)?.phase == .idle && !app.server.running("t1") }
        for frame in frames.all {
            #expect(frame.turns.allSatisfy { if case let .user(said) = $0.body { said.queued == nil } else { true } })
        }
    }

    /// What the seeded runs found next (seed 6008 at 22 steps, `agents`
    /// 6): a message sent into a running turn lets that turn's stream go
    /// for its own, and its request was lost. While it waited to go again
    /// the turn read what it held queued and ended, unheard. The repeat
    /// started a turn of its own, whose stream says nothing of the one
    /// before, and the message a call had read was drawn as waiting under
    /// an open stream. A stream let go takes what it said of the queue
    /// with it: the next says its turn's queue again, or has none.
    @Test func whatAStreamLetGoSaidWasQueuedIsNotDrawnSoUnderTheNext() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1), .call, .piece(0)])
        app.work()
        try await app.until { screen, _ in screen.turns.count == 2 }
        try await app.say("And the mint?", key)
        try await app.until { _, live in live.queued.count == 1 && live.hears }
        let first = try #require(app.server.rows(of: "t1").first { $0.content == "And the mint?" }?.id)
        // The next message's request is lost on its way, nothing written.
        app.server.fail("chat|t1|And the tea?", with: .dropped)
        try await app.say("And the tea?", key)
        try await app.until(advancing: 0) { _, live in live.pending != nil && app.server.chatRequests(saying: "And the tea?") == 1 }
        // No stream is open, so nothing is called queued.
        #expect(await app.said(key).allSatisfy { !$0.hasSuffix("(queued)") })
        // The turn reads the first message and ends while nothing is heard.
        app.server.finishTurns()
        #expect(app.server.wasTaken(first) && !app.server.running("t1"))
        // The message goes again and starts its own turn, which has said
        // nothing yet: its stream is open, and nothing waits in it.
        app.server.plan([.piece(0), .piece(1)])
        try await app.until(advancing: 2) { _, live in live.pending == nil && live.hears }
        #expect(app.server.running("t1") && app.server.userRows(saying: "And the tea?") == 1)
        let live = try #require(await app.rig.live(key))
        #expect(live.queued.isEmpty && live.recalled.isEmpty)
        #expect(await app.said(key).allSatisfy { !$0.hasSuffix("(queued)") })
        app.server.finishTurns()
        try await app.settled(key)
        #expect(await app.said(key) == app.server.spoken(in: "t1").map(\.content))
    }

    /// The same stream, of a message no call has read yet: it is drawn
    /// queued once the stream is known to have caught up with its turn,
    /// which a heartbeat says (it is sent only into a stream gone quiet),
    /// and the word that the run ended.
    @Test func aQueueAStreamSaidAgainTellsOfIsDrawnOnceTheStreamHasCaughtUp() throws {
        let conversation = Conversation(
            key: ChildKey("c"), seed: .init(thread: "t1", agent: helper, draft: "", listen: false), facts: nil, online: true, keyless: nil,
            newestTurn: nil, visible: true, pollNudge: 0, resumeNudge: 0, continuing: nil, agents: 6, sendsIDs: true)
        var state = conversation.initialState
        var queued = AgentEvent(kind: .messageQueued)
        (queued.id, queued.turn, queued.text) = ("m2", 2, "And the mint?")
        conversation.hear(&state, queued, replied: -1)
        #expect(state.queued.isEmpty && state.recalled.map(\.id) == ["m2"])
        conversation.hear(&state, AgentEvent(kind: .heartbeat), replied: -1)
        #expect(state.queued.map(\.id) == ["m2"] && state.recalled.isEmpty && state.caught)
        // From here the stream says what happens as it does.
        (queued.id, queued.turn) = ("m3", 3)
        conversation.hear(&state, queued, replied: -1)
        #expect(state.queued.map(\.id) == ["m2", "m3"])
        // A run that says it was stopped left both unread, said or not
        // while the stream was still catching up.
        var late = conversation.initialState
        conversation.hear(&late, queued, replied: -1)
        conversation.ended(&late, queueAfter: "stopped")
        #expect(late.unread == 1 && late.queued.isEmpty && late.recalled.isEmpty)
        // And a reply's piece past a message's ordinal read it, wherever
        // the message is kept.
        var read = conversation.initialState
        conversation.hear(&read, queued, replied: -1)
        conversation.hear(&read, .delta("So", turn: 4, at: 0), replied: -1)
        #expect(read.recalled.isEmpty && read.taken == ["m3"])
    }

    @Test func belowTheRevisionOnlyStopIsOfferedUntilTheReplyEnds() async throws {
        let app = Answering(agents: 5)
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1)])
        app.work()
        try await app.until { screen, _ in screen.turns.count == 2 }
        let screen = try await app.screen(key)
        // As before a message could join a turn: the text stays in the
        // field, Send waits, and nothing is sent.
        #expect(!screen.composer.canSend && !screen.composer.queues && screen.composer.blocked == "Waiting for the answer")
        #expect(screen.streaming && screen.composer.status != nil)
        try await app.say("And the mint?", key)
        await app.rig.harness.settle()
        #expect(app.server.chatRequests(saying: "And the mint?") == 0)
        #expect(await app.rig.live(key)?.draft == "And the mint?")
        app.server.finishTurns()
        try await app.settled(key)
        #expect(try await app.screen(key).composer.canSend)
    }

    @Test func oneWhoseAnswerIsLostIsSentAgainUnderItsIdAndWrittenOnce() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1), .tool(.query), .call, .piece(0)])
        app.work()
        try await app.until { screen, _ in screen.turns.count == 2 }
        // The server takes it and the answer is lost on the way back.
        app.server.fail("chat|t1|And the mint?", with: .dropped, lands: true)
        try await app.say("And the mint?", key)
        // It goes again a second later, under the same id.
        try await app.until(advancing: 2) { _, live in live.queued.count == 1 && live.pending == nil }
        #expect(app.server.chatRequests(saying: "And the mint?") == 2)
        #expect(app.server.userRows(saying: "And the mint?") == 1)
        #expect(await app.said(key) == ["Hi", "r1.0 ", "And the mint? (queued)"])
        app.server.finishTurns()
        try await app.settled(key)
        #expect(await app.said(key) == app.server.spoken(in: "t1").map(\.content))
    }

    @Test func oneTheServerRefusesGoesBackIntoTheComposerAndTheReplyGoesOn() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1)])
        app.work()
        try await app.until { screen, _ in screen.turns.count == 2 }
        app.server.fail("chat|t1|And the mint?", with: .status(429, code: "rate_limited", retryAfter: nil))
        try await app.say("And the mint?", key)
        // Nothing was written: the text is back, the strip says so, and
        // the turn it was sent into is read again.
        try await app.until { screen, live in screen.failures.strip != nil && live.connected && live.pending == nil }
        #expect(await app.rig.live(key)?.draft == "And the mint?")
        #expect(await app.said(key) == ["Hi", "r1.0 "])
        app.work()
        try await app.until { screen, _ in if case let .assistant(reply) = screen.turns[1].body { reply.text == "r1.0 r1.1 " } else { false } }
        app.server.finishTurns()
        try await app.settled(key)
        #expect(app.server.userRows(saying: "And the mint?") == 0)
    }

    @Test func theStreamsEventsAreReadAndFoldedWhereTheirRowsWillStand() {
        let queued = AgentEvent.parse(#"{"kind":"messageQueued","id":"q1","turn":4,"text":"And the mint?"}"#)
        let taken = AgentEvent.parse(#"{"kind":"messageTaken","id":"q1","turn":4}"#)
        #expect(queued?.kind == .messageQueued && queued?.id == "q1" && queued?.turn == 4 && queued?.text == "And the mint?")
        #expect(taken?.kind == .messageTaken && taken?.id == "q1")
        // A reply is being written, a message joins it, and a call of that
        // reply's comes after: it is the reply's, past the message.
        var rows = LiveRows()
        rows.hear([.delta("Looking. ", turn: 3, at: 0), try! #require(queued), .toolStarted("c1", turn: 5), .toolFinished("c1", turn: 5)])
        #expect(rows.turns.map(\.id) == ["turn-3", "q1"] && rows.turns[0].tools.map(\.id) == ["c1"])
        #expect(rows.turns[1].ordinal == 4 && rows.turns[1].role == .user && !rows.turns[1].pending)
        // Once a call has read it, the next call's work is another reply's.
        rows.hear([try! #require(taken), .toolStarted("c2", turn: 7)])
        #expect(rows.turns.map(\.id) == ["turn-3", "q1", "call-c2"])
        // One heard of late, in a replay that had lost it, stands where
        // its row will: before the reply that read it.
        var late = LiveRows()
        late.hear([.delta("One. ", turn: 3, at: 0), .delta("Two.", turn: 5, at: 0), try! #require(queued)])
        #expect(late.turns.map(\.id) == ["turn-3", "q1", "turn-5"])
        // And the fold puts each where its ordinal says, among the rows.
        let stored = StoredTurns(transcript(of: [messageRow("m0", "user", "Hi", turn: 0), messageRow("m3", "assistant", "One. ", turn: 3)]))
        #expect(TurnFold.fold(stored, late).turns.map(\.id) == ["m0", "turn-3", "q1", "turn-5"])
        // One sent from here into a running turn stands after everything
        // until the server says its place.
        var mine = LiveRows()
        mine.hear([.delta("One. ", turn: 3, at: 0)])
        mine.send("And the mint?", id: "q2", seq: 9, baseline: nil, at: Date(timeIntervalSince1970: 200), joining: true)
        #expect(TurnFold.fold(stored, mine).turns.map(\.id) == ["m0", "turn-3", "q2"])
    }

    /// A message that stands unsent started no turn. One that runs beside
    /// it, resumed by an answer to a card, is drawn above it and not below:
    /// the message is the last thing in the chat, with its card. It was
    /// drawn before the turn, as the message that started it would be
    /// (seed 6851 at 91 steps, once the seeded server's cards resumed
    /// threads on their own).
    @Test func aMessageThatStandsUnsentIsLastBesideATurnItDidNotStart() {
        // The turn beside it has ended, and its row is read.
        let stored = StoredTurns(
            transcript(of: [
                messageRow("m0", "user", "Hi", turn: 0), messageRow("m1", "assistant", "Hello.", turn: 1),
                messageRow("m3", "assistant", "Booked. ", turn: 3),
            ]))
        var beside = LiveRows()
        beside.hear([.delta("Booked. ", turn: 3, at: 0)])
        beside.turns.append(Turn(id: "u9", role: .user, content: "And the mint?", pending: true))
        // As the message that started the run it would stand before it.
        #expect(TurnFold.fold(stored, beside).turns.map(\.id) == ["m0", "m1", "u9", "turn-3"])
        beside.stands = "u9"
        #expect(TurnFold.fold(stored, beside).turns.map(\.id) == ["m0", "m1", "turn-3", "u9"])
    }

    @Test func aMessageNothingAnsweredSaysSoWithContinueWhereAServerTakesMessagesIntoATurn() {
        let ok = ChatThread(record: Record(id: "t1", kind: Kinds.thread, properties: ["status": "ok", "mode": "chat"]))
        let stopped = ChatThread(record: Record(id: "t1", kind: Kinds.thread, properties: ["status": "stopped", "mode": "chat"]))
        let turns = transcript(of: [
            messageRow("m0", "user", "Hi", turn: 0), messageRow("m1", "assistant", "Hello", turn: 1), messageRow("m2", "user", "And?", turn: 2),
        ])
        #expect(ChatApp.unanswered(ok, turns) && !ChatApp.unanswered(stopped, turns) && !ChatApp.unanswered(ok, Array(turns.prefix(2))))
        var context = FailurePresenter.Context()
        context.agent = "Moss"
        let card = FailurePresenter.present(Failure(.reply, .unanswered), context)
        #expect(card.sentence == "Moss didn't answer this message." && card.button?.title == "Continue" && card.button?.action == .resume)
        // An older one says so and offers nothing.
        context.acts = false
        #expect(FailurePresenter.present(Failure(.reply, .unanswered), context).button == nil)
        // Two messages a later reply read together are both answered.
        let both = transcript(of: [
            messageRow("m0", "user", "Hi", turn: 0), messageRow("m1", "user", "And?", turn: 1), messageRow("m2", "assistant", "Both.", turn: 2),
        ])
        #expect(ChatApp.readLater(both, after: 0, queues: true) && !ChatApp.readLater(both, after: 0, queues: false))
    }
}

@Suite("A card stands while it waits and folds into its call", .timeLimit(.minutes(10)))
struct FoldedCards {
    /// A chat whose turn asked the person, settled: the card's id.
    private func asked(_ app: Answering, _ tool: SeededServer.Tool = .ask) async throws -> (ChildKey, String) {
        let key = try await app.chat([.piece(0), .tool(tool), .call, .piece(0)])
        app.server.finishTurns()
        try await app.settled(key)
        let card = try #require(app.server.allCards.first)
        await app.show(tool == .ask ? .question(card.id) : .approval(card.id))
        try await app.until { screen, _ in screen.cards[card.id]?.waits == true }
        return (key, card.id)
    }

    @Test func aQuestionWaitsUnderItsCallAndIsItsMarkOnceAnswered() async throws {
        let app = Answering()
        try await app.signIn()
        let (key, id) = try await asked(app)
        var screen = try await app.screen(key)
        var row = try #require(await app.tools(key).first)
        #expect(row.attachment == .questions(id) && row.status == .held && row.statusLabel == "Waiting for your answer" && row.resolution == nil)
        #expect(screen.cards[id]?.stands == true && screen.cards[id]?.question?.words == "Answer in Your Own Words")
        let before = screen.turns.map(\.id)
        let pieces = before

        let question = try #require(screen.cards[id]?.question?.questions.first)
        await app.rig.harness.send(.answer(question: id, picks: [question.id: [question.options[0].value]]))
        try await app.until { screen, _ in screen.cards[id]?.resolution != nil }
        // The same row, under the same id, now says how it ended; no card
        // stands under it.
        row = try #require(await app.tools(key).first)
        #expect(row.attachment == nil && row.status == .done && row.statusLabel == "You answered")
        let resolution = try #require(row.resolution)
        #expect(resolution.card == .question(id) && resolution.mark == .done && resolution.at != nil && !resolution.loading)
        #expect(resolution.facts == [CardResolution.Fact(label: question.prompt, value: question.options[0].label)])
        screen = try await app.screen(key)
        #expect(screen.cards[id]?.stands == false && Array(screen.turns.map(\.id).prefix(pieces.count)) == before)
        #expect(app.server.card(id)?.state == "answered" && app.server.card(id)?.resolutions == 1)
        // The thread is resumed, and the line its rows hold for the answer
        // is not drawn a second time under the call that says it.
        app.server.finishTurns()
        try await app.settled(key)
        screen = try await app.screen(key)
        #expect(!screen.turns.contains { $0.body == .answer(OutcomeLine(text: "You answered", done: true)) })
        #expect(Array(screen.turns.map(\.id).prefix(pieces.count)) == before)
    }

    /// The engine alone. A question is drawn from the pending list before
    /// the card's own read lands, and its buttons work from then: Answer
    /// and Dismiss write over the version the list read, as Apply and
    /// Approve do. They did nothing until the card's own read, in the app
    /// as first built.
    @Test func aQuestionDrawnFromThePendingListIsAnsweredOverThatVersion() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        let question: JSONValue = ["id": "q1", "prompt": "Which day?", "options": [["value": "tue", "label": "Tuesday"]]]
        state.interactions = [Record(id: "i1", kind: Kinds.interaction, properties: ["state": "pending", "questions": [question]], version: 3)]
        var answered = try app.apply(.init(.answerAsked("i1", picks: ["q1": ["tue"]])), to: state)
        let answers: JSONValue = [["question": "q1", "selected": ["tue"]]]
        #expect(answered.asks.contains(.writeCard("i1", kind: Kinds.interaction, properties: ["state": "answered", "answers": answers], ifVersion: 3)))
        #expect(answered.cards["i1"]?.busy == "answer")
        answered = try app.apply(.init(.questionsDismissed("i1")), to: state)
        #expect(answered.asks.contains(.writeCard("i1", kind: Kinds.interaction, properties: ["state": "dismissed"], ifVersion: 3)))
    }

    /// A search's hit opens a chat at a message. The line the rows hold
    /// for an answer is not drawn where its call says the outcome, so a
    /// chat opened at that row stands at the turn drawn after it, which is
    /// there; and the call above it carries the outcome.
    @Test func aChatOpenedAtAnOutcomesRowStandsAtTheNextTurnDrawn() async throws {
        let app = Answering()
        try await app.signIn()
        let (key, id) = try await asked(app)
        let question = try #require(try await app.screen(key).cards[id]?.question?.questions.first)
        await app.rig.harness.send(.answer(question: id, picks: [question.id: [question.options[0].value]]))
        try await app.until { screen, _ in screen.cards[id]?.resolution != nil }
        app.server.plan([.piece(0)])
        app.server.finishTurns()
        try await app.settled(key)
        let line = try #require(app.server.rows(of: "t1").first { $0.role == "system" })
        let reply = try #require(app.server.rows(of: "t1").first { $0.role == "assistant" && $0.turn > line.turn })
        await app.rig.harness.send(.open(thread: "t1", message: line.id, scene: "s"))
        try await app.until { screen, _ in screen.opens != .end }
        let screen = try await app.screen(key)
        guard case let .turn(at) = screen.opens else {
            Issue.record("the chat opens at no turn")
            return
        }
        #expect(!screen.turns.contains { $0.id == line.id } && screen.turns.contains { $0.id == at })
        // The reply that followed the answer, under the id it is drawn by.
        let drawn = try #require(screen.turns.first { $0.id == at })
        if case let .assistant(turn) = drawn.body { #expect(turn.text == reply.content) } else { Issue.record("it stands at \(drawn.body)") }
        #expect(await app.tools(key).first?.resolution?.mark == .done)
    }

    /// Whose hand ended a card is the server's to say (`agents` 7). One
    /// another person resolved says "Someone else", never "You"; one this
    /// device resolved says "You" from its own write.
    @Test func aCardAnotherPersonEndedNeverSaysYou() async throws {
        let app = Answering()
        try await app.signIn()
        let (key, id) = try await asked(app, .approve)
        #expect(app.server.elsewhere(card: id, accept: true, by: "other"))
        try await app.until { screen, _ in screen.cards[id]?.resolution?.label.hasPrefix("Someone else approved gmail_send") == true }
        let row = try #require(await app.tools(key).first)
        #expect(row.statusLabel?.hasPrefix("You") == false && app.server.card(id)?.by == "other")
        app.server.finishTurns()
    }

    /// Below `agents` 7 no record links who resolved it, and nothing is
    /// asked: a card says only how it ended, whoever pressed.
    @Test func belowSevenACardSaysOnlyHowItEnded() async throws {
        let app = Answering(agents: 6)
        try await app.signIn()
        let (key, id) = try await asked(app)
        let question = try #require(try await app.screen(key).cards[id]?.question?.questions.first)
        await app.rig.harness.send(.answer(question: id, picks: [question.id: [question.options[0].value]]))
        try await app.until { screen, _ in screen.cards[id]?.resolution != nil }
        #expect(await app.tools(key).first?.statusLabel == "Answered")
        #expect(app.server.tally["a read of the person's own resolutions"] == nil)
        app.server.finishTurns()
    }

    /// The engine alone: what a whole answer of the person's own does not
    /// hold is somebody else's where it links somebody, and unsaid where
    /// it links nobody; past a first page nothing is concluded.
    @Test func whoseHandIsConcludedFromTheServersAnswerAlone() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        state.agentsRevision = 7
        func card(_ id: String, linked: Bool) -> Record {
            var properties: [String: JSONValue] = ["state": "answered", "thread": ["ref": .string("\(Kinds.thread)/t1")]]
            if linked { properties["resolvedBy"] = ["ref": "substrate.reamde.dev/core/actor/a1"] }
            return Record(id: id, kind: Kinds.interaction, properties: properties, version: 2)
        }
        for (id, linked) in [("mine", true), ("theirs", true), ("old", false)] {
            state = try app.apply(.init(.cardRead(id, card(id, linked: linked), target: nil)), to: state)
            // Asked once, for the first that ended; the rest are asked
            // about when that read is answered.
            #expect(state.asks.contains(.readOwn(thread: "t1", cards: ["mine"])) == (id == "mine"))
        }
        #expect(state.hand("mine", record: state.cards["mine"]?.record) == .unsaid)
        var whole = try app.apply(.init(.ownRead(thread: "t1", asked: ["mine", "old", "theirs"], mine: ["mine"], whole: true)), to: state)
        #expect(whole.hand("mine", record: whole.cards["mine"]?.record) == .you)
        #expect(whole.hand("theirs", record: whole.cards["theirs"]?.record) == .other)
        #expect(whole.hand("old", record: whole.cards["old"]?.record) == .unsaid)
        // A first page that may not be all of them concludes nothing about
        // the ones it does not hold.
        whole = try app.apply(.init(.ownRead(thread: "t1", asked: ["mine", "old", "theirs"], mine: ["mine"], whole: false)), to: state)
        #expect(whole.hand("mine", record: whole.cards["mine"]?.record) == .you)
        #expect(whole.hand("theirs", record: whole.cards["theirs"]?.record) == .unsaid)
        // Below the revision nothing is asked.
        var below = signedInState()
        below.agentsRevision = 6
        below = try app.apply(.init(.cardRead("mine", card("mine", linked: true), target: nil)), to: below)
        #expect(!below.asks.contains { if case .readOwn = $0 { true } else { false } })
    }

    @Test func oneResolvedOnAnotherDeviceFoldsWhenTheFeedSaysSo() async throws {
        let app = Answering()
        try await app.signIn()
        let (key, id) = try await asked(app)
        #expect(app.server.elsewhere(card: id, accept: false))
        // Another device of the same person: the server says the card is
        // theirs, once it is asked.
        try await app.until { screen, _ in screen.cards[id]?.resolution?.label == "You dismissed the question" }
        let row = try #require(await app.tools(key).first)
        #expect(row.attachment == nil && row.status == .declined && row.statusLabel == "You dismissed the question")
        #expect(row.resolution?.facts.map(\.value) == ["No answer"])
        // Its buttons are gone with it: a press that was on its way writes
        // nothing over the resolution.
        #expect(try await app.screen(key).cards[id]?.waits == false)
    }

    @Test func anAnswerThatFailedIsSaidInTheCardWithItsButton() async throws {
        let app = Answering()
        try await app.signIn()
        let (key, id) = try await asked(app)
        let question = try #require(try await app.screen(key).cards[id]?.question?.questions.first)
        let picks = [question.id: [question.options[0].value]]
        // The server answers 500 and took nothing.
        app.server.fail("decide|\(id)", with: .status(500, code: "internal", retryAfter: nil))
        await app.rig.harness.send(.answer(question: id, picks: picks))
        try await app.until { screen, _ in screen.cards[id]?.question?.failure != nil && screen.cards[id]?.question?.busy == nil }
        var card = try #require(try await app.screen(key).cards[id])
        let failure = try #require(card.question?.failure)
        #expect(failure.sentence == "That didn't go through." && failure.note == "The server ran into a problem.")
        #expect(failure.button?.title == "Try Again" && failure.button?.action == .retryCard(.question(id)))
        // It still waits, and still stands, after the card was read again.
        #expect(card.stands && card.waits && app.server.card(id)?.pending == true)
        // Try Again asks the same again.
        await app.rig.harness.send(.retryCard(.question(id)))
        try await app.until { screen, _ in screen.cards[id]?.resolution != nil }
        card = try #require(try await app.screen(key).cards[id])
        #expect(card.question?.failure == nil && app.server.card(id)?.answers == picks && app.server.card(id)?.resolutions == 1)
    }

    @Test func anAnswerTheServerTookWhoseReplyWasLostFoldsWithNoFailure() async throws {
        let app = Answering()
        try await app.signIn()
        let (key, id) = try await asked(app)
        let question = try #require(try await app.screen(key).cards[id]?.question?.questions.first)
        app.server.fail("decide|\(id)", with: .dropped, lands: true)
        await app.rig.harness.send(.answer(question: id, picks: [question.id: [question.options[0].value]]))
        // The write's answer is lost, the card is read again, and what it
        // reads is that it ended: nothing is left to say or to press.
        try await app.until { screen, _ in screen.cards[id]?.resolution != nil }
        let card = try #require(try await app.screen(key).cards[id])
        #expect(card.question?.failure == nil && !card.stands && app.server.card(id)?.resolutions == 1)
    }

    @Test func aWriteOverACardThatMovedIsRefusedAndSaysToLookAgain() async throws {
        let app = Answering()
        try await app.signIn()
        let (key, id) = try await asked(app)
        // Answered elsewhere, and dismissed here before the feed is heard:
        // the write is over the version read, so the server refuses it, and
        // the card reads what became of the questions.
        app.server.elsewhere(card: id, accept: true)
        await app.rig.harness.send(.dismissQuestions(id))
        try await app.until { screen, _ in screen.cards[id]?.resolution?.label == "You answered" }
        #expect(app.server.card(id)?.state == "answered" && app.server.card(id)?.resolutions == 1)
        #expect(await app.tools(key).first?.statusLabel == "You answered")
    }

    @Test func anApprovalIsItsCallsMarkOnceDecided() async throws {
        for approve in [true, false] {
            let app = Answering()
            try await app.signIn()
            let (key, id) = try await asked(app, .approve)
            var row = try #require(await app.tools(key).first)
            #expect(row.attachment == .approval(id) && row.status == .held && row.statusLabel == "Waiting for your approval")
            await app.rig.harness.send(.decideApproval(id, approve: approve))
            try await app.until { screen, _ in screen.cards[id]?.resolution != nil }
            row = try #require(await app.tools(key).first)
            #expect(row.attachment == nil && row.status == (approve ? .done : .declined))
            #expect(row.statusLabel == (approve ? "You approved gmail_send, and it ran" : "You declined gmail_send"))
            // What it was asked to run with is there to read, opened.
            #expect(row.resolution?.facts.contains(CardResolution.Fact(label: "To", value: "ada@example.com")) == true)
            #expect(app.server.card(id)?.resolutions == 1)
        }
    }

    @Test func aCardResolvedInAnOlderChatNeverFlashesAsWaiting() async throws {
        let app = Answering()
        try await app.signIn()
        let (key, id) = try await asked(app)
        app.server.elsewhere(card: id, accept: true)
        app.server.finishTurns()
        try await app.until { screen, _ in screen.cards[id]?.resolution != nil }
        // Launched again over nothing: the rows alone say it ended, before
        // the card's record is read.
        let again = ChatRig([], transport: app.server.connect(), clock: app.rig.clock, ids: app.ids)
        try await again.signIn()
        await again.harness.send(.sceneAppeared(scene: "s", restoring: nil))
        await again.harness.send(.show(thread: "t1", scene: "s"))
        let opened = try #require(await again.harness.state.scenes["s"]?.current)
        let tool = { @Sendable (screen: ConversationScreen?) -> ToolRowPresentation? in
            (screen?.turns ?? []).compactMap { turn -> ToolRowPresentation? in
                if case let .assistant(reply) = turn.body { reply.tools.first } else { nil }
            }.first
        }
        // Whose hand it was is the server's to say: asked once the rows are
        // read, and said without the card's record.
        try await again.harness.until(advancing: 0.1) { _ in tool(await again.conversation(opened))?.statusLabel == "You answered" }
        let screen = try #require(await again.conversation(opened))
        let row = try #require(tool(screen))
        #expect(row.attachment == nil && row.statusLabel == "You answered" && row.resolution?.loading == true)
        #expect(screen.cards[id]?.stands == false)
        _ = key
    }
}

@Suite("Answer in your own words", .timeLimit(.minutes(10)))
struct OwnWords {
    @Test func theCardGoesAsideTheComposerSaysSoAndTheWayBackBringsItBack() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .tool(.ask), .call, .piece(0)])
        app.server.finishTurns()
        try await app.settled(key)
        let id = try #require(app.server.allCards.first?.id)
        await app.show(.question(id))
        try await app.until { screen, _ in screen.cards[id]?.waits == true }
        let focus = try await app.screen(key).composer.focus

        try await app.rig.send(.answerInWords(id), to: key)
        try await app.until { screen, _ in screen.composer.answering != nil }
        var screen = try await app.screen(key)
        // Off the transcript, and the cursor is asked into the field.
        #expect(screen.cards[id]?.stands == false && screen.cards[id]?.waits == true && screen.cards[id]?.question?.aside == true)
        #expect(screen.composer.focus == focus + 1)
        #expect(screen.composer.answering == AnsweringNote(id: id, title: "Answering in your own words", prompt: "Question 1 of \(id)?", back: "Show Options"))
        var row = try #require(await app.tools(key).first)
        #expect(row.attachment == nil && row.status == .held && row.statusLabel == "You are answering in your own words")
        // Nothing was told to the server yet.
        #expect(app.server.card(id)?.pending == true)

        // Not sent: the card comes back.
        try await app.rig.send(.showOptions, to: key)
        try await app.until { screen, _ in screen.composer.answering == nil }
        screen = try await app.screen(key)
        row = try #require(await app.tools(key).first)
        #expect(screen.cards[id]?.stands == true && row.attachment == .questions(id) && row.statusLabel == "Waiting for your answer")
    }

    @Test(arguments: [6, 3, 2])
    func whatIsSentIsAnOrdinaryMessageAndTheQuestionIsDismissedOnceTheServerHasIt(agents: Int) async throws {
        let app = Answering(agents: agents)
        try await app.signIn()
        let key = try await app.chat([.piece(0), .tool(.ask), .call, .piece(0)])
        app.server.finishTurns()
        try await app.settled(key)
        let id = try #require(app.server.allCards.first?.id)
        await app.show(.question(id))
        try await app.until { screen, _ in screen.cards[id]?.waits == true }
        try await app.rig.send(.answerInWords(id), to: key)
        try await app.until { screen, _ in screen.composer.answering != nil }

        // A typed message alone answers and dismisses nothing at the
        // server: the app dismisses the question once the message is the
        // server's, so it does not wait for good.
        try await app.say("Neither: Friday.", key)
        try await app.until { screen, _ in screen.cards[id]?.resolution != nil }
        #expect(app.server.userRows(saying: "Neither: Friday.") == 1 && app.server.chatRequests(saying: "Neither: Friday.") == 1)
        #expect(app.server.card(id)?.state == "dismissed" && app.server.card(id)?.resolutions == 1)
        let screen = try await app.screen(key)
        #expect(screen.composer.answering == nil && screen.cards[id]?.question?.aside == false)
        let row = try #require(await app.tools(key).first)
        // Below `agents` 7 nothing says whose hand ended a card.
        #expect(row.status == .declined && row.statusLabel == "Dismissed")
        // The message is drawn once, and the chat settles on the rows.
        app.server.finishTurns()
        try await app.settled(key)
        #expect(await app.said(key) == app.server.spoken(in: "t1").map(\.content))
        #expect(await app.said(key).filter { $0 == "Neither: Friday." }.count == 1)
    }

    @Test func aSendTheServerRefusesLeavesTheQuestionWaitingAndTheAnswerInTheComposer() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .tool(.ask), .call, .piece(0)])
        app.server.finishTurns()
        try await app.settled(key)
        let id = try #require(app.server.allCards.first?.id)
        await app.show(.question(id))
        try await app.until { screen, _ in screen.cards[id]?.waits == true }
        try await app.rig.send(.answerInWords(id), to: key)
        app.server.fail("chat|t1|Neither: Friday.", with: .status(429, code: "rate_limited", retryAfter: nil))
        try await app.say("Neither: Friday.", key)
        try await app.until { screen, _ in screen.failures.strip != nil }
        // Nothing was sent, so nothing is dismissed: the answer is in the
        // field, and the composer still says what it answers.
        #expect(app.server.card(id)?.pending == true && app.server.userRows(saying: "Neither: Friday.") == 0)
        #expect(await app.rig.live(key)?.draft == "Neither: Friday.")
        #expect(try await app.screen(key).composer.answering?.id == id)
        // Sent again, it goes, and the question with it.
        try await app.rig.send(.send, to: key)
        try await app.until { screen, _ in screen.cards[id]?.resolution != nil }
        #expect(app.server.card(id)?.state == "replied" && app.server.userRows(saying: "Neither: Friday.") == 1)
    }

    /// At `agents` 7 the message names the questions it answers. The
    /// server marks them replied in the write of the message: no dismissal
    /// is written, nothing resumes, and the turn that reads the message is
    /// the only one. The call's row says they were answered in a message,
    /// by the person, with the message's own words.
    @Test func atSevenTheMessageNamesTheQuestionsAndNothingIsDismissed() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .tool(.ask), .call, .piece(0)])
        app.server.finishTurns()
        try await app.settled(key)
        let id = try #require(app.server.allCards.first?.id)
        await app.show(.question(id))
        try await app.until { screen, _ in screen.cards[id]?.waits == true }
        #expect(try await app.screen(key).cards[id]?.question?.words == "Answer in Your Own Words")
        try await app.rig.send(.answerInWords(id), to: key)
        try await app.until { screen, _ in screen.composer.answering != nil }
        app.server.plan([.piece(0)])
        try await app.say("Neither: Friday.", key)
        try await app.until { screen, _ in screen.cards[id]?.resolution?.label == "You answered in your own words" }
        let card = try #require(app.server.card(id))
        #expect(card.state == "replied" && card.resolutions == 1 && card.dismissals == 0 && card.resumes == 0)
        #expect(app.server.userRows(saying: "Neither: Friday.") == 1 && app.server.chatRequests(saying: "Neither: Friday.") == 1)
        let message = try #require(app.server.rows(of: "t1").first { $0.content == "Neither: Friday." })
        #expect(card.repliedIn == message.id)
        let screen = try await app.screen(key)
        #expect(screen.composer.answering == nil)
        let row = try #require(await app.tools(key).first)
        #expect(row.attachment == nil && row.status == .done && row.statusLabel == "You answered in your own words")
        let question = try #require(card.questions.first)
        #expect(row.resolution?.repliedIn == message.id)
        #expect(row.resolution?.facts.first == CardResolution.Fact(label: question.prompt, value: "Neither: Friday."))
        // One turn: the one that reads the message.
        app.server.finishTurns()
        try await app.settled(key)
        #expect(app.server.rows(of: "t1").filter { $0.role == "assistant" && $0.turn > message.turn }.count == 1)
        #expect(app.server.rows(of: "t1").filter { $0.role == "system" }.isEmpty)
        #expect(await app.said(key) == app.server.spoken(in: "t1").map(\.content))
    }

    /// The questions are resolved on another device while the reply in
    /// words is on its way: the server leaves them as they are and takes
    /// the message as an ordinary one. The strip goes, and the card says
    /// what the other device made of it.
    @Test func questionsResolvedElsewhereMeanwhileStayAsTheyWereAndTheMessageGoes() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .tool(.ask), .call, .piece(0)])
        app.server.finishTurns()
        try await app.settled(key)
        let id = try #require(app.server.allCards.first?.id)
        await app.show(.question(id))
        try await app.until { screen, _ in screen.cards[id]?.waits == true }
        try await app.rig.send(.answerInWords(id), to: key)
        try await app.until { screen, _ in screen.composer.answering != nil }
        // Another person dismisses them, which resumes the thread, and the
        // words are sent: before the feed is heard, or into the turn the
        // dismissal started, whichever the device comes to first. Where
        // the composer could not send at that instant (the chat was taking
        // up the resumed turn), the person presses Send again.
        app.server.elsewhere(card: id, accept: false, by: "other")
        try await app.rig.send(.draft("Neither: Friday."), to: key)
        try await app.rig.harness.until(advancing: 0.5) { _ in
            if app.server.userRows(saying: "Neither: Friday.") == 1 { return true }
            if await app.rig.live(key)?.draft == "Neither: Friday.", await app.rig.conversation(key)?.composer.canSend == true {
                try? await app.rig.send(.send, to: key)
            }
            return false
        }
        try await app.until { screen, _ in screen.cards[id]?.resolution?.label == "Someone else dismissed the question" }
        #expect(app.server.card(id)?.state == "dismissed" && app.server.card(id)?.resolutions == 1 && app.server.card(id)?.dismissals == 0)
        #expect(app.server.userRows(saying: "Neither: Friday.") == 1)
        try await app.until { screen, _ in screen.composer.answering == nil }
        #expect(try await app.screen(key).composer.answering == nil)
        app.server.finishTurns()
        try await app.settled(key)
    }

    /// A reply in words whose answer is lost is sent again under its id,
    /// with the questions it names: written once, and resolved once.
    @Test func aReplyInWordsWhoseAnswerIsLostIsSentAgainAndResolvesOnce() async throws {
        let app = Answering()
        try await app.signIn()
        let key = try await app.chat([.piece(0), .tool(.ask), .call, .piece(0)])
        app.server.finishTurns()
        try await app.settled(key)
        let id = try #require(app.server.allCards.first?.id)
        await app.show(.question(id))
        try await app.until { screen, _ in screen.cards[id]?.waits == true }
        try await app.rig.send(.answerInWords(id), to: key)
        app.server.plan([.piece(0)])
        app.server.fail("chat|t1|Neither: Friday.", with: .status(502, code: "bad_gateway", retryAfter: nil), lands: true)
        try await app.say("Neither: Friday.", key)
        try await app.rig.harness.until(advancing: 2) { _ in app.server.chatRequests(saying: "Neither: Friday.") >= 2 }
        try await app.until(advancing: 2) { screen, _ in screen.cards[id]?.resolution != nil }
        #expect(app.server.userRows(saying: "Neither: Friday.") == 1)
        #expect(app.server.card(id)?.state == "replied" && app.server.card(id)?.resolutions == 1)
        app.server.finishTurns()
        try await app.settled(key)
    }

    @Test func aDismissalThatFailsBringsTheCardBackWithItsFailure() async throws {
        // Below `agents` 7, where the questions are dismissed by a write.
        let app = Answering(agents: 6)
        try await app.signIn()
        let key = try await app.chat([.piece(0), .tool(.ask), .call, .piece(0)])
        app.server.finishTurns()
        try await app.settled(key)
        let id = try #require(app.server.allCards.first?.id)
        await app.show(.question(id))
        try await app.until { screen, _ in screen.cards[id]?.waits == true }
        try await app.rig.send(.answerInWords(id), to: key)
        app.server.fail("decide|\(id)", with: .status(500, code: "internal", retryAfter: nil))
        try await app.say("Neither: Friday.", key)
        try await app.until { screen, _ in screen.cards[id]?.question?.failure != nil }
        // The message is the server's; the question still waits, and its
        // card stands again and says what did not go through.
        let card = try #require(try await app.screen(key).cards[id])
        #expect(card.stands && card.question?.aside == false && card.question?.failure?.button?.action == .retryCard(.question(id)))
        #expect(app.server.userRows(saying: "Neither: Friday.") == 1 && app.server.card(id)?.pending == true)
        await app.rig.harness.send(.retryCard(.question(id)))
        try await app.until { screen, _ in screen.cards[id]?.resolution != nil }
        #expect(app.server.card(id)?.state == "dismissed")
    }
}

@Suite("Answer from a notification", .timeLimit(.minutes(10)))
struct NotificationActions {
    /// A chat whose turn ran while no window showed it, settled: what the
    /// app notified of it, by id.
    private func away(_ app: Answering, _ steps: [SeededServer.Step]) async throws -> [String: NotificationRequest] {
        let key = try await app.chat(steps)
        await app.rig.harness.send(.back(scene: "s"))
        app.server.finishTurns()
        // Its run ends with nobody looking: the conversation hands its
        // rows back and goes.
        try await app.rig.harness.until(advancing: 6) { _ in
            let live = await app.rig.live(key)
            return !app.server.running("t1") && (live == nil || live?.phase == .idle)
        }
        // The list hears of it, as when the app comes to the front; a
        // reply's notification waits five seconds for a mark.
        await app.rig.harness.send(.refresh)
        try await app.rig.harness.until(advancing: 6) { harness in
            let state = await harness.state
            return !state.refreshing && app.rig.notifications.pending.keys.contains { $0.hasPrefix("chat|t1|") }
        }
        await app.rig.harness.settle()
        return app.rig.notifications.pending
    }

    private func respond(_ app: Answering, _ note: NotificationRequest, _ action: String, text: String? = nil) -> NotificationResponse {
        let response = NotificationResponse(id: note.id, category: note.category, action: .named(action), userInfo: note.userInfo, text: text)
        app.rig.notifications.respond(response)
        return response
    }

    private func finished(_ app: Answering) -> [NotificationResponse] {
        app.rig.notifications.calls.compactMap { if case let .finished(response) = $0 { response } else { nil } }
    }

    private func scheduled(_ app: Answering) -> [NotificationRequest] {
        app.rig.notifications.calls.compactMap { if case let .schedule(request) = $0 { request } else { nil } }
    }

    @Test func aReplyIsSentUnderAnIdOfItsOwnAndItsNotificationGoes() async throws {
        let app = Answering()
        try await app.signIn()
        let note = try #require(try await away(app, [.piece(0), .piece(1)])["chat|t1|reply"])
        #expect(note.category == ChatNotifications.replyCategory && note.body == "r1.0 r1.1")
        #expect(app.rig.notifications.categories.first { $0.id == ChatNotifications.replyCategory }?.actions.first?.input != nil)

        app.server.plan([.piece(0)])
        let response = respond(app, note, "reply", text: "On my way")
        try await app.rig.harness.until(advancing: 0.1) { _ in !finished(app).isEmpty }
        // The server holds it once, under an id the app minted, and the
        // device was told only after: the notification is taken back, and
        // nothing says it failed.
        #expect(app.server.userRows(saying: "On my way") == 1 && app.server.chatRequests(saying: "On my way") == 1)
        #expect(finished(app) == [response])
        #expect(app.rig.notifications.calls.contains(.cancel(note.id)))
        #expect(!scheduled(app).contains { $0.id == "chat|t1|failed" })
        // It was in the folder before its request left.
        let sent = try #require(app.server.rows(of: "t1").last { $0.role == "user" })
        #expect(sent.content == "On my way" && sent.id.count == 36)
        // The chat was not opened for it: the conversation that sent it is
        // no window's.
        #expect(await app.rig.harness.state.scenes["s"]?.current == nil)
        app.server.finishTurns()
    }

    @Test func belowTheRevisionThatTakesAnIdNothingIsRepliedToFromANotification() async throws {
        let app = Answering(agents: 2)
        try await app.signIn()
        let note = try #require(try await away(app, [.piece(0), .piece(1)])["chat|t1|reply"])
        #expect(note.category == nil)
    }

    @Test func aReplyWhoseAnswerIsLostIsSentAgainUnderItsIdAndWrittenOnce() async throws {
        let app = Answering()
        try await app.signIn()
        let note = try #require(try await away(app, [.piece(0), .piece(1)])["chat|t1|reply"])
        app.server.plan([.piece(0)])
        app.server.fail("chat|t1|On my way", with: .dropped, lands: true)
        let response = respond(app, note, "reply", text: "On my way")
        try await app.rig.harness.until(advancing: 2) { _ in !finished(app).isEmpty }
        #expect(app.server.userRows(saying: "On my way") == 1 && app.server.chatRequests(saying: "On my way") == 2)
        #expect(finished(app) == [response] && !scheduled(app).contains { $0.id == "chat|t1|failed" })
        app.server.finishTurns()
    }

    @Test func aReplyTheServerRefusesIsSaidAndKeptInTheChat() async throws {
        let app = Answering()
        try await app.signIn()
        let note = try #require(try await away(app, [.piece(0), .piece(1)])["chat|t1|reply"])
        app.server.fail("chat|t1|On my way", with: .status(429, code: "rate_limited", retryAfter: nil))
        // What the folder holds as the device is told the action is over:
        // the system may suspend the app at that word.
        let folder = Kept()
        let drafts = app.rig.root.appendingPathComponent("drafts.json")
        app.rig.notifications.onFinished { _ in folder.set((try? String(contentsOf: drafts, encoding: .utf8)) ?? "") }
        let response = respond(app, note, "reply", text: "On my way")
        try await app.rig.harness.until(advancing: 0.1) { _ in !finished(app).isEmpty }
        #expect(folder.text.contains("On my way"))
        // What it could not do is said first, in the chat's own sentence,
        // and the device is told after.
        let failed = try #require(scheduled(app).first { $0.id == "chat|t1|failed" })
        #expect(failed.body == "Your message wasn't sent. Your reply is kept in the chat." && failed.userInfo[ChatNotifications.threadKey] == "t1")
        #expect(finished(app) == [response] && app.server.userRows(saying: "On my way") == 0)
        let calls = app.rig.notifications.calls
        #expect(try #require(calls.firstIndex(of: .schedule(failed))) < (try #require(calls.firstIndex(of: .finished(response)))))
        // Opening it lands on the chat with the text kept: in the composer,
        // and in the folder.
        app.rig.notifications.respond(NotificationResponse(id: failed.id, category: nil, action: .open, userInfo: failed.userInfo))
        try await app.rig.harness.until { await $0.presentation.routePending }
        await app.rig.harness.send(.claimRoute(scene: "s"))
        let key = try #require(await app.rig.harness.state.scenes["s"]?.current)
        #expect(await app.rig.live(key)?.draft == "On my way")
        #expect(await app.rig.harness.state.drafts["t1"] == "On my way")
    }

    @Test func anActionHeardTwiceRunsOnce() async throws {
        let app = Answering()
        try await app.signIn()
        let note = try #require(try await away(app, [.piece(0), .piece(1)])["chat|t1|reply"])
        app.server.plan([.piece(0)])
        _ = respond(app, note, "reply", text: "On my way")
        _ = respond(app, note, "reply", text: "On my way")
        try await app.rig.harness.until(advancing: 0.1) { _ in !finished(app).isEmpty }
        app.server.finishTurns()
        await app.rig.harness.settle()
        _ = respond(app, note, "reply", text: "On my way")
        await app.rig.harness.settle()
        #expect(app.server.userRows(saying: "On my way") == 1 && app.server.chatRequests(saying: "On my way") == 1)
    }

    @Test func anActionThatOutlivesItsTimeLeavesTheMessageStandingAndSaysSo() async throws {
        let app = Answering()
        try await app.signIn()
        let note = try #require(try await away(app, [.piece(0), .piece(1)])["chat|t1|reply"])
        // Every copy hangs past the time an action has.
        for _ in 0 ..< 4 { app.server.fail("chat|t1|On my way", with: .hang) }
        let response = respond(app, note, "reply", text: "On my way")
        try await app.rig.harness.until(advancing: 30) { _ in !finished(app).isEmpty }
        let failed = try #require(scheduled(app).first { $0.id == "chat|t1|failed" })
        #expect(failed.body == "No answer came back for your message. Your reply is kept in the chat.")
        #expect(finished(app) == [response])
        // Nothing is left half done: no copy is out, and the message
        // stands at its place in the chat with Send Again.
        let key = try #require(await app.rig.harness.state.key(of: "t1"))
        try await app.rig.harness.until(advancing: 0.1) { _ in await app.rig.live(key)?.standing == true }
        let live = try #require(await app.rig.live(key))
        #expect(live.phase == .idle && live.run == nil && live.pending?.message == "On my way")
        await app.rig.harness.send(.show(thread: "t1", scene: "s"))
        #expect(try #require(await app.rig.card(key)).button?.action == .sendAgain)
    }

    @Test func aReplyHeardBeforeTheAppWasReadyWaitsAndIsSent() async throws {
        let app = Answering()
        try await app.signIn()
        let note = try #require(try await away(app, [.piece(0), .piece(1)])["chat|t1|reply"])
        // The app is launched again by the action: its response is there
        // before the folder or the server's document is read.
        let again = app.relaunched()
        app.server.plan([.piece(0)])
        let response = NotificationResponse(id: note.id, category: note.category, action: .named("reply"), userInfo: note.userInfo, text: "On my way")
        again.notifications.respond(response)
        try await again.signIn()
        try await again.harness.until(advancing: 0.1) { _ in again.notifications.calls.contains(.finished(response)) }
        #expect(!again.notifications.calls.contains { if case let .schedule(request) = $0 { request.id == "chat|t1|failed" } else { false } })
        #expect(app.server.userRows(saying: "On my way") == 1)
        app.server.finishTurns()
    }

    /// The engine alone. A reply typed into a notification that launched
    /// the app, whose folder is not read inside the action's time: there is
    /// nowhere to write the text, so the notification says it whole, and it
    /// is the chat's draft, ahead of what the folder held, once the folder
    /// is read.
    @Test func aReplyWhoseTimeEndsBeforeTheFolderIsReadIsSaidWholeAndIsTheDraftOnceItIs() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        let lease = try #require(state.disk)
        state.disk = nil
        let response = NotificationResponse(
            id: "chat|t1|reply", category: ChatNotifications.replyCategory, action: .named("reply"),
            userInfo: [ChatNotifications.threadKey: "t1", ChatNotifications.nonceKey: "n"], text: "On my way")
        let stamp = ChatNotifications.stamp(response)
        state = try app.apply(.init(.notificationActed(response, key: ChildKey("k"), message: "m1", at: Date(timeIntervalSince1970: 0))), to: state)
        #expect(state.asks.contains(.actionWait(stamp, seconds: ChatApp.actionSeconds)) && state.deferred.count == 1)
        state.asks = []
        state = try app.apply(.init(.actionDue(stamp)), to: state)
        let over = state.asks.compactMap { ask -> (NotificationRequest?, KeptDrafts?)? in
            if case let .finishAction(told, _, notify, keep) = ask, told == response { (notify, keep) } else { nil }
        }
        #expect(over.count == 1 && over.first?.1 == nil)
        #expect(over.first?.0?.body == "The server took too long to answer. Your reply was not sent or saved. You wrote: “On my way”")
        #expect(state.unkept == ["t1": "On my way"] && state.deferred.isEmpty)
        state.asks = []
        state = try app.apply(
            .init(.diskOpened(.init(lease: lease, state: nil, drafts: ["t1": "Earlier words"], preferences: ChatPreferences(), stale: []))), to: state)
        #expect(state.drafts["t1"] == Conversation.above("On my way", "Earlier words") && state.unkept.isEmpty)
        #expect(state.asks.contains(.saveDrafts(lease, state.drafts, atOnce: true)))
    }

    /// The engine alone. With the folder read and the server's document
    /// not, a reply's time runs out: its text is the chat's draft, and the
    /// drafts ride the word that tells the device, to be written before it.
    @Test func aReplyWhoseTimeEndsWithTheFolderReadIsWrittenBeforeTheDeviceIsTold() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        let lease = try #require(state.disk)
        let response = NotificationResponse(
            id: "chat|t1|reply", category: ChatNotifications.replyCategory, action: .named("reply"),
            userInfo: [ChatNotifications.threadKey: "t1", ChatNotifications.nonceKey: "n"], text: "On my way")
        let stamp = ChatNotifications.stamp(response)
        state = try app.apply(.init(.notificationActed(response, key: ChildKey("k"), message: "m1", at: Date(timeIntervalSince1970: 0))), to: state)
        #expect(state.deferred.count == 1)
        state = try app.apply(.init(.actionDue(stamp)), to: state)
        #expect(state.drafts["t1"] == "On my way")
        let keep = state.asks.compactMap { ask -> KeptDrafts? in
            if case let .finishAction(told, _, notify, keep) = ask, told == response, notify?.body?.hasSuffix("Your reply is kept in the chat.") == true {
                keep
            } else {
                nil
            }
        }
        #expect(keep == [KeptDrafts(lease: lease, drafts: ["t1": "On my way"])])
    }

    @Test func anApprovalIsDecidedOverTheVersionItsNotificationShowed() async throws {
        let app = Answering()
        try await app.signIn()
        let notes = try await away(app, [.piece(0), .tool(.approve), .call, .piece(0)])
        let card = try #require(app.server.allCards.first)
        let note = try #require(notes["chat|t1|card|\(card.id)"])
        // Enough to decide: what would run, with what, to whom.
        #expect(note.category == ChatNotifications.approvalCategory)
        #expect(note.body == "r3.0\nWants your approval to run gmail_send.\nSubject: Subject \(card.id)\nTo: ada@example.com")
        #expect(notes["chat|t1|reply"] == nil)
        #expect(note.userInfo[ChatNotifications.versionKey] == "1" && note.userInfo[ChatNotifications.cardKey] == card.id)
        let response = respond(app, note, "approve")
        try await app.rig.harness.until(advancing: 0.1) { _ in !finished(app).isEmpty }
        #expect(app.server.card(card.id)?.state == "done" && app.server.card(card.id)?.resolutions == 1)
        #expect(finished(app) == [response] && app.rig.notifications.calls.contains(.cancel(note.id)))
        #expect(!scheduled(app).contains { $0.id == "chat|t1|failed" })
        app.server.finishTurns()
    }

    @Test func oneThatChangedSinceIsRefusedAndSaidNeverDecidedUnseen() async throws {
        let app = Answering()
        try await app.signIn()
        let notes = try await away(app, [.piece(0), .tool(.approve), .call, .piece(0)])
        let card = try #require(app.server.allCards.first)
        let note = try #require(notes["chat|t1|card|\(card.id)"])
        // Declined on another device meanwhile.
        app.server.elsewhere(card: card.id, accept: false)
        let response = respond(app, note, "approve")
        try await app.rig.harness.until(advancing: 0.1) { _ in !finished(app).isEmpty }
        #expect(app.server.card(card.id)?.state == "denied" && app.server.card(card.id)?.resolutions == 1)
        let failed = try #require(scheduled(app).first { $0.id == "chat|t1|failed" })
        #expect(failed.body == "It changed since you read it. Check it again. Open the chat to answer it there.")
        #expect(finished(app) == [response])
        app.server.finishTurns()
    }

    @Test func aSingleQuestionOfAFewOptionsIsAnsweredByOneOfThem() async throws {
        let app = Answering()
        try await app.signIn()
        let notes = try await away(app, [.piece(0), .tool(.ask), .call, .piece(0)])
        let card = try #require(app.server.allCards.first)
        let note = try #require(notes["chat|t1|card|\(card.id)"])
        guard card.questions.count == 1, card.questions[0].options.count <= 3, !card.questions[0].multi else {
            // This seed's question is one a notification only opens.
            #expect(note.category == nil)
            return
        }
        // Its options are its own category's actions, registered before
        // it was shown.
        // It is the one notification of that turn, so it says what the
        // reply said above what it asks.
        #expect(note.category == ChatNotifications.questionCategory(card.id) && note.body == "r3.0\n" + card.questions[0].prompt)
        #expect(notes["chat|t1|reply"] == nil)
        let category = try #require(app.rig.notifications.categories.first { $0.id == note.category })
        #expect(category.actions.map(\.title) == card.questions[0].options.map { "Option \($0)" })
        let calls = app.rig.notifications.calls
        let registered = try #require(calls.firstIndex { if case let .register(all) = $0 { all.contains { $0.id == note.category } } else { false } })
        #expect(registered < (try #require(calls.firstIndex(of: .schedule(note)))))
        let response = respond(app, note, "option.1")
        try await app.rig.harness.until(advancing: 0.1) { _ in !finished(app).isEmpty }
        #expect(app.server.card(card.id)?.answers == ["q1": [card.questions[0].options[1]]] && app.server.card(card.id)?.resolutions == 1)
        #expect(finished(app) == [response])
        app.server.finishTurns()
    }

    @Test func whatANotificationCannotShowWholeItOnlyOpens() {
        func approval(_ body: String) -> Record {
            Record(
                id: "p1", kind: Kinds.approval,
                properties: ["state": "pending", "tool": "gmail_send", "arguments": ["to": "ada@example.com", "body": .string(body)]], version: 3)
        }
        let short = ChatNotifications.attention(record: approval("Friday works."), thread: "t1", agent: "Moss", title: "Lunch")
        #expect(short.category == ChatNotifications.approvalCategory && short.body?.contains("Body: Friday works.") == true)
        let long = ChatNotifications.attention(record: approval(String(repeating: "A long body. ", count: 40)), thread: "t1", agent: "Moss", title: "Lunch")
        #expect(long.category == ChatNotifications.openCategory)
        #expect(long.body == "Wants your approval to run gmail_send. Open the chat to read all of what it would run with.")

        func question(options: Int, multi: Bool = false, questions: Int = 1, label: String = "Yes") -> Record {
            let option: JSONValue = ["value": "v", "label": .string(label)]
            let one: JSONValue = ["id": "q1", "prompt": "Which day?", "multi": .bool(multi), "options": .array((0 ..< options).map { _ in option })]
            return Record(
                id: "i1", kind: Kinds.interaction, properties: ["state": "pending", "questions": .array((0 ..< questions).map { _ in one })], version: 1)
        }
        #expect(ChatNotifications.answerable(question(options: 3)) != nil)
        // More than three options, several questions, several answers to
        // one, or a label too long to read on a button: answered in the chat.
        #expect(ChatNotifications.answerable(question(options: 4)) == nil)
        #expect(ChatNotifications.answerable(question(options: 2, multi: true)) == nil)
        #expect(ChatNotifications.answerable(question(options: 2, questions: 2)) == nil)
        #expect(ChatNotifications.answerable(question(options: 2, label: String(repeating: "long ", count: 10))) == nil)
        #expect(ChatNotifications.attention(record: question(options: 4), thread: "t1", agent: "Moss", title: "Lunch").category == nil)
        // The same response is told from the next reply's under the same id.
        let first = NotificationResponse(id: "chat|t1|reply", category: nil, action: .named("reply"), userInfo: ["nonce": "a"], text: "Hi")
        let next = NotificationResponse(id: "chat|t1|reply", category: nil, action: .named("reply"), userInfo: ["nonce": "b"], text: "Hi")
        #expect(ChatNotifications.stamp(first) != ChatNotifications.stamp(next))
    }
}

/// Answering beside the list's pins and archives (decision 0322): an
/// archived chat is silent but while it waits, a person's own message ends
/// its archive, and one notification says a turn that ended on a card.
@Suite("Answering in a list that archives", .timeLimit(.minutes(10)))
struct ArchivedAnswering {
    private func pending(_ app: Answering) -> [String: NotificationRequest] { app.rig.notifications.pending }

    /// The person archives a chat the list holds: the list is read first,
    /// since a chat just started is not in it yet.
    private func archive(_ app: Answering, _ thread: String) async throws {
        await app.rig.harness.send(.refresh)
        try await app.rig.harness.until(advancing: 1) { harness in
            let state = await harness.state
            return !state.refreshing && state.thread(thread) != nil
        }
        await app.rig.harness.send(.archive(thread: thread, archived: true))
        try await app.rig.harness.until(advancing: 1) { _ in app.server.mark(of: thread)?.archived == true }
        try await caughtUp(app)
    }

    /// Whatever the watch reported has been read again, and every write of
    /// a mark answered.
    private func caughtUp(_ app: Answering) async throws {
        try await app.rig.harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.feedBatch.isEmpty && state.feedApplying == nil && !state.refreshing && state.markSending.isEmpty && state.marking.isEmpty
        }
    }

    /// The whole of it, with no list refresh asked for: an archived chat
    /// comes to wait on a card, so it is listed, counted and notified for;
    /// the card answered from its notification, it waits no more, and is
    /// in no list, count or notification again.
    @Test func aCardPutsAnArchivedChatInTheListAndAnsweredFromItsNotificationTakesItOut() async throws {
        let app = Answering(records: 9)
        try await app.signIn()
        try await app.rig.harness.until { harness in await harness.state.flags }
        _ = try await app.chat([.piece(0), .tool(.ask), .call, .piece(0)])
        await app.rig.harness.send(.back(scene: "s"))
        try await archive(app, "t1")
        #expect(await app.rig.listed() == [])
        // Its turn ends on a question, with nobody looking.
        app.server.finishTurns()
        try await app.rig.harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.waiting.contains { $0.id == "t1" } && pending(app).keys.contains { $0.hasPrefix("chat|t1|card|") }
        }
        try await caughtUp(app)
        let card = try #require(app.server.allCards.first)
        #expect(await app.rig.listed() == ["t1"])
        #expect(await app.rig.harness.presentation.unread == 1)
        // One notification: the card's, which says the reply too.
        let note = try #require(pending(app)["chat|t1|card|\(card.id)"])
        #expect(pending(app).keys.filter { $0.hasPrefix("chat|t1|") } == [note.id] && note.body?.hasPrefix("r3.0\n") == true)
        #expect(await app.rig.harness.state.announced == ["t1"])

        // Answered from the notification, where it can be; else on another
        // device. Either way nothing waits in the chat.
        if note.category == ChatNotifications.questionCategory(card.id) {
            app.rig.notifications.respond(
                NotificationResponse(id: note.id, category: note.category, action: .named("option.0"), userInfo: note.userInfo))
        } else {
            app.server.elsewhere(card: card.id, accept: true)
        }
        try await app.rig.harness.until(advancing: 1) { _ in app.server.card(card.id)?.pending == false }
        app.server.finishTurns()
        try await app.rig.harness.until(advancing: 6) { harness in
            let state = await harness.state
            return !app.server.running("t1") && !state.waiting.contains { $0.id == "t1" } && state.thread("t1")?.running != true
        }
        try await caughtUp(app)
        #expect(app.server.card(card.id)?.resolutions == 1 && app.server.mark(of: "t1")?.archived == true)
        #expect(await app.rig.listed() == [])
        #expect(await app.rig.harness.presentation.unread == 0)
        // The reply that followed the answer is an archived chat's: silent.
        // and nothing is kept as standing for it.
        try await app.rig.harness.until(advancing: 6) { harness in
            let state = await harness.state
            return state.announced.isEmpty && !pending(app).keys.contains { $0.hasPrefix("chat|t1|") }
        }
    }

    /// A chat is read on another device while its card still waits: the
    /// card's notification stands. The list's own take-back of what a read
    /// answers took the chat's whole group, the card's with it (the review
    /// of the join), with nothing to post it again.
    @Test func aReadOnAnotherDeviceLeavesAWaitingCardsNotification() async throws {
        let app = Answering(records: 9)
        try await app.signIn()
        _ = try await app.chat([.piece(0), .tool(.ask), .call, .piece(0)])
        await app.rig.harness.send(.back(scene: "s"))
        app.server.finishTurns()
        try await app.rig.harness.until(advancing: 1) { _ in pending(app).keys.contains { $0.hasPrefix("chat|t1|card|") } }
        try await caughtUp(app)
        let card = try #require(app.server.allCards.first)
        // Unread here, so the read elsewhere is one the list hears of.
        #expect(await app.rig.harness.presentation.unread == 1)
        #expect(app.server.elsewhere("t1", unread: false))
        try await app.rig.harness.until(advancing: 1) { harness in await harness.presentation.unread == 0 }
        try await caughtUp(app)
        #expect(app.server.card(card.id)?.pending == true)
        #expect(pending(app)["chat|t1|card|\(card.id)"] != nil)
        #expect(await app.rig.harness.state.announced == ["t1"])
    }

    @Test func aMessageSentWhileAnArchivedChatsReplyRunsEndsItsArchiveAndIsNotHeld() async throws {
        let app = Answering(records: 9)
        try await app.signIn()
        try await app.rig.harness.until { harness in await harness.state.flags }
        let key = try await app.chat([.piece(0), .piece(1), .piece(2)])
        app.work(2)
        try await app.until { screen, _ in screen.streaming && screen.composer.queues }
        try await archive(app, "t1")
        app.server.plan([.piece(0)])
        try await app.say("And the mint?", key)
        // The message is written at once, and the archive ends with it.
        try await app.rig.harness.until(advancing: 1) { _ in
            app.server.userRows(saying: "And the mint?") == 1 && app.server.mark(of: "t1")?.archived == false
        }
        app.server.finishTurns()
        try await app.settled(key)
        #expect(app.server.userRows(saying: "And the mint?") == 1 && app.server.chatRequests(saying: "And the mint?") == 1)
    }

    @Test func anAnswerInThePersonsOwnWordsEndsAnArchivedChatsArchive() async throws {
        let app = Answering(records: 9)
        try await app.signIn()
        try await app.rig.harness.until { harness in await harness.state.flags }
        let key = try await app.chat([.piece(0), .tool(.ask), .call, .piece(0)])
        app.server.finishTurns()
        try await app.settled(key)
        let card = try #require(app.server.allCards.first)
        try await archive(app, "t1")
        try await app.rig.send(.answerInWords(card.id), to: key)
        app.server.plan([.piece(0)])
        try await app.say("Thursday, after eleven", key)
        try await app.rig.harness.until(advancing: 1) { _ in
            app.server.userRows(saying: "Thursday, after eleven") == 1 && app.server.mark(of: "t1")?.archived == false
                && app.server.card(card.id)?.state == "replied"
        }
        app.server.finishTurns()
    }

    /// The case a device cannot see coming: its notification stands, the
    /// chat is archived on another device, and the reply typed into the
    /// notification launches the app, which last knew the chat as not
    /// archived. The reply goes, once, and the archive ends with it.
    @Test func aReplyFromANotificationIntoAChatArchivedOnAnotherDeviceEndsItsArchive() async throws {
        let app = Answering(records: 9)
        try await app.signIn()
        try await app.rig.harness.until { harness in await harness.state.flags }
        let key = try await app.chat([.piece(0), .piece(1)])
        await app.rig.harness.send(.back(scene: "s"))
        app.server.finishTurns()
        try await app.rig.harness.until(advancing: 6) { _ in
            let live = await app.rig.live(key)
            return !app.server.running("t1") && (live == nil || live?.phase == .idle)
        }
        await app.rig.harness.send(.refresh)
        try await app.rig.harness.until(advancing: 6) { harness in
            let state = await harness.state
            return !state.refreshing && pending(app)["chat|t1|reply"] != nil
        }
        let note = try #require(pending(app)["chat|t1|reply"])
        // The app is gone; the chat is archived elsewhere; the reply's
        // action launches the app again.
        let again = app.relaunched()
        app.server.elsewhere("t1", archived: true)
        app.server.plan([.piece(0)])
        let response = NotificationResponse(id: note.id, category: note.category, action: .named("reply"), userInfo: note.userInfo, text: "On my way")
        again.notifications.respond(response)
        try await again.signIn()
        try await again.harness.until(advancing: 0.5) { _ in
            again.notifications.calls.contains(.finished(response)) && app.server.mark(of: "t1")?.archived == false
        }
        #expect(app.server.userRows(saying: "On my way") == 1)
        #expect(!again.notifications.calls.contains { if case let .schedule(request) = $0 { request.id == "chat|t1|failed" } else { false } })
        app.server.finishTurns()
    }

    /// The same from a launch that holds nothing of the chat: its folder
    /// keeps the list's first page alone, and an archived chat is in no
    /// list. The reply waits for the thread's row, read by name, and then
    /// goes as any reply does. It was given back at once before, for want
    /// of the agent the row names, and what said so was then taken back
    /// with the archived chat's notifications (found on a simulator
    /// against a server, with the app not running).
    @Test func aReplyFromANotificationInALaunchThatHoldsNoRowOfItsChatWaitsForTheRow() async throws {
        let app = Answering(records: 9)
        try await app.signIn()
        try await app.rig.harness.until { harness in await harness.state.flags }
        let key = try await app.chat([.piece(0), .piece(1)])
        await app.rig.harness.send(.back(scene: "s"))
        app.server.finishTurns()
        try await app.rig.harness.until(advancing: 6) { _ in
            let live = await app.rig.live(key)
            return !app.server.running("t1") && (live == nil || live?.phase == .idle)
        }
        app.server.elsewhere("t1", archived: true)
        app.server.plan([.piece(0)])
        // Another folder: nothing kept of the chat.
        let again = ChatRig([], transport: app.server.connect(), clock: app.rig.clock, ids: app.ids)
        let response = NotificationResponse(
            id: "pushed", category: ChatNotifications.replyCategory, action: .named("reply"),
            userInfo: [ChatNotifications.threadKey: "t1", ChatNotifications.nonceKey: "n"], text: "On my way")
        again.notifications.respond(response)
        try await again.signIn()
        try await again.harness.until(advancing: 0.5) { _ in
            again.notifications.calls.contains(.finished(response)) && app.server.mark(of: "t1")?.archived == false
        }
        #expect(app.server.userRows(saying: "On my way") == 1 && app.server.chatRequests(saying: "On my way") == 1)
        #expect(!again.notifications.calls.contains { if case let .schedule(request) = $0 { request.id == "chat|t1|failed" } else { false } })
        app.server.finishTurns()
    }

    /// Two replies into one chat, both heard before its row is read: the
    /// conversation is handed one reply at a time, so the second goes when
    /// the first has, and neither is lost (seed 657 at 53 steps lost the
    /// first, once a reply waited for a row).
    @Test func twoRepliesIntoOneChatGoOneAfterTheOther() async throws {
        let app = Answering(records: 9)
        try await app.signIn()
        let key = try await app.chat([.piece(0), .piece(1)])
        await app.rig.harness.send(.back(scene: "s"))
        app.server.finishTurns()
        try await app.rig.harness.until(advancing: 6) { _ in
            let live = await app.rig.live(key)
            return !app.server.running("t1") && (live == nil || live?.phase == .idle)
        }
        app.server.plan([.piece(0)])
        app.server.plan([.piece(0)])
        let again = ChatRig([], transport: app.server.connect(), clock: app.rig.clock, ids: app.ids)
        let replies = ["On my way", "Five minutes"].map { text in
            NotificationResponse(
                id: "pushed", category: ChatNotifications.replyCategory, action: .named("reply"),
                userInfo: [ChatNotifications.threadKey: "t1", ChatNotifications.nonceKey: "n"], text: text)
        }
        for reply in replies { again.notifications.respond(reply) }
        try await again.signIn()
        try await again.harness.until(advancing: 0.5) { _ in
            app.server.finishTurns()
            return replies.allSatisfy { again.notifications.calls.contains(.finished($0)) }
        }
        #expect(app.server.userRows(saying: "On my way") == 1 && app.server.userRows(saying: "Five minutes") == 1)
        let said = app.server.rows(of: "t1").filter { $0.role == "user" }.map(\.content)
        #expect(said.suffix(2) == ["On my way", "Five minutes"])
        app.server.finishTurns()
    }

    /// The engine alone: the mark read beside a reply from a notification
    /// says the chat was archived, which the device had not heard. The
    /// archive ends, by the person's own write.
    @Test func aMarkReadBesideAReplySaysTheChatWasArchivedAndTheArchiveEnds() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        state.recordsRevision = 9
        state.agentsRevision = 6
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        state.chats = [Record(id: "t1", kind: Kinds.thread, properties: ["status": "ok", "agent": ["ref": .string("\(Kinds.agent)/\(helper)")]], version: 2)]
        #expect(state.flags && !state.archived("t1"))
        let response = NotificationResponse(
            id: "chat|t1|reply", category: ChatNotifications.replyCategory, action: .named("reply"),
            userInfo: [ChatNotifications.threadKey: "t1", ChatNotifications.nonceKey: "n"], text: "On my way")
        state = try app.apply(.init(.notificationActed(response, key: ChildKey("k"), message: "m9", at: Date(timeIntervalSince1970: 0))), to: state)
        // The conversation sends it, and with its word of that the mark is
        // read beside the message.
        spoke(app, &state, in: "t1")
        #expect(state.asks.contains(.confirmMark("t1")))
        let mark = Record(
            id: "t1.me", kind: Kinds.threadMark,
            properties: ["thread": ["ref": .string("\(Kinds.thread)/t1")], "readThrough": "m1", "archived": true], version: 2)
        state.asks = []
        state = try app.apply(.init(.markConfirmed(thread: "t1", .init(threads: ["t1"], records: [mark]))), to: state)
        #expect(state.marking["t1"]?.archived == false || state.markSending["t1"]?.archived == false)
        #expect(!state.archived("t1"))
    }
}

private func spoke(_ app: ChatApp, _ state: inout ChatAppState, in thread: String) { app.spoke(&state, in: thread) }

/// The mark read beside a message says what the chat was when its person
/// spoke. It must not undo what they did after.
@Suite("A mark read beside a message")
struct SpokeThenArchived {
    @Test func aChatArchivedHereAfterItsPersonSpokeStaysArchived() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        state.recordsRevision = 9
        state.agentsRevision = 7
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        state.chats = [
            Record(
                id: "t1", kind: Kinds.thread, properties: ["status": "ok", "mode": "chat", "agent": ["ref": .string("\(Kinds.agent)/\(helper)")]],
                version: 2)
        ]
        spoke(app, &state, in: "t1")
        #expect(state.asks.contains(.confirmMark("t1")))
        // They archive it before the read is answered: the read is let go.
        state.asks = []
        state = try app.apply(.init(.archiveAsked(thread: "t1", archived: true, at: Date(timeIntervalSince1970: 10))), to: state)
        #expect(state.asks.contains(.cancel("confirmMark:t1")) && state.archived("t1"))
        // And one already on its way back changes nothing, whatever it
        // says: unarchived as it was when they spoke, or archived as they
        // have since made it.
        func mark(archived: Bool) -> Record {
            Record(
                id: "t1.me", kind: Kinds.threadMark,
                properties: ["thread": ["ref": .string("\(Kinds.thread)/t1")], "readThrough": "m1", "archived": .bool(archived)], version: 2)
        }
        for said in [false, true] {
            var after = state
            after.asks = []
            after = try app.apply(.init(.markConfirmed(thread: "t1", .init(threads: ["t1"], records: [mark(archived: said)]))), to: after)
            #expect(after.archived("t1"))
            #expect(after.marking["t1"]?.archived != false && after.markSending["t1"]?.archived != false)
        }
    }
}

/// One notification for a turn that ends waiting on a card, on the engine
/// alone: the card's, with what the reply said, whichever of the card and
/// the thread's row is read first.
@Suite("A turn that ends on a card is one notification")
struct OneNotification {
    let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
    let asked = "2026-11-15T10:00:05Z"

    private func start() -> ChatAppState {
        var state = signedInState()
        state.baselined = true
        state.recordsRevision = 8
        state.agentsRevision = 6
        state.marks = ["t1": ThreadMark(readThrough: "m0", version: 1)]
        state.chats = [row(running: true, version: 2)]
        return state
    }

    private func row(running: Bool, version: Int, finished: String = "2026-11-15T10:00:06Z", reply: String = "One thing before I book it.") -> Record {
        var properties: [String: JSONValue] = [
            "status": .string(running ? "running" : "ok"), "agent": ["ref": .string("\(Kinds.agent)/\(helper)")], "mode": "chat",
            "lastMessage": ["ref": .string("\(Kinds.message)/m\(version)")],
        ]
        if !running {
            properties["finishedAt"] = .string(finished)
            properties["lastReply"] = .string(reply)
            properties["waitingOnPerson"] = 1
        }
        return Record(id: "t1", kind: Kinds.thread, properties: properties, version: version)
    }

    private var card: Record {
        var record = Record(
            id: "i1", kind: Kinds.interaction,
            properties: [
                "state": "pending", "thread": ["ref": .string("\(Kinds.thread)/t1")],
                "questions": [["id": "q1", "prompt": "Which morning?", "options": [["value": "tue", "label": "Tuesday"]]]],
            ], version: 1)
        record.createdAt = asked
        return record
    }

    private func posted(_ state: ChatAppState) -> [NotificationRequest] {
        state.asks.compactMap { ask -> NotificationRequest? in
            switch ask {
            case let .notifyCard(request, _), let .notify(request), let .notifyHeld(request, _): request
            default: nil
            }
        }
    }

    @Test func theCardReadFirstWaitsForItsTurnToSettleAndSaysTheReply() throws {
        var state = try app.apply(.init(.cardsRead(.init(interactions: [card], requests: [], approvals: []))), to: start())
        // Its turn still runs: held, a few seconds at most.
        #expect(posted(state).isEmpty && state.asks.contains(.cardHold("i1", seconds: ChatApp.cardHold)))
        state.asks = []
        state = try app.apply(.init(.threadFetched(row(running: false, version: 3))), to: state)
        let notes = posted(state)
        #expect(notes.map(\.id) == ["chat|t1|card|i1"] && notes.first?.body == "One thing before I book it.\nWhich morning?")
        #expect(state.announced == ["t1"] && state.asks.contains(.cancel("cardHold:i1")))
    }

    @Test func theRowReadFirstIsSaidByTheCardThatFollowsIt() throws {
        var state = try app.apply(.init(.threadFetched(row(running: false, version: 3))), to: start())
        // The reply's own waits for a mark, as any reply's does.
        #expect(posted(state).map(\.id) == ["chat|t1|reply"])
        state.asks = []
        state = try app.apply(.init(.cardsRead(.init(interactions: [card], requests: [], approvals: []))), to: state)
        // The card's is posted with the reply, and the reply's own is
        // taken back before it is shown.
        #expect(posted(state).map(\.id) == ["chat|t1|card|i1"] && posted(state).first?.body == "One thing before I book it.\nWhich morning?")
        #expect(state.asks.contains(.cancelNotification("chat|t1|reply")))
    }

    @Test func aCardHeldAsLongAsItMayBeIsPostedAloneAndItsTurnsEndSaysNothingMore() throws {
        var state = try app.apply(.init(.cardsRead(.init(interactions: [card], requests: [], approvals: []))), to: start())
        state.asks = []
        state = try app.apply(.init(.cardHoldDue("i1")), to: state)
        #expect(posted(state).map(\.id) == ["chat|t1|card|i1"] && posted(state).first?.body == "Which morning?")
        state.asks = []
        state = try app.apply(.init(.threadFetched(row(running: false, version: 3))), to: state)
        #expect(posted(state).isEmpty)
    }

    /// A card stands for the turn that asked and no later one: a reply to
    /// a message sent while the card still waits is news of its own.
    @Test func aLaterReplyWhileTheCardStillWaitsNotifiesAsAnyReplyDoes() throws {
        var state = try app.apply(.init(.cardsRead(.init(interactions: [card], requests: [], approvals: []))), to: start())
        state = try app.apply(.init(.threadFetched(row(running: false, version: 3))), to: state)
        state.asks = []
        state = try app.apply(.init(.threadFetched(row(running: true, version: 4))), to: state)
        state = try app.apply(
            .init(.threadFetched(row(running: false, version: 5, finished: "2026-11-15T10:03:00Z", reply: "Noted, after eleven."))), to: state)
        #expect(posted(state).map(\.id) == ["chat|t1|reply"] && posted(state).first?.body == "Noted, after eleven.")
    }

    /// A read on another device answers a reply's notification. A card's
    /// stands while its chat waits, and goes once nothing does.
    @Test func aCardsNotificationStandsThroughAReadAndGoesWhenNothingWaits() throws {
        var state = try app.apply(.init(.cardsRead(.init(interactions: [card], requests: [], approvals: []))), to: start())
        state = try app.apply(.init(.threadFetched(row(running: false, version: 3))), to: state)
        state.asks = []
        // Read on another device: the mark is at the newest message.
        let read = Record(
            id: "t1.me", kind: Kinds.threadMark,
            properties: ["thread": ["ref": .string("\(Kinds.thread)/t1")], "readThrough": "m3"], version: 2)
        state = try app.apply(.init(.marksRead(.init(threads: ["t1"], records: [read]))), to: state)
        #expect(!state.asks.contains(.cancelGroup("t1")) && state.announced == ["t1"])
        // Answered there: the row says nothing waits, and the pending
        // lists hold it no more.
        var answered = row(running: false, version: 6, finished: "2026-11-15T10:05:00Z", reply: "Booked.")
        answered.properties["waitingOnPerson"] = nil
        answered.properties["lastMessage"] = ["ref": .string("\(Kinds.message)/m3")]
        state.asks = []
        state = try app.apply(.init(.cardsRead(.init(interactions: [], requests: [], approvals: []))), to: state)
        state = try app.apply(.init(.threadFetched(answered)), to: state)
        // Read, and nothing waits: everything the chat notified goes.
        #expect(state.asks.contains(.cancelGroup("t1")) && state.attention.isEmpty && state.announced.isEmpty)
    }

    /// Answered on another device, with the reply that followed unread:
    /// the card's notification goes on its own, and the chat's reply is
    /// news as any is.
    @Test func aCardAnsweredElsewhereTakesItsOwnNotificationBack() throws {
        var state = try app.apply(.init(.cardsRead(.init(interactions: [card], requests: [], approvals: []))), to: start())
        state = try app.apply(.init(.threadFetched(row(running: false, version: 3))), to: state)
        state.asks = []
        state = try app.apply(.init(.cardsRead(.init(interactions: [], requests: [], approvals: []))), to: state)
        #expect(!state.asks.contains(.cancelNotification("chat|t1|card|i1")))
        var answered = row(running: false, version: 6, finished: "2026-11-15T10:05:00Z", reply: "Booked.")
        answered.properties["waitingOnPerson"] = nil
        state = try app.apply(.init(.threadFetched(answered)), to: state)
        #expect(state.asks.contains(.cancelNotification("chat|t1|card|i1")) && state.attention.isEmpty)
        #expect(posted(state).map(\.id) == ["chat|t1|reply"] && state.announced == ["t1"])
    }

    /// A card of an archived chat is read before the row that counts it:
    /// the row held still says nothing waits. The card's notification is
    /// posted and stands; the stale row does not take it back (the review
    /// of the join found that it did, with nothing to post it again).
    @Test func aCardPostedAheadOfTheRowThatCountsItIsNotTakenBackByThatRow() throws {
        var state = start()
        state.recordsRevision = 9
        state.marks = ["t1": ThreadMark(readThrough: "m4", version: 2, archived: true)]
        var settled = row(running: false, version: 4, finished: "2026-11-15T10:00:06Z")
        settled.properties["waitingOnPerson"] = nil
        state.chats = [settled]
        #expect(state.archived("t1") && !state.notifies("t1"))
        state = try app.apply(.init(.cardsRead(.init(interactions: [card], requests: [], approvals: []))), to: state)
        #expect(posted(state).map(\.id) == ["chat|t1|card|i1"])
        #expect(!state.asks.contains(.cancelGroup("t1")) && state.announced == ["t1"])
        // Any event after it leaves it standing too.
        state.asks = []
        state = try app.apply(.init(.cardsRead(.init(interactions: [card], requests: [], approvals: []))), to: state)
        #expect(!state.asks.contains(.cancelGroup("t1")) && state.announced == ["t1"])
    }

    /// What the seeded runs found (seed 30167 at 45 steps on a server at
    /// `agents` 7, and seed 20442 at 101): an archived chat waits on a
    /// card, so a reply in it is notified. The card is answered on another
    /// device, and the chat's row says nothing waits, while this device
    /// still holds the card among the pending ones (or what an action
    /// could not do still stands for the chat). Those keep what is theirs:
    /// a card's notification kept too long is the cheaper mistake. The
    /// reply's own was said because the chat waited, by the row's count,
    /// and by that count it goes.
    @Test(arguments: [false, true])
    func aReplyNotifiedWhileAnArchivedChatWaitedGoesOnceItsRowSaysNothingWaits(action: Bool) throws {
        var state = start()
        state.recordsRevision = 9
        state.marks = ["t1": ThreadMark(readThrough: "m0", version: 1, archived: true)]
        // The card was said in a launch before this one.
        state.interactions = [card]
        state.notified = ["card|\(Kinds.interaction)/i1"]
        state = try app.apply(.init(.threadFetched(row(running: false, version: 3))), to: state)
        #expect(posted(state).map(\.id) == ["chat|t1|reply"] && state.announced == ["t1"] && state.replySaid == ["t1"])
        if action { app.noteFailed(&state, "t1") }
        // Answered elsewhere: the row counts nothing, and the pending
        // list on this device has not been read again.
        var answered = row(running: false, version: 3)
        answered.properties["waitingOnPerson"] = nil
        answered.version = 4
        if action { state.interactions = [] }
        state.asks = []
        state = try app.apply(.init(.threadFetched(answered)), to: state)
        #expect(!state.notifies("t1") && (state.mayWait("t1") || state.failedActions == ["t1"]))
        #expect(state.asks.contains(.cancelNotification("chat|t1|reply")) && state.replySaid.isEmpty)
        // What else stands for the chat is not taken with it.
        #expect(!state.asks.contains(.cancelGroup("t1")) && state.announced == ["t1"])
        // Once nothing may wait and nothing else stands, the rest goes.
        if !action {
            state.asks = []
            state = try app.apply(.init(.cardsRead(.init(interactions: [], requests: [], approvals: []))), to: state)
            #expect(state.asks.contains(.cancelGroup("t1")) && state.announced.isEmpty)
        }
    }

    /// The one predicate: an archived chat is silent, for a reply and for
    /// what a card's action could not do once the card no longer waits;
    /// a card is itself what waits, and is said.
    @Test func anArchivedChatIsNotifiedForOnlyWhileItWaits() throws {
        var state = start()
        state.recordsRevision = 9
        state.marks = ["t1": ThreadMark(readThrough: "m0", version: 1, archived: true)]
        #expect(state.flags && state.archived("t1"))
        #expect(!state.notifies("t1") && state.notifies("t1", waits: true) && state.notifies(nil))
        // A row that counts something waiting, or a card among the pending.
        #expect(state.notifies("t1", row: ChatThread(record: row(running: false, version: 3))))
        // Where the server counts what waits, the row held is its word: a
        // pending list may still hold a card answered on another device.
        // With no row held, a card among the pending ones says it.
        state.interactions = [card]
        #expect(!state.notifies("t1") && state.mayWait("t1"))
        var unlisted = state
        unlisted.chats = []
        #expect(unlisted.notifies("t1"))
        // Its card notifies; its reply does not.
        state.interactions = []
        var quiet = try app.apply(.init(.threadFetched(row(running: true, version: 3))), to: state)
        var settled = row(running: false, version: 4)
        settled.properties["waitingOnPerson"] = nil
        quiet = try app.apply(.init(.threadFetched(settled)), to: quiet)
        #expect(posted(quiet).isEmpty)
        let asking = try app.apply(.init(.cardsRead(.init(interactions: [card], requests: [], approvals: []))), to: quiet)
        #expect(posted(asking).map(\.id) == ["chat|t1|card|i1"])
    }
}

/// A text a test keeps from another thread.
final class Kept: @unchecked Sendable {
    private let lock = NSLock()
    private var held = ""
    func set(_ text: String) { lock.withLock { held = text } }
    var text: String { lock.withLock { held } }
}
