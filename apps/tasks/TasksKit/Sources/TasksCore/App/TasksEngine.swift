import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// The root's engine: every event applied to the state, purely, after the
/// records' own events are applied by the kit's `LinkedRecords`
/// (`LinkedRecordsApp`): `state` holds them, `previous` is from before.
public extension TasksApp {
    func apply(_ event: Event, to state: TasksState, from previous: TasksState) throws -> TasksState {
        var next = state
        switch event {
        case let .records(.enqueued(ids, _, _)):
            // The outbox has an editor's write: its editor closes.
            for (key, save) in state.saves where save.writes(ids) {
                next.editors[key] = nil
                next.saves[key] = nil
            }
        case let .records(.notQueued(ids, words)):
            // An editor's write is said in the editor, beside its draft;
            // any other in the app's problem.
            let editors = state.saves.filter { $0.value.writes(ids) }.keys
            for key in editors { next.saves[key] = .notKept(words) }
            if editors.isEmpty { next.problem = words }
        case let .records(.wrote(write)):
            // A completion's write went to the outbox: its window is over.
            for (id, pending) in state.completing where pending == write { next.completing[id] = nil }
        case .records:
            break
        case let .settingsRead(settings, usage):
            next.settings = settings
            next.usage = usage
            next.settingsRead = true
        // A view telling the state it already holds (a tab bar or a list
        // drawn again) moves nothing, and forgets no tap waiting for its
        // task.
        case let .sectionSelected(section):
            guard section != state.section else { break }
            next.openTask = nil
            next.section = section
            next.tapped = nil
            if section == TasksMenu.needsYou { next.selection = .needsYou }
            if section == TasksMenu.lists, next.selection == .needsYou { next.selection = nil }
        case let .shown(selection):
            guard selection != state.selection || state.openTask != nil else { break }
            next.selection = selection
            next.openTask = nil
            next.tapped = nil
        case let .filtered(filter):
            next.filter = filter
        case let .completing(id, write):
            next.completing[id] = write
        case let .uncompleted(id):
            next.completing[id] = nil
        case let .opened(task):
            guard task != state.openTask else { break }
            next.openTask = task
            next.tapped = nil
        case let .editorOpened(key, seed):
            next.editors[key] = seed
        case let .editorSaving(key, ids):
            next.saves[key] = .sending(ids)
        case let .editorClosed(key):
            next.editors[key] = nil
            next.saves[key] = nil
        case let .asked(confirmation):
            next.confirmation = confirmation
        case .answered:
            next.confirmation = nil
        case let .settingsChanged(settings):
            next.settings = settings
        case let .permission(permission):
            next.permission = permission
        case let .scheduled(plan):
            next.scheduled = plan
        case let .tapped(id):
            next.tapped = id
        case let .usage(usage):
            next.usage = usage
        case let .offered(version):
            next.offered = version
        case .problemDismissed:
            next.problem = nil
        }
        openTapped(&next)
        if next.records != previous.records || next.settings != previous.settings || next.selection != previous.selection
            || next.filter != previous.filter || next.completing != previous.completing
        {
            next.revision += 1
        }
        return next
    }

    /// A tapped reminder's task, once its record is there: the Lists section
    /// on its list, the task open.
    private func openTapped(_ state: inout TasksState) {
        guard let id = state.tapped, let record = state.records[RecordFold.key(TasksKinds.task, id)] else { return }
        state.section = TasksMenu.lists
        state.selection = TasksCodec.listID(record.properties["list"]).map(ListSelection.list) ?? .inbox
        state.openTask = id
        state.tapped = nil
    }
}

extension EditorSave {
    /// A save on its way whose write has one of `ids`.
    func writes(_ ids: Set<UUID>) -> Bool {
        if case let .sending(entries) = self { !entries.isDisjoint(with: ids) } else { false }
    }
}
