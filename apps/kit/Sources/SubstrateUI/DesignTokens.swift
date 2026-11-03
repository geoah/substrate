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

    public static func menuSymbol(_ symbol: AppMenuSymbol) -> Glyph {
        switch symbol {
        case .today: Glyph("sun.max", "WbSunny")
        case .upcoming: Glyph("calendar", "CalendarMonth")
        case .list: Glyph("checklist", "Checklist")
        case .inbox: Glyph("tray", "Inbox")
        case .chat: Glyph("bubble.left.and.bubble.right", "Forum")
        case .search: Glyph("magnifyingglass", "Search")
        case .records: Glyph("square.stack", "ViewAgenda")
        case .activity: Glyph("waveform.path.ecg", "MonitorHeart")
        case .history: Glyph("clock.arrow.circlepath", "History")
        case .person: Glyph("person.crop.circle", "AccountCircle")
        case .settings: Glyph("gearshape", "Settings")
        }
    }

    /// The marks controls draw beside their words.
    public enum Control: String, CaseIterable, Sendable {
        /// Allowed or done, beside a line of words.
        case check
        /// Something to know.
        case info
        /// A row that opens a screen of its own.
        case disclosure
    }

    public static func control(_ control: Control) -> Glyph {
        switch control {
        case .check: Glyph("checkmark", "Check")
        case .info: Glyph("info.circle", "Info")
        case .disclosure: Glyph("chevron.right", "AutoMirrored.KeyboardArrowRight")
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

public extension ScreenSymbol {
    var systemName: String { DesignTokens.symbol(self).sfSymbol }
}

public extension AppMenuSymbol {
    var systemName: String { DesignTokens.menuSymbol(self).sfSymbol }
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
