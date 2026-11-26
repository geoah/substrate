import SwiftUI
import TodayCore

/// The control marks the pane draws itself: a control's glyph acts only
/// inside a button's shape, or as the chevron of a row.
enum Symbol: String, Sendable {
    case back = "chevron.backward"
    /// Opens a page.
    case page = "chevron.right"
    /// Opens in place.
    case open = "chevron.down"
    /// Open: closes in place.
    case close = "chevron.up"
    case flag = "flag"
    case dismiss = "xmark"
    case settings = "gearshape"
    case tick = "checkmark"
    /// Takes a finish back while it counts down.
    case undo = "arrow.uturn.backward"
    case add = "plus"
    case edit = "pencil"
    case dictate = "mic"
    case send = "arrow.up"
    /// Opens the day's conversation with the assistant.
    case chat = "bubble.left.and.text.bubble.right"
    /// Leaves the app: a page at its source, a record in a browser.
    case leaves = "arrow.up.right"

    var systemName: String { rawValue }
}

extension Glyph {
    /// The SF Symbol the kit names for it (`KindGlyphSpec`, `DesignTokens.Control`).
    var systemName: String {
        switch self {
        // Not a check in a circle: a task's row ends with a real tick, and
        // its kind's mark must not read as a second one.
        case .task: "list.clipboard"
        case .meeting: "calendar"
        case .routine: "repeat"
        case .night: "moon"
        case .moss: "sparkles"
        case .urgent: "flame"
        case .plan: "checklist"
        case .done: "checkmark"
        case .edit: "pencil"
        case .add: "plus"
        case .dictate: "mic"
        case .send: "arrow.up"
        case .join: "video"
        case .document: "doc.text"
        case .link: "arrow.up.right"
        case .clock: "clock"
        }
    }
}

/// Where a tile, a badge or a glyph is drawn, which sets its size.
enum MarkSize {
    /// Inside a chip, and beside a meeting's title: 20 points.
    case chip
    /// Leading a row: 26 points.
    case row
    /// On the current item: a glyph is 24 points there.
    case lead
    /// Beside a page's title: 34 points.
    case page

    /// A tile's or a badge's side.
    var points: CGFloat {
        switch self {
        case .chip: 20
        case .row, .lead: 26
        case .page: 34
        }
    }

    var letters: CGFloat {
        switch self {
        case .chip: 9
        case .row, .lead: 11
        case .page: 14
        }
    }

    /// A tile's corners: barely rounded, so it never reads as a circle.
    var corner: CGFloat {
        switch self {
        case .chip: 3
        case .row, .lead: 4
        case .page: 5
        }
    }

    /// A glyph's size inside its box.
    var glyph: CGFloat {
        switch self {
        case .chip: 14
        case .row: 20
        case .lead: 24
        case .page: 26
        }
    }

    /// The box a bare glyph is drawn in: a tile's or a badge's side, or a
    /// little more than the glyph where the glyph is larger.
    var glyphBox: CGFloat { max(glyph + 4, points) }
}

/// A kind's glyph in the box rows line up on: bare, with no fill and no
/// shape behind it, so it is never mistaken for a tile or a badge. The
/// urgent flame is in the urgent red; every other kind is in ink.
struct GlyphView: View {
    var glyph: Glyph
    var size: MarkSize = .row

    var body: some View {
        Image(systemName: glyph.systemName)
            .font(.system(size: size.glyph, weight: .regular))
            .foregroundStyle(tint)
            .frame(width: size.glyphBox, height: size.glyphBox)
            .accessibilityHidden(true)
    }

    private var tint: Color { glyph == .urgent ? .urgent : .ink }
}

/// A mark beside a row's words: a kind's glyph, a badge, a chevron. It
/// stands at the middle of the first line of those words, at every text
/// size and however many lines the words run to. An unseen letter in the
/// words' font gives that line's place and its baseline, so the mark sits
/// on one baseline with the words in a stack aligned on the first text
/// baseline, and in a stack aligned at the top it is as tall as one line of
/// them. The mark itself is laid over it and takes no room of its own
/// beyond `width` and `minHeight`. Only for marks that say nothing to
/// VoiceOver: a control goes beside the words, not in here.
struct FirstLineMark<Mark: View>: View {
    var width: CGFloat
    /// The least room it takes down the row: a glyph's box, so a line of
    /// one row of words is as tall as its glyph.
    var minHeight: CGFloat = 0
    /// The font of the words it stands beside.
    var font: Font = TodayLook.Text.rowTitle
    @ViewBuilder var mark: () -> Mark

    var body: some View {
        Text(verbatim: "H")
            .font(font)
            .fixedSize()
            .hidden()
            .frame(width: width)
            .frame(minHeight: minHeight)
            .alignmentGuide(.firstTextBaseline) { $0[.firstTextBaseline] }
            .overlay { mark() }
            .accessibilityHidden(true)
    }
}

/// The chevron of a row: right for a page, down for what opens in place,
/// up while it is open. No other arrow is drawn anywhere.
struct RowChevron: View {
    enum Turn {
        case page
        case closed
        case open
    }

    var turn: Turn

    var body: some View {
        Image(systemName: symbol.systemName)
            .font(TodayLook.font(.footnote, weight: .semibold))
            .foregroundStyle(.inkQuiet)
            .frame(width: 20, height: 20)
            .accessibilityHidden(true)
    }

    private var symbol: Symbol {
        switch turn {
        case .page: .page
        case .closed: .open
        case .open: .close
        }
    }
}
