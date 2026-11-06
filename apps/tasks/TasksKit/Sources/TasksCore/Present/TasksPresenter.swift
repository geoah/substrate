import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateScreens

/// The root's presenter: every screen of the app from the state, the
/// editors' presentations, the instant, the locale and the time zone. It
/// writes every word the screens show.
public enum TasksPresenter {
    public static func present(_ input: PresenterInput<TasksApp>, linked: LinkedRecords, device: String, build: BuildFacts) -> TasksPresentation {
        let state = input.state
        let calendar = input.calendar
        let model = TasksModel(state.records, in: calendar)
        let today = CivilDay(input.now, in: calendar)
        let words = Words(now: input.now, calendar: calendar, locale: input.locale, model: model)
        // Monotonic in both parts: a new record, setting or selection, and
        // a new minute (an hour passing makes a timed task overdue).
        let revision = state.revision + Int(input.now.timeIntervalSince1970 / 60)
        let needsYou = model.needsYouCount(now: input.now, today: today, calendar: calendar)

        let plan = Reminders.plan(
            model, settings: state.settings, permission: state.permission, now: input.now, calendar: calendar,
            locale: input.locale)
        let midnight = today.adding(days: 1, in: calendar).start(in: calendar)
        let nextDue = model.tasks.values.compactMap { $0.isOpen ? $0.due?.instant : nil }.filter { $0 > input.now }.min()

        return TasksPresentation(
            menu: TasksMenu.present(selected: state.section, needsYou: needsYou),
            sidebar: sidebar(state, model: model, needsYou: needsYou, revision: revision),
            list: state.selection.map { list($0, state: state, model: model, today: today, words: words, revision: revision) },
            detail: state.openTask.flatMap { model.tasks[$0] }.map { detail($0, state: state, model: model, words: words) },
            editors: input.children,
            confirmation: state.confirmation.flatMap { confirmation($0, model: model) },
            settings: TasksSettings.sections(
                state.settings, permission: state.permission, scheduled: state.scheduled?.count ?? 0,
                device: device, locale: input.locale),
            storage: SettingsPresenter.storage(state.usage ?? [], budget: nil),
            about: AboutPresentation.present(
                appName: TasksApp.appName, version: build.version, channel: build.channel,
                offered: state.offered, privacyPolicy: build.privacyPolicy),
            notices: linked.notice(state.linked).map { [$0] } ?? [],
            problem: state.problem.map { ProblemPresentation(title: "A change was not kept", message: $0) },
            reminders: plan,
            nextChange: [midnight, nextDue].compactMap { $0 }.min())
    }

    // MARK: The lists column

    static func sidebar(_ state: TasksState, model: TasksModel, needsYou: Int, revision: Int) -> SidebarPresentation {
        func row(_ id: String, _ selection: ListSelection, _ title: String, _ symbol: String, actions: [ActionButton<ListAction>] = [])
            -> SidebarRow
        {
            let open = selection == .needsYou ? needsYou : model.tasks(in: selection).filter(\.isOpen).count
            return SidebarRow(
                id: id, selection: selection, title: title, symbol: symbol, open: open,
                openWords: open == 1 ? "1 open task" : "\(open) open tasks", actions: actions)
        }
        let lists = model.arrangedLists.map { list in
            row(
                list.id, .list(list.id), list.name.isEmpty ? "Untitled list" : list.name, "list",
                actions: [ActionButton("Rename…", .rename(list.id)), ActionButton("Delete…", .delete(list.id))])
        }
        return SidebarPresentation(
            title: "Lists",
            fixed: [row("needs", .needsYou, "Needs you", "needs"), row("inbox", .inbox, InboxWords.title, "inbox")],
            listsTitle: "My Lists", lists: KeyedList(lists, revision: revision), newList: "New List",
            noLists: lists.isEmpty ? "No lists yet. Add one to sort your tasks." : nil, selected: state.selection)
    }

