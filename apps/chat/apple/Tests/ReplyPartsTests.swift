#if os(macOS)
import AppKit
import Foundation
import SubstrateChatCore
import SubstrateKit
import SwiftUI
import Testing

@testable import SubstrateChat
@testable import SubstrateChatUI

/// What stands in and under a reply, as the app's own views draw it: the
/// row of actions under the last reply, code in its colours with the
/// switch to wrap, a table in each of its layouts, and the sheet a reply's
/// text is selected in. Read from the accessibility tree the system is
/// handed, which is what VoiceOver reads.
@MainActor
@Suite(.serialized) struct ReplyPartsTests {
    /// A view laid out in a window nobody sees, at a phone's width, once
    /// what it measures of itself has settled.
    @MainActor final class Hosted {
        let hosting: NSHostingView<AnyView>
        let window: NSWindow
        private let measured = HandoverTests.Measured()
        /// How tall the view is at its width.
        var height: CGFloat { measured.height }

        init(_ view: some View, width: CGFloat = OffscreenDrawing.phone.width) async throws {
            _ = NSApplication.shared
            let measured = measured
            hosting = NSHostingView(
                rootView: AnyView(
                    view.frame(width: width, alignment: .topLeading).fixedSize(horizontal: false, vertical: true)
                        .onGeometryChange(for: CGFloat.self) {
                            $0.size.height
                        } action: {
                            measured.height = $0
                        }))
            hosting.sizingOptions = []
            hosting.frame = CGRect(x: 0, y: 0, width: width, height: 3000)
            window = NSWindow(contentRect: hosting.frame, styleMask: [.borderless], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            window.contentView = hosting
            for _ in 0 ..< 8 {
                hosting.layoutSubtreeIfNeeded()
                try await Task.sleep(for: .milliseconds(25))
            }
        }

        var elements: [AnyObject] { LeavingTheApp.elements(under: hosting) }

        /// What a screen reader is told of each element: its label, or the
        /// text it holds. A link inside a text is an element of its own
        /// beside the text's.
        var words: [String] {
            elements.compactMap { element in
                if let label = element.accessibilityLabel?(), !label.isEmpty { return label }
                return Self.text((element as? NSAccessibilityProtocol)?.accessibilityValue())
            }
        }

        /// A value as its words: a text's may be a styled one.
        static func text(_ value: Any?) -> String? {
            if let text = value as? String { return text }
            if let text = value as? NSAttributedString { return text.string }
            return nil
        }

        /// The links, by what each is read as.
        var links: [String] {
            elements.filter { $0.accessibilityRole?() == .link }.map { ReplyPartsTests.plain(($0.accessibilityLabel?() ?? nil) ?? "") }
        }

        /// The buttons, by their labels.
        var buttons: [String] {
            elements.filter { $0.accessibilityRole?() == .button }.compactMap { $0.accessibilityLabel?() ?? nil }
        }

        isolated deinit {
            window.contentView = nil
        }
    }

    /// The same words with every run of spaces, breaking or not, as one,
    /// and without what makes a labelled piece as long as its label
    /// (`SpokenPiece`).
    static func plain(_ text: String) -> String {
        text.replacingOccurrences(of: "\u{00A0}", with: " ").replacingOccurrences(of: "\u{202F}", with: " ")
            .replacingOccurrences(of: "\u{2060}", with: "")
            .split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }

    // MARK: Links

