import WorkoutsCore
import SwiftUI

/// Where an edit's stack goes: a plain exercise, a group, a planned set.
enum RoutineEditorRoute: Hashable {
    case slot(String)
    case group(String)
    case set(slot: String, set: String)
}

/// The routine editor as a sheet, for a duplicate and for an edit resumed
/// from the Routines list: the routine page's own editing (the name, folder
/// and notes, then the plan as a session draws it, each set opening in
/// place), with Save and Cancel. The routine page edits in place with the
/// same pieces.
struct RoutineEditorView: View {
    let model: RoutineEditorModel

    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var edit: PlanEditState

    init(model: RoutineEditorModel) {
        self.model = model
        _edit = State(initialValue: PlanEditState(model: model))
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    RoutineDetailsFields(model: model)
                    RoutinePlanCards(plan: RoutinePlanPresenter.present(model.routine, exercises: app.exercises, history: app.historyIndex,
                                                                        rests: app.settings.session),
                                     edit: edit)
                }
                .padding(.horizontal, 16)
                .padding(.bottom, 24)
            }
            .background(Color(.systemGroupedBackground))
            .navigationTitle(model.editor.isNew ? "New routine" : "Edit routine")
            .navigationBarTitleDisplayMode(.inline)
            .routineEditControls(model) { dismiss() }
            .planEditing(edit)
        }
        .interactiveDismissDisabled(model.editor.hasChanges)
    }
}

/// An exercise added on this iPhone goes to the substrate before the
/// routine that names it: the outbox writes it first.
@MainActor
func sendIfLocal(_ exercise: Exercise, link: SubstrateLink) {
    guard exercise.isLocal else { return }
    Task { await link.exerciseAdded(exercise) }
}

#if DEBUG
#Preview("Duplicate") {
    let app = PreviewData.app()
    let routine = PreviewData.catalog.routines[SampleRoutine.id]!
    RoutineEditorView(model: RoutineEditorModel(RoutineEditor.duplicate(routine, taken: [routine.id])) { _ in })
        .environment(app)
        .environment(PreviewData.noSession(app))
        .environment(SubstrateLink.shared)
}
#endif
