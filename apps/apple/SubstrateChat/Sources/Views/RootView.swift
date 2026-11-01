import SwiftUI
import SubstrateKit

/// Signed out, signed out by the server, or the chats.
struct RootView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        Group {
            if model.expired {
                ExpiredView()
            } else if let store = model.store, !store.canChat {
                NoChatAccessView()
            } else if let store = model.store {
                // One view per store: a change of access starts a new one on
                // the same identity, and a conversation of the old is closed.
                ChatSplitView()
                    .environment(store)
                    .recordViewer(store.records)
                    .id(ObjectIdentifier(store))
            } else {
                SignInView()
            }
        }
        .animation(.default, value: model.store == nil)
    }
}

/// One window's state: which chat it shows. The store is shared by every
/// window; this is not.
@MainActor
@Observable
final class SceneModel {
    let id = UUID()
    var current: Conversation?
    var showSettings = false
    var compactColumn = NavigationSplitViewColumn.sidebar

    func open(_ conversation: Conversation) {
        current = conversation
        compactColumn = .detail
    }
}

struct ChatSplitView: View {
    @Environment(AppModel.self) private var model
    @EnvironmentObject private var updates: Updater
    @Environment(ChatStore.self) private var store
    @Environment(\.scenePhase) private var phase
    @Environment(\.horizontalSizeClass) private var sizeClass
    #if os(macOS)
    @Environment(\.controlActiveState) private var keyState
    #endif
    @State private var scene = SceneModel()
    @SceneStorage("chat.thread") private var restored = ""
    @State private var columns = NavigationSplitViewVisibility.automatic

    var body: some View {
        @Bindable var scene = scene
        NavigationSplitView(columnVisibility: $columns, preferredCompactColumn: $scene.compactColumn) {
            ChatListView()
                .navigationSplitViewColumnWidth(min: 260, ideal: 320, max: 420)
        } detail: {
            if let conversation = scene.current {
                ConversationView(conversation: conversation)
                    .id(conversation.id)
            } else {
                NoChatView()
            }
        }
        .environment(scene)
        .onAppear {
            if !restored.isEmpty, scene.current == nil, store.thread(restored) != nil {
                scene.open(store.conversation(thread: restored))
            }
            take()
        }
        .onChange(of: model.pendingRoute) { _, _ in take() }
        .onChange(of: scene.compactColumn) { _, column in
            // Back to the list on an iPhone: nothing is selected, so the same
            // row opens again when tapped.
            if column == .sidebar, sizeClass == .compact { scene.current = nil }
        }
        .onChange(of: scene.current?.threadID) { _, thread in restored = thread ?? "" }
        .onChange(of: phase) { _, phase in
            store.setActive(phase == .active)
            if phase == .active { Task { await model.status.refresh() } }
            #if os(iOS)
            // Asked for while the app is still in front, as iOS requires of a
            // continued processing task: inactive comes before background.
            if phase == .inactive { Background.leaving(store) }
            if phase == .background {
                Background.schedule()
                Notifier.badge(store.unreadCount)
            }
            #endif
        }
        #if os(iOS)
        .sheet(isPresented: $scene.showSettings) {
            SettingsView()
                .environmentObject(updates)
        }
        #endif
    }

    /// One window takes a route: the key one on a Mac, else the first to
    /// claim it.
    private func take() {
        guard model.pendingRoute != nil else { return }
        #if os(macOS)
        if keyState != .key {
            Task {
                try? await Task.sleep(for: .milliseconds(80))
                if let route = model.claimRoute() { open(route) }
            }
            return
        }
        #endif
        if let route = model.claimRoute() { open(route) }
    }

    private func open(_ route: Route) {
        switch route {
        case let .thread(id):
            scene.open(store.conversation(thread: id))
        case let .newChat(agent, listen):
            let conversation = store.newConversation(agent: agent)
            conversation.listenOnOpen = listen
            scene.open(conversation)
        }
    }
}

/// The detail column with nothing chosen.
struct NoChatView: View {
    @Environment(ChatStore.self) private var store
    @Environment(SceneModel.self) private var scene

    var body: some View {
        ContentUnavailableView {
            Label("No chat selected", systemImage: "bubble.left.and.bubble.right")
        } description: {
            Text("Pick a chat, or start a new one.")
        } actions: {
            Button("New Chat") { scene.open(store.newConversation()) }
                .buttonStyle(.glassProminent)
        }
    }
}
