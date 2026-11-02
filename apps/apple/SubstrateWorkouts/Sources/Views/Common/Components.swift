import WorkoutsCore
import SwiftUI
import UIKit

/// The four accessible colours groups take in turn (DESIGN.md §7.6): a
/// superset's bracket, its header and its member labels. The system's teal,
/// orange and pink are too light for text on a white card, so each is a
/// shade that reads at 4.5:1 or better on the card in both appearances.
enum GroupPalette {
    static func color(_ index: Int?) -> Color {
        switch (index ?? 0) % GroupHeader.paletteSize {
        case 0: adaptive(light: 0x5856D6, dark: 0x7D7AFF)
        case 1: adaptive(light: 0x00788A, dark: 0x40C8E0)
        case 2: warmup
        default: adaptive(light: 0xD70040, dark: 0xFF6482)
        }
    }

    /// The orange of a group and of a warm-up's `W`.
    static let warmup = adaptive(light: 0xB35C00, dark: 0xFF9F0A)

    static func adaptive(light: UInt32, dark: UInt32) -> Color {
        Color(UIColor { traits in
            UIColor(rgb: traits.userInterfaceStyle == .dark ? dark : light)
        })
    }
}

/// A ghost value's grey in the set grid (§5.5, §7.6): readable at arm's
/// length, regular weight, distinct from a typed value (black) and a done one
/// (black, bold). About 5:1 on the light field and 6:1 to 7:1 on the dark
/// one, where the system's placeholder grey is under 2:1; darker or lighter
/// again with Increase Contrast.
enum GhostValueStyle {
    static let color = Color(UIColor { traits in
        switch (traits.userInterfaceStyle == .dark, traits.accessibilityContrast == .high) {
        case (false, false): UIColor(rgb: 0x6C6C70)
        case (false, true): UIColor(rgb: 0x48484A)
        case (true, false): UIColor(rgb: 0x98989D)
        case (true, true): UIColor(rgb: 0xC7C7CC)
        }
    })
}

private extension UIColor {
    convenience init(rgb: UInt32) {
        self.init(red: CGFloat((rgb >> 16) & 0xFF) / 255, green: CGFloat((rgb >> 8) & 0xFF) / 255,
                  blue: CGFloat(rgb & 0xFF) / 255, alpha: 1)
    }
}

/// A label stacked over its value, never side by side in fixed columns
/// (the repository's UI rule; §7.9's facts, the Finish summary).
struct LabelledValueView: View {
    let label: String
    let value: String

    init(_ labelled: LabelledValue) {
        label = labelled.label
        value = labelled.value
    }

    init(label: String, value: String) {
        self.label = label
        self.value = value
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.body)
                .monospacedDigit()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

/// A content card: the grouped background's raised surface, never glass (§8).
struct CardBackground: ViewModifier {
    var cornerRadius: CGFloat = 16

    func body(content: Content) -> some View {
        content.background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: cornerRadius))
    }
}

extension View {
    func cardBackground(cornerRadius: CGFloat = 16) -> some View {
        modifier(CardBackground(cornerRadius: cornerRadius))
    }
}

/// The session clock: elapsed time that ticks with no app code, or the
/// frozen time while paused.
struct SessionClockText: View {
    let clockStart: Date
    let pausedAt: Date?

    var body: some View {
        Text(timerInterval: clockStart ... Date.distantFuture, pauseTime: pausedAt, countsDown: false)
            .monospacedDigit()
    }
}

/// A countdown to `end` that ticks with no app code. The range is clamped so
/// an end in the past never makes an invalid range.
struct CountdownText: View {
    let start: Date
    let end: Date

    var body: some View {
        let lower = min(start, end)
        Text(timerInterval: lower ... max(lower, end), countsDown: true)
            .monospacedDigit()
    }
}

extension HealthSaveStatus {
    /// History's trailing glyph (§7.8); nil shows nothing.
    var symbol: String? {
        switch self {
        case .saved: "heart.fill"
        case .pending: "clock"
        case .notSaved: "exclamationmark.triangle"
        case .notAvailable: nil
        }
    }

    var accessibilityText: String {
        switch self {
        case .saved: "Saved to Apple Health"
        case .pending: "Saving to Apple Health"
        case .notSaved: "Not saved to Apple Health"
        case .notAvailable: ""
        }
    }

    var tint: Color {
        switch self {
        case .saved: .pink
        case .pending: .secondary
        case .notSaved: .orange
        case .notAvailable: .secondary
        }
    }
}
