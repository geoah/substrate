#if canImport(SwiftUI)
import Foundation
import SubstrateScreens
import SubstrateUI
import SwiftUI

/// The colours Chat's own views choose, named in one place by what each is
/// for, as light and dark sRGB values: words, and the fills words are laid
/// on. `pairs` lists the pairings the list, the transcript and the cards
/// draw from these names, and the app's tests hold each to WCAG's ratio
/// (`ColourContrast`), so a value changed to one that does not read fails
/// by name. What another app would share is the kit's
/// (`DesignTokens`: quiet text, a tone as words, the page).
enum ChatColours {
    typealias Colour = DesignTokens.Colour

    // MARK: The accent

    /// The accent as words and marks on the page: app.json's `accent.text`
    /// and the asset catalog's AccentColor, which a test holds this to.
    static let accent = Colour(light: 0x6A3FE0, dark: 0xA286FF)
    /// The accent as a fill with words on it, a prominent button's: the
    /// icon's purple in both modes, which the kit's `Color.accentFill`
    /// draws. The dark mode's accent is a text tone, and white on it is
    /// 2.8:1.
    static let accentFill = Colour(light: 0x6A3FE0, dark: 0x6A3FE0)
    /// A prominent button that deletes: a red white reads on, which the
    /// system's red (3.6:1) is not.
    static let destructiveFill = DesignTokens.toneFill(.critical)

    // MARK: The chat list

    /// The fill behind the selected chat while the list has the focus. An
    /// iPad fills it with the list's tint, which the list sets to
    /// `accentFill`. A Mac's source list fills it with the system's own
    /// tone of the app's accent (`selectedContentBackgroundColor`): these
    /// are what it made of this app's accent when they were read, and a
    /// test holds the row's words to whatever the system makes of it.
    #if os(macOS)
    static let selection = Colour(light: 0x552CC4, dark: 0x7D63D1)
    #else
    static let selection = accentFill
    #endif
    /// Every word on that fill, the title as much as the preview and the
    /// date: white, which is what the system makes of the primary style
    /// there.
    static let onSelection = Colour(light: 0xFFFFFF, dark: 0xFFFFFF)
    /// The grey behind the selected chat while the focus is elsewhere (the
    /// composer, another window): an iPad's as measured, a Mac's
    /// `unemphasizedSelectedContentBackgroundColor`.
    #if os(macOS)
    static let unfocusedSelection = Colour(light: 0xDCDCDC, dark: 0x464646)
    #else
    static let unfocusedSelection = Colour(light: 0xD5D4D9, dark: 0x39393D)
    #endif
    /// The system's label colour: a title, and every word of the selected
    /// row while its list is not focused.
    static let label = Colour(light: 0x000000, dark: 0xFFFFFF)
    /// The page behind a conversation: the kit's page, and a Mac's window.
    #if os(macOS)
    static let page = Colour(light: 0xFFFFFF, dark: 0x1E1E1E)
    #else
    static let page = DesignTokens.page
    #endif
    /// The list's own background.
    static let sidebar = DesignTokens.grouped
    /// The composer's field, glass over the page, as measured: what an
    /// agent's pill sits on in a new chat.
    static let composerField = Colour(light: 0xFDFDFD, dark: 0x202020)
    /// A row of Settings, where each agent has its pill.
    static let settingsRow = Colour(light: 0xFFFFFF, dark: 0x2C2C2E)

    // MARK: How much of a colour a fill is

    /// A user's bubble, of the accent.
    static let bubble = 0.16
    /// A card of a tone, and one of no tone (grey).
    static let card = 0.1
    static let plainCard = 0.08
    /// A badge's and a mark's pale fill of its own tone.
    static let badge = 0.15
    /// A card's edge, of its tone.
    static let edge = 0.18
    /// Inline code's tint, of grey.
    static let inlineCode = 0.14
    /// An agent's pill, of the agent's colour.
    static let agentPill = (light: 0.13, dark: 0.22)

    // MARK: An agent's colour

    /// A hue from the id alone: the same agent is the same colour on every
    /// device, with nothing stored.
    static func hue(of id: String) -> Int {
        var h: UInt64 = 1_469_598_103_934_665_603
        for byte in id.utf8 { h = (h ^ UInt64(byte)) &* 1_099_511_628_211 }
        return Int(h % 360)
    }

    /// The agent's colour as a pill's fill and a mark: the hue at one
    /// saturation and brightness per mode.
    static func agentTint(hue: Int) -> Colour {
        Colour(light: hsb(hue, 0.7, 0.62), dark: hsb(hue, 0.5, 0.95))
    }

