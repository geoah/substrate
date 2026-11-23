import Foundation

// The day as a home-screen widget draws it. The app makes these values from
// the pane (TodayCore's `GlancePresenter`) and writes them where the widget
// extension reads them; the extension links this library and nothing else of
// the app, so it reads no record and reaches no server. Every word a widget
// shows is in these values.

/// What a line of the day is: the app's kinds, each drawn as its glyph.
public enum GlanceGlyph: String, Codable, Hashable, Sendable, CaseIterable {
    case task, meeting, routine, night, moss, urgent, plan
}

/// A flag, drawn as a stripe in its colour and never as a word. The word is
/// what a screen reader is told.
public enum GlanceFlag: String, Codable, Hashable, Sendable, CaseIterable {
    case high, medium, low

    public var word: String {
        switch self {
        case .high: "High"
        case .medium: "Medium"
        case .low: "Low"
        }
    }
}

/// One thing of the day on one line: a task, a routine, a meeting, the
/// night, the plan. A routine is its group and a count, never the names or
/// the doses of what is in it.
public struct GlanceItem: Codable, Equatable, Sendable, Identifiable {
    /// The item's id in the pane: "row:task:…", "meeting:…", "night".
    public var id: String
    public var glyph: GlanceGlyph
    public var title: String
    /// One line under the title where a widget has room: why it matters
    /// now, or what it holds.
    public var line: String?
    /// A meeting's start: "14:20".
    public var time: String?
    public var flag: GlanceFlag?
    /// The flag is the assistant's suggestion: its stripe is drawn hollow.
    public var suggested: Bool
    /// The project's name, said quietly where there is room.
    public var project: String?
    /// The instant the count of minutes runs to ("in 12 min"): set on a
    /// meeting that leads the day and starts within the hour.
    public var count: Date?
    /// What a tap opens (`GlanceLinks`).
    public var link: String

    public init(
        id: String, glyph: GlanceGlyph, title: String, line: String? = nil, time: String? = nil, flag: GlanceFlag? = nil,
        suggested: Bool = false, project: String? = nil, count: Date? = nil, link: String = GlanceLinks.day
    ) {
        self.id = id
        self.glyph = glyph
        self.title = title
        self.line = line
        self.time = time
        self.flag = flag
        self.suggested = suggested
        self.project = project
        self.count = count
        self.link = link
    }
}

/// A meeting of the rest of today, with its real instants.
public struct GlanceMeeting: Codable, Equatable, Sendable, Identifiable {
    /// A meeting's place in a pair that shares time and waits for a choice:
    /// one band is drawn behind both.
    public enum Pair: String, Codable, Hashable, Sendable {
        case first, second
    }

    public var id: String
    /// "14:20".
    public var time: String
    public var title: String
    public var start: Date
    public var end: Date
    /// What ends its line: "now, until 14:40" on the one in progress,
    /// "Choose one" on the first of a pair.
    public var note: String?
    /// On now.
    public var live: Bool
    /// The owner does not go to it: drawn struck through.
    public var off: Bool
    public var pair: Pair?
    /// The minutes left to `start` are drawn ("in 12 min"): it is the next
    /// fixed point and starts within the hour.
    public var counts: Bool
    public var link: String

    public init(
        id: String, time: String, title: String, start: Date, end: Date, note: String? = nil, live: Bool = false, off: Bool = false,
        pair: Pair? = nil, counts: Bool = false, link: String = GlanceLinks.day
    ) {
        self.id = id
        self.time = time
        self.title = title
        self.start = start
        self.end = end
        self.note = note
        self.live = live
        self.off = off
        self.pair = pair
        self.counts = counts
        self.link = link
    }
}

/// Today's open tasks, in the order of the day.
public struct GlanceTasks: Codable, Equatable, Sendable {
    /// "Today".
    public var title: String
    /// "2 of 6 done"; nil on a day with no task.
    public var standing: String?
    public var items: [GlanceItem]
    /// Said in place of the items when none is open.
    public var empty: String

    public init(title: String = "Today", standing: String? = nil, items: [GlanceItem] = [], empty: String = "Nothing is left today.") {
        self.title = title
        self.standing = standing
        self.items = items
        self.empty = empty
    }

    /// The last quiet line where a widget draws `shown` of them: "and 3 more".
    public func more(after shown: Int) -> String? { GlanceWords.more(items.count - shown) }
}

/// The meetings of the rest of today, in order of their start.
public struct GlanceMeetings: Codable, Equatable, Sendable {
    /// "Meetings".
    public var title: String
    /// "5 left": the ones the owner goes to. Nil when none is left.
    public var standing: String?
    public var items: [GlanceMeeting]
    /// Said in place of the items when none is left.
    public var empty: String

