import SubstrateScreens
import SwiftUI
import TasksCore

/// The open task: its round mark beside its name, then each fact once under
/// the label the editor uses, the list and the priority as the pickers that
/// change them where they stand, the notes and the link as text of their
/// own, then what can be done to it, Delete apart. Edit is in the toolbar.
struct TaskDetailView: View {
    @Environment(TasksShell.self) private var shell
    @ScaledMetric(relativeTo: .title2) private var markSize = 26
    let detail: TaskDetailPresentation

    var body: some View {
        Form {
            Section {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Button {
                        shell.send(detail.status == .open ? .complete(detail.id) : .reopen(detail.id))
                    } label: {
                        Image(systemName: TasksSymbols.mark(detail.status))
                            .font(.system(size: markSize))
                            .foregroundStyle(detail.status == .done ? Color.brandAccent : Color.secondary)
                            .contentTransition(.symbolEffect(.replace))
                            .frame(minWidth: 44, minHeight: 44, alignment: .trailing)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel(detail.status == .open ? "Mark as Done" : "Reopen")
                    Text(detail.title)
                        .font(.title2.weight(.semibold))
                        .foregroundStyle(detail.status == .open ? Color.primary : Color.secondary)
                        .strikethrough(detail.status == .abandoned)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                }
            }
            Section {
                ForEach(facts) { field in
                    if let choice = field.choice {
                        Picker(field.label, selection: Binding(get: { choice.selected }, set: { choose(field.field, $0) })) {
                            ForEach(choice.options) { Text($0.title).tag($0.id) }
                        }
                    } else {
                        ViewThatFits(in: .horizontal) {
                            LabeledContent(field.label) { value(field) }
                            VStack(alignment: .leading, spacing: 2) {
                                Text(field.label).font(.subheadline).foregroundStyle(.secondary)
                                value(field)
                            }
                        }
                    }
                }
            }
            if let notes = detail.fields.first(where: { $0.field == .notes }) {
                Section(notes.label) {
                    Text(notes.value).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            if let link = detail.fields.first(where: { $0.field == .link }), let url = link.url.flatMap(URL.init(string:)) {
                Section(link.label) {
                    Link(destination: url) {
                        Label(link.value, systemImage: "link").lineLimit(2).truncationMode(.middle)
                    }
                }
            }
            Section {
                ForEach(detail.actions.filter { $0.action != .delete }, id: \.self) { action in
                    Button(action.title) { perform(action.action) }
                }
            }
            if let delete = detail.actions.first(where: { $0.action == .delete }) {
                Section {
                    Button(delete.title, role: .destructive) { perform(delete.action) }
                }
            }
        }
        .formStyle(.grouped)
        .scrollContentBackground(.hidden)
        .background { TasksSymbols.screen }
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button(detail.edit) { shell.send(.edit(detail.id)) }
            }
        }
    }

    /// The facts the rows hold: the state is the round mark's, and the notes
    /// and the link are sections of their own.
    private var facts: [DetailField] {
        detail.fields.filter { ![.status, .notes, .link].contains($0.field) }
    }

    private func value(_ field: DetailField) -> some View {
        Text(field.value)
            .foregroundStyle(field.tone == .overdue ? Color.red : Color.primary)
            .multilineTextAlignment(.leading)
    }

    private func choose(_ field: TaskField, _ id: String) {
        switch field {
        case .list: shell.send(.move(detail.id, toList: id.isEmpty ? nil : id))
        case .priority: shell.send(.setPriority(detail.id, TaskPriority(rawValue: id) ?? .none))
        default: break
        }
    }

    private func perform(_ action: TaskAction) {
        switch action {
        case .complete: shell.send(.complete(detail.id))
        case .reopen: shell.send(.reopen(detail.id))
        case .abandon: shell.send(.abandon(detail.id))
        case .delete: shell.send(.deleteTask(detail.id))
        }
    }
}