    /// The agent's colour as words: its name in the list, and in its pill.
    /// The tint itself is 2.7:1 on white for a yellow or a green, so the
    /// brightness comes down (and in dark mode the saturation) until the
    /// words hold 4.5:1 on each of `agentBackgrounds` and on the pill's
    /// own fill over each.
    static func agentInk(hue: Int) -> Colour {
        let tint = agentTint(hue: hue)
        func reads(_ ink: UInt32, _ mode: KeyPath<Colour, UInt32>, _ opacity: Double) -> Bool {
            agentBackgrounds.allSatisfy { background in
                let base = background[keyPath: mode]
                let fill = DesignTokens.blend(tint[keyPath: mode], over: base, opacity: opacity)
                return DesignTokens.contrast(ink, base) >= inkRatio && DesignTokens.contrast(ink, fill) >= inkRatio
            }
        }
        var light = tint.light
        var brightness = 0.62
        while !reads(light, \.light, agentPill.light), brightness > 0 {
            brightness -= 0.01
            light = hsb(hue, 0.7, brightness)
        }
        var dark = tint.dark
        var saturation = 0.5
        while !reads(dark, \.dark, agentPill.dark), saturation > 0 {
            saturation -= 0.01
            dark = hsb(hue, saturation, 0.95)
        }
        return Colour(light: light, dark: dark)
    }

    /// Where an agent's name and its pill are drawn: a conversation, the
    /// chat list, the composer of a new chat, and Settings.
    static let agentBackgrounds = [page, sidebar, composerField, settingsRow]

    /// What `agentInk` aims for: a little over 4.5, so a rounded channel
    /// never lands under it.
    private static let inkRatio = 4.6

    /// An sRGB colour from a hue in degrees and a saturation and brightness
    /// of 0 to 1.
    static func hsb(_ hue: Int, _ saturation: Double, _ brightness: Double) -> UInt32 {
        let h = Double(((hue % 360) + 360) % 360) / 60
        let c = brightness * saturation
        let x = c * (1 - abs(h.truncatingRemainder(dividingBy: 2) - 1))
        let m = brightness - c
        let (r, g, b): (Double, Double, Double) =
            switch Int(h) {
            case 0: (c, x, 0)
            case 1: (x, c, 0)
            case 2: (0, c, x)
            case 3: (0, x, c)
            case 4: (x, 0, c)
            default: (c, 0, x)
            }
        func channel(_ value: Double) -> UInt32 { UInt32(((value + m) * 255).rounded()) }
        return channel(r) << 16 | channel(g) << 8 | channel(b)
    }

    // MARK: What words are drawn in, and on

    /// What a view draws words and marks in. A view names one of these and
    /// the surface it stands on (`ink(_:on:)`), never a colour.
    enum Ink: Hashable, Sendable {
        /// The system's label colour: what words are in unless they say
        /// otherwise.
        case label
        /// Quiet words that still read: a preview, a caption, a note.
        case quiet
        /// The accent as words and marks: a link, a chosen option.
        case accent
        /// A tone as words: failed, held, done.
        case tone(ScreenTone)
        /// A tone as a mark that says so alone: a check, a cross, an
        /// hourglass. Seen at 3:1, where words are read at 4.5:1.
        case mark(ScreenTone)
        /// White, on a fill made for it.
        case onFill
        /// A mark that says nothing its row does not say in words: a
        /// chevron, a globe before a site's name. Held to no ratio.
        case faint

        var colour: Colour? {
            switch self {
            case .label: ChatColours.label
            case .quiet: DesignTokens.quietText
            case .accent: ChatColours.accent
            case let .tone(tone): DesignTokens.toneText(tone)
            case let .mark(tone): DesignTokens.toneMark(tone)
            case .onFill: DesignTokens.onFill
            case .faint: nil
            }
        }

        /// A mark and no words: WCAG holds it to 3:1.
        var isMark: Bool { if case .mark = self { true } else { false } }
    }

    /// What words are drawn on.
    enum Surface: String, Hashable, Sendable, CaseIterable {
        /// A conversation's page, and a screen of the app's own.
        case page
        /// The chat list.
        case list
        /// A row of a grouped form: Settings.
        case form
        /// The composer's field, and the lines over it.
        case field
        /// A card of any tone, or of none.
        case card
        /// A box inside a card: what a call would run with.
        case box
        /// The person's own message.
        case bubble
        /// A block of code, and a table.
        case code
        case table
        /// A pill and a chip: a page a reply cites, an opener, a tag.
        case pill
        /// A badge of its own tone, on a card and on the list.
        case badge
        /// A fill made for white words: a prominent button's, a round
        /// button's on glass.
        case fill

