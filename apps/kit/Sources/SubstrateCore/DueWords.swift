import Foundation

/// A due date in words, relative to today where that is shorter: "Today",
/// "Tomorrow, 2:00 PM", "Fri, Nov 6", "Overdue since Mon, Nov 2". Every
/// word comes from the locale and the time zone the presenter is given.
public enum DueWords {
    public enum Tone: String, Codable, Hashable, Sendable {
        /// Due later.
        case quiet
        /// Due today.
        case soon
        /// Past its day, or its moment.
        case overdue
    }

    public static func words(_ due: Due, now: Date, calendar: Calendar, locale: Locale) -> (text: String, tone: Tone) {
        let today = CivilDay(now, in: calendar)
        let day = dayWords(due.day, today: today, calendar: calendar, locale: locale)
        let text = due.instant.map { "\(day), \(time($0, calendar: calendar, locale: locale))" } ?? day
        let overdue = due.instant.map { $0 < now } ?? (due.day < today)
        if overdue { return ("Overdue: \(text)", .overdue) }
        return (text, due.day == today ? .soon : .quiet)
    }

    /// "Today", "Tomorrow", "Yesterday", else the weekday and the date,
    /// with the year where it is not this one.
    public static func dayWords(_ day: CivilDay, today: CivilDay, calendar: Calendar, locale: Locale) -> String {
        if day == today { return "Today" }
        if day == today.adding(days: 1, in: calendar) { return "Tomorrow" }
        if day == today.adding(days: -1, in: calendar) { return "Yesterday" }
        let formatter = DateFormatter()
        formatter.locale = locale
        formatter.timeZone = calendar.timeZone
        formatter.setLocalizedDateFormatFromTemplate(day.year == today.year ? "EEEMMMd" : "EEEMMMdyyyy")
        return formatter.string(from: day.start(in: calendar, hour: 12))
    }

    public static func time(_ instant: Date, calendar: Calendar, locale: Locale) -> String {
        let formatter = DateFormatter()
        formatter.locale = locale
        formatter.timeZone = calendar.timeZone
        formatter.setLocalizedDateFormatFromTemplate("jmm")
        return formatter.string(from: instant)
    }

    /// A day and a time in full, for the detail: "Friday, November 6, 2026
    /// at 2:00 PM".
    public static func full(_ due: Due, calendar: Calendar, locale: Locale) -> String {
        let formatter = DateFormatter()
        formatter.locale = locale
        formatter.timeZone = calendar.timeZone
        formatter.dateStyle = .full
        formatter.timeStyle = due.instant == nil ? .none : .short
        return formatter.string(from: due.instant ?? due.day.start(in: calendar, hour: 12))
    }
}
