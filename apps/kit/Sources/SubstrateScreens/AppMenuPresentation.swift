import SubstrateCore

/// The app's menu (§5.6): its destinations as data, in sections, with the
/// one shown now. Each system draws it as its own: a tab bar on a phone, a
/// sidebar on iPad and the Mac, a navigation bar, rail or drawer on Android
/// by the window's width. A tab bar shows every destination in order and
/// drops the section titles; a sidebar shows both.
public struct AppMenuPresentation: Bridged, Hashable, Sendable {
    /// The app's name, over a sidebar.
    public var title: String
    public var sections: [AppMenuSection]
    /// The destination shown now, by id.
    public var selected: String

    public init(title: String, sections: [AppMenuSection], selected: String) {
        self.title = title
        self.sections = sections
        self.selected = selected
    }

    /// Every destination, in the order a tab bar shows them.
    public var items: [AppMenuItem] { sections.flatMap(\.items) }
}

/// A run of destinations under a title a sidebar shows; nil for the first.
public struct AppMenuSection: Bridged, Hashable, Sendable, Identifiable {
    public var id: String
    public var title: String?
    public var items: [AppMenuItem]

    public init(id: String, title: String? = nil, items: [AppMenuItem]) {
        self.id = id
        self.title = title
        self.items = items
    }
}

/// One destination: its title, its mark, and a count or a word beside it
/// where there is one ("3", "New").
public struct AppMenuItem: Bridged, Hashable, Sendable, Identifiable {
    public var id: String
    public var title: String
    public var symbol: AppMenuSymbol
    public var badge: String?

    public init(id: String, title: String, symbol: AppMenuSymbol, badge: String? = nil) {
        self.id = id
        self.title = title
        self.symbol = symbol
        self.badge = badge
    }
}

/// A destination's mark, by what it holds; each system draws it as its own
/// symbol (an SF Symbol, a Material Symbol), from the design tokens.
public enum AppMenuSymbol: String, Bridged, Hashable, Sendable, CaseIterable {
    case today
    case upcoming
    case list
    case inbox
    case chat
    case search
    case records
    case activity
    case history
    case person
    case settings
}

/// What the menu sends: the destination chosen.
public enum AppMenuCommand: Bridged, Hashable, Sendable {
    case select(item: String)
}