        /// Every fill a view on this surface may have behind its words, by
        /// name.
        var fills: [(name: String, colour: Colour)] {
            let cards = ChatColours.cardTones.map { tone in
                (name: "a \(tone.map { "\($0)" } ?? "plain") card", colour: ChatColours.cardFill(tone))
            }
            switch self {
            case .page: return [("the page", ChatColours.page)]
            case .list: return [("the list", ChatColours.sidebar)]
            case .form: return [("a form's row", ChatColours.settingsRow)]
            case .field: return [("the composer's field", ChatColours.composerField)]
            case .card: return cards
            case .box: return cards.map { ("a box on \($0.name)", DesignTokens.gray.over($0.colour, opacity: ChatColours.plainCard)) }
            case .bubble: return [("a bubble", ChatColours.accent.over(ChatColours.page, opacity: ChatColours.bubble))]
            case .code: return [("a code block", ChatColours.codeFill)]
            case .table: return [("a table", ChatColours.tableFill)]
            case .pill:
                return ([("the page", ChatColours.page), ("the list", ChatColours.sidebar)] + cards).map {
                    ("a pill on \($0.name)", ChatColours.pillFill(over: $0.colour))
                }
            // A badge's fill is its ink's tone: `pairs` makes it.
            case .badge: return []
            case .fill:
                return [("the accent as a fill", ChatColours.accentFill), ("a deleting button's fill", ChatColours.destructiveFill)]
                    + [ScreenTone.caution, .quiet].map { ("a \($0) fill", DesignTokens.toneFill($0)) }
            }
        }
    }

    /// The tones a card is drawn in; nil is a card of no tone.
    static let cardTones: [ScreenTone?] = [nil, .positive, .informative, .caution, .critical]

    static func cardFill(_ tone: ScreenTone?) -> Colour {
        tone.map { DesignTokens.tone($0).over(page, opacity: card) } ?? DesignTokens.gray.over(page, opacity: plainCard)
    }

    /// A pill's fill, the system's tertiary fill, as it lies over `base`.
    static func pillFill(over base: Colour) -> Colour {
        let grey = DesignTokens.Colour(light: 0x767680, dark: 0x767680)
        return Colour(
            light: DesignTokens.blend(grey.light, over: base.light, opacity: 0.12), dark: DesignTokens.blend(grey.dark, over: base.dark, opacity: 0.24))
    }

    /// A badge of `tone` as it lies over `base`.
    static func badgeFill(_ tone: ScreenTone, over base: Colour) -> Colour {
        (tone == .quiet ? DesignTokens.gray : DesignTokens.tone(tone)).over(base, opacity: badge)
    }

    /// Words of one ink on one surface, as a view names them.
    struct Use: Hashable, Sendable {
        var ink: Ink
        var surface: Surface

        init(_ ink: Ink, on surface: Surface) {
            self.ink = ink
            self.surface = surface
        }
    }

    // MARK: Every pairing the views draw

    /// Words on a fill, by name. `large` is text of 18 points and larger,
    /// or bold 14 points and larger, and a mark, which WCAG holds to 3:1.
    struct Pair {
        var name: String
        var text: Colour
        var fill: Colour
        var large = false

        /// The least WCAG lets this pair be.
        var floor: Double { large ? 3 : 4.5 }
    }

    /// Every ink a view names on every surface it names it on: what the
    /// source says, exactly. A test reads the views and holds this to them
    /// both ways: a view that draws a pairing not here fails, and so does a
    /// pairing here that no view draws.
    static let named: Set<Use> = {
        var out: Set<Use> = []
        func list(_ ink: Ink, on surfaces: Surface...) { for surface in surfaces { out.insert(Use(ink, on: surface)) } }
        list(.quiet, on: .page, .list, .form, .field, .card, .box, .code, .table, .pill)
        // A link and its mark, a chosen option, a word a search found, the
        // unread date, "New", a switch that is on.
        list(.accent, on: .page, .list, .field, .card, .code, .table)
        // A status in words: held, failed, a problem.
        list(.tone(.critical), on: .page, .card, .form, .field, .badge)
        // A helper's run that stopped, beside its name on its card.
        list(.tone(.caution), on: .page, .form, .badge, .card)
        list(.tone(.positive), on: .badge)
        list(.tone(.quiet), on: .badge)
        // And as its mark: done, failed, held, declined, waiting on you,
        // a key that is missing.
        // On a card: a call to another assistant, its own panel.
        list(.mark(.positive), on: .page, .card)
        list(.mark(.critical), on: .page, .card)
        list(.mark(.caution), on: .page, .list, .card)
        list(.mark(.quiet), on: .page)
        list(.onFill, on: .fill)
        list(.faint, on: .page, .pill)
        return out
    }()

