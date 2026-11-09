import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// Settings → Collections: every candidate with its label, its full
/// reference, what it holds and a switch, and where new tasks go. A row
/// opens the collection's page.
struct CollectionsView: View {
    @Environment(TasksShell.self) private var shell
    let collections: CollectionsPresentation

    var body: some View {
        Form {
            Section {
                Text(collections.intro).font(.subheadline).foregroundStyle(.secondary)
            }
            Section {
                ForEach(collections.rows) { row in
                    HStack(alignment: .center, spacing: 12) {
                        Button {
                            shell.send(.showSettings(.collection(row.id)))
                        } label: {
                            HStack {
                                VStack(alignment: .leading, spacing: 3) {
                                    Text(row.title).foregroundStyle(Color.primary)
                                    Text(row.reference).font(.caption.monospaced()).foregroundStyle(.secondary)
                                    if let meta = row.meta {
                                        Text(meta).font(.subheadline).foregroundStyle(.secondary)
                                    }
                                }
                                Spacer(minLength: 8)
                            }
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        Toggle(row.title, isOn: Binding(get: { row.on }, set: { shell.send(.setCollection(row.id, on: $0)) }))
                            .labelsHidden()
                        Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(.tertiary)
                    }
                    .padding(.vertical, 2)
                }
            } header: {
                Text(collections.showTitle)
            } footer: {
                Text(collections.footer)
            }
            if !collections.newTasks.options.isEmpty {
                Section(collections.newTasksTitle) {
                    Picker(
                        collections.newTasks.label,
                        selection: Binding(get: { collections.newTasks.selected }, set: { shell.send(.setNewTasks($0)) })
                    ) {
                        ForEach(collections.newTasks.options) { Text($0.title).tag($0.id) }
                    }
                }
            }
        }
        .formStyle(.grouped)
        .navigationTitle(collections.title)
    }
}

/// One collection: what it is grouped by, how its states read, and the
/// property each field is read from.
struct CollectionView: View {
    @Environment(TasksShell.self) private var shell
    let collection: CollectionPresentation

    var body: some View {
        Form {
            Section {
                Text(collection.reference).font(.callout.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
            }
            Section(collection.groupTitle) {
                ForEach(collection.groups) { choice in
                    Button {
                        shell.send(.setGrouping(collection.kind, property: choice.id.isEmpty ? nil : choice.id))
                    } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 3) {
                                Text(choice.title).foregroundStyle(Color.primary)
                                Text(choice.detail)
                                    .font(choice.id.isEmpty ? .subheadline : .caption.monospaced())
                                    .foregroundStyle(.secondary)
                            }
                            Spacer()
                            if choice.selected {
                                Image(systemName: "checkmark").foregroundStyle(Color.brandAccent).accessibilityLabel("Chosen")
                            }
                        }
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(choice.selected ? .isSelected : [])
                }
            }
            Section(collection.statesTitle) {
                ForEach(collection.states, id: \.label) { state in
                    LabeledContent(state.label) {
                        Text(state.value)
                            .font(.caption.monospaced().weight(.semibold))
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, 8)
                            .padding(.vertical, 3)
                            .background(Color.primary.opacity(0.07), in: .capsule)
                    }
                }
            }
            Section(collection.fieldsTitle) {
                ForEach(collection.fields, id: \.label) { field in
                    LabeledContent(field.label) {
                        Text(field.value).font(field.verbatim ? .callout.monospaced() : .body).foregroundStyle(.secondary)
                    }
                }
            }
        }
        .formStyle(.grouped)
        .navigationTitle(collection.title)
    }
}
