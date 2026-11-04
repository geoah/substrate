import EventKit
import Foundation

/// EventKit's reminders as providers.substrate.reamde.dev/apple declares
/// them: a list is a `reminderlist`, an item a `reminder`. Enum values are
/// EventKit's case names as one lowercase word.
enum ReminderMapping {
    static let listKind = "providers.substrate.reamde.dev/apple/reminderlist"
    static let kind = "providers.substrate.reamde.dev/apple/reminder"
    static let eventstoreKind = EventMapping.eventstoreKind

    static func list(_ c: EKCalendar, eventstore: String) -> JSON {
        var f = Fields()
        f.set("eventstore", reference(eventstoreKind, eventstore))
        f.set("calendarIdentifier", c.calendarIdentifier)
        f.set("listTitle", c.title)
        f.set("type", EventMapping.calendarType(c.type))
        f.set("sourceTitle", c.source?.title)
        f.set("sourceType", c.source.map { EventMapping.sourceType($0.sourceType) })
        f.set("color", EventMapping.hex(c.cgColor))
        f.set("allowsContentModifications", c.allowsContentModifications)
        return f.json
    }

    static func reminder(_ r: EKReminder, list: String) -> JSON {
        var f = Fields()
        f.set("list", reference(listKind, list))
        f.set("calendarItemIdentifier", r.calendarItemIdentifier)
        f.set("calendarItemExternalIdentifier", r.calendarItemExternalIdentifier)
        f.set("reminderTitle", r.title)
        f.set("notes", r.notes)
        f.set("url", r.url?.absoluteString)
        f.set("location", r.location)
        f.set("priority", r.priority)
        f.set("isCompleted", r.isCompleted)
        f.set("completionDate", r.completionDate.map(Formats.utc))
        // A due date is a day, or a day and a time: EventKit keeps which as
        // whether the components carry an hour. The day is the one the
        // components name; the moment only exists with the time.
        let (dueDay, dueDate) = moments(r.dueDateComponents, zone: r.timeZone)
        f.set("dueDay", dueDay)
        f.set("dueDate", dueDate)
        let (startDay, startDate) = moments(r.startDateComponents, zone: r.timeZone)
        f.set("startDay", startDay)
        f.set("startDate", startDate)
        f.set("timeZone", r.timeZone.map(\.identifier).flatMap(EventMapping.ianaZone))
        f.set("hasRecurrenceRules", r.hasRecurrenceRules)
        f.set("recurrenceRules", .array((r.recurrenceRules ?? []).map(EventMapping.rule)))
        f.set("alarms", .array((r.alarms ?? []).map(alarm)))
        f.set("creationDate", r.creationDate.map(Formats.utc))
        f.set("lastModifiedDate", r.lastModifiedDate.map(Formats.utc))
        return f.json
    }

    /// The civil day date components name, and the moment when they carry
    /// a time, in their own zone, else the reminder's, else the phone's.
    static func moments(_ c: DateComponents?, zone: TimeZone?) -> (day: String?, moment: String?) {
        guard let c, let year = c.year, let month = c.month, let day = c.day else { return (nil, nil) }
        let name = String(format: "%04d-%02d-%02d", year, month, day)
        guard c.hour != nil else { return (name, nil) }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = c.timeZone ?? zone ?? .current
        return (name, calendar.date(from: c).map(Formats.utc))
    }

    /// An alarm at a moment, or an offset in seconds from the due date.
    private static func alarm(_ a: EKAlarm) -> JSON {
        var f = Fields()
        if let date = a.absoluteDate {
            f.set("absoluteDate", Formats.utc(date))
        } else {
            f.set("relativeOffset", a.relativeOffset)
        }
        return f.json
    }
}
