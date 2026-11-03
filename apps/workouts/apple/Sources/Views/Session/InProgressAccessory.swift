import WorkoutsCore
import SwiftUI

/// The session's bar in the tab view's bottom accessory (DESIGN.md §7.5): a
/// plain button filling the bar, with no glass of its own (the bar is the
/// system's). It says what is useful at a glance: the session's name and,
/// while a rest runs, its countdown, or while a timed set runs, its clock;
/// and the next set with its values (`Leg Press (Machine), set 2,` then
/// `105 kg × 10`, which is never cut: a long name gives way first). Two lines
/// when expanded, one when the tab bar is minimised. A tap opens the session
/// at that set. Its clocks tick with no app code. No discard here (06 §5 item 8).
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

    private var dot: some View {
        Circle()
            .fill(accessory.isPaused ? Color.secondary : Color.green)
            .frame(width: 8, height: 8)
            .accessibilityHidden(true)
    }

    /// The rest's countdown, or a running timed set's clock counting up.
    @ViewBuilder
    private var clock: some View {
        if let end = accessory.restEndsAt {
            HStack(spacing: 4) {
                Text("Rest")
                CountdownText(start: .now, end: end)
            }
            .font(.subheadline.weight(.semibold).monospacedDigit())
            .foregroundStyle(.tint)
            .fixedSize()
        } else if let start = accessory.timerStartedAt {
            HStack(spacing: 4) {
                Circle().fill(.red).frame(width: 6, height: 6)
                Text(timerInterval: start ... .distantFuture, countsDown: false)
            }
            .font(.subheadline.weight(.semibold).monospacedDigit())
            .foregroundStyle(.red)
            .fixedSize()
        }
    }

    private var isCounting: Bool { accessory.restEndsAt != nil || accessory.timerStartedAt != nil }

    private var title: some View {
        Text(accessory.title)
            .font(.subheadline.weight(.semibold))
            .lineLimit(1)
    }

    /// The set, which truncates, then its values, which never do.
    private func nextSet(font: Font) -> some View {
        HStack(spacing: 0) {
            if let set = accessory.set {
                Text(accessory.values == nil ? set : "\(set), ")
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
            if let values = accessory.values {
                Text(values)
                    .lineLimit(1)
                    .fixedSize()
                    .layoutPriority(1)
            }
        }
        .font(font)
    }

    @ViewBuilder
    private var next: some View {
        if accessory.set != nil {
            nextSet(font: .footnote)
                .foregroundStyle(.secondary)
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
                clock
            }
            next
                .padding(.leading, 16)
        }
    }

    /// One line: the rest or the timed set's clock, then the next set; else the name.
    private var inline: some View {
        HStack(spacing: 8) {
            dot
            if isCounting {
                clock
            }
            if accessory.set != nil {
                nextSet(font: .subheadline.weight(.medium))
            } else {
                title
            }
        }
    }

    private var accessibilityText: String {
        var parts = [accessory.title]
        if accessory.isPaused { parts.append("paused") }
        if let detail = accessory.detail { parts.append(accessory.timerStartedAt != nil ? detail : "next \(detail)") }
        return parts.joined(separator: ", ")
    }

    private var accessibilityValue: String {
        if let end = accessory.restEndsAt { return "Rest ends at \(end.formatted(date: .omitted, time: .shortened))" }
        if let start = accessory.timerStartedAt { return "Started at \(start.formatted(date: .omitted, time: .shortened))" }
        return ""
    }
}