    /// A link on a Mac is an element of its own, read as its words out of
    /// the line. With a pill or another link's mark before it on the line
    /// it was read from the wrong place: as other words, or, past the
    /// line's end, as nothing, AppKit raising where it was asked, which a
    /// test meets as a call that never returns.
    @Test(.timeLimit(.minutes(1))) func everyLinkOfALineIsReadAsItsOwnWordsWhateverStandsBeforeIt() async throws {
        let lines: [(markdown: String, links: [String])] = [
            ("See [a](https://example.com/a) and [b](https://example.com/b).", ["a", "b"]),
            ("[Q4 budget](substrate://ada.example.com/tasks/task/q4)", ["Q4 budget"]),
            ("Ask [Grace](substrate://ada.example.com/people/person/grace) about [the venue](https://example.com/v)", ["Grace", "the venue"]),
            (
                "First [the venue](https://example.com/v), then [Grace](substrate://ada.example.com/people/person/grace) and [more](https://example.com/m)",
                ["the venue", "Grace", "more"]
            ),
            (ChatFixtures.links, ["Q4 budget", "the planning notes", "the board deck", "Grace Hopper", "finance"]),
            // A kind whose name is shorter than its tile's place, and two
            // pills of one record.
            (
                "[Io](substrate://ada.example.com/moons/io/one), [Io](substrate://ada.example.com/moons/io/one) and [Europa](https://example.com/e)",
                ["Io", "Io", "Europa"]
            ),
        ]
        for line in lines {
            for selectable in [false, true] {
                let view = MarkdownView(text: line.markdown).replyTools(wraps: false)
                let hosted = try await Hosted(selectable ? AnyView(view.textSelection(.enabled)) : AnyView(view))
                #expect(hosted.links == line.links, "\(line.markdown), selectable: \(selectable)")
            }
        }
        // In a table's cell and in a list too.
        let table = try await Hosted(MarkdownView(text: ChatFixtures.tables).replyTools(wraps: false))
        #expect(table.links.contains("Q4 budget") && !table.links.contains(""))
    }

    // MARK: Code

    @Test(arguments: CodeFixtures.all.map(\.language))
    func codeIsMadeIntoColouredTextALineAtATimeAndIsWhatMakingItWholeGives(_ language: String) throws {
        let code = try #require(CodeFixtures.all.first { $0.language == language }?.code)
        let kept = CodePaint()
        var end = code.startIndex
        while end < code.endIndex {
            end = code.index(end, offsetBy: 7, limitedBy: code.endIndex) ?? code.endIndex
            let prefix = String(code[..<end])
            let runs = CodeHighlighter.highlight(prefix, language: language).runs
            let made = kept.text(prefix, runs: runs)
            // Every character of the code, and no other.
            #expect(String(made.characters) == prefix, "\(language), after \(prefix.count) characters")
            // As a text made in one go is: the lines kept are the lines.
            #expect(made == CodePaint().text(prefix, runs: runs), "\(language), after \(prefix.count) characters")
            if String(made.characters) != prefix { return }
        }
    }

    @Test func eachRunIsDrawnInItsTokensColourAndAKeywordAndACommentNotByColourAlone() throws {
        let code = "let a = \"b\" // c"
        let made = CodePaint().text(code, runs: CodeHighlighter.highlight(code, language: "swift").runs)
        var drawn: [String: (colour: Color?, font: Font?)] = [:]
        for run in made.runs { drawn[String(made[run.range].characters)] = (run.foregroundColor, run.font) }
        let keyword = try #require(drawn["let"])
        #expect(keyword.colour == CodePaint.colours[.keyword])
        #expect(keyword.font == ChatType.codeKeyword)
        let string = try #require(drawn["\"b\""])
        #expect(string.colour == CodePaint.colours[.string])
        #expect(string.font == nil)
        let comment = try #require(drawn["// c"])
        #expect(comment.colour == CodePaint.colours[.comment])
        #expect(comment.font == ChatType.codeComment)
        // What is no token is the text's own colour.
        let between = try #require(drawn[" a = "])
        #expect(between.colour == nil)
        // A text that is not the one kept is made from its start.
        let kept = CodePaint()
        _ = kept.text("let a = 1\nlet b", runs: CodeHighlighter.highlight("let a = 1\nlet b", language: "swift").runs)
        let other = "var c = 2\nvar d"
        #expect(String(kept.text(other, runs: CodeHighlighter.highlight(other, language: "swift").runs).characters) == other)
    }

