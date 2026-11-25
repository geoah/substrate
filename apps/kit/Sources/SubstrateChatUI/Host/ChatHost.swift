#if canImport(SwiftUI)
import Foundation
import Observation
import SubstrateAPI
import SubstrateAuth
import SubstrateChatCore
import SubstrateCore
import SubstrateDevice
import SubstrateDeviceApple
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI

#if os(iOS)
import UIKit
#endif

/// The process's one link and chat list, and the flows only the device can
/// run: the sign-in in the browser, Change Access, the password door. The
/// views draw `model.presentation` and send commands; everything else is
/// SubstrateChatCore's, on the kit's runtime.
@MainActor
@Observable
public final class ChatHost {
    public typealias Link = LinkController<ChatApp>

    /// What the host is made of: the device's own in the app, which the app
    /// names (its session store, its folder, its device services); a
    /// fixture's for a measurement or a gallery, which must reach neither
    /// the Keychain nor the folder a signed-in build keeps.
    public struct Parts {
        public var sessions: SessionStore
        public var transport: any HTTPTransport
        public var folders: any AppFolders
        public var preferences: any PreferenceStore
        public var notifications: (any Notifications)?
        public var background: (any BackgroundWork)?
        /// The registration with the publisher's push relay: the device's
        /// alone, never a fixture's.
        public var push: PushRegistrar?
        /// The device's network where nil.
        public var connectivity: (any Connectivity)?
        /// The manifest of the app the session is another app's own: one
        /// that shows a conversation of the chat (`ChatSessions`). The link
        /// then asks about that app's access, and the session is never
        /// rewritten here. Nil in Substrate Chat, whose manifest is
        /// `ChatManifest.manifest`.
        public var manifest: AppManifest?

        public init(
            sessions: SessionStore, transport: any HTTPTransport, folders: any AppFolders, preferences: any PreferenceStore,
            notifications: (any Notifications)? = nil, background: (any BackgroundWork)? = nil, push: PushRegistrar? = nil,
            connectivity: (any Connectivity)? = nil, manifest: AppManifest? = nil
        ) {
            self.sessions = sessions
            self.transport = transport
            self.folders = folders
            self.preferences = preferences
            self.notifications = notifications
            self.background = background
            self.push = push
            self.connectivity = connectivity
            self.manifest = manifest
        }
    }

    public let model: PresentationModel<Link>
    /// Where the session is kept: the Keychain's, but for a fixture's. Every
    /// read and write of the session here and in the views goes through it.
    @ObservationIgnored let sessions: SessionStore
    /// The browser is open for a change of access.
    private(set) var authorizing = false
    /// Why the last change of access did not finish.
    private(set) var authorizeProblem: String?
    /// The change of access waiting on the browser. On macOS that is a tab
    /// the person may close unfinished, which never answers, so a new
    /// request replaces it and Cancel stops it.
    @ObservationIgnored private var authorization: Task<Void, Never>?
    /// The record views' reader, one per signed-in repository.
    private(set) var records = RecordDirectory(reader: nil)
    @ObservationIgnored private var recordsFor: String?
    /// The provider whose key was saved since the last `addKey`: its sheet
    /// closes.
    private(set) var savedKey: String?
    /// What the chat list's microphone hands the new chat it opens.
    public var voice = ListVoice()

    public struct ListVoice: Equatable {
        /// A tap opened it listening, and the listening ends in a send.
        public var talks = false
        /// A hold heard these words before it opened: they go into the
        /// field, and are sent where `sends`.
        var heard: String?
        var sends = false
    }

    /// A session is in the Keychain: until the link has read it, the first
    /// screen is the chats coming, never the sign-in page (`AppShell.stored`).
    private(set) var stored: Bool

