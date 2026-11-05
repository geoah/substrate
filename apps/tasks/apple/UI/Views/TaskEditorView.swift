import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// The edit form: each field under the label the read view gives it,
/// every keystroke a command to the editor, Save and Cancel in the toolbar,
/// and above them why the last save was not kept, when it was not.
struct TaskEditorView: View {
    @Environment(TasksShell.self) private var shell
    let key: ChildKey
    let editor: TaskEditorPresentation
    @FocusState private var naming: Bool

    var body: some View {
        NavigationStack {
            Form {
                if let notKept = editor.notKept {
                    Section { ProblemBox(notKept) }
                }
                Section {
                    field(editor.name, lines: 1 ... 3) { shell.send(.setName($0), to: key) }
                        .focused($naming)
                    field(editor.notes, lines: 3 ... 8) { shell.send(.setNotes($0), to: key) }
                    field(editor.link, link: true) { shell.send(.setLink($0), to: key) }
                } footer: {
                    if let problem = editor.problem { Text(problem).foregroundStyle(.red) }
                }
                Section {
                    Picker(
                        editor.list.label,
                        selection: Binding(
                            get: { editor.list.selected },
                            set: { shell.send(.setList($0.isEmpty ? nil : $0), to: key) })
                    ) {
                        ForEach(editor.list.options) { Text($0.title).tag($0.id) }
                    }
                    Picker(
                        editor.due.label,
                        selection: Binding(
                            get: { editor.due.kind },
                            set: { shell.send(.setDueKind($0), to: key) })
                    ) {
                        ForEach(editor.due.kinds) { Text($0.title).tag(DueKind(rawValue: $0.id) ?? .none) }
                    }
                    if let date = editor.due.date, let label = editor.due.dateLabel {
                        DatePicker(
                            label, selection: Binding(get: { date }, set: { shell.send(.setDueDate($0), to: key) }),
                            displayedComponents: editor.due.kind == .time ? [.date, .hourAndMinute] : [.date])
                    }
                    Picker(
                        editor.priority.label,
                        selection: Binding(
                            get: { editor.priority.selected },
                            set: { shell.send(.setPriority(TaskPriority(rawValue: $0) ?? .none), to: key) })
                    ) {
                        ForEach(editor.priority.options) { Text($0.title).tag($0.id) }
                    }
                }
            }
            .formStyle(.grouped)
            // A new task starts at its name; an edit leaves the keyboard down.
            .onAppear { if editor.name.value.isEmpty { naming = true } }
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

    /// A text field under its label, the prompt inside it, the whole
    /// width of the form.
    private func field(
        _ text: EditorText, lines: ClosedRange<Int> = 1 ... 1, link: Bool = false, set: @escaping (String) -> Void
    ) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(text.label).font(.subheadline).foregroundStyle(.secondary)
            TextField(text.label, text: Binding(get: { text.value }, set: set), prompt: Text(text.prompt), axis: lines.upperBound > 1 ? .vertical : .horizontal)
                .lineLimit(lines)
                .labelsHidden()
                #if os(iOS)
            .keyboardType(link ? .URL : .default)
            .textInputAutocapitalization(link ? .never : .sentences)
                #endif
                .autocorrectionDisabled(link)
        }
        .padding(.vertical, 2)
    }
}
