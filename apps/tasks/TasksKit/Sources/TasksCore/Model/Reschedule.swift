import SubstrateCore

/// Where a task that needs the owner can be moved to from its row: a named
/// day, or the editor to pick one.
public enum Reschedule: String, Bridged, Hashable, Sendable, CaseIterable {
    case today
    case tomorrow
    case nextWeek
    case pickDay
}
