import SwiftUI
import TasksCore

/// A task as a row: the round mark that completes or reopens it, its name,
/// and beside it, each under its own mark, when it is due, how urgent it
/// is and the list it is in.
struct TaskRowView: View {
    @Environment(TasksShell.self) private var shell
    let row: TaskRow

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Button {
                shell.send(row.status == .open ? .complete(row.id) : .reopen(row.id))
            } label: {
                Image(systemName: TasksSymbols.mark(row.status))
                    .font(.title3)
                    .foregroundStyle(row.status == .open ? Color.secondary : Color.accentColor)
                    .contentTransition(.symbolEffect(.replace))
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(row.toggle)

            VStack(alignment: .leading, spacing: 4) {
                Text(row.title)
                    .strikethrough(row.status == .abandoned)
                    .foregroundStyle(row.status == .open ? Color.primary : Color.secondary)
                if row.due != nil || row.priority != nil || row.listName != nil || row.hasNotes {
                    HStack(spacing: 10) {
                        if let due = row.due {
                            Label(due.text, systemImage: "calendar")
                                .foregroundStyle(TasksSymbols.color(due.tone))
                        }
                        if let priority = row.priority {
                            Label(priority.text, systemImage: TasksSymbols.symbol(priority.priority))
                                .foregroundStyle(TasksSymbols.color(priority.priority))
                        }
                        if let list = row.listName {
                            Label(list, systemImage: "list.bullet")
                                .foregroundStyle(.secondary)
                        }
                        if row.hasNotes {
                            Image(systemName: "text.alignleft")
                                .foregroundStyle(.tertiary)
                                .accessibilityLabel("Has notes")
                        }
                    }
                    .font(.caption)
                    .labelStyle(.titleAndIcon)
                }
            }
            Spacer(minLength: 0)
        }
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(row.spoken)
    }
}
