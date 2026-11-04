import CryptoKit
import Foundation

/// The record ids the device computes, so a re-push upserts rather than
/// duplicates. The algorithm is `ExternalID` in internal/runner/ids.go, the
/// one `host.ids.external` runs; the apple README's vectors pin this port.
enum ExternalID {
    static func make(_ provider: String, _ account: String, _ externalID: String) -> String {
        let slug = String(slugify(provider).prefix(48))
        let digest = hash([provider, account, externalID])
        return slug.isEmpty ? digest : slug + "-" + digest
    }

    /// Each part as `<byte length>:<bytes>|`, so ("ab", "c") and ("a", "bc")
    /// never collide; the first 32 hex characters of the SHA-256.
    private static func hash(_ parts: [String]) -> String {
        var data = Data()
        for part in parts {
            let bytes = Data(part.utf8)
            data.append(Data("\(bytes.count):".utf8))
            data.append(bytes)
            data.append(Data("|".utf8))
        }
        let digest = SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
        return String(digest.prefix(32))
    }

    /// ASCII [a-z0-9] kept, every other run one dash, no dash at either end.
    private static func slugify(_ s: String) -> String {
        var out = ""
        var dash = false
        for scalar in s.unicodeScalars {
            var c = scalar
            if c.value >= 65, c.value <= 90 { c = Unicode.Scalar(c.value + 32)! }
            if (c.value >= 97 && c.value <= 122) || (c.value >= 48 && c.value <= 57) {
                out.unicodeScalars.append(c)
                dash = false
            } else if !dash, !out.isEmpty {
                out += "-"
                dash = true
            }
        }
        while out.hasSuffix("-") { out.removeLast() }
        return out
    }
}

/// The ids of one install's rows under providers.substrate.reamde.dev/apple.
struct AppleIDs: Sendable {
    let install: String

    var addressbook: String { ExternalID.make("apple", "addressbook", install) }
    var eventstore: String { ExternalID.make("apple", "eventstore", install) }

    func contact(_ identifier: String) -> String {
        ExternalID.make("apple", addressbook, identifier)
    }

    func calendar(_ calendarIdentifier: String) -> String {
        ExternalID.make("apple", eventstore, calendarIdentifier)
    }

    func event(key: String) -> String {
        ExternalID.make("apple", eventstore, key)
    }

    /// A reminder is its item identifier in the event store, as a single
    /// event is: EventKit keeps reminders and events in one store.
    func reminder(_ calendarItemIdentifier: String) -> String {
        ExternalID.make("apple", eventstore, calendarItemIdentifier)
    }

    var visitstore: String { ExternalID.make("apple", "visitstore", install) }

    /// A visit is its arrival, or its departure when the arrival is unknown
    /// (Visit.key): the two reports of one visit land on one row.
    func visit(_ key: String) -> String {
        ExternalID.make("apple", visitstore, key)
    }

    /// A single event is its item identifier; an occurrence of a series is
    /// that and its original start in UTC, because every occurrence shares
    /// the item identifier.
    static func eventKey(item: String, occurrence: Date?, repeats: Bool) -> String {
        guard repeats, let occurrence else { return item }
        return item + "@" + Formats.utc(occurrence)
    }
}

enum Formats {
    /// `2026-10-27T09:00:00Z`: a datetime property, and an occurrence key.
    static func utc(_ date: Date) -> String {
        utcFormatter.string(from: date)
    }

    private static let utcFormatter: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        f.timeZone = TimeZone(identifier: "UTC")
        return f
    }()

    /// Health Auto Export's `yyyy-MM-dd HH:mm:ss Z` in the phone's zone:
    /// `2026-10-07 08:05:37 +0300`.
    static func health(_ date: Date, in zone: TimeZone) -> String {
        formatter("yyyy-MM-dd HH:mm:ss Z", zone).string(from: date)
    }

    /// A local day's name, `2026-10-07`, which is what the app remembers it
    /// sent.
    static func day(_ date: Date, in zone: TimeZone) -> String {
        formatter("yyyy-MM-dd", zone).string(from: date)
    }

    /// One formatter per pattern and zone: a day's minutes delivery formats
    /// thousands of dates, and a formatter is costly to make. Formatting
    /// from several threads is safe; the cache itself is locked.
    private static func formatter(_ pattern: String, _ zone: TimeZone) -> DateFormatter {
        let key = pattern + "|" + zone.identifier
        cacheLock.lock()
        defer { cacheLock.unlock() }
        if let f = cache[key] { return f }
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.calendar = Calendar(identifier: .gregorian)
        f.timeZone = zone
        f.dateFormat = pattern
        cache[key] = f
        return f
    }

    private nonisolated(unsafe) static var cache: [String: DateFormatter] = [:]
    private static let cacheLock = NSLock()
}
