import WorkoutsCore
import SwiftUI

/// One exercise or group on the Rows layout (DESIGN.md §7.6a): an opaque
/// card with the name, the plan in one line and the `…` menu, then its lines
/// in the order they are done (a group's members interleaved by round, with
/// the hand-off and the rests as thin lines), the set to do now open in
/// place, and `+ Add set`. A finished block folds to one line with the
/// headline change until it is opened again.
struct RowsBlockView: View {
    let block: RowsBlock
    let presentation: SessionPresentation
    /// A rest counts down before the open set.
    let isResting: Bool
    let showExercise: (ExerciseID) -> Void

    @Environment(SessionController.self) private var session
    @Environment(CoverModel.self) private var cover
    @Environment(RowsScreenModel.self) private var screen
    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .body) private var separatorIndent: CGFloat = 48

    var body: some View {
        if block.isFinished, let summary = block.summary, !screen.expanded.contains(block.id), !holdsFix {
            summaryCard(summary)
        } else {
            card
        }
    }

    private var layout: RowsLayout { presentation.rowsLayout }
    private var holdsFix: Bool { screen.fixing.map { block.item($0.row) != nil } ?? false }
    private var holdsOpenRow: Bool { layout.openRow.map { block.item($0) != nil } ?? false }
    private var groupHeader: GroupHeader? { presentation.blocks.first { $0.id == block.id }?.group }
    private var cards: [ExerciseCard] { block.slots.compactMap { presentation.card($0) } }
    private var groupColor: Color { block.group == nil ? .accentColor : GroupPalette.color(block.colorIndex) }

    // MARK: - The folded line

    private func summaryCard(_ summary: RowsBlockSummary) -> some View {
        Button {
            withAnimation(.smooth(duration: 0.25)) { _ = screen.expanded.insert(block.id) }
        } label: {
            HStack(spacing: 10) {
                RowsGlyph(kind: .done)
                VStack(alignment: .leading, spacing: 1) {
                    Text(block.title)
                        .font(.body.weight(.semibold))
                        .foregroundStyle(.primary)
                    // Every set's values: two lines at the default sizes, all at the accessibility sizes.
                    Text(summary.text)
                        .font(.footnote.monospacedDigit())
                        .foregroundStyle(.secondary)
                        .lineLimit(typeSize.isAccessibilitySize ? nil : 2)
                }
                Spacer(minLength: 8)
                Text(summary.change.text)
                    .font(.subheadline.monospacedDigit())
                    .fontWeight(summary.change.isGain ? .medium : .regular)
                    .foregroundStyle(summary.change.isGain ? RowsStyle.gain : Color.secondary)
                    .lineLimit(1)
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
            }
            .padding(.leading, 16)
            .padding(.trailing, 14)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, minHeight: 56, alignment: .leading)
            .background(RowsStyle.card, in: .rect(cornerRadius: RowsStyle.cardRadius))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(summary.sentence)
        .accessibilityHint("Shows the sets")
    }

    // MARK: - The card

    private var card: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            ForEach(Array(block.items.enumerated()), id: \.element.id) { index, item in
                if index > 0, isLine(item), isLine(block.items[index - 1]) {
                    separator
                }
                itemView(item)
            }
            ForEach(Array(block.addSets.enumerated()), id: \.offset) { _, button in
                if block.items.last.map(isLine) ?? false {
                    separator
                }
                addSetRow(button)
            }
        }
        .padding(.bottom, 4)
        .background(RowsStyle.card, in: .rect(cornerRadius: RowsStyle.cardRadius))
    }

    private var separator: some View {
        Rectangle()
            .fill(Color(.separator))
            .frame(height: 0.5)
            .padding(.leading, separatorIndent)
            .accessibilityHidden(true)
    }

    private var header: some View {
        HStack(alignment: .top, spacing: 8) {
            VStack(alignment: .leading, spacing: 1) {
                title
                if let subtitle = block.subtitle {
                    Text(subtitle)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                // The notes where they help: on the exercise being done now.
                if holdsOpenRow, let notes {
                    Text(notes)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .padding(.top, 3)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if block.isFinished {
                Button {
                    withAnimation(.smooth(duration: 0.25)) { _ = screen.expanded.remove(block.id) }
                } label: {
                    roundIcon("chevron.up")
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Fold \(block.title)")
            }
            menu
        }
        .padding(.top, 14)
        .padding(.leading, 16)
        .padding(.trailing, 12)
        .padding(.bottom, 6)
    }

    @ViewBuilder
    private var title: some View {
        let text = Text(block.title)
            .font(.title3.weight(.semibold))
            .foregroundStyle(.primary)
            .multilineTextAlignment(.leading)
        if block.group == nil, let card = cards.first {
            Button {
                showExercise(card.exercise)
            } label: {
                text
            }
            .buttonStyle(.plain)
            .accessibilityAddTraits(.isHeader)
            .accessibilityHint("Shows the exercise's history")
        } else {
            text
                .accessibilityAddTraits(.isHeader)
        }
    }

    /// The routine's notes and the gym's, as the Table card shows them.
    private var notes: String? {
        let all = cards.flatMap { [$0.planNotes, $0.notes].compactMap { $0 } }
        return all.isEmpty ? nil : all.joined(separator: "\n")
    }

    private var menu: some View {
        Menu {
            if let group = groupHeader {
                GroupMenuItems(group: group)
                Divider()
                ForEach(cards) { card in
                    Menu(card.groupLabel.map { "\($0) \(card.name)" } ?? card.name) {
                        Button("History", systemImage: "chart.line.uptrend.xyaxis") { showExercise(card.exercise) }
                        ExerciseMenuItems(card: card, group: group, allCards: presentation.cards)
                    }
                }
            } else if let card = cards.first {
                Button("History", systemImage: "chart.line.uptrend.xyaxis") { showExercise(card.exercise) }
                ExerciseMenuItems(card: card, group: nil, allCards: presentation.cards)
            }
        } label: {
            roundIcon("ellipsis")
        }
        .accessibilityLabel("\(block.title) options")
    }

    private func roundIcon(_ symbol: String) -> some View {
        Image(systemName: symbol)
            .font(.body.weight(.bold))
            .foregroundStyle(Color.accentColor)
            .frame(width: 34, height: 34)
            .background(Color(.tertiarySystemFill), in: .circle)
            .frame(width: 44, height: 44)
            .contentShape(.rect)
    }

    // MARK: - Lines

    /// A line that takes a hairline above it when the one before is a line too.
    private func isLine(_ item: RowsItem) -> Bool {
        switch item {
        case let .set(set): !showsEditor(set)
        case .warmups, .miniSets: true
        case .round, .connector, .chainHeader: false
        }
    }

    private func showsEditor(_ set: RowsSetItem) -> Bool {
        if let fixing = screen.fixing { return fixing.row == set.id && set.fix != nil }
        return set.state == .now && set.editor != nil
    }

    @ViewBuilder
    private func itemView(_ item: RowsItem) -> some View {
        switch item {
        case let .set(set):
            setView(set)
        case let .warmups(fold):
            let open = screen.unfolded.contains(fold.id) || fold.sets.contains { $0.id == screen.fixing?.row }
            RowsWarmupFoldLine(fold: fold, isOpen: open) {
                withAnimation(.smooth(duration: 0.25)) {
                    if open { _ = screen.unfolded.remove(fold.id) } else { _ = screen.unfolded.insert(fold.id) }
                }
            }
            if open {
                ForEach(fold.sets) { set in
                    separator
                    setView(set)
                }
            }
        case let .round(header):
            RowsRoundHeaderView(header: header)
        case let .connector(connector):
            RowsConnectorView(connector: connector)
        case let .chainHeader(header):
            RowsChainHeaderView(header: header)
        case let .miniSets(line):
            RowsMiniSetsLine(line: line) { row in
                if let set = line.sets.first(where: { $0.id == row }) { screen.startFix(set) }
            }
            if let fixing = screen.fixing, let set = line.sets.first(where: { $0.id == fixing.row }) {
                setView(set)
            }
        }
    }

    @ViewBuilder
    private func setView(_ set: RowsSetItem) -> some View {
        Group {
            if let fixing = screen.fixing, fixing.row == set.id, let fix = set.fix {
                RowsEditorPanel(editor: fix.editor(draft: fixing.draft), row: nil, role: set.role, groupColor: groupColor)
            } else if screen.fixing == nil, set.state == .now, let editor = set.editor {
                RowsEditorPanel(editor: editor, row: presentation.row(set.id), isResting: isResting, role: set.role,
                                groupColor: groupColor)
            } else {
                RowsSetLine(item: set, collapsed: set.state == .now, isResting: isResting, isFresh: screen.fresh == set.id,
                            groupColor: groupColor) {
                    tap(set)
                }
            }
        }
        .id(set.id)
    }

    private func tap(_ set: RowsSetItem) {
        switch set.tap {
        case .fix:
            withAnimation(.smooth(duration: 0.25)) { screen.startFix(set) }
        case let .doNow(action):
            screen.perform(action, session: session, cover: cover)
        case .none:
            // The open set drawn as a line while another is fixed: back to it.
            if set.state == .now { withAnimation(.smooth(duration: 0.25)) { screen.cancelFix() } }
        }
    }

    private func addSetRow(_ button: RowsButton) -> some View {
        Button {
            screen.perform(button.action, session: session, cover: cover)
        } label: {
            HStack(spacing: 10) {
                Image(systemName: "plus")
                    .font(.body.weight(.medium))
                    .frame(width: 22)
                Text(button.title)
                    .font(.subheadline.weight(.medium))
            }
            .foregroundStyle(Color.accentColor)
            .padding(.horizontal, 16)
            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .disabled(!button.isEnabled)
        .accessibilityLabel(button.accessibilityLabel)
    }
}
