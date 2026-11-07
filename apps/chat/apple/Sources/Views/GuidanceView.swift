import ChatKit
import SubstrateKit
import SwiftUI

/// Settings → Assistant: the owner's memories and instructions, a screen
/// for each.
struct GuidanceSectionView: View {
    let section: GuidanceSection

    var body: some View {
        Section {
            NavigationLink {
                GuidanceListView(list: \.memories)
            } label: {
                LabeledContent(section.memories.title, value: section.memories.count)
            }
            NavigationLink {
                GuidanceListView(list: \.instructions)
            } label: {
                LabeledContent(section.instructions.title, value: section.instructions.count)
            }
            if let problem = section.problem {
                Text(problem).font(.footnote).foregroundStyle(.red)
            }
        } header: {
            Text(section.title)
        } footer: {
            Text(section.footer)
        }
    }
}

/// One list, drawn as the host presents it now: each row's switch, Edit and
/// Delete send a change, and what the server says of it comes back as the
/// row.
struct GuidanceListView: View {
    @Environment(ChatHost.self) private var host
    let list: KeyPath<GuidanceSection, GuidanceList>
    @State private var editing: GuidanceRow?
    @State private var deleting: GuidanceRow?

    var body: some View {
        if let section = host.chats?.settings.guidance {
            let shown = section[keyPath: list]
            Form {
                Section {
                    if let loading = section.loading {
                        HStack(spacing: 8) {
                            ProgressView().controlSize(.small)
                            Text(loading).foregroundStyle(.secondary)
                        }
                    } else if shown.rows.isEmpty {
                        Text(shown.empty).foregroundStyle(.secondary)
                    }
                    ForEach(shown.rows) { row in
                        GuidanceRowView(
                            row: row,
                            active: Binding(
                                get: { row.active },
                                set: { host.send(.changeGuidance(row.reference, .active($0))) })
                        )
                        .swipeActions {
                            Button("Delete", systemImage: "trash", role: .destructive) { deleting = row }
                            Button("Edit", systemImage: "pencil") { editing = row }
                        }
                        .contextMenu {
                            Button("Edit…", systemImage: "pencil") { editing = row }
                            Button("Delete…", systemImage: "trash", role: .destructive) { deleting = row }
                        }
                    }
                    if let problem = section.problem {
                        ProblemBox(message: problem)
                    }
                } footer: {
                    Text(shown.footer)
                }
            }
            .formStyle(.grouped)
            .navigationTitle(shown.title)
            .task { host.send(.readGuidance) }
            .sheet(item: $editing) { row in
                GuidanceEditor(title: shown.editTitle, text: row.text) { host.send(.changeGuidance(row.reference, .text($0))) }
            }
            .confirmationDialog(
                shown.deleteQuestion, isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                titleVisibility: .visible, presenting: deleting
            ) { row in
                Button("Delete", role: .destructive) { host.send(.changeGuidance(row.reference, .delete)) }
            } message: { _ in
                Text(shown.deleteMessage)
            }
        }
    }
}

/// A memory or an instruction: its words, its facts each labelled, and
/// whether the agents read it.
private struct GuidanceRowView: View {
    let row: GuidanceRow
    @Binding var active: Bool

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(row.text)
                    .foregroundStyle(row.active ? .primary : .secondary)
                    .lineLimit(6)
                HStack(spacing: 10) {
                    ForEach(row.facts, id: \.self) { fact in
                        HStack(spacing: 3) {
                            Text(fact.label).fontWeight(.semibold)
                            Text(fact.value)
                        }
                    }
                }
                .font(.caption)
                .foregroundStyle(.secondary)
                if let problem = row.problem {
                    Text(problem).font(.caption).foregroundStyle(.red)
                }
            }
            Spacer(minLength: 8)
            if row.busy {
                ProgressView().controlSize(.small)
            } else {
                Toggle(row.text, isOn: $active).labelsHidden()
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// The words of one memory or instruction, edited whole.
private struct GuidanceEditor: View {
    @Environment(\.dismiss) private var dismiss
    let title: String
    @State var text: String
    let save: (String) -> Void

    var body: some View {
        NavigationStack {
            Form {
                TextField(title, text: $text, axis: .vertical)
                    .lineLimit(3 ... 12)
                    .labelsHidden()
            }
            .formStyle(.grouped)
            .navigationTitle(title)
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        save(text)
                        dismiss()
                    }
                    .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
        }
        #if os(macOS)
        .frame(minWidth: 420, minHeight: 240)
        #endif
    }
}
