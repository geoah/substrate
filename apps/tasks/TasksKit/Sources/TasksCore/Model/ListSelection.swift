import SubstrateCore

/// What the list column shows: what needs the owner across every collection
/// (the home screen), the suggestions, the open tasks with no date, one
/// group's tasks, a collection's tasks in no group or in a group out of
/// reach, or every task of a collection that is not grouped.
public enum ListSelection: Bridged, Hashable, Sendable {
    case needsYou
    case suggestions
    case noDate
    /// One group's tasks, by the group record's path.
    case group(String)
    /// A collection's tasks in no group, by the collection's kind.
    case ungrouped(String)
    /// A collection's tasks whose group is gone or out of reach.
    case missing(String)
    /// Every task of a collection with no group reading.
    case collection(String)

    /// The app's own tasks in no list.
    public static var inbox: ListSelection { .ungrouped(TasksKinds.task) }

    /// One of the app's own task lists, by its id.
    public static func list(_ id: String) -> ListSelection { .group(RecordReference(kind: TasksKinds.list, id: id).path) }

    /// The path a group selection names.
    public var listPath: String {
        if case let .group(path) = self { return path }
        return ""
    }

    /// The task list a task added here goes into, of the app's own kind;
    /// nil is none.
    public var listID: String? {
        guard case let .group(path) = self, let reference = RecordReference(path: path), reference.kind == TasksKinds.list else { return nil }
        return reference.recordID
    }

    /// The collection a selection is of, where it is one collection's.
    public var collection: String? {
        switch self {
        case let .ungrouped(kind), let .missing(kind), let .collection(kind): kind
        case .group(_): nil
        case .needsYou, .suggestions, .noDate: nil
        }
    }
}
