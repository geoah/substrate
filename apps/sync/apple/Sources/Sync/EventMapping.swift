import EventKit
import Foundation
import UIKit

/// EventKit's objects as providers.substrate.reamde.dev/apple declares them.
/// Enum values are EventKit's case names as one lowercase word.
enum EventMapping {
    static let calendarKind = "providers.substrate.reamde.dev/apple/calendar"
    static let eventstoreKind = "providers.substrate.reamde.dev/apple/eventstore"

    static func calendar(_ c: EKCalendar, eventstore: String) -> JSON {
        var f = Fields()
        f.set("eventstore", reference(eventstoreKind, eventstore))
        f.set("calendarIdentifier", c.calendarIdentifier)
        f.set("calendarTitle", c.title)
        f.set("type", calendarType(c.type))
        f.set("sourceTitle", c.source?.title)
        f.set("sourceType", c.source.map { sourceType($0.sourceType) })
        f.set("color", hex(c.cgColor))
        f.set("allowsContentModifications", c.allowsContentModifications)
        f.set("isSubscribed", c.isSubscribed)
        f.set("isImmutable", c.isImmutable)
        return f.json
    }

    /// The occurrence's key: its item identifier, and for an occurrence of a
    /// series its original start.
    static func key(_ e: EKEvent) -> String {
        AppleIDs.eventKey(
            item: e.calendarItemIdentifier, occurrence: e.occurrenceDate ?? e.startDate,
            repeats: e.hasRecurrenceRules || e.isDetached)
    }

    static func event(_ e: EKEvent, calendar: String) -> JSON {
        var f = Fields()
        f.set("calendar", reference(calendarKind, calendar))
        f.set("calendarItemIdentifier", e.calendarItemIdentifier)
        f.set("eventIdentifier", e.eventIdentifier)
        f.set("calendarItemExternalIdentifier", e.calendarItemExternalIdentifier)
        f.set("eventTitle", e.title)
        f.set("location", e.location)
        if let place = e.structuredLocation {
            var p = Fields()
            p.set("locationTitle", place.title)
            if let geo = place.geoLocation {
                p.set("latitude", geo.coordinate.latitude)
                p.set("longitude", geo.coordinate.longitude)
            }
            if place.radius > 0 { p.set("radius", place.radius) }
            f.set("structuredLocation", p.json)
        }
        f.set("notes", e.notes)
        f.set("url", e.url?.absoluteString)
        f.set("startDate", e.startDate.map(Formats.utc))
        f.set("endDate", e.endDate.map(Formats.utc))
        f.set("isAllDay", e.isAllDay)
        // The server holds `timeZone` to an IANA name; EventKit can hand out
        // a fixed offset (`GMT+0300`) for a zone it could not map, which
        // would refuse the whole event, so only a known name is sent.
        f.set("timeZone", e.timeZone.map(\.identifier).flatMap(ianaZone))
        f.set("occurrenceDate", e.occurrenceDate.map(Formats.utc))
        f.set("isDetached", e.isDetached)
        f.set("hasRecurrenceRules", e.hasRecurrenceRules)
        f.set("recurrenceRules", .array((e.recurrenceRules ?? []).map(rule)))
        f.set("availability", availability(e.availability))
        f.set("status", status(e.status))
        if let organizer = e.organizer {
            var o = Fields()
            o.set("name", organizer.name)
            o.set("url", participantURL(organizer.url))
            o.set("isCurrentUser", organizer.isCurrentUser)
            f.set("organizer", o.json)
        }
        f.set("attendees", .array((e.attendees ?? []).map { a in
            var p = Fields()
            p.set("name", a.name)
            p.set("url", participantURL(a.url))
            p.set("participantRole", role(a.participantRole))
            p.set("participantStatus", participantStatus(a.participantStatus))
            p.set("participantType", participantType(a.participantType))
            p.set("isCurrentUser", a.isCurrentUser)
            return p.json
        }))
        f.set("creationDate", e.creationDate.map(Formats.utc))
        f.set("lastModifiedDate", e.lastModifiedDate.map(Formats.utc))
        return f.json
    }

    static func ianaZone(_ identifier: String) -> String? {
        TimeZone.knownTimeZoneIdentifiers.contains(identifier) ? identifier : nil
    }

    private static func participantURL(_ url: URL) -> String? {
        let s = url.absoluteString
        return s.isEmpty ? nil : s
    }

    static func rule(_ r: EKRecurrenceRule) -> JSON {
        var f = Fields()
        f.set("frequency", frequency(r.frequency))
        f.set("interval", r.interval)
        if let end = r.recurrenceEnd {
            f.set("endDate", end.endDate.map(Formats.utc))
            if end.occurrenceCount > 0 { f.set("occurrenceCount", end.occurrenceCount) }
        }
        return f.json
    }

    /// `#RRGGBB` in sRGB; nil for a colour that does not convert.
    static func hex(_ color: CGColor?) -> String? {
        guard let color, let srgb = CGColorSpace(name: CGColorSpace.sRGB),
              let c = color.converted(to: srgb, intent: .defaultIntent, options: nil),
              let comps = c.components, comps.count >= 3
        else { return nil }
        func byte(_ v: CGFloat) -> Int { Int((min(max(v, 0), 1) * 255).rounded()) }
        return String(format: "#%02X%02X%02X", byte(comps[0]), byte(comps[1]), byte(comps[2]))
    }

    static func calendarType(_ t: EKCalendarType) -> String {
        switch t {
        case .local: "local"
        case .calDAV: "caldav"
        case .exchange: "exchange"
        case .subscription: "subscription"
        case .birthday: "birthday"
        @unknown default: "local"
        }
    }

    static func sourceType(_ t: EKSourceType) -> String {
        switch t {
        case .local: "local"
        case .exchange: "exchange"
        case .calDAV: "caldav"
        case .mobileMe: "mobileme"
        case .subscribed: "subscribed"
        case .birthdays: "birthdays"
        @unknown default: "local"
        }
    }

    static func frequency(_ f: EKRecurrenceFrequency) -> String {
        switch f {
        case .daily: "daily"
        case .weekly: "weekly"
        case .monthly: "monthly"
        case .yearly: "yearly"
        @unknown default: "daily"
        }
    }

    static func availability(_ a: EKEventAvailability) -> String {
        switch a {
        case .notSupported: "notsupported"
        case .busy: "busy"
        case .free: "free"
        case .tentative: "tentative"
        case .unavailable: "unavailable"
        @unknown default: "notsupported"
        }
    }

    static func status(_ s: EKEventStatus) -> String {
        switch s {
        case .none: "none"
        case .confirmed: "confirmed"
        case .tentative: "tentative"
        case .canceled: "canceled"
        @unknown default: "none"
        }
    }

    static func role(_ r: EKParticipantRole) -> String {
        switch r {
        case .unknown: "unknown"
        case .required: "required"
        case .optional: "optional"
        case .chair: "chair"
        case .nonParticipant: "nonparticipant"
        @unknown default: "unknown"
        }
    }

    static func participantStatus(_ s: EKParticipantStatus) -> String {
        switch s {
        case .unknown: "unknown"
        case .pending: "pending"
        case .accepted: "accepted"
        case .declined: "declined"
        case .tentative: "tentative"
        case .delegated: "delegated"
        case .completed: "completed"
        case .inProcess: "inprocess"
        @unknown default: "unknown"
        }
    }

    static func participantType(_ t: EKParticipantType) -> String {
        switch t {
        case .unknown: "unknown"
        case .person: "person"
        case .room: "room"
        case .resource: "resource"
        case .group: "group"
        @unknown default: "unknown"
        }
    }
}
