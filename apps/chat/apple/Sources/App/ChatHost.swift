import ChatKit
import Foundation
import Observation
import SubstrateKit
import SwiftUI

#if os(iOS)
import UIKit
#endif

/// The process's one link and chat list, and the flows only the device can
/// run: the sign-in in the browser, Change Access, the password door. The
/// views draw `model.presentation` and send commands; everything else is
/// ChatKit's, on the kit's runtime.
@MainActor
@Observable
final class ChatHost {
    typealias Link = LinkController<ChatApp>

    static let shared = ChatHost()

    let model: PresentationModel<Link>
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

    private init() {
        let store = SessionStore.chat
        let transport = URLSessionTransport.shared
        let folders = ApplicationFolders(name: "SubstrateChat")
        // No `environment.folder`: the link has one, and Chat's folder is the
        // signed-in repository's, so `ChatStorage` keeps the link generation
        // in each and moves it as a sign-out retires the folder.
        var environment = AppEnvironment.apple(transport: transport, folders: folders)
        environment.client = ClientSource { store.load().map { SubstrateClient(server: $0.server, token: $0.secret, transport: transport) } }
        environment.devices.notifications = ChatServices.notifications
        environment.devices.background = ChatServices.background
        #if os(iOS)
        let platform = ChatPlatform.ios
        let device = UIDevice.current.userInterfaceIdiom == .pad ? "this iPad" : "this iPhone"
        #else
        let platform = ChatPlatform.macos
        let device = "this Mac"
        #endif
        let app = ChatApp(
            storage: ChatStorage(base: try? folders.data(), writingOptions: folders.writingOptions),
            preferences: DefaultsPreferenceStore(defaults: .standard), platform: platform)
        let link = Link(
            app: app, manifest: ChatManifest.manifest, sessions: store,
            words: StatusWords(
                app: ChatManifest.appName, device: device, kept: "saved chats",
                signOut: "\(device.capitalizedFirst) forgets its saved chats. Your chats stay on the server."),
            offers: [.review, .signInAgain, .openFix])
        model = PresentationModel(link, environment: environment)
        model.onSignal(\.app?.answered) { [weak self] answer in self?.answered(answer) }
        model.onSignal(\.app?.keySaved) { [weak self] provider in self?.savedKey = provider }
        // A session an earlier build kept names no request: it is kept with
        // the one every such build asked for, so the check never asks about
        // a scope the owner left out.
        if let session = store.load(), ChatManifest.asked(session) != session {
            Task {
                try? await store.save(ChatManifest.asked(session), replacing: nil)
                model.send(.signedIn)
            }
        }
    }

    var link: LinkPresentation<ChatPresentation> { model.presentation }
    var chats: ChatPresentation? { model.presentation.app }

    func send(_ command: ChatApp.Command) {
        model.send(command, to: Link.appPath)
    }

    func send(_ command: Conversation.Command, to key: ChildKey) {
        model.send(command, to: Link.appPath + [key])
    }

    func route(_ route: ChatRoute) { send(.route(route)) }

    /// A provider's key, sent once; `savedKey` names the provider when the
    /// server kept it.
    func addKey(provider: String, key: String) {
        savedKey = nil
        send(.addKey(provider: provider, key: key))
    }

    /// A run settled: said, and felt on a phone, where its chat is on
    /// screen. Elsewhere its notification says it.
    private func answered(_ answer: ChatAnswer) {
        guard let thread = answer.thread, chats?.onScreen.contains(thread) == true else { return }
        AccessibilityNotification.Announcement(answer.words).post()
        #if os(iOS)
        UINotificationFeedbackGenerator().notificationOccurred(.success)
        #endif
    }

    /// The record views read through the session's own client, refused in
    /// the app's words where its access does not reach.
    func recordDirectory() -> RecordDirectory {
        let session = SessionStore.chat.load()
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
        try await adopt(oauth.signIn(server: probe.origin, repository: probe.repository, keepingRejectedIn: .chat))
    }

    /// The password door, for a server without app authorization.
    func signIn(server: URL, repository: String, password: String, totp: String?) async throws {
        let result = try await SubstrateClient(server: server).login(
            repository: repository, password: password, totp: totp,
            label: Self.tokenLabel)
        try await adopt(Session(server: server, repository: result.repository, secret: result.secret, tokenID: result.token.id))
    }

    /// The flow again on the same server with the whole request: a change of
    /// access, a renewal, or what a newer build asks for. The old access
    /// works until the new one is kept, so closing the sheet changes nothing;
    /// then the old token is kept for revocation and revoked.
    func reauthorize() {
        guard let old = SessionStore.chat.load() else { return }
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
            try await adopt(oauth.signIn(server: old.server), replacing: old)
        } catch let error where error is WebAuthenticator.Cancelled || isCancellation(error) {
            // The person closed the sheet: the access stays as it was.
        } catch {
            authorizeProblem = explain(error, app: manifest.appName, server: old.server)
        }
    }

    /// Keeps a new session through the kit's store, which keeps the token it
    /// replaces for revocation first, then tells the link.
    private func adopt(_ session: Session, replacing old: Session? = nil) async throws {
        try await SessionStore.chat.save(session, replacing: old)
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
