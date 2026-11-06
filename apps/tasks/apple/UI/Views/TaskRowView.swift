import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// Completion and opening remain separate controls, each with a full touch target.
struct TaskRowView: View {
    @Environment(TasksShell.self) private var shell
    @Environment(\.dynamicTypeSize) private var typeSize
    @ScaledMetric(relativeTo: .title3) private var markSize = 22
    let row: TaskRow
    var open: (() -> Void)?
    var selected = false

    var body: some View {
        HStack(alignment: .top, spacing: 6) {
            Button(action: toggle) {
                Image(systemName: row.status == .done ? "checkmark.circle.fill" : TasksSymbols.mark(row.status))
                    .font(.system(size: markSize))
                    .foregroundStyle(row.status == .done ? Color.accentColor : Color.secondary)
                    .contentTransition(.symbolEffect(.replace))
                    .frame(minWidth: 44, minHeight: 44, alignment: .trailing)
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
        .padding(.leading, 0)
        .padding(.trailing, 14)
        .padding(.vertical, 8)
        .background {
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(TasksSymbols.card.opacity(isOpen ? 1 : 0.6))
        }
        .overlay {
            // The open task, beside its detail: a wash of the accent and a
            // fine line, never an alarm.
            if selected {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(Color.brandAccent.opacity(0.06))
                    .strokeBorder(Color.brandAccent.opacity(0.45), lineWidth: 1)
                    .allowsHitTesting(false)
            }
        }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(row.title)
                .font(.body.weight(.medium))
                .foregroundStyle(isOpen ? Color.primary : Color.secondary)
                .strikethrough(!isOpen)
                .fixedSize(horizontal: false, vertical: true)
            if let notes = row.notes {
                Text(notes)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .strikethrough(!isOpen)
                    .lineLimit(1)
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
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .contentShape(.rect)
    }

    private var isOpen: Bool { row.status == .open }

    private var hasChips: Bool { row.due != nil || row.priority != nil || row.listName != nil }

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
    }

    private func dueColor(_ tone: DueWords.Tone) -> Color {
        switch tone {
        case .overdue: .red
        case .soon: .orange
        case .quiet: .secondary
        }
    }

    private func toggle() {
        shell.send(row.status == .open ? .complete(row.id) : .reopen(row.id))
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
