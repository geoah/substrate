import Foundation

/// One `substrate.reamde.dev/tasks/task` record. Named apart from Swift's
/// `Task`, which every file here also reaches for.
public struct TaskItem: Codable, Hashable, Sendable, Identifiable {
    /// A lowercase UUIDv7 chosen on the device, so every retry of the
    /// create lands the same row.
    public var id: String
    public var name: String
    public var notes: String?
    public var url: String?
    /// The task list's id; nil is in no list, the app's inbox.
    public var list: String?
    public var position: Decimal?
    public var due: Due?
    public var priority: TaskPriority
    public var status: TaskStatus
    public var completedAt: Date?
    /// The version the server last answered; nil for a task not sent yet.
    public var version: Int?
    public var createdAt: Date?

    public init(id: String, name: String, notes: String? = nil, url: String? = nil, list: String? = nil, position: Decimal? = nil,
                due: Due? = nil, priority: TaskPriority = .none, status: TaskStatus = .open, completedAt: Date? = nil,
                version: Int? = nil, createdAt: Date? = nil)
    {
        self.id = id
        self.name = name
        self.notes = notes
        self.url = url
        self.list = list
        self.position = position
        self.due = due
        self.priority = priority
        self.status = status
        self.completedAt = completedAt
        self.version = version
        self.createdAt = createdAt
    }

    public var isOpen: Bool { status == .open }
}
