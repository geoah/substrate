import WorkoutsCore
import SwiftUI

/// One set as one line that reads like a sentence (DESIGN.md §7.6a):
/// `Set 1  110 kg × 8  @ 8      +5 kg, −2 reps  =`. Done lines end with the
/// change against last time and the verdict mark; pending lines are muted
/// and end with last time. When the line does not fit, its end moves under
/// it. VoiceOver reads the presenter's sentence. A done line's check unticks
/// it, and its `…` holds the set's menu (Untick set, Delete set and the
/// rest), as the open set's does (owner, 2026-11-02).
struct RowsSetLine<MenuItems: View>: View {
    let item: RowsSetItem
    /// The set to do now, drawn as a line while another set is being fixed.
    var collapsed = false
    /// A rest runs before the open set: its ring is hollow.
    var isResting = false
    /// Just logged: a green wash for a moment.
    var isFresh = false
    /// A "Then" tap took the list here: an accent wash for a moment.
    var isMarked = false
    var groupColor: Color = .accentColor
    /// A done set: a tap on its check unticks it.
    var onUntick: (() -> Void)?
    let onTap: () -> Void
    /// The set's `…` menu; none when there are no items.
    @ViewBuilder var menuItems: () -> MenuItems

    @ScaledMetric(relativeTo: .subheadline) private var labelWidth: CGFloat = 44
    @ScaledMetric(relativeTo: .body) private var glyphSize: CGFloat = 22

    var body: some View {
        HStack(spacing: 0) {
            check
            line
            if MenuItems.self != EmptyView.self {
                Menu {
                    menuItems()
                } label: {
                    Image(systemName: "ellipsis")
                        .font(.subheadline.weight(.bold))
                        .foregroundStyle(.secondary)
                        .frame(width: 44, height: 44)
                        .contentShape(.rect)
                }
                .accessibilityLabel("\(item.sentence) options")
                .accessibilityHint("Untick, delete, set kind, rest and note")
                .padding(.trailing, 4)
            }
        }
        .frame(maxWidth: .infinity, minHeight: 46, alignment: .leading)
        .background {
            if isFresh || isMarked {
                LinearGradient(colors: [(isFresh ? Color.green : Color.accentColor).opacity(0.12), .clear],
                               startPoint: .leading, endPoint: UnitPoint(x: 0.75, y: 0.5))
                    .transition(.opacity)
            }
        }
        .animation(.smooth(duration: 0.3), value: isMarked)
        .opacity(item.state == .locked ? 0.55 : 1)
    }

    /// The glyph, at the line's start; a done set's check is a 44 pt button
    /// centred where the glyph always sits.
    @ViewBuilder
    private var check: some View {
        let inset = max(0, 16 + glyphSize / 2 - 22)
        if let onUntick, isDone {
            Button(action: onUntick) {
                RowsGlyph(kind: glyph)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .padding(.leading, inset)
            .padding(.trailing, max(0, 10 - (44 - glyphSize) / 2))
            .accessibilityLabel("Untick \(item.title)")
            .accessibilityHint("Back to not done, with its values")
        } else {
            RowsGlyph(kind: glyph)
                .padding(.leading, 16)
                .padding(.trailing, 10)
                .accessibilityHidden(true)
        }
    }

    @ViewBuilder
    private var line: some View {
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
        .padding(.trailing, MenuItems.self == EmptyView.self ? 16 : 0)
        .padding(.vertical, 6)
        .frame(maxWidth: .infinity, minHeight: 46, alignment: .leading)
        .contentShape(.rect)
    }

    private var glyph: RowsGlyph.Kind {
        let warmup = item.role == .warmup
        switch item.state {
        case .done: return warmup ? .warmupDone : .done
        case .now: return isResting || collapsed ? .waiting : warmup ? .warmupNow : .now
        case .next: return warmup ? .warmupTodo : item.role == .drop ? .hook : .todo
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

extension RowsSetLine where MenuItems == EmptyView {
    init(item: RowsSetItem, collapsed: Bool = false, isResting: Bool = false, isFresh: Bool = false, isMarked: Bool = false,
         groupColor: Color = .accentColor, onTap: @escaping () -> Void)
    {
        self.init(item: item, collapsed: collapsed, isResting: isResting, isFresh: isFresh, isMarked: isMarked,
                  groupColor: groupColor, onUntick: nil, onTap: onTap) { EmptyView() }
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