    // MARK: A list's tasks

    static func list(_ selection: ListSelection, state: TasksState, model: TasksModel, today: CivilDay, words: Words, revision: Int)
        -> ListPresentation
    {
        let menu = TaskMenuWords(
            complete: "Mark as Done", abandon: "Won't Do", reopen: "Reopen", edit: "Edit", priority: TaskField.priority.label,
            priorities: priorities, move: "Move to List", delete: "Delete Task", swipeDelete: "Delete")
        if selection == .needsYou { return needsYou(state: state, model: model, today: today, words: words, revision: revision, menu: menu) }
        let order = TasksSettings.order(state.settings)
        let showsClosed = TasksSettings.showsCompleted(state.settings)
        let tasks = model.tasks(in: selection)
        let open = order.sorted(tasks.filter(\.isOpen), in: words.calendar)
        let closed = tasks.filter { !$0.isOpen }.sorted { ($0.completedAt ?? .distantPast, $0.id) > ($1.completedAt ?? .distantPast, $1.id) }
        let title: String
        var actions: [ActionButton<ListAction>] = []
        switch selection {
        case .needsYou: title = "Needs you"
        case .inbox: title = InboxWords.title
        case let .list(id):
            title = model.lists[id].map { $0.name.isEmpty ? "Untitled list" : $0.name } ?? "List"
            actions = [ActionButton("Rename List…", .rename(id)), ActionButton("Delete List…", .delete(id))]
        }
        let empty: EmptyPresentation? =
            if !open.isEmpty { nil } else {
                selection == .inbox
                    ? EmptyPresentation(title: "No tasks", message: "Tasks in no list show here. Tap + to add one.")
                    : EmptyPresentation(title: "No open tasks", message: "Tap + to add a task.")
            }
        let named = selection == .inbox ? "" : "to \(title)"
        let openRows = open.map { row($0, state: state, words: words, mixed: false) }
        let late = openRows.filter { $0.due?.tone == .overdue }.count
        let summary = ListSummary(
            headline: open.isEmpty ? "All done" : "\(open.count) to do", detail: nil, overdue: late == 0 ? nil : "\(late) overdue")
        return ListPresentation(
            selection: selection, title: title, summary: summary, chips: [], bands: [], clear: nil,
            open: KeyedList(openRows, revision: revision), openTitle: "To Do",
            closed: KeyedList(showsClosed ? closed.map { row($0, state: state, words: words, mixed: false) } : [], revision: revision),
            closedTitle: "Done",
            showsClosed: showsClosed, toggleClosed: showsClosed ? "Hide Completed" : "Show Completed",
            quickAdd: named.isEmpty ? "Add a task" : "Add a task \(named)", newTask: "New Task",
            reorders: order == .manual,
            orderNote: order == .due ? "Sorted by due date" : nil, empty: empty, actions: actions,
            rowMenu: menu)
    }

    // MARK: Needs you

