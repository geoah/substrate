import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime

/// One task's edit form, or a new task's, kept by the app's root under a
/// key it minted. The value carries what the root hands it (the task as it
/// stands, the lists, where a new task goes); the draft is its own state,
/// so a change to the task on the server while the owner types leaves the
/// typing alone. Save makes the write and hands it up; Cancel hands up
/// nothing to write. Either way the root closes it.
public struct TaskEditor: Controller, Equatable {
    public enum Command: Codable, Equatable, Sendable {
        case setName(String)
        case setNotes(String)
        case setLink(String)
        case setList(String?)
        case setDueKind(DueKind)
        /// The picker's value: a day, or a day and time, by the due kind.
        case setDueDate(Date)
        case setPriority(TaskPriority)
        case save
        case cancel
    }

    public enum Event: Codable, Equatable, Sendable {
        case named(String)
        case noted(String)
        case linked(String)
        case listed(String?)
        case dueSet(DueKind, day: CivilDay?, at: Date?)
        case prioritized(TaskPriority)
        /// What saving writes, and the task it writes.
        case saved(LocalWrite, task: String)
        case cancelled
    }

    public struct State: Codable, Equatable, Sendable {
        public var draft: TaskDraft
        /// The owner changed something: a problem with the draft is said.
        public var touched = false
    }

    public typealias Presentation = TaskEditorPresentation

    /// The task as it stands; nil for a new one.
    public let original: TaskItem?
    /// The list a new task goes in, and the day it is due on (Today's).
    public let seedList: String?
    public let seedDay: CivilDay?
    /// Every list, in the owner's order, as `(id, name)`.
    public let lists: [EditorOption]
    /// The highest position in each list ("" for no list): a new or moved
    /// task goes after it.
    public let lastPositions: [String: Decimal]

    public init(original: TaskItem?, seedList: String? = nil, seedDay: CivilDay? = nil, lists: [EditorOption], lastPositions: [String: Decimal])
    {
        self.original = original
        self.seedList = seedList
        self.seedDay = seedDay
        self.lists = lists
        self.lastPositions = lastPositions
    }

    public var initialState: State {
        State(draft: original.map(TaskDraft.init) ?? TaskDraft(list: seedList, dueKind: seedDay == nil ? .none : .day, day: seedDay))
    }

    // MARK: Events

    public func events(for command: Command, in context: Enrichment<TaskEditor>) -> [Event] {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = context.environment.timeZone
        let draft = context.state.draft
        switch command {
        case let .setName(name): return [.named(name)]
        case let .setNotes(notes): return [.noted(notes)]
        case let .setLink(link): return [.linked(link)]
        case let .setList(list): return [.listed(list)]
        case let .setPriority(priority): return [.prioritized(priority)]
        case let .setDueKind(kind):
            let today = CivilDay(context.now, in: calendar)
            let day = draft.day ?? today
            switch kind {
            case .none: return [.dueSet(.none, day: draft.day, at: draft.at)]
            case .day: return [.dueSet(.day, day: day, at: draft.at)]
            case .time:
                // A moment on the day chosen: the hour reminders come at.
                let at = draft.at ?? day.start(in: calendar, hour: 9)
                return [.dueSet(.time, day: CivilDay(at, in: calendar), at: at)]
            }
        case let .setDueDate(date):
            switch draft.dueKind {
            case .none, .day: return [.dueSet(.day, day: CivilDay(date, in: calendar), at: draft.at)]
            case .time: return [.dueSet(.time, day: CivilDay(date, in: calendar), at: date)]
            }
        case .cancel:
            return [.cancelled]
        case .save:
            guard !draft.trimmedName.isEmpty else { return [] }
            return [save(draft, writes: TaskWrites(mint: { context.mint() }))]
        }
    }

    /// The write a save makes: a create of a new task in the list chosen,
    /// after its last task; or a patch of what changed, on the version the
    /// owner saw, never merged over, with a moved task put after the last of
    /// its new list. A task not sent yet has no version, and its create goes
    /// first in the same batch.
    func save(_ draft: TaskDraft, writes: TaskWrites) -> Event {
        guard let original else {
            var task = draft.applied(to: TaskItem(id: writes.newID(), name: ""))
            task.position = Position.between(lastPositions[draft.list ?? ""], nil)
            return .saved(writes.create(task), task: task.id)
        }
        var task = draft.applied(to: original)
        if task.list != original.list { task.position = Position.between(lastPositions[task.list ?? ""], nil) }
        return .saved(writes.edit(original, to: task), task: original.id)
    }

    // MARK: The engine

    public func apply(_ event: Event, to state: State) throws -> State {
        var next = state
        switch event {
        case let .named(name): next.draft.name = name
        case let .noted(notes): next.draft.notes = notes
        case let .linked(link): next.draft.link = link
        case let .listed(list): next.draft.list = list
        case let .dueSet(kind, day, at): (next.draft.dueKind, next.draft.day, next.draft.at) = (kind, day, at)
        case let .prioritized(priority): next.draft.priority = priority
        case .saved, .cancelled: return next
        }
        next.touched = true
        return next
    }

    // MARK: The presenter

    public func present(_ input: PresenterInput<TaskEditor>) -> TaskEditorPresentation {
        let draft = input.state.draft
        let calendar = input.calendar
        let date: Date? = switch draft.dueKind {
        case .none: nil
        case .day: draft.day?.start(in: calendar, hour: 12)
        case .time: draft.at
        }
        let named = !draft.trimmedName.isEmpty
        return TaskEditorPresentation(
            title: original == nil ? "New Task" : "Edit Task",
            name: EditorText(label: TaskField.name.label, value: draft.name, prompt: "What to do"),
            notes: EditorText(label: TaskField.notes.label, value: draft.notes, prompt: "Details"),
            link: EditorText(label: TaskField.link.label, value: draft.link, prompt: "https://example.com"),
            list: EditorChoice(label: TaskField.list.label, selected: draft.list ?? "",
                               options: [EditorOption(id: "", title: InboxWords.title)] + lists),
            due: EditorDue(label: TaskField.due.label, kind: draft.dueKind,
                           kinds: DueKind.allCases.map { EditorOption(id: $0.rawValue, title: $0.words) }, date: date,
                           dateLabel: draft.dueKind == .none ? nil : draft.dueKind == .day ? "Day" : "Day and Time"),
            priority: EditorChoice(label: TaskField.priority.label, selected: draft.priority.rawValue,
                                   options: TasksPresenter.priorities),
            save: original == nil ? "Add" : "Save", canSave: named, cancel: "Cancel",
            problem: input.state.touched && !named ? "A task needs a name." : nil)
    }
}
