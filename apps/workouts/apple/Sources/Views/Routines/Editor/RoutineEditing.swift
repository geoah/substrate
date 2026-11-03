import WorkoutsCore
import WorkoutsSync
import SwiftUI

extension View {
    /// The routine editor as a sheet, for a duplicate and an edit resumed from the Routines list.
    func routineEditor(_ request: Binding<RoutineEditRequest?>) -> some View {
        sheet(item: request) { request in
            RoutineEditorView(model: request.model)
        }
    }

    /// Save and Cancel of a routine edit, on the routine page and in the
    /// editor sheet; nothing while `model` is nil. `close` ends the edit
    /// once it is saved or dropped.
    func routineEditControls(_ model: RoutineEditorModel?, close: @escaping () -> Void) -> some View {
        modifier(RoutineEditControls(model: model, close: close))
    }
}

/// Save sends one guarded write through the outbox (a new routine whole),
/// and is refused while the routine has a problem or a conflict waits for
/// the owner; the reason shows at the bottom. Cancel drops the draft,
/// asking first when it holds changes.
private struct RoutineEditControls: ViewModifier {
    let model: RoutineEditorModel?
    let close: () -> Void

    @Environment(SubstrateLink.self) private var link
    @State private var saving = false
    @State private var confirmingCancel = false
    @State private var failure: String?

    func body(content: Content) -> some View {
        if let model {
            content
                .disabled(saving)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel", role: .cancel) {
                            if model.editor.hasChanges { confirmingCancel = true } else { discard() }
                        }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Save") { save(model) }
                            .disabled(!model.editor.hasChanges || blocked(model) != nil || saving)
                    }
                }
                .safeAreaBar(edge: .bottom) {
                    if let problem = blocked(model), model.editor.hasChanges {
                        Label(problem, systemImage: "exclamationmark.circle")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 10)
                            .frame(maxWidth: .infinity)
                            .background(.bar)
                    }
                }
                .confirmationDialog("Discard your changes?", isPresented: $confirmingCancel, titleVisibility: .visible) {
                    Button("Discard Changes", role: .destructive, action: discard)
                    Button("Keep Editing", role: .cancel) {}
                }
                .alert("The routine was not saved", isPresented: Binding { failure != nil } set: { if !$0 { failure = nil } }) {
                    Button("OK", role: .cancel) {}
                } message: {
                    Text(failure ?? "")
                }
        } else {
            content
        }
    }

    /// What stops Save: the routine's own problem, or a conflict waiting
    /// for the owner's choice.
    private func blocked(_ model: RoutineEditorModel) -> String? {
        if link.conflict(for: model.routine.id) != nil { return SubstrateLink.conflictFirst }
        return model.editor.problem
    }

    private func save(_ model: RoutineEditorModel) {
        saving = true
        let editor = model.editor
        Task {
            defer { saving = false }
            do {
                try await link.saveRoutine(editor)
                close()
            } catch {
                failure = error.localizedDescription
            }
        }
    }

    private func discard() {
        link.dropDraft()
        close()
    }
}

/// The routine's name, folder and notes while it is edited, in one card,
/// each field labelled as the page shows it.
struct RoutineDetailsFields: View {
    let model: RoutineEditorModel

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            field("Name") {
                TextField("Name", text: Binding { model.routine.name } set: { value in model.change { $0.rename(value) } })
                    .font(.headline)
            }
            Divider().padding(.leading, 16)
            field("Folder") {
                TextField("None", text: Binding { model.routine.folder ?? "" } set: { value in model.change { $0.setFolder(value) } })
            }
            Divider().padding(.leading, 16)
            field("Notes") {
                TextField("What the routine is for", text: Binding { model.routine.notes ?? "" } set: { value in
                    model.change { $0.setNotes(value) }
                }, axis: .vertical)
                    .lineLimit(1 ... 8)
                    .font(.subheadline)
            }
        }
        .background(RowsStyle.card, in: .rect(cornerRadius: RowsStyle.cardRadius))
    }

    private func field(_ label: String, @ViewBuilder _ content: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.caption)
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            content()
                .accessibilityLabel(label)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, minHeight: 56, alignment: .leading)
    }
}

/// What the routine page says about the routine's edits: a conflict waiting
/// for the owner's choice, an edit not saved, an edit waiting to be sent.
struct RoutineEditStatus: View {
    let routineID: RoutineID
    let resume: () -> Void

    @Environment(SubstrateLink.self) private var link

    var body: some View {
        if let conflict = link.conflict(for: routineID) {
            VStack(alignment: .leading, spacing: 8) {
                Label("Your edit was not saved", systemImage: "exclamationmark.triangle.fill")
                    .font(.subheadline.weight(.semibold))
                Text("This routine changed on \(link.host ?? "your substrate") since you began editing it, by the coach or another device.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                HStack {
                    Button("Keep My Edit") { Task { await link.resolve(conflict, keepMine: true) } }
                        .buttonStyle(.borderedProminent)
                    Button("Use Theirs") { Task { await link.resolve(conflict, keepMine: false) } }
                        .buttonStyle(.bordered)
                }
                .font(.subheadline)
                Text("Keep My Edit replaces what you changed, such as the exercises or the name; what you did not change stays as it is now.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.orange.opacity(0.15), in: .rect(cornerRadius: 20))
        } else if let draft = link.draft, !draft.isNew, draft.routine.id == routineID, draft.hasChanges {
            HStack(spacing: 12) {
                Label("You have unsaved changes", systemImage: "pencil.circle")
                    .font(.subheadline)
                Spacer(minLength: 8)
                Button("Resume", action: resume)
                    .font(.subheadline.weight(.semibold))
                    .frame(minHeight: 44)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 4)
            .background(RowsStyle.card, in: .rect(cornerRadius: 20))
        } else if link.waitingRoutines.contains(routineID) {
            Label("Your changes wait to be sent to \(link.host ?? "your substrate").", systemImage: "icloud.and.arrow.up")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 4)
        }
    }
}
