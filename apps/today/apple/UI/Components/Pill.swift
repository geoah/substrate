import SwiftUI
import TodayCore

/// A capsule with one state word, and at most one glyph before it. One size
/// and one shape everywhere; it says a state and is never tapped.
struct PillView: View {
    var pill: Pill
    /// On the urgent card itself, where the pale fill would not show.
    var onTint = false

    @ScaledMetric(relativeTo: .caption) private var height: CGFloat = 18
    @ScaledMetric(relativeTo: .caption) private var mark: CGFloat = 11

    init(_ pill: Pill, onTint: Bool = false) {
        self.pill = pill
        self.onTint = onTint
    }

    var body: some View {
        HStack(spacing: 3) {
            if let glyph {
                Image(systemName: glyph.systemName)
                    .font(.system(size: mark, weight: .semibold))
            }
            Text(pill.word)
                .font(TodayLook.Text.pill)
                .lineLimit(1)
        }
        .foregroundStyle(words)
        .padding(.leading, glyph == nil ? 8 : 6)
        .padding(.trailing, 8)
        .frame(minHeight: height)
        .background(fill, in: Capsule())
        .fixedSize()
        .accessibilityElement(children: .combine)
    }

    /// The one glyph a kind carries: sparkles, a flame, a clock.
    private var glyph: Glyph? {
        switch pill.kind {
        case .suggested: .moss
        case .urgent: .urgent
        case .stale: .clock
        case .neutral, .caution: nil
        }
    }

    private var words: Color {
        switch pill.kind {
        case .neutral: .inkSecond
        case .suggested: .mossWords
        case .urgent: .urgentWords
        case .stale, .caution: .cautionWords
        }
    }

    private var fill: Color {
        if onTint { return .card }
        return switch pill.kind {
        case .neutral: .fillQuiet
        case .suggested: .mossFill
        case .urgent: .urgentFill
        case .stale, .caution: .cautionFill
        }
    }
}
