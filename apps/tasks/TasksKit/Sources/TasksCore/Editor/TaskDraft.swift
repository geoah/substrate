import Foundation
import SubstrateCore

/// How a draft is due: not at all, on a day, or at a moment.
public enum DueKind: String, Bridged, Hashable, Sendable, CaseIterable {
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
/// form shows, as typed. The group is the group record's path, nil for
/// none; the priority is the collection's own value, nil for none.
public struct TaskDraft: Codable, Hashable, Sendable {
    public var name: String
    public var notes: String
    public var link: String
    public var list: String?
    public var dueKind: DueKind
    public var day: CivilDay?
    public var at: Date?
    public var priority: String?

    public init(
        name: String = "", notes: String = "", link: String = "", list: String? = nil, dueKind: DueKind = .none,
        day: CivilDay? = nil, at: Date? = nil, priority: String? = nil
    ) {
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
        let group: String? = if case let .record(path)? = task.group { path } else { nil }
        self.init(name: task.name, notes: task.notes ?? "", link: task.url ?? "", list: group, priority: task.priorityValue)
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
        next.group = list.map(TaskGroupLink.record) ?? TaskGroupLink.none
        next.due = due
        next.priority = priority.flatMap(TaskPriority.init(rawValue:)) ?? .none
        next.priorityValue = priority
        return next
    }

    /// `task` with the fields the owner changed since `opened` over it: a
    /// field left as it was is never written, even where the draft would
    /// spell it otherwise (a name with spaces around it).
    public func applied(to task: TaskItem, changedFrom opened: TaskDraft) -> TaskItem {
        let changed = applied(to: task)
        var next = task
        if name != opened.name { next.name = changed.name }
        if notes != opened.notes { next.notes = changed.notes }
        if link != opened.link { next.url = changed.url }
        if list != opened.list { next.group = changed.group }
        if due != opened.due { next.due = changed.due }
        if priority != opened.priority {
            next.priority = changed.priority
            next.priorityValue = changed.priorityValue
        }
        return next
    }
}
