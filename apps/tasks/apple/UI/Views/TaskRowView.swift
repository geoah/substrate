import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// A task as a row: the round mark that completes or reopens it, beside its
/// name, and under the name its facts as plain words that wrap: when it is
/// due, how urgent it is, the list it is in, and a mark for notes. Red is
/// lateness alone; a priority is its word, weighted by how urgent it is.
/// The mark and the rest are two controls: `open`, where a row is a button
/// (a phone), opens the task; beside a detail column the list's selection
/// does.
struct TaskRowView: View {
    @Environment(TasksShell.self) private var shell
    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .body) private var markSize = 22
    let row: TaskRow
    var open: (() -> Void)?

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Button(action: toggle) {
                Image(systemName: TasksSymbols.mark(row.status))
                    .font(.system(size: markSize, weight: .regular))
                    .foregroundStyle(row.status == .done ? Color.brandAccent : Color.secondary)
                    .contentTransition(.symbolEffect(.replace))
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(row.toggle)

            if let open {
                Button(action: open) { content }
                    .buttonStyle(.plain)
                    .accessibilityLabel(row.spoken)
                    .accessibilityAction(named: Text(row.toggle), toggle)
            } else {
                content
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(row.spoken)
                    .accessibilityAction(named: Text(row.toggle), toggle)
            }
        }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(row.title)
                .font(.body)
                .foregroundStyle(isOpen ? Color.primary : Color.secondary)
                .strikethrough(row.status == .abandoned)
                .fixedSize(horizontal: false, vertical: true)
            if hasFacts {
                Group {
                    if typeSize.isAccessibilitySize {
                        VStack(alignment: .leading, spacing: 3) { facts }
                    } else {
                        FactsFlow(spacing: 10, lineSpacing: 3) { facts }
                    }
                }
                .font(.subheadline)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 6)
        .contentShape(.rect)
    }

    private var isOpen: Bool { row.status == .open }

    private var hasFacts: Bool { row.due != nil || row.priority != nil || row.listName != nil || row.hasNotes }

    @ViewBuilder
    private var facts: some View {
        if let due = row.due {
            Text(due.text)
                .foregroundStyle(isOpen ? dueColor(due.tone) : Color.secondary)
                .fontWeight(isOpen && due.tone != .quiet ? .medium : .regular)
        }
        if let priority = row.priority {
            Text(priority.text)
                .foregroundStyle(isOpen && [.high, .urgent].contains(priority.priority) ? Color.primary : Color.secondary)
                .fontWeight(isOpen ? priorityWeight(priority.priority) : .regular)
        }
        if let list = row.listName {
            Text(list).foregroundStyle(.secondary)
        }
        if row.hasNotes {
            Image(systemName: "note.text").foregroundStyle(.secondary)
        }
    }

    private func dueColor(_ tone: DueWords.Tone) -> Color {
        switch tone {
        case .overdue: .red
        case .soon: .primary
        case .quiet: .secondary
        }
    }

    private func priorityWeight(_ priority: TaskPriority) -> Font.Weight {
        switch priority {
        case .urgent: .semibold
        case .high: .medium
        case .none, .low, .medium: .regular
        }
    }

    private func toggle() {
        shell.send(row.status == .open ? .complete(row.id) : .reopen(row.id))
    }
}

/// A row's facts, left to right, wrapping whole onto the next line where
/// they do not fit; a fact wider than the line takes the line and wraps
/// inside it.
private struct FactsFlow: Layout {
    var spacing: CGFloat
    var lineSpacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let lines = arrange(subviews, width: proposal.width ?? .infinity)
        let width = lines.map { $0.width }.max() ?? 0
        let height = lines.map(\.height).reduce(0, +) + lineSpacing * CGFloat(max(lines.count - 1, 0))
        return CGSize(width: proposal.width.map { min(width, $0) } ?? width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var y = bounds.minY
        for line in arrange(subviews, width: bounds.width) {
            var x = bounds.minX
            for item in line.items {
                subviews[item.index].place(
                    at: CGPoint(x: x, y: y + (line.baseline - item.baseline)), proposal: ProposedViewSize(item.size))
                x += item.size.width + spacing
            }
            y += line.height + lineSpacing
        }
    }

    private struct Item {
        var index: Int
        var size: CGSize
        var baseline: CGFloat
    }

    private struct Line {
        var items: [Item] = []
        var width: CGFloat = 0
        var baseline: CGFloat = 0
        var descent: CGFloat = 0
        var height: CGFloat { baseline + descent }
    }

    /// Each fact at its own size, capped at the line's width, set on a
    /// shared baseline per line.
    private func arrange(_ subviews: Subviews, width: CGFloat) -> [Line] {
        var lines: [Line] = []
        var line = Line()
        for (index, subview) in subviews.enumerated() {
            let ideal = subview.sizeThatFits(.unspecified)
            let size = ideal.width > width ? subview.sizeThatFits(ProposedViewSize(width: width, height: nil)) : ideal
            let dimensions = subview.dimensions(in: ProposedViewSize(size))
            let baseline = dimensions[VerticalAlignment.firstTextBaseline]
            let needed = line.items.isEmpty ? size.width : line.width + spacing + size.width
            if !line.items.isEmpty, needed > width {
                lines.append(line)
                line = Line()
            }
            line.width = line.items.isEmpty ? size.width : line.width + spacing + size.width
            line.items.append(Item(index: index, size: size, baseline: baseline))
            line.baseline = max(line.baseline, baseline)
            line.descent = max(line.descent, size.height - baseline)
        }
        if !line.items.isEmpty { lines.append(line) }
        return lines
    }
}
