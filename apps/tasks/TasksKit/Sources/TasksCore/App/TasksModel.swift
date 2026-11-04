import Foundation
import SubstrateCore

/// The folded records decoded: every task and list as the owner sees them.
/// A task in a list the repository no longer holds is left out: the list's
/// delete takes it with it at the server's next pass.
public struct TasksModel: Equatable, Sendable {
    public var tasks: [String: TaskItem]
    public var lists: [String: TaskList]

    public init(tasks: [String: TaskItem] = [:], lists: [String: TaskList] = [:]) {
        self.tasks = tasks
        self.lists = lists
    }

    public init(_ records: [String: Record], in calendar: Calendar) {
        var tasks: [String: TaskItem] = [:]
        var lists: [String: TaskList] = [:]
        for record in records.values {
            switch record.kind {
            case TasksKinds.list: lists[record.id] = TasksCodec.list(record)
            case TasksKinds.task: tasks[record.id] = TasksCodec.task(record, in: calendar)
            default: break
            }
        }
        self.tasks = tasks.filter { $0.value.list.map { lists[$0] != nil } ?? true }
        self.lists = lists
    }

    public var arrangedLists: [TaskList] { Array(lists.values).arranged }

    /// The tasks a selection shows, open and closed alike.
    public func tasks(in selection: ListSelection, today: CivilDay) -> [TaskItem] {
        tasks.values.filter { task in
            switch selection {
            case .inbox: task.list == nil
            case let .list(id): task.list == id
            case .today: task.isOpen && task.due.map { $0.day <= today } == true
            }
        }
    }

    /// The highest position among the tasks of a list (nil: the inbox).
    public func lastPosition(inList list: String?) -> Decimal? {
        tasks.values.filter { $0.list == list }.compactMap(\.position).max()
    }
}
