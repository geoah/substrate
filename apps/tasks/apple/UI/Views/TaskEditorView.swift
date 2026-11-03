import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// The edit form: each field under the label the read view gives it,
/// every keystroke a command to the editor, Save and Cancel in the toolbar.
struct TaskEditorView: View {
    @Environment(TasksShell.self) private var shell
    let key: ChildKey
    let editor: TaskEditorPresentation

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    field(editor.name) { shell.send(.setName($0), to: key) }
                    field(editor.notes, lines: true) { shell.send(.setNotes($0), to: key) }
                    field(editor.link) { shell.send(.setLink($0), to: key) }
                } footer: {
                    if let problem = editor.problem { Text(problem).foregroundStyle(.red) }
                }
                Section {
                    Picker(editor.list.label, selection: Binding(get: { editor.list.selected },
                                                                 set: { shell.send(.setList($0.isEmpty ? nil : $0), to: key) })) {
                        ForEach(editor.list.options) { Text($0.title).tag($0.id) }
                    }
                    Picker(editor.due.label, selection: Binding(get: { editor.due.kind },
                                                                set: { shell.send(.setDueKind($0), to: key) })) {
                        ForEach(editor.due.kinds) { Text($0.title).tag(DueKind(rawValue: $0.id) ?? .none) }
                    }
                    if let date = editor.due.date, let label = editor.due.dateLabel {
                        DatePicker(label, selection: Binding(get: { date }, set: { shell.send(.setDueDate($0), to: key) }),
                                   displayedComponents: editor.due.kind == .time ? [.date, .hourAndMinute] : [.date])
                    }
                    Picker(editor.priority.label, selection: Binding(get: { editor.priority.selected },
                                                                     set: { shell.send(.setPriority(TaskPriority(rawValue: $0) ?? .none), to: key) })) {
                        ForEach(editor.priority.options) { Text($0.title).tag($0.id) }
                    }
                }
            }
            .formStyle(.grouped)
            .navigationTitle(editor.title)
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(editor.cancel) { shell.send(.cancel, to: key) }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(editor.save) { shell.send(.save, to: key) }
                        .disabled(!editor.canSave)
                }
            }
        }
        #if os(macOS)
        .frame(minWidth: 440, minHeight: 420)
        #endif
    }

    /// A text field under its label, the prompt inside it.
    private func field(_ text: EditorText, lines: Bool = false, set: @escaping (String) -> Void) -> some View {
        LabeledContent(text.label) {
            TextField(text.label, text: Binding(get: { text.value }, set: set), prompt: Text(text.prompt), axis: lines ? .vertical : .horizontal)
                .lineLimit(lines ? 1 ... 6 : 1 ... 1)
                .multilineTextAlignment(.leading)
                .labelsHidden()
        }
    }
}
