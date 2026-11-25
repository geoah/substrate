import Foundation
import SubstrateCore
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import SubstrateChatCore

@Suite("A reply is revealed at a steady rate", .timeLimit(.minutes(10)))
struct TextRevealTests {
    let start = Date(timeIntervalSince1970: 1000)
    let step = 1 / TextReveal.steps

    @Test func aBurstPoursInAndIsAllDrawnWithinTheLag() {
        var reveal = TextReveal()
        var shown: [Int] = []
        var now = start
        // 2000 code points at once, as an attach replays them.
        while let due = reveal.advance(to: 2000, at: now) {
            shown.append(reveal.shown)
            #expect(due == now.addingTimeInterval(step))
            now = due
        }
        #expect(reveal.shown == 2000 && !reveal.waits)
        // More than one step, never back, and done by the lag.
        #expect(shown.count > 3 && shown == shown.sorted() && Set(shown).count == shown.count)
        #expect(now.timeIntervalSince(start) <= TextReveal.lag + step)
    }

    @Test func aSteadyStreamIsDrawnAtItsOwnRateNeverFurtherBehindThanTheLag() {
        var reveal = TextReveal()
        // 400 code points a second in pieces every 50 ms, drawn 30 times a
        // second: what arrived by `lag` ago is always drawn.
        var arrived: [(at: Date, total: Int)] = []
        var total = 0
        var steps: [Int] = []
        for tick in 0 ..< 150 {
            let now = start.addingTimeInterval(Double(tick) * step)
            let due = Int(now.timeIntervalSince(start) / 0.05) * 20
            if due > total {
                total = due
                arrived.append((now, total))
            }
            let before = reveal.shown
            _ = reveal.advance(to: total, at: now)
            steps.append(reveal.shown - before)
            let old = arrived.last { now.timeIntervalSince($0.at) >= TextReveal.lag }?.total ?? 0
            #expect(reveal.shown >= old, "behind by more than the lag at \(tick)")
            #expect(reveal.shown <= total)
        }
        // Even: once it runs, no step draws a whole piece at once.
        #expect(steps.dropFirst(30).allSatisfy { $0 <= 16 })
        #expect(steps.dropFirst(30).filter { $0 > 0 }.count > 100)
    }

    @Test func itNeverMovesBackAndASecondStepAtOneInstantDrawsNothingMore() {
        var reveal = TextReveal()
        _ = reveal.advance(to: 100, at: start)
        let first = reveal.shown
        #expect(first > 0 && first < 100)
        // Made again at the same instant, as a kept part is to be verified.
        _ = reveal.advance(to: 100, at: start)
        #expect(reveal.shown == first)
        // A total that is less (it never is) draws no less.
        _ = reveal.advance(to: 3, at: start.addingTimeInterval(step))
        #expect(reveal.shown == first)
        // A clock that went back steps as one step does.
        _ = reveal.advance(to: 100, at: start.addingTimeInterval(-60))
        #expect(reveal.shown >= first)
    }

    @Test func aTrickleIsDrawnAsItComes() {
        var reveal = TextReveal()
        var now = start
        for total in 1 ... 20 {
            // One code point every 100 ms: drawn at the next step.
            _ = reveal.advance(to: total, at: now)
            #expect(reveal.shown == total)
            now = now.addingTimeInterval(0.1)
        }
    }
}

@Suite("The presenter paces a reply", .timeLimit(.minutes(10)))
struct RevealPresenterTests {
    static let reply = "The balcony plants need water twice a week, and the tomatoes more often than that in a dry May. "

    private func present(_ rig: ChatRig, _ key: ChildKey, _ live: ConversationState, _ state: ChatAppState, at now: Date) async -> ChatPresentation {
        let app = rig.runtime.controller
        let conversation = app.children(in: state)[key]
        return app.present(
            PresenterInput(
                state: state, children: conversation.map { [key: $0.present(PresenterInput(state: live, now: now))] } ?? [:], now: now,
                locale: Locale(identifier: "en_US"), timeZone: TimeZone(identifier: "UTC")!))
    }

    private func text(_ tree: ChatPresentation, _ key: ChildKey) -> String {
        guard case let .assistant(turn)? = tree.conversations[key]?.turns.last?.body else { return "" }
        return turn.text ?? ""
    }

