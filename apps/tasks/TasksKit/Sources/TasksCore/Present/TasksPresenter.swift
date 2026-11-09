import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateScreens

/// The root's presenter: every screen of the app from the state, the
/// editors' presentations, the instant, the locale and the time zone. It
/// writes every word the screens show; the new screens' words are the
/// contract's (apps/tasks/contract/README.md, "Words").
public enum TasksPresenter {
    public static func present(_ input: PresenterInput<TasksApp>, app: TasksApp) -> TasksPresentation {
        let state = input.state
        let calendar = input.calendar
        let model = app.model(state, calendar: calendar)
        let today = CivilDay(input.now, in: calendar)
        let words = Words(now: input.now, calendar: calendar, locale: input.locale, model: model, state: state)
        // Monotonic in both parts: a new record, setting or selection, and
        // a new minute (an hour passing makes a timed task late).
        let revision = state.revision + Int(input.now.timeIntervalSince1970 / 60)
        let badge = model.lateOrUrgentCount(now: input.now, today: today)

        let plan = Reminders.plan(
            model, settings: state.settings, permission: state.permission, now: input.now, calendar: calendar,
            locale: input.locale)
        let midnight = today.adding(days: 1, in: calendar).start(in: calendar)
        let nextDue = model.tasks.values.compactMap { $0.isOpen ? $0.due?.instant : nil }.filter { $0 > input.now }.min()
        let open = state.openTask.flatMap { model.tasks[$0] }
        var notices = app.linked.notice(state.linked).map { [$0] } ?? []
        if notices.isEmpty, let line = app.settingsLinked.notice(state.settingsLinked) { notices.append(line) }

        return TasksPresentation(
            menu: TasksMenu.present(selected: state.section, needsYou: badge),
            sidebar: sidebar(state, model: model, words: words, badge: badge),
            list: state.selection.flatMap { $0 == .suggestions ? nil : list($0, state: state, model: model, today: today, words: words, revision: revision) },
            queue: state.selection == .suggestions ? queue(state: state, model: model, words: words, revision: revision) : nil,
            detail: open.flatMap { $0.isSuggestion ? nil : detail($0, state: state, model: model, words: words) },
            suggestion: open.flatMap { $0.isSuggestion ? suggestion($0, state: state, model: model, words: words) : nil },
            choice: state.choice.flatMap { choice($0, model: model) },
            editors: input.children,
            confirmation: state.confirmation.flatMap { confirmation($0, model: model) },
            settings: TasksSettings.sections(
                state.settings, permission: state.permission, scheduled: state.scheduled?.count ?? 0,
                device: app.device, locale: input.locale, collectionsOn: model.enabled.count),
            storage: SettingsPresenter.storage(state.usage ?? [], budget: nil),
            about: AboutPresentation.present(
                appName: TasksApp.appName, version: app.build.version, channel: app.build.channel,
                offered: state.offered, privacyPolicy: app.build.privacyPolicy),
            collections: collections(model),
            collection: state.settingsPage.flatMap { page in
                if case let .collection(kind) = page { model.collection(kind).map { collection($0, model: model) } } else { nil }
            },
            settingsPage: state.settingsPage,
            approval: state.approval.map { approval($0, model: model) },
            notices: notices,
            problem: state.problem.map { ProblemPresentation(title: "A change was not kept", message: $0) },
            reminders: plan,
            nextChange: [midnight, nextDue].compactMap { $0 }.min())
    }

    // MARK: The lists column

