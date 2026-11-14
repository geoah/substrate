import Foundation

/// The colours a failure is drawn in, as values: each tone's fill, the ink
/// its words are in, and its button, for light and for dark. They are
/// opaque, so what is under the strip above the composer never changes how
/// its words read, and they are numbers, so a test holds every pair that
/// carries text to 4.5:1.
public struct FailurePalette: Hashable, Sendable {
    /// One colour, each channel from 0 to 1.
    public struct Shade: Hashable, Sendable {
        public var red: Double
        public var green: Double
        public var blue: Double

        init(_ hex: UInt32) {
            red = Double((hex >> 16) & 0xFF) / 255
            green = Double((hex >> 8) & 0xFF) / 255
            blue = Double(hex & 0xFF) / 255
        }

        /// The relative luminance WCAG 2 defines.
        public var luminance: Double {
            func linear(_ channel: Double) -> Double {
                channel <= 0.03928 ? channel / 12.92 : pow((channel + 0.055) / 1.055, 2.4)
            }
            return 0.2126 * linear(red) + 0.7152 * linear(green) + 0.0722 * linear(blue)
        }

        /// WCAG's contrast ratio of two colours, from 1 to 21.
        public func contrast(with other: Shade) -> Double {
            let lighter = max(luminance, other.luminance)
            let darker = min(luminance, other.luminance)
            return (lighter + 0.05) / (darker + 0.05)
        }
    }

    /// What the card or the strip is filled with, and its hairline.
    public var fill: Shade
    public var edge: Shade
    /// The sentence.
    public var ink: Shade
    /// The note, the details and a button that cannot be pressed now.
    public var soft: Shade
    /// The symbol, the Details disclosure and the button's own fill.
    public var accent: Shade
    /// The button's title, on `accent`.
    public var onAccent: Shade
    /// A button that cannot be pressed now: its fill, under `soft`.
    public var idle: Shade

    public static func of(_ tone: FailurePresentation.Tone, dark: Bool) -> FailurePalette {
        switch (tone, dark) {
        case (.bad, false):
            FailurePalette(fill: Shade(0xFDECEA), edge: Shade(0xF2C4BF), ink: Shade(0x1C1C1E), soft: Shade(0x4A4A4F), accent: Shade(0xB3261E), onAccent: Shade(0xFFFFFF), idle: Shade(0xF3D9D6))
        case (.bad, true):
            FailurePalette(fill: Shade(0x3A1715), edge: Shade(0x6B2C27), ink: Shade(0xFFFFFF), soft: Shade(0xD6D0D0), accent: Shade(0xFFB4AB), onAccent: Shade(0x3A1715), idle: Shade(0x4F2623))
        case (.warn, false):
            FailurePalette(fill: Shade(0xFFF4E3), edge: Shade(0xF1D5A8), ink: Shade(0x1C1C1E), soft: Shade(0x4A4A4F), accent: Shade(0x8A4B00), onAccent: Shade(0xFFFFFF), idle: Shade(0xF5E3C6))
        case (.warn, true):
            FailurePalette(fill: Shade(0x38290F), edge: Shade(0x66491A), ink: Shade(0xFFFFFF), soft: Shade(0xD6D2CA), accent: Shade(0xFFCC80), onAccent: Shade(0x38290F), idle: Shade(0x4C3A1B))
        case (.quiet, false):
            FailurePalette(fill: Shade(0xF2F2F7), edge: Shade(0xD9D9E0), ink: Shade(0x1C1C1E), soft: Shade(0x4A4A4F), accent: Shade(0x48484A), onAccent: Shade(0xFFFFFF), idle: Shade(0xE3E3EA))
        case (.quiet, true):
            FailurePalette(fill: Shade(0x2C2C2E), edge: Shade(0x48484A), ink: Shade(0xFFFFFF), soft: Shade(0xD1D1D6), accent: Shade(0xD1D1D6), onAccent: Shade(0x1C1C1E), idle: Shade(0x3A3A3C))
        }
    }

    /// Every pair that carries text: the words on the fill, the button's
    /// title on the button, a waiting button's title on its own fill.
    public var textPairs: [(name: String, text: Shade, on: Shade)] {
        [
            ("the sentence", ink, fill), ("the note", soft, fill), ("the symbol and Details", accent, fill),
            ("the button", onAccent, accent), ("a waiting button", soft, idle),
        ]
    }
}
