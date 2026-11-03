/// An app's top level as data (§5.6): its sections, drawn as a tab bar on a
/// phone and as a sidebar on a tablet, a computer and a wide Android
/// screen, and which one is open.
public struct AppMenuPresentation: Codable, Hashable, Sendable {
    public var sections: [AppMenuSection]
    public var selected: String

    public init(sections: [AppMenuSection], selected: String) {
        self.sections = sections
        self.selected = selected
    }
}

public struct AppMenuSection: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var title: String
    /// The app's name for the section's mark, which each system's renderer
    /// draws as its own symbol.
    public var symbol: String
    /// A count beside the title (what is due), nil for none.
    public var badge: Int?

    public init(id: String, title: String, symbol: String, badge: Int? = nil) {
        self.id = id
        self.title = title
        self.symbol = symbol
        self.badge = badge
    }
}
