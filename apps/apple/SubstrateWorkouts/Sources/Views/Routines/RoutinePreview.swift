import WorkoutsCore
import SwiftUI

/// A routine before it starts (DESIGN.md §7.3): each exercise's planned sets
/// in words, groups with their bracket and A1, A2 labels, the rests that
/// differ from the defaults, tempo and notes. Start floats in a bar of its
/// own at the bottom of the list, above the tab bar and the session's bar
/// when one runs: the list scrolls clear of all three, and neither glass
/// ever lies over Start. A routine on the signed-in substrate has Edit in
/// the toolbar, and Duplicate and Delete in its menu; signed out, the sample
/// the app carries cannot be edited, and says so.
struct RoutinePreview: View {
    let routineID: RoutineID

    @Environment(AppModel.self) private var app
    @Environment(SubstrateLink.self) private var link
    @Environment(\.dismiss) private var dismiss
    @State private var deleting: RoutineToDelete?
    @State private var editing: RoutineEditRequest?
    @State private var replacingDraft: RoutineEditor?

    var body: some View {
        if let catalog = app.catalog, let routine = catalog.routines[routineID] {
            let preview = RoutinePresenter.preview(routine, catalog: catalog, history: app.historyIndex,
                                                   settings: app.presenterSettings)
            list(preview)
                .navigationTitle(preview.name)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    if canEdit {
                        ToolbarItem(placement: .topBarTrailing) {
                            Button("Edit") { edit(RoutineEditor(routine)) }
                                .disabled(link.conflict(for: routineID) != nil)
                        }
                        ToolbarItem(placement: .topBarTrailing) {
                            Menu("More", systemImage: "ellipsis") {
                                Button("Duplicate", systemImage: "plus.square.on.square") {
                                    edit(RoutineEditor.duplicate(routine, taken: Set(catalog.routines.keys)))
                                }
                                Button("Delete Routine", systemImage: "trash", role: .destructive) {
                                    deleting = RoutineToDelete(id: routineID, name: preview.name)
                                }
                            }
                        }
                    }
                }
                .confirmsRoutineDelete($deleting) { dismiss() }
                .routineEditor($editing)
                .confirmationDialog(replacingDraft.map { _ in "Discard your unsaved edit of \u{201C}\(link.draft?.routine.name ?? "")\u{201D}?" } ?? "",
                                    isPresented: Binding { replacingDraft != nil } set: { if !$0 { replacingDraft = nil } },
                                    titleVisibility: .visible, presenting: replacingDraft) { editor in
                    Button("Discard and Edit", role: .destructive) {
                        replacingDraft = nil
                        open(editor)
                    }
                    Button("Cancel", role: .cancel) { replacingDraft = nil }
                }
                .safeAreaBar(edge: .bottom) {
                    StartSessionButton(request: .routine(routineID)) {
                        Text("Start routine")
                            .font(.body.weight(.semibold))
                            .frame(maxWidth: .infinity, minHeight: 36)
                    }
                    .buttonStyle(.glassProminent)
                    .padding(.horizontal, 20)
                    .padding(.bottom, 8)
                }
        } else {
            ContentUnavailableView("Routine not found", systemImage: "list.bullet.clipboard")
        }
    }

    /// Routines on the signed-in substrate can be edited, duplicated and
    /// deleted; the sample the app carries signed out cannot.
    private var canEdit: Bool {
        guard case .server = app.catalogOrigin else { return false }
        return link.canDeleteRoutines
    }

    /// Opens the editor: the unsaved draft of this routine when there is
    /// one, else `editor`. A draft of another routine is replaced only
    /// after the owner says so.
    private func edit(_ editor: RoutineEditor) {
        if let draft = link.draft, draft.hasChanges {
            if !editor.isNew, !draft.isNew, draft.routine.id == routineID { return open(draft) }
            replacingDraft = editor
            return
        }
        open(editor)
    }

    private func open(_ editor: RoutineEditor) {
        editing = RoutineEditRequest(model: RoutineEditorModel(editor) { [link] in link.keepDraft($0) })
        // A duplicate is a change already: kept before the first edit.
        if editor.hasChanges { link.keepDraft(editor) }
    }

    private func list(_ preview: RoutinePreviewPresentation) -> some View {
        List {
            RoutineEditStatus(routineID: routineID) { if let draft = link.draft { open(draft) } }
            if preview.notes != nil || preview.lastDone != nil {
                Section {
                    VStack(alignment: .leading, spacing: 6) {
                        if let notes = preview.notes {
                            Text(notes)
                                .font(.subheadline)
                        }
                        if let lastDone = preview.lastDone {
                            Text(lastDone)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
            ForEach(preview.blocks) { block in
                Section {
                    ForEach(block.slots) { slot in
                        SlotLines(slot: slot, color: block.header == nil ? nil : GroupPalette.color(block.colorIndex))
                    }
                } header: {
                    if let header = block.header {
                        Text(header)
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(GroupPalette.color(block.colorIndex))
                            .textCase(nil)
                    }
                }
            }
            if case .bundled = app.catalogOrigin {
                Section {} footer: {
                    Text("This sample comes with the app and can\u{2019}t be changed. Sign in to your substrate to edit routines or make your own copy.")
                }
            }
            if !preview.unreadable.isEmpty {
                Section("Could not be read") {
                    ForEach(preview.unreadable, id: \.self) { line in
                        Text(line)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
    }
}

/// One exercise of the preview: the name (with its group label), then one
/// line per kind of planned set, then its notes.
private struct SlotLines: View {
    let slot: RoutinePreviewSlot
    let color: Color?

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            if let color {
                RoundedRectangle(cornerRadius: 2)
                    .fill(color)
                    .frame(width: 4)
                    .accessibilityHidden(true)
            }
            VStack(alignment: .leading, spacing: 4) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    if let label = slot.groupLabel {
                        Text(label)
                            .font(.subheadline.weight(.bold))
                            .foregroundStyle(color ?? .secondary)
                    }
                    Text(slot.name)
                        .font(.headline)
                }
                ForEach(Array(slot.lines.enumerated()), id: \.offset) { _, line in
                    Text(line)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                if let notes = slot.notes {
                    Text(notes)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .italic()
                }
            }
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
    }
}

#if DEBUG
#Preview {
    let app = PreviewData.app()
    NavigationStack {
        RoutinePreview(routineID: SampleRoutine.id)
    }
    .environment(app)
    .environment(PreviewData.noSession(app))
}
#endif
