import Foundation

/// How a list's open tasks are ordered, as the owner chose in Settings.
public enum TaskOrder: String, Codable, Hashable, Sendable, CaseIterable {
    /// By `position`, the order the owner arranged; tasks with none last,
    /// oldest first.
    case manual
    /// By when they are due, soonest first; tasks with no due date last,
    /// then by `position`.
    case due

    public func sorted(_ tasks: [TaskItem], in calendar: Calendar) -> [TaskItem] {
        tasks.sorted { a, b in
            switch self {
            case .manual:
                return Self.manual(a, b)
            case .due:
                switch (a.due, b.due) {
                case let (x?, y?):
                    let (kx, ky) = (x.sortKey(in: calendar), y.sortKey(in: calendar))
                    return kx != ky ? kx < ky : Self.manual(a, b)
                case (.some, nil): return true
                case (nil, .some): return false
                case (nil, nil): return Self.manual(a, b)
                }
            }
        }
    }

    static func manual(_ a: TaskItem, _ b: TaskItem) -> Bool {
        switch (a.position, b.position) {
        case let (x?, y?) where x != y: return x < y
        case (.some, nil): return true
        case (nil, .some): return false
        default: return a.id < b.id
        }
    }
}

public extension [TaskList] {
    /// By `position`, lists with none last, then by name.
    var arranged: [TaskList] {
        sorted { a, b in
            switch (a.position, b.position) {
            case let (x?, y?) where x != y: return x < y
            case (.some, nil): return true
            case (nil, .some): return false
            default: return (a.name.lowercased(), a.id) < (b.name.lowercased(), b.id)
            }
        }
    }
}
