import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// A task as a card: the round mark that completes or reopens it, its name
/// in the serif the lists are headed in, the opening of its notes, and its
/// facts as small tinted chips that wrap: how urgent it is, when it is due,
/// the list it is in. Red is lateness and the most urgent; a done task is
/// struck through and faded. The mark and the rest are two controls: `open`,
/// where a card is a button (a phone, an iPad's middle column), opens the
/// task; `selected` outlines the open one.
struct TaskRowView: View {
    @Environment(TasksShell.self) private var shell
    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .title3) private var markSize = 22
    let row: TaskRow
    var open: (() -> Void)?
    var selected = false

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Button(action: toggle) {
                Image(systemName: row.status == .done ? "checkmark.circle.fill" : TasksSymbols.mark(row.status))
                    .font(.system(size: markSize))
                    .foregroundStyle(row.status == .done ? Color.green : Color.secondary)
                    .contentTransition(.symbolEffect(.replace))
                    .frame(minWidth: 32, minHeight: 32)
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
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .background {
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .fill(TasksSymbols.card.opacity(isOpen ? 1 : 0.6))
                .shadow(color: .black.opacity(isOpen ? 0.06 : 0), radius: 10, y: 3)
        }
        .overlay {
            if selected {
                RoundedRectangle(cornerRadius: 20, style: .continuous).strokeBorder(Color.brandAccent, lineWidth: 2)
            }
        }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(row.title)
                .font(.system(.title3, design: .serif).weight(.semibold))
                .foregroundStyle(isOpen ? Color.primary : Color.secondary)
                .strikethrough(!isOpen)
                .fixedSize(horizontal: false, vertical: true)
            if let notes = row.notes {
                Text(notes)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .strikethrough(!isOpen)
                    .lineLimit(2)
            }
            if isOpen, hasChips {
                Group {
                    if typeSize.isAccessibilitySize {
                        VStack(alignment: .leading, spacing: 6) { chips }
                    } else {
                        ChipsFlow(spacing: 6, lineSpacing: 6) { chips }
                    }
                }
                .padding(.top, 2)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
    }

    private var isOpen: Bool { row.status == .open }

    private var hasChips: Bool { row.due != nil || row.priority != nil || row.listName != nil }

    @ViewBuilder
    private var chips: some View {
        if let priority = row.priority {
            Chip(text: priority.text.uppercased(), symbol: "flag.fill", color: priorityColor(priority.priority), emphasis: true)
        }
        if let due = row.due {
            Chip(text: due.text, symbol: "clock", color: dueColor(due.tone), emphasis: due.tone == .overdue)
        }
        if let list = row.listName {
            Chip(text: list, symbol: "list.bullet", color: .secondary, emphasis: false)
        }
    }

    private func dueColor(_ tone: DueWords.Tone) -> Color {
        switch tone {
        case .overdue: .red
        case .soon: .orange
        case .quiet: .secondary
        }
    }

    private func priorityColor(_ priority: TaskPriority) -> Color {
        switch priority {
        case .urgent, .high: .red
        case .medium: .orange
        case .none, .low: .secondary
        }
    }

    private func toggle() {
        shell.send(row.status == .open ? .complete(row.id) : .reopen(row.id))
    }
}

/// One fact on a card: a small symbol and its words, on a tint of its
/// colour.
private struct Chip: View {
    let text: String
    let symbol: String
    let color: Color
    let emphasis: Bool

    var body: some View {
        Label(text, systemImage: symbol)
            .labelStyle(TightLabelStyle(spacing: 4))
            .font(.caption.weight(emphasis ? .bold : .medium))
            .foregroundStyle(color)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(color.opacity(0.07), in: .capsule)
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