    /// A chat whose reply has streamed its first piece, as the engine
    /// holds it.
    private func streaming(paces: Bool) async throws -> (ChatRig, ChildKey, ChatAppState, ConversationState) {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 1))])
        try await rig.signIn()
        await rig.harness.send(.pace(paces))
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.connected == true }
        return (rig, key, await rig.harness.state, try #require(await rig.live(key)))
    }

    @Test func textIsDrawnOverTheStepsThatFollowItAndTheTreeAsksForEachOne() async throws {
        let (rig, key, state, held) = try await streaming(paces: true)
        let app = rig.runtime.controller
        let conversation = try #require(app.children(in: state)[key])
        var live = held
        let start = rig.clock.now.addingTimeInterval(10)
        live = try conversation.apply(.init(.streamed(.delta(Self.reply, turn: 1, at: 0), at: start)), to: live)

        var now = start
        var drawn: [String] = []
        var tree = await present(rig, key, live, state, at: now)
        while let next = app.nextChange(of: tree) {
            drawn.append(text(tree, key))
            // While it is drawn the reply is still being written.
            if case let .assistant(turn)? = tree.conversations[key]?.turns.last?.body { #expect(turn.live) }
            #expect(next > now && next <= now.addingTimeInterval(1 / TextReveal.steps + 0.001))
            now = next
            tree = await present(rig, key, live, state, at: now)
        }
        drawn.append(text(tree, key))
        // Several steps, each the start of the next, ending on the whole.
        #expect(drawn.count > 4 && drawn.last == Self.reply)
        #expect(zip(drawn, drawn.dropFirst()).allSatisfy { $1.hasPrefix($0) && $1.count > $0.count })
        #expect(now.timeIntervalSince(start) <= TextReveal.lag + 0.05)
        // No part of the app's own moved for it: the conversation's alone.
        #expect(
            app.parts(of: tree).first?.value as? ChatPresentation == app.parts(of: await present(rig, key, held, state, at: start)).first?.value
                as? ChatPresentation)
        rig.server.release()
    }

    @Test func aReplyThatEndedGoesOnBeingDrawnAndIsFinishedWhenItIs() async throws {
        let (rig, key, state, held) = try await streaming(paces: true)
        let app = rig.runtime.controller
        let conversation = try #require(app.children(in: state)[key])
        var live = held
        let start = rig.clock.now.addingTimeInterval(10)
        live = try conversation.apply(.init(.streamed(.delta(Self.reply + "[the notes](https://example.com/notes)", turn: 1, at: 0), at: start)), to: live)
        var tree = await present(rig, key, live, state, at: start)
        let first = text(tree, key)
        #expect(first.count < Self.reply.count)
        // The run ends while most of the text still waits.
        var done = AgentEvent(kind: .done)
        done.result = AgentResult()
        live = try conversation.apply(.init(.streamed(done, at: start)), to: live)
        live = try conversation.apply(.init(.streamEnded(nil, outcome: UUID())), to: live)
        #expect(live.phase == .handingOver)
        var now = start.addingTimeInterval(1 / TextReveal.steps)
        tree = await present(rig, key, live, state, at: now)
        // Not all at once: it goes on at its rate, still being written.
        #expect(text(tree, key).count > first.count && text(tree, key).count < Self.reply.count)
        while let next = app.nextChange(of: tree) {
            now = next
            tree = await present(rig, key, live, state, at: now)
        }
        guard case let .assistant(turn)? = tree.conversations[key]?.turns.last?.body else {
            Issue.record("The reply is not the last turn.")
            return
        }
        // Whole, finished, and citing its page now that it is.
        #expect(turn.text?.hasPrefix(Self.reply) == true && !turn.live && turn.sources != nil)
        rig.server.release()
    }

    @Test func whereTheDeviceAsksForLessMotionEachPieceIsDrawnAsItArrives() async throws {
        let (rig, key, state, held) = try await streaming(paces: false)
        let app = rig.runtime.controller
        let conversation = try #require(app.children(in: state)[key])
        let start = rig.clock.now.addingTimeInterval(10)
        let live = try conversation.apply(.init(.streamed(.delta(Self.reply, turn: 1, at: 0), at: start)), to: held)
        let tree = await present(rig, key, live, state, at: start)
        #expect(text(tree, key) == Self.reply && app.nextChange(of: tree) == nil)
        // Turned on part way, the reveal starts from what is drawn.
        var paced = state
        paced.paces = true
        let more = try conversation.apply(.init(.streamed(.delta("And more.", turn: 1, at: Self.reply.unicodeScalars.count), at: start)), to: live)
        let next = await present(rig, key, more, paced, at: start.addingTimeInterval(0.05))
        #expect(text(next, key).hasPrefix(Self.reply) && text(next, key).count < (Self.reply + "And more.").count)
        rig.server.release()
    }

    @Test func theNewestOfTheThinkingIsItsTailFromTheStartOfAWord() {
        let short = "Let me look at the list."
        #expect(TurnPresenter.thinkingTail(short) == short)
        let long = (1 ... 200).map { "word\($0)" }.joined(separator: " ")
        let tail = TurnPresenter.thinkingTail(long)
        #expect(long.hasSuffix(tail) && tail.count <= TurnPresenter.tailLength && tail.hasPrefix("word"))
        // Only while the model thinks.
        var turn = Turn(id: "turn-1", role: .assistant, content: "", reasoning: long, thinking: true)
        guard case let .assistant(thinking) = TurnPresenter.present(turn, agent: nil, live: true, decided: [], titles: [:]).body else { return }
        #expect(thinking.reasoning?.tail == tail && thinking.reasoning?.label == "Thinking…")
        turn.thinking = false
        turn.reasoningMillis = 12000
        guard case let .assistant(thought) = TurnPresenter.present(turn, agent: nil, live: true, decided: [], titles: [:]).body else { return }
        #expect(thought.reasoning?.tail == "" && thought.reasoning?.label == "Thought for 12 s")
    }
}

