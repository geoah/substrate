import WorkoutsCore
import SwiftUI

/// The session's bar in the tab view's bottom accessory (DESIGN.md §7.5): a
/// plain button filling the bar, with no glass of its own (the bar is the
/// system's). Two lines when expanded, one when the tab bar is minimised.
/// Its clock and rest tick with no app code. No discard here (06 §5 item 8).
struct InProgressAccessory: View {
    let accessory: AccessoryPresentation
    let header: SessionHeaderStats
    let open: () -> Void

    @Environment(\.tabViewBottomAccessoryPlacement) private var placement

    var body: some View {
        Button(action: open) {
            Group {
                if placement == .inline {
                    inline
                } else {
                    expanded
                }
            }
            .padding(.horizontal, 16)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(accessibilityText)
        .accessibilityValue(accessibilityValue)
        .accessibilityHint("Opens the session")
    }

    private var dot: some View {
        Circle()
            .fill(accessory.isPaused ? Color.secondary : Color.green)
            .frame(width: 8, height: 8)
            .accessibilityHidden(true)
    }

    private var clock: some View {
        Group {
            if accessory.isPaused {
                Text("Paused")
            } else {
                SessionClockText(clockStart: header.clockStart, pausedAt: header.clockPausedAt)
            }
        }
        .font(.subheadline.monospacedDigit())
        .foregroundStyle(.secondary)
    }

    @ViewBuilder
    private var rest: some View {
        if let end = accessory.restEndsAt, end > .now {
            HStack(spacing: 4) {
                Text("Rest")
                CountdownText(start: .now, end: end)
            }
            .font(.subheadline.weight(.semibold).monospacedDigit())
            .foregroundStyle(.tint)
        }
    }

    /// Two lines while they fit the bar; at the largest text sizes, one.
    private var expanded: some View {
        ViewThatFits(in: .vertical) {
            twoLines
            inline
        }
    }

    private var twoLines: some View {
        VStack(alignment: .leading, spacing: 1) {
            HStack(spacing: 8) {
                dot
                Text(accessory.title)
                    .font(.subheadline.weight(.semibold))
                    .lineLimit(1)
                Spacer(minLength: 8)
                clock
            }
            if accessory.detail != nil || accessory.restEndsAt != nil {
                HStack(spacing: 8) {
                    if let detail = accessory.detail {
                        Text(detail)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .padding(.leading, 16)
                    }
                    Spacer(minLength: 8)
                    rest
                }
            }
        }
    }

    private var inline: some View {
        HStack(spacing: 8) {
            dot
            if let end = accessory.restEndsAt, end > .now {
                rest
            } else {
                clock
            }
        }
    }

    private var accessibilityText: String {
        var parts = [accessory.title]
        if accessory.isPaused { parts.append("paused") }
        if let detail = accessory.detail { parts.append("next \(detail)") }
        return parts.joined(separator: ", ")
    }

    /// What the bar shows ticking: the rest's end, else the session clock.
    private var accessibilityValue: String {
        if let end = accessory.restEndsAt, end > .now {
            return "Rest ends at \(end.formatted(date: .omitted, time: .shortened))"
        }
        if accessory.isPaused { return "Clock at \(header.durationText)" }
        return "Clock at \(Format.duration(Seconds(max(0, Date.now.timeIntervalSince(header.clockStart)))))"
    }
}
