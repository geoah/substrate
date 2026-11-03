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
        let dueToday = model.tasks(in: .today, today: today).count

        let plan = Reminders.plan(model, settings: state.settings, permission: state.permission, now: input.now, calendar: calendar,
                                  locale: input.locale)
        let midnight = today.adding(days: 1, in: calendar).start(in: calendar)
        let nextDue = model.tasks.values.compactMap { $0.isOpen ? $0.due?.instant : nil }.filter { $0 > input.now }.min()

        return TasksPresentation(
            menu: TasksMenu.present(selected: state.section, dueToday: dueToday),
            sidebar: sidebar(state, model: model, today: today, revision: revision),
            list: state.selection.map { list($0, state: state, model: model, today: today, words: words, revision: revision) },
            detail: state.openTask.flatMap { model.tasks[$0] }.map { detail($0, model: model, words: words) },
            editors: input.children,
            confirmation: state.confirmation.flatMap { confirmation($0, model: model) },
            settings: TasksSettings.sections(state.settings, permission: state.permission, scheduled: state.scheduled?.count ?? 0,
                                             device: device, locale: input.locale),
            storage: SettingsPresenter.storage(state.usage ?? [], budget: nil),
            about: AboutPresentation.present(appName: TasksApp.appName, version: build.version, channel: build.channel,
                                             offered: state.offered),
            notices: linked.notice(state.linked).map { [$0] } ?? [],
            problem: state.problem.map { ProblemPresentation(title: "A change was not kept", message: $0) },
            reminders: plan,
            nextChange: [midnight, nextDue].compactMap { $0 }.min())
    }

    // MARK: The lists column

    static func sidebar(_ state: TasksState, model: TasksModel, today: CivilDay, revision: Int) -> SidebarPresentation {
        func row(_ id: String, _ selection: ListSelection, _ title: String, _ symbol: String, actions: [ActionButton<ListAction>] = [])
            -> SidebarRow
        {
            let open = model.tasks(in: selection, today: today).filter(\.isOpen).count
            return SidebarRow(id: id, selection: selection, title: title, symbol: symbol, open: open,
                              openWords: open == 1 ? "1 open task" : "\(open) open tasks", actions: actions)
        }
        let lists = model.arrangedLists.map { list in
            row(list.id, .list(list.id), list.name.isEmpty ? "Untitled list" : list.name, "list",
                actions: [ActionButton("Rename…", .rename(list.id)), ActionButton("Delete…", .delete(list.id))])
        }
        return SidebarPresentation(
            title: "Lists",
            fixed: [row("today", .today, "Today", "today"), row("inbox", .inbox, InboxWords.title, "inbox")],
            listsTitle: "My Lists", lists: KeyedList(lists, revision: revision), newList: "New List",
            noLists: lists.isEmpty ? "No lists yet. Add one to sort your tasks." : nil, selected: state.selection)
    }

    // MARK: A list's tasks

    static func list(_ selection: ListSelection, state: TasksState, model: TasksModel, today: CivilDay, words: Words, revision: Int)
        -> ListPresentation
    {
        let order = selection == .today ? TaskOrder.due : TasksSettings.order(state.settings)
        let showsClosed = selection != .today && TasksSettings.showsCompleted(state.settings)
        let tasks = model.tasks(in: selection, today: today)
        let open = order.sorted(tasks.filter(\.isOpen), in: words.calendar)
        let closed = tasks.filter { !$0.isOpen }.sorted { ($0.completedAt ?? .distantPast, $0.id) > ($1.completedAt ?? .distantPast, $1.id) }
        let title: String
        var actions: [ActionButton<ListAction>] = []
        switch selection {
        case .today: title = "Today"
        case .inbox: title = InboxWords.title
        case let .list(id):
            title = model.lists[id].map { $0.name.isEmpty ? "Untitled list" : $0.name } ?? "List"
            actions = [ActionButton("Rename List…", .rename(id)), ActionButton("Delete List…", .delete(id))]
        }
        let empty: EmptyPresentation? = if !open.isEmpty { nil } else {
            switch selection {
            case .today: EmptyPresentation(title: "Nothing due today", message: "Tasks due today or earlier show here.")
            case .inbox: EmptyPresentation(title: "No tasks", message: "Tasks in no list show here. Add one below.")
            case .list: EmptyPresentation(title: "No open tasks", message: "Add a task below.")
            }
        }
        let named = selection == .today ? "for today" : selection == .inbox ? "" : "to \(title)"
        let menu = TaskMenuWords(complete: "Mark as Done", abandon: "Won't Do", reopen: "Reopen", edit: "Edit", priority: TaskField.priority.label,
                                 priorities: priorities, move: "Move to List", delete: "Delete Task", swipeDelete: "Delete")
        return ListPresentation(
            selection: selection, title: title,
            open: KeyedList(open.map { row($0, words: words, mixed: selection == .today) }, revision: revision),
            closed: KeyedList(showsClosed ? closed.map { row($0, words: words, mixed: false) } : [], revision: revision),
            closedTitle: closed.count == 1 ? "1 Completed" : "\(closed.count) Completed",
            showsClosed: showsClosed, toggleClosed: showsClosed ? "Hide Completed" : "Show Completed",
            quickAdd: named.isEmpty ? "Add a task" : "Add a task \(named)", newTask: "New Task",
            reorders: selection != .today && order == .manual,
            orderNote: selection != .today && order == .due ? "Sorted by due date" : nil, empty: empty, actions: actions,
            rowMenu: menu)
    }

    static func row(_ task: TaskItem, words: Words, mixed: Bool) -> TaskRow {
        let due = task.due.map { words.due($0) }
        let priority = task.priority == .none ? nil : PriorityBadge(text: task.priority.words, priority: task.priority)
        let listName = mixed ? task.list.flatMap { words.model.lists[$0]?.name } ?? InboxWords.title : nil
        let title = task.name.isEmpty ? "Untitled task" : task.name
        var spoken = [title]
        if task.status != .open { spoken.append(task.status.words) }
        if let due { spoken.append("due \(due.text)") }
        if let priority { spoken.append("priority \(priority.text)") }
        if let listName { spoken.append("in \(listName)") }
        return TaskRow(id: task.id, title: title, status: task.status, toggle: task.isOpen ? "Mark as Done" : "Reopen", due: due,
                       priority: priority, listName: listName, hasNotes: task.notes != nil, spoken: spoken.joined(separator: ", "))
    }

    // MARK: The open task

    static func detail(_ task: TaskItem, model: TasksModel, words: Words) -> TaskDetailPresentation {
        var fields = [DetailField(field: .status, label: TaskField.status.label, value: task.status.words)]
        let listName = task.list.flatMap { model.lists[$0]?.name } ?? InboxWords.title
        let lists = [EditorOption(id: "", title: InboxWords.title)] + model.arrangedLists.map { EditorOption(id: $0.id, title: $0.name) }
        fields.append(DetailField(field: .list, label: TaskField.list.label, value: listName,
                                  choice: EditorChoice(label: TaskField.list.label, selected: task.list ?? "", options: lists)))
        if let due = task.due {
            let badge = words.due(due)
            fields.append(DetailField(field: .due, label: TaskField.due.label, value: DueWords.full(due, calendar: words.calendar, locale: words.locale),
                                      tone: badge.tone))
        } else {
            fields.append(DetailField(field: .due, label: TaskField.due.label, value: "No due date"))
        }
        fields.append(DetailField(field: .priority, label: TaskField.priority.label, value: task.priority.words,
                                  choice: EditorChoice(label: TaskField.priority.label, selected: task.priority.rawValue, options: priorities)))
        if let notes = task.notes { fields.append(DetailField(field: .notes, label: TaskField.notes.label, value: notes)) }
        if let url = task.url { fields.append(DetailField(field: .link, label: TaskField.link.label, value: url, url: url)) }
        if task.status == .done, let completed = task.completedAt {
            fields.append(DetailField(field: .completed, label: TaskField.completed.label,
                                      value: DueWords.full(.at(completed, in: words.calendar), calendar: words.calendar, locale: words.locale)))
        }
        var actions: [ActionButton<TaskAction>] = switch task.status {
        case .open: [ActionButton("Mark as Done", .complete), ActionButton("Won't Do", .abandon)]
        case .done, .abandoned: [ActionButton("Reopen", .reopen)]
        }
        actions.append(ActionButton("Delete Task", .delete))
        return TaskDetailPresentation(id: task.id, title: task.name.isEmpty ? "Untitled task" : task.name, status: task.status, fields: fields,
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
            let message = switch count {
            case 0: "The list has no tasks. You can restore it from your substrate's console."
            case 1: "Its 1 task is deleted with it. You can restore both from your substrate's console."
            default: "Its \(count) tasks are deleted with it. You can restore them from your substrate's console."
            }
            return ConfirmationPresentation(question: confirmation, title: "Delete \(name)?", message: message,
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
    }
}
