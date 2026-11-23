import SubstrateTodayUI
import SwiftUI

@main
struct SubstrateTodayApp: App {
    /// Keeps a released build current from its feed (apps/README.md). A
    /// local build has no update address and never checks.
    @StateObject private var updates = Updater()
    @Environment(\.scenePhase) private var scenePhase

    init() {
        Today.start()
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
                .onChange(of: updates.offer?.version, initial: true) { _, version in Today.updateOffered(version) }
                // A tap on a widget, or on one of its lines, opens the app with a link of its own, in the
                // window that is already open.
                .onOpenURL { Today.open($0) }
                .handlesExternalEvents(preferring: ["*"], allowing: ["*"])
                #if os(macOS)
            .frame(minWidth: 520, minHeight: 420)
                #endif
        }
        #if os(macOS)
        // Wide enough at first for the day with a page beside it: that layout starts at 900 points.
        .defaultSize(width: 1180, height: 820)
        #endif
        .onChange(of: scenePhase, initial: true) { _, phase in
            guard phase == .active else { return }
            Today.cameForward()
        }
    }
}
