import SubstrateScreens
import SwiftUI
import TasksCore

/// The open task: its name, then each fact once under the label the editor
/// uses, the list and the priority as the pickers that change them where
/// they stand, then what can be done to it. Edit is in the toolbar.
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
                    if let choice = field.choice {
                        Picker(field.label, selection: Binding(get: { choice.selected }, set: { choose(field.field, $0) })) {
                            ForEach(choice.options) { Text($0.title).tag($0.id) }
                        }
                    } else {
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
            }
            Section {
                ForEach(detail.actions, id: \.self) { action in
                    Button(action.title, role: action.action == .delete ? .destructive : nil) { perform(action.action) }
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
                Button(detail.edit) { shell.send(.edit(detail.id)) }
            }
        }
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
