import Foundation
import SubstrateChatCore
import Testing

@testable import SubstrateChat
@testable import SubstrateChatUI

/// A reply as it is written, through the renderer the app draws it with:
/// SubstrateChatCore's own tests hold the same with a renderer by rule, since the
/// real one is not on every system SubstrateChatCore builds for.
@MainActor
@Suite struct StreamedMarkdownTests {
    /// What some blocks draw, as words: each inline text as the app's
    /// inline parser reads it, code as it is.
    static func words(_ blocks: [MarkdownBlock]) -> String {
        func inline(_ text: String) -> String { String(parseInline(text).characters) }
        func drawn(_ blocks: [MarkdownBlock]) -> [String] {
            blocks.flatMap { block -> [String] in
                switch block {
                case let .paragraph(text), let .heading(_, text): [inline(text)]
                case let .code(_, text, _): [text]
                case let .quote(blocks): drawn(blocks)
                case let .list(ordered, start, items):
                    items.enumerated().flatMap { index, item in (ordered ? ["\(start + index)."] : []) + drawn(item.blocks) }
                case let .table(header, _, rows): ([header] + rows).flatMap { $0.map(inline) }
                case .rule: []
                }
            }
        }
        return drawn(blocks).joined(separator: " ").split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }

    @Test(.timeLimit(.minutes(5))) func everyPrefixOfAReplyDrawsTheStartOfTheWholeAndNeverShrinks() {
        let reply = ChatFixtures.reply
        let whole = Self.words(Markdown.parse(reply))
        var before = ""
        var prefix = ""
        for character in reply {
            prefix.append(character)
            let now = Self.words(Markdown.parse(writing: prefix))
            #expect(whole.hasPrefix(now), "after \(prefix.count) characters the page reads “…\(now.suffix(60))”")
            #expect(now.hasPrefix(before), "after \(prefix.count) characters the page lost “\(before.suffix(40))” for “\(now.suffix(40))”")
            if !whole.hasPrefix(now) || !now.hasPrefix(before) { return }
            before = now
        }
        #expect(before == whole)
    }

    @Test(.timeLimit(.minutes(5))) func parsedAsItStandsTheSamePrefixesShowMarkers() {
        // What the test above would miss if it could not see it.
        let reply = ChatFixtures.reply
        let whole = Self.words(Markdown.parse(reply))
        var prefix = ""
        var strays = 0
        for character in reply {
            prefix.append(character)
            if !whole.hasPrefix(Self.words(Markdown.parse(prefix))) { strays += 1 }
        }
        #expect(strays > 50)
    }
}
