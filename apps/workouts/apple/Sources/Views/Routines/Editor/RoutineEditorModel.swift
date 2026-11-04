import Foundation
import Observation
import WorkoutsCore

/// The routine edit on screen: the editor and its draft on disk. Every
/// change is kept at once (`SubstrateLink.keepDraft`), so killing the app
/// mid-edit loses nothing; Save sends it, Cancel drops it.
///
/// Explicit Save and Cancel rather than autosave: an edit is often several
/// steps (move an exercise out of a group, into another, fix its sets), and
/// autosave would send each half-done step to the server, where the coach
/// reads it and each write moves the version another device edits against.
/// One Save is one guarded write.
@MainActor
@Observable
final class RoutineEditorModel: Identifiable {
    private(set) var editor: RoutineEditor
    let id = UUID()
    @ObservationIgnored private let keep: (RoutineEditor) -> Void

    init(_ editor: RoutineEditor, keep: @escaping (RoutineEditor) -> Void) {
        self.editor = editor
        self.keep = keep
    }

    var routine: Routine { editor.routine }

    /// Changes the routine and keeps the draft.
    func change(_ edit: (inout RoutineEditor) -> Void) {
        var next = editor
        edit(&next)
        guard next != editor else { return }
        editor = next
        keep(next)
    }
}

/// An edit to open: the routine's own, or a duplicate.
struct RoutineEditRequest: Identifiable {
    let model: RoutineEditorModel
    var id: UUID { model.id }
}
