import SubstrateCore

/// How urgent a task is: the `tasks` sample's ladder and default.
public enum TaskPriority: String, Bridged, Hashable, Sendable, CaseIterable, Comparable {
    case none
    case low
    case medium
    case high
    case urgent

    public var rank: Int { Self.allCases.firstIndex(of: self) ?? 0 }

    public static func < (lhs: TaskPriority, rhs: TaskPriority) -> Bool { lhs.rank < rhs.rank }
}
