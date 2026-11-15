import ChatKit
import SubstrateKit
import SwiftUI

#if os(iOS)
import UIKit
#endif

#if os(iOS)
final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        MainActor.assumeIsolated { ChatServices.start() }
        return true
    }
}
#endif

@main
struct SubstrateChatApp: App {
    #if os(iOS)
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    #endif
    @State private var host: ChatHost
    @StateObject private var updates = Updater()
    @Environment(\.scenePhase) private var phase

    init() {
        ChatServices.start()
        _host = State(initialValue: ChatHost.shared)
        #if DEBUG
        Measure.start(ChatHost.shared)
        Demo.start(ChatHost.shared)
        #endif
        ChatShortcuts.updateAppShortcutParameters()
    }

    var body: some Scene {
        WindowGroup(id: "chats") {
            RootView()
                .environment(host)
                .environmentObject(updates)
                .updateOffer(updates)
                .onChange(of: phase, initial: true) { _, phase in
                    switch phase {
                    case .active:
                        host.send(.phase(.active))
                        host.model.send(.refresh(force: false))
                        #if os(iOS)
                        updates.checkIfDue()
                        #endif
                    case .inactive: host.send(.phase(.inactive))
                    case .background: host.send(.phase(.background))
                    @unknown default: break
                    }
                }
                .onChange(of: host.chats?.offered) { _, _ in ChatShortcuts.updateAppShortcutParameters() }
        }
        #if os(macOS)
        .defaultSize(ChatSize.window)
        .windowToolbarStyle(.unified)
        .commands {
            ChatCommands(host: host)
            UpdateCommands(updates: updates)
        }
        #endif

        #if os(macOS)
        Settings {
            if host.chats?.signedIn == true {
                SettingsView()
                    .environment(host)
                    .environmentObject(updates)
                    .frame(width: ChatSize.settings.width, height: ChatSize.settings.height)
            } else {
                Text("Sign in to change settings.")
                    .ink(.quiet, on: .page)
                    .frame(width: ChatSize.settingsEmpty.width, height: ChatSize.settingsEmpty.height)
            }
        }
        #endif
    }
}

#if os(macOS)
/// The Mac's menus: New Chat, Stop, and Refresh.
struct ChatCommands: Commands {
    let host: ChatHost
    @FocusedValue(\.conversation) private var conversation
    @Environment(\.openWindow) private var openWindow

    private var signedIn: Bool { host.chats?.signedIn == true }

    var body: some Commands {
        // New Chat takes ⌘N, so the window SwiftUI's own item opened moves to ⌥⌘N.
        CommandGroup(replacing: .newItem) {
            Button("New Chat") { host.route(.newChat(agent: nil, listen: false)) }
                .keyboardShortcut("n")
                .disabled(!signedIn)
            Button("New Window") { openWindow(id: "chats") }
                .keyboardShortcut("n", modifiers: [.command, .option])
            Button("New Chat with Voice") { host.route(.newChat(agent: nil, listen: true)) }
                .keyboardShortcut("n", modifiers: [.command, .shift])
                .disabled(!signedIn)
        }
        CommandMenu("Chat") {
            Button("Stop Answering") { if let conversation { host.send(.stop, to: conversation) } }
                .keyboardShortcut(".")
                .disabled(conversation.map { host.chats?.streaming.contains($0) == true } != true)
            Button("Refresh") { host.send(.refresh) }
                .keyboardShortcut("r")
                .disabled(!signedIn)
        }
    }
}
#endif

extension FocusedValues {
    /// The conversation the focused window shows.
    @Entry var conversation: ChildKey?
}
