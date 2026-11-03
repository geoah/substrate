import SubstrateScreens

/// Chat's own settings: the assistants, then its sections as rows (Chats,
/// Notifications, Voice, Storage), beside the kit's Repository, Status and
/// About.
public struct ChatSettingsPresentation: Codable, Equatable, Sendable {
    public var assistants: AssistantsSection
    public var sections: [SettingsSection]
    /// Notifications are off for the app: the system's settings turn them on.
    public var notificationsDenied: Bool
    public var voiceLocale: String?
}

public struct AssistantsSection: Codable, Equatable, Sendable {
    public var title: String
    public var footer: String
    public var defaultLabel: String
    /// The default for new chats, by id; empty for the last one chatted with.
    public var defaultChoice: String
    public var defaultOptions: [SettingsOption]
    public var rows: [AssistantRow]
    /// "No agent can chat yet.", "Loading…"
    public var empty: String?
    public var background: [AgentChoice]
    public var backgroundTitle: String
    public var backgroundFooter: String
}

public struct AssistantRow: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var description: String?
    /// Provider · model.
    public var runsOn: String
    public var offered: Bool
    /// The provider without a key, by id and name.
    public var keyless: KeylessProvider?
}

public struct KeylessProvider: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var action: String
}
