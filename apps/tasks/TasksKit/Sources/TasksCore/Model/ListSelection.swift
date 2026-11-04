import SubstrateCore

/// What the list column shows: the tasks due today or before, the tasks in
/// no list (the inbox), or one task list's.
public enum ListSelection: Bridged, Hashable, Sendable {
    case today
    case inbox
    case list(String)

    /// The list a task added here goes into; nil is none.
    public var listID: String? {
        if case let .list(id) = self { return id }
        return nil
    }
}
