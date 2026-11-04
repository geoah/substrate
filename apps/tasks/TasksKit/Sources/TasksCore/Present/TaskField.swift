import SubstrateCore

/// A task's facts, each with the one label it has in the read view and the
/// edit view alike.
public enum TaskField: String, Bridged, Hashable, Sendable, CaseIterable {
    /// Its raw value is the property's; the case is named apart because
    /// Kotlin's enums take `name` for themselves.
    case taskName = "name"
    case notes
    case link
    case list
    case due
    case priority
    case status
    case completed

    public var label: String {
        switch self {
        case .taskName: "Name"
        case .notes: "Notes"
        case .link: "Link"
        case .list: "List"
        case .due: "Due"
        case .priority: "Priority"
        case .status: "Status"
        case .completed: "Completed"
        }
    }
}

public extension TaskPriority {
    var words: String {
        switch self {
        case .none: "None"
        case .low: "Low"
        case .medium: "Medium"
        case .high: "High"
        case .urgent: "Urgent"
        }
    }
}

public extension TaskStatus {
    var words: String {
        switch self {
        case .open: "Open"
        case .done: "Done"
        case .abandoned: "Won't do"
        }
    }
}

/// What the owner calls a task in no list.
public enum InboxWords {
    public static let title = "No List"
}
