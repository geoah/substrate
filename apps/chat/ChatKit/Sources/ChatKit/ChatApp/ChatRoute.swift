/// Where a notification, a shortcut or a menu asks the app to go: taken by
/// one window.
public enum ChatRoute: Codable, Equatable, Sendable {
    case thread(String)
    case newChat(agent: String?, listen: Bool)
}

/// Where the app is on the screen.
public enum AppPhase: String, Codable, Equatable, Sendable {
    case active
    /// Leaving: what still streams asks to keep running now, while iOS
    /// lets it.
    case inactive
    case background
}

/// A card a turn shows, by the record behind it.
public enum CardReference: Codable, Hashable, Sendable {
    case question(String)
    case suggestion(String)

    var id: String {
        switch self {
        case let .question(id), let .suggestion(id): id
        }
    }
}

/// What the owner does to one of their memories or instructions.
public enum GuidanceChange: Codable, Equatable, Sendable {
    /// Switched on or off: the agents read only the ones on.
    case active(Bool)
    case text(String)
    case delete
}
