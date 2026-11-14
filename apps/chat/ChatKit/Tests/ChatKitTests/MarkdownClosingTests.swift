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

    /// The kinds of the blocks, in order.
    static func kinds(_ blocks: [MarkdownBlock]) -> [String] {
        blocks.map { block in
            switch block {
            case .paragraph: "paragraph"
            case .heading: "heading"
            case .code: "code"
            case .quote: "quote"
            case .list: "list"
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
            let closed = Markdown.closed(prefix)
            let now = Self.words(Markdown.parse(closed))
            // Nothing is drawn that the whole reply does not draw there: no
            // marker waiting for its partner, no line that becomes a table.
            #expect(whole.hasPrefix(now), "after \(prefix.count) characters the page reads “…\(now.suffix(60))”, closed from “…\(closed.suffix(60))”")
            // And it only grows.
            #expect(now.hasPrefix(before), "after \(prefix.count) characters the page lost “\(before.suffix(40))” for “\(now.suffix(40))”")
            before = now
            if !whole.hasPrefix(now) || !now.hasPrefix(before) { return }
        }
        #expect(before == whole)
    }

    @Test(arguments: replies.indices)
    func everyPrefixKeepsTheBlocksItHadAndTheirKinds(_ index: Int) {
        let reply = Self.replies[index]
        var before: [String] = []
        var prefix = ""
        for character in reply {
            prefix.append(character)
            let now = Self.kinds(Markdown.parse(Markdown.closed(prefix)))
            // A finished block is never taken back or made another kind:
            // only the last may still change, and only into what it grows to.
            let settled = before.dropLast()
            #expect(Array(now.prefix(settled.count)) == Array(settled), "after \(prefix.count) characters: \(before) became \(now)")
            if Array(now.prefix(settled.count)) != Array(settled) { return }
            before = now
        }
    }

    @Test(arguments: replies.indices)
    func aFinishedReplyClosesToItself(_ index: Int) {
        let reply = Self.replies[index]
        #expect(Markdown.parse(Markdown.closed(reply)) == Markdown.parse(reply))
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

    @Test func anyTextAtAllClosesToSomethingThatParses() {
        // Marks in every order, a line at a time: nothing here is a reply,
        // and none of it may stop the app.
        let pieces = ["*", "**", "_", "`", "``", "```", "~", "~~", "[", "]", "(", ")", "<", ">", "|", "-", "#", "\\", " ", "\n", "a", "1.", "x]", "- [", ":-"]
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
                let closed = Markdown.closed(text)
                _ = Markdown.parse(closed)
                // Closing adds a table's rule and the marks that close, and
                // takes away only what waits: never more than it was given.
                #expect(closed.count <= text.count * 2 + 64)
            }
        }
    }

    // MARK: Each rule

    @Test func anOpenEmphasisACodeSpanAndAStrikeAreClosed() {
        #expect(Markdown.closed("You have **three thi") == "You have **three thi**")
        #expect(Markdown.closed("the *Q4 bud") == "the *Q4 bud*")
        #expect(Markdown.closed("from `finance/act") == "from `finance/act`")
        #expect(Markdown.closed("and ~~the old") == "and ~~the old~~")
        #expect(Markdown.closed("***both") == "***both***")
        #expect(Markdown.closed("**see `foo") == "**see `foo`**")
        // The closing marker touches the text it closes.
        #expect(Markdown.closed("a **bold ") == "a **bold** ")
        // Across the lines of one paragraph, and not into the next block.
        #expect(Markdown.closed("a **bold\nline") == "a **bold\nline**")
        #expect(Markdown.closed("# A **title\nand text") == "# A **title\nand text")
    }

    @Test func aMarkerWithNothingInItYetWaits() {
        #expect(Markdown.closed("You have **") == "You have ")
        #expect(Markdown.closed("You have *") == "You have ")
        #expect(Markdown.closed("use `") == "use ")
        #expect(Markdown.closed("and ~") == "and ")
        #expect(Markdown.closed("and ~~") == "and ")
        // Half of the marker that closes.
        #expect(Markdown.closed("**three things due*") == "**three things due**")
        #expect(Markdown.closed("``a ` b`") == "``a ` b``")
        #expect(Markdown.closed("an escape \\") == "an escape ")
    }

    @Test func whatIsNoMarkerIsLeftAlone() {
        for text in [
            "5*3 and 2 * 3", "a snake_case_name", "about ~15 and ~/tasks/task", "it costs 3 < 5 and 7 > 2", "see [1] and [12",
            "array[i", "done **bold** and *it* and `code`.", "a \\* star",
        ] {
            #expect(Markdown.closed(text) == text, "“\(text)” became “\(Markdown.closed(text))”")
        }
    }

    @Test func aLinkReadsAsItsLabelUntilItsAddressIsWhole() {
        #expect(Markdown.closed("is in [the plan") == "is in the plan")
        #expect(Markdown.closed("is in [the planning notes]") == "is in the planning notes")
        #expect(Markdown.closed("is in [the planning notes](") == "is in the planning notes")
        #expect(Markdown.closed("is in [the planning notes](https://exa") == "is in the planning notes")
        #expect(Markdown.closed("is in [the planning notes](https://example.com)") == "is in [the planning notes](https://example.com)")
        #expect(Markdown.closed("is in [the **plan") == "is in the **plan**")
        #expect(Markdown.closed("ask at <https://exa") == "ask at https://exa")
        #expect(Markdown.closed("ask at <https://example.com>") == "ask at <https://example.com>")
    }

    @Test func aTableIsAHeaderRowFromItsFirstCell() {
        let rule = "| --- | --- |"
        #expect(Markdown.closed("|") == "")
        #expect(Markdown.closed("| It") == "| It\n| --- |")
        #expect(Markdown.closed("| Item | Ow") == "| Item | Ow\n\(rule)")
        #expect(Markdown.closed("| Item | Owner |") == "| Item | Owner |\n\(rule)")
        #expect(Markdown.closed("| Item | Owner |\n") == "| Item | Owner |\n\(rule)\n")
        // The line under it, in place of the part of it that has arrived.
        #expect(Markdown.closed("| Item | Owner |\n|") == "| Item | Owner |\n\(rule)\n")
        #expect(Markdown.closed("| Item | Owner |\n| :-") == "| Item | Owner |\n\(rule)")
        #expect(Markdown.closed("| Item | Owner |\n| :--- | --") == "| Item | Owner |\n| :--- | --")
        #expect(Markdown.closed("| Item | Owner |\n| :--- | ---: |\n| Q4 | **A") == "| Item | Owner |\n| :--- | ---: |\n| Q4 | **A**")
        for prefix in ["| It", "| Item | Owner |\n", "| Item | Owner |\n| :"] {
            guard case let .table(header, _, rows)? = Markdown.parse(Markdown.closed(prefix)).first else {
                Issue.record("“\(prefix)” is not a table")
                continue
            }
            #expect(rows.isEmpty && !header.isEmpty)
        }
        // Under a line of text it is that text's, as the parser reads it.
        #expect(Markdown.closed("text\n| a | b") == "text\n| a | b")
    }

    @Test func aBlocksFirstMarkWaitsUntilItSaysWhatItIs() {
        for start in ["-", "- ", "--", "*", "+", "> -"] {
            #expect(Markdown.parse(Markdown.closed("Above.\n\n" + start)) == [.paragraph("Above.")], "“\(start)” drew something")
        }
        #expect(Markdown.closed("Above.\n\n---") == "Above.\n\n---")
        #expect(Markdown.closed("- [") == "")
        #expect(Markdown.closed("- [x") == "")
        #expect(Markdown.closed("- [x]") == "- [x] ")
        #expect(Markdown.closed("- [ ] Confirm") == "- [ ] Confirm")
        #expect(Markdown.closed("- [the link") == "- the link")
    }

    @Test func codeIsNeverTouched() {
        let open = "```python\ndef due(run, **kwargs):\n    return `x"
        #expect(Markdown.closed(open) == open)
        // The fence's closing line waits until it is whole.
        #expect(Markdown.closed(open + "\n`") == open)
        #expect(Markdown.closed(open + "\n``") == open)
        #expect(Markdown.closed(open + "\n```") == open + "\n```")
        #expect(Markdown.closed(open + "\n```\nand **after") == open + "\n```\nand **after**")
        #expect(Markdown.closed("> ```\n> a **b") == "> ```\n> a **b")
        let indented = "Text.\n\n    x = **kwargs"
        #expect(Markdown.closed(indented) == indented)
        // Under a list item four spaces are its text.
        #expect(Markdown.closed("- item\n\n    more **text") == "- item\n\n    more **text**")
    }
}