    /// Every open task across the lists in its band: what waits on a
    /// decision, with the decisions on its row; what is coming this week;
    /// the rest under Later. The chips narrow it to one list.
    static func needsYou(state: TasksState, model: TasksModel, today: CivilDay, words: Words, revision: Int, menu: TaskMenuWords)
        -> ListPresentation
    {
        let banded = model.banded(filter: state.filter, now: words.now, today: today, calendar: words.calendar)
        let rows = banded.map { row($0.task, state: state, words: words, mixed: true, band: $0.band, today: today) }
        let bands = TaskBand.allCases.compactMap { band -> BandHeading? in
            let count = banded.filter { $0.band == band }.count
            guard count > 0 else { return nil }
            return BandHeading(band: band, title: bandTitle(band), count: count)
        }
        // A chip for every list and the inbox, so a list narrowed to stays
        // chosen while its tasks are done, not only while it holds any.
        var chips = [FilterChip(id: "all", title: "All", selection: nil, selected: state.filter == nil)]
        if model.tasks.values.contains(where: { $0.isOpen && $0.list == nil }) || state.filter == .inbox {
            chips.append(FilterChip(id: "inbox", title: InboxWords.title, selection: .inbox, selected: state.filter == .inbox))
        }
        for list in model.arrangedLists {
            chips.append(
                FilterChip(
                    id: list.id, title: list.name.isEmpty ? "Untitled list" : list.name, selection: .list(list.id),
                    selected: state.filter == .list(list.id)))
        }
        let needing = bands.first?.band == .needsYou
        let empty: EmptyPresentation? =
            if !rows.isEmpty { nil } else if state.filter == nil {
                EmptyPresentation(title: "All clear", message: "Nothing is open. Tap + to add a task.")
            } else {
                EmptyPresentation(title: "All clear", message: "Nothing is open here. Choose All to see every list.")
            }
        let summary = ListSummary(
            headline: needing ? "\(bands[0].count) need you" : "Nothing needs you", detail: dayTitle(today, words: words),
            overdue: nil)
        let narrowed = state.filter.flatMap { filter -> String? in
            switch filter {
            case .inbox: InboxWords.title
            case let .list(id): model.lists[id]?.name
            case .needsYou: nil
            }
        }
        return ListPresentation(
            selection: .needsYou, title: "Needs you", summary: summary, chips: chips.count > 1 ? chips : [], bands: bands,
            clear: !rows.isEmpty && !needing ? "Nothing needs you right now." : nil,
            open: KeyedList(rows, revision: revision), openTitle: "To Do", closed: KeyedList([], revision: revision), closedTitle: "Done",
            showsClosed: false, toggleClosed: "Show Completed",
            quickAdd: narrowed.map { "Add a task for today to \($0)" } ?? "Add a task for today", newTask: "New Task", reorders: false,
            orderNote: nil, empty: empty, actions: [], rowMenu: menu)
    }

    static func bandTitle(_ band: TaskBand) -> String {
        switch band {
        case .needsYou: "Needs you"
        case .upcoming: "Coming up"
        case .later: "Later"
        }
    }

    /// The days to move a task that waits on one to: today where it is
    /// late, tomorrow, a week on, or the editor.
    static func reschedule(_ task: TaskItem, today: CivilDay, words: Words) -> RescheduleMenu {
        let week = today.adding(days: 7, in: words.calendar)
        var options: [ActionButton<Reschedule>] = []
        if task.due.map({ $0.day != today }) ?? true { options.append(ActionButton("Today", .today)) }
        options.append(ActionButton("Tomorrow", .tomorrow))
        options.append(ActionButton(DueWords.dayWords(week, today: today, calendar: words.calendar, locale: words.locale), .nextWeek))
        options.append(ActionButton("Pick a Day…", .pickDay))
        return RescheduleMenu(title: task.due == nil ? "Pick a Day" : "Move", options: options)
    }

