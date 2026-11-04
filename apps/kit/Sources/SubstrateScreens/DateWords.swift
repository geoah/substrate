import Foundation

/// Dates as a screen says them, in the environment's locale and time zone,
/// never the process's.
public enum DateWords {
    /// A day as a person reads it: "1 December", the year added once it is
    /// not this one.
    public static func day(_ date: Date, now: Date, locale: Locale, timeZone: TimeZone) -> String {
        let calendar = calendar(locale, timeZone)
        let sameYear = calendar.component(.year, from: date) == calendar.component(.year, from: now)
        let style = Date.FormatStyle(locale: locale, calendar: calendar, timeZone: timeZone).day().month(.wide)
        return date.formatted(sameYear ? style : style.year())
    }

    /// A day and a time, abbreviated: "1 Dec 2026 at 09:00".
    public static func moment(_ date: Date, locale: Locale, timeZone: TimeZone) -> String {
        date.formatted(
            Date.FormatStyle(
                date: .abbreviated, time: .shortened, locale: locale, calendar: calendar(locale, timeZone),
                timeZone: timeZone))
    }

    /// A day, abbreviated: "1 Dec 2026".
    public static func date(_ date: Date, locale: Locale, timeZone: TimeZone) -> String {
        date.formatted(
            Date.FormatStyle(
                date: .abbreviated, time: .omitted, locale: locale, calendar: calendar(locale, timeZone),
                timeZone: timeZone))
    }

    private static func calendar(_ locale: Locale, _ timeZone: TimeZone) -> Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.locale = locale
        calendar.timeZone = timeZone
        return calendar
    }
}