    @Test func theSwitchToWrapIsDrawnOnlyOnABlockWithALineLongerThanItsRoom() async throws {
        let scrolled = try await Hosted(MarkdownView(text: ChatFixtures.longLines).replyTools(wraps: false))
        // Two blocks with a long line, one with none: two switches, three
        // Copy buttons, each saying what it copies.
        #expect(scrolled.buttons.filter { $0 == "Wrap Lines" }.count == 2)
        #expect(scrolled.buttons.filter { $0 == "Copy Code" }.count == 3)
        // Wrapped, the switch stays, and says so.
        let wrapped = try await Hosted(MarkdownView(text: ChatFixtures.longLines).replyTools(wraps: true))
        #expect(wrapped.buttons.filter { $0 == "Wrap Lines" }.count == 2)
        // And the block is taller for it, with nothing to scroll sideways.
        #expect(wrapped.height > scrolled.height + 20, "\(scrolled.height) scrolled, \(wrapped.height) wrapped")
        // In a room wide enough for its longest line, no block offers it.
        let wide = try await Hosted(MarkdownView(text: ChatFixtures.longLines).replyTools(wraps: false), width: 1400)
        #expect(!wide.buttons.contains("Wrap Lines"))
    }

    // MARK: Tables

    /// Every cell of the fixture's tables, as its words.
    static var cells: [String] {
        Markdown.parse(ChatFixtures.tables).flatMap { block -> [String] in
            guard case let .table(header, _, rows) = block else { return [] }
            return ([header] + rows).flatMap { $0 }.map { plain(String(parseInline($0).characters)) }
        }
    }