    static func sidebar(_ state: TasksState, model: TasksModel, words: Words, badge: Int) -> SidebarPresentation {
        let suggestions = model.suggestions.count
        let undated = model.tasks(in: .noDate).count
        let fixed = [
            SidebarRow(
                id: "needs", selection: .needsYou, title: "Needs you", symbol: "needs", open: badge, openWords: openWords(badge),
                pinned: false, actions: []),
            SidebarRow(
                id: "suggestions", selection: .suggestions, title: "Suggestions", symbol: "suggestion", open: suggestions,
                openWords: suggestionWords(suggestions), pinned: false, actions: []),
            SidebarRow(
                id: "nodate", selection: .noDate, title: "No date", symbol: "nodate", open: undated, openWords: openWords(undated),
                pinned: false, actions: []),
        ]
        let pins = Set(model.pins)
        let pinned = model.pins.compactMap { model.groups[$0] }.map { groupRow($0, model: model, pinned: true, detailed: true) }
        var sections: [GroupSection] = []
        for kind in groupKinds(model) {
            let collections = model.enabled.filter { $0.group?.kind == kind }
            let groups = model.ordered(model.groups.values.filter { $0.kind == kind }, of: kind)
            let rows = groups.map { groupRow($0, model: model, pinned: pins.contains($0.id), detailed: false) }
            var busy = rows.filter { $0.open > 0 }
            let quiet = rows.filter { $0.open == 0 }
            for collection in collections {
                let reference = collection.group?.label ?? ""
                let none = model.tasks(in: .ungrouped(collection.kind)).filter(\.isOpen).count
                if none > 0 {
                    busy.append(
                        SidebarRow(
                            id: "none:\(collection.kind)", selection: .ungrouped(collection.kind), title: "No \(reference)",
                            detail: "Tasks with no \(reference)", symbol: "inbox", open: none, openWords: openWords(none),
                            pinned: false, actions: []))
                }
                let missing = model.tasks(in: .missing(collection.kind)).filter(\.isOpen).count
                if missing > 0 {
                    busy.append(
                        SidebarRow(
                            id: "missing:\(collection.kind)", selection: .missing(collection.kind), title: "Missing",
                            detail: "Tasks whose \(reference) is gone or out of reach", symbol: "missing", open: missing,
                            openWords: openWords(missing), pinned: false, actions: []))
                }
            }
            let declaration = model.declarations[kind]
            let plural = declaration?.pluralLabel ?? kindDisplayPlural(kind)
            let busiest = 5
            sections.append(
                GroupSection(
                    id: kind, title: plural, reference: kind, rows: busy, quiet: quiet,
                    more: quiet.isEmpty ? nil : "\(quiet.count) more with nothing open", busiest: busiest,
                    all: groups.count > busiest ? "All \(groups.count) \(plural.lowercased())…" : nil, reorders: kind == TasksKinds.list))
        }
        let ungrouped = model.enabled.filter { $0.group == nil }.map { collection in
            let open = model.tasks(in: .collection(collection.kind)).filter(\.isOpen).count
            return SidebarRow(
                id: "collection:\(collection.kind)", selection: .collection(collection.kind), title: collection.label,
                detail: collection.kind, symbol: "list", open: open, openWords: openWords(open), pinned: false, actions: [])
        }
        let ownLists = model.collection(TasksKinds.task).map { $0.enabled && $0.group?.kind == TasksKinds.list } ?? false
        let labels = sections.map { $0.title.lowercased() }
        return SidebarPresentation(
            title: "Lists", fixed: fixed, search: labels.isEmpty ? "Search" : "Search \(joinWords(labels))", pinnedTitle: "Pinned",
            pinned: pinned, sections: sections, ungroupedTitle: "Collections", ungrouped: ungrouped, newList: "New List",
            noLists: ownLists && model.arrangedLists.isEmpty ? "No lists yet. Add one to sort your tasks." : nil, selected: state.selection)
    }

    /// The group kinds of the collections that are on, in their order.
    static func groupKinds(_ model: TasksModel) -> [String] {
        var out: [String] = []
        for kind in model.enabled.compactMap(\.group?.kind) where !out.contains(kind) { out.append(kind) }
        return out
    }

    static func groupRow(_ group: TaskGroup, model: TasksModel, pinned: Bool, detailed: Bool) -> SidebarRow {
        let open = model.tasks(in: .group(group.id)).filter(\.isOpen).count
        let own = group.kind == TasksKinds.list
        let kindLabel = model.declarations[group.kind]?.label ?? kindDisplayName(group.kind)
        let detail: String? =
            if detailed { [kindLabel, group.status].compactMap { $0 }.joined(separator: " · ") } else { group.status }
        return SidebarRow(
            id: group.id, selection: .group(group.id), title: group.name.isEmpty ? "Untitled" : group.name, detail: detail,
            symbol: own ? "list" : "group", open: open, openWords: openWords(open), pinned: pinned,
            actions: own ? [ActionButton("Rename…", .rename(group.recordID)), ActionButton("Delete…", .delete(group.recordID))] : [])
    }

    static func openWords(_ n: Int) -> String { n == 1 ? "1 open task" : "\(n) open tasks" }
    static func suggestionWords(_ n: Int) -> String { n == 1 ? "1 suggestion" : "\(n) suggestions" }

    /// The suggestions as one row, counted, where there are any.
    static func queueRow(_ count: Int) -> CountedRow? {
        guard count > 0 else { return nil }
        return CountedRow(
            title: suggestionWords(count), detail: "Tasks waiting for you to accept or dismiss", count: count, selection: .suggestions)
    }

    // MARK: A list's tasks

    static var menu: TaskMenuWords {
        TaskMenuWords(
            complete: "Mark as Done", abandon: "Won't Do", reopen: "Reopen", edit: "Edit", priority: TaskField.priority.label,
            priorities: priorities, move: "Move to List", delete: "Delete Task", swipeDelete: "Delete", accept: "Accept",
            dismiss: "Dismiss", undo: "Undo")
    }

