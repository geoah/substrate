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

    var approval: ApprovalPresentation?

    var body: some View {
        Form {
            Section {
                Text(collections.intro).font(.subheadline).foregroundStyle(.secondary)
            }
            if let approval { ApprovalAskSection(approval: approval) }
            Section {
                ForEach(collections.rows) { row in
                    // One switch, labelled by the row's words, and one button
                    // that opens the collection's page.
                    HStack(alignment: .center, spacing: 12) {
                        Toggle(isOn: Binding(get: { row.on }, set: { shell.send(.setCollection(row.id, on: $0)) })) {
                            VStack(alignment: .leading, spacing: 3) {
                                Text(row.title).foregroundStyle(Color.primary)
                                Text(row.reference).font(.caption.monospaced()).foregroundStyle(.secondary)
                                if let meta = row.meta {
                                    Text(meta).font(.subheadline).foregroundStyle(.secondary)
                                }
                            }
                        }
                        Button {
                            shell.send(.showSettings(.collection(row.id)))
                        } label: {
                            Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(.tertiary)
                                .frame(minWidth: 32, minHeight: 44)
                                .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(row.title)
                        .accessibilityHint(row.reference)
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
    var approval: ApprovalPresentation?

    var body: some View {
        Form {
            Section {
                Text(collection.reference).font(.callout.monospaced()).foregroundStyle(.secondary).textSelection(.enabled)
            }
            if let approval, approval.collection == collection.kind {
                ApprovalAskSection(approval: approval)
            } else if let line = collection.approval {
                ApprovalLineSection(line: line)
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

/// A page's approval line and its Approve.
struct ApprovalLineSection: View {
    @Environment(TasksShell.self) private var shell
    let line: ApprovalLine

    var body: some View {
        Section {
            Label(line.message, systemImage: "lock")
                .fixedSize(horizontal: false, vertical: true)
            Button(line.approve) { shell.request(line) }
                .font(.body.weight(.semibold))
        }
    }
}

/// The approval the app asked for when a collection was turned on or
/// grouped, or that it was not given: said where it was asked, on the
/// Collections pages.
struct ApprovalAskSection: View {
    @Environment(TasksShell.self) private var shell
    let approval: ApprovalPresentation

    var body: some View {
        if !approval.waiting {
            Section {
                Label(approval.refused ?? approval.message, systemImage: approval.refused == nil ? "lock" : "exclamationmark.triangle")
                    .fixedSize(horizontal: false, vertical: true)
                if approval.refused == nil {
                    Button(approval.approve) { shell.approve(approval) }
                        .font(.body.weight(.semibold))
                }
                Button(approval.cancel, role: .cancel) { shell.send(.dismissApproval) }
            }
        }
    }
}
