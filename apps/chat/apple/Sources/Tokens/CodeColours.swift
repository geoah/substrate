import ChatKit
import SubstrateKit
import SwiftUI

/// The colours of a reply's code and of its tables, named beside the
/// chat's others (`ChatColours`) and held to WCAG's ratio with them by
/// `ColourContrast`: each token's colour on the code block's fill, in light
/// and in dark.
///
/// No colour says anything alone. A keyword is also heavier and a comment
/// slanted; a diff's added and removed lines start with their own `+` and
/// `-`; the rest (a string's quotes, a number's digits) say what they are
/// in the text itself.
extension ChatColours {
    /// How much of grey a code block's fill and a table's are, over the
    /// page: opaque once blended, so a table's held column covers what
    /// scrolls under it.
    static let codeTint = 0.09
    static let tableTint = 0.06

    static let codeFill = DesignTokens.gray.over(page, opacity: codeTint)
    static let tableFill = DesignTokens.gray.over(page, opacity: tableTint)

    /// A token's colour: Xcode's and GitHub's hues, each at a tone that
    /// reads on the fill.
    static func code(_ token: CodeToken) -> Colour {
        switch token {
        case .keyword: Colour(light: 0x9B2393, dark: 0xFF7AB2)
        case .string: Colour(light: 0xB3261E, dark: 0xFF8170)
        case .comment: Colour(light: 0x566370, dark: 0x9AA7B3)
        case .number: Colour(light: 0x1C00CF, dark: 0xD9C97C)
        case .name: Colour(light: 0x0B5F8F, dark: 0x6CC7E0)
        case .meta: Colour(light: 0x78492A, dark: 0xFFA14F)
        case .added: Colour(light: 0x116329, dark: 0x7EE787)
        case .removed: Colour(light: 0xA4201A, dark: 0xFFA198)
        }
    }

    /// The pairings a code block and a table draw.
    static var codePairs: [Pair] {
        CodeToken.allCases.map { Pair(name: "code: a \($0.rawValue) on a code block", text: code($0), fill: codeFill) } + [
            Pair(name: "code: plain code on a code block", text: label, fill: codeFill),
            Pair(name: "code: a block's language and its buttons", text: DesignTokens.quietText, fill: codeFill),
            Pair(name: "a table's words", text: label, fill: tableFill),
            Pair(name: "a table's header beside its value, in a card", text: DesignTokens.quietText, fill: tableFill),
            Pair(name: "a link in a table", text: accent, fill: tableFill),
        ]
    }
}
