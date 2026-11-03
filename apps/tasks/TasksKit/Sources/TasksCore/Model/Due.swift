import Foundation

/// When a task is due: on a day, or at a moment. A timed task carries the
/// day too, the day of the moment on the writer's clock, so "due by Friday"
/// is one filter on `dueDay` for both (apps/tasks/package/README.md).
public enum Due: Codable, Hashable, Sendable {
    case day(CivilDay)
    case time(Date, day: CivilDay)

    /// A moment, with its day in `calendar`'s time zone.
    public static func at(_ instant: Date, in calendar: Calendar) -> Due {
        .time(instant, day: CivilDay(instant, in: calendar))
    }

    public var day: CivilDay {
        switch self {
        case let .day(day), let .time(_, day): day
        }
    }

    public var instant: Date? {
        if case let .time(instant, _) = self { return instant }
        return nil
    }

    /// When it is due, to order by: the moment, or the day's start.
    public func sortKey(in calendar: Calendar) -> Date {
        instant ?? day.start(in: calendar)
    }
}
