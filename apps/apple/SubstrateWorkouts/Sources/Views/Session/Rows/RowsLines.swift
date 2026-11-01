import WorkoutsCore
import SwiftUI

/// One set as one line that reads like a sentence (DESIGN.md §7.6a):
/// `Set 1  110 kg × 8  @ 8      +5 kg, −2 reps  =`. Done lines end with the
/// change against last time and the verdict mark; pending lines are muted
/// and end with last time. When the line does not fit, its end moves under
/// it. VoiceOver reads the presenter's sentence.
struct RowsSetLine: View {
    let item: RowsSetItem
    /// The set to do now, drawn as a line while another set is being fixed.
    var collapsed = false
    /// A rest runs before the open set: its ring is hollow.
    var isResting = false
    /// Just logged: a green wash for a moment.
    var isFresh = false
    var groupColor: Color = .accentColor
    let onTap: () -> Void

    @ScaledMetric(relativeTo: .subheadline) private var labelWidth: CGFloat = 44

    var body: some View {
        if isTappable {
            Button(action: onTap) { content }
                .buttonStyle(.plain)
                .accessibilityLabel(item.sentence)
                .accessibilityHint(hint)
        } else {
            content
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(item.sentence)
        }
    }

    private var isTappable: Bool {
        collapsed || item.tap != .none
    }

    private var hint: String {
        if collapsed { return "Opens it again" }
        switch item.tap {
        case .fix: return "Opens it to fix"
        case .doNow: return "Does this set now"
        case .none: return ""
        }
    }

    private var content: some View {
        HStack(spacing: 10) {
            RowsGlyph(kind: glyph)
            ViewThatFits(in: .horizontal) {
                // One line at its ideal width, or the end on a line of its own:
                // a word is never cut to fit.
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    leading
                        .fixedSize()
                    Spacer(minLength: 6)
                    trailing
                        .fixedSize()
                }
                VStack(alignment: .leading, spacing: 2) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        title
                            .fixedSize()
                        values
                            .fixedSize()
                    }
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        effortAndTags
                        trailing
                    }
                }
                // The largest text: the name, the values and the rest each on a line.
                VStack(alignment: .leading, spacing: 2) {
                    title
                    values
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        effortAndTags
                        trailing
                    }
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 6)
        .frame(maxWidth: .infinity, minHeight: 46, alignment: .leading)
        .background {
            if isFresh {
                LinearGradient(colors: [Color.green.opacity(0.12), .clear], startPoint: .leading,
                               endPoint: UnitPoint(x: 0.75, y: 0.5))
                    .transition(.opacity)
            }
        }
        .contentShape(.rect)
        .opacity(item.state == .locked ? 0.55 : 1)
    }

    private var glyph: RowsGlyph.Kind {
        let warmup = item.role == .warmup
        switch item.state {
        case .done: return warmup ? .warmupDone : .done
        case .now: return isResting || collapsed ? .waiting : warmup ? .warmupNow : .now
        case .next: return warmup ? .warmupTodo : item.role == .drop ? .hook : .todo
        case .later: return .later
        case .locked: return item.role == .drop ? .hook : .locked
        }
    }

    private var isDone: Bool { item.state == .done }

    private var leading: some View {
        HStack(alignment: .firstTextBaseline, spacing: 7) {
            title
            values
            effortAndTags
        }
    }

    @ViewBuilder
    private var title: some View {
        if let badge = item.badge {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                RowsBadge(text: badge, color: groupColor)
                Text(item.title)
                    .font(.body)
                    .foregroundStyle(isDone || item.state == .now ? Color.primary : Color.secondary)
            }
            .lineLimit(2)
        } else {
            Text(item.title)
                .font(item.role == .warmup || item.role == .drop ? .subheadline.weight(.semibold) : .subheadline)
                .foregroundStyle(item.role == .warmup || item.role == .drop ? RowsStyle.orange : Color.secondary)
                .lineLimit(1)
                .frame(minWidth: labelWidth, alignment: .leading)
        }
    }

    private var values: some View {
        Text(item.values)
            .font(.body.monospacedDigit())
            .foregroundStyle(valuesColor)
            .lineLimit(2)
    }

    private var valuesColor: Color {
        if item.valuesChanged, !isDone { return .accentColor }
        return isDone ? .primary : .secondary
    }

    @ViewBuilder
    private var effortAndTags: some View {
        let tags = collapsed ? item.tags + [item.collapsedTag].compactMap { $0 } : item.tags
        if item.rpe != nil || !tags.isEmpty {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                if let rpe = item.rpe {
                    Text(rpe)
                        .font(.subheadline.monospacedDigit())
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                ForEach(Array(tags.enumerated()), id: \.offset) { _, tag in
                    RowsTagView(tag: tag)
                }
            }
        }
    }

    @ViewBuilder
    private var trailing: some View {
        if isDone {
            RowsChangesView(changes: item.changes, verdict: item.verdict)
        } else if let last = item.last {
            Text(last)
                .font(.subheadline.monospacedDigit())
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
    }
}

/// Every warm-up of an exercise on one line once all are done:
/// `Warm-ups  40 kg × 12, 70 kg × 6      as last time`. A tap unfolds them.
struct RowsWarmupFoldLine: View {
    let fold: RowsWarmupFold
    let isOpen: Bool
    let onTap: () -> Void