    static func list(_ selection: ListSelection, state: TasksState, model: TasksModel, today: CivilDay, words: Words, revision: Int)
        -> ListPresentation
    {
        if selection == .needsYou { return needsYou(state: state, model: model, today: today, words: words, revision: revision) }
        let order = TasksSettings.order(state.settings)
        let showsClosed = TasksSettings.showsCompleted(state.settings)
        let tasks = model.tasks(in: selection).filter { !$0.isSuggestion }
        let open = order.sorted(tasks.filter(\.isOpen), in: words.calendar)
        let closed = tasks.filter(\.isClosed).sorted { ($0.completedAt ?? .distantPast, $0.id) > ($1.completedAt ?? .distantPast, $1.id) }
        var title: String
        var detail: String?
        var actions: [ActionButton<ListAction>] = []
        var pin: ActionButton<PinAction>?
        var queue: CountedRow?
        // A row names its collection where this list mixes collections.
        var mixed = Set(tasks.map(\.kind)).count > 1
        var adds = false
        switch selection {
        case let .group(path):
            let group = model.groups[path]
            title = group.map { $0.name.isEmpty ? "Untitled" : $0.name } ?? "List"
            let kindLabel = group.flatMap { model.declarations[$0.kind]?.label } ?? group.map { kindDisplayName($0.kind) }
            detail = [kindLabel, group?.status].compactMap { $0 }.joined(separator: " · ")
            if let group, group.kind == TasksKinds.list {
                actions = [ActionButton("Rename List…", .rename(group.recordID)), ActionButton("Delete List…", .delete(group.recordID))]
                adds = true
            }
            if group != nil { pin = model.preference.pins.contains(path) ? ActionButton("Unpin", .unpin(path)) : ActionButton("Pin", .pin(path)) }
            queue = queueRow(model.tasks(in: selection).filter(\.isSuggestion).count)
        case let .ungrouped(kind):
            let collection = model.collection(kind)
            let reference = collection?.group?.label ?? "list"
            title = "No \(reference)"
            detail = collection.map { _ in "Tasks with no \(reference)" }
            adds = kind == TasksKinds.task
            mixed = false
        case let .missing(kind):
            let reference = model.collection(kind)?.group?.label ?? "list"
            title = "Missing"
            detail = "Tasks whose \(reference) is gone or out of reach"
            mixed = false
        case let .collection(kind):
            title = model.collection(kind)?.label ?? kindDisplayPlural(kind)
            detail = kind
            adds = kind == TasksKinds.task
            mixed = false
        case .noDate:
            title = "No date"
            detail = "Open tasks with no due date"
        case .needsYou, .suggestions:
            title = "Needs you"
        }
        adds = adds && model.collection(TasksKinds.task)?.creatable == true
        let empty: EmptyPresentation? =
            if !open.isEmpty { nil } else {
                selection == .inbox
                    ? EmptyPresentation(title: "No tasks", message: "Tasks in no list show here. Tap + to add one.")
                    : EmptyPresentation(title: "No open tasks", message: adds ? "Tap + to add a task." : "Nothing here is open.")
            }
        let openRows = open.map { row($0, state: state, words: words, mixed: mixed, grouped: !isGroup(selection)) }
        let late = openRows.filter { $0.due?.tone == .overdue }.count
        let summary = ListSummary(
            headline: isGroup(selection) ? "\(open.count) open" : open.isEmpty ? "All done" : "\(open.count) to do", detail: detail,
            overdue: late == 0 ? nil : "\(late) overdue")
        let reorders = order == .manual && open.allSatisfy { model.collection($0.kind)?.order != nil && model.collection($0.kind)?.writable == true }
        return ListPresentation(
            selection: selection, title: title, summary: summary, chips: [], queue: queue, bands: [], clear: nil, noDate: nil,
            open: KeyedList(openRows, revision: revision), openTitle: "To Do",
            closed: KeyedList(
                showsClosed ? closed.map { row($0, state: state, words: words, mixed: mixed, grouped: !isGroup(selection)) } : [], revision: revision),
            closedTitle: "Done",
            showsClosed: showsClosed, toggleClosed: showsClosed ? "Hide Completed" : "Show Completed",
            quickAdd: adds ? (selection.listID == nil ? "Add a task" : "Add a task to \(title)") : nil, newTask: "New Task",
            reorders: reorders, orderNote: order == .due ? "Sorted by due date" : nil, empty: empty, actions: actions, pin: pin,
            rowMenu: menu)
    }

    static func isGroup(_ selection: ListSelection) -> Bool {
        if case .group = selection { true } else { false }
    }

    // MARK: Needs you

    /// Every open task across the collections in its band: what is late or
    /// urgent, with the decisions on its row; what is coming this week; the
    /// rest under Later; the undated counted on one row, and the
    /// suggestions on another. The chips narrow it to a pinned group.
    static func needsYou(state: TasksState, model: TasksModel, today: CivilDay, words: Words, revision: Int) -> ListPresentation {
        let banded = model.banded(filter: state.filter, now: words.now, today: today).filter { $0.band != .noDate }
        let undated = model.banded(filter: state.filter, now: words.now, today: today).filter { $0.band == .noDate }.count
        let rows = banded.map { row($0.task, state: state, words: words, mixed: true, grouped: true, band: $0.band, today: today) }
        let bands = [TaskBand.lateOrUrgent, .comingUp, .later].compactMap { band -> BandHeading? in
            let count = banded.filter { $0.band == band }.count
            guard count > 0 else { return nil }
            return BandHeading(band: band, title: bandTitle(band), count: count)
        }
        var chips = [FilterChip(id: "all", title: "All", count: nil, selection: nil, selected: state.filter == nil)]
        for path in model.pins {
            guard let group = model.groups[path] else { continue }
            chips.append(
                FilterChip(
                    id: path, title: group.name.isEmpty ? "Untitled" : group.name,
                    count: model.tasks(in: .group(path)).filter(\.isOpen).count, selection: .group(path), selected: state.filter == .group(path)))
        }
        let suggestions = state.filter.map { filter in model.tasks(in: filter).filter(\.isSuggestion).count } ?? model.suggestions.count
        let needing = bands.first?.band == .lateOrUrgent
        let empty: EmptyPresentation? =
            if !rows.isEmpty || undated > 0 || suggestions > 0 { nil } else if state.filter == nil {
                EmptyPresentation(title: "All clear", message: "Nothing is open. Tap + to add a task.")
            } else {
                EmptyPresentation(title: "All clear", message: "Nothing is open here. Choose All to see everything.")
            }
        let summary = ListSummary(
            headline: needing ? "\(bands[0].count) need you" : "Nothing needs you", detail: dayTitle(today, words: words), overdue: nil)
        let narrowed = state.filter.flatMap { filter -> String? in
            guard filter.listID != nil, case let .group(path) = filter else { return nil }
            return model.groups[path]?.name
        }
        let adds = model.collection(TasksKinds.task)?.creatable == true
        return ListPresentation(
            selection: .needsYou, title: "Needs you", summary: summary, chips: chips.count > 1 ? chips : [], queue: queueRow(suggestions),
            bands: bands, clear: !rows.isEmpty && !needing ? "Nothing needs you right now." : nil,
            noDate: undated > 0 ? CountedRow(title: "No date", detail: "Open tasks with no due date", count: undated, selection: .noDate) : nil,
            open: KeyedList(rows, revision: revision), openTitle: "To Do", closed: KeyedList([], revision: revision), closedTitle: "Done",
            showsClosed: false, toggleClosed: "Show Completed",
            quickAdd: adds ? narrowed.map { "Add a task for today to \($0)" } ?? "Add a task for today" : nil, newTask: "New Task",
            reorders: false, orderNote: nil, empty: empty, actions: [], pin: nil, rowMenu: menu)
    }