    /// `named`, and the label colour on every surface: what words are in
    /// where a view names no ink. `pairs` is each of these on every fill
    /// its surface has.
    static let uses: Set<Use> = named.union(Surface.allCases.filter { $0 != .fill && $0 != .badge }.map { Use(.label, on: $0) })

    /// What a badge of `tone` lies on: Applied on the card of a change
    /// that was, Dismissed on a plain one, and a chat's mark ("Didn’t
    /// finish", "Stopped", "Failed") on the list.
    static func badgeBases(_ tone: ScreenTone) -> [(name: String, colour: Colour)] {
        switch tone {
        case .positive: [("its card", cardFill(.positive))]
        case .quiet: [("a plain card", cardFill(nil)), ("the list", sidebar)]
        default: [("the list", sidebar)]
        }
    }

    /// The tones a card, a badge and a mark are drawn in.
    static let tones: [ScreenTone] = [.positive, .informative, .caution, .critical]

    static var pairs: [Pair] {
        var out: [Pair] = [
            // The selected row is the system's to fill, and its words the
            // system's primary style on it.
            Pair(name: "every word of the selected row, the list focused", text: onSelection, fill: selection),
            Pair(name: "every word of the selected row, the list unfocused", text: label, fill: unfocusedSelection),
        ]
        for use in uses.sorted(by: { "\($0)" < "\($1)" }) {
            guard let text = use.ink.colour else { continue }
            if use.surface == .badge, case let .tone(tone) = use.ink {
                out += badgeBases(tone).map { Pair(name: "a \(tone) badge's word on \($0.name)", text: text, fill: badgeFill(tone, over: $0.colour)) }
                continue
            }
            out += use.surface.fills.map { Pair(name: "\(use.ink) on \($0.name)", text: text, fill: $0.colour, large: use.ink.isMark) }
        }
        // Inline code's tint under a line's words, on the surfaces a reply
        // is drawn on.
        for surface in [Surface.page, .card, .table] {
            out += surface.fills.map {
                Pair(name: "inline code on \($0.name)", text: label, fill: DesignTokens.gray.over($0.colour, opacity: inlineCode))
            }
        }
        // A reply's code (`CodeColours.swift`).
        return out + codePairs
    }
}

extension ChatColours {
    /// The chat list's tint, which an iPad fills the focused selection
    /// with: `accentFill`. With Increase Contrast on, an iPad draws a tint
    /// lighter in dark mode (by about 30 in each channel, as measured:
    /// `accentFill` became #885CFF, which white is 4.2:1 on), so there the
    /// tint starts that much darker and is drawn as `accentFill`.
    #if os(iOS)
    static let selectionTint = Color(
        uiColor: UIColor { traits in
            let raised = traits.userInterfaceStyle == .dark && traits.accessibilityContrast == .high
            let value: UInt32 = raised ? 0x4C21C2 : accentFill.dark
            return UIColor(
                red: CGFloat((value >> 16) & 0xFF) / 255, green: CGFloat((value >> 8) & 0xFF) / 255, blue: CGFloat(value & 0xFF) / 255, alpha: 1)
        })
    #else
    static let selectionTint = accentFill.color
    #endif
}

extension ChatColours.Ink {
    /// The ink as SwiftUI draws it. Each is made once: a colour made at
    /// each draw is another colour to everything that compares them.
    var style: AnyShapeStyle {
        switch self {
        case .label: Self.styles.label
        case .quiet: Self.styles.quiet
        case .accent: Self.styles.accent
        case let .tone(tone): Self.styles.tones[tone] ?? Self.styles.quiet
        case let .mark(tone): Self.styles.marks[tone] ?? Self.styles.quiet
        case .onFill: Self.styles.onFill
        case .faint: Self.styles.faint
        }
    }

    /// The ink as a colour, where a style will not do (a tint, an
    /// attribute of a text).
    var color: Color {
        switch self {
        case .label, .faint: .primary
        case .quiet: Self.colors.quiet
        case .accent: Self.colors.accent
        case let .tone(tone): Self.colors.tones[tone] ?? Self.colors.quiet
        case let .mark(tone): Self.colors.marks[tone] ?? Self.colors.quiet
        case .onFill: Self.colors.onFill
        }
    }

