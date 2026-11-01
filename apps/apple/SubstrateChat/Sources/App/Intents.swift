import AppIntents
import SubstrateKit

/// An assistant a chat can go to: the agents offered in Settings → Agents.
struct AgentEntity: AppEntity {
    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Assistant"
    static let defaultQuery = AgentQuery()

    let id: String
    let name: String
    let summary: String?

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(name)", subtitle: summary.map { "\($0)" })
    }
}

struct AgentQuery: EntityQuery {
    @MainActor
    private func offered() -> [AgentEntity] {
        (AppModel.shared.store?.offered ?? []).map {
            AgentEntity(id: $0.id, name: $0.name, summary: $0.description)
        }
    }

    func entities(for identifiers: [String]) async throws -> [AgentEntity] {
        let all = await offered()
        return identifiers.compactMap { id in
            all.first { $0.id == id } ?? AgentEntity(id: id, name: agentName(id), summary: nil)
        }
    }

    func suggestedEntities() async throws -> [AgentEntity] { await offered() }
}

/// Opens a new chat, with an assistant when one is named (else the default),
/// and with the microphone already on when asked. What the iPhone's Action
/// Button runs.
struct NewChatIntent: AppIntent {
    static let title: LocalizedStringResource = "New Chat"
    static let description = IntentDescription(
        "Opens Substrate Chat on a new conversation, with the assistant you choose, and listening if you like.")
    static let supportedModes: IntentModes = .foreground

    @Parameter(title: "Assistant", description: "Who the chat is with. Empty is your default assistant.")
    var assistant: AgentEntity?

    @Parameter(title: "Start Listening", description: "Turn the microphone on as the chat opens.", default: false)
    var listen: Bool

    static var parameterSummary: some ParameterSummary {
        Summary("New chat with \(\.$assistant)") { \.$listen }
    }

    init() {}

    init(listen: Bool) {
        self.listen = listen
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        AppModel.shared.route(.newChat(agent: assistant?.id, listen: listen))
        return .result()
    }
}

/// The same, listening from the start: one press of the Action Button and
/// speak.
struct TalkIntent: AppIntent {
    static let title: LocalizedStringResource = "Talk to an Assistant"
    static let description = IntentDescription(
        "Opens a new Substrate Chat conversation with the microphone on.")
    static let supportedModes: IntentModes = .foreground

    @Parameter(title: "Assistant", description: "Who the chat is with. Empty is your default assistant.")
    var assistant: AgentEntity?

    static var parameterSummary: some ParameterSummary {
        Summary("Talk to \(\.$assistant)")
    }

    @MainActor
    func perform() async throws -> some IntentResult {
        AppModel.shared.route(.newChat(agent: assistant?.id, listen: true))
        return .result()
    }
}

struct ChatShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: NewChatIntent(),
            phrases: ["New chat in \(.applicationName)", "Start a \(.applicationName) chat",
                      "New chat with \(\.$assistant) in \(.applicationName)"],
            shortTitle: "New Chat",
            systemImageName: "square.and.pencil")
        AppShortcut(
            intent: TalkIntent(),
            phrases: ["Talk to \(.applicationName)", "Ask \(.applicationName)",
                      "Ask \(\.$assistant) in \(.applicationName)"],
            shortTitle: "Talk",
            systemImageName: "waveform")
    }

    static let shortcutTileColor: ShortcutTileColor = .purple
}
