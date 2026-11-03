import ChatKit
import SubstrateKit
import SwiftUI

/// Signed out, the access ended, no access to the agents, or the chats.
struct RootView: View {
    @Environment(ChatHost.self) private var host

    var body: some View {
        let link = host.link
        Group {
            if let ended = link.ended {
                AccessEndedScreen(ended) { host.signInAgain() }
            } else if let chats = link.app, chats.signedIn, !chats.canChat {
                NoChatAccessView()
            } else if let chats = link.app, chats.signedIn {
                ChatSplitView()
                    .recordViewer(host.recordDirectory())
                    .id(chats.host)
            } else {
                SignInView()
            }
        }
        .animation(.default, value: link.app?.signedIn)
        .alert("Couldn't sign out", isPresented: Binding(get: { link.signOutProblem != nil }, set: { _ in })) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(link.signOutProblem?.message ?? "")
        }
    }
}

/// One window: which chat it shows, kept by the chat list by this window's
/// id, and the column a phone shows.
struct ChatSplitView: View {
    @Environment(ChatHost.self) private var host
    @EnvironmentObject private var updates: Updater
    @Environment(\.scenePhase) private var phase
    @Environment(\.horizontalSizeClass) private var sizeClass
    #if os(macOS)
    @Environment(\.controlActiveState) private var keyState
    #endif
    @State private var scene = UUID().uuidString
    @SceneStorage("chat.thread") private var restored = ""
    @State private var columns = NavigationSplitViewVisibility.automatic
    @State private var compactColumn = NavigationSplitViewColumn.sidebar
    @State private var showSettings = false

    private var current: ChildKey? { host.chats?.scenes[scene]?.current }

    var body: some View {
        NavigationSplitView(columnVisibility: $columns, preferredCompactColumn: $compactColumn) {
            ChatListView(scene: scene, showSettings: $showSettings)
                .navigationSplitViewColumnWidth(min: 260, ideal: 320, max: 420)
        } detail: {
            if let current, host.chats?.conversations[current] != nil {
                ConversationView(key: current, scene: scene)
                    .id(current)
            } else {
                NoChatView(scene: scene)
            }
        }
        .onAppear {
            host.send(.sceneAppeared(scene: scene, restoring: restored.isEmpty ? nil : restored))
            take()
        }
        .onDisappear { host.send(.sceneGone(scene: scene)) }
        .onChange(of: host.chats?.routePending) { _, _ in take() }
        .onChange(of: current) { _, key in
            if key != nil { compactColumn = .detail }
            restored = key.flatMap { host.chats?.conversations[$0]?.thread } ?? ""
        }
        .onChange(of: compactColumn) { _, column in
            // Back to the list on an iPhone: nothing is selected, so the same
            // row opens again when tapped.
            if column == .sidebar, sizeClass == .compact { host.send(.back(scene: scene)) }
        }
        .onChange(of: phase, initial: true) { _, phase in host.send(.sceneActive(scene: scene, active: phase == .active)) }
        #if os(iOS)
        .sheet(isPresented: $showSettings) {
            SettingsView()
                .environmentObject(updates)
        }
        #endif
    }

    /// One window takes a route: the key one on a Mac, else the first to
    /// claim it.
    private func take() {
        guard host.chats?.routePending == true else { return }
        #if os(macOS)
        if keyState != .key {
            Task {
                try? await Task.sleep(for: .milliseconds(80))
                host.send(.claimRoute(scene: scene))
            }
            return
        }
        #endif
        host.send(.claimRoute(scene: scene))
    }
}

/// The detail column with nothing chosen.
struct NoChatView: View {
    @Environment(ChatHost.self) private var host
    let scene: String

    var body: some View {
        ContentUnavailableView {
            Label("No chat selected", systemImage: "bubble.left.and.bubble.right")
        } description: {
            Text("Pick a chat, or start a new one.")
        } actions: {
            Button("New Chat") { host.send(.newChat(agent: nil, listen: false, scene: scene)) }
                .buttonStyle(.glassProminent)
        }
    }
}
