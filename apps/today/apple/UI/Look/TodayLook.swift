import SubstrateUI
import SwiftUI

/// The design guide's values (proposal 098, `DESIGN-GUIDE.md`), on the
/// kit's tokens where the kit has one and as the guide's own where it has
/// none: the three flag colours, the assistant's hue, the urgent red, and
/// the six grounds. Nothing else in the app names a colour, a size or a
/// font. The guide draws light mode alone; the dark values here keep its
/// rules (words at 4.5:1 on their fill, the three flags apart in lightness
/// as well as hue).
enum TodayLook {
    typealias Colour = DesignTokens.Colour

    // MARK: Colour

    /// Text, a project's tile, the mark of a chosen option.
    static let ink = Colour(light: 0x16181B, dark: 0xF2F3F5)
    /// Second lines, kind glyphs, neutral pills, section labels and times:
    /// quiet words that still read at 4.5:1 on a card and on every ground.
    static let inkSecond = Colour(light: 0x565C66, dark: 0xAEB3BC)
    /// Marks alone: chevrons, an empty option's ring. Never words.
    static let inkQuiet = Colour(light: 0x858B95, dark: 0x7E848E)
    static let card = Colour(light: 0xFFFFFF, dark: 0x1F2128)
    /// Secondary buttons, chips, neutral pills.
    static let fillQuiet = Colour(light: 0xF3F4F6, dark: 0x2A2D35)
    /// The primary button, a ticked box, the mark of a chosen option.
    static let action = Colour(light: 0x1E2024, dark: 0xECEDF0)
    static let onAction = Colour(light: 0xFFFFFF, dark: 0x15171A)

    /// The assistant's hue: the outline of its box and of what it suggests.
    static let moss = Colour(light: 0x2F6B4F, dark: 0x3F8E69)
    static let mossWords = Colour(light: 0x245A41, dark: 0x8FD0AE)
    static let mossFill = Colour(light: 0xE2EFE7, dark: 0x1D3329)

    static let urgent = Colour(light: 0xE4572E, dark: 0xF0714B)
    static let urgentWords = Colour(light: 0xB23A16, dark: 0xFF9A7A)
    static let urgentFill = Colour(light: 0xFDE8E0, dark: 0x3B2018)

    static let cautionWords = Colour(light: 0x8A5A0A, dark: 0xE9BE6A)
    static let cautionFill = Colour(light: 0xFBEFD3, dark: 0x3A2E14)

    /// The three flags. In light mode High is the darkest and Low the
    /// palest; on a dark card the order turns, High the brightest and Low
    /// the dimmest, each a clear step from the next and Low still 3:1 on
    /// the card.
    static let flagHigh = Colour(light: 0xD63E18, dark: 0xFF8259)
    static let flagMedium = Colour(light: 0xE7A52A, dark: 0xB8862A)
    static let flagLow = Colour(light: 0xB1BBCA, dark: 0x6B7686)

    /// A surface over the ground that is not a card: a list of rows, a
    /// closed line.
    static func soft(_ scheme: ColorScheme) -> Color { scheme == .dark ? Color.white.opacity(0.07) : Color.white.opacity(0.55) }
    /// The strip and the bar at the bottom, where glass is not drawn.
    static func glass(_ scheme: ColorScheme) -> Color {
        scheme == .dark ? Color(red: 34 / 255, green: 36 / 255, blue: 44 / 255).opacity(0.78) : Color.white.opacity(0.74)
    }
    static func line(_ scheme: ColorScheme) -> Color { scheme == .dark ? Color.white.opacity(0.10) : ink.color.opacity(0.09) }
    /// The bed of an option list.
    static func bed(_ scheme: ColorScheme) -> Color { scheme == .dark ? Color.white.opacity(0.07) : ink.color.opacity(0.06) }

    // MARK: Sizes

    /// From the edge of the screen to a card.
    static let side: CGFloat = 14
    /// From a card's edge to what is in it.
    static let inset: CGFloat = DesignTokens.Spacing.content.points
    /// Between one card and the next, and above and below a row's text.
    static let gap: CGFloat = DesignTokens.Spacing.row.points
    /// Between a glyph, tile or badge and the text beside it.
    static let lead: CGFloat = DesignTokens.Spacing.group.points
    static let betweenButtons: CGFloat = DesignTokens.Spacing.medium.points
    static let betweenChips: CGFloat = DesignTokens.Spacing.small.points

    static let cardRadius: CGFloat = 26
    static let listRadius: CGFloat = 22
    static let bedRadius: CGFloat = DesignTokens.Radius.card.points
    static let optionRadius: CGFloat = 12
    static let chipRadius: CGFloat = 9

