import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// A task's row in its band's or section's card: a title, one meta line,
/// and one trailing control. The words on the left open it; on the right is
/// the round mark, centred on the row, which completes it, or for a
/// suggestion Dismiss beside Accept. The mark fills at once, with a ring
/// that drains for the undo window, and a second tap within it takes the
/// mark back; any other decision shows its words with Undo; the row only
/// leaves when the window closes and the write goes. On Needs you a late
/// task's row also offers where to move it and Won't Do. While suggestions
/// are picked, a row's leading check picks it. The card, the dividers and
/// the open task's tint are the list's (`TaskListView`).
struct TaskRowView: View {
    @Environment(TasksShell.self) private var shell
    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .title2) private var markSize = 26
    let row: TaskRow
    var open: (() -> Void)?
    var selected = false
    /// When the mark filled: the ring drains from then over the window.
    @State private var markedAt: Date?

    var body: some View {
        if row.suggestion { suggestion } else { line(trailing: true) }
    }

    /// A suggestion: its title at the row's full width, then its meta with
    /// Dismiss and Accept at the trailing end of the meta's line, or on a
    /// line of their own under it where the column is too narrow.
    private var suggestion: some View {
        HStack(alignment: .top, spacing: 8) {
            if row.picked != nil { pickMark }
            VStack(alignment: .leading, spacing: 5) {
                Text(row.title)
                    .font(.body.weight(.medium))
                    .foregroundStyle(row.pending ? Color.secondary : Color.primary)
                    .strikethrough(row.pending)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .contentShape(.rect)
                    .onTapGesture { openOrPick() }
                ViewThatFits(in: .horizontal) {
                    HStack(alignment: .center, spacing: 8) {
                        meta
                        Spacer(minLength: 8)
                        trailing.fixedSize()
                    }
                    VStack(alignment: .leading, spacing: 6) {
                        meta
                        trailing.fixedSize().frame(maxWidth: .infinity, alignment: .trailing)
                    }
                }
            }
            .padding(.vertical, 8)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(row.spoken)
        .accessibilityAction(.default) { openOrPick() }
        .onChange(of: row.pending, initial: true) { _, pending in
            markedAt = pending ? Date() : nil
        }
        .sensoryFeedback(.impact(weight: .light), trigger: row.pending)
    }

    /// A suggestion's meta: its collection and when it was written, View
    /// only where it cannot be decided here, its decision while its window
    /// is open, then its due and its group.
    private var meta: some View {
        VStack(alignment: .leading, spacing: 4) {
            if let written = row.written {
                Label(written, systemImage: "sparkle")
                    .labelStyle(TightLabelStyle(spacing: 4))
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.secondary)
            }
            if let viewOnly = row.viewOnly {
                Text(viewOnly).font(.caption.weight(.medium)).foregroundStyle(.secondary)
            }
            if let pending = row.pendingWords, row.pending {
                Text(pending).font(.caption.weight(.semibold)).foregroundStyle(Color.brandAccent)
            }
            if hasChips {
                if typeSize.isAccessibilitySize {
                    VStack(alignment: .leading, spacing: 6) { chips }
                } else {
                    ChipsFlow(spacing: 6, lineSpacing: 6) { chips }
                }
            }
        }
        .contentShape(.rect)
        .onTapGesture { openOrPick() }
    }

    private func openOrPick() {
        if row.picked != nil { shell.send(.picked(row.id)) } else { open?() }
    }

    private var pickMark: some View {
        Button {
            shell.send(.picked(row.id))
        } label: {
            Image(systemName: row.picked == true ? "checkmark.circle.fill" : "circle")
                .font(.title3)
                .foregroundStyle(row.picked == true ? Color.brandAccent : Color.secondary)
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(.rect)
        }
        .buttonStyle(.borderless)
        .accessibilityLabel(row.title)
        .accessibilityAddTraits(row.picked == true ? .isSelected : [])
    }

    private func line(trailing shown: Bool) -> some View {
        HStack(alignment: .center, spacing: 8) {
            if row.picked != nil { pickMark }
            if let open {
                Button(action: open) { content }
                    .buttonStyle(.plain)
                    .accessibilityLabel(row.spoken)
                    .accessibilityAction(named: Text(row.toggle ?? ""), toggle)
            } else {
                content
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(row.spoken)
                    .accessibilityAction(named: Text(row.toggle ?? ""), toggle)
            }
            if shown {
                trailing
                    .fixedSize()
                    .layoutPriority(1)
            }
        }
        .opacity(row.band == .later || !isOpen ? 0.62 : 1)
        .onChange(of: row.pending, initial: true) { _, pending in
            markedAt = pending ? Date() : nil
        }
        .sensoryFeedback(.impact(weight: .light), trigger: row.pending)
    }

    /// The one trailing control: Undo for a decision in its window that is
    /// not a completion, Dismiss and Accept for a suggestion, else the mark.
    @ViewBuilder
    private var trailing: some View {
        if row.pending, row.pendingWords != "Done" {
            Button {
                shell.send(.undo(row.id))
            } label: {
                DecisionLabel(title: "Undo", symbol: "arrow.uturn.backward", prominent: false)
            }
            .buttonStyle(.plain)
            .frame(minHeight: 44)
        } else if row.suggestion {
            if row.picked == nil {
                HStack(spacing: 8) {
                    ForEach(row.decisions, id: \.self) { decision in
                        if decision.action == .dismiss {
                            Button {
                                shell.perform(decision.action, on: row.id)
                            } label: {
                                Image(systemName: "xmark")
                                    .font(.subheadline.weight(.semibold))
                                    .foregroundStyle(Color.secondary)
                                    .frame(width: 36, height: 36)
                                    .background(Color.primary.opacity(0.07), in: .circle)
                                    .frame(minWidth: 44, minHeight: 44)
                                    .contentShape(.rect)
                            }
                            .buttonStyle(.plain)
                            .accessibilityLabel(decision.title)
                        } else {
                            Button(decision.title) { shell.perform(decision.action, on: row.id) }
                                .font(.subheadline.weight(.semibold))
                                .buttonStyle(.borderedProminent)
                                .buttonBorderShape(.capsule)
                                .frame(minHeight: 44)
                        }
                    }
                }
            }
        } else if row.toggle != nil {
            mark
        }
    }

    /// The round mark: open, done (filled, the ring draining while the
    /// window is open), or abandoned.
    private var mark: some View {
        Button(action: toggle) {
            ZStack {
                if row.pending, let markedAt {
                    // Drawn from the clock, not a state the list's own
                    // animations could override: the ring is the window.
                    TimelineView(.animation(minimumInterval: 1 / 30)) { context in
                        let elapsed = context.date.timeIntervalSince(markedAt)
                        Circle()
                            .trim(from: 0, to: max(0, 1 - elapsed / TasksApp.undoWindow))
                            .stroke(Color.brandAccent.opacity(0.55), style: StrokeStyle(lineWidth: 3, lineCap: .round))
                            .rotationEffect(.degrees(-90))
                    }
                    .frame(width: markSize + 10, height: markSize + 10)
                }
                Image(systemName: row.pending || row.standing == .done ? "checkmark.circle.fill" : TasksSymbols.mark(row.standing))
                    .font(.system(size: markSize))
                    .foregroundStyle(row.pending || row.standing == .done ? Color.brandAccent : Color.secondary)
                    .contentTransition(.symbolEffect(.replace))
                    .scaleEffect(row.pending ? 1.08 : 1)
            }
            .animation(.bouncy(duration: 0.35), value: row.pending)
            .frame(minWidth: 44, minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.borderless)
        .accessibilityLabel(row.toggle ?? "")
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(row.title)
                .font(.body.weight(.medium))
                .foregroundStyle(isOpen && !row.pending ? Color.primary : Color.secondary)
                .strikethrough(!isOpen || row.pending)
                .fixedSize(horizontal: false, vertical: true)
            if let written = row.written {
                Label(written, systemImage: "sparkle")
                    .labelStyle(TightLabelStyle(spacing: 4))
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.secondary)
            }
            if let viewOnly = row.viewOnly {
                Text(viewOnly).font(.caption.weight(.medium)).foregroundStyle(.secondary)
            }
            if let pending = row.pendingWords, row.pending, pending != "Done" {
                Text(pending).font(.caption.weight(.semibold)).foregroundStyle(Color.brandAccent)
            }
            if let notes = row.notes, !row.suggestion {
                Text(notes)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .strikethrough(!isOpen || row.pending)
                    .lineLimit(1)
            }
            if isOpen || row.suggestion, hasChips {
                Group {
                    if typeSize.isAccessibilitySize {
                        VStack(alignment: .leading, spacing: 6) { chips }
                    } else {
                        ChipsFlow(spacing: 6, lineSpacing: 6) { chips }
                    }
                }
                .padding(.top, 2)
            }
            if hasDecisions {
                decisions.padding(.top, 6)
            }
        }
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .contentShape(.rect)
        .animation(.snappy(duration: 0.25), value: row.pending)
    }

    private var isOpen: Bool { row.standing == .open || row.standing == .proposed }

    private var hasChips: Bool { row.due != nil || row.priority != nil || row.listName != nil || row.collection != nil }

    private var hasDecisions: Bool { row.reschedule != nil || (!row.suggestion && !row.decisions.isEmpty) }

    @ViewBuilder
    private var chips: some View {
        if let due = row.due {
            Chip(text: due.text, symbol: "clock", color: dueColor(due.tone), emphasis: due.tone == .overdue)
        }
        if let priority = row.priority {
            Chip(text: priority.text, symbol: "flag", color: TasksSymbols.color(priority.priority), emphasis: false)
        }
        if let list = row.listName {
            Chip(text: list, symbol: "list.bullet", color: .secondary, emphasis: false)
        }
        if let collection = row.collection {
            Chip(text: collection, symbol: "square.stack", color: .secondary, emphasis: false)
        }
    }

    /// What a late task's card decides from where it is: the days it can
    /// move to, and Won't Do.
    private var decisions: some View {
        HStack(spacing: 8) {
            if let reschedule = row.reschedule {
                Menu {
                    ForEach(reschedule.options, id: \.self) { option in
                        Button(option.title) { shell.send(.reschedule(row.id, option.action)) }
                    }
                } label: {
                    DecisionLabel(title: reschedule.title, symbol: "calendar", prominent: true)
                }
                .buttonStyle(.plain)
                .fixedSize()
            }
            ForEach(row.decisions.filter { !row.suggestion && $0.action != .accept && $0.action != .dismiss }, id: \.self) { decision in
                Button {
                    shell.perform(decision.action, on: row.id)
                } label: {
                    DecisionLabel(title: decision.title, symbol: nil, prominent: false)
                }
                .buttonStyle(.plain)
            }
        }
    }

    private func dueColor(_ tone: DueWords.Tone) -> Color {
        switch tone {
        case .overdue: .red
        case .soon: .orange
        case .quiet: .secondary
        }
    }

    private func toggle() {
        guard row.toggle != nil else { return }
        shell.send(row.standing == .open && !row.pending ? .complete(row.id) : .reopen(row.id))
    }
}

