import Foundation

/// A day on the calendar with no instant: what `dueDay` holds, written
/// `YYYY-MM-DD`. Midnight is in no zone until a calendar is named.
public struct CivilDay: Codable, Hashable, Sendable, Comparable, CustomStringConvertible {
    public var year: Int
    public var month: Int
    public var day: Int

    public init(year: Int, month: Int, day: Int) {
        self.year = year
        self.month = month
        self.day = day
    }

    /// `YYYY-MM-DD`, nil for anything else or a day no month has.
    public init?(_ text: String) {
        let parts = text.split(separator: "-", omittingEmptySubsequences: false)
        guard parts.count == 3, parts[0].count == 4, parts[1].count == 2, parts[2].count == 2,
              let year = Int(parts[0]), let month = Int(parts[1]), let day = Int(parts[2]),
              (1 ... 12).contains(month), day >= 1
        else { return nil }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        guard let first = calendar.date(from: DateComponents(year: year, month: month, day: 1)),
              let days = calendar.range(of: .day, in: .month, for: first), days.contains(day)
        else { return nil }
        self.init(year: year, month: month, day: day)
    }

    /// The day `instant` falls on in `calendar`'s time zone.
    public init(_ instant: Date, in calendar: Calendar) {
        let parts = calendar.dateComponents([.year, .month, .day], from: instant)
        self.init(year: parts.year ?? 1970, month: parts.month ?? 1, day: parts.day ?? 1)
    }

    public var description: String {
        String(format: "%04d-%02d-%02d", year, month, day)
    }

    /// The instant the day starts at, or `hour`:`minute` of it, in
    /// `calendar`'s time zone.
    public func start(in calendar: Calendar, hour: Int = 0, minute: Int = 0) -> Date {
        calendar.date(from: DateComponents(year: year, month: month, day: day, hour: hour, minute: minute)) ?? .distantPast
    }

    /// The day `count` days later (earlier when negative).
    public func adding(days count: Int, in calendar: Calendar) -> CivilDay {
        let moved = calendar.date(byAdding: .day, value: count, to: start(in: calendar, hour: 12)) ?? start(in: calendar)
        return CivilDay(moved, in: calendar)
    }

    public static func < (lhs: CivilDay, rhs: CivilDay) -> Bool {
        (lhs.year, lhs.month, lhs.day) < (rhs.year, rhs.month, rhs.day)
    }

    public init(from decoder: any Decoder) throws {
        let text = try decoder.singleValueContainer().decode(String.self)
        guard let day = CivilDay(text) else {
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "not a civil date: \(text)"))
        }
        self = day
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(description)
    }
}
