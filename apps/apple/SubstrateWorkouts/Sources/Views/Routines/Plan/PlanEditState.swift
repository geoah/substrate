import Observation
import WorkoutsCore

/// A routine edit on screen, on the routine page or in the editor sheet:
/// the editor and its draft (`RoutineEditorModel`), and what the plan shows
/// of it. View state only; every change goes through `model.change`, which
/// keeps the draft on disk.
@MainActor
@Observable
final class PlanEditState {
    let model: RoutineEditorModel
    /// The set open in place; one at a time, as in a session.
    var openSet: RoutinePlanSetRef?
    /// The exercise picker, to add an exercise or replace one.
    var picker: PlanPicker?
    /// The reorder sheet: the blocks as a list to drag.
    var reordering = false
    /// "Custom…" of the open set's rest.
    var customRest: RoutinePlanSetRef?
    /// An editor page pushed from a menu: an exercise's, a group's, a set's.
    var route: RoutineEditorRoute?

    init(model: RoutineEditorModel, openSet: RoutinePlanSetRef? = nil) {
        self.model = model
        self.openSet = openSet
    }

    /// Changes one planned set's targets, as its panel's steps and stops do.
    func apply(_ change: RoutinePlanChange, to ref: RoutinePlanSetRef) {
        model.change { $0.updateSet(ref.slot, ref.set) { change.apply(to: &$0) } }
    }

    /// Deletes a planned set; its panel closes with it.
    func remove(_ ref: RoutinePlanSetRef) {
        guard let slot = model.editor.slot(ref.slot), let index = slot.sets.firstIndex(where: { $0.key == ref.set }) else { return }
        if openSet == ref { openSet = nil }
        model.change { $0.removeSets(of: ref.slot, at: [index]) }
    }

    /// Moves a set one place up or down among the sets of its role: warm-ups
    /// stay first.
    func move(_ ref: RoutinePlanSetRef, by offset: Int) {
        guard let target = neighbour(ref, by: offset), let slot = model.editor.slot(ref.slot),
              let index = slot.sets.firstIndex(where: { $0.key == ref.set }) else { return }
        model.change { $0.moveSets(of: ref.slot, from: [index], to: offset < 0 ? target : target + 1) }
    }

    /// The index the set would move to, or nil at its role's end.
    func neighbour(_ ref: RoutinePlanSetRef, by offset: Int) -> Int? {
        guard let slot = model.editor.slot(ref.slot), let index = slot.sets.firstIndex(where: { $0.key == ref.set }) else { return nil }
        let target = index + offset
        guard slot.sets.indices.contains(target), slot.sets[target].role == slot.sets[index].role else { return nil }
        return target
    }
}

/// What the exercise picker is for.
enum PlanPicker: Identifiable, Hashable {
    /// A new exercise at the end, or as the last member of a group.
    case add(group: String?)
    /// Another exercise in this slot.
    case replace(slot: String)

    var id: Self { self }
}
