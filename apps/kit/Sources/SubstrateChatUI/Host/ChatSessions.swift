#if canImport(SwiftUI)
import Foundation
import Observation
import SubstrateAuth
import SubstrateChatCore
import SubstrateDevice
import SubstrateDeviceApple
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI

/// The chat inside another app: one conversation with an agent, drawn by
/// the screens Substrate Chat draws it with and run by the same engine, so
/// a reply streams, its tools and cards show, a draft is kept and a turn is
/// stopped as they are there. An app makes one of these for its process and
/// shows a conversation with `ChatSessionView`.
///
/// It runs Chat's own root (`ChatHost`) over the app's session: the session
/// is the app's to keep, so nothing here signs in, signs out or revokes a
/// token. The app says when its session changed (`sessionChanged()`), and
/// the root reads it again. What it keeps on the device (the transcripts it
/// read, drafts) is in a folder of the app's own, never Substrate Chat's.
/// It asks for no notifications, no background runs and no pushes: those
/// are the app's.
@MainActor
@Observable
public final class ChatSessions {
    /// What an app hands over to embed a conversation.
    public struct Configuration {
        /// The app's manifest: its name in the chat's words, and what its
        /// status check asks about.
        public var manifest: AppManifest
        /// Where the app keeps its session.
        public var sessions: SessionStore
        /// The folder in Application Support the chat keeps its saved
        /// copies in: one of the app's own, such as "SubstrateToday/Chat".
        public var folder: String

        public init(manifest: AppManifest, sessions: SessionStore, folder: String) {
            self.manifest = manifest
            self.sessions = sessions
            self.folder = folder
        }
    }

    let host: ChatHost

    public init(_ configuration: Configuration) {
        ChatManifest.runs(in: configuration.manifest.appName)
        host = ChatHost(
            ChatHost.Parts(
                sessions: configuration.sessions, transport: URLSessionTransport.shared, folders: ApplicationFolders(name: configuration.folder),
                // The owner's choices for the chat are kept apart from the
                // app's own defaults, under the folder's name.
                preferences: DefaultsPreferenceStore(
                    defaults: UserDefaults(suiteName: configuration.folder.replacingOccurrences(of: "/", with: ".")) ?? .standard),
                manifest: configuration.manifest))
    }

    /// The app signed in, signed out or changed its access: the session is
    /// read again from the app's store. Never a sign-out of the chat's own:
    /// the token is the app's to revoke.
    public func sessionChanged() {
        host.model.send(.readAgain)
        host.refreshStored()
    }

    /// The app came to the front or left it: the chat's watch and its
    /// streams follow.
    public func phase(_ phase: AppPhase) {
        host.send(.phase(phase))
        if phase == .active { host.model.send(.refresh(force: false)) }
    }
}

/// One conversation with an agent, in the chat's own screen: its
/// transcript from the start, the reply as it is written, and the composer.
/// With a `thread` it opens that thread; with none it is a new chat with
/// `agent`, whose first message opens one, said through `onThread` so the
/// app can keep it and open the same conversation next time.
public struct ChatSessionView: View {
    let sessions: ChatSessions
    let thread: String?
    let agent: String
    let name: String?
    let message: String?
    let close: (() -> Void)?
    let onThread: (String) -> Void

    @Environment(\.scenePhase) private var phase
    /// The window this view is to the chat list: it shows one conversation.
    @State private var scene = UUID().uuidString
    @State private var opened = false
    @State private var sent = false

    /// - Parameters:
    ///   - thread: the thread to open; nil starts a new chat.
    ///   - agent: who a new chat is with.
    ///   - name: what a thread this view opens is named in the chat list;
    ///     nil leaves it to be named from its first message.
    ///   - message: words to send as the view opens, such as what was typed
    ///     in the app's own field. Where they cannot be sent yet they stand
    ///     in the composer, and nothing is lost.
    ///   - close: draws a Done button that calls it, for a sheet.
    ///   - onThread: the thread the conversation is in, once it has one.
    public init(
        _ sessions: ChatSessions, thread: String?, agent: String, name: String? = nil, message: String? = nil, close: (() -> Void)? = nil,
        onThread: @escaping (String) -> Void = { _ in }
    ) {
        self.sessions = sessions
        self.thread = thread
        self.agent = agent
        self.name = name
        self.message = message
        self.close = close
        self.onThread = onThread
    }

    private var host: ChatHost { sessions.host }
    private var current: ChildKey? { host.chats?.scenes[scene]?.current }
    private var bound: String? { host.chats?.scenes[scene]?.thread }

    public var body: some View {
        NavigationStack {
            Group {
                if let ended = host.link.ended {
                    AccessEndedScreen(ended) { close?() }
                } else if host.chats?.signedIn == true, host.chats?.canChat == false {
                    ContentUnavailableView(
                        "\(ChatManifest.appName) can't talk to your assistants", systemImage: "bubble.left.and.exclamationmark.bubble.right",
                        description: Text("Its approval left out talking to your agents. Change its access in Settings."))
                } else if let current {
                    ConversationView(key: current, scene: scene)
                        .id(current)
                } else {
                    ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .toolbar {
                if let close {
                    ToolbarItem(placement: .cancellationAction) { Button("Done", action: close) }
                }
            }
        }
        .recordViewer(host.recordDirectory())
        .pacesReplies()
        .onAppear {
            host.send(.sceneAppeared(scene: scene, restoring: nil))
            open()
        }
        .onDisappear { host.send(.sceneGone(scene: scene)) }
        .onChange(of: phase, initial: true) { _, phase in host.send(.sceneActive(scene: scene, active: phase == .active)) }
        // The root reads the session a moment after it is made: the
        // conversation opens once it has.
        .onChange(of: host.chats?.signedIn) { _, _ in open() }
        .onChange(of: current, initial: true) { _, key in
            if let key { say(to: key) }
        }
        .onChange(of: bound, initial: true) { _, bound in
            guard let bound else { return }
            // A thread this view opened is named, so the chat list says
            // what it is; one that was there keeps its name.
            if thread == nil, let name { host.send(.rename(thread: bound, subject: name)) }
            onThread(bound)
        }
        // Last, so everything above reads it: how replies are paced is the
        // host's to hear too.
        .environment(host)
    }

    /// Shows the conversation in this view's window of the chat list, once.
    private func open() {
        guard !opened, host.chats?.signedIn == true else { return }
        opened = true
        if let thread {
            host.send(.show(thread: thread, scene: scene))
        } else {
            host.send(.newChat(agent: agent, listen: false, scene: scene))
        }
    }

    /// Sends the words the view was opened with, once: into the composer
    /// first, where they stay if the send cannot go yet.
    private func say(to key: ChildKey) {
        guard !sent, let message, !message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        sent = true
        host.send(.fill(message), to: key)
        host.send(.send, to: key)
    }
}
#endif
