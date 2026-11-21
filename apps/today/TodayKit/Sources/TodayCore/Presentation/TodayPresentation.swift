import Foundation
import SubstrateRuntime
import SubstrateScreens

/// Everything the app's root draws, as one value: the pane, every page a
/// route can name, what stands in their way, and the app's part of
/// Settings. A UI reads this and sends `TodayCommand`s; it decides nothing.
public struct TodayPresentation: Codable, Equatable, Sendable {
    public var pane: PanePresentation
    /// The pages, answered by route (`page(_:)`).
    public var pages: PageSource
    public var notices: [NoticeLine]
    public var problem: ProblemPresentation?
    /// The app's own settings sections, before the kit's: how the assistant
    /// behaves, and reminders.
    public var settings: [SettingsSection]
    /// Each area of the repository's folder, with Clear Saved Copies.
    public var storage: SettingsSection
    public var about: AboutPresentation
    /// The reminders the device should hold, as the root planned them: a
    /// view never reads this; the root's effect reconciles it.
    public var reminders: [ReminderPlan]
    /// The instant the pane stops being true as drawn: the next minute,
    /// or the moment an urgent mark nobody checked starts to interrupt.
    public var nextChange: Date?

    public init(
        pane: PanePresentation, pages: PageSource = PageSource(), notices: [NoticeLine] = [], problem: ProblemPresentation? = nil,
        settings: [SettingsSection] = [], storage: SettingsSection, about: AboutPresentation, reminders: [ReminderPlan] = [],
        nextChange: Date? = nil
    ) {
        self.nextChange = nextChange
        self.pane = pane
        self.pages = pages
        self.notices = notices
        self.problem = problem
        self.settings = settings
        self.storage = storage
        self.about = about
        self.reminders = reminders
    }

    /// The page a route names, as it stands now. Cheap and pure: a view
    /// calls it for the route on top of its path each time it draws.
    public func page(_ route: TodayRoute) -> PagePresentation { pages.page(route) }
}

/// Where a page comes from: a table of pages made ahead (fixtures, a
/// gallery), else whatever makes one from the day (`PageMaking`, the
/// root's engine). Two sources are equal when their tables are and they
/// were made from the same day (`stamp`), so a view is drawn again when a
/// page could differ.
public struct PageSource: Equatable, Sendable {
    public var fixed: [TodayRoute: PagePresentation]
    public var maker: (any PageMaking)?
    /// Moves whenever the day the maker reads from changed.
    public var stamp: Int

    public init(fixed: [TodayRoute: PagePresentation] = [:], maker: (any PageMaking)? = nil, stamp: Int = 0) {
        self.fixed = fixed
        self.maker = maker
        self.stamp = stamp
    }

    public func page(_ route: TodayRoute) -> PagePresentation {
        if let page = fixed[route] { return page }
        return maker?.page(route) ?? .missing(MissingPage())
    }

    public static func == (lhs: PageSource, rhs: PageSource) -> Bool { lhs.stamp == rhs.stamp && lhs.fixed == rhs.fixed }
}

/// A source's table encodes; its maker does not cross a bridge.
extension PageSource: Codable {
    public init(from decoder: any Decoder) throws {
        let container = try decoder.singleValueContainer()
        let pairs = try container.decode([Pair].self)
        self.init(fixed: Dictionary(pairs.map { ($0.route, $0.page) }, uniquingKeysWith: { first, _ in first }))
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(fixed.map { Pair(route: $0.key, page: $0.value) }.sorted { "\($0.route)" < "\($1.route)" })
    }

    private struct Pair: Codable {
        var route: TodayRoute
        var page: PagePresentation
    }
}

/// What makes a page from the day on demand.
public protocol PageMaking: Sendable {
    func page(_ route: TodayRoute) -> PagePresentation
}

/// One reminder the device should hold: a routine row's slot. Its words
/// name no medicine, person or clinic.
public struct ReminderPlan: Codable, Equatable, Hashable, Sendable, Identifiable {
    /// `today.routine.<row id>`.
    public var id: String
    /// "Morning medication".
    public var title: String
    /// "5 items".
    public var body: String
    public var fireAt: Date
    /// The row it is about, which its actions log.
    public var row: String

    public init(id: String, title: String, body: String, fireAt: Date, row: String) {
        self.id = id
        self.title = title
        self.body = body
        self.fireAt = fireAt
        self.row = row
    }
}
