import Foundation
import Observation
import SubstrateKit
#if os(iOS)
import UIKit
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
    /// The browser sheet is open for a change of access.
    private(set) var authorizing = false
    /// Why the last change of access did not finish.
    private(set) var authorizeProblem: String?
    /// A route waiting for a scene to take it.
    var pendingRoute: Route?
    /// Whether an app session is still set up with its server: the kit's
    /// check, at launch and on return to the foreground.
    @ObservationIgnored private(set) lazy var status = AppStatusModel { [weak self] in self?.statusCheck() }

    private init() {
        if let session = SessionStore.chat.load() { begin(session) }
    }

    var session: Session? { store?.session }

    // MARK: Signing in

    /// The app authorization flow on a server the sign-in screen probed: the
    /// browser sheet on its own sign-in and approval page, the redirect on a
    /// loopback port, the code exchanged with its PKCE verifier. The app
    /// never sees the password.
    func signIn(_ probe: ServerProbe.Result) async throws {
        guard let oauth = OAuthClient(manifest: ChatManifest.manifest, probe: probe) else {
            throw URLError(.badServerResponse)
        }
        try adopt(try await oauth.signIn(server: probe.origin))
    }

    /// The password door, for a server without app authorization.
    func signIn(server: URL, repository: String, password: String, totp: String?) async throws {
        let result = try await SubstrateClient(server: server)
            .login(repository: repository, password: password, totp: totp, label: Self.tokenLabel)
        try adopt(Session(server: server, repository: result.repository, secret: result.secret, tokenID: result.token.id))
    }

    /// The flow again on the same server with the whole request: a change of
    /// access, a renewal, or what a newer build asks for. The old access
    /// works until the new one is kept, so closing the sheet changes nothing;
    /// then the old token is revoked.
    func reauthorize() async {
        guard let old = session, !authorizing else { return }
        authorizing = true
        defer { authorizing = false }
        authorizeProblem = nil
        let manifest = ChatManifest.manifest
        do {
            let probe = try await ServerProbe().probe(old.server, requirement: manifest.requirement, app: manifest.appName)
            guard manifest.door(for: probe) == .app, let oauth = OAuthClient(manifest: manifest, probe: probe) else {
                throw Refusal(text: "\(ServerAddress.display(old.server)) can't give \(manifest.appName) access of its own: it needs an update first.")
            }
            try adopt(try await oauth.signIn(server: old.server))
            Self.revoke(old)
        } catch let error where error is WebAuthenticator.Cancelled || isCancellation(error) {
            // The person closed the sheet: the access stays as it was.
        } catch {
            authorizeProblem = explain(error, app: manifest.appName, server: old.server)
        }
    }

    /// Keeps a new session of either kind and starts on it. Kept first: a
    /// sign-in the Keychain would not hold is refused here, with the token it
    /// minted revoked, rather than lost at the next launch.
    private func adopt(_ session: Session) throws {
        do {
            try SessionStore.chat.save(session)
        } catch {
            Self.revoke(session)
            throw error
        }
        end(forget: false)
        begin(session, requested: session.isApp ? ChatManifest.scopes : nil)
    }

    /// After the server refused the token: back to sign-in, keeping the saved
    /// chats and choices, which the same repository signing in again reuses.
    func signInAgain() {
        guard let session else {
            expired = false
            return
        }
        end(forget: false)
        SessionStore.chat.clear()
        expired = false
        Self.revoke(session)
    }

    /// Forgets the session on this device at once, and revokes the token in
    /// the background, so it works with the server gone or the token refused.
    func signOut() {
        guard let session else {
            expired = false
            return
        }
        end(forget: true)
        SessionStore.chat.clear()
        Notifier.removeAll()
        Self.revoke(session)
    }

    /// Revokes a token this device no longer keeps, in the background and as
    /// a best effort: an app token at its revocation endpoint under the
    /// client id it was approved for, a password one with its own
    /// `DELETE /tokens/{id}`.
    nonisolated static func revoke(_ session: Session) {
        let session = revocable(session)
        Task.detached {
            if session.isApp {
                try? await OAuthClient.revoke(session)
            } else if let id = session.tokenID {
                try? await SubstrateClient(session).revokeToken(id: id)
            }
        }
    }

    /// The server leaves a token live when it is revoked under a client id
    /// it was not approved for. An app session an earlier build kept names
    /// none; that build sent the one it carried, which this build's is the
    /// best guess at.
    nonisolated static func revocable(_ session: Session) -> Session {
        var session = session
        if session.isApp, session.clientID == nil { session.clientID = ChatManifest.manifest.clientID }
        return session
    }

    private func begin(_ session: Session, requested: [String]? = nil) {
        expired = false
        let store = ChatStore(session: session)
        if let requested { store.prefs.requestedScopes = requested }
        store.onSessionExpired = { [weak self] in self?.sessionExpired() }
        self.store = store
        store.start()
        status.reset()
        Task { await status.refresh(force: true) }
    }

    private func end(forget: Bool) {
        store?.close(forget: forget)
        store = nil
        status.reset()
    }

    private func sessionExpired() {
        guard !expired else { return }
        expired = true
    }

    // MARK: Still set up

    /// What the app shows of the kit's check. A password session reaches
    /// everything and never ends, so only an app session hears of it; a
    /// server it could not reach is the chat list's own to say.
    var issues: [AppIssue] {
        guard session?.isApp == true else { return [] }
        return (status.status?.issues ?? []).filter {
            switch $0 {
            case .accessEndsSoon, .accessEnded, .serverTooOld, .needsApproval: true
            case .unreachable, .legacyAccess: false
            }
        }
    }

    private func statusCheck() -> AppStatusCheck? {
        guard let store, store.session.isApp else { return nil }
        return AppStatusCheck(manifest: ChatManifest.checked(session: store.session, requested: store.prefs.requestedScopes),
                              session: store.session)
    }

    // MARK: Routes

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

/// A refusal of the app's own, in its words.
private struct Refusal: LocalizedError {
    let text: String
    var errorDescription: String? { text }
}
