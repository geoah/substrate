import Foundation
import SubstrateCore
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

/// The presenter's parts, each kept until what it reads changes, and the
/// parts the tree goes out in.
@Suite("The presentation is made of parts", .timeLimit(.minutes(10)))
struct PresenterPartsTests {
    /// A list of several chats, one shown in a window, and a new chat in
    /// another whose reply is being written and held part way.
    private func streaming() async throws -> (rig: ChatRig, shown: ChildKey, live: ChildKey) {
        let threads = ChatFixtures.threads(count: 12).replacingOccurrences(of: ChatFixtures.agent, with: helper)
        let rig = ChatRig(
            baseRoutes(threads: threads) + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 2))])
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t-5", scene: "a"))
        let shown = try #require(await rig.harness.state.scenes["a"]?.current)
        try await rig.harness.until { _ in await rig.turns(shown) == ["Hi", "Hello"] }
        await rig.harness.send(.newChat(agent: helper, listen: false, scene: "b"))
        let live = try #require(await rig.harness.state.scenes["b"]?.current)
        try await rig.send(.draft("Hi"), to: live)
        try await rig.send(.send, to: live)
        try await rig.harness.until { _ in await rig.live(live)?.overlay.turns.contains { $0.role == .assistant } == true }
        // Nothing is on its way: the prefetch and the saves have ended.
        let mark = await rig.harness.effectsStarted
        try await rig.harness.untilIdle(since: mark, advancing: 2)
        return (rig, shown, live)
    }

    /// What the list's presenter is handed for `state`, with each
    /// conversation's own presentation made from the state given for it.
    private func input(_ rig: ChatRig, _ state: ChatAppState, _ conversations: [ChildKey: ConversationState]) -> PresenterInput<ChatApp> {
        let app = rig.runtime.controller
        var children: [ChildKey: ConversationLive] = [:]
        for (key, conversation) in app.children(in: state) {
            guard let held = conversations[key] else { continue }
            children[key] = conversation.present(PresenterInput(state: held, now: rig.clock.now))
        }
        return PresenterInput(
            state: state, children: children, now: rig.clock.now, locale: Locale(identifier: "en_US"), timeZone: TimeZone(identifier: "UTC")!)
    }

    @Test func aDeltaMakesItsOwnConversationsLiveTurnAgainAndNothingElse() async throws {
        let (rig, shown, live) = try await streaming()
        let app = rig.runtime.controller
        let state = await rig.harness.state
        var conversations = [shown: try #require(await rig.live(shown)), live: try #require(await rig.live(live))]
        let before = app.present(input(rig, state, conversations))
        let parts = ["list", "scene/", "frame", "settings", "chat/\(shown.rawValue)/", "chat/\(live.rawValue)/stored", "chat/\(live.rawValue)/titles"]
        let made = parts.map(app.kept.made)
        let screens = app.kept.made("chat/\(live.rawValue)/screen")
        let turns = app.kept.made("chat/\(live.rawValue)/turn/")

        // A piece of the reply arrives in the chat being written.
        var delta = AgentEvent(kind: .delta)
        delta.text = " there"
        delta.turn = 1
        let conversation = try #require(app.children(in: state)[live])
        conversations[live] = try conversation.apply(.init(.streamed(delta, at: rig.clock.now)), to: try #require(conversations[live]))
        let after = app.present(input(rig, state, conversations))

        // Its screen was made again, and of its turns the one being written.
        #expect(app.kept.made("chat/\(live.rawValue)/screen") == screens + 1)
        #expect(app.kept.made("chat/\(live.rawValue)/turn/") == turns + 1)
        // The list, the windows, Settings, the other conversation and this
        // one's stored rows were all kept.
        #expect(parts.map(app.kept.made) == made)
        #expect(after.list == before.list && after.conversations[shown] == before.conversations[shown])
        guard case let .assistant(turn)? = after.conversations[live]?.turns.last?.body else {
            Issue.record("The reply is not the last turn.")
            return
        }
        #expect(turn.text == "Hel there")

        // And of the parts the tree goes out in, its conversation's alone moved.
        let moved = zip(app.parts(of: before), app.parts(of: after)).filter { !$0.0.same(as: $0.1) }.map(\.1.path)
        #expect(moved == [[live]])
        rig.server.release()
    }

    @Test func theTreeGoesOutAsTheAppsOwnPartAndEachConversationUnderItsKey() async throws {
        let (rig, shown, live) = try await streaming()
        let tree = await rig.harness.presentation
        let parts = rig.runtime.controller.parts(of: tree)
        #expect(parts.map(\.path) == [[]] + [shown, live].sorted().map { [$0] })
        let own = try #require(parts.first?.value as? ChatPresentation)
        #expect(own.conversations.isEmpty && own.list == tree.list && own.scenes == tree.scenes)
        // The app's own part says what a view of it needs of a conversation:
        // the thread a window shows, and which conversations stream.
        #expect(own.scenes["a"]?.thread == "t-5" && own.scenes["b"]?.thread == "t1")
        #expect(own.streaming == [live])
        for part in parts.dropFirst() {
            #expect(part.value as? ConversationScreen == tree.conversations[try #require(part.path.first)])
        }
        rig.server.release()
    }

    @Test func aStateThatMovedMakesAgainOnlyThePartsThatReadWhatMoved() async throws {
        let (rig, shown, live) = try await streaming()
        let app = rig.runtime.controller
        var state = await rig.harness.state
        let conversations = [shown: try #require(await rig.live(shown)), live: try #require(await rig.live(live))]
        _ = app.present(input(rig, state, conversations))
        func made() -> [Int] { ["list", "settings", "frame", "chat/\(shown.rawValue)/screen", "chat/\(live.rawValue)/screen"].map(app.kept.made) }

        // A draft told after a pause is nobody's to draw.
        var counts = made()
        state.drafts["t-5"] = "half a thought"
        _ = app.present(input(rig, state, conversations))
        #expect(made() == counts)

        // A choice in Settings: Settings, and each conversation, which
        // reads the owner's choices; not the list.
        counts = made()
        state.preferences.expandThinking.toggle()
        _ = app.present(input(rig, state, conversations))
        #expect(zip(made(), counts).map { $0 - $1 } == [0, 1, 1, 1, 1])

        // A chat marked unread: the list alone.
        counts = made()
        state.seen["t-7"] = nil
        _ = app.present(input(rig, state, conversations))
        #expect(zip(made(), counts).map { $0 - $1 } == [1, 0, 0, 0, 0])
        rig.server.release()
    }

    @Test func theListIsKeptUntilTheInstantARowStopsBeingTrue() async throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos, verifiesParts: true)
        var state = ChatAppState()
        state.loaded = true
        let now = Timestamp.parse("2026-11-14T10:00:30Z")!
        func thread(_ id: String, _ updated: String) -> Record {
            try! ValueCoding.decode(
                Record.self,
                from: JSONValue.parse(
                    #"{"id":"\#(id)","kind":"\#(Kinds.thread)","version":1,"createdAt":"\#(updated)","updatedAt":"\#(updated)","properties":{"mode":"chat","status":"ok","subject":"\#(id)"}}"#
                )!)
        }
        state.chats = [thread("fresh", "2026-11-14T10:00:00Z"), thread("morning", "2026-11-14T08:15:00Z"), thread("old", "2026-11-01T08:15:00Z")]
        func stamps(at instant: Date) -> [String] {
            app.present(PresenterInput(state: state, now: instant, locale: Locale(identifier: "en_GB"))).list.sections.flatMap(\.rows).map(\.stamp)
        }
        #expect(stamps(at: now) == ["now", "08:15", "1 Nov"])
        let made = app.kept.made("list")
        // Half a minute on, every row still says what is true: kept.
        #expect(stamps(at: now.addingTimeInterval(29)) == ["now", "08:15", "1 Nov"])
        #expect(app.kept.made("list") == made)
        // A minute after the newest chat moved, it says the time.
        #expect(stamps(at: now.addingTimeInterval(30)) == ["10:00", "08:15", "1 Nov"])
        #expect(app.kept.made("list") == made + 1)
        // Kept through the day, and made again when the day ends.
        #expect(stamps(at: Timestamp.parse("2026-11-14T23:59:59Z")!) == ["10:00", "08:15", "1 Nov"])
        #expect(app.kept.made("list") == made + 1)
        #expect(stamps(at: Timestamp.parse("2026-11-15T00:00:00Z")!) == ["Yesterday", "Yesterday", "1 Nov"])
        #expect(app.kept.made("list") == made + 2)
        // A clock that went back is no reason to keep it.
        #expect(stamps(at: now) == ["now", "08:15", "1 Nov"])
        #expect(app.kept.made("list") == made + 3)
    }

    @Test func aPartsReadsAreWhatKeepsItAndWhatItIsMadeFromIsHeldToThem() {
        let parts = PresenterParts()
        var made = 0
        func part(_ reads: Int) -> Int {
            parts.part("double", reading: reads) {
                made += 1
                return reads * 2
            }
        }
        #expect(part(2) == 4 && part(2) == 4 && made == 1)
        #expect(part(3) == 6 && made == 2)
        #expect(parts.made("double") == 2)
        parts.keep(under: "dou") { _ in false }
        #expect(part(3) == 6 && made == 3)
    }

    @Test func aToolsRowIsMadeOnceWhileTheReplyAfterItIsWritten() {
        let parts = PresenterParts()
        let call = ToolCall(id: "c1", name: "query", callable: nil, arguments: #"{"kinds":["~/tasks/task"]}"#, output: #"{"records":[]}"#, ok: true)
        var turn = Turn(id: "live-a1", role: .assistant, content: "Look", tools: [call])
        func present() -> TurnPresentation {
            TurnPresenter.present(turn, agent: nil, live: true, decided: [], titles: [:], kept: parts, scope: "chat/k")
        }
        let first = present()
        turn.content += "ing at it"
        let second = present()
        #expect(parts.made("chat/k/tool/") == 1)
        guard case let .assistant(a) = first.body, case let .assistant(b) = second.body else {
            Issue.record("Not an assistant's turn.")
            return
        }
        #expect(a.tools == b.tools && b.text == "Looking at it")
        // The call settling differently is another row.
        turn.tools[0].ok = false
        _ = present()
        #expect(parts.made("chat/k/tool/") == 2)
    }
}
