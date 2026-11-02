import WorkoutsCore
import WorkoutsSync
import SwiftUI

extension View {
    /// The routine editor as a sheet, for the preview and the Routines list.
    func routineEditor(_ request: Binding<RoutineEditRequest?>) -> some View {
        sheet(item: request) { request in
            RoutineEditorView(model: request.model)
        }
    }
}

/// What the preview says about a routine's edits: a conflict waiting for
/// the owner's choice, an edit not saved, an edit waiting to be sent.
struct RoutineEditStatus: View {
    let routineID: RoutineID
    let resume: () -> Void

    @Environment(SubstrateLink.self) private var link

    var body: some View {
        if let conflict = link.conflict(for: routineID) {
            Section {
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
                .padding(.vertical, 4)
            }
        } else if let draft = link.draft, !draft.isNew, draft.routine.id == routineID, draft.hasChanges {
            Section {
                HStack {
                    Label("You have unsaved changes", systemImage: "pencil.circle")
                        .font(.subheadline)
                    Spacer()
                    Button("Resume", action: resume)
                        .font(.subheadline.weight(.semibold))
                }
            }
        } else if link.waitingRoutines.contains(routineID) {
            Section {} footer: {
                Label("Your changes wait to be sent to \(link.host ?? "your substrate").", systemImage: "icloud.and.arrow.up")
                    .font(.footnote)
            }
        }
    }
}