/// A decision's button on a card: a small capsule, the first one filled.
private struct DecisionLabel: View {
    let title: String
    let symbol: String?
    let prominent: Bool

    var body: some View {
        Label {
            Text(title)
        } icon: {
            if let symbol { Image(systemName: symbol) }
        }
        .labelStyle(TightLabelStyle(spacing: 5))
        .font(.subheadline.weight(.semibold))
        .foregroundStyle(prominent ? TasksSymbols.card : Color.primary)
        .padding(.horizontal, 12)
        .padding(.vertical, 7)
        .background(prominent ? Color.primary : Color.primary.opacity(0.07), in: .capsule)
        .contentShape(.capsule)
    }
}

/// Reserve a filled badge for overdue dates so neutral facts stay quiet.
private struct Chip: View {
    let text: String
    let symbol: String
    let color: Color
    let emphasis: Bool

    var body: some View {
        Label(text, systemImage: symbol)
            .labelStyle(TightLabelStyle(spacing: 4))
            .font(.caption.weight(emphasis ? .semibold : .medium))
            .foregroundStyle(color)
            .padding(.horizontal, emphasis ? 6 : 0)
            .padding(.vertical, 2)
            .background(color.opacity(emphasis ? 0.08 : 0), in: .capsule)
    }
}

/// A card's chips, left to right, wrapping whole onto the next line where
/// they do not fit; a chip wider than the line takes the line.
private struct ChipsFlow: Layout {
    var spacing: CGFloat
    var lineSpacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let lines = arrange(subviews, width: proposal.width ?? .infinity)
        let width = lines.map(\.width).max() ?? 0
        let height = lines.map(\.height).reduce(0, +) + lineSpacing * CGFloat(max(lines.count - 1, 0))
        return CGSize(width: proposal.width.map { min(width, $0) } ?? width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var y = bounds.minY
        for line in arrange(subviews, width: bounds.width) {
            var x = bounds.minX
            for item in line.items {
                subviews[item.index].place(
                    at: CGPoint(x: x, y: y + (line.height - item.size.height) / 2), proposal: ProposedViewSize(item.size))
                x += item.size.width + spacing
            }
            y += line.height + lineSpacing
        }
    }

    private struct Item {
        var index: Int
        var size: CGSize
    }

    private struct Line {
        var items: [Item] = []
        var width: CGFloat = 0
        var height: CGFloat = 0
    }

    private func arrange(_ subviews: Subviews, width: CGFloat) -> [Line] {
        var lines: [Line] = []
        var line = Line()
        for (index, subview) in subviews.enumerated() {
            let ideal = subview.sizeThatFits(.unspecified)
            let size = ideal.width > width ? subview.sizeThatFits(ProposedViewSize(width: width, height: nil)) : ideal
            if !line.items.isEmpty, line.width + spacing + size.width > width {
                lines.append(line)
                line = Line()
            }
            line.width = line.items.isEmpty ? size.width : line.width + spacing + size.width
            line.height = max(line.height, size.height)
            line.items.append(Item(index: index, size: size))
        }
        if !line.items.isEmpty { lines.append(line) }
        return lines
    }
}
