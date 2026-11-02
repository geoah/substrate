import Foundation
import SubstrateKit

/// The Console app's sign-ins, one per server, in the Keychain: the app holds
/// the session and hands it to the page (decision 0204), so pointing the app
/// elsewhere and back finds each server signed in as it was left. A server
/// that cannot grant the app full access is signed in to on the console's own
/// page instead, which keeps that session itself; the app remembers only that
/// it chose that door.
@MainActor
final class ConsoleSessions: ObservableObject {
    static let service = "dev.reamde.substrate.console"
    private static let pageSignInKey = "server.pageSignIn"

    /// Moves whenever a sign-in starts or ends, so views reading through the
    /// methods below render again.
    @Published private(set) var revision = 0
    private var pageSignIn: Set<String>
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        pageSignIn = Set(defaults.stringArray(forKey: Self.pageSignInKey) ?? [])
    }

    private func store(for server: URL) -> SessionStore {
        SessionStore(service: Self.service, account: "session " + server.absoluteString)
    }

    /// The app's sign-in to `server`, if it holds one.
    func session(for server: URL) -> Session? {
        store(for: server).load().flatMap { $0.server == server ? $0 : nil }
    }

    /// Whether the console opens on `server` without asking: the app holds a
    /// sign-in, or the console's own page keeps one.
    func opens(_ server: URL) -> Bool {
        session(for: server) != nil || pageSignIn.contains(server.absoluteString)
    }

    /// Signs in to the server a probe found: its own page in Safari's sheet,
    /// or the default browser on a Mac, asking for full access. The session
    /// replaces any the app held for that server, whose token is revoked.
    func signIn(_ probe: ServerProbe.Result) async throws {
        let session = try await ConsoleSignIn.session(probe)
        let server = probe.origin
        try await store(for: server).save(session, replacing: self.session(for: server))
        setPageSignIn(false, for: server)
        revision += 1
        _ = await store(for: server).revokePending(current: session)
    }

    /// The console's own page signs in to `server`, which cannot grant the
    /// app full access.
    func usePageSignIn(on server: URL) {
        setPageSignIn(true, for: server)
        revision += 1
    }

    /// The page said the sign-in ended (it signed out, or the server refused
    /// the token): the app stops using it and revokes it, which answers at
    /// once for a token the page already deleted. One the server does not
    /// answer for now stays kept, and is tried again at the next start.
    func signedOut(of server: URL) async {
        if let session = session(for: server) {
            try? await store(for: server).signOut(session)
        }
        revision += 1
        _ = await store(for: server).revokePending(current: nil)
        revision += 1
    }

    /// Tokens the app stopped using on any of `servers` that their server has
    /// not revoked yet; each is tried again at every start.
    func unrevoked(on servers: [URL]) -> [Session] {
        servers.flatMap { store(for: $0).pendingRevocations() }
    }

    func revokePending(on servers: [URL]) async {
        for server in servers {
            _ = await store(for: server).revokePending(current: session(for: server))
        }
        revision += 1
    }

    private func setPageSignIn(_ on: Bool, for server: URL) {
        if on { pageSignIn.insert(server.absoluteString) } else { pageSignIn.remove(server.absoluteString) }
        defaults.set(Array(pageSignIn).sorted(), forKey: Self.pageSignInKey)
    }
}
