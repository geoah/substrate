#if canImport(SwiftUI)
import Speech
import SubstrateChatCore
import SubstrateScreens
import SubstrateUI
import SwiftUI

/// Settings: Chat's assistants, the owner's memories and instructions, its
/// own rows (Chats, Notifications, Voice, Storage), and the kit's
/// Repository, Status and About.
struct SettingsView: View {
    @Environment(ChatHost.self) private var host
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    @Environment(\.chatUpdates) private var updates
    @State private var keySheet: KeySheet?
    @State private var locales: [Locale] = []
    @State private var changingAccess = false
    @State private var sharingDiagnostics = false

    var body: some View {
        let link = host.link
        let settings = host.chats?.settings
        NavigationStack {
            Form {
                if let settings {
                    assistants(settings.assistants)
                    GuidanceSectionView(section: settings.guidance)
                    ForEach(settings.sections) { section in
                        SettingsSectionView(section, send: send)
                        if section.id == .app("notifications") { voice(settings) }
                    }
                }
                if let repository = link.repository {
                    ForEach(SettingsPresenter.repository(repository)) { SettingsSectionView($0, send: send) }
                }
                if let problem = host.authorizeProblem {
                    Section { ProblemBox(title: "Access didn't change", message: problem) }
                }
                ForEach(SettingsPresenter.status(link.status)) { SettingsSectionView($0, send: send) }
                SettingsSectionView(SettingsPresenter.about(about), send: send)
                if updates.enabled {
                    Section {
                        Toggle("Include pre-releases", isOn: updates.prereleases)
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
            .sheet(item: $keySheet) { ProviderKeySheet(sheet: $0).environment(host) }
            .sheet(isPresented: $changingAccess) { ReviewSheet(isPresented: $changingAccess, changingAccess: true).environment(host) }
            .sheet(isPresented: $sharingDiagnostics) { DiagnosticsSheet(version: updates.build.version).environment(host) }
            .task {
                host.send(.readSettings)
                let supported = await SpeechTranscriber.supportedLocales
                locales = supported.sorted {
                    (Locale.current.localizedString(forIdentifier: $0.identifier) ?? "") < (Locale.current.localizedString(forIdentifier: $1.identifier) ?? "")
                }
            }
        }
    }

    private var about: AboutPresentation {
        AboutPresentation.present(
            appName: ChatManifest.appName, version: updates.build.version,
            channel: updates.enabled ? updates.build.channel : nil, offered: nil,
            privacyPolicy: updates.build.privacyPolicy)
    }

    private func send(_ command: SettingsCommand) {
        switch command {
        // The row asked first, by its own button (the kit's Sign Out).
        case .perform(.repository(.signOut)):
            dismiss()
            host.signOut()
        case .perform(.repository(.changeAccess)): changingAccess = true
        case .perform(.about(.check)): updates.checkNow()
        case .perform(.about(.privacyPolicy)):
            if let url = updates.build.privacyPolicy.flatMap(URL.init(string:)) { openURL(url) }
        case let .perform(.status(.action(action))): perform(action)
        case .perform(.app("allowNotifications")): openNotificationSettings()
        case .perform(.app("askNotifications")): host.send(.notifications(allow: true))
        case .perform(.app("shareDiagnostics")): sharingDiagnostics = true
        default: host.send(.settings(command))
        }
    }

    // MARK: Assistants

    @ViewBuilder private func assistants(_ section: AssistantsSection) -> some View {
        Section {
            Picker(
                section.defaultLabel,
                selection: Binding(
                    get: { section.defaultChoice },
                    set: { host.send(.settings(.choose(row: .app("defaultAgent"), option: $0))) })
            ) {
                ForEach(section.defaultOptions) { Text($0.label).tag($0.id) }
            }
            ForEach(section.rows) { row in
                AssistantRowView(
                    row: row,
                    offered: Binding(
                        get: { row.offered },
                        set: { host.send(.settings(.toggle(row: .app("agent:\(row.id)"), on: $0))) }),
                    addKey: { keySheet = host.chats?.keys[$0] })
            }
            if let empty = section.empty {
                Text(empty).ink(.quiet, on: .form)
            }
        } header: {
            Text(section.title)
        } footer: {
            Text(section.footer)
        }
        if !section.background.isEmpty {
            Section {
                ForEach(section.background) { agent in
                    VStack(alignment: .leading, spacing: ChatSpace.pair) {
                        Text(agent.name)
                        // Whole: what the agent does, or its reference where
                        // it says nothing of itself, which is who it is.
                        Text(agent.description ?? agent.id)
                            .font(ChatType.caption).ink(.quiet, on: .form)
                    }
                }
            } header: {
                Text(section.backgroundTitle)
            } footer: {
                Text(section.backgroundFooter)
            }
        }
    }

    // MARK: Voice

    /// The languages are the device's speech models, which only it knows.
    private func voice(_ settings: ChatSettingsPresentation) -> some View {
        Section {
            Picker(
                "Language",
                selection: Binding(
                    get: { settings.voiceLocale ?? "" },
                    set: { host.send(.settings(.choose(row: .app("voiceLocale"), option: $0))) })
            ) {
                Text("This device's").tag("")
                ForEach(locales, id: \.identifier) { locale in
                    Text(Locale.current.localizedString(forIdentifier: locale.identifier) ?? locale.identifier)
                        .tag(locale.identifier)
                }
            }
            Picker(
                "Tap the Microphone",
                selection: Binding(
                    get: { settings.micTap },
                    set: { host.send(.settings(.choose(row: .app("micTap"), option: $0.rawValue))) })
            ) {
                Text("Dictate").tag(MicAction.dictate)
                Text("Talk and Send").tag(MicAction.talk)
            }
            Picker(
                "Hold the Microphone",
                selection: Binding(
                    get: { settings.micHold },
                    set: { host.send(.settings(.choose(row: .app("micHold"), option: $0.rawValue))) })
            ) {
                Text("Talk and Send").tag(MicAction.talk)
                Text("Dictate").tag(MicAction.dictate)
                Text("Nothing").tag(MicAction.off)
            }
        } header: {
            Text("Voice")
        } footer: {
            Text(
                "Dictate puts what you say in the message, to change before you send it. Talk and Send sends it when you tap Send or let go. "
                    + "Speech is turned into text on this device. A language is downloaded the first time you use it.")
        }
    }

    private func perform(_ action: StatusAction) {
        switch action {
        case .sendNow: host.model.send(.sendNow)
        case .review, .renew: changingAccess = true
        case .signIn: host.signInAgain()
        case let .retry(id): host.model.send(.retry(id))
        case let .discard(id): host.model.send(.discard(id))
        case .readAgain: host.model.send(.readAgain)
        }
    }

    private func openNotificationSettings() {
        #if os(iOS)
        if let url = URL(string: UIApplication.openNotificationSettingsURLString) { openURL(url) }
        #else
        if let url = URL(string: "x-apple.systempreferences:com.apple.Notifications-Settings.extension") { openURL(url) }
        #endif
    }
}

private struct AssistantRowView: View {
    let row: AssistantRow
    @Binding var offered: Bool
    let addKey: (String) -> Void

    var body: some View {
        HStack(alignment: .top, spacing: ChatSpace.apart) {
            VStack(alignment: .leading, spacing: ChatSpace.mark) {
                AgentPill(id: row.id, name: row.name)
                if let description = row.description {
                    Text(description).font(ChatType.caption).ink(.quiet, on: .form)
                }
                WrapLayout(spacing: ChatSpace.words) {
                    ForEach(row.runsOn, id: \.self) { fact in
                        FactText(label: fact.label, value: fact.value, verbatim: fact.verbatim)
                            .font(ChatType.small)
                            .ink(.quiet, on: .form)
                    }
                    if let keyless = row.keyless {
                        Button(keyless.action) { addKey(keyless.id) }
                            .font(ChatType.smallLabel)
                            .buttonStyle(.borderless)
                            .tinted(.tone(.caution), on: .form)
                            .touchTarget()
                    }
                }
            }
            Spacer(minLength: ChatSpace.beside)
            Toggle("Show in chat", isOn: $offered)
                .labelsHidden()
        }
        .accessibilityElement(children: .combine)
    }
}

#if os(macOS)
/// What a Mac's Settings window holds: Settings where a repository is
/// signed in, and a line saying so where none is.
public struct ChatSettingsWindow: View {
    @Environment(ChatHost.self) private var host

    public init() {}

    public var body: some View {
        if host.chats?.signedIn == true {
            SettingsView()
                .frame(width: ChatSize.settings.width, height: ChatSize.settings.height)
        } else {
            Text("Sign in to change settings.")
                .ink(.quiet, on: .page)
                .frame(width: ChatSize.sheet.width, height: ChatSize.sheet.height)
        }
    }
}
#endif
#endif
