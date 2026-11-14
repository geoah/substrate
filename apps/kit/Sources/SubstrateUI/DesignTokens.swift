import Foundation
import SubstrateCore
import SubstrateScreens

/// The look both systems draw the shared screens with (§7.4), as data:
/// each tone's colour, each mark as an SF Symbol and a Material Symbol, the
/// type roles, the spacing and the radii. SwiftUI reads it below; a test
/// writes it as Kotlin for kit-ui (`KitTokens.kt`), so a mark or a colour
/// changes on both in one edit. Apple draws its own system colours, which
/// these are the values of; Android draws these values.
public enum DesignTokens {
    /// An sRGB colour, `0xRRGGBB`, in light and in dark mode.
    public struct Colour: Hashable, Sendable {
        public var light: UInt32
        public var dark: UInt32

        public init(light: UInt32, dark: UInt32) {
            self.light = light
            self.dark = dark
        }

        /// This colour at `opacity` over `base`, in each mode.
        public func over(_ base: Colour, opacity: Double) -> Colour {
            Colour(
                light: DesignTokens.blend(light, over: base.light, opacity: opacity),
                dark: DesignTokens.blend(dark, over: base.dark, opacity: opacity))
        }

        /// The least of the two modes' contrast ratios with `fill`.
        public func contrast(on fill: Colour) -> Double {
            min(DesignTokens.contrast(light, fill.light), DesignTokens.contrast(dark, fill.dark))
        }
    }

    /// One mark on each system: an SF Symbol, and a Material Symbol as
    /// Compose's rounded icons name it (`AutoMirrored.` for one that turns
    /// with the reading direction).
    public struct Glyph: Hashable, Sendable {
        public var sfSymbol: String
        public var material: String

        public init(_ sfSymbol: String, _ material: String) {
            self.sfSymbol = sfSymbol
            self.material = material
        }
    }

    // MARK: Colour

    /// The system colours the tones and the kind hues are drawn in.
    public static let gray = Colour(light: 0x8E8E93, dark: 0x8E8E93)
    public static let brown = Colour(light: 0xA2845E, dark: 0xAC8E68)
    public static let orange = Colour(light: 0xFF9500, dark: 0xFF9F0A)
    public static let yellow = Colour(light: 0xFFCC00, dark: 0xFFD60A)
    public static let green = Colour(light: 0x34C759, dark: 0x30D158)
    public static let teal = Colour(light: 0x30B0C7, dark: 0x40CBE0)
    public static let blue = Colour(light: 0x007AFF, dark: 0x0A84FF)
    public static let purple = Colour(light: 0xAF52DE, dark: 0xBF5AF2)
    public static let pink = Colour(light: 0xFF2D55, dark: 0xFF375F)
    public static let red = Colour(light: 0xFF3B30, dark: 0xFF453A)
    /// Secondary text: what a quiet tone is.
    public static let secondary = Colour(light: 0x8A8A8E, dark: 0x98989F)

    public static func tone(_ tone: ScreenTone) -> Colour {
        switch tone {
        case .quiet: secondary
        case .positive: green
        case .informative: blue
        case .supporting: teal
        case .caution: orange
        case .critical: red
        }
    }

    public static func hue(_ hue: KindHue) -> Colour {
        switch hue {
        case .gray: gray
        case .brown: brown
        case .orange: orange
        case .yellow: yellow
        case .green: green
        case .teal: teal
        case .blue: blue
        case .purple: purple
        case .pink: pink
        case .red: red
        }
    }

    // MARK: Words on fills

    /// The page behind content: white, and black in dark mode.
    public static let page = Colour(light: 0xFFFFFF, dark: 0x000000)
    /// A sidebar's and a grouped screen's background, as an iPad draws it.
    public static let grouped = Colour(light: 0xE9E8ED, dark: 0x1D1C20)

    /// Quiet words that still read: 4.5:1 on the page, on a grouped
    /// background and on a tone's pale fill over either. The system's
    /// secondary label is 3.4:1 on a white page.
    public static let quietText = Colour(light: 0x58585D, dark: 0xB4B4BA)

    /// A tone as words: the tone's own colour is for marks and fills, and
    /// reads as text on neither the page nor its own pale fill (orange on
    /// white is 2.2:1). This is the tone darkened in light mode, and
    /// lightened in dark mode where it needs it, to 4.5:1 on the page, on a
    /// grouped background and on the tone's fill at up to `fillOpacity`
    /// over either.
    public static func toneText(_ tone: ScreenTone) -> Colour {
        switch tone {
        case .quiet: quietText
        case .positive: Colour(light: 0x1D6D31, dark: 0x30D158)
        case .informative: Colour(light: 0x0055B2, dark: 0x369AFF)
        case .supporting: Colour(light: 0x1C6673, dark: 0x40CBE0)
        case .caution: Colour(light: 0x8C5200, dark: 0xFF9F0A)
        case .critical: Colour(light: 0xA82720, dark: 0xFF635A)
        }
    }

