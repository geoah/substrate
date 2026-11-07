import SubstrateCore
import SubstrateScreens

/// Chat's own settings: the assistants, then its sections as rows (Chats,
/// Notifications, Voice, Storage), beside the kit's Repository, Status and
/// About.
public struct ChatSettingsPresentation: Codable, Equatable, Sendable {
    public var assistants: AssistantsSection
    /// The owner's memories and instructions.
    public var guidance: GuidanceSection
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
    /// What it runs on: "Provider" and "Model", where the agent names them.
    public var runsOn: [LabeledValue]
    public var offered: Bool
    /// The provider without a key, by id and name.
    public var keyless: KeylessProvider?
}

public struct KeylessProvider: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var action: String
}

/// Settings → Assistant: what the owner's assistants know about them and how
/// they should work, each list a screen of its own.
public struct GuidanceSection: Codable, Equatable, Sendable {
    public var title: String
    public var footer: String
    public var memories: GuidanceList
    public var instructions: GuidanceList
    /// Said until the first read lands.
    public var loading: String?
    /// Why the last read did not land.
    public var problem: String?
}

public struct GuidanceList: Codable, Equatable, Sendable {
    public var title: String
    /// How many there are, and how many are on: "3 of 4 on".
    public var count: String
    public var footer: String
    public var empty: String
    public var rows: [GuidanceRow]
    /// The editor's title: "Edit Memory".
    public var editTitle: String
    /// What a delete asks first, and why switching off may be better.
    public var deleteQuestion: String
    public var deleteMessage: String
}

/// One memory or instruction: its words, whether the agents read it, and its
/// facts, each labelled.
public struct GuidanceRow: Codable, Equatable, Sendable, Identifiable {
    public var id: String { reference.path }
    public var reference: RecordReference
    public var text: String
    public var active: Bool
    /// "For" the agent an instruction is for, "Added by" where it came from.
    public var facts: [LabeledValue]
    /// A write is out for it: it takes no other until that one lands.
    public var busy: Bool
    public var problem: String?
}