    public init(_ parts: Parts) {
        let store = parts.sessions
        sessions = store
        stored = store.load() != nil
        let transport = parts.transport
        let folders = parts.folders
        // No `environment.folder`: the link has one, and Chat's folder is the
        // signed-in repository's, so `ChatStorage` keeps the link generation
        // in each and moves it as a sign-out retires the folder.
        var environment = AppEnvironment.apple(transport: transport, folders: folders)
        // Each client says its session's token: an effect gets only the
        // session it started under.
        environment.client = .sessions({
            await store.load().map { .init(client: SubstrateClient(server: $0.server, token: $0.secret, transport: transport), tokenID: $0.tokenID) }
        })
        environment.devices.notifications = parts.notifications
        environment.devices.background = parts.background
        environment[PushRegistrarKey.self] = parts.push
        if let connectivity = parts.connectivity { environment.devices.connectivity = connectivity }
        #if os(iOS)
        let platform = ChatPlatform.ios
        let device = UIDevice.current.userInterfaceIdiom == .pad ? "this iPad" : "this iPhone"
        #else
        let platform = ChatPlatform.macos
        let device = "this Mac"
        #endif
        let app = ChatApp(
            storage: ChatStorage(base: try? folders.data(), writingOptions: folders.writingOptions),
            preferences: parts.preferences, platform: platform)
        let link = Link(
            app: app, manifest: parts.manifest ?? ChatManifest.manifest, sessions: store,
            words: StatusWords(
                app: ChatManifest.appName, device: device, kept: "saved chats",
                signOut: "\(device.capitalizedFirst) forgets its saved chats. Your chats stay on the server."),
            offers: [.review, .signInAgain, .openFix])
        model = PresentationModel(link, environment: environment)
        model.onSignal(\.app?.answered) { [weak self] answer in self?.answered(answer) }
        model.onSignal(\.app?.keySaved) { [weak self] provider in self?.savedKey = provider }
        // A session an earlier build kept names no request: it is kept with
        // the one every such build asked for, so the check never asks about
        // a scope the owner left out. Substrate Chat's own sessions alone:
        // another app's session is that app's to write.
        if parts.manifest == nil, let session = store.load(), ChatManifest.asked(session) != session {
            Task {
                try? await store.save(ChatManifest.asked(session), replacing: nil)
                model.send(.signedIn)
            }
        }
    }

    /// The link's phase moved: signed out, the sign-in page is the one to
    /// show.
    func refreshStored() {
        let now = sessions.load() != nil
        if now != stored { stored = now }
    }

    // What a view reads, each a part of the tree of its own (the kit's
    // `PresentationModel.part(at:)`): a view is drawn again when the part
    // it reads changes, and for nothing else, so a reply being written
    // draws its conversation and neither the list nor Settings.

    /// The link's own screens: the banner, the access ended, the status.
    /// Its `app` is nil: the app's part is `chats`.
    var link: LinkPresentation<ChatPresentation> {
        model.part(at: []) ?? model.presentation
    }

    /// The app's own part: the list, the windows, Settings, the agents.
    public var chats: ChatSurface? {
        model.part(at: Link.appPath, as: ChatPresentation.self).map(ChatSurface.init)
    }

    /// One conversation's screen, by its key.
    public func conversation(_ key: ChildKey) -> ConversationScreen? {
        model.part(at: Link.appPath + [key])
    }

    public func send(_ command: ChatApp.Command) {
        model.send(command, to: Link.appPath)
    }

    public func send(_ command: Conversation.Command, to key: ChildKey) {
        model.send(command, to: Link.appPath + [key])
    }

    public func route(_ route: ChatRoute) { send(.route(route)) }

    /// A provider's key, sent once; `savedKey` names the provider when the
    /// server kept it.
    func addKey(provider: String, key: String) {
        savedKey = nil
        send(.addKey(provider: provider, key: key))
    }

    /// A run settled: said, and felt on a phone, where its chat is on
    /// screen. Elsewhere in the app (another chat, the list, Settings) it
    /// is felt lightly and not said: the person is reading something else,
    /// and the list shows which chat it was. Away from the app its
    /// notification says it.
    private func answered(_ answer: ChatAnswer) {
        guard let thread = answer.thread else { return }
        guard model.presentation.app?.onScreen.contains(thread) == true else {
            #if os(iOS)
            if UIApplication.shared.applicationState == .active { UIImpactFeedbackGenerator(style: .light).impactOccurred() }
            #endif
            return
        }
        AccessibilityNotification.Announcement(answer.words).post()
        #if os(iOS)
        UINotificationFeedbackGenerator().notificationOccurred(.success)
        #endif
    }

    /// The record views read through the session's own client, refused in
    /// the app's words where its access does not reach.
    func recordDirectory() -> RecordDirectory {
        let session = sessions.load()
        guard session?.identity != recordsFor else { return records }
        recordsFor = session?.identity
        records = RecordDirectory(
            reader: session.map {
                HTTPRecordReader(client: SubstrateClient(server: $0.server, token: $0.secret), deniedHint: ChatManifest.deniedHint($0.grants))
            })
        return records
    }

