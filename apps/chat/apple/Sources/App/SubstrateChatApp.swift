import SubstrateChatCore
import SubstrateChatUI
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

    /// Apple's answer to the registration for pushes, handed to the kit.
    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        MainActor.assumeIsolated { RemoteNotifications.shared?.registered(deviceToken) }
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: any Error) {
        MainActor.assumeIsolated { RemoteNotifications.shared?.failed(error) }
    }

    /// A push iOS runs the app for (`content-available`): a `read`, which
    /// takes back the notifications it covers. The system is answered on
    /// the main thread, once.
    func application(
        _ application: UIApplication, didReceiveRemoteNotification userInfo: [AnyHashable: Any],
        fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void
    ) {
        let flattened = PushPayload.flatten(userInfo)
        let answer = FetchAnswer(completionHandler)
        Task { @MainActor in
            let read = await ChatServices.takeBack(flattened)
            answer.call(read ? .newData : .noData)
        }
    }
}

/// The system's handler, carried to the main thread to be called there.
private final class FetchAnswer: @unchecked Sendable {
    private let handler: (UIBackgroundFetchResult) -> Void

    init(_ handler: @escaping (UIBackgroundFetchResult) -> Void) {
        self.handler = handler
    }

    func call(_ result: UIBackgroundFetchResult) { handler(result) }
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
                .chatUpdates(chatUpdates)
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
        .chatWindowSize()
        .windowToolbarStyle(.unified)
        .commands {
            ChatCommands(host: host)
            UpdateCommands(updates: updates)
        }
        #endif

        #if os(macOS)
        Settings {
            ChatSettingsWindow()
                .environment(host)
                .chatUpdates(chatUpdates)
        }
        #endif
    }

    /// What the chat's Settings reads of the updater this scene holds: the
    /// views are a library's, which cannot name the updater's type.
    private var chatUpdates: ChatUpdates {
        let updater = updates
        return ChatUpdates(
            build: BuildFacts(version: updater.build.version, channel: updater.build.channel, privacyPolicy: updater.build.privacyPolicy),
            enabled: updater.enabled, prereleases: $updates.prereleases, checkNow: { updater.checkNow() })
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