@Suite("A transcript follows its end", .timeLimit(.minutes(10)))
struct TranscriptFollowTests {
    @Test func aChatShownAtItsEndFollowsItWhateverArrives() {
        let follow = TranscriptFollow()
        #expect(follow.following)
        // A reply grew, rows landed, the keyboard rose, the window was
        // resized, the text size changed: to the end, at once.
        #expect(follow.resized() == .end(animated: false))
        // And the app came back to the front with a screen more below.
        #expect(follow.returned() == .end(animated: false))
    }

    @Test func aTouchLetsGoAndComingToRestNearTheEndTakesItUpAgain() {
        var follow = TranscriptFollow()
        follow.touched()
        #expect(!follow.following)
        // Let go of: what arrives moves nothing, at the front or back at it.
        #expect(follow.resized() == .stay && follow.returned() == .stay)
        // At rest far up: still let go.
        #expect(follow.rested(distance: 400) == .stay && !follow.following)
        // At rest within reach of the end: followed again, from the end.
        follow.touched()
        #expect(follow.rested(distance: TranscriptFollow.near) == .end(animated: true) && follow.following)
        // At the end already: nothing to move.
        follow.touched()
        #expect(follow.rested(distance: 0) == .stay && follow.following)
        follow.touched()
        #expect(follow.rested(distance: TranscriptFollow.near + 1) == .stay && !follow.following)
    }

    @Test func aSendAndJumpToLatestGoToTheEndFromAnywhere() {
        var follow = TranscriptFollow()
        follow.touched()
        _ = follow.rested(distance: 2000)
        #expect(follow.sent() == .end(animated: true) && follow.following)
        follow.touched()
        _ = follow.rested(distance: 2000)
        #expect(follow.jumped() == .end(animated: true) && follow.following)
    }

    @Test func aChatShownAtATurnStaysThereUntilTheReaderComesDown() {
        var follow = TranscriptFollow(opening: .turn("m7"))
        #expect(!follow.following && follow.resized() == .stay && follow.returned() == .stay)
        #expect(follow.rested(distance: 10) == .end(animated: true) && follow.following)
    }

    @Test func jumpToLatestSaysHowManyMessagesAreBelow() async throws {
        #expect(JumpToLatest(below: 0) == JumpToLatest(below: 0) && JumpToLatest(below: 0).count == nil)
        #expect(JumpToLatest(below: 0).label == "Jump to Latest")
        #expect(JumpToLatest(below: 1).count == "1" && JumpToLatest(below: 1).label == "Jump to Latest, 1 message below")
        #expect(JumpToLatest(below: 3).label == "Jump to Latest, 3 messages below")
        #expect(JumpToLatest(below: 250).count == "99+")
        // Messages are what somebody said: a line about the run is none.
        let rows = [
            messageRow("m0", "user", "One", turn: 0), messageRow("m1", "assistant", "Two", turn: 1),
            messageRow("m2", "summary", "Earlier", turn: 2), messageRow("m3", "user", "Three", turn: 3),
            messageRow("m4", "assistant", "Four", turn: 4),
        ]
        let rig = ChatRig(baseRoutes())
        rig.server.route("GET", "/api/v1/records", query: "llm/message", .json(200, page(rows)))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.conversation(key)?.turns.count == 5 }
        let screen = try #require(await rig.conversation(key))
        #expect(screen.messages(below: "m0") == 3 && screen.messages(below: "m1") == 2 && screen.messages(below: "m4") == 0)
        #expect(screen.messages(below: nil) == 0 && screen.messages(below: "gone") == 0)
        #expect(screen.opens == .end && screen.sends == 0)
    }
}
