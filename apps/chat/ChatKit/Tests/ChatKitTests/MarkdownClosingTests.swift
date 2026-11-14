import Foundation
import Testing

@testable import ChatKit

/// A reply as it is written, closed for display at every prefix: what is
/// drawn is always the start of what the whole reply draws, with no marker
/// in it that the reply does not show, and it never shrinks.
@Suite("Unfinished Markdown is closed for display", .timeLimit(.minutes(10)))
struct MarkdownClosingTests {
    // MARK: What is drawn, as text

    /// The text a line of inline Markdown draws, by rule: a code span as its
    /// content, a link as its label, a pair of markers gone. A marker with
    /// no partner stays, as a renderer leaves it: that is what these tests
    /// look for.
    static func drawn(_ line: String) -> String {
        var text = line
        var held: [String] = []
        // Code first: nothing inside it is a marker.
        text = replacing(text, #"(`+)(.+?)\1"#) { groups in
            held.append(groups[2].trimmingCharacters(in: .whitespaces))
            return "\u{1}\(held.count - 1)\u{2}"
        }
        text = text.replacingOccurrences(of: #"\\([\\`*_{}\[\]()#+\-.!|~<>])"#, with: "\u{3}$1", options: .regularExpression)
        for (pattern, template) in [
            (#"!?\[([^\]]*)\]\([^)\s]*\)"#, "$1"),
            (#"<((?:https?://|mailto:)[^>\s]*)>"#, "$1"),
            (#"\*\*(\S(?:.*?\S)??)\*\*"#, "$1"),
            (#"(?<![A-Za-z0-9])__(\S(?:.*?\S)??)__(?![A-Za-z0-9])"#, "$1"),
            (#"~~(\S(?:.*?\S)??)~~"#, "$1"),
            (#"(?<![A-Za-z0-9*])\*(?!\s)((?:[^*])+?)(?<!\s)\*"#, "$1"),
            (#"(?<![A-Za-z0-9_])_(?!\s)((?:[^_])+?)(?<!\s)_(?![A-Za-z0-9])"#, "$1"),
        ] {
            text = text.replacingOccurrences(of: pattern, with: template, options: [.regularExpression])
        }
        text = text.replacingOccurrences(of: "\u{3}", with: "")
        for (index, code) in held.enumerated() { text = text.replacingOccurrences(of: "\u{1}\(index)\u{2}", with: code) }
        return text
    }

    private static func replacing(_ text: String, _ pattern: String, with make: ([String]) -> String) -> String {
        guard let regex = try? NSRegularExpression(pattern: pattern) else { return text }
        var out = ""
        var at = text.startIndex
        let whole = NSRange(text.startIndex..., in: text)
        for match in regex.matches(in: text, range: whole) {
            guard let range = Range(match.range, in: text) else { continue }
            out += text[at ..< range.lowerBound]
            out += make((0 ..< match.numberOfRanges).map { Range(match.range(at: $0), in: text).map { String(text[$0]) } ?? "" })
            at = range.upperBound
        }
        return out + text[at...]
    }

    /// What some blocks draw, as words: each block's text in order, a list's
    /// numbers where it is ordered, a table's cells, code as it is.
    static func drawn(_ blocks: [MarkdownBlock]) -> [String] {
        blocks.flatMap { block -> [String] in
            switch block {
            case let .paragraph(text): [drawn(text)]
            case let .heading(_, text): [drawn(text)]
            case let .code(_, text, _): [text]
            case let .quote(blocks): drawn(blocks)
            case let .list(ordered, start, items):
                items.enumerated().flatMap { index, item in (ordered ? ["\(start + index)."] : []) + drawn(item.blocks) }
            case let .table(header, _, rows): ([header] + rows).flatMap { $0.map(drawn) }
            case .rule: []
            }
        }
    }

    static func words(_ blocks: [MarkdownBlock]) -> String {
        drawn(blocks).joined(separator: " ").split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }

    /// Every code block's text, in order, wherever it is nested: code is
    /// drawn as it is, so its spaces and its marks are held to the letter.
    static func code(_ blocks: [MarkdownBlock]) -> [String] {
        blocks.flatMap { block -> [String] in
            switch block {
            case let .code(_, text, _): [text]
            case let .quote(blocks): code(blocks)
            case let .list(_, _, items): items.flatMap { code($0.blocks) }
            default: []
            }
        }
    }

    /// The kinds of the blocks, in order, with the kinds inside a quote and
    /// inside each item of a list.
    static func kinds(_ blocks: [MarkdownBlock]) -> [String] {
        blocks.map { block in
            switch block {
            case .paragraph: "paragraph"
            case .heading: "heading"
            case .code: "code"
            case let .quote(blocks): "quote(" + kinds(blocks).joined(separator: " ") + ")"
            case let .list(ordered, _, items):
                (ordered ? "ol(" : "ul(") + items.map { kinds($0.blocks).joined(separator: " ") }.joined(separator: ", ") + ")"
            case .table: "table"
            case .rule: "rule"
            }
        }
    }

    // MARK: The replies

    static let replies: [String] = [
        ChatFixtures.reply,
        """
        **Short answer:** yes, *mostly*. The `sync` trait covers it, and ~~the old cron job~~ is gone.

        Three things to know:

        1. The **first** run reads everything, so it is `slow` by design.
        2. Later runs read *only what changed* since the cursor.
        3. A refused cursor starts over: see [the watch notes](https://example.com/docs/watch_notes) for why.

        > **Careful:** a run that is *still going* when the next is due is skipped,
        > not queued.

        | Step | Reads | Writes |
        | --- | :---: | ---: |
        | First run | **all** | `cursor` |
        | Later runs | changed | `cursor` |

        ```python
        def due(run, **kwargs):
            # 2 * 3 is not *emphasis* here, and `this` is not a span
            return run.next_at <= kwargs["now"]
        ```

        That is all of it.
        """,
        """
        # Plan for `apps/chat`

        The cost is 5*3 units a day, about ~15, and the path is ~/tasks/task with snake_case_names in it.

        - [x] Measure the draw path
        - [ ] Split the **presenter**
          - keep `TurnMemo`'s idea
          - _then_ publish by path
        - [ ] Parse off the *main* thread

        ***

        Ask at <https://example.com/help> or read the plan later.

        ## Open questions

        Does `a|b` in a cell work?

        | Question | Answer |
        | :--- | :--- |
        | Is it one cell? | **Yes** |
        | And *this*? | Also [yes](https://example.com/a_b_c) |

        Done.
        """,
    ]

    // MARK: Every prefix

    @Test(arguments: replies.indices)
    func everyPrefixDrawsTheStartOfTheWholeAndNeverShrinks(_ index: Int) {
        let reply = Self.replies[index]
        let whole = Self.words(Markdown.parse(reply))
        var before = ""
        var prefix = ""
        for character in reply {
            prefix.append(character)
            let now = Self.words(Markdown.parse(writing: prefix))
            // Nothing is drawn that the whole reply does not draw there: no
            // marker waiting for its partner, no line that becomes a table.
            #expect(whole.hasPrefix(now), "after \(prefix.count) characters the page reads “…\(now.suffix(60))”")
            // And it only grows.
            #expect(now.hasPrefix(before), "after \(prefix.count) characters the page lost “\(before.suffix(40))” for “\(now.suffix(40))”")
            if !whole.hasPrefix(now) || !now.hasPrefix(before) { return }
            before = now
        }
        #expect(before == whole)
    }

    @Test(arguments: replies.indices)
    func everyPrefixDrawsItsCodeToTheLetter(_ index: Int) {
        let reply = Self.replies[index]
        let whole = Self.code(Markdown.parse(reply))
        var prefix = ""
        for character in reply {
            prefix.append(character)
            let now = Self.code(Markdown.parse(writing: prefix))
            #expect(now.count <= whole.count)
            // A block that is finished is the whole's, and the one being
            // written is its start: not a space or a mark more.
            for (at, text) in now.enumerated() where at < whole.count {
                let finished = at < now.count - 1
                #expect(
                    finished ? text == whole[at] : whole[at].hasPrefix(text), "after \(prefix.count) characters, code block \(at) reads “…\(text.suffix(40))”")
            }
        }
    }

    @Test(arguments: replies.indices)
    func everyPrefixKeepsTheBlocksItHadAndTheirKinds(_ index: Int) {
        let reply = Self.replies[index]
        var before: [String] = []
        var prefix = ""
        for character in reply {
            prefix.append(character)
            let now = Self.kinds(Markdown.parse(writing: prefix))
            // A finished block is never taken back or made another kind, nor
            // anything inside it: only the last may still change.
            let settled = before.dropLast()
            #expect(Array(now.prefix(settled.count)) == Array(settled), "after \(prefix.count) characters: \(before) became \(now)")
            if Array(now.prefix(settled.count)) != Array(settled) { return }
            before = now
        }
        #expect(before == Self.kinds(Markdown.parse(reply)))
    }

    @Test(arguments: replies.indices)
    func aFinishedReplyDrawsAsItStands(_ index: Int) {
        // Well formed, a reply being written at its last character already
        // draws what the finished one does.
        let reply = Self.replies[index]
        #expect(Markdown.parse(writing: reply) == Markdown.parse(reply))
    }

    @Test(arguments: replies.indices)
    func withoutClosingThePageShowsMarkersAndTakesThemBack(_ index: Int) {
        // What these tests would miss if they could not see it: the same
        // prefixes, parsed as they stand, draw what the whole does not.
        let reply = Self.replies[index]
        let whole = Self.words(Markdown.parse(reply))
        var prefix = ""
        var strays = 0
        for character in reply {
            prefix.append(character)
            if !whole.hasPrefix(Self.words(Markdown.parse(prefix))) { strays += 1 }
        }
        #expect(strays > 50)
    }

    @Test func anyTextAtAllIsParsedWithoutStopping() {
        // Marks in every order, a piece at a time: nothing here is a reply,
        // and none of it may stop the app.
        let pieces = [
            "*", "**", "_", "`", "``", "```", "~", "~~", "[", "]", "(", ")", "<", ">", "|", "-", "#", "\\", " ", "\n", "a", "1.", "x]", "- [", ":-",
            "\t", "    ", "> ",
        ]
        var seed: UInt64 = 0x2545_F491_4F6C_DD1D
        func next() -> Int {
            seed ^= seed << 13
            seed ^= seed >> 7
            seed ^= seed << 17
            return Int(seed % UInt64(pieces.count))
        }
        for _ in 0 ..< 3000 {
            var text = ""
            for _ in 0 ..< 24 {
                text += pieces[next()]
                let blocks = Markdown.parse(writing: text)
                // Closing adds the marks that close and nothing else: what
                // is drawn is never much more than what was written.
                #expect(Self.drawn(blocks).reduce(0) { $0 + $1.count } <= text.count * 2 + 16)
            }
        }
    }

    // MARK: Each rule

    private func writing(_ text: String) -> [MarkdownBlock] { Markdown.parse(writing: text) }

    @Test func anOpenEmphasisACodeSpanAndAStrikeAreClosed() {
        #expect(writing("You have **three thi") == [.paragraph("You have **three thi**")])
        #expect(writing("the *Q4 bud") == [.paragraph("the *Q4 bud*")])
        #expect(writing("from `finance/act") == [.paragraph("from `finance/act`")])
        #expect(writing("and ~~the old") == [.paragraph("and ~~the old~~")])
        #expect(writing("***both") == [.paragraph("***both***")])
        #expect(writing("**see `foo") == [.paragraph("**see `foo`**")])
        // The closing marker touches the text it closes.
        #expect(writing("a **bold ") == [.paragraph("a **bold**")])
        // Across the lines of one paragraph, and in the block being written
        // alone: a heading its line's end finished keeps what it left open.
        #expect(writing("a **bold\nline") == [.paragraph("a **bold\nline**")])
        #expect(writing("# A **title") == [.heading(level: 1, text: "A **title**")])
        #expect(writing("# A **title\nand *text") == [.heading(level: 1, text: "A **title"), .paragraph("and *text*")])
        // In a list's item and in a quote, whatever marks begin their lines.
        #expect(writing("- one\n- **two") == [.list(ordered: false, start: 0, items: [item("one"), item("**two**")])])
        #expect(writing("> said **so\n> and *more") == [.quote([.paragraph("said **so\nand *more***")])])
        // A paragraph a blank line ended keeps what its writer left open,
        // through a line after it that waits and the block that line becomes.
        #expect(writing("a **bold\n\n") == [.paragraph("a **bold")])
        #expect(writing("a **bold\n\n-") == [.paragraph("a **bold")])
        #expect(writing("a **bold\n\n- [") == [.paragraph("a **bold")])
        #expect(writing("a **bold\n\n---") == [.paragraph("a **bold"), .rule])
        // Under it with no blank line between, the paragraph is still open.
        #expect(writing("a **bold\n-") == [.paragraph("a **bold**")])
    }

    private func item(_ text: String, checked: Bool? = nil) -> MarkdownListItem {
        MarkdownListItem(checked: checked, blocks: text.isEmpty ? [] : [.paragraph(text)])
    }

    @Test func aMarkerWithNothingInItYetWaits() {
        #expect(writing("You have **") == [.paragraph("You have ")])
        #expect(writing("You have *") == [.paragraph("You have ")])
        #expect(writing("use `") == [.paragraph("use ")])
        #expect(writing("and ~") == [.paragraph("and ")])
        #expect(writing("and ~~") == [.paragraph("and ")])
        // Alone, there is nothing to draw yet.
        #expect(writing("Above.\n\n**") == [.paragraph("Above.")])
        #expect(writing("Above.\n\n`") == [.paragraph("Above.")])
        #expect(writing("Above.\n\n``") == [.paragraph("Above.")])
        // Half of the marker that closes.
        #expect(writing("**three things due*") == [.paragraph("**three things due**")])
        #expect(writing("``a ` b`") == [.paragraph("``a ` b``")])
        #expect(writing("an escape \\") == [.paragraph("an escape ")])
    }

    @Test func whatIsNoMarkerIsLeftAlone() {
        for text in [
            "5*3 and 2 * 3", "a snake_case_name", "about ~15 and ~/tasks/task", "it costs 3 < 5 and 7 > 2", "see [1] and [12",
            "array[i", "done **bold** and *it* and `code`.", "a \\* star",
        ] {
            #expect(writing(text) == [.paragraph(text)], "“\(text)” became \(writing(text))")
        }
    }

    @Test func aLinkReadsAsItsLabelUntilItsAddressIsWhole() {
        #expect(writing("is in [the plan") == [.paragraph("is in the plan")])
        #expect(writing("is in [the planning notes]") == [.paragraph("is in the planning notes")])
        #expect(writing("is in [the planning notes](") == [.paragraph("is in the planning notes")])
        #expect(writing("is in [the planning notes](https://exa") == [.paragraph("is in the planning notes")])
        #expect(writing("is in [the planning notes](https://example.com)") == [.paragraph("is in [the planning notes](https://example.com)")])
        #expect(writing("is in [the **plan") == [.paragraph("is in the **plan**")])
        #expect(writing("ask at <https://exa") == [.paragraph("ask at https://exa")])
        #expect(writing("ask at <https://example.com>") == [.paragraph("ask at <https://example.com>")])
    }

    @Test func aTableIsAHeaderRowFromItsFirstCell() {
        func header(_ cells: [String]) -> [MarkdownBlock] {
            [.table(header: cells, alignments: Array(repeating: .leading, count: cells.count), rows: [])]
        }
        #expect(writing("|") == [])
        #expect(writing("| It") == header(["It"]))
        #expect(writing("| Item | Ow") == header(["Item", "Ow"]))
        #expect(writing("| Item | **Ow") == header(["Item", "**Ow**"]))
        #expect(writing("| Item | Owner |") == header(["Item", "Owner"]))
        #expect(writing("| Item | Owner |\n") == header(["Item", "Owner"]))
        // The line under it, in place of the part of it that has arrived.
        #expect(writing("| Item | Owner |\n|") == header(["Item", "Owner"]))
        #expect(writing("| Item | Owner |\n| :-") == header(["Item", "Owner"]))
        #expect(writing("| Item | Owner |\n| :--- | --") == [.table(header: ["Item", "Owner"], alignments: [.leading, .leading], rows: [])])
        #expect(
            writing("| Item | Owner |\n| :--- | ---: |\n| Q4 | **A")
                == [.table(header: ["Item", "Owner"], alignments: [.leading, .trailing], rows: [["Q4", "**A**"]])])
        // A row its line's end finished keeps what it left open.
        #expect(
            writing("| Item | Owner |\n| :--- | ---: |\n| Q4 | **A\n")
                == [.table(header: ["Item", "Owner"], alignments: [.leading, .trailing], rows: [["Q4", "**A"]])])
        // Under a heading, which is a block by itself, as under a blank line.
        #expect(writing("## Costs\n| Item | Ow") == [.heading(level: 2, text: "Costs")] + header(["Item", "Ow"]))
        // Under a line of text it is that text's, as the parser reads it.
        #expect(writing("text\n| a | b") == [.paragraph("text\n| a | b")])
    }

    @Test func aBlocksFirstMarkWaitsUntilItSaysWhatItIs() {
        for start in ["-", "- ", "--", "*", "+", "> -", "|", "- [", "- [x"] {
            #expect(writing("Above.\n\n" + start) == [.paragraph("Above.")], "“\(start)” drew \(writing("Above.\n\n" + start).dropFirst())")
        }
        #expect(writing("Above.\n\n---") == [.paragraph("Above."), .rule])
        #expect(writing("- [x]") == [.list(ordered: false, start: 0, items: [item("", checked: true)])])
        #expect(writing("- [ ] Confirm") == [.list(ordered: false, start: 0, items: [item("Confirm", checked: false)])])
        #expect(writing("- [the link") == [.list(ordered: false, start: 0, items: [item("the link")])])
    }

    @Test func codeIsNeverTouched() {
        let open = "```python\ndef due(run, **kwargs):\n    return `x"
        let code = MarkdownBlock.code(language: "python", text: "def due(run, **kwargs):\n    return `x", closed: false)
        #expect(writing(open) == [code])
        // The fence's closing line waits until it is whole.
        #expect(writing(open + "\n`") == [code])
        #expect(writing(open + "\n``") == [code])
        #expect(writing(open + "\n```") == Markdown.parse(open + "\n```"))
        #expect(writing(open + "\n```\nand **after").last == .paragraph("and **after**"))
        // Wherever the parser says the code is: a quote's fence, a list
        // item's, four spaces, a tab.
        for text in ["> ```\n> a **b", "- ```\n  **literal", "Text.\n\n    x = **kwargs", "\t**literal", "~~~\n**kwargs"] {
            #expect(writing(text) == Markdown.parse(text), "“\(text)” became \(writing(text))")
        }
        // And where it says the code ended: a quote's fence ends with it.
        #expect(writing("> ```\n> x\n\n**bold") == [.quote([.code(language: nil, text: "x", closed: false)]), .paragraph("**bold**")])
        // Under a list item four spaces are its text.
        #expect(
            writing("- item\n\n    more **text") == [
                .list(ordered: false, start: 0, items: [MarkdownListItem(checked: nil, blocks: [.paragraph("item"), .paragraph("more **text**")])])
            ])
    }
}
