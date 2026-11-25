/// What the microphone does when it is tapped, or held.
public enum MicAction: String, Codable, Sendable, CaseIterable {
    /// The words go into the message, to be read, changed and sent.
    case dictate
    /// The message is sent when the listening ends: a tap on Send, or
    /// letting go of the held microphone.
    case talk
    /// Nothing: a hold is only a long tap.
    case off
}

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
    /// A long line of code wraps; off, it scrolls sideways.
    public var wrapCode = false
    public var notifyReplies = true
    public var notifyAttention = true
    /// A locale identifier, or nil for the device's.
    public var voiceLocale: String?
    /// What a tap on the microphone does, and what holding it does.
    public var micTap: MicAction = .dictate
    public var micHold: MicAction = .talk
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
