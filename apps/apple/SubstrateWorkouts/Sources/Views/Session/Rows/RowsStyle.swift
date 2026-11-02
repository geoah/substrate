import WorkoutsCore
import SwiftUI

/// The Rows layout's colours, sizes and small shared pieces (DESIGN.md
/// §7.6a). Content is opaque: cards and editors use flat fills; only the
/// capsule, the keypad and the undo toast float on glass.
enum RowsStyle {
    static let cardRadius: CGFloat = 24
    static let editorRadius: CGFloat = 20
    /// The card's surface, as every content card in the app.
    static let card = Color(.secondarySystemGroupedBackground)
    /// A gain against last time, as text: the system green does not read on white.
    static let gain = GroupPalette.adaptive(light: 0x248A3D, dark: 0x30D158)
    /// Warm-ups and drops: the same orange as the Table's `W`.
    static let orange = GroupPalette.warmup
    /// An orange a white label reads on, for the warm-up's and the drop's Log.
    static let orangeButton = GroupPalette.adaptive(light: 0xB35C00, dark: 0xC26100)
    static let red = GroupPalette.adaptive(light: 0xD70015, dark: 0xFF6961)

    /// The colour of an editor's controls: the accent, or orange for a warm-up or a drop.
    static func tint(_ tint: RowsEditor.Tint) -> Color {
        tint == .orange ? orange : .accentColor
    }

    /// The fill behind a Log or Save button.
    static func buttonFill(_ tint: RowsEditor.Tint) -> Color {
        tint == .orange ? orangeButton : .accentColor
    }

    static func tagColor(_ style: RowsTag.Style) -> Color {
        switch style {
        case .accent: .accentColor
        case .grey: .secondary
        case .orange: orange
        case .red: red
        }
    }

    static func tagFill(_ style: RowsTag.Style) -> Color {
        switch style {
        case .accent: Color.accentColor.opacity(0.12)
        case .grey: Color(.tertiarySystemFill)
        case .orange: Color.orange.opacity(0.14)
        case .red: Color.red.opacity(0.12)
        }
    }

    /// The SF Symbol for a verdict's mark (`↗`, `=`, `↘`).
    static func verdictSymbol(_ verdict: RowsVerdict) -> String {
        switch verdict {
        case .better: "arrow.up.right"
        case .same: "equal"
        case .below: "arrow.down.right"
        }
    }
}

/// The circle at the start of a line: done, now, waiting, to do, later, locked.
struct RowsGlyph: View {
    enum Kind: Equatable {
        case done
        case warmupDone
        /// The set to do now.
        case now
        case warmupNow
        /// The set to do now while a rest runs before it: a hollow ring.
        case waiting
        case todo
        case warmupTodo
        /// Passed over: a pause mark.
        case later
        case locked
        /// A drop hanging off its set.
        case hook
    }

    let kind: Kind

    @ScaledMetric(relativeTo: .body) private var size: CGFloat = 22