    static func bandTitle(_ band: TaskBand) -> String {
        switch band {
        case .lateOrUrgent: "Late or urgent"
        case .comingUp: "Coming up"
        case .later: "Later"
        case .noDate: "No date"
        }
    }

    /// The days to move a task that waits on one to: today where it is
    /// late, tomorrow, a week on, or the editor.
    static func reschedule(_ task: TaskItem, today: CivilDay, words: Words) -> RescheduleMenu? {
        guard let collection = words.model.collection(task.kind), collection.writable,
            collection.dueDay != nil || (collection.due != nil && task.due?.instant != nil)
        else { return nil }
        let week = today.adding(days: 7, in: words.calendar)
        var options: [ActionButton<Reschedule>] = []
        if task.due.map({ $0.day != today }) ?? true { options.append(ActionButton("Today", .today)) }
        options.append(ActionButton("Tomorrow", .tomorrow))
        options.append(ActionButton(DueWords.dayWords(week, today: today, calendar: words.calendar, locale: words.locale), .nextWeek))
        if task.kind == TasksKinds.task { options.append(ActionButton("Pick a Day…", .pickDay)) }
        return RescheduleMenu(title: task.due == nil ? "Pick a Day" : "Move", options: options)
    }

    /// What a decision in its window says on its row.
    static func pendingWords(_ decision: TaskDecision) -> String {
        switch decision {
        case .accept: "Accepted"
        case .dismiss: "Dismissed"
        case .complete: "Done"
        case .wontDo: "Won't do"
        case .reopen: "Open"
        }
    }

    static func row(
        _ task: TaskItem, state: TasksState, words: Words, mixed: Bool, grouped: Bool, band: TaskBand? = nil, today: CivilDay? = nil
    ) -> TaskRow {
        let pending = state.pending[task.id]
        let collection = words.model.collection(task.kind)
        var due = task.due.map { words.due($0) }
        // On Needs you a late task says how late, not when it was due: the
        // day is in the decision its row offers.
        if band == .lateOrUrgent, let late = task.due, let today, due?.tone == .overdue {
            due = DueBadge(text: words.late(late, today: today), tone: .overdue)
        }
        if band == .lateOrUrgent, task.due == nil, task.pressing, let label = task.priorityValue.flatMap({ words.priorityLabel($0, task) }) {
            due = nil
            return finish(
                task, state: state, words: words, mixed: mixed, grouped: grouped, band: band, today: today, due: due,
                priority: PriorityBadge(text: "\(label), no date", priority: task.priority), pending: pending, collection: collection)
        }
        let priority =
            task.priority == .none || task.priorityValue == nil
            ? nil : PriorityBadge(text: task.priorityValue.flatMap { words.priorityLabel($0, task) } ?? task.priority.words, priority: task.priority)
        return finish(
            task, state: state, words: words, mixed: mixed, grouped: grouped, band: band, today: today, due: due, priority: priority,
            pending: pending, collection: collection)
    }

