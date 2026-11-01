import Foundation
import Testing
import SubstrateKit
@testable import SubstrateChat

@Suite struct MarkdownBlocks {
    @Test func paragraphsHeadingsAndRules() {
        let blocks = Markdown.parse("# Title\n\nOne line\nnext line\n\n---\n\nAfter")
        #expect(blocks == [
            .heading(level: 1, text: "Title"),
            .paragraph("One line\nnext line"),
            .rule,
            .paragraph("After"),
        ])
    }

    @Test func anUnclosedFenceIsStillCode() {
        #expect(Markdown.parse("Here:\n```swift\nlet a = 1") == [
            .paragraph("Here:"),
            .code(language: "swift", text: "let a = 1", closed: false),
        ])
        #expect(Markdown.parse("```\nx\n```\ntail") == [
            .code(language: nil, text: "x", closed: true),
            .paragraph("tail"),
        ])
    }

    @Test func nestedListsAndTasks() {
        let blocks = Markdown.parse("- one\n  - inner\n- [x] done\n- [ ] todo")
        guard case let .list(ordered, _, items) = blocks.first else { Issue.record("not a list"); return }
        #expect(!ordered)
        #expect(items.count == 3)
        #expect(items[0].blocks == [.paragraph("one"), .list(ordered: false, start: 0, items: [
            MarkdownListItem(checked: nil, blocks: [.paragraph("inner")]),
        ])])
        #expect(items[1].checked == true)
        #expect(items[2].checked == false)
    }

    @Test func orderedListsKeepTheirStartAndLooseItems() {
        let blocks = Markdown.parse("3. three\n\n4. four\n\nafter")
        guard case let .list(ordered, start, items) = blocks.first else { Issue.record("not a list"); return }
        #expect(ordered)
        #expect(start == 3)
        #expect(items.count == 2)
        #expect(blocks.last == .paragraph("after"))
    }

    @Test func aNumberMidParagraphIsNotAList() {
        #expect(Markdown.parse("We have\n2. reasons") == [.paragraph("We have\n2. reasons")])
    }

    @Test func tables() {
        let blocks = Markdown.parse("| a | b |\n|:--|--:|\n| 1 | `x|y` |\n| 2 |")
        #expect(blocks == [.table(header: ["a", "b"], alignments: [.leading, .trailing],
                                  rows: [["1", "`x|y`"], ["2", ""]])])
    }

    @Test func aHalfTableIsText() {
        #expect(Markdown.parse("| a | b |") == [.paragraph("| a | b |")])
    }

    @Test func quotes() {
        #expect(Markdown.parse("> said\n> - item") == [.quote([.paragraph("said"), .list(ordered: false, start: 0, items: [
            MarkdownListItem(checked: nil, blocks: [.paragraph("item")]),
        ])])])
    }

    @Test func everyPrefixParses() {
        let text = "# H\n\n- a\n  1. b\n\n```js\nx\n```\n\n| a |\n|---|\n| 1 |\n\n> q"
        for n in 0 ... text.count {
            _ = Markdown.parse(String(text.prefix(n)))
        }
    }
}

@Suite struct Previews {
    @Test func markdownReadsAsOneLine() {
        let text = "Here is **bold**:\n\n## Head\n\n- one\n- [x] two\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```swift\nlet x = 1\n```\n> quoted `code`"
        #expect(plainText(markdown: text) == "Here is bold: Head one two a, b 1, 2 let x = 1 quoted code")
    }

    @Test func aReplyAlreadyOnOneLineLosesItsMarkersToo() {
        let line = "Here is what I found, **in Markdown**: ## A heading - First point * Second - [x] done | Name | Value | |:--|--:| | a | 1 | ```swift let x = 1 ``` > quoted"
        #expect(plainText(markdown: line) == "Here is what I found, in Markdown: A heading - First point Second done, Name, Value, a, 1, let x = 1 quoted")
    }
}

@Suite struct RecordLinks {
    let kind = "ada.example.com/tasks/task"

    @Test func bothSpellingsNameTheRecord() {
        #expect(linkedRecord(URL(string: "substrate://\(kind)/q4")!) == RecordReference(kind: kind, id: "q4"))
        #expect(linkedRecord(URL(string: "ref:\(kind)/q4")!) == RecordReference(kind: kind, id: "q4"))
        #expect(linkedRecord(URL(string: "https://example.com/a/b/c")!) == nil)
        #expect(linkedRecord(URL(string: "substrate://nodot/b/c/d")!) == nil)
    }

    @Test func aRecordLinkStaysALinkAndAMissingOneIsText() {
        let text = "[Q4](ref:\(kind)/q4) and [Ghost](substrate://\(kind)/ghost) and [x](javascript:alert)"
        let out = parseInline(text, missing: ["\(kind)/ghost"])
        let links = out.runs.compactMap(\.link)
        #expect(links == [URL(string: "substrate://\(kind)/q4")!])
        #expect(String(out.characters) == "Q4 and Ghost and x")
    }
}