    static func row(_ task: TaskItem, state: TasksState, words: Words, mixed: Bool, band: TaskBand? = nil, today: CivilDay? = nil)
        -> TaskRow
    {
        let pending = state.completing[task.id] != nil
        var due = task.due.map { words.due($0) }
        // On Needs you a late task says how late, not when it was due: the
        // day is in the decision its row offers.
        if band == .needsYou, let late = task.due, let today, due?.tone == .overdue { due = DueBadge(text: words.late(late, today: today), tone: .overdue) }
        let priority = task.priority == .none ? nil : PriorityBadge(text: task.priority.words, priority: task.priority)
        let listName = mixed ? task.list.flatMap { words.model.lists[$0]?.name } ?? InboxWords.title : nil
        let title = task.name.isEmpty ? "Untitled task" : task.name
        var spoken = [title]
        if pending { spoken.append("marked done, tap to undo") } else if task.status != .open { spoken.append(task.status.words) }
        if let due { spoken.append(due.tone == .overdue && band == .needsYou ? due.text : "due \(due.text)") }
        if let priority { spoken.append("priority \(priority.text)") }
        if let listName { spoken.append("in \(listName)") }
        if task.notes != nil { spoken.append("has notes") }
        let deciding = band == .needsYou && !pending
        return TaskRow(
            id: task.id, title: title, status: task.status, pending: pending,
            toggle: pending ? "Undo" : task.isOpen ? "Mark as Done" : "Reopen", due: due,
            priority: priority, listName: listName, band: band,
            reschedule: deciding ? today.map { reschedule(task, today: $0, words: words) } : nil,
            decisions: deciding ? [ActionButton("Won't Do", .abandon)] : [],
            hasNotes: task.notes != nil, notes: task.notes.map(preview),
            spoken: spoken.joined(separator: ", "))
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

    // MARK: The open task

    static func detail(_ task: TaskItem, state: TasksState, model: TasksModel, words: Words) -> TaskDetailPresentation {
        let pending = state.completing[task.id] != nil
        var fields = [DetailField(field: .status, label: TaskField.status.label, value: pending ? "Done, tap Undo to keep it open" : task.status.words)]
        let listName = task.list.flatMap { model.lists[$0]?.name } ?? InboxWords.title
        let lists = [EditorOption(id: "", title: InboxWords.title)] + model.arrangedLists.map { EditorOption(id: $0.id, title: $0.name) }
        fields.append(
            DetailField(
                field: .list, label: TaskField.list.label, value: listName,
                choice: EditorChoice(label: TaskField.list.label, selected: task.list ?? "", options: lists)))
        if let due = task.due {
            let badge = words.due(due)
            fields.append(
                DetailField(
                    field: .due, label: TaskField.due.label, value: DueWords.full(due, calendar: words.calendar, locale: words.locale),
                    tone: badge.tone))
        } else {
            fields.append(DetailField(field: .due, label: TaskField.due.label, value: "No due date"))
        }
        fields.append(
            DetailField(
                field: .priority, label: TaskField.priority.label, value: task.priority.words,
                choice: EditorChoice(label: TaskField.priority.label, selected: task.priority.rawValue, options: priorities)))
        if let notes = task.notes { fields.append(DetailField(field: .notes, label: TaskField.notes.label, value: notes)) }
        if let url = task.url { fields.append(DetailField(field: .link, label: TaskField.link.label, value: url, url: url)) }
        if task.status == .done, let completed = task.completedAt {
            fields.append(
                DetailField(
                    field: .completed, label: TaskField.completed.label,
                    value: DueWords.full(.at(completed, in: words.calendar), calendar: words.calendar, locale: words.locale)))
        }
        var actions: [ActionButton<TaskAction>] =
            if pending { [ActionButton("Undo", .reopen)] } else {
                switch task.status {
                case .open: [ActionButton("Mark as Done", .complete), ActionButton("Won't Do", .abandon)]
                case .done, .abandoned: [ActionButton("Reopen", .reopen)]
                }
            }
        actions.append(ActionButton("Delete Task", .delete))
        return TaskDetailPresentation(
            id: task.id, title: task.name.isEmpty ? "Untitled task" : task.name, status: task.status, pending: pending, fields: fields,
            actions: actions, edit: "Edit")
    }

    /// Every priority, as the detail and the editor choose among them.
    static var priorities: [EditorOption] {
        TaskPriority.allCases.map { EditorOption(id: $0.rawValue, title: $0.words) }
    }

    // MARK: Questions

    static func confirmation(_ confirmation: Confirmation, model: TasksModel) -> ConfirmationPresentation<Confirmation>? {
        switch confirmation {
        case let .deleteList(id):
            guard let list = model.lists[id] else { return nil }
            let count = model.tasks.values.filter { $0.list == id }.count
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
    }
}
