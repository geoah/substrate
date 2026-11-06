import SubstrateScreens

/// The app's top level (§5.6's App menu): what needs the owner, the lists,
/// and Settings, by id.
public enum TasksMenu {
    public static let needsYou = "needs"
    public static let lists = "lists"
    public static let settings = "settings"

    public static func present(selected: String, needsYou count: Int) -> AppMenuPresentation {
        AppMenuPresentation(
            sections: [
                AppMenuSection(id: needsYou, title: "Needs you", symbol: "needs", badge: count > 0 ? count : nil),
                AppMenuSection(id: lists, title: "Lists", symbol: "lists"),
                AppMenuSection(id: settings, title: "Settings", symbol: "settings"),
            ], selected: selected)
    }
}
