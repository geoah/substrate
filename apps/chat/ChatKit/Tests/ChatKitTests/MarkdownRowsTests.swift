import Foundation
import SubstrateCore
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

/// A reply's blocks as rows: parsed where the presentation is made, each
/// with an id of its position and its content, so a finished block holds
/// still while the reply is written, and what a stream drew at its end is
/// what the stored reply draws.
@Suite("A reply's blocks are rows", .timeLimit(.minutes(10)))
struct MarkdownRowsTests {
    static let replies = MarkdownClosingTests.replies + [ChatFixtures.reply(bytes: 6000)]

    /// The reply as a stream delivers it: every prefix that ends where a
    /// piece of `size` characters does.
    static func prefixes(_ reply: String, size: Int) -> [String] {
        var out: [String] = []
        var end = reply.startIndex
        while end < reply.endIndex {
            end = reply.index(end, offsetBy: size, limitedBy: reply.endIndex) ?? reply.endIndex
            out.append(String(reply[..<end]))
        }
        return out
    }

    @Test func aRowsIdIsItsPositionAndAHashOfWhatItHolds() {
        let rows = Markdown.rows("# Title\n\nSome text.\n\nSome text.", open: false)
        #expect(rows.map(\.block) == [.heading(level: 1, text: "Title"), .paragraph("Some text."), .paragraph("Some text.")])
        // The same on every device and at every launch.
        #expect(rows.map(\.id) == ["0-95b59b2ec65077cf", "1-d3ff1a0a2d68e718", "2-d3ff1a0a2d68e718"])
        #expect(Markdown.rows("# Title\n\nSome text.\n\nSome text.", open: false) == rows)
        // Another text, another level, another kind: another row.
        #expect(MarkdownRow(position: 0, block: .paragraph("a")).id != MarkdownRow(position: 0, block: .paragraph("b")).id)
        #expect(MarkdownRow(position: 0, block: .heading(level: 1, text: "a")).id != MarkdownRow(position: 0, block: .heading(level: 2, text: "a")).id)
        #expect(MarkdownRow(position: 0, block: .paragraph("a")).id != MarkdownRow(position: 0, block: .heading(level: 1, text: "a")).id)
        #expect(
            MarkdownRow(position: 0, block: .code(language: nil, text: "a", closed: false)).id
                != MarkdownRow(position: 0, block: .code(language: nil, text: "a", closed: true)).id)
        #expect(
            MarkdownRow(position: 0, block: .table(header: ["ab", "c"], alignments: [], rows: [])).id
                != MarkdownRow(position: 0, block: .table(header: ["a", "bc"], alignments: [], rows: [])).id)
    }

    @Test(arguments: replies.indices, [1, 12, 40])
    func whileAReplyIsWrittenOnlyItsLastRowChanges(_ index: Int, _ size: Int) {
        var before: [MarkdownRow] = []
        for prefix in Self.prefixes(Self.replies[index], size: size) {
            let now = Markdown.rows(prefix, open: true)
            // Every row but the last of the flush before is there still,
            // with its id: a finished block is an unchanged row.
            let settled = before.dropLast().map(\.id)
            #expect(Array(now.prefix(settled.count).map(\.id)) == settled, "after \(prefix.count) characters")
            if Array(now.prefix(settled.count).map(\.id)) != settled { return }
            before = now
        }
    }

    @Test(arguments: replies.indices, [1, 12, 40])
    func theStreamedPathEndsOnTheBlocksOfAOneShotParse(_ index: Int, _ size: Int) throws {
        let reply = Self.replies[index]
        let parts = PresenterParts(verifies: true)
        var turn = Turn(id: "live-a1", role: .assistant, content: "")
        var drawn: [MarkdownRow] = []
        var from = reply.startIndex
        for prefix in Self.prefixes(reply, size: size) {
            // A piece arrives, and the turn being written is presented.
            turn.content += reply[from ..< prefix.endIndex]
            from = prefix.endIndex
            guard case let .assistant(live) = TurnPresenter.present(turn, agent: nil, live: true, decided: [], titles: [:], kept: parts, scope: "chat/k").body
            else { return }
            drawn = live.blocks.rows
        }
        #expect(turn.content == reply)
        // The stream ends: the turn is no longer being written.
        guard case let .assistant(ended) = TurnPresenter.present(turn, agent: nil, live: false, decided: [], titles: [:], kept: parts, scope: "chat/k").body
        else { return }
        // Its rows land: the stored turn.
        let stored = Turn(id: "m2", role: .assistant, content: reply)
        guard case let .assistant(kept) = TurnPresenter.present(stored, agent: nil, live: false, decided: [], titles: [:]).body else { return }
        let whole = Markdown.parse(reply)
        #expect(ended.blocks.rows.map(\.block) == whole)
        #expect(kept.blocks == ended.blocks && kept.pages == ended.pages)
        #expect(kept.text == reply && ended.text == reply)
        // And what was on screen at the last piece was that already.
        #expect(drawn.map(\.block) == whole)
    }

    @Test func aLongReplyIsSeveralRowsOfTheTranscriptAndItsPagesNeverMove() {
        let reply = ChatFixtures.reply(bytes: 16 * 1024)
        let rows = Markdown.rows(reply, open: false)
        let pages = Markdown.pages(rows)
        #expect(pages.first == 0 && pages == pages.sorted() && Set(pages).count == pages.count)
        #expect(pages.count >= 8)
        // Each about a screen and a half: a page's weight but for its last
        // block, which may run over.
        for (page, start) in pages.enumerated() {
            let end = page + 1 < pages.count ? pages[page + 1] : rows.count
            let weight = rows[start ..< end].dropLast().reduce(0) { $0 + $1.block.weight }
            #expect(weight < Markdown.pageWeight)
        }
        #expect(Markdown.pages(Markdown.rows("Short.", open: false)) == [0])
        #expect(Markdown.pages([]) == [0])
        // As it is written a page that has started stays where it started.
        var before = [0]
        for prefix in Self.prefixes(reply, size: 24) {
            let now = Markdown.pages(Markdown.rows(prefix, open: true))
            #expect(Array(now.prefix(before.count)) == before, "after \(prefix.count) characters")
            before = now
        }
        #expect(before == pages)
    }

    @Test func aReplyIsParsedOnceForItsTextWhateverElseMovesInItsTurn() {
        let parts = PresenterParts()
        var turn = Turn(id: "live-a1", role: .assistant, content: "Look **here")
        func present() -> AssistantTurn? {
            guard case let .assistant(reply) = TurnPresenter.present(turn, agent: nil, live: true, decided: [], titles: [:], kept: parts, scope: "chat/k").body
            else { return nil }
            return reply
        }
        // Still being written: closed for display.
        #expect(present()?.blocks.rows.map(\.block) == [.paragraph("Look **here**")])
        #expect(parts.made("chat/k/blocks/") == 1)
        // A tool starts and the model thinks: the text is as it was.
        turn.tools.append(ToolCall(id: "c1", name: "query", callable: nil, arguments: "{}"))
        turn.reasoning = "Thinking about it"
        #expect(present()?.blocks.rows.map(\.block) == [.paragraph("Look **here**")])
        #expect(parts.made("chat/k/blocks/") == 1)
        turn.content += "** now"
        #expect(present()?.blocks.rows.map(\.block) == [.paragraph("Look **here** now")])
        #expect(parts.made("chat/k/blocks/") == 2)
    }

    @Test func aTurnWithNoTextHasNoRows() {
        let turn = Turn(id: "m1", role: .assistant, content: "", tools: [ToolCall(id: "c1", name: "query", callable: nil, arguments: "{}")])
        guard case let .assistant(reply) = TurnPresenter.present(turn, agent: nil, live: false, decided: [], titles: [:]).body else { return }
        #expect(reply.text == nil && reply.blocks.rows.isEmpty && reply.pages == [0])
    }

    @Test func aReplysRowsCrossToAndroidAsADeltaOfTheRowThatChanged() throws {
        // What the bridge sends for a keyed list: here, the reply's rows
        // between two flushes.
        let before = KeyedList(Markdown.rows("# Title\n\nFirst paragraph.\n\nSecond para", open: true))
        var after = before
        after.update(Markdown.rows("# Title\n\nFirst paragraph.\n\nSecond paragraph, longer", open: true))
        let delta = after.delta(from: before)
        // The block being written is another row in its place; the two
        // above it are neither sent nor touched.
        #expect(delta.inserted.map(\.block) == [.paragraph("Second paragraph, longer")])
        #expect(delta.removed == [before.rows[2].id] && delta.changed.isEmpty)
        #expect(delta.order.prefix(2) == before.ids.prefix(2))
        var applied = before
        try applied.apply(delta)
        #expect(applied == after)
    }

    // MARK: On the runtime

    private final class Seen: @unchecked Sendable {
        private let lock = NSLock()
        private var kept: [String] = []
        var texts: [String] { lock.withLock { kept } }
        func add(_ text: String) { lock.withLock { if kept.last != text { kept.append(text) } } }
    }

    @Test func aStreamedReplyIsParsedOnceAWindowAndNeverDrawnOlderThanItWas() async throws {
        #expect(Conversation.textWindow >= .milliseconds(34), "30 parses a second at most")
        let reply = String(ChatFixtures.reply.prefix(600))
        let stream = ChatFixtures.stream(thread: "t1", text: reply, turn: 1, piece: 5)
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(stream, holdAfter: stream.count - 1))])
        try await rig.signIn()
        let key = try await rig.newChat()
        let seen = Seen()
        let presentations = await rig.runtime.presentations()
        let watching = Task {
            for await tree in presentations {
                if case let .assistant(turn)? = tree.conversations[key]?.turns.last?.body, let text = turn.text { seen.add(text) }
            }
        }
        defer { watching.cancel() }
        let began = rig.clock.now
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        // 120 pieces, as fast as the stream hands them over.
        try await rig.harness.until(advancing: 1) { _ in await rig.turns(key).last == reply }
        let windows = Int((rig.clock.now.timeIntervalSince(began) / 0.05).rounded(.up)) + 1
        let parses = rig.runtime.controller.kept.made("chat/\(key.rawValue)/blocks/")
        #expect(parses >= 1 && parses <= windows, "\(parses) parses in \(windows) windows")
        #expect(parses < 120)
        // Each text drawn continues the one before: none is older.
        await rig.harness.settle()
        let texts = seen.texts
        #expect(texts.last == reply)
        for (earlier, later) in zip(texts, texts.dropFirst()) { #expect(later.hasPrefix(earlier)) }
        // On screen while it is written: closed for display.
        guard case let .assistant(live)? = await rig.conversation(key)?.turns.last?.body else { return }
        #expect(live.live && live.blocks.rows.map(\.block) == Markdown.parse(writing: reply))
        rig.server.release()
    }
}

