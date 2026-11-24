import SwiftUI
import TodayGlance

/// The widgets' look: the few of the app's colours, sizes and type a widget
/// draws with. They are repeated here because the extension links no part
/// of the app, and a test holds each colour to `TodayLook`'s and each glyph
/// to the app's own, so a widget cannot drift from the pane.
enum GlanceLook {
    /// A colour in light mode and in dark, as the app's tokens hold one.
    struct Colour: Equatable, Sendable {
        var light: UInt32
        var dark: UInt32

        /// The colour as SwiftUI draws it: `light`, and `dark` in dark mode.
        var color: Color {
            func srgb(_ value: UInt32) -> (CGFloat, CGFloat, CGFloat) {
                (CGFloat((value >> 16) & 0xFF) / 255, CGFloat((value >> 8) & 0xFF) / 255, CGFloat(value & 0xFF) / 255)
            }
            let (l, d) = (srgb(light), srgb(dark))
            #if canImport(UIKit)
            return Color(
                uiColor: UIColor { traits in
                    let c = traits.userInterfaceStyle == .dark ? d : l
                    return UIColor(red: c.0, green: c.1, blue: c.2, alpha: 1)
                })
            #else
            return Color(
                nsColor: NSColor(name: nil) { appearance in
                    let c = appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua ? d : l
                    return NSColor(srgbRed: c.0, green: c.1, blue: c.2, alpha: 1)
                })
            #endif
        }
    }

    // MARK: Colour

    /// Titles and times.
    static let ink = Colour(light: 0x16181B, dark: 0xF2F3F5)
    /// Second lines, counts and what is said quietly: still 4.5:1 on the card.
    static let inkSecond = Colour(light: 0x565C66, dark: 0xAEB3BC)
    /// The widget's own background, the app's card.
    static let card = Colour(light: 0xFFFFFF, dark: 0x1F2128)
    /// The flame of something urgent.
    static let urgent = Colour(light: 0xE4572E, dark: 0xF0714B)
    /// Two meetings that wait for a choice: the band behind them and its words.
    static let cautionWords = Colour(light: 0x8A5A0A, dark: 0xE9BE6A)
    static let cautionFill = Colour(light: 0xFBEFD3, dark: 0x3A2E14)
    static let flagHigh = Colour(light: 0xD63E18, dark: 0xFF8259)
    static let flagMedium = Colour(light: 0xE7A52A, dark: 0xB8862A)
    static let flagLow = Colour(light: 0xB1BBCA, dark: 0x6B7686)

    static func flag(_ flag: GlanceFlag) -> Colour {
        switch flag {
        case .high: flagHigh
        case .medium: flagMedium
        case .low: flagLow
        }
    }

    // MARK: Sizes

    static let stripeWidth: CGFloat = 4
    /// How far into the widget's margin a flag's stripe hangs beside its
    /// row, so rows with a flag and rows without one keep their glyphs and
    /// their words on one edge.
    static let stripeHang: CGFloat = 9
    /// The box a glyph leads a row in: every row starts its words on one edge.
    static let glyphBox: CGFloat = 16
    /// A glyph's own size in that box.
    static let glyphSize: CGFloat = 12
    /// Between a glyph and the words beside it, and between a row's parts.
    static let glyphGap: CGFloat = 6
    /// Above and below a row's words: a row of the default text size is
    /// twenty points, so a medium widget draws six and a large one sixteen.
    static let rowPadding: CGFloat = 2

    // MARK: Type

    /// What a run of text is in a widget: the app's type, two steps
    /// smaller, so a row of the home screen holds six things.
    enum Role {
        /// The title of the row that leads the day.
        case lead
        /// A row's title.
        case row
        /// A meeting's start.
        case time
        /// A meeting's end, after its start.
        case until
        /// What ends a row quietly, a count, and what is said last.
        case quiet
    }

    /// A Dynamic Type style on an iPhone and an iPad, so text grows with the
    /// text size. A Mac, where the gallery draws, has no text size: there it
    /// is the size an iPhone draws by default, times `scale`.
    static func font(_ role: Role, scale: CGFloat = 1) -> Font {
        let (style, weight, design): (Font.TextStyle, Font.Weight, Font.Design) =
            switch role {
            case .lead: (.footnote, .semibold, .default)
            case .row: (.footnote, .regular, .default)
            case .time: (.caption, .semibold, .rounded)
            case .until: (.caption, .regular, .rounded)
            case .quiet: (.caption2, .medium, .default)
            }
        #if os(macOS)
        let font = Font.system(size: points(style) * scale, weight: weight, design: design)
        #else
        // The weight is set on the font it is given to: a text style named with a weight drew at the regular one in a widget.
        let font = Font.system(style, design: design).weight(weight)
        #endif
        return role == .time || role == .until || role == .quiet ? font.monospacedDigit() : font
    }

    /// A style's size at the default text size on an iPhone.
    static func points(_ style: Font.TextStyle) -> CGFloat {
        switch style {
        case .headline: 17
        case .subheadline: 15
        case .footnote: 13
        case .caption: 12
        case .caption2: 11
        default: 17
        }
    }
}

extension GlanceGlyph {
    /// The SF Symbol the app draws the kind with.
    var symbol: String {
        switch self {
        case .task: "list.clipboard"
        case .meeting: "calendar"
        case .routine: "repeat"
        case .night: "moon"
        case .moss: "sparkles"
        case .urgent: "flame"
        case .plan: "checklist"
        }
    }
}

extension ShapeStyle where Self == Color {
    static var glanceInk: Color { GlanceLook.ink.color }
    static var glanceSecond: Color { GlanceLook.inkSecond.color }
}

// MARK: What a widget is drawn under

extension EnvironmentValues {
    /// The count to a meeting stands still at the day's own instant: a
    /// picture of a widget, which no clock moves.
    @Entry var glancePaused = false
    /// The system draws the widget in one tint, where a colour says
    /// nothing: a flag is then told by how solid its stripe is.
    @Entry var glanceFlat = false
    /// How much larger than the default the text is drawn where the system
    /// has no text size of its own (`GlanceLook.font`).
    @Entry var glanceScale: CGFloat = 1
}

extension View {
    /// The font of a run of text in a widget.
    func glance(_ role: GlanceLook.Role) -> some View { modifier(GlanceType(role: role)) }
}

private struct GlanceType: ViewModifier {
    @Environment(\.glanceScale) private var scale
    var role: GlanceLook.Role

    func body(content: Content) -> some View { content.font(GlanceLook.font(role, scale: scale)) }
}