    private static func finish(
        _ task: TaskItem, state: TasksState, words: Words, mixed: Bool, grouped: Bool, band: TaskBand?, today: CivilDay?, due: DueBadge?,
        priority: PriorityBadge?, pending: PendingDecision?, collection: TaskCollection?
    ) -> TaskRow {
        let listName = grouped ? words.groupWords(task) : nil
        let collectionName = mixed && words.model.mixed && !task.isSuggestion ? collection?.label : nil
        let title = task.name.isEmpty ? "Untitled task" : task.name
        let written =
            task.isSuggestion ? [collection?.label, task.createdAt.map { words.ago($0, short: true) }].compactMap { $0 }.joined(separator: " · ") : nil
        var spoken = [title]
        if let pending {
            spoken.append("\(pendingWords(pending.decision).lowercased()), tap to undo")
        } else if !task.isOpen {
            spoken.append(words.model.label(of: task.status ?? "", for: task))
        }
        if let written { spoken.append("suggested, \(written)") }
        if let due { spoken.append(due.tone == .overdue && band == .lateOrUrgent ? due.text : "due \(due.text)") }
        if let priority { spoken.append("priority \(priority.text)") }
        if let listName { spoken.append("in \(listName)") }
        if let collectionName { spoken.append("from \(collectionName)") }
        if task.notes != nil { spoken.append("has notes") }
        let deciding = band == .lateOrUrgent && pending == nil
        var decisions: [ActionButton<TaskAction>] = []
        if task.isSuggestion, pending == nil {
            if !task.actions.dismiss.isEmpty { decisions.append(ActionButton("Dismiss", .dismiss)) }
            if !task.actions.accept.isEmpty { decisions.append(ActionButton("Accept", .accept)) }
        } else if deciding, !task.actions.wontDo.isEmpty {
            decisions.append(ActionButton("Won't Do", .abandon))
        }
        let toggle: String? =
            if pending != nil { "Undo" } else if task.isOpen, !task.actions.complete.isEmpty {
                "Mark as Done"
            } else if task.isClosed,
                !task.actions.reopen.isEmpty
            { "Reopen" } else { nil }
        return TaskRow(
            id: task.id, title: title, standing: task.standing, suggestion: task.isSuggestion, pending: pending != nil,
            pendingWords: pending.map { pendingWords($0.decision) }, toggle: toggle, written: written, due: due, priority: priority,
            listName: listName, collection: collectionName, band: band,
            reschedule: deciding && !task.isSuggestion ? today.flatMap { reschedule(task, today: $0, words: words) } : nil,
            decisions: decisions, picked: task.isSuggestion ? state.picked.map { $0.contains(task.id) } : nil,
            editable: task.kind == TasksKinds.task && collection?.writable == true, hasNotes: task.notes != nil,
            notes: task.notes.map(preview), spoken: spoken.joined(separator: ", "))
    }

    /// Today's date as Needs you's summary says it: "Thursday, 5 November".
    static func dayTitle(_ day: CivilDay, words: Words) -> String {
        let formatter = DateFormatter()
        formatter.locale = words.locale
        formatter.timeZone = words.calendar.timeZone
        formatter.setLocalizedDateFormatFromTemplate("EEEEdMMMM")
        return formatter.string(from: day.start(in: words.calendar, hour: 12))
    }

    /// Notes as a row shows them: their words on one line, at most 160
    /// characters, ending in "…" where they go on.
    static func preview(_ notes: String) -> String {
        let words = notes.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        return words.count > 160 ? String(words.prefix(159)) + "…" : words
    }

    // MARK: The suggestions

    static func queue(state: TasksState, model: TasksModel, words: Words, revision: Int) -> QueuePresentation {
        let rows = model.suggestions.map { row($0, state: state, words: words, mixed: model.mixed, grouped: true) }
        let picked = state.picked?.filter { model.tasks[$0]?.isSuggestion == true }.count ?? 0
        return QueuePresentation(
            title: "Suggestions", intro: "Accept one to put it on your list, or dismiss it.", rows: KeyedList(rows, revision: revision),
            select: state.picked == nil ? "Select" : "Cancel", picking: state.picked != nil,
            picked: state.picked.map { _ in "\(picked) selected" }, acceptPicked: picked > 0 ? "Accept" : nil,
            dismissPicked: picked > 0 ? "Dismiss" : nil,
            empty: rows.isEmpty ? EmptyPresentation(title: "No suggestions", message: "Suggested tasks show here until you accept or dismiss them.") : nil)
    }

    static func suggestion(_ task: TaskItem, state: TasksState, model: TasksModel, words: Words) -> SuggestionPresentation {
        let collection = model.collection(task.kind)
        let edits = state.edits[task.id] ?? JSONObject()
        let pending = state.pending[task.id]
        let written = task.createdAt.map { words.ago($0, short: false) }
        var fields: [DetailField] = []
        if let collection, collection.writable {
            fields = editableFields(task, collection: collection, edits: edits, model: model, words: words)
        }
        let suggestions = model.suggestions
        let place = suggestions.firstIndex { $0.id == task.id }.map { "\($0 + 1) of \(suggestions.count)" }
        var actions: [ActionButton<TaskAction>] = []
        if pending != nil {
            actions = [ActionButton("Undo", .undo)]
        } else {
            if !task.actions.dismiss.isEmpty { actions.append(ActionButton("Dismiss", .dismiss)) }
            if !task.actions.accept.isEmpty { actions.append(ActionButton("Accept", .accept)) }
        }
        return SuggestionPresentation(
            id: task.id,
            header: ["Suggestion in \(collection?.label ?? kindDisplayPlural(task.kind))", written.map { "written \($0)" }].compactMap { $0 }
                .joined(separator: " · "),
            title: task.name.isEmpty ? "Untitled task" : task.name, fields: fields,
            description: description(task, collection: collection), detailsTitle: "Details",
            details: details(task, collection: collection, model: model, state: state), place: place, pending: pending != nil,
            pendingWords: pending.map { pendingWords($0.decision) }, actions: actions)
    }