    /// The most a tone's colour is laid over a background as a pale fill
    /// with words on it: a badge's, above a card's.
    public static let fillOpacity = 0.18

    /// The WCAG 2 contrast ratio of two opaque sRGB colours, 1 to 21: 4.5
    /// is the least for text, 3 for text of 18 points and larger or bold
    /// 14 points and larger.
    public static func contrast(_ a: UInt32, _ b: UInt32) -> Double {
        let (la, lb) = (luminance(a), luminance(b))
        return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
    }

    /// WCAG 2's relative luminance of an sRGB colour.
    public static func luminance(_ colour: UInt32) -> Double {
        func linear(_ channel: UInt32) -> Double {
            let c = Double(channel & 0xFF) / 255
            return c <= 0.04045 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * linear(colour >> 16) + 0.7152 * linear(colour >> 8) + 0.0722 * linear(colour)
    }

    /// `top` at `opacity` over `bottom`, as a fill is drawn: the colour
    /// words on that fill are read against.
    public static func blend(_ top: UInt32, over bottom: UInt32, opacity: Double) -> UInt32 {
        func channel(_ shift: UInt32) -> UInt32 {
            let (t, b) = (Double((top >> shift) & 0xFF), Double((bottom >> shift) & 0xFF))
            return UInt32((opacity * t + (1 - opacity) * b).rounded()) << shift
        }
        return channel(16) | channel(8) | channel(0)
    }

    // MARK: Marks

    public static func symbol(_ symbol: ScreenSymbol) -> Glyph {
        switch symbol {
        case .serverUpdate: Glyph("arrow.up.circle.fill", "ArrowCircleUp")
        case .warning: Glyph("exclamationmark.triangle.fill", "Warning")
        case .approval: Glyph("checkmark.shield.fill", "VerifiedUser")
        case .clock: Glyph("clock.fill", "Schedule")
        case .ended: Glyph("xmark.circle.fill", "Cancel")
        case .offline: Glyph("wifi.exclamationmark", "WifiOff")
        case .key: Glyph("key.fill", "Key")
        case .server: Glyph("server.rack", "Dns")
        case .found: Glyph("checkmark.circle.fill", "CheckCircle")
        case .accessEnded: Glyph("person.crop.circle.badge.exclamationmark", "NoAccounts")
        case .allowed: Glyph("checkmark.circle.fill", "CheckCircle")
        case .leftOut: Glyph("circle.dashed", "RadioButtonUnchecked")
        case .install: Glyph("arrow.down.circle.fill", "DownloadForOffline")
        case .upgrade: Glyph("arrow.up.circle.fill", "ArrowCircleUp")
        case .create: Glyph("plus.circle.fill", "AddCircle")
        case .webhook: Glyph("bolt.circle.fill", "OfflineBolt")
        case .cannot: Glyph("xmark.circle", "HighlightOff")
        case .upToDate: Glyph("checkmark.circle.fill", "CheckCircle")
        case .sending: Glyph("arrow.triangle.2.circlepath", "Sync")
        case .device: Glyph("iphone", "Smartphone")
        case .problem: Glyph("exclamationmark.circle.fill", "Error")
        }
    }

    /// The menu's and an app's marks by the names its presentations give
    /// them (`AppMenuSection.symbol`); a name not here draws `fallback`.
    public static let menuSymbols: [String: Glyph] = [
        "today": Glyph("calendar", "CalendarToday"),
        "upcoming": Glyph("calendar.badge.clock", "Upcoming"),
        "inbox": Glyph("tray", "Inbox"),
        "list": Glyph("list.bullet", "AutoMirrored.List"),
        "lists": Glyph("checklist", "Checklist"),
        "needs": Glyph("checkmark.circle", "CheckCircle"),
        "chat": Glyph("bubble.left.and.bubble.right", "Forum"),
        "search": Glyph("magnifyingglass", "Search"),
        "records": Glyph("square.stack", "ViewAgenda"),
        "activity": Glyph("waveform.path.ecg", "MonitorHeart"),
        "history": Glyph("clock.arrow.circlepath", "History"),
        "person": Glyph("person.crop.circle", "AccountCircle"),
        "settings": Glyph("gearshape", "Settings"),
    ]

    public static let fallback = Glyph("circle", "Circle")

    public static func menuSymbol(_ name: String) -> Glyph { menuSymbols[name] ?? fallback }

    /// The marks controls draw beside their words.
    public enum Control: String, CaseIterable, Sendable {
        /// Allowed or done, beside a line of words.
        case check
        /// Something to know.
        case info
        /// A row that opens a screen of its own.
        case disclosure
        /// Back to the screen before.
        case back
    }

    public static func control(_ control: Control) -> Glyph {
        switch control {
        case .check: Glyph("checkmark", "Check")
        case .info: Glyph("info.circle", "Info")
        case .disclosure: Glyph("chevron.right", "AutoMirrored.KeyboardArrowRight")
        case .back: Glyph("chevron.backward", "AutoMirrored.ArrowBack")
        }
    }

    /// Each SF Symbol a kind's glyph names (`KindGlyphSpec`), as its
    /// Material Symbol.
    public static let kindSymbols: [String: String] = [
        "checkmark.circle": "CheckCircle", "folder": "Folder", "person": "Person", "at": "AlternateEmail",
        "building.2": "Business", "person.2": "Group", "doc.text": "Description", "calendar": "CalendarMonth",
        "repeat": "Repeat", "envelope": "Mail", "message": "AutoMirrored.Chat", "book": "AutoMirrored.MenuBook",
        "globe": "Public", "tag": "Sell", "gearshape": "Settings", "clock.arrow.circlepath": "History",
        "sparkles": "AutoAwesome", "pencil": "Edit", "shippingbox": "Inventory2",
    ]

    // MARK: Type

    /// What a run of text is on a screen, which each system sets in its own
    /// type scale: a Dynamic Type style, a Material 3 role.
    public enum TextRole: String, CaseIterable, Sendable {
        /// The app's name over the connect screen.
        case appTitle
        /// A screen's own heading: the access ended, a server too old.
        case screenTitle
        /// A card's or a row's title: an issue, a problem, a refused write.
        case heading
        case body
        /// Words under a heading, and a note.
        case secondary
        /// A section's footer, a change's detail.
        case footnote
        /// An identifier a person may copy.
        case identifier
        /// A badge's word.
        case badge
    }

    public struct TextStyle: Hashable, Sendable {
        /// SwiftUI's `Font.TextStyle`, by name.
        public var apple: String
        public var appleWeight: String
        /// Material 3's type role, by its name in `Typography`.
        public var material: String
        public var materialWeight: String?

        public init(apple: String, appleWeight: String = "regular", material: String, materialWeight: String? = nil) {
            self.apple = apple
            self.appleWeight = appleWeight
            self.material = material
            self.materialWeight = materialWeight
        }
    }

    public static func text(_ role: TextRole) -> TextStyle {
        switch role {
        case .appTitle: TextStyle(apple: "largeTitle", appleWeight: "bold", material: "headlineLarge", materialWeight: "SemiBold")
        case .screenTitle: TextStyle(apple: "title2", appleWeight: "semibold", material: "headlineSmall")
        case .heading: TextStyle(apple: "subheadline", appleWeight: "semibold", material: "titleSmall")
        case .body: TextStyle(apple: "body", material: "bodyLarge")
        case .secondary: TextStyle(apple: "subheadline", material: "bodyMedium")
        case .footnote: TextStyle(apple: "footnote", material: "bodySmall")
        case .identifier: TextStyle(apple: "caption", material: "labelMedium")
        case .badge: TextStyle(apple: "caption", appleWeight: "semibold", material: "labelSmall")
        }
    }

    // MARK: Space

    /// Points on Apple systems, density-independent pixels on Android.
    public enum Spacing: Int, CaseIterable, Sendable {
        case hairline = 2
        case tight = 4
        case small = 6
        case medium = 8
        case row = 10
        case card = 14
        case content = 16
        case section = 20
        case screen = 24
        case block = 28
    }

    public enum Radius: Int, CaseIterable, Sendable {
        case control = 10
        case field = 14
        case card = 16
    }
}

#if canImport(SwiftUI)
import SwiftUI
#if canImport(UIKit)
import UIKit
#else
import AppKit
#endif

// The presentations' marks and tones as SwiftUI draws them; nothing else in
// the renderers names a colour or a symbol.

public extension ScreenTone {
    var color: Color {
        switch self {
        case .quiet: .secondary
        case .positive: .green
        case .informative: .blue
        case .supporting: .teal
        case .caution: .orange
        case .critical: .red
        }
    }
}

public extension ScreenTone {
    /// The tone as words (`DesignTokens.toneText`).
    var textColor: Color { DesignTokens.toneText(self).color }
}

public extension DesignTokens.Colour {
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

public extension ShapeStyle where Self == Color {
    /// Quiet words that still read (`DesignTokens.quietText`).
    static var quietText: Color { DesignTokens.quietText.color }
}

public extension ScreenSymbol {
    var systemName: String { DesignTokens.symbol(self).sfSymbol }
}

public extension DesignTokens.Control {
    var systemName: String { DesignTokens.control(self).sfSymbol }
}

extension DesignTokens.Spacing {
    var points: CGFloat { CGFloat(rawValue) }
}

extension DesignTokens.Radius {
    var points: CGFloat { CGFloat(rawValue) }
}
#endif
