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
        #expect(live.live && live.blocks.rows.map(\.block) == Markdown.parse(Markdown.closed(reply)))
        rig.server.release()
    }
}
