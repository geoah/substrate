import Foundation

/// How a draft is due: not at all, on a day, or at a moment.
public enum DueKind: String, Codable, Hashable, Sendable, CaseIterable {
    case none
    case day
    case time

    public var words: String {
        switch self {
        case .none: "No Due Date"
        case .day: "On a Day"
        case .time: "At a Time"
        }
    }
}

/// A task as the editor holds it while the owner types: every field the
/// form shows, as typed.
public struct TaskDraft: Codable, Hashable, Sendable {
    public var name: String
    public var notes: String
    public var link: String
    public var list: String?
    public var dueKind: DueKind
    public var day: CivilDay?
    public var at: Date?
    public var priority: TaskPriority

    public init(name: String = "", notes: String = "", link: String = "", list: String? = nil, dueKind: DueKind = .none,
                day: CivilDay? = nil, at: Date? = nil, priority: TaskPriority = .none)
    {
        self.name = name
        self.notes = notes
        self.link = link
        self.list = list
        self.dueKind = dueKind
        self.day = day
        self.at = at
        self.priority = priority
    }

    public init(_ task: TaskItem) {
        self.init(name: task.name, notes: task.notes ?? "", link: task.url ?? "", list: task.list, priority: task.priority)
        switch task.due {
        case nil: dueKind = .none
        case let .day(day)?: (dueKind, self.day) = (.day, day)
        case let .time(instant, day)?: (dueKind, self.day, at) = (.time, day, instant)
        }
    }

    public var trimmedName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// When it is due, as the task will hold it.
    public var due: Due? {
        switch dueKind {
        case .none: nil
        case .day: day.map(Due.day)
        case .time: at.flatMap { at in day.map { .time(at, day: $0) } }
        }
    }

    /// `task` with the draft's fields over it.
    public func applied(to task: TaskItem) -> TaskItem {
        var next = task
        next.name = trimmedName
        next.notes = notes.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : notes
        let link = link.trimmingCharacters(in: .whitespacesAndNewlines)
        next.url = link.isEmpty ? nil : link
        next.list = list
        next.due = due
        next.priority = priority
        return next
    }
}
