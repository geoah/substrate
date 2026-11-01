import Foundation
import Observation
#if os(iOS)
import UIKit
import SubstrateKit
#endif

/// Where a notification, a shortcut or a menu asks the app to go.
enum Route: Equatable {
    case thread(String)
    case newChat(agent: String?, listen: Bool)
}

/// The session's lifecycle: signed out, signed in with a store, or signed
/// out by the server. One per process.
@MainActor
@Observable
final class AppModel {
    static let shared = AppModel()

    private(set) var store: ChatStore?
    /// The server refused the token: one screen, Sign In Again.
    private(set) var expired = false
    /// A route waiting for a scene to take it.
    var pendingRoute: Route?

    private init() {
        if let session = Keychain.load() { begin(session) }
    }

    var session: Session? { store?.session }

    /// The OAuth door: the browser sheet on the substrate's own sign-in and
    /// approval page, the redirect on a loopback port, the code exchanged with
    /// its PKCE verifier. The app never sees the password.
    func signIn(server: URL, metadata: OAuth.Metadata) async throws {
        let receiver = try await LoopbackReceiver.start()
        defer { receiver.stop() }
        let pkce = OAuth.PKCE()
        let state = OAuth.state()
        let redirect = receiver.redirect
        guard let url = OAuth.authorizeURL(metadata, redirect: redirect, pkce: pkce, state: state) else {
            throw URLError(.badURL)
        }
        let query = try await WebSignIn().run(url, receiver: receiver)
        let code = try OAuth.callback(query, state: state, issuer: metadata.issuer)
        let grant = try await OAuth.exchange(metadata, code: code, redirect: redirect, pkce: pkce)
        let session = Session(
            server: server, repository: grant.repository, secret: grant.accessToken, tokenID: "",
            grants: Grants(scopes: grant.scopes),
            expiresAt: grant.expiresIn.map { Date().addingTimeInterval(TimeInterval($0)) },
            revocation: metadata.revocationEndpoint)
        try Keychain.save(session)
        end(forget: false)
        begin(session)
    }

    /// The password door, for a server that predates app authorization.
    func signIn(server: URL, repository: String, password: String, totp: String?) async throws {
        let api = API(server: server)
        let result = try await api.login(repository: repository, password: password, totp: totp, label: Self.tokenLabel)
        let session = Session(server: server, repository: result.repository, secret: result.secret, tokenID: result.token.id)
        try Keychain.save(session)
        end(forget: false)
        begin(session)
    }

    /// After the server refused the token: back to sign-in, keeping the saved
    /// chats and choices, which the same repository signing in again reuses.
    func signInAgain() {
        guard let session else {
            expired = false
            return
        }
        end(forget: false)
        Keychain.clear()
        expired = false
        Task.detached { await Self.revoke(session) }
    }

    /// Best effort and quick: the token goes whether or not the server can be
    /// reached, and a server that already refuses it has nothing to revoke.
    private static func revoke(_ session: Session) async {
        if session.grants != nil {
            let endpoint = session.revocation ?? session.server.appending(path: "oauth/revoke")
            try? await OAuth.revoke(session.secret, at: endpoint)
        } else {
            try? await API(session).revoke(token: session.tokenID)
        }
    }

    /// Forgets the session on this device at once, and revokes the token in
    /// the background, so it works with the server gone or the token refused.
    func signOut() {
        guard let session else {
            expired = false
            return
        }
        end(forget: true)
        Keychain.clear()
        Notifier.removeAll()
        Task.detached { await Self.revoke(session) }
    }

    private func begin(_ session: Session) {
        expired = false
        let store = ChatStore(session: session)
        store.onSessionExpired = { [weak self] in self?.sessionExpired() }
        self.store = store
        store.start()
    }

    private func end(forget: Bool) {
        store?.close(forget: forget)
        store = nil
    }

    private func sessionExpired() {
        guard !expired else { return }
        expired = true
    }

    func route(_ route: Route) {
        pendingRoute = route
    }

    /// The pending route, for the one window that takes it.
    func claimRoute() -> Route? {
        defer { pendingRoute = nil }
        return pendingRoute
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