    public init(title: String = "Meetings", standing: String? = nil, items: [GlanceMeeting] = [], empty: String = "No more meetings today.") {
        self.title = title
        self.standing = standing
        self.items = items
        self.empty = empty
    }

    public func more(after shown: Int) -> String? { GlanceWords.more(items.count - shown) }
}

/// The day at one instant, as the three widgets draw it. It holds from `at`
/// until the next day of its timeline does.
public struct GlanceDay: Codable, Equatable, Sendable {
    public var at: Date
    /// "Thursday 19 November", and "Thu 19 Nov".
    public var date: String
    public var dateShort: String
    /// What leads the day now: the pane's current item.
    public var now: GlanceItem?
    /// Said in place of `now` when nothing leads: "Nothing is left today."
    public var quiet: String?
    /// The next fixed point: the next meeting the owner goes to that has
    /// not started and does not lead.
    public var next: GlanceMeeting?
    /// "3 tasks and 2 meetings left".
    public var left: String?
    /// What follows in the order of the day, without what leads and
    /// without the next fixed point.
    public var upcoming: [GlanceItem]
    public var tasks: GlanceTasks
    public var meetings: GlanceMeetings
    /// Said in place of the day, by every widget: nothing to draw a day
    /// from, signed out, or a day that is no longer known.
    public var message: String?

    public init(
        at: Date, date: String = "", dateShort: String = "", now: GlanceItem? = nil, quiet: String? = nil, next: GlanceMeeting? = nil,
        left: String? = nil, upcoming: [GlanceItem] = [], tasks: GlanceTasks = GlanceTasks(), meetings: GlanceMeetings = GlanceMeetings(),
        message: String? = nil
    ) {
        self.at = at
        self.date = date
        self.dateShort = dateShort
        self.now = now
        self.quiet = quiet
        self.next = next
        self.left = left
        self.upcoming = upcoming
        self.tasks = tasks
        self.meetings = meetings
        self.message = message
    }

    /// What a widget draws with no day to read: signed out, or the app has
    /// not been opened yet.
    public static func closed(at: Date) -> GlanceDay { GlanceDay(at: at, message: GlanceWords.closed) }

    /// What a widget draws once the instants its timeline was made at have
    /// passed, and the app has not written another.
    public static func outOfDate(at: Date) -> GlanceDay { GlanceDay(at: at, message: GlanceWords.outOfDate) }

    /// Whether a widget draws the same from this day as from `other`,
    /// whenever each starts to hold.
    public func says(sameAs other: GlanceDay) -> Bool {
        var mine = self
        mine.at = other.at
        return mine == other
    }
}

/// The fixed words of the widgets.
public enum GlanceWords {
    public static let closed = "Open Substrate Today to see your day."
    public static let outOfDate = "Open Substrate Today to bring your day up to date."
    /// Before the minutes left to a meeting's start: "in 12 min".
    public static let until = "in"
    /// Said in place of the minutes once none is left.
    public static let now = "now"

    /// "and 3 more", for what a widget has no room to list.
    public static func more(_ count: Int) -> String? { count > 0 ? "and \(count) more" : nil }

    /// The count to a meeting's start as it stands at one instant, in the
    /// app's own words: whole minutes, a started one counted, "in 12 min",
    /// and "now" once it is there. A picture of a widget draws these, since
    /// no clock moves a picture. On the home screen the system counts the
    /// minutes itself, and a test holds what it says to these.
    public static func count(from: Date, to: Date) -> String {
        let minutes = Int((to.timeIntervalSince(from) / 60).rounded(.up))
        return minutes > 0 ? "\(until) \(minutes) min" : now
    }
}

/// The three widgets: the kind WidgetKit knows each by, and its name and
/// description in the widget gallery.
public enum GlanceKind: String, Codable, Hashable, Sendable, CaseIterable {
    case now = "today.now"
    case tasks = "today.tasks"
    case meetings = "today.meetings"

    public var name: String {
        switch self {
        case .now: "Now and next"
        case .tasks: "Tasks"
        case .meetings: "Meetings"
        }
    }

    public var summary: String {
        switch self {
        case .now: "What leads your day now, your next meeting, and what follows."
        case .tasks: "Today's open tasks, in the order of the day."
        case .meetings: "The meetings of the rest of today."
        }
    }
}

/// What a tap on a widget opens, as the app's own links. The app reads them
/// back as routes (TodayCore's `TodayLinks`); one it does not know opens
/// the day.
public enum GlanceLinks {
    public static let scheme = "substrate-today"
    /// The day itself: what a tap anywhere on a widget opens.
    public static let day = "\(scheme)://day"
}
