import Foundation
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens

/// One task's edit form, or a new task's, kept by the app's root under a
/// key it minted. The value carries what the root hands it (the task as it
/// stands, the lists, where a new task goes, how its save is going); the
/// draft and the task as it was when the editor opened are its own state,
/// so a change to the task on the server while the owner types leaves the
/// typing alone, and Save patches only what the owner changed, on the
/// version they saw: a change made elsewhere meanwhile is a conflict, never
/// written over. The root closes the editor once the outbox has the write,
/// or at Cancel.
public struct TaskEditor: Controller, Equatable {
    public enum Command: Bridged, Equatable {
        case setName(String)
        case setNotes(String)
        case setLink(String)
        case setList(String?)
        case setDueKind(DueKind)
        /// The picker's value: a day, or a day and time, by the due kind.
        case setDueDate(Date)
        /// A value of the collection's priority; nil for none.
        case setPriority(String?)
        case save
        case cancel
    }

    public enum Event: Codable, Equatable, Sendable {
        case named(String)
        case noted(String)
        case linked(String)
        case listed(String?)
        case dueSet(DueKind, day: CivilDay?, at: Date?)
        case prioritized(String?)
        /// What saving writes, and the task it writes.
        case saved(LocalWrite, task: String)
        case cancelled
    }

    public struct State: Codable, Equatable, Sendable {
        public var draft: TaskDraft
        /// The task as it stood when the editor opened, nil for a new one:
        /// what a save patches, on its version.
        public var seen: TaskItem?
        /// The owner changed something: a problem with the draft is said.
        public var touched = false
    }

    public typealias Presentation = TaskEditorPresentation

    /// The task as it stands, read again with each change to it; nil for a
    /// new one.
    public let original: TaskItem?
    /// The task's collection, or the one new tasks go to: a save writes
    /// through its readings, and the form shows the fields they name.
    public let collection: TaskCollection
    /// The group a new task goes in, by path, and the day it is due on
    /// (Needs you's).
    public let seedList: String?
    public let seedDay: CivilDay?
    /// Every group of the collection's group kind, in its order, as
    /// `(path, name)`.
    public let lists: [EditorOption]
    /// The highest position in each group, by path ("" for none): a new or
    /// moved task goes after it.
    public let lastPositions: [String: Decimal]
    /// The last save, as the root follows it to the outbox; nil before one.
    public let saving: EditorSave?

    public init(
        original: TaskItem?, collection: TaskCollection, seedList: String? = nil, seedDay: CivilDay? = nil, lists: [EditorOption],
        lastPositions: [String: Decimal],
        saving: EditorSave? = nil
    ) {
        self.original = original
        self.collection = collection
        self.seedList = seedList
        self.seedDay = seedDay
        self.lists = lists
        self.lastPositions = lastPositions
        self.saving = saving
    }

    public var initialState: State {
        State(
            draft: original.map(TaskDraft.init)
                ?? TaskDraft(list: seedList, dueKind: seedDay == nil || collection.dueDay == nil ? .none : .day, day: seedDay),
            seen: original)
    }

    /// A save is on its way to the outbox: Save waits for it.
    var sending: Bool {
        if case .sending = saving { true } else { false }
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
            guard !draft.trimmedName.isEmpty, !sending else { return [] }
            return [save(context.state, writes: TaskWrites(mint: { context.mint() }))]
        }
    }

    /// The write a save makes: a create of a new task in the list chosen,
    /// after its last task; or a patch of the fields the owner changed, on
    /// the version they saw (`seen`), never merged over, with a moved task
    /// put after the last of its new list. A task not sent yet has no
    /// version, and its create goes first in the same batch.
    func save(_ state: State, writes: TaskWrites) -> Event {
        let draft = state.draft
        guard let seen = state.seen else {
            var task = draft.applied(to: TaskItem(kind: collection.kind, recordID: writes.newID(), name: ""))
            task.position = Position.between(lastPositions[draft.list ?? ""], nil)
            return .saved(writes.create(task, in: collection), task: task.id)
        }
        var task = draft.applied(to: seen, changedFrom: TaskDraft(seen))
        if task.group != seen.group {
            let path: String = if case let .record(path)? = task.group { path } else { "" }
            task.position = Position.between(lastPositions[path], nil)
        }
        return .saved(writes.edit(seen, to: task, in: collection), task: seen.id)
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
        // Touched by a change, never by a field telling the draft what it
        // already holds (a field focusing, a picker drawn).
        if next.draft != state.draft { next.touched = true }
        return next
    }

    // MARK: The presenter

    public func present(_ input: PresenterInput<TaskEditor>) -> TaskEditorPresentation {
        let draft = input.state.draft
        let calendar = input.calendar
        let date: Date? =
            switch draft.dueKind {
            case .none: nil
            case .day: draft.day?.start(in: calendar, hour: 12)
            case .time: draft.at
            }
        let named = !draft.trimmedName.isEmpty
        let notKept: String? = if case let .notKept(words) = saving { words } else { nil }
        let reference = collection.group
        let none = reference.map { collection.isOwn ? InboxWords.title : "No \($0.label)" } ?? InboxWords.title
        let dueKinds = DueKind.allCases.filter { kind in
            switch kind {
            case .none: true
            case .day: collection.dueDay != nil
            case .time: collection.due != nil
            }
        }
        var priorities = collection.priorities.map { EditorOption(id: $0, title: collection.declaration.label(of: $0, in: collection.priority ?? "")) }
        if !collection.priorities.contains("none") { priorities.insert(EditorOption(id: "", title: "None"), at: 0) }
        return TaskEditorPresentation(
            title: original == nil ? "New Task" : "Edit Task",
            name: EditorText(label: TaskField.taskName.label, value: draft.name, prompt: "What to do"),
            notes: collection.notes.map { _ in EditorText(label: TaskField.notes.label, value: draft.notes, prompt: "Details") },
            link: collection.link.map { _ in EditorText(label: TaskField.link.label, value: draft.link, prompt: "https://example.com") },
            list: reference.map { reference in
                EditorChoice(
                    label: propertyLabel(reference.property), selected: draft.list ?? "", options: [EditorOption(id: "", title: none)] + lists)
            },
            due: dueKinds.count > 1
                ? EditorDue(
                    label: TaskField.due.label, kind: draft.dueKind,
                    kinds: dueKinds.map { EditorOption(id: $0.rawValue, title: $0.words) }, date: date,
                    dateLabel: draft.dueKind == .none ? nil : draft.dueKind == .day ? "Day" : "Day and Time") : nil,
            priority: collection.priority.map { _ in
                EditorChoice(label: TaskField.priority.label, selected: draft.priority ?? "", options: priorities)
            },
            save: notKept != nil ? "Retry" : original == nil ? "Add" : "Save", canSave: named && !sending, cancel: "Cancel",
            problem: input.state.touched && !named ? "A task needs a name." : nil,
            notKept: notKept.map { ProblemPresentation(title: "A change was not kept", message: $0) })
    }
}
