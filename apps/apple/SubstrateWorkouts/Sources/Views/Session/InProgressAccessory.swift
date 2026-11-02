import WorkoutsCore
import SwiftUI

/// The session's bar in the tab view's bottom accessory (DESIGN.md §7.5): a
/// plain button filling the bar, with no glass of its own (the bar is the
/// system's). It says what is useful at a glance: the session's name and,
/// while a rest runs, its countdown; otherwise the next set with its values
/// (`Leg Press (Machine), set 2, 105 kg × 10`). Two lines when expanded, one
/// when the tab bar is minimised. A tap opens the session at that set. Its
/// rest ticks with no app code. No discard here (06 §5 item 8).
struct InProgressAccessory: View {
    let accessory: AccessoryPresentation
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
        .accessibilityHint("Opens the session at the next set")
    }

    private var isResting: Bool { accessory.restEndsAt.map { $0 > .now } ?? false }

    private var dot: some View {
        Circle()
            .fill(accessory.isPaused ? Color.secondary : Color.green)
            .frame(width: 8, height: 8)
            .accessibilityHidden(true)
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
            .fixedSize()
        }
    }

    private var title: some View {
        Text(accessory.title)
            .font(.subheadline.weight(.semibold))
            .lineLimit(1)
    }

    @ViewBuilder
    private var next: some View {
        if let detail = accessory.detail {
            Text(detail)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .lineLimit(1)
                // A long name gives way before the load and reps do.
                .truncationMode(.middle)
        } else {
            Text(accessory.isPaused ? "Paused" : "All sets done")
                .font(.footnote)
                .foregroundStyle(.secondary)
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
                title
                Spacer(minLength: 8)
                rest
            }
            next
                .padding(.leading, 16)
        }
    }

    /// One line: the rest beside the name while resting, else the next set.
    private var inline: some View {
        HStack(spacing: 8) {
            dot
            if isResting {
                rest
                title
            } else if let detail = accessory.detail {
                Text(detail)
                    .font(.subheadline.weight(.medium))
                    .lineLimit(1)
                    .truncationMode(.middle)
            } else {
                title
            }
        }
    }

    private var accessibilityText: String {
        var parts = [accessory.title]
        if accessory.isPaused { parts.append("paused") }
        if let detail = accessory.detail { parts.append("next \(detail)") }
        return parts.joined(separator: ", ")
    }

    private var accessibilityValue: String {
        guard let end = accessory.restEndsAt, end > .now else { return "" }
        return "Rest ends at \(end.formatted(date: .omitted, time: .shortened))"
    }
}