    @Test func aTableDrawsEveryCellInEachOfItsLayouts() async throws {
        #expect(Self.cells.count == 36)
        // As a grid: one that fits, one wrapped to fit, one that scrolls.
        // Its words can be selected, as a reply's are, which is also how
        // a text says what it holds to a test that does not see it.
        let grid = try await Hosted(MarkdownView(text: ChatFixtures.tables).textSelection(.enabled).replyTools(wraps: false))
        let drawn = grid.words.map(Self.plain)
        for cell in Self.cells { #expect(drawn.contains { $0.contains(cell) }, "the grid lost “\(cell)”") }
        // As cards, at an accessibility size: every header is beside each
        // row's value, so a header is drawn once a row.
        let cards = try await Hosted(
            MarkdownView(text: ChatFixtures.tables).textSelection(.enabled).replyTools(wraps: false)
                .environment(\.dynamicTypeSize, .accessibility3))
        let said = cards.words.map(Self.plain).joined(separator: " ")
        for cell in Self.cells { #expect(said.contains(cell), "the cards lost “\(cell)”") }
        #expect(said.components(separatedBy: "Estimate").count - 1 == 3)
        // A table with no row yet is its headers, in a card as in a grid.
        for size in [DynamicTypeSize.large, .accessibility3] {
            let bare = try await Hosted(
                MarkdownView(text: "| Name | Status |\n| --- | --- |").textSelection(.enabled).replyTools(wraps: false)
                    .environment(\.dynamicTypeSize, size))
            let headers = bare.words.map(Self.plain).joined(separator: " ")
            #expect(headers.contains("Name") && headers.contains("Status"), "\(size)")
        }
        // A record's link in a cell is its pill there too: the line is
        // drawn through the pill's renderer.
        #expect(inline("[Q4 budget](substrate://ada.example.com/tasks/task/q4)").pills)
    }

    @Test func aTableKeepsItsRoomWhenItFitsAndIsWiderThanItWhenItCannot() async throws {
        let room = OffscreenDrawing.phone.width
        func width(_ markdown: String) async throws -> CGFloat {
            let measured = HandoverTests.Measured()
            let hosted = try await Hosted(
                MarkdownView(text: markdown).onScrollGeometryChange(for: CGFloat.self) {
                    $0.contentSize.width
                } action: { _, width in
                    measured.height = width
                })
            _ = hosted
            return measured.height
        }
        // Natural width: narrower than the room.
        let narrow = try await width("| Owner | Due |\n| --- | ---: |\n| Ada | 3 |")
        #expect(narrow > 0 && narrow < room / 2)
        // A column of prose: wrapped to the room, to the point.
        let prose = try await width("| Plant | Care |\n| --- | --- |\n| Basil | \(String(repeating: "water it well ", count: 12)) |")
        #expect(abs(prose - room) < 1.5)
        // More columns than fit: wider than the room, so it scrolls.
        let many = try await width(
            "| A | B | C | D | E | F |\n| --- | --- | --- | --- | --- | --- |\n" + "| "
                + Array(repeating: "a cell of some words", count: 6).joined(separator: " | ") + " |")
        #expect(many > room + 100)
    }

    // MARK: Under a reply

    @Test func theRowUnderAReplyIsThereFromItsFirstWordsAndSettlingMovesNothing() async throws {
        let run = HandoverTests()
        let (server, _, stage) = try await run.staged(paced: false)
        stage.host.send(.draft("What needs doing on the balcony?"), to: stage.key)
        stage.host.send(.send, to: stage.key)
        try await run.watch(stage) { $0.streaming }
        // Thinking: nothing said, nothing to copy.
        try await run.step(server, stage, 4) { screen in
            if case let .assistant(reply)? = screen.turns.last?.body { reply.reasoning != nil } else { false }
        }
        #expect(stage.host.conversation(stage.key)?.replyActions == nil)
        #expect(!Self.buttons(stage).contains("Copy"))
        // Its first words: Copy, of what is there, and no Share.
        try await run.step(server, stage, 2) { HandoverTests.text($0).contains("check the list") }
        #expect(Self.buttons(stage).filter { $0 == "Copy" }.count == 1 && !Self.buttons(stage).contains("Share"))
        // The call that follows says nothing yet: the row stays where the
        // last words are.
        try await run.step(server, stage, 2) {
            $0.turns.contains { turn in
                if case let .assistant(reply) = turn.body { !reply.tools.isEmpty } else { false }
            }
        }
        #expect(Self.buttons(stage).filter { $0 == "Copy" }.count == 1)
        // To the end, and its rows.
        server.step(Self.thread, 100)
        try await run.watch(stage) { !$0.streaming && $0.turns.last?.id.hasPrefix("turn-") == true && $0.replyActions?.whole == true }
        #expect(Self.buttons(stage).filter { $0 == "Copy" }.count == 1 && Self.buttons(stage).filter { $0 == "Share" }.count == 1)
        // Through all of it the content never got shorter: the row took
        // its place with the words and gave none back.
        run.hold(stage.frames)
        // And the reply that settles is as tall as it was a frame before,
        // with all its words drawn: Share arrives in the row's own height.
        let whole = stage.frames.filter { HandoverTests.text($0.screen) == HandoverTests.text(stage.frames.last?.screen) }
        let writing = whole.last { $0.screen.replyActions?.whole == false }
        let settled = whole.first { $0.screen.replyActions?.whole == true && !$0.screen.streaming }
        if let writing, let settled { #expect(abs(settled.height - writing.height) < 0.5, "\(writing.height) then \(settled.height)") }
    }

    static let thread = HandoverTests.thread

    static func buttons(_ stage: HandoverTests.Stage) -> [String] {
        stage.elements.filter { $0.accessibilityRole?() == .button }.compactMap { $0.accessibilityLabel?() ?? nil }
    }

    @Test func tryAgainIsUnderTheReplyOnlyWhereItsCardLeadsElsewhere() async throws {
        let server = FixtureServer(reply: ChatFixtures.short, pace: .init(piece: 1, interval: .zero))
        server.seed("t-reply", message: "Show me.", reply: ChatFixtures.short)
        let host = ChatHost(.fixture(server))
        host.send(.sceneAppeared(scene: "s", restoring: nil))
        for _ in 0 ..< 400 where host.chats?.list.loading != false { try await Task.sleep(for: .milliseconds(25)) }
        host.send(.show(thread: "t-reply", scene: "s"))
        for _ in 0 ..< 400 where host.chats?.scenes["s"]?.current.flatMap({ host.conversation($0) })?.turns.count != 2 {
            try await Task.sleep(for: .milliseconds(25))
        }
        let key = try #require(host.chats?.scenes["s"]?.current)
        var screen = try #require(host.conversation(key))
        let last = try #require(screen.turns.last?.id)
        func drawn(_ failure: FailurePresentation?) async throws -> [String] {
            screen.failures.turns = failure.map { [last: $0] } ?? [:]
            let hosted = try await Hosted(
                VStack(alignment: .leading, spacing: ChatSpace.turns) { TranscriptRows(screen: screen, key: key, onKey: { _ in }, act: { _ in }) }
                    .replyLinks().replyTools(wraps: false).environment(host))
            return hosted.buttons
        }
        // No failure: Copy and Share.
        var buttons = try await drawn(nil)
        #expect(buttons.filter { ["Copy", "Share", "Try Again", "Continue"].contains($0) } == ["Copy", "Share"])
        // The card's own button runs it again: once, on the card.
        let overloaded = try #require(FailureFixtures.table(now: Date()).first { $0.sentence.contains("having trouble") })
        buttons = try await drawn(overloaded)
        #expect(buttons.filter { $0 == "Try Again" }.count == 1 && overloaded.resume == nil)
        // The card leads to the key: once, under the reply.
        buttons = try await drawn(FailureFixtures.keyRefused)
        #expect(buttons.filter { $0 == "Try Again" }.count == 1 && buttons.contains("Replace Key") && FailureFixtures.keyRefused.resume != nil)
        // Whatever the cause, never twice.
        for card in FailureFixtures.table(now: Date()) {
            buttons = try await drawn(card)
            #expect(buttons.filter { $0.hasPrefix("Try Again") || $0 == "Continue" }.count <= 1, "\(card.sentence)")
        }
        // A run that ended before the assistant said anything: the card is
        // at the person's message, and Try Again stands under that.
        screen.turns.removeLast()
        let asked = try #require(screen.turns.last?.id)
        screen.failures.turns = [asked: FailureFixtures.keyRefused]
        let alone = try await Hosted(
            VStack(alignment: .leading, spacing: ChatSpace.turns) { TranscriptRows(screen: screen, key: key, onKey: { _ in }, act: { _ in }) }
                .replyLinks().replyTools(wraps: false).environment(host))
        #expect(alone.buttons.filter { ["Copy", "Share", "Try Again", "Continue"].contains($0) } == ["Try Again"])
        #expect(alone.buttons.contains("Replace Key"))
    }

    // MARK: Select Text

    @Test func theTextOpenedForSelectingHoldsEveryWordTheReplyDraws() {
        for reply in [ChatFixtures.reply, ChatFixtures.tables, ChatFixtures.longLines, GalleryTests.linking] {
            let rows = Markdown.rows(reply, open: false)
            let text = Self.plain(ReplyText.plain(rows))
            for word in Self.plain(StreamedMarkdownTests.words(rows.map(\.block))).split(separator: " ") {
                #expect(text.contains(word), "“\(word)” is not in the text to select")
            }
        }
        // As the reply reads, not as it was written: no marker of Markdown.
        let text = ReplyText.plain(Markdown.rows(ChatFixtures.reply, open: false))
        #expect(!text.contains("**") && !text.contains("```") && !text.contains("| ---") && !text.contains("](http"))
        #expect(text.contains("• Offsite agenda: two sessions have no owner") && text.contains("1. Ask for the actuals today"))
        #expect(text.contains("☑ Draft the budget summary") && text.contains("Item\tOwner\tDue\tState"))
        // Code keeps its lines and its indentation.
        #expect(text.contains("for task in due.records where task[\"done\"]?.bool != true {\n    print(task.id, task[\"title\"]?.string ?? \"\")\n}"))
        // A link is a link there too, and emphasis is the font's.
        let attributed = ReplyText.attributed(Markdown.rows("See [the notes](https://example.com/n) in **bold**.", open: false))
        var links: [URL] = []
        var bold = false
        attributed.enumerateAttributes(in: NSRange(location: 0, length: attributed.length)) { attributes, _, _ in
            if let link = attributes[.link] as? URL { links.append(link) }
            if let font = attributes[.font] as? NSFont, font.fontDescriptor.symbolicTraits.contains(.bold) { bold = true }
        }
        #expect(links == [URL(string: "https://example.com/n")!] && bold)
    }
}
#endif
