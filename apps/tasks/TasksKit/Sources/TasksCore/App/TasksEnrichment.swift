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
        let model = model(state, calendar: calendar)
        let today = CivilDay(context.now, in: calendar)
        let writes = TaskWrites(mint: { context.mint() })
        let wrote = { (write: LocalWrite) in Event.records(.wrote(write)) }
        let own = model.collection(TasksKinds.task)

        switch command {
        case let .select(section):
            return [.sectionSelected(section)]
        case let .show(selection):
            return [.shown(selection)]
        case let .open(task):
            return [.opened(task)]
        case let .filter(filter):
            guard filter != state.filter else { return [] }
            if case let .group(path)? = filter, model.groups[path] == nil { return [] }
            return [.filtered(filter)]
        case let .quickAdd(name):
            let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !name.isEmpty, let own, own.creatable else { return [] }
            // On Needs you a task goes into the list it is narrowed to and is
            // due today, so it lands where the owner is looking.
            let selection = state.selection == .needsYou ? state.filter ?? .needsYou : state.selection ?? .inbox
            let list = selection.listID
            let task = TaskItem(
                recordID: writes.newID(), name: name, group: list.map { .record(ListSelection.list($0).listPath) } ?? TaskGroupLink.none,
                position: Position.between(model.lastPosition(inList: list), nil), due: state.selection == .needsYou ? .day(today) : nil)
            return [wrote(writes.create(task, in: own))]
        case .newTask:
            guard own?.creatable == true else { return [] }
            let selection = state.selection == .needsYou ? state.filter ?? .needsYou : state.selection ?? .inbox
            return [.editorOpened(ChildKey(writes.newID()), .new(list: selection.listID, day: state.selection == .needsYou ? today : nil))]
        case let .edit(path):
            guard let task = model.tasks[path], task.kind == TasksKinds.task, own?.writable == true else { return [] }
            return [.editorOpened(ChildKey(writes.newID()), .task(path))]
        case let .complete(path):
            return decide(.complete, path, to: nil, state: state, model: model, writes: writes)
        case let .abandon(path):
            return decide(.wontDo, path, to: nil, state: state, model: model, writes: writes)
        case let .accept(path):
            return decide(.accept, path, to: nil, state: state, model: model, writes: writes)
        case let .dismiss(path):
            return decide(.dismiss, path, to: nil, state: state, model: model, writes: writes)
        case let .reopen(path):
            guard state.pending[path] == nil else { return [.undone(path)] }
            return decide(.reopen, path, to: nil, state: state, model: model, writes: writes)
        case let .decide(path, decision, value):
            return decide(decision, path, to: value, state: state, model: model, writes: writes)
        case let .undo(path):
            return state.pending[path] == nil ? [] : [.undone(path)]
        case .dismissChoice:
            return state.choice == nil ? [] : [.chose(nil)]
        case let .setSuggestion(path, property, value):
            guard let task = model.tasks[path], task.isSuggestion, let collection = model.collection(task.kind), collection.writable,
                Self.editable(collection).contains(property)
            else { return [] }
            var edits = state.edits[path] ?? JSONObject()
            let record = state.records[path]
            // A field set back to what the record holds is no edit.
            if (value ?? .null) == (record?.properties[property] ?? .null) { edits[property] = nil } else { edits[property] = value ?? .null }
            // A day set clears a moment the record holds, which would win.
            if property == collection.dueDay, let at = collection.due, record?.properties[at].map({ $0 != .null }) == true {
                edits[at] = .null
            }
            return edits == (state.edits[path] ?? JSONObject()) ? [] : [.suggestionSet(path, edits)]
        case let .pick(on):
            return [.picking(on ? [] : nil)]
        case let .picked(path):
            guard var picked = state.picked, model.tasks[path]?.isSuggestion == true else { return [] }
            if let index = picked.firstIndex(of: path) { picked.remove(at: index) } else { picked.append(path) }
            return [.picking(picked)]
        case .acceptPicked, .dismissPicked:
            // One decision for every one picked: each its first value, as a
            // swipe would write it, in its own window.
            let decision: TaskDecision = command == .acceptPicked ? .accept : .dismiss
            var events: [Event] = []
            for path in state.picked ?? [] {
                guard let value = model.tasks[path]?.actions[decision].first else { continue }
                events += decide(decision, path, to: value, state: state, model: model, writes: writes)
            }
            return events + [.picking(nil)]
        case let .reschedule(path, how):
            guard let task = model.tasks[path], let collection = model.collection(task.kind), collection.writable else { return [] }
            guard how != .pickDay else {
                return task.kind == TasksKinds.task ? [.editorOpened(ChildKey(writes.newID()), .task(path))] : []
            }
            let day =
                switch how {
                case .today: today
                case .tomorrow: today.adding(days: 1, in: calendar)
                case .nextWeek, .pickDay: today.adding(days: 7, in: calendar)
                }
            var next = task
            // A timed task keeps its time of day on the new day; a kind with
            // no due day takes a day alone only where it already has a time.
            if let instant = task.due?.instant, collection.due != nil {
                let clock = calendar.dateComponents([.hour, .minute], from: instant)
                next.due = .time(day.start(in: calendar, hour: clock.hour ?? 0).addingTimeInterval(Double(clock.minute ?? 0) * 60), day: day)
            } else if collection.dueDay != nil {
                next.due = .day(day)
            } else {
                return []
            }
            guard next.due != task.due else { return [] }
            return undone(path, state) + [wrote(writes.change(task, to: next, in: collection))]
        case let .setPriority(path, priority):
            guard let task = model.tasks[path], let collection = model.collection(task.kind), collection.writable,
                collection.priorities.contains(priority.rawValue), task.priorityValue != priority.rawValue
            else { return [] }
            var next = task
            next.priority = priority
            next.priorityValue = priority.rawValue
            return [wrote(writes.change(task, to: next, in: collection))]
        case let .move(path, list):
            guard let task = model.tasks[path], task.kind == TasksKinds.task, let own, own.writable, task.list != list,
                list.map({ model.groups[ListSelection.list($0).listPath] != nil }) ?? true
            else { return [] }
            var next = task
            next.group = list.map { .record(ListSelection.list($0).listPath) } ?? TaskGroupLink.none
            next.position = Position.between(model.lastPosition(inList: list), nil)
            return [wrote(writes.change(task, to: next, in: own))]
        case let .reorderTask(path, index):
            guard let selection = state.selection, selection != .needsYou, TasksSettings.order(state.settings) == .manual,
                let task = model.tasks[path], let collection = model.collection(task.kind), collection.writable, collection.order != nil
            else { return [] }
            let open = TaskOrder.manual.sorted(model.tasks(in: selection).filter { $0.isOpen && $0.kind == task.kind }, in: calendar)
            let moves = Position.moves(path, to: index, in: open.map(\.id), positions: open.reduce(into: [:]) { $0[$1.id] = $1.position })
            let changes = moves.sorted { $0.key < $1.key }.compactMap { path, position -> LocalWrite? in
                guard let task = model.tasks[path], task.position != position else { return nil }
                var next = task
                next.position = position
                return writes.change(task, to: next, in: collection)
            }
            return changes.isEmpty ? [] : [wrote(LocalWrite(entries: changes.flatMap(\.entries)))]
        case let .reorderList(id, index):
            let lists = model.arrangedLists
            let moves = Position.moves(id, to: index, in: lists.map(\.recordID), positions: lists.reduce(into: [:]) { $0[$1.recordID] = $1.position })
            let changes = moves.sorted { $0.key < $1.key }.compactMap { id, position -> LocalWrite? in
                guard let list = model.groups[ListSelection.list(id).listPath], list.position != position else { return nil }
                var next = list
                next.position = position
                return writes.change(list, to: next)
            }
            return changes.isEmpty ? [] : [wrote(LocalWrite(entries: changes.flatMap(\.entries)))]
        case let .deleteTask(path):
            guard let task = model.tasks[path], model.collection(task.kind)?.writable == true else { return [] }
            var events: [Event] = undone(path, state) + [wrote(writes.delete(task.kind, task.recordID, version: task.version))]
            if state.openTask == path { events.append(.opened(nil)) }
            return events
        case let .createList(name):
            let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !name.isEmpty else { return [] }
            let list = TaskGroup(
                recordID: writes.newID(), name: name, position: Position.between(model.arrangedLists.compactMap(\.position).max(), nil))
            return [wrote(writes.create(list)), .shown(.group(list.id))]
        case let .renameList(id, name):
            let name = name.trimmingCharacters(in: .whitespacesAndNewlines)
            guard let list = model.groups[ListSelection.list(id).listPath], !name.isEmpty, name != list.name else { return [] }
            var next = list
            next.name = name
            return [wrote(writes.change(list, to: next))]
        case let .deleteList(id):
            guard model.groups[ListSelection.list(id).listPath] != nil else { return [] }
            return [.asked(.deleteList(id: id))]
        case .confirm:
            guard case let .deleteList(id)? = state.confirmation, let list = model.groups[ListSelection.list(id).listPath] else { return [.answered] }
            var events: [Event] = [.answered, wrote(writes.delete(list.kind, list.recordID, version: list.version))]
            if state.selection == .list(id) { events.append(.shown(state.section == TasksMenu.lists ? nil : .inbox)) }
            if state.filter == .list(id) { events.append(.filtered(nil)) }
            return events
        case .dismissConfirmation:
            return [.answered]
        case let .pin(path), let .unpin(path):
            guard model.groups[path] != nil || command == .unpin(path) else { return [] }
            var pins = model.pinsKept(held: state.records, reading: linked.kinds(in: state.linked))
            if case .pin = command {
                guard !pins.contains(path) else { return [] }
                pins.append(path)
            } else {
                guard pins.contains(path) else { return [] }
                pins.removeAll { $0 == path }
            }
            let values = JSONValue.array(pins.map(TasksCodec.reference))
            return preference(["pins": values], model: model, writes: writes)
        case let .setNewTasks(kind):
            guard let collection = model.collection(kind), collection.enabled, collection.writable,
                (model.preference.newTasks ?? TasksKinds.task) != kind
            else { return [] }
            return preference(["newTasks": kind == TasksKinds.task ? .null : .string(kind)], model: model, writes: writes)
        case let .setCollection(kind, on):
            guard let collection = model.collection(kind), collection.enabled != on else { return [] }
            var events = setting(kind, ["enabled": .bool(on)], model: model, writes: writes)
            // Turned on, it asks for what changes it, and for its grouping's
            // reads: the owner approves exactly the kinds they chose.
            if on {
                var scopes: [String] = []
                if !collection.writable { scopes.append("write:\(kind)") }
                if let group = collection.group, !Self.grant(state.linked.link).reads(group.kind) { scopes.append("read:\(group.kind)") }
                if !scopes.isEmpty { events.append(.approvalAsked(ApprovalAsk(collection: kind, scopes: scopes, held: state.linked.link?.scopes))) }
            }
            return events
        case let .setGrouping(kind, property):
            guard let collection = model.collection(kind) else { return [] }
            guard let property else {
                guard collection.group != nil else { return [] }
                return setting(kind, ["group": .string(CollectionSetting.none)], model: model, writes: writes)
            }
            guard let reference = collection.allReferences.first(where: { $0.property == property }), collection.group != reference
            else { return [] }
            var events = setting(kind, ["group": .string(property)], model: model, writes: writes)
            if !Self.grant(state.linked.link).reads(reference.kind) {
                events.append(
                    .approvalAsked(
                        ApprovalAsk(collection: kind, scopes: ["read:\(reference.kind)"], held: state.linked.link?.scopes, group: reference.kind)))
            }
            return events
        case let .showSettings(page):
            if case let .collection(kind)? = page, model.collection(kind) == nil { return [] }
            return page == state.settingsPage ? [] : [.settingsShown(page)]
        case let .approvalAnswered(completed):
            return state.approval == nil ? [] : [.approvalAnswered(completed: completed)]
        case .dismissApproval:
            return state.approval == nil ? [] : [.approvalAsked(nil)]
        case let .settings(command):
            if case .open(row: .app(TasksSettings.collections)) = command { return [.settingsShown(.collections)] }
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
        case let .reminderTapped(path):
            return [.tapped(path)]
        case .dismissProblem:
            return [.problemDismissed]
        }
    }

    /// The properties a suggestion's page sets before it is accepted: the
    /// readings it edits.
    static func editable(_ collection: TaskCollection) -> [String] {
        [collection.group?.property, collection.due, collection.dueDay, collection.priority].compactMap { $0 }
    }

    /// A decision still in its window is taken back before anything else
    /// happens to the task.
    private func undone(_ path: String, _ state: TasksState) -> [Event] {
        state.pending[path] == nil ? [] : [.undone(path)]
    }

    /// A decision on a task: written after its undo window, or at once for a
    /// reopen; with several values and none picked, the choice is asked. A
    /// suggestion's fields as the owner set them go in its accept's patch.
    private func decide(
        _ decision: TaskDecision, _ path: String, to value: String?, state: TasksState, model: TasksModel, writes: TaskWrites
    ) -> [Event] {
        guard state.pending[path] == nil else { return [] }
        let edits = decision == .accept ? state.edits[path] ?? JSONObject() : JSONObject()
        switch model.outcome(of: decision, on: path, to: value, edits: edits) {
        case .none:
            return []
        case let .choose(values):
            return [.chose(DecisionChoice(task: path, decision: decision, values: values))]
        case let .write(task, properties):
            let write = writes.decide(task, properties: properties)
            let chosen = properties[model.collection(task.kind)?.status.property ?? ""]?.text ?? ""
            let closing: [Event] = state.choice == nil ? [] : [.chose(nil)]
            if decision == .reopen { return closing + [.records(.wrote(write))] }
            return [.deciding(path, PendingDecision(decision: decision, value: chosen, write: write))]
        }
    }

    /// A change to a collection's settings record, under its derived id.
    private func setting(_ kind: String, _ change: JSONObject, model: TasksModel, writes: TaskWrites) -> [Event] {
        let held = model.collection(kind)?.setting
        let id = held?.recordID ?? CollectionSetting.id(for: kind)
        let write = writes.setting(
            TasksKinds.collection, id, held: held?.version, created: ["kind": .string(kind)], change: change)
        return write.entries.isEmpty ? [] : [.settings(.wrote(write))]
    }

    /// A change to the preference record, under its derived id. A pin whose
    /// record is gone is dropped by the write.
    private func preference(_ change: JSONObject, model: TasksModel, writes: TaskWrites) -> [Event] {
        let id = model.preference.recordID ?? TasksPreference.id
        let write = writes.setting(TasksKinds.preference, id, held: model.preference.version, created: [:], change: change)
        return write.entries.isEmpty ? [] : [.settings(.wrote(write))]
    }
}
