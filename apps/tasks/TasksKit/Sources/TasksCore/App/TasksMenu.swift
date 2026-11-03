import SubstrateScreens

/// The app's top level (§5.6's App menu): what is due, the lists, and
/// Settings, by id.
public enum TasksMenu {
    public static let today = "today"
    public static let lists = "lists"
    public static let settings = "settings"

    public static func present(selected: String, dueToday: Int) -> AppMenuPresentation {
        AppMenuPresentation(sections: [
            AppMenuSection(id: today, title: "Today", symbol: "today", badge: dueToday > 0 ? dueToday : nil),
            AppMenuSection(id: lists, title: "Lists", symbol: "lists"),
            AppMenuSection(id: settings, title: "Settings", symbol: "settings"),
        ], selected: selected)
    }
}