    /// The readings a suggestion's page sets before it is accepted, each as
    /// the record holds it with the owner's edits over it.
    static func editableFields(
        _ task: TaskItem, collection: TaskCollection, edits: JSONObject, model: TasksModel, words: Words
    ) -> [DetailField] {
        var fields: [DetailField] = []
        let record = words.state.records[task.id]
        let value = { (name: String) in edits[name] ?? record?.properties[name] }
        if let group = collection.group {
            let current = RecordReference(value(group.property) ?? .null)?.path ?? ""
            let groups = model.groups.values.filter { $0.kind == group.kind }.arranged
            let options = [EditorOption(id: "", title: "No \(group.label)")] + groups.map { EditorOption(id: $0.id, title: $0.name) }
            fields.append(
                DetailField(
                    field: .list, label: propertyLabel(group.property),
                    value: options.first { $0.id == current }?.title ?? "Missing",
                    choice: EditorChoice(label: propertyLabel(group.property), selected: current, options: options), property: group.property))
        }
        if let dueDay = collection.dueDay {
            let today = CivilDay(words.now, in: words.calendar)
            let days = [today, today.adding(days: 1, in: words.calendar), today.adding(days: 7, in: words.calendar)]
            let current = value(dueDay)?.text ?? ""
            var options = [EditorOption(id: "", title: "None")]
            options += days.map { EditorOption(id: $0.description, title: DueWords.dayWords($0, today: today, calendar: words.calendar, locale: words.locale)) }
            if !current.isEmpty, !options.contains(where: { $0.id == current }), let day = CivilDay(current) {
                options.append(EditorOption(id: current, title: DueWords.dayWords(day, today: today, calendar: words.calendar, locale: words.locale)))
            }
            fields.append(
                DetailField(
                    field: .due, label: TaskField.due.label, value: options.first { $0.id == current }?.title ?? "None",
                    choice: EditorChoice(label: TaskField.due.label, selected: current, options: options), property: dueDay))
        }
        if let priority = collection.priority {
            let current = value(priority)?.text ?? ""
            var options = collection.priorities.map { EditorOption(id: $0, title: collection.declaration.label(of: $0, in: priority)) }
            if !collection.priorities.contains("none") { options.insert(EditorOption(id: "", title: "None"), at: 0) }
            fields.append(
                DetailField(
                    field: .priority, label: TaskField.priority.label, value: options.first { $0.id == current }?.title ?? "None",
                    choice: EditorChoice(label: TaskField.priority.label, selected: current, options: options), property: priority))
        }
        return fields
    }

    static func description(_ task: TaskItem, collection: TaskCollection?) -> DetailFact? {
        guard let notes = task.notes, let property = collection?.notes else { return nil }
        return DetailFact(id: property, label: propertyLabel(property), value: notes, note: nil)
    }

    /// Every property the readings do not show, read only under its label,
    /// then the collection by its label with its full reference.
    static func details(_ task: TaskItem, collection: TaskCollection?, model: TasksModel, state: TasksState) -> [DetailFact] {
        guard let collection, let record = state.records[task.id] else { return [] }
        let shown = Set(
            [
                collection.title, collection.due, collection.dueDay, collection.priority, collection.order, collection.group?.property,
                collection.notes, collection.link, collection.status.property, "title",
            ].compactMap { $0 })
        var out: [DetailFact] = []
        for name in collection.declaration.propertyNames where !shown.contains(name) {
            guard let value = record.properties[name], value != .null else { continue }
            if let stamps = task.status.map({ collection.declaration.stamps(into: $0, of: collection.status.property) }), stamps.contains(name) {
                continue
            }
            let words: String
            if let reference = RecordReference(value), collection.declaration.type(of: name) == "reference" {
                words = model.groups[reference.path]?.name ?? state.records[reference.path]?["title"]?.text ?? fallbackName(reference)
            } else if collection.declaration.type(of: name) == "enum" || collection.declaration.type(of: name) == "state", let text = value.text {
                words = collection.declaration.label(of: text, in: name)
            } else {
                words = valueWords(value)
            }
            out.append(DetailFact(id: name, label: propertyLabel(name), value: words, note: nil))
        }
        out.append(DetailFact(id: "collection", label: "Collection", value: collection.label, note: collection.kind))
        return out
    }

    static func choice(_ choice: DecisionChoice, model: TasksModel) -> ChoicePresentation? {
        guard let task = model.tasks[choice.task] else { return nil }
        let title =
            switch choice.decision {
            case .accept: "Accept as"
            case .reopen: "Reopen as"
            case .dismiss: "Dismiss as"
            case .complete: "Done as"
            case .wontDo: "Won't do as"
            }
        return ChoicePresentation(
            task: choice.task, decision: choice.decision, title: title,
            options: choice.values.map { EditorOption(id: $0, title: model.label(of: $0, for: task)) }, cancel: "Cancel")
    }

    // MARK: The open task

