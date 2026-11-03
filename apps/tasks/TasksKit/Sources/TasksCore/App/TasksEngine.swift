import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// The root's engine: every event applied to the state, purely; the records'
/// own events by the kit's `LinkedRecords`.
public extension TasksApp {
    func apply(_ event: Event, to state: TasksState) throws -> TasksState {
        var next = state
        switch event {
        case let .records(event):
            next.linked = linked.apply(event, to: state.linked)
            if case let .notQueued(_, words) = event { next.problem = words }
        case .opening:
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
            if section == TasksMenu.today { next.selection = .today }
            if section == TasksMenu.lists, next.selection == .today { next.selection = nil }
        case let .shown(selection):
            guard selection != state.selection || state.openTask != nil else { break }
            next.selection = selection
            next.openTask = nil
            next.tapped = nil
        case let .opened(task):
            guard task != state.openTask else { break }
            next.openTask = task
            next.tapped = nil
        case let .editorOpened(key, seed):
            next.editors[key] = seed
        case let .editorSaved(key, write, _):
            next.editors[key] = nil
            next.linked = linked.apply(.wrote(write), to: next.linked)
        case let .editorClosed(key):
            next.editors[key] = nil
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
        if next.records != state.records || next.settings != state.settings || next.selection != state.selection {
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