/// A reply being written is parsed no more often than the limit, whatever
/// else has the tree presented in between.
@Suite("A reply's parses are paced", .timeLimit(.minutes(10)))
struct ParsePaceTests {
    @Test func aPartMadeTooRecentlyIsAnsweredAsItWasWithTheInstantItMayBeMadeAgain() {
        let parts = PresenterParts()
        let start = Date(timeIntervalSince1970: 1000)
        func part(_ reads: Int, at offset: TimeInterval) -> (value: Int, due: Date?) {
            parts.part("p", reading: reads, at: start.addingTimeInterval(offset), every: 0.1) { reads * 2 }
        }
        #expect(part(1, at: 0).value == 2 && part(1, at: 0).due == nil)
        // Other reads, too soon: as it was, and when to ask again.
        let early = part(2, at: 0.04)
        #expect(early.value == 2 && early.due == start.addingTimeInterval(0.1))
        #expect(parts.made("p") == 1)
        // The same reads again cost nothing and wait for nothing.
        #expect(part(1, at: 0.05).due == nil)
        let due = part(2, at: 0.1)
        #expect(due.value == 4 && due.due == nil && parts.made("p") == 2)
        // A clock that went back is no reason to wait.
        #expect(part(3, at: -5).value == 6)
    }

    /// The rig's streaming conversation, presented by hand at instants of
    /// the test's choosing.
    private func present(_ rig: ChatRig, _ key: ChildKey, _ conversation: ConversationState, at now: Date) async -> ChatPresentation {
        let app = rig.runtime.controller
        let state = await rig.harness.state
        let child = app.children(in: state)[key]
        let live = child.map { [key: $0.present(PresenterInput(state: conversation, now: now))] } ?? [:]
        return app.present(
            PresenterInput(state: state, children: live, now: now, locale: Locale(identifier: "en_US"), timeZone: TimeZone(identifier: "UTC")!))
    }