    static func detail(_ task: TaskItem, state: TasksState, model: TasksModel, words: Words) -> TaskDetailPresentation {
        let pending = state.pending[task.id]
        let collection = model.collection(task.kind)
        let own = task.kind == TasksKinds.task && collection?.writable == true
        let status = pending.map { "\(pendingWords($0.decision)), tap Undo to take it back" } ?? model.label(of: task.status ?? "", for: task)
        var fields = [DetailField(field: .status, label: TaskField.status.label, value: status)]
        if let group = collection?.group {
            let label = propertyLabel(group.property)
            let name = words.groupWords(task) ?? "No \(group.label)"
            if own, group.kind == TasksKinds.list {
                let lists = [EditorOption(id: "", title: InboxWords.title)] + model.arrangedLists.map { EditorOption(id: $0.recordID, title: $0.name) }
                fields.append(
                    DetailField(
                        field: .list, label: label, value: name, choice: EditorChoice(label: label, selected: task.list ?? "", options: lists)))
            } else {
                fields.append(DetailField(field: .list, label: label, value: name))
            }
        }
        if let due = task.due {
            let badge = words.due(due)
            fields.append(
                DetailField(
                    field: .due, label: TaskField.due.label, value: DueWords.full(due, calendar: words.calendar, locale: words.locale),
                    tone: badge.tone))
        } else if collection?.due != nil || collection?.dueDay != nil {
            fields.append(DetailField(field: .due, label: TaskField.due.label, value: "No due date"))
        }
        if let priority = collection?.priority {
            let value = task.priorityValue.map { collection?.declaration.label(of: $0, in: priority) ?? $0 } ?? "None"
            fields.append(
                DetailField(
                    field: .priority, label: TaskField.priority.label, value: value,
                    choice: own ? EditorChoice(label: TaskField.priority.label, selected: task.priority.rawValue, options: priorities) : nil))
        }
        if let notes = task.notes {
            fields.append(
                DetailField(field: .notes, label: own ? TaskField.notes.label : collection?.notes.map(propertyLabel) ?? TaskField.notes.label, value: notes))
        }
        if let url = task.url { fields.append(DetailField(field: .link, label: TaskField.link.label, value: url, url: url)) }
        if task.standing == .done, let completed = task.completedAt {
            fields.append(
                DetailField(
                    field: .completed, label: TaskField.completed.label,
                    value: DueWords.full(.at(completed, in: words.calendar), calendar: words.calendar, locale: words.locale)))
        }
        var actions: [ActionButton<TaskAction>] = []
        if pending != nil {
            actions = [ActionButton("Undo", .undo)]
        } else {
            if !task.actions.complete.isEmpty { actions.append(ActionButton("Mark as Done", .complete)) }
            if !task.actions.wontDo.isEmpty { actions.append(ActionButton("Won't Do", .abandon)) }
            if !task.actions.reopen.isEmpty { actions.append(ActionButton("Reopen", .reopen)) }
            if !task.actions.accept.isEmpty { actions.append(ActionButton("Accept", .accept)) }
        }
        if own { actions.append(ActionButton("Delete Task", .delete)) }
        return TaskDetailPresentation(
            id: task.id, title: task.name.isEmpty ? "Untitled task" : task.name, standing: task.standing, pending: pending != nil,
            fields: fields, detailsTitle: "Details", details: own ? [] : details(task, collection: collection, model: model, state: state),
            actions: actions, edit: own ? "Edit" : nil)
    }

    /// Every priority of the app's own kind, as the detail and the editor
    /// choose among them.
    static var priorities: [EditorOption] {
        TaskPriority.allCases.map { EditorOption(id: $0.rawValue, title: $0.words) }
    }

    // MARK: Settings → Collections

    static func collections(_ model: TasksModel) -> CollectionsPresentation {
        let rows = model.collections.map { collection -> CollectionRow in
            var meta: [String] = []
            if collection.viewOnly { meta.append("View only") }
            if collection.enabled {
                let tasks = model.tasks.values.filter { $0.kind == collection.kind }
                meta.append("\(tasks.filter(\.isOpen).count) open")
                let suggested = tasks.filter(\.isSuggestion).count
                if suggested > 0 { meta.append("\(suggested) suggested") }
                if let group = collection.group { meta.append("grouped by \(group.label)") }
            }
            return CollectionRow(
                id: collection.kind, title: collection.label, reference: collection.kind,
                meta: meta.isEmpty ? nil : meta.joined(separator: " · "), on: collection.enabled, viewOnly: collection.viewOnly)
        }
        let targets = model.enabled.filter(\.takesNew)
        let selected = model.newTasks?.kind ?? model.preference.newTasks ?? TasksKinds.task
        return CollectionsPresentation(
            title: "Collections",
            intro: "Every collection of yours with a done state. Turn one on to see its tasks here; Substrate Tasks then asks to change them.",
            showTitle: "Show tasks from", rows: rows,
            footer: "Turning a collection off only hides it here. Its tasks stay where they are.", newTasksTitle: "Save new tasks to",
            newTasks: EditorChoice(label: "New tasks", selected: selected, options: targets.map { EditorOption(id: $0.kind, title: $0.label) }))
    }