    var body: some View {
        Button(action: onTap) {
            HStack(spacing: 10) {
                RowsGlyph(kind: .warmupDone)
                ViewThatFits(in: .horizontal) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        title
                            .fixedSize()
                        values
                            .fixedSize()
                        Spacer(minLength: 8)
                        comparison
                            .fixedSize()
                    }
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            title
                            values
                        }
                        comparison
                    }
                }
                if isOpen {
                    Image(systemName: "chevron.up")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.tertiary)
                        .accessibilityHidden(true)
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 6)
            .frame(maxWidth: .infinity, minHeight: 46, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(fold.sentence)
        .accessibilityHint(isOpen ? "Folds the warm-ups" : "Shows each warm-up")
    }

    private var title: some View {
        Text(fold.title)
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(RowsStyle.orange)
            .lineLimit(1)
    }

    private var values: some View {
        Text(fold.values)
            .font(.subheadline.monospacedDigit())
            .foregroundStyle(.secondary)
            .lineLimit(1)
    }

    @ViewBuilder
    private var comparison: some View {
        if let comparison = fold.comparison {
            Text(comparison.text)
                .font(.subheadline.monospacedDigit())
                .fontWeight(comparison.isGain ? .medium : .regular)
                .foregroundStyle(comparison.isGain ? RowsStyle.gain : Color.secondary)
                .lineLimit(1)
        }
    }
}

/// `Round 1 of 2`, in a group.
struct RowsRoundHeaderView: View {
    let header: RowsRoundHeader

    @ScaledMetric(relativeTo: .body) private var indent: CGFloat = 48

    var body: some View {
        Text(header.title)
            .font(.footnote.weight(.semibold))
            .foregroundStyle(header.isDone ? .tertiary : .secondary)
            .padding(.leading, indent)
            .padding(.top, 8)
            .frame(maxWidth: .infinity, minHeight: 26, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}

/// A thin line between a group's sets, on the rail that joins their rings:
/// `→ 0:00, straight to A2`, `⏱ Rest 1:00, then round 2`.
struct RowsConnectorView: View {
    let connector: RowsConnector

    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .body) private var glyph: CGFloat = 22

    var body: some View {
        HStack(spacing: 10) {
            ZStack {
                Rectangle()
                    .fill(Color(.tertiaryLabel))
                    .frame(width: 1.5)
                Image(systemName: connector.kind == .transition ? "arrow.right" : "timer")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(isRest ? Color.accentColor : Color.secondary)
                    .frame(width: glyph, height: 18)
                    .background(RowsStyle.card)
            }
            .frame(width: glyph)
            Text("\(Text("\(connector.lead),").fontWeight(.semibold).foregroundStyle(leadColor)) \(connector.text)")
                .font(.footnote.monospacedDigit())
                .foregroundStyle(.secondary)
                .lineLimit(typeSize.isAccessibilitySize ? nil : 2)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, 16)
        .frame(minHeight: 28)
        .opacity(connector.phase == .passed ? 0.55 : 1)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(connector.sentence)
        .accessibilityValue(connector.phase == .running ? "running now" : "")
    }

    private var isRest: Bool { connector.kind != .transition }

    private var leadColor: Color {
        isRest ? .accentColor : .primary
    }
}

/// `Set 2 as myo-reps, 0:15 between mini-sets`, above a chain's head.
struct RowsChainHeaderView: View {
    let header: RowsChainHeader

    @ScaledMetric(relativeTo: .body) private var indent: CGFloat = 48

    var body: some View {
        Text(header.text)
            .font(.footnote.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.leading, indent)
            .padding(.trailing, 16)
            .padding(.top, 8)
            .frame(maxWidth: .infinity, minHeight: 26, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}

/// A chain's done mini-sets as chips on one line: `Mini-sets [4] [4]  as
/// last time`. A tap on a chip fixes that mini-set.
struct RowsMiniSetsLine: View {
    let line: RowsMiniSets
    let onChip: (RowID) -> Void

    @ScaledMetric(relativeTo: .subheadline) private var chip: CGFloat = 26

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 10) {
                RowsGlyph(kind: .done)
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 8) {
                        title
                        chips
                        Spacer(minLength: 8)
                        comparison
                    }
                    VStack(alignment: .leading, spacing: 4) {
                        HStack(spacing: 8) {
                            title
                            chips
                        }
                        comparison
                    }
                }
            }
            if let stop = line.stopText {
                Text(stop)
                    .font(.footnote)
                    .foregroundStyle(RowsStyle.orange)
                    .padding(.leading, 32)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 6)
        .frame(maxWidth: .infinity, minHeight: 46, alignment: .leading)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(line.sentence)
    }

    private var title: some View {
        Text(line.title)
            .font(.subheadline)
            .foregroundStyle(.secondary)
            .lineLimit(1)
    }

    private var chips: some View {
        HStack(spacing: 5) {
            ForEach(line.chips) { item in
                Button {
                    onChip(item.row)
                } label: {
                    Text(item.text)
                        .font(.subheadline.weight(.semibold).monospacedDigit())
                        .foregroundStyle(.primary)
                        .padding(.horizontal, 6)
                        .frame(minWidth: chip, minHeight: chip)
                        .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 8))
                        .padding(.vertical, 9)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(line.sets.first { $0.id == item.row }?.sentence ?? item.text)
                .accessibilityHint("Opens it to fix")
            }
        }
    }

    @ViewBuilder
    private var comparison: some View {
        if let comparison = line.comparison {
            Text(comparison.text)
                .font(.subheadline.monospacedDigit())
                .fontWeight(comparison.isGain ? .medium : .regular)
                .foregroundStyle(comparison.isGain ? RowsStyle.gain : Color.secondary)
                .lineLimit(1)
        }
    }
}
