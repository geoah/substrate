import WorkoutsCore
import SwiftUI

/// Where the editor's stack goes: a plain exercise, a group, a planned set.
enum RoutineEditorRoute: Hashable {
    case slot(String)
    case group(String)
    case set(slot: String, set: String)
}

/// The routine editor, a sheet over the preview: the name, folder and
/// notes, then the exercises as blocks (a plain exercise, or a group with
/// its members) to drag, swipe away, group with the next or open. Save
/// sends one guarded write through the outbox; Cancel drops the draft.
struct RoutineEditorView: View {
    @Bindable var model: RoutineEditorModel

    @Environment(AppModel.self) private var app
    @Environment(SubstrateLink.self) private var link
    @Environment(\.dismiss) private var dismiss
    @State private var path: [RoutineEditorRoute] = []
    @State private var adding = false
    @State private var confirmingCancel = false
    @State private var saving = false
    @State private var failure: String?

    var body: some View {
        NavigationStack(path: $path) {
            List {
                details
                exercises
            }
            .listStyle(.insetGrouped)
            .navigationTitle(model.editor.isNew ? "New routine" : "Edit routine")
            .navigationBarTitleDisplayMode(.inline)
            .navigationDestination(for: RoutineEditorRoute.self) { route in
                switch route {
                case let .slot(key): SlotEditorView(model: model, slotKey: key)
                case let .group(key): GroupEditorView(model: model, groupKey: key)
                case let .set(slot, set): PlannedSetEditorView(model: model, slotKey: slot, setKey: set)
                }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) {
                        if model.editor.hasChanges { confirmingCancel = true } else { close() }
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save", action: save)
                        .disabled(!model.editor.hasChanges || blocked != nil || saving)
                }
            }
            .disabled(saving)
            .safeAreaInset(edge: .bottom) {
                if let problem = blocked, model.editor.hasChanges {
                    Text(problem)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .padding(8)
                        .frame(maxWidth: .infinity)
                        .background(.bar)
                }
            }
            .sheet(isPresented: $adding) {
                ExercisePicker(title: "Add exercise") { exercise in
                    model.change { $0.addExercise(exercise) }
                    sendIfLocal(exercise, link: link)
                }
            }
            .confirmationDialog("Discard your changes?", isPresented: $confirmingCancel, titleVisibility: .visible) {
                Button("Discard Changes", role: .destructive, action: close)
                Button("Keep Editing", role: .cancel) {}
            }
            .alert("The routine was not saved", isPresented: Binding { failure != nil } set: { if !$0 { failure = nil } }) {
                Button("OK", role: .cancel) {}
            } message: {
                Text(failure ?? "")
            }
        }
        .interactiveDismissDisabled(model.editor.hasChanges)
    }

    private var details: some View {
        Section {
            TextField("Name", text: Binding { model.routine.name } set: { value in model.change { $0.rename(value) } })
                .font(.headline)
            TextField("Folder", text: Binding { model.routine.folder ?? "" } set: { value in model.change { $0.setFolder(value) } })
            TextField("Notes", text: Binding { model.routine.notes ?? "" } set: { value in model.change { $0.setNotes(value) } },
                      axis: .vertical)
                .lineLimit(2 ... 8)
                .font(.subheadline)
        }
    }

    private var exercises: some View {
        let blocks = model.editor.blocks
        return Section {
            ForEach(Array(blocks.enumerated()), id: \.element.id) { index, block in
                NavigationLink(value: block.group.map(RoutineEditorRoute.group) ?? .slot(block.slots[0])) {
                    BlockRow(model: model, block: block, colorIndex: colorIndex(of: block, in: blocks))
                }
                .swipeActions(edge: .leading) {
                    if index + 1 < blocks.count {
                        Button("Group with Next", systemImage: "link") {
                            withAnimation { model.change { $0.groupWithNext(index) } }
                        }
                        .tint(.indigo)
                    }
                    if let group = block.group {
                        Button("Ungroup", systemImage: "link.badge.plus") {
                            withAnimation { model.change { $0.ungroup(group) } }
                        }
                    }
                }
                .contextMenu {
                    if index + 1 < blocks.count {
                        Button("Group with Next", systemImage: "link") { model.change { $0.groupWithNext(index) } }
                    }
                    if let group = block.group {
                        Button("Ungroup", systemImage: "rectangle.split.1x2") { model.change { $0.ungroup(group) } }
                    }
                    Button("Remove", systemImage: "trash", role: .destructive) {
                        model.change { $0.removeBlocks(at: [index]) }
                    }
                }
            }
            .onMove { source, destination in model.change { $0.moveBlocks(from: source, to: destination) } }
            .onDelete { offsets in model.change { $0.removeBlocks(at: offsets) } }
            Button {
                adding = true
            } label: {
                Label("Add exercise", systemImage: "plus")
            }
        } header: {
            HStack {
                Text("Exercises")
                Spacer()
                EditButton()
                    .font(.subheadline)
                    .textCase(nil)
            }
        } footer: {
            Text("Drag to reorder. Swipe right to group an exercise with the next one.")
        }
    }

    /// What stops Save: the routine's own problem, or a conflict waiting
    /// for the owner's choice.
    private var blocked: String? {
        if link.conflict(for: model.routine.id) != nil { return SubstrateLink.conflictFirst }
        return model.editor.problem
    }

    /// The group's colour, counted over the groups before it, as the preview counts.
    private func colorIndex(of block: RoutineEditor.Block, in blocks: [RoutineEditor.Block]) -> Int? {
        guard block.group != nil, let index = blocks.firstIndex(of: block) else { return nil }
        return blocks[..<index].count { $0.group != nil }
    }

    private func save() {
        saving = true
        let editor = model.editor
        Task {
            defer { saving = false }
            do {
                try await link.saveRoutine(editor)
                dismiss()
            } catch {
                failure = error.localizedDescription
            }
        }
    }

    private func close() {
        link.dropDraft()
        dismiss()
    }
}

/// An exercise added on this iPhone goes to the substrate before the
/// routine that names it: the outbox writes it first.
@MainActor
func sendIfLocal(_ exercise: Exercise, link: SubstrateLink) {
    guard exercise.isLocal else { return }
    Task { await link.exerciseAdded(exercise) }
}

/// One block of the editor's list: a plain exercise with its sets in words,
/// or a group's header and its members.
private struct BlockRow: View {
    let model: RoutineEditorModel
    let block: RoutineEditor.Block
    let colorIndex: Int?

    @Environment(AppModel.self) private var app

    var body: some View {
        if let key = block.group, let group = model.editor.group(key) {
            let color = GroupPalette.color(colorIndex)
            VStack(alignment: .leading, spacing: 4) {
                Text(GroupEditorView.title(group))
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(color)
                ForEach(Array(block.slots.enumerated()), id: \.element) { position, slotKey in
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(groupMemberLabel(group: key, position: position + 1))
                            .font(.subheadline.weight(.bold))
                            .foregroundStyle(color)
                        Text(name(slotKey))
                            .font(.subheadline)
                    }
                }
            }
            .padding(.vertical, 2)
        } else if let slot = model.editor.slot(block.slots[0]) {
            VStack(alignment: .leading, spacing: 2) {
                Text(name(slot.key))
                    .font(.headline)
                ForEach(RoutinePresenter.editorLines(slot, routine: model.routine, catalog: app.catalog ?? Catalog()), id: \.self) { line in
                    Text(line)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    private func name(_ slotKey: String) -> String {
        guard let slot = model.editor.slot(slotKey) else { return slotKey }
        return app.exercises[slot.exercise]?.name ?? slot.exercise.rawValue
    }
}
