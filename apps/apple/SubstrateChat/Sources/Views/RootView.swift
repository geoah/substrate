import SwiftUI

/// Signed out, signed out by the server, or the chats.
struct RootView: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        Group {
            if model.expired {
                ExpiredView()
            } else if let store = model.store {
                ChatSplitView()
                    .environment(store)
                    .id(store.session.identity)
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
            take(model.pendingRoute)
        }
        .onChange(of: model.pendingRoute) { _, route in take(route) }
        .onChange(of: scene.current?.threadID) { _, thread in restored = thread ?? "" }
        .onChange(of: phase) { _, phase in
            store.setActive(phase == .active)
            #if os(iOS)
            if phase == .background {
                Background.leaving(store)
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

    private func take(_ route: Route?) {
        guard let route else { return }
        model.pendingRoute = nil
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
