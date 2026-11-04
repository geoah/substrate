import SubstrateLocal
import SubstrateScreens

/// Chat's own settings, in its words; the kit's sections (Repository,
/// Status, About) are drawn beside them from the link's presentation.
extension ChatApp {
    func settings(_ state: State) -> ChatSettingsPresentation {
        let all = state.agentList
        let defaults = defaultOffered(all)
        let chat = all.filter(\.chatCapable).sorted { $0.name < $1.name }
        let background = all.filter { !$0.chatCapable }.sorted { $0.name < $1.name }
        let rows = chat.map { agent -> AssistantRow in
            let provider = agent.providerID.flatMap { state.providers[$0] }.map(Provider.init(record:))
            return AssistantRow(
                id: agent.id, name: agent.name, description: agent.description,
                runsOn: [
                    (provider?.name ?? agent.providerID.map(providerName)).map { LabeledValue("Provider", $0) },
                    agent.model.map { LabeledValue("Model", $0, verbatim: true) },
                ].compactMap { $0 },
                offered: state.preferences.offers(agent, defaults: defaults),
                keyless: provider.flatMap { $0.hasKey ? nil : KeylessProvider(id: $0.id, name: $0.name, action: "No API key: Add") })
        }
        let assistants = AssistantsSection(
            title: "Assistants",
            footer:
                "The assistants switched on are offered for new chats, in the app and to the Action Button. New primary agents are switched on when they appear.",
            defaultLabel: "Default for new chats", defaultChoice: state.preferences.defaultAgent ?? "",
            defaultOptions: [SettingsOption(id: "", label: "The last one I chatted with")]
                + state.offered.map { SettingsOption(id: $0.id, label: $0.name) },
            rows: rows, empty: chat.isEmpty ? (state.loaded ? "No agent can chat yet." : "Loading…") : nil,
            background: background.map(Self.choice), backgroundTitle: "Works for other agents",
            backgroundFooter: "These agents only take work from other agents, so they are not offered for chats.")
        let prefs = state.preferences
        let reach =
            platform == .ios
            ? "iPhone and iPad have no push from your server: a reply notifies when the app sees it settle, while it is open, while it keeps an answer running after you leave, or when iOS lets it check in the background."
            : "Your server has no push: replies and questions notify while the app is open."
        let denied = state.permission == .off
        var sections = [
            SettingsSection(
                id: .app("chats"), title: "Chats",
                rows: [
                    SettingsRow(id: .app("showAutomated"), label: "Show automated runs", control: .toggle(prefs.showAutomated)),
                    SettingsRow(id: .app("expandThinking"), label: "Expand thinking", control: .toggle(prefs.expandThinking)),
                    SettingsRow(id: .app("technicalDetails"), label: "Technical details", control: .toggle(prefs.technicalDetails)),
                ], footer: "Automated runs are the ones a trigger, a schedule or a webhook started. Technical details show each chat's id, status and cost."),
            SettingsSection(
                id: .app("notifications"), title: "Notifications",
                rows: [
                    SettingsRow(id: .app("notifyReplies"), label: "Replies", control: .toggle(prefs.notifyReplies)),
                    SettingsRow(id: .app("notifyAttention"), label: "Questions and suggestions", control: .toggle(prefs.notifyAttention)),
                ]
                    + (denied
                        ? [
                            SettingsRow(
                                id: .app("allowNotifications"), label: "Allow Notifications in Settings",
                                control: .action(destructive: false, enabled: true))
                        ] : []),
                footer: denied ? "Notifications are off for Substrate Chat. " + reach : reach),
        ]
        if state.session != nil { sections.append(SettingsPresenter.storage(state.usage, budget: ChatStorage.budget)) }
        return ChatSettingsPresentation(
            assistants: assistants, sections: sections, notificationsDenied: denied, voiceLocale: prefs.voiceLocale)
    }
}
