import SubstrateKit
import SwiftUI

#if canImport(UIKit)
import UIKit
#endif

// Every size the chat's views are drawn with, in one place: the space
// between things, the sizes that are fixed, the corners and the lines. A
// view names what a number is for and never the number, and each name here
// is a step of the kit's scale (`DesignTokens.Spacing`, `Radius`), so the
// chat is made roomier or tighter there, and one relationship is moved
// here. The colours are in `ChatColours`, the type in `ChatType`, and what
// moves in `ChatMotion`.

/// The space between two things, by what they are to each other.
enum ChatSpace {
    private static func step(_ step: DesignTokens.Spacing) -> CGFloat { step.points }

    // MARK: Down a column

    /// A turn and the next.
    static let turns = step(.screen)
    /// The blocks of a reply; the sections of a card that asks.
    static let blocks = step(.group)
    /// The parts of one turn, one card, one strip.
    static let parts = step(.row)
    /// Like things in a column: options, facts, cards, the lines over the
    /// composer.
    static let items = step(.medium)
    /// The lines of one list, and a heading over what it heads.
    static let lines = step(.small)
    /// Two lines that are one thing: a label over its value, a title over
    /// its note.
    static let pair = step(.hairline)

    // MARK: Along a line

    /// A mark and the word it belongs to.
    static let mark = step(.tight)
    /// Like things on one line: pills in a wrap, a title and its date.
    static let words = step(.small)
    /// A mark's column and the text it marks; the least between a row's
    /// two sides.
    static let beside = step(.medium)
    /// Controls side by side, and a rule and the word it carries.
    static let apart = step(.row)

    // MARK: From an edge

    /// A card's content from its edge; a bubble's and a chip's words from
    /// their sides.
    static let inset = step(.card)
    /// What stands inside a fill that is itself inside one: a box in a
    /// card, a table's cell, a bubble's words from its top and bottom.
    static let inner = step(.row)
    /// A block's own content from its edge: code, a table.
    static let blockInset = step(.group)
    /// A pill's word from its ends and from its top and bottom: the kit's
    /// record pill's, so every pill is one shape.
    static let pillSide = RecordPillStyle.trailing
    static let pillEdge = RecordPillStyle.vertical
    /// The transcript from the window's sides.
    static let margin = step(.section)
    /// What floats over a screen from its sides (the composer's bar, a
    /// strip, the Undo), and the transcript from its top.
    static let gutter = step(.group)
    /// Words from the edge of the surface they are on: the field's, a
    /// preview's, the transcript's last row from the bar; and the lines
    /// over the field, which stand over its words.
    static let edge = step(.content)
    /// Text behind a rail: a quote, the model's thinking.
    static let indent = step(.group)
    /// A whole screen's words from its edges: no access, a sheet that
    /// loads.
    static let screen = step(.block)
}

/// A size that is fixed at the usual text size and grows with the text:
/// read through `@Scaled`, never as a number. What bounds words grows as
/// far as the words do. What bounds a mark or a control stops at `most`,
/// since a mark three times its size takes the room the words need, and
/// the mark is held to the same size (`growing(to:)`).
struct ChatSize: Sendable {
    let points: CGFloat
    /// The text style it grows with.
    let style: Font.TextStyle
    /// The largest text size it grows to; nil is every size.
    var most: DynamicTypeSize?

    /// The column a row's leading mark stands in: a tool's status, a
    /// card's symbol, a failure's.
    static let markColumn = ChatSize(points: 20, style: .subheadline, most: .accessibility2)
    /// A dot: unread, working, answering.
    static let dot = ChatSize(points: 8, style: .body, most: .accessibility1)
    /// The composer's row: its field's least height and its round buttons.
    static let control = ChatSize(points: 46, style: .body, most: .xxxLarge)
    /// The window on the model's thinking: about three of its lines.
    static let thinkingWindow = ChatSize(points: 58, style: .footnote)
    /// The bar above a block of code.
    static let codeBar = ChatSize(points: 30, style: .caption, most: .accessibility2)
    /// The microphone's meter, as tall as its loudest bar.
    static let meter = ChatSize(points: 24, style: .body, most: .xxxLarge)
    /// A mark of the row under a reply and of the line over the field.
    static let actionMark = ChatSize(points: 16, style: .subheadline, most: .accessibility2)
    /// A new chat's title and its assistant under it, in the bar: the bar
    /// holds two lines no larger than this.
    static let barTitle = ChatSize(points: 17, style: .body, most: .xxxLarge)

