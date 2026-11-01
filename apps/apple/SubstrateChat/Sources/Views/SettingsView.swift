import Speech
import SwiftUI
import UserNotifications
import SubstrateKit

struct SettingsView: View {
    @Environment(AppModel.self) private var model
    @Environment(ChatStore.self) private var store
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    @EnvironmentObject private var updates: Updater
    @State private var keySheet: Provider?
    @State private var notifications: UNAuthorizationStatus = .notDetermined
    @State private var locales: [Locale] = []
    @State private var size = 0
    @State private var confirmSignOut = false
    @State private var changingAccess = false
    @State private var serverVersion: String?

    var body: some View {
        @Bindable var prefs = store.prefs
        NavigationStack {
            Form {
                agentsSection

                Section {
                    Toggle("Show automated runs", isOn: Binding(get: { prefs.showAutomated }, set: {
                        prefs.showAutomated = $0
                        store.refresh()
                    }))
                    Toggle("Expand thinking", isOn: $prefs.expandThinking)
                    Toggle("Technical details", isOn: $prefs.technicalDetails)
                } header: {
                    Text("Chats")
                } footer: {
                    Text("Automated runs are the ones a trigger, a schedule or a webhook started. Technical details show each chat's id, status and cost.")
                }

                Section {
                    Toggle("Replies", isOn: $prefs.notifyReplies)
                    Toggle("Questions and suggestions", isOn: $prefs.notifyAttention)
                    if notifications == .denied {
                        Button("Allow Notifications in Settings") { openNotificationSettings() }
                    }
                } header: {
                    Text("Notifications")
                } footer: {
                    Text(notificationFooter)
                }

                Section {
                    Picker("Language", selection: Binding(get: { prefs.voiceLocale ?? "" }, set: { prefs.voiceLocale = $0.isEmpty ? nil : $0 })) {
                        Text("This device's").tag("")
                        ForEach(locales, id: \.identifier) { locale in
                            Text(Locale.current.localizedString(forIdentifier: locale.identifier) ?? locale.identifier)
                                .tag(locale.identifier)
                        }
                    }
                } header: {
                    Text("Voice")
                } footer: {
                    Text("Speech is turned into text on this device. A language is downloaded the first time you use it.")
                }

                Section("Storage") {
                    LabeledContent("Saved chats", value: byteWords(size))
                    Button("Clear Saved Chats", role: .destructive) {
                        Task {
                            await store.clearSavedChats()
                            size = store.cacheSize()
                        }
                    }
                }

                AccountSection(session: store.session, manifest: ChatManifest.manifest,
                               serverVersion: model.status.status?.serverVersion ?? serverVersion, unrevoked: model.unrevoked,
                               onChangeAccess: { changingAccess = true },
                               onSignOut: { confirmSignOut = true })

                if let problem = model.authorizeProblem {
                    Section {
                        ProblemBox(title: "Access didn't change", message: problem)
                    }
                }

                Section("App") {
                    LabeledContent("Version") {
                        Text(updates.build.version).monospaced().textSelection(.enabled)
                    }
                    if updates.enabled {
                        Button("Check for Updates") { updates.checkNow() }
                            .disabled(!updates.canCheck)
                        Toggle("Include pre-releases", isOn: $updates.prereleases)
                    }
                }
            }
            .formStyle(.grouped)
            .navigationTitle("Settings")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
            #endif
            .confirmationDialog("Sign out of \(store.session.repository)?", isPresented: $confirmSignOut) {
                Button("Sign Out", role: .destructive) {
                    dismiss()
                    model.signOut()
                }
            } message: {
                Text(store.grants == nil
                    ? "This device forgets its saved chats and the sign-in. Your chats stay on the server."
                    : "This device forgets its saved chats, and Substrate Chat's access is revoked. Your chats stay on the server.")
            }
            .sheet(item: $keySheet) { ProviderKeySheet(provider: $0).environment(store) }
            .sheet(isPresented: $changingAccess) {
                ReviewChangesSheet(appName: ChatManifest.appName, host: ServerAddress.display(store.session.server),
                                   changes: model.reviewChanges, heading: .changeAccess,
                                   onContinue: {
                                       changingAccess = false
                                       Task { await model.reauthorize() }
                                   },
                                   onCancel: { changingAccess = false })
            }
            .task {
                notifications = await Notifier.status()
                size = store.cacheSize()
                serverVersion = try? await SubstrateClient(server: store.session.server).discovery().server.version
                let supported = await SpeechTranscriber.supportedLocales
                locales = supported.sorted {
                    (Locale.current.localizedString(forIdentifier: $0.identifier) ?? "") <
                        (Locale.current.localizedString(forIdentifier: $1.identifier) ?? "")
                }
            }
        }
    }

