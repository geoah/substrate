import Foundation
import SubstrateCore

/// One record of a collection, as the app reads it through its
/// collection's readings. Named apart from Swift's `Task`, which every file
/// here also reaches for. Its identity is its record path: two kinds may
/// hold the same id.
public struct TaskItem: Codable, Hashable, Sendable, Identifiable {
    /// `<kind>/<id>`.
    public var id: String
    public var kind: String
    /// The record's id within its kind: a lowercase UUIDv7 chosen on the
    /// device for a task the app creates, so every retry lands the same row.
    public var recordID: String
    /// The title shown.
    public var name: String
    /// The description, under its declared label.
    public var notes: String?
    public var url: String?
    /// The group reference, read as the collection's group; nil where the
    /// collection has no group reading.
    public var group: TaskGroupLink?
    public var position: Decimal?
    public var due: Due?
    /// The priority reading's value, and where it stands on the app's
    /// ladder, which a badge is drawn with.
    public var priorityValue: String?
    public var priority: TaskPriority
    public var pressing: Bool
    /// The status value, and where it stands.
    public var status: String?
    public var standing: KindStatus.Standing
    /// What can be done to it, each as the values its move may write.
    public var actions: TaskActions
    public var completedAt: Date?
    /// The version the server last answered; nil for a task not sent yet.
    public var version: Int?
    public var createdAt: Date?

    public init(
        kind: String = TasksKinds.task, recordID: String, name: String, notes: String? = nil, url: String? = nil,
        group: TaskGroupLink? = nil, position: Decimal? = nil, due: Due? = nil, priorityValue: String? = nil,
        priority: TaskPriority = .none, pressing: Bool = false, status: String? = "open", standing: KindStatus.Standing = .open,
        actions: TaskActions = TaskActions(), completedAt: Date? = nil, version: Int? = nil, createdAt: Date? = nil
    ) {
        id = RecordReference(kind: kind, id: recordID).path
        self.kind = kind
        self.recordID = recordID
        self.name = name
        self.notes = notes
        self.url = url
        self.group = group
        self.position = position
        self.due = due
        self.priorityValue = priorityValue
        self.priority = priority
        self.pressing = pressing
        self.status = status
        self.standing = standing
        self.actions = actions
        self.completedAt = completedAt
        self.version = version
        self.createdAt = createdAt
    }

    public var isOpen: Bool { standing == .open }
    public var isSuggestion: Bool { standing == .proposed }
    public var isClosed: Bool { standing == .done || standing == .dropped }

    /// The task list it is in, of the app's own kind: what the editor and
    /// a move read. Nil is none, or a group of another kind.
    public var list: String? {
        guard case let .record(path)? = group, let reference = RecordReference(path: path), reference.kind == TasksKinds.list
        else { return nil }
        return reference.recordID
    }
}

/// Where a task's group reference points.
public enum TaskGroupLink: Codable, Hashable, Sendable {
    /// It holds none.
    case none
    /// It names a record the token does not hold, or holds deleted.
    case missing
    /// A group record, by path.
    case record(String)
}

/// What the owner may decide about a task, each a move to one standing.
public enum TaskDecision: String, Bridged, Hashable, Sendable, CaseIterable {
    /// A suggestion taken on: to an open value that is not proposed.
    case accept
    /// A suggestion turned down: to a dropped value.
    case dismiss
    /// An open task done.
    case complete
    /// An open task dropped.
    case wontDo
    /// A done or dropped task open again.
    case reopen

    /// The standing a record must stand in for it, and the one its move
    /// reaches.
    var from: [KindStatus.Standing] {
        switch self {
        case .accept, .dismiss: [.proposed]
        case .complete, .wontDo: [.open]
        case .reopen: [.done, .dropped]
        }
    }

    var to: KindStatus.Standing {
        switch self {
        case .accept, .reopen: .open
        case .dismiss, .wontDo: .dropped
        case .complete: .done
        }
    }
}

/// The values each decision's move may write, in declared order; empty
/// where it is not offered.
public struct TaskActions: Codable, Hashable, Sendable {
    public var accept: [String] = []
    public var dismiss: [String] = []
    public var complete: [String] = []
    public var wontDo: [String] = []
    public var reopen: [String] = []

    public init() {}

    public subscript(decision: TaskDecision) -> [String] {
        get {
            switch decision {
            case .accept: accept
            case .dismiss: dismiss
            case .complete: complete
            case .wontDo: wontDo
            case .reopen: reopen
            }
        }
        set {
            switch decision {
            case .accept: accept = newValue
            case .dismiss: dismiss = newValue
            case .complete: complete = newValue
            case .wontDo: wontDo = newValue
            case .reopen: reopen = newValue
            }
        }
    }

    /// The decisions offered, in the order a row offers them.
    public var offered: [TaskDecision] { TaskDecision.allCases.filter { !self[$0].isEmpty } }

    /// A status's actions from `value`, under a grant that writes the kind:
    /// each the moves to its standing, never into a proposed value for
    /// accept and reopen, never to an undeclared value.
    public init(status: KindStatus, from value: String?, writable: Bool) {
        guard writable else { return }
        let standing = status.standing(of: value)
        let moves = status.moves(from: value).filter { status.values.contains($0) }
        for decision in TaskDecision.allCases where decision.from.contains(standing) {
            self[decision] = moves.filter { status.standing(of: $0) == decision.to }
        }
    }
}

/// A group record: what a collection's group reference points at (a task
/// list, a project, an area). Its identity is its path.
public struct TaskGroup: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var kind: String
    public var recordID: String
    public var name: String
    public var position: Decimal?
    public var version: Int?
    /// Its status where its kind binds the lifecycle trait, by its label,
    /// and where it stands.
    public var status: String?
    public var standing: KindStatus.Standing?

    public init(
        kind: String = TasksKinds.list, recordID: String, name: String, position: Decimal? = nil, version: Int? = nil,
        status: String? = nil, standing: KindStatus.Standing? = nil
    ) {
        id = RecordReference(kind: kind, id: recordID).path
        self.kind = kind
        self.recordID = recordID
        self.name = name
        self.position = position
        self.version = version
        self.status = status
        self.standing = standing
    }
}
