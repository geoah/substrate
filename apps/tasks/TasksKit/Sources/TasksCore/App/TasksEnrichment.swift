import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// The root's commands as events: each write made here, where the instant,
/// new ids and the records as they stand may be read, so the engine only
/// lays it over.
public extension TasksApp {
    func events(for command: Command, in context: Enrichment<TasksApp>) -> [Event] {
        let state = context.state
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = context.environment.timeZone
        calendar.locale = context.environment.locale
        let model = TasksModel(state.records, in: calendar)
        let today = CivilDay(context.now, in: calendar)
        let writes = TaskWrites(mint: { context.mint() })
        let wrote = { (write: LocalWrite) in Event.records(.wrote(write)) }

        switch command {
        case let .select(section):
            return [.sectionSelected(section)]
        case let .show(selection):
            return [.shown(selection)]
        case let .open(task):
            return [.opened(task)]
        case let .filter(filter):
            guard filter != state.filter, filter.flatMap(\.listID).map({ model.lists[$0] != nil }) ?? true else { return [] }
            return [.filtered(filter)]
        case let .quickAdd(name):
            let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !name.isEmpty else { return [] }
            // On Needs you a task goes into the list it is narrowed to and is
            // due today, so it lands where the owner is looking.
            let selection = state.selection == .needsYou ? state.filter ?? .needsYou : state.selection ?? .inbox
            let list = selection.listID
            let task = TaskItem(
                id: writes.newID(), name: name, list: list, position: Position.between(model.lastPosition(inList: list), nil),
                due: state.selection == .needsYou ? .day(today) : nil)
            return [wrote(writes.create(task))]
        case .newTask:
            let selection = state.selection == .needsYou ? state.filter ?? .needsYou : state.selection ?? .inbox
            return [.editorOpened(ChildKey(writes.newID()), .new(list: selection.listID, day: state.selection == .needsYou ? today : nil))]
        case let .edit(id):
            guard model.tasks[id] != nil else { return [] }
            return [.editorOpened(ChildKey(writes.newID()), .task(id))]
        case let .complete(id):
            guard state.completing[id] == nil, let event = status(id, .done, model, writes).first, case let .records(.wrote(write)) = event else {
                return []
            }
            return [.completing(id, write)]
        case let .reopen(id):
            guard state.completing[id] == nil else { return [.uncompleted(id)] }
            return status(id, .open, model, writes)
        case let .abandon(id):
            return undone(id, state) + status(id, .abandoned, model, writes)
        case let .reschedule(id, how):
            guard let task = model.tasks[id] else { return [] }
            guard how != .pickDay else { return [.editorOpened(ChildKey(writes.newID()), .task(id))] }
            let day =
                switch how {
                case .today: today
                case .tomorrow: today.adding(days: 1, in: calendar)
                case .nextWeek, .pickDay: today.adding(days: 7, in: calendar)
                }
            var next = task
            // A timed task keeps its time of day on the new day.
            if let instant = task.due?.instant {
                let clock = calendar.dateComponents([.hour, .minute], from: instant)
                next.due = .time(day.start(in: calendar, hour: clock.hour ?? 0).addingTimeInterval(Double(clock.minute ?? 0) * 60), day: day)
            } else {
                next.due = .day(day)
            }
            guard next.due != task.due else { return [] }
            return [wrote(writes.change(task, to: next))]
        case let .setPriority(id, priority):
            guard let task = model.tasks[id], task.priority != priority else { return [] }
            var next = task
            next.priority = priority
            return [wrote(writes.change(task, to: next))]
        case let .move(id, list):
            guard let task = model.tasks[id], task.list != list, list.map({ model.lists[$0] != nil }) ?? true else { return [] }
            var next = task
            next.list = list
            next.position = Position.between(model.lastPosition(inList: list), nil)
            return [wrote(writes.change(task, to: next))]
        case let .reorderTask(id, index):
            guard let selection = state.selection, selection != .needsYou, TasksSettings.order(state.settings) == .manual else { return [] }
            let open = TaskOrder.manual.sorted(model.tasks(in: selection).filter(\.isOpen), in: calendar)
            let moves = Position.moves(id, to: index, in: open.map(\.id), positions: open.reduce(into: [:]) { $0[$1.id] = $1.position })
            let changes = moves.sorted { $0.key < $1.key }.compactMap { id, position -> LocalWrite? in
                guard let task = model.tasks[id], task.position != position else { return nil }
                var next = task
                next.position = position
                return writes.change(task, to: next)
            }
            return changes.isEmpty ? [] : [wrote(LocalWrite(entries: changes.flatMap(\.entries)))]
        case let .reorderList(id, index):
            let lists = model.arrangedLists
            let moves = Position.moves(id, to: index, in: lists.map(\.id), positions: lists.reduce(into: [:]) { $0[$1.id] = $1.position })
            let changes = moves.sorted { $0.key < $1.key }.compactMap { id, position -> LocalWrite? in
                guard let list = model.lists[id], list.position != position else { return nil }
                var next = list
                next.position = position
                return writes.change(list, to: next)
            }
            return changes.isEmpty ? [] : [wrote(LocalWrite(entries: changes.flatMap(\.entries)))]
        case let .deleteTask(id):
            guard let task = model.tasks[id] else { return [] }
            var events: [Event] = undone(id, state) + [wrote(writes.delete(TasksKinds.task, id, version: task.version))]
            if state.openTask == id { events.append(.opened(nil)) }
            return events
        case let .createList(name):
            let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !name.isEmpty else { return [] }
            let list = TaskList(
                id: writes.newID(), name: name,
                position: Position.between(model.lists.values.compactMap(\.position).max(), nil))
            return [wrote(writes.create(list)), .shown(.list(list.id))]
        case let .renameList(id, name):
            let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
            guard let list = model.lists[id], !name.isEmpty, name != list.name else { return [] }
            var next = list
            next.name = name
            return [wrote(writes.change(list, to: next))]
        case let .deleteList(id):
            guard model.lists[id] != nil else { return [] }
            return [.asked(.deleteList(id: id))]
        case .confirm:
            guard case let .deleteList(id)? = state.confirmation, let list = model.lists[id] else { return [.answered] }
            var events: [Event] = [.answered, wrote(writes.delete(TasksKinds.list, id, version: list.version))]
            if state.selection == .list(id) { events.append(.shown(state.section == TasksMenu.lists ? nil : .inbox)) }
            if state.filter == .list(id) { events.append(.filtered(nil)) }
            return events
        case .dismissConfirmation:
            return [.answered]
        case let .settings(command):
            let next = state.settings.applying(command)
            return next == state.settings ? [] : [.settingsChanged(next)]
        case .clearSaved:
            return [.records(.clearAsked)]
        case .refresh:
            return [.records(.refreshAsked)]
        case .foreground:
            return [.records(.foregrounded)]
        case let .offered(version):
            return [.offered(version)]
        case let .reminderTapped(id):
            return [.tapped(id)]
        case .dismissProblem:
            return [.problemDismissed]
        }
    }

    /// A completion still in its window is taken back before anything else
    /// happens to the task.
    private func undone(_ id: String, _ state: TasksState) -> [Event] {
        state.completing[id] == nil ? [] : [.uncompleted(id)]
    }

    /// A move of state, where the package allows it.
    private func status(_ id: String, _ status: TaskStatus, _ model: TasksModel, _ writes: TaskWrites) -> [Event] {
        guard let task = model.tasks[id], task.status.moves(to: status) else { return [] }
        var next = task
        next.status = status
        if status == .open { next.completedAt = nil }
        return [.records(.wrote(writes.change(task, to: next)))]
    }
}
