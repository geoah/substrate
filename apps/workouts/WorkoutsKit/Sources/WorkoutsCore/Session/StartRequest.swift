/// How a new session starts: from a routine or from nothing.
public enum StartRequest: Codable, Equatable, Sendable {
    case routine(RoutineID)
    case empty
}
