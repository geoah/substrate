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

    init(agents: Int = 6) {
        let clock = TestClock()
        server = SeededServer(seed: 1, clock: clock, revision: agents)
        rig = ChatRig([], transport: server.connect(), clock: clock)
        self.agents = agents
    }

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
        // and nothing is drawn twice or queued twice.
        await app.rig.harness.send(.phase(.background))
        await app.rig.harness.send(.phase(.active))
        try await app.until { _, live in if case .attach? = live.run { live.connected } else { false } }
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
        // The thread is resumed, and the answer's own line is in it.
        app.server.finishTurns()
        try await app.settled(key)
        #expect(try await app.screen(key).turns.contains { $0.body == .answer(OutcomeLine(text: "You answered", done: true)) })
    }

    @Test func oneResolvedOnAnotherDeviceFoldsWhenTheFeedSaysSo() async throws {
        let app = Answering()
        try await app.signIn()
        let (key, id) = try await asked(app)
        #expect(app.server.elsewhere(card: id, accept: false))
        try await app.until { screen, _ in screen.cards[id]?.resolution != nil }
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
        try await app.until { screen, _ in screen.cards[id]?.resolution != nil }
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
        let again = ChatRig([], transport: app.server.connect(), clock: app.rig.clock)
        try await again.signIn()
        await again.harness.send(.sceneAppeared(scene: "s", restoring: nil))
        await again.harness.send(.show(thread: "t1", scene: "s"))
        let opened = try #require(await again.harness.state.scenes["s"]?.current)
        try await again.harness.until(advancing: 0.1) { _ in (await again.conversation(opened)?.turns.count ?? 0) >= 2 }
        let screen = try #require(await again.conversation(opened))
        let row = try #require(
            screen.turns.compactMap { turn -> ToolRowPresentation? in if case let .assistant(reply) = turn.body { reply.tools.first } else { nil } }.first)
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
        #expect(row.status == .declined && row.statusLabel == "You dismissed the question")
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
        #expect(app.server.card(id)?.state == "dismissed" && app.server.userRows(saying: "Neither: Friday.") == 1)
    }

    @Test func aDismissalThatFailsBringsTheCardBackWithItsFailure() async throws {
        let app = Answering()
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
