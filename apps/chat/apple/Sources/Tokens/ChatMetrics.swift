import SubstrateKit
import SwiftUI

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
/// read through `@Scaled`, never as a number.
struct ChatSize: Sendable {
    let points: CGFloat
    /// The text style it grows with.
    let style: Font.TextStyle

    /// The column a row's leading mark stands in: a tool's status, a
    /// card's symbol, a failure's.
    static let markColumn = ChatSize(points: 20, style: .subheadline)
    /// A dot: unread, working, answering.
    static let dot = ChatSize(points: 8, style: .body)
    /// The composer's row: its field's least height and its round buttons.
    static let control = ChatSize(points: 46, style: .body)
    /// The window on the model's thinking: about three of its lines.
    static let thinkingWindow = ChatSize(points: 58, style: .footnote)
    /// The bar above a block of code.
    static let codeBar = ChatSize(points: 30, style: .caption)
    /// The microphone's meter, as tall as its loudest bar.
    static let meter = ChatSize(points: 24, style: .body)
    /// A mark of the row under a reply, whose target is wider.
    static let actionMark = ChatSize(points: 16, style: .subheadline)
    /// The app's mark over a screen of its own.
    static let appMark = ChatSize(points: 72, style: .title2)

    // MARK: Not scaled

    /// The least a control may be to touch (`DesignTokens.touchTarget`).
    static let target = DesignTokens.touchTarget
    /// The width a transcript keeps however wide its window: a readable
    /// measure, about 75 characters of the body's type.
    static let measure: CGFloat = 680
    /// A pill's widest, where its words may be long: a page's title, an
    /// assistant's name in the composer.
    static let pillMax: CGFloat = 280
    static let chipMax: CGFloat = 180
    /// A page's preview on a long press.
    static let preview: CGFloat = 320
    /// A screen of one sentence and one button: no access.
    static let door: CGFloat = 440
    /// The least a Mac's sheet is, and the sheet a reply is selected in.
    static let sheet = CGSize(width: 420, height: 260)
    static let readingSheet = (least: CGSize(width: 520, height: 420), ideal: CGSize(width: 620, height: 560))
    /// A Mac's windows as they first open: the chats, Settings, and
    /// Settings with nobody signed in.
    static let window = CGSize(width: 1080, height: 760)
    static let settings = CGSize(width: 560, height: 620)
    static let settingsEmpty = CGSize(width: 360, height: 160)
    /// The list's column on an iPad and a Mac.
    static let sidebar = (least: CGFloat(260), ideal: CGFloat(320), most: CGFloat(420))
    /// The far side of a bubble: a message never runs the transcript's
    /// whole width.
    static let bubbleGutter: CGFloat = 48
}

/// A `ChatSize` at the text's size.
@propertyWrapper
struct Scaled: DynamicProperty {
    @ScaledMetric private var value: CGFloat

    init(_ size: ChatSize) {
        _value = ScaledMetric(wrappedValue: size.points, relativeTo: size.style)
    }

    var wrappedValue: CGFloat { value }
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