    // MARK: Signing in

    /// The app authorization flow on a server the connect screen probed: the
    /// browser on its own sign-in and approval page, the redirect on a
    /// claimed callback or a loopback port, the code exchanged with its
    /// PKCE verifier. The app never sees the password.
    func signIn(_ probe: ServerProbe.Result) async throws {
        guard let oauth = OAuthClient(manifest: ChatManifest.manifest, probe: probe) else { throw URLError(.badServerResponse) }
        try await adopt(oauth.signIn(server: probe.origin, repository: probe.repository, keepingRejectedIn: sessions, browser: WebAuthenticator()))
    }

    /// The password door, for a server without app authorization: the name
    /// the person signs in with, sent as the probed door names it, and the
    /// repository the person picked where the door asked them to.
    func signIn(_ probe: ServerProbe.Result, name: String, repository: String? = nil, password: String, totp: String?) async throws {
        let result = try await SubstrateClient(server: probe.origin).login(
            name: name, repository: repository ?? probe.repository, password: password, totp: totp,
            label: Self.tokenLabel, door: probe.discovery)
        try await adopt(Session(server: probe.origin, repository: result.repository, secret: result.secret, tokenID: result.token.id))
    }

    /// The flow again on the same server with the whole request: a change of
    /// access, a renewal, or what a newer build asks for. The old access
    /// works until the new one is kept, so closing the sheet changes nothing;
    /// then the old token is kept for revocation and revoked.
    func reauthorize() {
        guard let old = sessions.load() else { return }
        authorization?.cancel()
        authorizing = true
        authorizeProblem = nil
        let task = Task { await reauthorize(replacing: old) }
        authorization = task
        Task {
            await task.value
            if authorization == task {
                authorization = nil
                authorizing = false
            }
        }
    }

    func cancelAuthorization() {
        authorization?.cancel()
    }

    private func reauthorize(replacing old: Session) async {
        let manifest = ChatManifest.manifest
        do {
            let probe = try await ServerProbe().probe(old.server, requirement: manifest.requirement, app: manifest.appName)
            guard manifest.door(for: probe) == .app, let oauth = OAuthClient(manifest: manifest, probe: probe) else {
                authorizeProblem = "\(ServerAddress.display(old.server)) can't give \(manifest.appName) access of its own: it needs an update first."
                return
            }
            try await adopt(oauth.signIn(server: old.server, browser: WebAuthenticator()), replacing: old)
        } catch let error where error is WebAuthenticator.Cancelled || isCancellation(error) {
            // The person closed the sheet: the access stays as it was.
        } catch {
            authorizeProblem = explain(error, app: manifest.appName, server: old.server)
        }
    }

    /// Keeps a new session through the kit's store, which keeps the token it
    /// replaces for revocation first, then tells the link.
    private func adopt(_ session: Session, replacing old: Session? = nil) async throws {
        try await sessions.save(session, replacing: old)
        model.send(.signedIn)
    }

    // MARK: Signing out

    /// Forgets the session on this device, and the saved chats with it; the
    /// token is revoked in the background, and one the server does not
    /// answer for now is tried again at the next launch.
    func signOut() {
        send(.forgetOnSignOut(true))
        model.send(.signOut)
    }

    /// After the server refused the token: back to sign-in, keeping the
    /// saved chats, which the same repository signing in again reuses.
    func signInAgain() {
        send(.forgetOnSignOut(false))
        model.send(.signOut)
    }

    static var tokenLabel: String {
        #if os(macOS)
        let device = Host.current().localizedName ?? "Mac"
        #else
        let device = UIDevice.current.name
        #endif
        return "Substrate Chat on \(device)"
    }
}

/// The app's own part of the tree, as a view reads it: every field of the
/// presentation but its conversations, each of which is a part of its own
/// and is read by its key (`ChatHost.conversation(_:)`). Reading one here
/// would find none, so it does not compile.
@dynamicMemberLookup
public struct ChatSurface {
    let own: ChatPresentation

    init(_ own: ChatPresentation) {
        self.own = own
    }

    public subscript<Value>(dynamicMember path: KeyPath<ChatPresentation, Value>) -> Value { own[keyPath: path] }

    @available(*, unavailable, message: "The app's own part holds no conversation: read one by its key, with ChatHost.conversation(_:).")
    public var conversations: [ChildKey: ConversationScreen] { [:] }
}
#endif
