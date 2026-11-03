import WorkoutsCore
import SwiftUI

/// One set as one line that reads like a sentence (DESIGN.md §7.6a):
/// `[1]  110 kg × 8  @ 8      +5 kg, −2 reps  =`, the set's label first and,
/// in a group, its exercise beside it. Done lines sit on a light green wash
/// with their values struck through and grey, ending with the change against
/// last time and the verdict mark, so a logged set never looks like the one
/// to do (owner, 2026-11-02); pending lines are muted and end with last time.
/// When the line does not fit, its end moves under it, lined up with the
/// text after the label. VoiceOver reads the presenter's sentence. A done
/// line's check unticks it, and its `…` holds the set's menu (Untick set,
/// Delete set and the rest), as the open set's does.
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
    /// A done set: a tap on its check unticks it.
    var onUntick: (() -> Void)?
    /// What a tap does, said aloud, where it is not the session's (the routine page's plan).
    var tapHint: String?
    /// A routine's plan, read only: nothing to tick, so no glyph, and the
    /// targets are what the line is for, so they read in the primary colour.
    var isPlan = false
    let onTap: () -> Void
    /// The set's `…` menu; none when there are no items.
    @ViewBuilder var menuItems: () -> MenuItems

    @ScaledMetric(relativeTo: .body) private var glyphSize: CGFloat = 22
    @Environment(\.colorSchemeContrast) private var contrast
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        HStack(spacing: 0) {
            if !isPlan {
                check
            }
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
            if isDone {
                RowsStyle.doneFill(contrast)
            }
        }
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
        if let tapHint { return tapHint }
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
            // The second row starts where the text after the label does.
            VStack(alignment: .afterSetLabel, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: Self.afterLabel) {
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
            // The name, the values and the rest each on a line, under the
            // text after the label; at the accessibility sizes, under the
            // label, where the width is needed more.
            VStack(alignment: typeSize.isAccessibilitySize ? .leading : .afterSetLabel, spacing: 2) {
                title
                values
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    effortAndTags
                    trailing
                }
            }
        }
        .padding(.leading, isPlan ? 16 : 0)
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

    /// The space between the label and the text after it.
    private static var afterLabel: CGFloat { 7 }

    private var title: some View {
        HStack(alignment: .center, spacing: Self.afterLabel) {
            RowsSetLabel(text: item.setLabel, isWarmup: item.role == .warmup, emphasis: labelEmphasis)
                .alignmentGuide(.afterSetLabel) { $0[.trailing] + Self.afterLabel }
            if let caption = item.caption {
                Text(caption)
                    .font(item.isChained ? .subheadline.weight(.semibold) : .body)
                    .foregroundStyle(captionColor)
                    .lineLimit(2)
            }
        }
    }

    private var labelEmphasis: RowsSetLabel.Emphasis {
        switch item.state {
        case .done: .done
        case .now: collapsed ? .todo : .now
        case .next, .locked: .todo
        }
    }

    private var captionColor: Color {
        if item.role == .drop { return RowsStyle.orange }
        return item.state == .now ? .primary : .secondary
    }

    private var values: some View {
        Text(item.values)
            .font(.body.monospacedDigit())
            .strikethrough(isDone, color: .secondary)
            .foregroundStyle(valuesColor)
            .lineLimit(2)
    }

    private var valuesColor: Color {
        if item.valuesChanged, !isDone { return .accentColor }
        // Struck through and grey is hard to read with Increase Contrast: the strike alone says done there.
        if isDone, contrast == .increased { return .primary }
        return item.state == .now || isPlan ? .primary : .secondary
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
            // Last time is a reference, not a target: a step quieter than the RPE beside it.
            Text(last)
                .font(.subheadline.monospacedDigit())
                .foregroundStyle(contrast == .increased ? AnyShapeStyle(.secondary) : AnyShapeStyle(.tertiary))
                .lineLimit(1)
        }
    }
}

private extension HorizontalAlignment {
    /// Where a set line's text starts after its label.
    enum AfterSetLabel: AlignmentID {
        static func defaultValue(in context: ViewDimensions) -> CGFloat { context[.leading] }
    }

    static let afterSetLabel = HorizontalAlignment(AfterSetLabel.self)
}

extension RowsSetLine where MenuItems == EmptyView {
    init(item: RowsSetItem, collapsed: Bool = false, isResting: Bool = false, isFresh: Bool = false, isMarked: Bool = false,
         onTap: @escaping () -> Void)
    {
        self.init(item: item, collapsed: collapsed, isResting: isResting, isFresh: isFresh, isMarked: isMarked,
                  onUntick: nil, onTap: onTap) { EmptyView() }
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

    /// On a routine's plan, whose lines have no glyph: under the labels.
    var isPlan = false

    @ScaledMetric(relativeTo: .body) private var indent: CGFloat = 48

    var body: some View {
        Text(header.title)
            .font(.footnote.weight(.semibold))
            .foregroundStyle(header.isDone ? .tertiary : .secondary)
            .padding(.leading, isPlan ? 16 : indent)
            .padding(.top, 8)
            .frame(maxWidth: .infinity, minHeight: 26, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}

/// `Set 2 as myo-reps, 0:15 between mini-sets`, above a chain's head.
struct RowsChainHeaderView: View {
    let header: RowsChainHeader

    /// On a routine's plan, whose lines have no glyph: under the labels.
    var isPlan = false

    @ScaledMetric(relativeTo: .body) private var indent: CGFloat = 48

    var body: some View {
        Text(header.text)
            .font(.footnote.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.leading, isPlan ? 16 : indent)
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
