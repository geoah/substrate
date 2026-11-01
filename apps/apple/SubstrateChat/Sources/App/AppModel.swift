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
    /// Tokens this device stopped using that the server has not revoked yet:
    /// each is tried again at every launch until it is.
    private(set) var unrevoked: [Session] = []
    /// Why the last sign-out did not happen: the token could be neither kept
    /// to revoke later nor revoked, so the session stays.
    var signOutProblem: String?
    /// A route waiting for a scene to take it.
    var pendingRoute: Route?
    /// Whether an app session is still set up with its server: the kit's
    /// check, at launch and on return to the foreground.
    @ObservationIgnored private(set) lazy var status = AppStatusModel { [weak self] in self?.statusCheck() }

    private init() {
        if let session = SessionStore.chat.load() { begin(Self.asked(session)) }
        Task { await revokePending() }
    }

    /// An app session an earlier build kept names no request: every such
    /// build asked for `ChatManifest.earliestRequest`, so a scope it held
    /// out of that was left out by the owner.
    nonisolated static func asked(_ session: Session) -> Session {
        guard session.isApp, session.requestedScopes == nil else { return session }
        var asked = session
        asked.requestedScopes = ChatManifest.earliestRequest
        return asked
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
        try await adopt(try await oauth.signIn(server: probe.origin))
    }

    /// The password door, for a server without app authorization.
    func signIn(server: URL, repository: String, password: String, totp: String?) async throws {
        let result = try await SubstrateClient(server: server)
            .login(repository: repository, password: password, totp: totp, label: Self.tokenLabel)
        try await adopt(Session(server: server, repository: result.repository, secret: result.secret, tokenID: result.token.id))
    }

    /// The flow again on the same server with the whole request: a change of
    /// access, a renewal, or what a newer build asks for. The old access
    /// works until the new one is kept, so closing the sheet changes nothing;
    /// then the old token is kept for revocation and revoked.
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
            try await adopt(try await oauth.signIn(server: old.server), replacing: old)
        } catch let error where error is WebAuthenticator.Cancelled || isCancellation(error) {
            // The person closed the sheet: the access stays as it was.
        } catch {
            authorizeProblem = explain(error, app: manifest.appName, server: old.server)
        }
    }

    /// Keeps a new session of either kind and starts on it, through the
    /// kit's store: the token it replaces is kept for revocation before the
    /// save overwrites the only other copy of it, or revoked first where the
    /// Keychain will not keep it, and a sign-in the Keychain will not hold is
    /// refused here, with the token it minted revoked, rather than lost at
    /// the next launch.
    private func adopt(_ session: Session, replacing old: Session? = nil) async throws {
        try await SessionStore.chat.save(session, replacing: old)
        end(forget: false)
        begin(session)
        if old != nil { await revokePending() }
    }

    /// After the server refused the token: back to sign-in, keeping the saved
    /// chats and choices, which the same repository signing in again reuses.
    func signInAgain() {
        guard let session else {
            expired = false
            return
        }
        stopUsing(session, forget: false)
    }

    /// Forgets the session on this device, and revokes the token in the
    /// background, so it works with the server gone or the token refused; a
    /// token the server does not answer for now is kept and tried again at
    /// the next launch.
    func signOut() {
        guard let session else {
            expired = false
            return
        }
        stopUsing(session, forget: true)
    }

    /// Keeps the token for revocation, or revokes it where the Keychain will
    /// not keep it, and only then forgets the session; one neither kept nor
    /// revoked stays, and the sign-out says why.
    private func stopUsing(_ session: Session, forget: Bool) {
        signOutProblem = nil
        Task {
            do {
                try await SessionStore.chat.signOut(session)
            } catch {
                signOutProblem = explain(error, server: session.server)
                return
            }
            if self.session?.secret == session.secret { end(forget: forget) }
            expired = false
            if forget { Notifier.removeAll() }
            await revokePending()
        }
    }

    /// Revokes the tokens kept for revocation, and keeps the ones still live
    /// to say so.
    func revokePending() async {
        unrevoked = await SessionStore.chat.revokePending(current: SessionStore.chat.load())
    }

    private func begin(_ session: Session) {
        expired = false
        let store = ChatStore(session: session)
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
        return AppStatusCheck(manifest: ChatManifest.manifest, session: store.session)
    }

    /// What a review asks the owner for the request Chat sends now: what the
    /// check found, and what they left out last time, offered back.
    var reviewChanges: [Change] {
        guard let session else { return [] }
        return ChatManifest.manifest.review(status.status, session: session)
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
