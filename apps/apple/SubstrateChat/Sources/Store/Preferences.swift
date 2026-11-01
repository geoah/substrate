import Foundation
import Observation
import SubstrateKit

/// The owner's choices for one signed-in repository. Kept in UserDefaults
/// under the identity's hash, so two repositories never share them.
@MainActor
@Observable
final class Preferences {
    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private let prefix: String

    /// The agents the owner turned off, and those they turned on that are
    /// not offered by default. Kept as both, never as one "shown" set, so an
    /// agent added later follows the default until the owner says otherwise.
    var hiddenAgents: Set<String> { didSet { save("hiddenAgents", Array(hiddenAgents)) } }
    var addedAgents: Set<String> { didSet { save("addedAgents", Array(addedAgents)) } }
    var defaultAgent: String? { didSet { save("defaultAgent", defaultAgent) } }
    var showAutomated: Bool { didSet { save("showAutomated", showAutomated) } }
    var expandThinking: Bool { didSet { save("expandThinking", expandThinking) } }
    var technicalDetails: Bool { didSet { save("technicalDetails", technicalDetails) } }
    var notifyReplies: Bool { didSet { save("notifyReplies", notifyReplies) } }
    var notifyAttention: Bool { didSet { save("notifyAttention", notifyAttention) } }
    /// A locale identifier, or nil for the device's.
    var voiceLocale: String? { didSet { save("voiceLocale", voiceLocale) } }
    /// Whether the notification permission was asked after a first send.
    var askedNotifications: Bool { didSet { save("askedNotifications", askedNotifications) } }
    /// The scopes the last approval asked for, of an app session; nil where
    /// a build before this one signed in (`ChatManifest.earliestRequest`).
    var requestedScopes: [String]? { didSet { save("requestedScopes", requestedScopes) } }

    init(identity: String, defaults: UserDefaults = .standard) {
        self.defaults = defaults
        prefix = "chat.\(DiskCache.hash(identity))."
        let p = prefix
        hiddenAgents = Set(defaults.stringArray(forKey: p + "hiddenAgents") ?? [])
        addedAgents = Set(defaults.stringArray(forKey: p + "addedAgents") ?? [])
        defaultAgent = defaults.string(forKey: p + "defaultAgent")
        showAutomated = defaults.object(forKey: p + "showAutomated") as? Bool ?? false
        expandThinking = defaults.object(forKey: p + "expandThinking") as? Bool ?? false
        technicalDetails = defaults.object(forKey: p + "technicalDetails") as? Bool ?? false
        notifyReplies = defaults.object(forKey: p + "notifyReplies") as? Bool ?? true
        notifyAttention = defaults.object(forKey: p + "notifyAttention") as? Bool ?? true
        voiceLocale = defaults.string(forKey: p + "voiceLocale")
        askedNotifications = defaults.object(forKey: p + "askedNotifications") as? Bool ?? false
        requestedScopes = defaults.stringArray(forKey: p + "requestedScopes")
    }

    private func save(_ key: String, _ value: Any?) {
        if let value { defaults.set(value, forKey: prefix + key) } else { defaults.removeObject(forKey: prefix + key) }
    }

    /// Whether an agent is offered for a new chat.
    func offers(_ agent: Agent, defaults offered: Set<String>) -> Bool {
        guard agent.chatCapable else { return false }
        if hiddenAgents.contains(agent.id) { return false }
        return offered.contains(agent.id) || addedAgents.contains(agent.id)
    }

    func setOffered(_ agent: Agent, _ on: Bool, defaults offered: Set<String>) {
        if on {
            hiddenAgents.remove(agent.id)
            if !offered.contains(agent.id) { addedAgents.insert(agent.id) }
        } else {
            addedAgents.remove(agent.id)
            if offered.contains(agent.id) { hiddenAgents.insert(agent.id) }
            if defaultAgent == agent.id { defaultAgent = nil }
        }
    }

    /// Forgets every choice: the repository was signed out of.
    func clear() {
        for key in defaults.dictionaryRepresentation().keys where key.hasPrefix(prefix) {
            defaults.removeObject(forKey: key)
        }
    }
}