    static func collection(_ collection: TaskCollection, model: TasksModel) -> CollectionPresentation {
        var groups = collection.allReferences.map { reference in
            GroupChoice(
                id: reference.property, title: propertyLabel(reference.property), detail: reference.kind,
                selected: collection.group == reference)
        }
        groups.append(GroupChoice(id: "", title: "Nothing", detail: "One list of every task", selected: collection.group == nil))
        let status = collection.status
        let open = status.values.filter { status.standing(of: $0) == .open }
        let values = { (list: [String]) in list.isEmpty ? "None" : list.joined(separator: ", ") }
        let field = { (label: String, property: String?) in LabeledValue(label, property ?? "None", verbatim: property != nil) }
        return CollectionPresentation(
            kind: collection.kind, title: collection.label, reference: collection.kind, groupTitle: "Group by", groups: groups,
            statesTitle: "How its states read",
            states: [
                LabeledValue("Suggestion", values(status.proposed), verbatim: true), LabeledValue("Open", values(open), verbatim: true),
                LabeledValue("Done", values(status.done), verbatim: true), LabeledValue("Dropped", values(status.dropped), verbatim: true),
            ],
            fieldsTitle: "Fields the app shows",
            fields: [
                field("Title", collection.title), field("Due", collection.due), field("Due day", collection.dueDay),
                field("Priority", collection.priority), field("Order", collection.order),
            ])
    }

    static func approval(_ ask: ApprovalAsk, model: TasksModel) -> ApprovalPresentation {
        let name = model.collection(ask.collection)?.label ?? kindDisplayPlural(ask.collection)
        // A grouping asks to read the group kind; turning a collection on, to change it.
        let message: String
        let refused: String
        if let group = ask.group {
            let groups = model.declarations[group]?.pluralLabel ?? kindDisplayPlural(group)
            message = "Substrate Tasks needs your approval to read your \(groups.lowercasedFirst)."
            refused = "Not approved. \(name) stays ungrouped."
        } else {
            message = "Substrate Tasks needs your approval to change your \(name)."
            refused = "Not approved. \(name) stays view only."
        }
        return ApprovalPresentation(
            collection: ask.collection, message: message, scopes: ask.scopes,
            approve: "Approve", cancel: "Cancel", refused: ask.refused ? refused : nil,
            waiting: ask.answered && !ask.refused)
    }

    // MARK: Questions

    static func confirmation(_ confirmation: Confirmation, model: TasksModel) -> ConfirmationPresentation<Confirmation>? {
        switch confirmation {
        case let .deleteList(id):
            guard let list = model.groups[ListSelection.list(id).listPath] else { return nil }
            let count = model.tasks.values.filter { $0.kind == TasksKinds.task && $0.list == id }.count
            let name = list.name.isEmpty ? "this list" : "“\(list.name)”"
            // No promise of a restore: the server's next sweep, within
            // minutes, purges the list and the tasks it owns, and no
            // console page brings a deleted record back.
            let message =
                switch count {
                case 0: "The list has no tasks."
                case 1: "Its 1 task is deleted with it."
                default: "Its \(count) tasks are deleted with it."
                }
            return ConfirmationPresentation(
                question: confirmation, title: "Delete \(name)?", message: message,
                confirm: count == 0 ? "Delete List" : count == 1 ? "Delete List and 1 Task" : "Delete List and \(count) Tasks",
                cancel: "Cancel")
        }
    }

    /// What words are made from.
    struct Words {
        var now: Date
        var calendar: Calendar
        var locale: Locale
        var model: TasksModel
        var state: TasksState

        func due(_ due: Due) -> DueBadge {
            let words = DueWords.words(due, now: now, calendar: calendar, locale: locale)
            return DueBadge(text: words.text, tone: words.tone)
        }

        /// How late a task is: "3 days late", or for one due at a time
        /// earlier today, "Late since 3:00 PM".
        func late(_ due: Due, today: CivilDay) -> String {
            let days = calendar.dateComponents([.day], from: due.day.start(in: calendar), to: today.start(in: calendar)).day ?? 0
            if days >= 1 { return days == 1 ? "1 day late" : "\(days) days late" }
            if let instant = due.instant { return "Late since \(DueWords.time(instant, calendar: calendar, locale: locale))" }
            return "Late"
        }

        /// The group a task is in, as a row names it: its name, "No list",
        /// or "Missing"; nil where its collection groups by nothing.
        func groupWords(_ task: TaskItem) -> String? {
            switch task.group {
            case let .record(path)?: model.groups[path]?.name
            case .none?: "No \(model.collection(task.kind)?.group?.label ?? "list")"
            case .missing?: "Missing"
            case nil: nil
            }
        }

        func priorityLabel(_ value: String, _ task: TaskItem) -> String? {
            guard let collection = model.collection(task.kind), let priority = collection.priority else { return nil }
            return collection.declaration.label(of: value, in: priority)
        }

        /// How long ago a suggestion was written: "2h ago" on its row, "2
        /// hours ago" on its page; "yesterday", then the day.
        func ago(_ date: Date, short: Bool) -> String {
            let seconds = max(0, now.timeIntervalSince(date))
            let minutes = Int(seconds / 60)
            let hours = minutes / 60
            if minutes < 1 { return "just now" }
            if minutes < 60 { return short ? "\(minutes)m ago" : minutes == 1 ? "1 minute ago" : "\(minutes) minutes ago" }
            if hours < 24, CivilDay(date, in: calendar) == CivilDay(now, in: calendar) || hours < 12 {
                return short ? "\(hours)h ago" : hours == 1 ? "1 hour ago" : "\(hours) hours ago"
            }
            let today = CivilDay(now, in: calendar)
            let day = CivilDay(date, in: calendar)
            if day == today.adding(days: -1, in: calendar) { return "yesterday" }
            return DueWords.dayWords(day, today: today, calendar: calendar, locale: locale)
        }
    }
}
