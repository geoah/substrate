import SubstrateScreens
import SwiftUI
import TasksCore

/// The open task: its name beside its round mark (filled while a completion
/// waits its undo window), then each fact once under
/// the label the editor uses, the list and the priority as the pickers that
/// change them where they stand, the notes and the link as text of their
/// own, every other property under Details, then what can be done to it,
/// Delete apart. Edit is in the toolbar, for the app's own tasks.
struct TaskDetailView: View {
    @Environment(TasksShell.self) private var shell
    @ScaledMetric(relativeTo: .title2) private var markSize = 26
    let detail: TaskDetailPresentation

    var body: some View {
        Form {
            Section {
                HStack(alignment: .center, spacing: 6) {
                    Text(detail.title)
                        .font(.title2.weight(.semibold))
                        .foregroundStyle(detail.standing == .open && !detail.pending ? Color.primary : Color.secondary)
                        .strikethrough(detail.standing == .dropped || detail.pending)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    Button {
                        shell.send(detail.standing == .open && !detail.pending ? .complete(detail.id) : .reopen(detail.id))
                    } label: {
                        Image(systemName: detail.pending ? "checkmark.circle.fill" : TasksSymbols.mark(detail.standing))
                            .font(.system(size: markSize))
                            .foregroundStyle(detail.standing == .done || detail.pending ? Color.brandAccent : Color.secondary)
                            .contentTransition(.symbolEffect(.replace))
                            .frame(minWidth: 44, minHeight: 44)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.borderless)
                    .disabled(detail.actions.allSatisfy { $0.action == .delete })
                    .accessibilityLabel(detail.pending ? "Undo" : detail.standing == .open ? "Mark as Done" : "Reopen")
                }
                .animation(.snappy(duration: 0.25), value: detail.pending)
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
            if !detail.details.isEmpty {
                Section(detail.detailsTitle) {
                    ForEach(detail.details) { fact in DetailFactRow(fact: fact) }
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
            if let edit = detail.edit {
                ToolbarItem(placement: .primaryAction) {
                    Button(edit) { shell.send(.edit(detail.id)) }
                }
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
        shell.perform(action, on: detail.id)
    }
}

/// A property under Details: its label, its value, and the full reference
/// under it where it names one.
struct DetailFactRow: View {
    let fact: DetailFact

    var body: some View {
        LabeledContent(fact.label) {
            VStack(alignment: .trailing, spacing: 2) {
                Text(fact.value).multilineTextAlignment(.trailing)
                if let note = fact.note {
                    Text(note).font(.caption.monospaced()).foregroundStyle(.secondary).multilineTextAlignment(.trailing)
                }
            }
        }
    }
}
