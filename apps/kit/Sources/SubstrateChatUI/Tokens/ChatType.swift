#if canImport(SwiftUI)
import SubstrateUI
import SwiftUI

/// The chat's type, by what a run of text is: each a Dynamic Type style and
/// a weight, so all of it grows with the text size. The roles the kit's
/// screens share are the kit's (`DesignTokens.TextRole`); the rest are a
/// chat's own.
enum ChatType {
    /// A screen's own heading, and a new chat's: who it is with.
    static let title = DesignTokens.TextRole.screenTitle.font
    /// A card's title, a failure's sentence, a table's header.
    static let heading = DesignTokens.TextRole.heading.font
    /// A message, a reply, a row's own words.
    static let body = DesignTokens.TextRole.body.font
    /// A row's title in a list: heavier while it is unread.
    static func rowTitle(unread: Bool = false) -> Font { body.weight(unread ? .semibold : .medium) }
    /// A control's mark on glass: Send, Stop, Jump to Latest.
    static let control = body.weight(.semibold)
    /// What a row says under or beside its title: a tool's line, the
    /// model's thinking, a preview, a table's cell.
    static let detail = DesignTokens.TextRole.secondary.font
    /// A note under something, and a fact's value.
    static let note = DesignTokens.TextRole.footnote.font
    /// A note that stands out: a disclosure, a link out, an agent's name
    /// in its pill.
    static let noteStrong = note.weight(.semibold)
    /// A caption: a date, a description, a count.
    static let caption = DesignTokens.TextRole.identifier.font
    /// A label over its value, a badge's word, a section's name.
    static let label = DesignTokens.TextRole.badge.font
    /// The smallest words: a mark's on a row, a chevron, a fact of the
    /// machinery.
    static let small = Font.caption2
    static let smallLabel = Font.caption2.weight(.semibold)
    /// Text exactly as something outside the app wrote it: a tool's
    /// output, the server's own words.
    static let verbatim = Font.caption.monospaced()
    /// A block of code.
    static let code = Font.system(.callout, design: .monospaced)
    /// A keyword and a comment in it: told apart by more than a colour.
    static let codeKeyword = code.weight(.semibold)
    static let codeComment = code.italic()
    /// Code inside a line.
    static let inlineCode = Font.system(.body, design: .monospaced)

    /// A heading inside a reply, by its level.
    static func heading(level: Int) -> Font {
        switch level {
        case 1: .title2.weight(.bold)
        case 2: .title3.weight(.bold)
        default: .headline
        }
    }

    /// The weight of words that are told apart inside a line: a name
    /// before a preview, a label before its value, a word a search found.
    static let strong = Font.Weight.semibold
}
#endif
