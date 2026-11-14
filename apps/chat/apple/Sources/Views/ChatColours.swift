import Foundation
import SubstrateKit
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
    /// icon's purple in both modes. The dark mode's accent is a text tone,
    /// and white on it is 2.8:1.
    static let accentFill = Colour(light: 0x6A3FE0, dark: 0x6A3FE0)
    static let onAccentFill = Colour(light: 0xFFFFFF, dark: 0xFFFFFF)
    /// A prominent button that deletes: a red white reads on, which the
    /// system's red (3.6:1) is not.
    static let destructiveFill = Colour(light: 0xC6281F, dark: 0xC6281F)

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

    // MARK: Every pairing the views draw

    /// Words on a fill, by name. `large` is text of 18 points and larger,
    /// or bold 14 points and larger, which WCAG holds to 3:1.
    struct Pair {
        var name: String
        var text: Colour
        var fill: Colour
        var large = false

        /// The least WCAG lets this pair be.
        var floor: Double { large ? 3 : 4.5 }
    }

    /// The tones a card, a badge and a mark are drawn in.
    static let tones: [ScreenTone] = [.positive, .informative, .caution, .critical]

    static var pairs: [Pair] {
        var out: [Pair] = [
            // The chat list.
            Pair(name: "a row's title on the list", text: label, fill: sidebar),
            Pair(name: "a row's preview and date on the list", text: DesignTokens.quietText, fill: sidebar),
            Pair(name: "an unread row's date on the list", text: accent, fill: sidebar),
            Pair(name: "every word of the selected row, the list focused", text: onSelection, fill: selection),
            Pair(name: "every word of the selected row, the list unfocused", text: label, fill: unfocusedSelection),
            Pair(name: "a stopped chat's mark on the list", text: DesignTokens.quietText, fill: DesignTokens.quietText.over(sidebar, opacity: badge)),
            // The transcript.
            Pair(name: "quiet words on the page: a tool row, a label, a caption", text: DesignTokens.quietText, fill: page),
            Pair(name: "a user's bubble", text: label, fill: accent.over(page, opacity: bubble)),
            Pair(name: "the accent as words on the page: a link, \"and 2 more\"", text: accent, fill: page),
            Pair(name: "quiet words on a card of no tone", text: DesignTokens.quietText, fill: DesignTokens.gray.over(page, opacity: plainCard)),
            Pair(name: "quiet words in a pill: a source's site", text: DesignTokens.quietText, fill: DesignTokens.gray.over(page, opacity: 0.24)),
            Pair(
                name: "an Applied badge on its card", text: DesignTokens.toneText(.positive),
                fill: DesignTokens.green.over(DesignTokens.green.over(page, opacity: card), opacity: badge)),
            Pair(
                name: "a Dismissed badge on its card", text: DesignTokens.quietText,
                fill: DesignTokens.gray.over(DesignTokens.gray.over(page, opacity: plainCard), opacity: badge)),
            Pair(name: "a prominent button's words", text: onAccentFill, fill: accentFill),
            Pair(name: "a deleting button's words", text: onAccentFill, fill: destructiveFill),
        ]
        for tone in tones {
            let card = DesignTokens.tone(tone).over(page, opacity: card)
            out += [
                Pair(
                    name: "a \(tone) mark's words on the list", text: DesignTokens.toneText(tone), fill: DesignTokens.tone(tone).over(sidebar, opacity: badge)),
                Pair(name: "\(tone) words on the page", text: DesignTokens.toneText(tone), fill: page),
                Pair(name: "a title on a \(tone) card", text: label, fill: card),
                Pair(name: "quiet words on a \(tone) card", text: DesignTokens.quietText, fill: card),
                Pair(name: "\(tone) words on a \(tone) card", text: DesignTokens.toneText(tone), fill: card),
                Pair(name: "the accent as words on a \(tone) card: a link", text: accent, fill: card),
            ]
        }
        return out
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

extension ShapeStyle where Self == Color {
    /// The accent as a fill with words on it (`ChatColours.accentFill`).
    static var accentFill: Color { ChatColours.accentFill.color }
    /// A deleting button's fill (`ChatColours.destructiveFill`).
    static var destructiveFill: Color { ChatColours.destructiveFill.color }
}