    // MARK: Agents

    @ViewBuilder private var agentsSection: some View {
        let defaults = defaultOffered(store.agents)
        let chat = store.agents.filter(\.chatCapable).sorted { $0.name < $1.name }
        let background = store.agents.filter { !$0.chatCapable }.sorted { $0.name < $1.name }
        Section {
            Picker("Default for new chats", selection: Binding(
                get: { store.prefs.defaultAgent ?? "" },
                set: { store.prefs.defaultAgent = $0.isEmpty ? nil : $0 })) {
                Text("The last one I chatted with").tag("")
                ForEach(store.offered) { Text($0.name).tag($0.id) }
            }
            ForEach(chat) { agent in
                AgentRow(agent: agent, provider: agent.providerID.flatMap { store.providers[$0] },
                         offered: Binding(get: { store.prefs.offers(agent, defaults: defaults) },
                                          set: { store.prefs.setOffered(agent, $0, defaults: defaults) }),
                         addKey: { keySheet = $0 })
            }
            if chat.isEmpty {
                Text(store.loaded ? "No agent can chat yet." : "Loading…").foregroundStyle(.secondary)
            }
        } header: {
            Text("Assistants")
        } footer: {
            Text("The assistants switched on are offered for new chats, in the app and to the Action Button. New primary agents are switched on when they appear.")
        }
        if !background.isEmpty {
            Section {
                ForEach(background) { agent in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(agent.name)
                        Text(agent.description ?? agent.id)
                            .font(.caption).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
            } header: {
                Text("Works for other agents")
            } footer: {
                Text("These agents only take work from other agents, so they are not offered for chats.")
            }
        }
    }

    private var notificationFooter: String {
        #if os(iOS)
        let reach = "iPhone and iPad have no push from your server: a reply notifies when the app sees it settle, while it is open, while it keeps an answer running after you leave, or when iOS lets it check in the background."
        #else
        let reach = "Your server has no push: replies and questions notify while the app is open."
        #endif
        return notifications == .denied ? "Notifications are off for Substrate Chat. " + reach : reach
    }

    private func openNotificationSettings() {
        #if os(iOS)
        if let url = URL(string: UIApplication.openNotificationSettingsURLString) { openURL(url) }
        #else
        if let url = URL(string: "x-apple.systempreferences:com.apple.Notifications-Settings.extension") { openURL(url) }
        #endif
    }

}

private struct AgentRow: View {
    let agent: Agent
    let provider: Provider?
    @Binding var offered: Bool
    let addKey: (Provider) -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                AgentPill(id: agent.id)
                if let description = agent.description {
                    Text(description).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                }
                HStack(spacing: 6) {
                    Text([provider?.name ?? agent.providerID.map(providerName), agent.model].compactMap { $0 }.joined(separator: " · "))
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                    if let provider, !provider.hasKey {
                        Button("No API key: Add") { addKey(provider) }
                            .font(.caption2.weight(.semibold))
                            .buttonStyle(.borderless)
                            .tint(.orange)
                    }
                }
            }
            Spacer(minLength: 8)
            Toggle("Show in chat", isOn: $offered)
                .labelsHidden()
        }
        .accessibilityElement(children: .combine)
    }
}
