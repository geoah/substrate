import Foundation

/// Where the owner's choices are kept, per repository.
public protocol PreferenceStore: Sendable {
    func load(_ identity: String) -> ChatPreferences
    func save(_ preferences: ChatPreferences, _ identity: String)
    /// Forgets every choice: the repository was signed out of.
    func clear(_ identity: String)
}

/// The choices in UserDefaults, under `chat.<identity's hash>.<key>`, the
/// keys every build has kept them under, so two repositories never share
/// them.
public final class DefaultsPreferenceStore: PreferenceStore, @unchecked Sendable {
    private let defaults: UserDefaults

    public init(defaults: UserDefaults) {
        self.defaults = defaults
    }

    private func prefix(_ identity: String) -> String { "chat.\(ChatStorage.hash(identity))." }

    public func load(_ identity: String) -> ChatPreferences {
        let p = prefix(identity)
        var out = ChatPreferences()
        out.hiddenAgents = Set(defaults.stringArray(forKey: p + "hiddenAgents") ?? [])
        out.addedAgents = Set(defaults.stringArray(forKey: p + "addedAgents") ?? [])
        out.defaultAgent = defaults.string(forKey: p + "defaultAgent")
        out.showAutomated = defaults.object(forKey: p + "showAutomated") as? Bool ?? false
        out.expandThinking = defaults.object(forKey: p + "expandThinking") as? Bool ?? false
        out.technicalDetails = defaults.object(forKey: p + "technicalDetails") as? Bool ?? false
        out.notifyReplies = defaults.object(forKey: p + "notifyReplies") as? Bool ?? true
        out.notifyAttention = defaults.object(forKey: p + "notifyAttention") as? Bool ?? true
        out.voiceLocale = defaults.string(forKey: p + "voiceLocale")
        out.askedNotifications = defaults.object(forKey: p + "askedNotifications") as? Bool ?? false
        return out
    }

    public func save(_ preferences: ChatPreferences, _ identity: String) {
        let p = prefix(identity)
        func set(_ key: String, _ value: Any?) {
            if let value { defaults.set(value, forKey: p + key) } else { defaults.removeObject(forKey: p + key) }
        }
        set("hiddenAgents", preferences.hiddenAgents.sorted())
        set("addedAgents", preferences.addedAgents.sorted())
        set("defaultAgent", preferences.defaultAgent)
        set("showAutomated", preferences.showAutomated)
        set("expandThinking", preferences.expandThinking)
        set("technicalDetails", preferences.technicalDetails)
        set("notifyReplies", preferences.notifyReplies)
        set("notifyAttention", preferences.notifyAttention)
        set("voiceLocale", preferences.voiceLocale)
        set("askedNotifications", preferences.askedNotifications)
    }

    public func clear(_ identity: String) {
        let p = prefix(identity)
        for key in defaults.dictionaryRepresentation().keys where key.hasPrefix(p) {
            defaults.removeObject(forKey: key)
        }
    }
}

/// The choices in memory: a test, a preview.
public final class MemoryPreferenceStore: PreferenceStore, @unchecked Sendable {
    private let lock = NSLock()
    private var kept: [String: ChatPreferences] = [:]

    public init() {}

    public func load(_ identity: String) -> ChatPreferences { lock.withLock { kept[identity] ?? ChatPreferences() } }
    public func save(_ preferences: ChatPreferences, _ identity: String) { lock.withLock { kept[identity] = preferences } }
    public func clear(_ identity: String) { lock.withLock { kept[identity] = nil } }
}