    static let target: CGFloat = DesignTokens.touchTarget
    /// The box a kind glyph, a tile or a badge leads a row in.
    static let leadBox: CGFloat = 26
    /// The least room a clock time takes before a line's words.
    static let timeColumn: CGFloat = 44
    static let stripeWidth: CGFloat = 4
    static let optionMark: CGFloat = 22
    /// The widest the pane's one column is drawn, on a wide screen.
    static let column: CGFloat = 560

    // MARK: Type

    /// A Dynamic Type style, so text grows with the text size. A Mac has no
    /// text size of its own and its styles are a desktop's, far smaller:
    /// there the pane is drawn at the sizes an iPhone draws by default, so
    /// it is one design on every screen.
    static func font(_ style: Font.TextStyle, weight: Font.Weight = .regular, design: Font.Design = .default) -> Font {
        #if os(macOS)
        Font.system(size: points(style), weight: weight, design: design)
        #else
        Font.system(style, design: design, weight: weight)
        #endif
    }

    /// A style's size at the default text size on an iPhone.
    static func points(_ style: Font.TextStyle) -> CGFloat {
        switch style {
        case .largeTitle: 34
        case .title: 28
        case .title2: 22
        case .title3: 20
        case .headline, .body: 17
        case .callout: 16
        case .subheadline: 15
        case .footnote: 13
        case .caption: 12
        case .caption2: 11
        @unknown default: 17
        }
    }

    /// What a run of text is in the pane, each a Dynamic Type style so it
    /// grows with the text size.
    enum Text {
        static let pageTitle = TodayLook.font(.title, weight: .bold)
        static let leadTitle = TodayLook.font(.title2, weight: .bold)
        static let openTitle = TodayLook.font(.title3, weight: .semibold)
        static let rowTitle = TodayLook.font(.callout, weight: .semibold)
        static let body = TodayLook.font(.subheadline)
        static let bodyStrong = TodayLook.font(.subheadline, weight: .medium)
        static let second = TodayLook.font(.subheadline)
        static let footnote = TodayLook.font(.footnote)
        static let sectionLabel = TodayLook.font(.footnote, weight: .semibold)
        static let button = TodayLook.font(.subheadline, weight: .semibold)
        static let wideButton = TodayLook.font(.headline, weight: .semibold)
        static let pill = TodayLook.font(.caption, weight: .semibold)
        static let time = TodayLook.font(.subheadline, weight: .semibold, design: .rounded).monospacedDigit()
        static let stripEntry = TodayLook.font(.footnote, weight: .semibold)
        static let stripSecond = TodayLook.font(.caption, weight: .medium)
        static let option = TodayLook.font(.subheadline, weight: .medium)
        static let optionChosen = TodayLook.font(.subheadline, weight: .semibold)
        static let field = TodayLook.font(.body)
    }
}

extension ShapeStyle where Self == Color {
    static var ink: Color { TodayLook.ink.color }
    static var inkSecond: Color { TodayLook.inkSecond.color }
    static var inkQuiet: Color { TodayLook.inkQuiet.color }
    static var card: Color { TodayLook.card.color }
    static var fillQuiet: Color { TodayLook.fillQuiet.color }
    static var action: Color { TodayLook.action.color }
    static var onAction: Color { TodayLook.onAction.color }
    static var moss: Color { TodayLook.moss.color }
    static var mossWords: Color { TodayLook.mossWords.color }
    static var mossFill: Color { TodayLook.mossFill.color }
    static var urgent: Color { TodayLook.urgent.color }
    static var urgentWords: Color { TodayLook.urgentWords.color }
    static var urgentFill: Color { TodayLook.urgentFill.color }
    static var cautionWords: Color { TodayLook.cautionWords.color }
    static var cautionFill: Color { TodayLook.cautionFill.color }
}

/// A card's shadow: soft under a card on a ground. A surface that is a card
/// only while it is open keeps the shadow and draws it clear while closed
/// (`shown`), so it comes and goes with the card.
struct CardShadow: ViewModifier {
    @Environment(\.colorScheme) private var scheme
    var small = false
    var shown = true

    func body(content: Content) -> some View {
        content
            .shadow(color: .black.opacity(shown ? (scheme == .dark ? 0.30 : 0.04) : 0), radius: shown ? 0.5 : 0, y: shown ? 1 : 0)
            .shadow(
                color: .black.opacity(shown ? (scheme == .dark ? 0.36 : (small ? 0.07 : 0.10)) : 0), radius: shown ? (small ? 7 : 14) : 0,
                y: shown ? (small ? 4 : 10) : 0)
    }
}

extension View {
    func cardShadow(small: Bool = false, shown: Bool = true) -> some View { modifier(CardShadow(small: small, shown: shown)) }
}