    private func reply(_ tree: ChatPresentation, _ key: ChildKey) -> AssistantTurn? {
        guard case let .assistant(turn)? = tree.conversations[key]?.turns.last?.body else { return nil }
        return turn
    }

    @Test func piecesPresentedFasterThanTheLimitAreParsedAtTheLimitAndNoneIsLeftUndrawn() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 2))])
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.overlay.turns.contains { $0.role == .assistant } == true }
        let app = rig.runtime.controller
        let state = await rig.harness.state
        let conversation = try #require(app.children(in: state)[key])
        var live = try #require(await rig.live(key))
        let window = 1 / Markdown.parsesPerSecond
        let start = rig.clock.now.addingTimeInterval(10)
        func piece(_ text: String, at now: Date) throws {
            var delta = AgentEvent(kind: .delta)
            delta.text = text
            delta.turn = 1
            live = try conversation.apply(.init(.streamed(delta, at: now)), to: live)
        }

        // A piece, drawn: parsed, and nothing waits.
        try piece(" one", at: start)
        var tree = await present(rig, key, live, at: start)
        let parses = app.kept.made("chat/\(key.rawValue)/blocks/")
        #expect(reply(tree, key)?.blocks.rows.map(\.block) == [.paragraph("Hel one")])
        #expect(tree.redrawAt == nil && app.nextChange(of: tree) == nil)

        // Pieces every 5 ms, each presented at once, as when something else
        // keeps the tree moving: the text is theirs, the rows as they were.
        for step in 1 ... 5 {
            let now = start.addingTimeInterval(Double(step) * 0.005)
            try piece(" \(step)", at: now)
            tree = await present(rig, key, live, at: now)
            #expect(reply(tree, key)?.text == "Hel one" + (1 ... step).map { " \($0)" }.joined())
            #expect(reply(tree, key)?.blocks.rows.map(\.block) == [.paragraph("Hel one")])
            // And the tree says when it must be presented again.
            #expect(app.nextChange(of: tree) == start.addingTimeInterval(window))
        }
        #expect(app.kept.made("chat/\(key.rawValue)/blocks/") == parses)
        // Presented again for something else meanwhile, it still says so.
        tree = await present(rig, key, live, at: start.addingTimeInterval(0.03))
        #expect(app.nextChange(of: tree) == start.addingTimeInterval(window))
        // No view is drawn for the wait itself: it is in no part.
        #expect((app.parts(of: tree).first?.value as? ChatPresentation)?.redrawAt == nil)

        // At that instant, with no piece more: everything written is drawn.
        tree = await present(rig, key, live, at: start.addingTimeInterval(window))
        #expect(reply(tree, key)?.blocks.rows.map(\.block) == [.paragraph("Hel one 1 2 3 4 5")])
        #expect(tree.redrawAt == nil && app.kept.made("chat/\(key.rawValue)/blocks/") == parses + 1)
        rig.server.release()
    }

    @Test func aReplyThatEndsIsParsedAtOnceHoweverRecentItsLastParse() {
        let parts = PresenterParts()
        let now = Date(timeIntervalSince1970: 1000)
        var turn = Turn(id: "live-a1", role: .assistant, content: "Almost **don")
        func present(live: Bool, at instant: Date) -> (AssistantTurn?, Date?) {
            let pace = TurnPresenter.Pace(now: instant)
            guard
                case let .assistant(reply) = TurnPresenter.present(
                    turn, agent: nil, live: live, decided: [], titles: [:], kept: parts, scope: "chat/k", pace: pace
                ).body
            else { return (nil, nil) }
            return (reply, pace.due)
        }
        #expect(present(live: true, at: now).0?.blocks.rows.map(\.block) == [.paragraph("Almost **don**")])
        turn.content += "e**."
        // A millisecond later the stream is over: its last text is drawn as
        // the stored reply will draw it, with no wait.
        let ended = present(live: false, at: now.addingTimeInterval(0.001))
        #expect(ended.0?.blocks.rows.map(\.block) == [.paragraph("Almost **done**.")])
        #expect(ended.1 == nil)
    }
}
