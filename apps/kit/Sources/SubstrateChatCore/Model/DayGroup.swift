import Foundation

/// Where a chat sits in the list, by its last activity.
enum DayGroup: String, CaseIterable, Codable, Sendable {
    case today = "Today"
    case yesterday = "Yesterday"
    case week = "Previous 7 Days"
    case earlier = "Earlier"

    static func of(_ date: Date, now: Date, calendar: Calendar) -> DayGroup {
        let start = calendar.startOfDay(for: now)
        if date >= start { return .today }
        if let y = calendar.date(byAdding: .day, value: -1, to: start), date >= y { return .yesterday }
        if let w = calendar.date(byAdding: .day, value: -7, to: start), date >= w { return .week }
        return .earlier
    }
}
