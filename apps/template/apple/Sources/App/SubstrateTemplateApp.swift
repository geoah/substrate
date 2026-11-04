import SubstrateTemplateUI
import SwiftUI

@main
struct SubstrateTemplateApp: App {
    /// Keeps a released build current from its feed (apps/README.md). A
    /// local build has no update address and never checks.
    @StateObject private var updates = Updater()
    @Environment(\.scenePhase) private var scenePhase

    init() {
        Template.start()
    }

    var body: some Scene {
        #if os(macOS)
        window
            .commands { UpdateCommands(updates: updates) }
        Settings {
            SettingsView()
                .checkingForUpdates { updates.checkNow() }
                .frame(width: 520, height: 620)
        }
        #else
        window
        #endif
    }

    private var window: some Scene {
        WindowGroup {
            RootView()
                .checkingForUpdates { updates.checkNow() }
                .updateOffer(updates)
                .onChange(of: updates.offer?.version, initial: true) { _, version in Template.updateOffered(version) }
                #if os(macOS)
            .frame(minWidth: 520, minHeight: 420)
                #endif
        }
        .onChange(of: scenePhase, initial: true) { _, phase in
            guard phase == .active else { return }
            Template.cameForward()
        }
    }
}