    var body: some View {
        ZStack {
            switch kind {
            case .done:
                Circle().fill(.green)
                check
            case .warmupDone:
                Circle().fill(.orange)
                check
            case .now:
                Circle().strokeBorder(Color.accentColor, lineWidth: 2)
                Circle().fill(Color.accentColor).frame(width: size * 0.45, height: size * 0.45)
            case .warmupNow:
                Circle().strokeBorder(RowsStyle.orange, lineWidth: 2)
                Circle().fill(RowsStyle.orange).frame(width: size * 0.45, height: size * 0.45)
            case .waiting:
                Circle().strokeBorder(Color.accentColor, lineWidth: 2)
                Circle().strokeBorder(Color.accentColor, lineWidth: 1.5).frame(width: size * 0.4, height: size * 0.4)
            case .todo:
                Circle().strokeBorder(Color(.tertiaryLabel), lineWidth: 1.5)
            case .warmupTodo:
                Circle().strokeBorder(Color.orange.opacity(0.55), lineWidth: 1.5)
            case .later:
                Circle().fill(Color(.tertiarySystemFill))
                Image(systemName: "pause.fill")
                    .font(.system(size: size * 0.36, weight: .bold))
                    .foregroundStyle(.secondary)
            case .locked:
                Circle().strokeBorder(Color(.quaternaryLabel), style: StrokeStyle(lineWidth: 1.5, dash: [2.5, 2.5]))
            case .hook:
                Image(systemName: "arrow.turn.down.right")
                    .font(.system(size: size * 0.7, weight: .semibold))
                    .foregroundStyle(RowsStyle.orange)
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }

    private var check: some View {
        Image(systemName: "checkmark")
            .font(.system(size: size * 0.48, weight: .bold))
            .foregroundStyle(.white)
    }
}

/// `A1`, a group member's badge, in the group's colour.
struct RowsBadge: View {
    let text: String
    let color: Color

    var body: some View {
        Text(text)
            .font(.footnote.weight(.bold))
            .foregroundStyle(color)
            .padding(.horizontal, 5)
            .padding(.vertical, 1)
            .background(color.opacity(0.13), in: .rect(cornerRadius: 6))
    }
}

/// A small tag after a line's values: `+ drop`, `later`, `up next`.
struct RowsTagView: View {
    let tag: RowsTag

    var body: some View {
        Text(tag.text)
            .font(.footnote.weight(.semibold))
            .foregroundStyle(RowsStyle.tagColor(tag.style))
            .padding(.horizontal, 7)
            .padding(.vertical, 1)
            .background(RowsStyle.tagFill(tag.style), in: .rect(cornerRadius: 7))
            .lineLimit(1)
    }
}

/// A done set's change against last time and its verdict mark: gains green,
/// everything else grey, joined by commas.
struct RowsChangesView: View {
    let changes: [RowsChange]
    let verdict: RowsVerdict?

    var body: some View {
        HStack(spacing: 6) {
            if !changes.isEmpty {
                changes.enumerated().reduce(Text(verbatim: "")) { text, pair in
                    let (index, change) = pair
                    let part = Text(change.text)
                        .foregroundStyle(change.isGain ? RowsStyle.gain : Color.secondary)
                        .fontWeight(change.isGain ? .medium : .regular)
                    return index == 0 ? part : Text("\(text)\(Text(", ").foregroundStyle(.secondary))\(part)")
                }
                .font(.subheadline.monospacedDigit())
                .lineLimit(1)
            }
            if let verdict {
                RowsVerdictMark(verdict: verdict)
            }
        }
    }
}

/// The verdict's small round mark: green only when better, never red.
struct RowsVerdictMark: View {
    let verdict: RowsVerdict

    @ScaledMetric(relativeTo: .subheadline) private var size: CGFloat = 20

    var body: some View {
        Image(systemName: RowsStyle.verdictSymbol(verdict))
            .font(.system(size: size * 0.55, weight: .bold))
            .foregroundStyle(verdict == .better ? RowsStyle.gain : Color.secondary)
            .frame(width: size, height: size)
            .background(verdict == .better ? Color.green.opacity(0.16) : Color(.tertiarySystemFill), in: .circle)
            .accessibilityHidden(true)
    }
}

/// A filled capsule button: Log, Save, Start, Stop and log, the keypad's Set.
struct RowsFilledButtonStyle: ButtonStyle {
    let fill: Color
    var minHeight: CGFloat = 50
    var horizontalPadding: CGFloat = 20

    @Environment(\.isEnabled) private var isEnabled

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(isEnabled ? Color.white : Color(.tertiaryLabel))
            .padding(.horizontal, horizontalPadding)
            .frame(minHeight: minHeight)
            .background(isEnabled ? fill : Color(.tertiarySystemFill), in: .capsule)
            .opacity(configuration.isPressed ? 0.75 : 1)
            .contentShape(.capsule)
    }
}

/// A tinted capsule button: the secondary beside Start or Stop, Go back.
struct RowsTintedButtonStyle: ButtonStyle {
    let tint: Color
    var minHeight: CGFloat = 50

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(tint)
            .padding(.horizontal, 18)
            .frame(minHeight: minHeight)
            .background(tint.opacity(configuration.isPressed ? 0.24 : 0.13), in: .capsule)
            .contentShape(.capsule)
    }
}

/// Reserves the width of `template` so a ticking time never moves what is
/// beside it (monospaced digits keep each digit the same width; this keeps
/// 9:59 to 10:00 from growing the line). Set the font on this view, so the
/// template and the time share it.
struct ReservedWidth<Content: View>: View {
    let template: String
    var alignment: Alignment = .trailing
    @ViewBuilder let content: Content

    var body: some View {
        ZStack(alignment: alignment) {
            Text(verbatim: template).hidden()
            content
        }
    }
}

extension RowsAction {
    /// The controller's command for the action: one each (WorkoutsKit/API.md).
    var command: SessionCommand {
        switch self {
        case let .tick(row): .tick(row)
        case let .touch(row): .touchRow(row)
        case let .edit(row, patch): .edit(row, patch)
        case let .removeSet(row): .removeSet(row)
        case let .addSet(slot, role): .addSet(slot: slot, role: role)
        case let .startTimedSet(row): .startTimedSet(row)
        case let .stopTimedSet(row): .stopTimedSet(row)
        case let .cancelTimedSet(row): .cancelTimedSet(row)
        case let .adjustRest(seconds): .adjustRest(seconds)
        case .skipRest: .skipRest
        }
    }

    /// The glyph on a primary button: a check for Log and Save, play and stop for a timer.
    var symbol: String {
        switch self {
        case .startTimedSet: "play.fill"
        case .stopTimedSet: "stop.fill"
        default: "checkmark"
        }
    }
}
