/// The system the app runs on, for what differs by it: a phone stops the
/// watch while no window is in front and says what it can notify from the
/// background; a Mac runs while the app does.
public enum ChatPlatform: String, Codable, Equatable, Sendable {
    case ios
    case macos
}
