import Foundation

/// The widgets' day at each instant it changes, made ahead by the app so a
/// widget stays right as meetings start and end while the app is not
/// running. The app writes it as one file in the App Group's folder, and the
/// widget extension reads it there.
public struct GlanceTimeline: Codable, Equatable, Sendable {
    /// The App Group the app and its widget extension share.
    public static let group = "group.dev.reamde.substrate.today"
    /// The file's name in the group's folder.
    public static let fileName = "glance.json"

    public var writtenAt: Date
    /// In order of `at`: each holds from its own until the next one's.
    public var entries: [GlanceDay]
    /// The instant past which the day is not known: the timeline was made
    /// up to it. Nil for one that never ends (the day signed out).
    public var until: Date?

    public init(writtenAt: Date, entries: [GlanceDay], until: Date? = nil) {
        self.writtenAt = writtenAt
        self.entries = entries
        self.until = until
    }

    /// What is written when the owner signs out, or clears what the device
    /// kept: one day that says to open the app.
    public static func closed(at: Date) -> GlanceTimeline { GlanceTimeline(writtenAt: at, entries: [.closed(at: at)]) }

    /// The days a widget draws from `now` on: the one that holds then, each
    /// one after it, and one that says the day is out of date at the
    /// instant the timeline ends. A timeline with nothing to draw is the
    /// one day that says to open the app.
    public func days(from now: Date) -> [GlanceDay] {
        if let until, until <= now { return [.outOfDate(at: now)] }
        var out: [GlanceDay] = []
        if let held = entries.last(where: { $0.at <= now }) { out.append(held) }
        out += entries.filter { $0.at > now }
        guard !out.isEmpty else { return [.closed(at: now)] }
        if let until { out.append(.outOfDate(at: until)) }
        return out
    }

    /// The day that holds at an instant.
    public func day(at instant: Date) -> GlanceDay { days(from: instant)[0] }

    /// Whether a widget draws the same from `now` on from this timeline as
    /// from `other`: when each was written, and what held before `now`, are
    /// left out.
    public func says(sameAs other: GlanceTimeline, from now: Date) -> Bool {
        let (mine, theirs) = (days(from: now), other.days(from: now))
        guard mine.count == theirs.count, mine[0].says(sameAs: theirs[0]) else { return false }
        return mine.dropFirst().elementsEqual(theirs.dropFirst())
    }

    // MARK: The file

    /// The timeline as the file holds it: JSON with its keys in order, so
    /// the same timeline is the same bytes.
    public func encoded() throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        encoder.dateEncodingStrategy = .secondsSince1970
        return try encoder.encode(self)
    }

    public init(data: Data) throws {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .secondsSince1970
        self = try decoder.decode(GlanceTimeline.self, from: data)
    }
}

public extension GlanceDay {
    /// A small invented day, for a widget's placeholder and its picture in
    /// the widget gallery. Nothing in it is a record of anyone.
    static func sample(at: Date) -> GlanceDay {
        let review = GlanceMeeting(
            id: "review", time: "14:00", title: "Design review", start: at.addingTimeInterval(2 * 3600), end: at.addingTimeInterval(3 * 3600))
        let planning = GlanceMeeting(
            id: "planning", time: "16:30", title: "Weekly planning", start: at.addingTimeInterval(4.5 * 3600), end: at.addingTimeInterval(5 * 3600))
        let outline = GlanceItem(
            id: "row:task:outline", glyph: .task, title: "Draft the workshop outline", line: "Due today.", flag: .high, project: "Workshop")
        let venue = GlanceItem(id: "row:task:venue", glyph: .task, title: "Reply to the venue", flag: .medium, project: "Workshop")
        let train = GlanceItem(id: "row:task:train", glyph: .task, title: "Book the train", flag: .low, project: "Travel")
        let walk = GlanceItem(id: "row:routine:walk", glyph: .routine, title: "Evening walk", line: "At 18:30.")
        return GlanceDay(
            at: at, date: "Thursday 19 November", dateShort: "Thu 19 Nov", now: outline, next: review, left: "3 tasks and 2 meetings left",
            upcoming: [venue, train, GlanceItem(id: "meeting:planning", glyph: .meeting, title: planning.title, time: planning.time), walk],
            tasks: GlanceTasks(standing: "1 of 4 done", items: [outline, venue, train]),
            meetings: GlanceMeetings(standing: "2 left", items: [review, planning]))
    }
}
