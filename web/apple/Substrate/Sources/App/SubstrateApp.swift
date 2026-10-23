import SwiftUI

@main
struct SubstrateApp: App {
    @StateObject private var store = ServerStore()
    @StateObject private var updates = Updater()
    @Environment(\.scenePhase) private var phase

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(store)
                .environmentObject(updates)
                .updateOffer(updates)
                #if os(macOS)
                .frame(minWidth: 720, minHeight: 480)
                #endif
        }
        #if os(macOS)
        .defaultSize(width: 1280, height: 860)
        .commands {
            ConsoleCommands()
            UpdateCommands(updates: updates)
        }
        #else
        .onChange(of: phase, initial: true) { _, phase in
            if phase == .active { updates.checkIfDue() }
        }
        #endif
    }
}

#if os(macOS)
/// The menu items that act on the focused window's console.
struct ConsoleCommands: Commands {
    @FocusedObject private var console: ConsoleController?

    var body: some Commands {
        CommandGroup(after: .appSettings) {
            Button("Change Server…") { console?.picking = true }
                .keyboardShortcut(",", modifiers: [.command, .shift])
                .disabled(console == nil)
        }
        CommandGroup(before: .toolbar) {
            Button("Reload Page") { console?.reload() }
                .keyboardShortcut("r")
                .disabled(console == nil)
            Divider()
            Button("Actual Size") { console?.zoom(by: nil) }
                .keyboardShortcut("0")
                .disabled(console == nil)
            Button("Zoom In") { console?.zoom(by: 0.1) }
                .keyboardShortcut("+")
                .disabled(console == nil)
            Button("Zoom Out") { console?.zoom(by: -0.1) }
                .keyboardShortcut("-")
                .disabled(console == nil)
            Divider()
        }
        CommandMenu("Go") {
            Button("Back") { console?.goBack() }
                .keyboardShortcut("[")
                .disabled(!(console?.canGoBack ?? false))
            Button("Forward") { console?.goForward() }
                .keyboardShortcut("]")
                .disabled(!(console?.canGoForward ?? false))
            Button("Home") { console?.goHome() }
                .keyboardShortcut("h", modifiers: [.command, .shift])
                .disabled(console == nil)
        }
    }
}
#endif
