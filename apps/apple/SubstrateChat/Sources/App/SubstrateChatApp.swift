import SwiftUI
import UserNotifications
import SubstrateKit

@main
struct SubstrateChatApp: App {
    #if os(iOS)
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    #endif
    @State private var model = AppModel.shared
    @StateObject private var updates = Updater()
    @Environment(\.scenePhase) private var phase

    init() {
        UNUserNotificationCenter.current().delegate = NotificationDelegate.shared
        ChatShortcuts.updateAppShortcutParameters()
    }

    var body: some Scene {
        WindowGroup(id: "chats") {
            RootView()
                .environment(model)
                .environmentObject(updates)
                .updateOffer(updates)
                .onChange(of: phase) { _, phase in
                    #if os(iOS)
                    if phase == .active { updates.checkIfDue() }
                    #endif
                }
        }
        #if os(macOS)
        .defaultSize(width: 1080, height: 760)
        .windowToolbarStyle(.unified)
        .commands {
            ChatCommands(model: model)
            UpdateCommands(updates: updates)
        }
        #endif

        #if os(macOS)
        Settings {
            if let store = model.store {
                SettingsView()
                    .environment(model)
                    .environment(store)
                    .environmentObject(updates)
                    .frame(width: 560, height: 620)
            } else {
                Text("Sign in to change settings.")
                    .foregroundStyle(.secondary)
                    .frame(width: 360, height: 160)
            }
        }
        #endif
    }
}

#if os(macOS)
/// The Mac's menus: New Chat, Stop, and Sign Out.
struct ChatCommands: Commands {
    let model: AppModel
    @FocusedValue(\.conversation) private var conversation
    @Environment(\.openWindow) private var openWindow

    var body: some Commands {
        // New Chat takes ⌘N, so the window SwiftUI's own item opened moves to ⌥⌘N.
        CommandGroup(replacing: .newItem) {
            Button("New Chat") { model.route(.newChat(agent: nil, listen: false)) }
                .keyboardShortcut("n")
                .disabled(model.store == nil)
            Button("New Window") { openWindow(id: "chats") }
                .keyboardShortcut("n", modifiers: [.command, .option])
            Button("New Chat with Voice") { model.route(.newChat(agent: nil, listen: true)) }
                .keyboardShortcut("n", modifiers: [.command, .shift])
                .disabled(model.store == nil)
        }
        CommandMenu("Chat") {
            Button("Stop Answering") { conversation?.stop() }
                .keyboardShortcut(".")
                .disabled(conversation?.streaming != true)
            Button("Refresh") { model.store?.refresh() }
                .keyboardShortcut("r")
                .disabled(model.store == nil)
        }
    }
}
#endif

extension FocusedValues {
    @Entry var conversation: Conversation?
}
