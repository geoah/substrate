/// The owner's choices for one signed-in repository.
public struct ChatPreferences: Codable, Equatable, Sendable {
    /// The agents the owner turned off, and those they turned on that are
    /// not offered by default. Kept as both, never as one "shown" set, so an
    /// agent added later follows the default until the owner says otherwise.
    public var hiddenAgents: Set<String> = []
    public var addedAgents: Set<String> = []
    public var defaultAgent: String?
    public var showAutomated = false
    public var expandThinking = false
    public var technicalDetails = false
    public var notifyReplies = true
    public var notifyAttention = true
    /// A locale identifier, or nil for the device's.
    public var voiceLocale: String?
    /// Whether the notification permission was asked after a first send.
    public var askedNotifications = false

    public init() {}

    /// Whether an agent is offered for a new chat.
    func offers(_ agent: Agent, defaults offered: Set<String>) -> Bool {
        guard agent.chatCapable else { return false }
        if hiddenAgents.contains(agent.id) { return false }
        return offered.contains(agent.id) || addedAgents.contains(agent.id)
    }

    mutating func setOffered(_ agent: String, _ on: Bool, defaults offered: Set<String>) {
        if on {
            hiddenAgents.remove(agent)
            if !offered.contains(agent) { addedAgents.insert(agent) }
        } else {
            addedAgents.remove(agent)
            if offered.contains(agent) { hiddenAgents.insert(agent) }
            if defaultAgent == agent { defaultAgent = nil }
        }
    }
}