    private static let colors = (
        quiet: DesignTokens.quietText.color, accent: ChatColours.accent.color, onFill: DesignTokens.onFill.color,
        tones: Dictionary(uniqueKeysWithValues: ScreenTone.allCases.map { ($0, DesignTokens.toneText($0).color) }),
        marks: Dictionary(uniqueKeysWithValues: ScreenTone.allCases.map { ($0, DesignTokens.toneMark($0).color) })
    )
    private static let styles = (
        label: AnyShapeStyle(.primary), quiet: AnyShapeStyle(colors.quiet), accent: AnyShapeStyle(colors.accent),
        onFill: AnyShapeStyle(colors.onFill), faint: AnyShapeStyle(.tertiary),
        tones: colors.tones.mapValues { AnyShapeStyle($0) }, marks: colors.marks.mapValues { AnyShapeStyle($0) }
    )
}

extension View {
    /// Draws this view's words and marks in `ink`. `surface` is what they
    /// stand on, each surface where the view is drawn on several: it draws
    /// nothing, and is what the tests hold to `ChatColours.uses`, so no
    /// ink is put on a fill it was not checked against.
    func ink(_ ink: ChatColours.Ink, on surface: ChatColours.Surface...) -> some View { foregroundStyle(ink.style) }

    /// The same for a row of a list, which draws every word in the
    /// system's primary style while it is the selection: the system makes
    /// that white on the focused selection's fill and the label colour on
    /// the unfocused one's.
    func ink(_ ink: ChatColours.Ink, on surface: ChatColours.Surface, selected: Bool) -> some View {
        foregroundStyle(selected ? ChatColours.Ink.label.style : ink.style)
    }
}

extension View {
    /// Tints the controls under this view with `ink`: a button whose words
    /// are the accent, on `surface`.
    func tinted(_ ink: ChatColours.Ink, on surface: ChatColours.Surface...) -> some View { tint(ink.color) }
}

extension Text {
    func ink(_ ink: ChatColours.Ink, on surface: ChatColours.Surface...) -> Text { foregroundStyle(ink.style) }

    func ink(_ ink: ChatColours.Ink, on surface: ChatColours.Surface, selected: Bool) -> Text {
        foregroundStyle(selected ? ChatColours.Ink.label.style : ink.style)
    }
}

extension ChatColours {
    // MARK: Fills and lines, as SwiftUI draws them

    /// A card's fill and its edge, of a tone or of none.
    static func cardStyle(_ tone: ScreenTone?) -> Color { (tone?.color ?? neutral).opacity(tone == nil ? plainCard : card) }
    static func cardEdge(_ tone: ScreenTone?) -> Color { (tone?.color ?? neutral).opacity(edge) }
    /// The grey a fill of no tone is made of.
    static let neutral = Color.secondary
    /// A box inside a card.
    static let boxStyle = neutral.opacity(plainCard)
    /// The person's own message.
    static let bubbleStyle = accent.color.opacity(bubble)
    /// A pill's and a chip's fill: the kit's record pill's.
    static var pillStyle: some ShapeStyle { RecordPillStyle.fill }
    /// A badge's fill, of its tone.
    static func badgeStyle(_ tone: ScreenTone) -> Color { (tone == .quiet ? neutral : tone.color).opacity(badge) }
    /// Inline code's tint under its words.
    static let inlineCodeStyle = neutral.opacity(inlineCode)
    /// A rule and a rail: a line that carries no words.
    static var ruleStyle: some ShapeStyle { .quaternary }
    /// A rail beside quoted text, which must be seen to say so.
    static var railStyle: some ShapeStyle { .tertiary }
    /// A code block's edge and the line under its bar; a table's rule
    /// under its header, between its rows and beside its held column.
    static let codeEdge = neutral.opacity(0.15)
    static let tableRule = (header: neutral.opacity(0.35), row: neutral.opacity(0.18), held: neutral.opacity(0.3))
    /// The rule either side of "New", of the accent.
    static let newRule = accent.color.opacity(0.5)
    /// What shows of text in a window whose older lines go out at its
    /// top: a mask, so its colours are how much shows.
    static let fadesFromTop = LinearGradient(
        stops: [.init(color: .clear, location: 0), .init(color: .black, location: 0.45)], startPoint: .top, endPoint: .bottom)
    /// The light that passes over words that wait.
    static let shimmer = Color.white.opacity(0.55)
    /// A dot that is not the lit one, and one that pulses, at its dimmest.
    static let dimmed = 0.35
    /// A message still being sent, and a control while it is pressed.
    static let pending = 0.7
    /// The outline of a mark on the selected row, of the row's own style.
    static let outline = 0.6
}

extension ShapeStyle where Self == Color {
    /// A deleting button's fill (`ChatColours.destructiveFill`).
    static var destructiveFill: Color { ChatColours.destructiveFill.color }
}
#endif