    /// The size at a text size.
    func points(at size: DynamicTypeSize) -> CGFloat {
        #if canImport(UIKit)
        let held = most.map { min(size, $0) } ?? size
        return Self.scaled.value(points, style, held)
        #else
        // A Mac has one text size.
        return points
        #endif
    }

    #if canImport(UIKit)
    private static let styles: [Font.TextStyle: UIFont.TextStyle] = [
        .body: .body, .subheadline: .subheadline, .footnote: .footnote, .caption: .caption1, .title2: .title2,
    ]

    /// Each size at each text size, worked out once: a row reads its sizes
    /// every time it is drawn, and a reply being written is drawn at every
    /// flush.
    private final class Scaling: @unchecked Sendable {
        private struct Key: Hashable {
            let points: CGFloat
            let style: Font.TextStyle
            let size: DynamicTypeSize
        }

        private let lock = NSLock()
        private var known: [Key: CGFloat] = [:]

        func value(_ points: CGFloat, _ style: Font.TextStyle, _ size: DynamicTypeSize) -> CGFloat {
            let key = Key(points: points, style: style, size: size)
            lock.lock()
            defer { lock.unlock() }
            if let value = known[key] { return value }
            let traits = UITraitCollection(preferredContentSizeCategory: UIContentSizeCategory(size))
            let value = UIFontMetrics(forTextStyle: ChatSize.styles[style] ?? .body).scaledValue(for: points, compatibleWith: traits)
            known[key] = value
            return value
        }
    }

    private static let scaled = Scaling()
    #endif

    // MARK: Not scaled

    /// The app's mark over a screen of its own.
    static let appMark: CGFloat = 72
    /// The least a control may be to touch (`DesignTokens.touchTarget`).
    static let target = DesignTokens.touchTarget
    /// The width a transcript keeps however wide its window: a readable
    /// measure, about 75 characters of the body's type.
    static let measure: CGFloat = 680
    /// A pill's widest, where its words may be long: a page's title.
    static let pillMax: CGFloat = 280
    /// A page's preview on a long press.
    static let preview: CGFloat = 320
    /// A screen of one sentence and one button: no access.
    static let door: CGFloat = 440
    /// The least a Mac's sheet is, and a sheet of text to read: a reply
    /// opened for selecting, the diagnostics.
    static let sheet = CGSize(width: 420, height: 260)
    static let readingSheet = (least: CGSize(width: 520, height: 420), ideal: CGSize(width: 620, height: 560))
    /// A Mac's windows as they first open: the chats, and Settings.
    static let window = CGSize(width: 1080, height: 760)
    static let settings = CGSize(width: 560, height: 620)
    /// The list's column on an iPad and a Mac.
    static let sidebar = (least: CGFloat(260), ideal: CGFloat(320), most: CGFloat(420))
    /// The far side of a bubble: a message never runs the transcript's
    /// whole width.
    static let bubbleGutter: CGFloat = 48
}

/// A `ChatSize` at the text's size.
@propertyWrapper
struct Scaled: DynamicProperty {
    @Environment(\.dynamicTypeSize) private var size
    private let token: ChatSize

    init(_ size: ChatSize) {
        token = size
    }

    var wrappedValue: CGFloat { token.points(at: size) }
}

extension View {
    /// What this view draws (a mark, a control's symbol) grows with the
    /// text as far as `size` does, and no further: it stays inside the
    /// room `@Scaled` gives it.
    @ViewBuilder func growing(to size: ChatSize) -> some View {
        if let most = size.most { dynamicTypeSize(...most) } else { self }
    }
}

/// A corner, by what it is the corner of.
enum ChatRadius {
    /// A block inside a reply or a card: code, a table, a box of
    /// arguments.
    static let block = DesignTokens.Radius.control.points
    /// A card, a strip, the Undo.
    static let card = DesignTokens.Radius.card.points
    /// The person's own message.
    static let bubble: CGFloat = 20
}

/// A line's width.
enum ChatLine {
    /// A card's edge and a table's rule.
    static let hairline: CGFloat = 0.5
    /// A rule across the transcript, and an outline.
    static let rule: CGFloat = 1
    /// The rail beside a quote and the model's thinking; a bar of the
    /// microphone's meter.
    static let rail: CGFloat = 3
}
