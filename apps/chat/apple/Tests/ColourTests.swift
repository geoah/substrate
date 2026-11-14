import SubstrateKit
import SwiftUI
import Testing

@testable import SubstrateChat

#if os(macOS)
import AppKit
#endif

/// Every pairing of words and fill the chat's views draw, by name, held to
/// WCAG's ratio in light and in dark: 4.5:1, and 3:1 for large text. A
/// colour changed to one that does not read fails here.
@MainActor @Suite struct ColourContrast {
    @Test func everyNamedPairReads() {
        for pair in ChatColours.pairs {
            let light = DesignTokens.contrast(pair.text.light, pair.fill.light)
            let dark = DesignTokens.contrast(pair.text.dark, pair.fill.dark)
            #expect(light >= pair.floor, "\(pair.name), light: \(Self.ratio(light))")
            #expect(dark >= pair.floor, "\(pair.name), dark: \(Self.ratio(dark))")
        }
    }

    /// An agent's colour is a hue from its id, so every hue is an agent's:
    /// its name reads on the page, on the list and in the composer, bare
    /// and in its pill.
    @Test func everyAgentsNameReadsWhateverItsHue() {
        for hue in 0 ..< 360 {
            let ink = ChatColours.agentInk(hue: hue)
            let tint = ChatColours.agentTint(hue: hue)
            for background in ChatColours.agentBackgrounds {
                #expect(ink.contrast(on: background) >= 4.5, "hue \(hue) on a background")
                let pill = ChatColours.Colour(
                    light: DesignTokens.blend(tint.light, over: background.light, opacity: ChatColours.agentPill.light),
                    dark: DesignTokens.blend(tint.dark, over: background.dark, opacity: ChatColours.agentPill.dark))
                #expect(ink.contrast(on: pill) >= 4.5, "hue \(hue) in its pill")
            }
        }
    }

    /// The same agent keeps its hue: the id's hash is what every build and
    /// device computes.
    @Test func anAgentsHueIsItsIdsAlone() {
        #expect(ChatColours.hue(of: "substrate.reamde.dev/assistant/moss") == ChatColours.hue(of: "substrate.reamde.dev/assistant/moss"))
        #expect((0 ..< 360).contains(ChatColours.hue(of: "")))
        #expect(ChatColours.hsb(0, 1, 1) == 0xFF0000)
        #expect(ChatColours.hsb(120, 1, 1) == 0x00FF00)
        #expect(ChatColours.hsb(240, 1, 0.5) == 0x000080)
        #expect(ChatColours.hsb(0, 0, 1) == 0xFFFFFF)
    }

    /// Why the pairs are what they are: what the views drew before did not
    /// read. If one of these starts to pass, the note beside its token is
    /// out of date.
    @Test func whatTheTokensReplaceDidNotRead() {
        // The system's secondary label on a white page.
        #expect(DesignTokens.contrast(0x8A8A8E, 0xFFFFFF) < 4.5)
        // White on the dark mode's accent: a selected row's title, a prominent button.
        #expect(DesignTokens.contrast(0xFFFFFF, ChatColours.accent.dark) < 4.5)
        // An agent's own tint as its name, for a green.
        #expect(DesignTokens.contrast(ChatColours.agentTint(hue: 100).light, 0xFFFFFF) < 4.5)
    }

    static func ratio(_ value: Double) -> String { String(format: "%.2f:1", value) }

    #if os(macOS)
    static func srgb(_ color: NSColor, dark: Bool) -> UInt32? {
        var out: UInt32?
        NSAppearance(named: dark ? .darkAqua : .aqua)?.performAsCurrentDrawingAppearance {
            guard let c = color.usingColorSpace(.sRGB) else { return }
            func channel(_ value: CGFloat) -> UInt32 { UInt32((value * 255).rounded()) }
            out = channel(c.redComponent) << 16 | channel(c.greenComponent) << 8 | channel(c.blueComponent)
        }
        return out
    }

    /// `ChatColours.accent` is the asset catalog's AccentColor, the colour
    /// the system draws links, marks and an iPad's selection in.
    @Test func theAccentIsTheAssetCatalogs() throws {
        let asset = try #require(NSColor(named: "AccentColor"))
        #expect(Self.srgb(asset, dark: false) == ChatColours.accent.light)
        #expect(Self.srgb(asset, dark: true) == ChatColours.accent.dark)
    }

    /// A Mac's source list draws the selection in the system's colours:
    /// its own tone of the app's accent while the list has the focus, a
    /// grey while it does not, and white for the primary style on the
    /// first. The named colours are what this system draws, so the pairs
    /// test holds the words to it. A person who picks another accent in
    /// System Settings gets the system's pairing of that one with white.
    @Test func aMacsSelectionColoursAreTheSystems() throws {
        let appsOwnAccent = UserDefaults.standard.object(forKey: "AppleAccentColor") == nil
        for dark in [false, true] {
            let focused = try #require(Self.srgb(.selectedContentBackgroundColor, dark: dark))
            let unfocused = try #require(Self.srgb(.unemphasizedSelectedContentBackgroundColor, dark: dark))
            if appsOwnAccent {
                #expect(focused == (dark ? ChatColours.selection.dark : ChatColours.selection.light), "the focused selection, \(dark ? "dark" : "light")")
            }
            #expect(unfocused == (dark ? ChatColours.unfocusedSelection.dark : ChatColours.unfocusedSelection.light))
            #expect(Self.srgb(.alternateSelectedControlTextColor, dark: dark) == (dark ? ChatColours.onSelection.dark : ChatColours.onSelection.light))
        }
    }
    #endif
}
