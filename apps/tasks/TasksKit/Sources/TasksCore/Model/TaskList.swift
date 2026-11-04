import Foundation

/// One `substrate.reamde.dev/tasks/list` record: a task list, a name and a
/// place among the owner's lists.
public struct TaskList: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var position: Decimal?
    public var version: Int?

    public init(id: String, name: String, position: Decimal? = nil, version: Int? = nil) {
        self.id = id
        self.name = name
        self.position = position
        self.version = version
    }
}
