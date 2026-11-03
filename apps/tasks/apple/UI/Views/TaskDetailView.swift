import SwiftUI
import TasksCore

/// The open task, read: its name, then each fact under the label the
/// editor uses, then what can be done to it.
struct TaskDetailView: View {
    @Environment(TasksShell.self) private var shell
    let detail: TaskDetailPresentation

    var body: some View {
        Form {
            Section {
                Text(detail.title)
                    .font(.title2.weight(.semibold))
                    .strikethrough(detail.status == .abandoned)
            }
            Section {
                ForEach(detail.fields) { field in
                    LabeledContent(field.label) {
                        if let url = field.url.flatMap(URL.init(string:)) {
                            Link(field.value, destination: url)
                        } else {
                            Text(field.value)
                                .foregroundStyle(field.tone.map(TasksSymbols.color) ?? Color.primary)
                                .multilineTextAlignment(.trailing)
                        }
                    }
                }
            }
            Section {
                ForEach(detail.actions, id: \.self) { action in
                    Button(action.title, role: action.action == .delete ? .destructive : nil) { perform(action.action) }
                }
                Picker(detail.priorityTitle, selection: Binding(get: { detail.priorities.first(where: \.current)?.priority ?? .none },
                                                                set: { shell.send(.setPriority(detail.id, $0)) })) {
                    ForEach(detail.priorities) { Text($0.title).tag($0.priority) }
                }
                Picker(detail.moveTitle, selection: Binding(get: { detail.destinations.first(where: \.current)?.id ?? "" },
                                                            set: { id in shell.send(.move(detail.id, toList: id.isEmpty ? nil : id)) })) {
                    ForEach(detail.destinations) { Text($0.title).tag($0.id) }
                }
            }
        }
        .formStyle(.grouped)
        .navigationTitle(detail.title)
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button("Edit") { shell.send(.edit(detail.id)) }
            }
        }
    }

    private func perform(_ action: TaskAction) {
        switch action {
        case .complete: shell.send(.complete(detail.id))
        case .reopen: shell.send(.reopen(detail.id))
        case .abandon: shell.send(.abandon(detail.id))
        case .edit: shell.send(.edit(detail.id))
        case .delete: shell.send(.deleteTask(detail.id))
        }
    }
}
