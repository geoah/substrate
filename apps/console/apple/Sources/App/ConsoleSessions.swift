import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDeviceApple

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
    private static let knownKey = "server.signedIn"

    /// Moves whenever a sign-in starts or ends, so views reading through the
    /// methods below render again.
    @Published private(set) var revision = 0
    private var pageSignIn: Set<String>
    /// Every server the app has held a sign-in for and not forgotten: what
    /// pending revocations are looked for under, however long ago it was used.
    private var known: [URL]
    /// The Keychain read once per server, until a write here moves it: views
    /// ask on every render.
    private var cache: [URL: Session?] = [:]
    /// Sign-ins the page ended that the Keychain would not let go: never used
    /// again, and retired by the next sign-in to that server.
    private var ended: Set<URL> = []
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        pageSignIn = Set(defaults.stringArray(forKey: Self.pageSignInKey) ?? [])
        known = (defaults.stringArray(forKey: Self.knownKey) ?? []).compactMap(ServerAddress.origin(of:))
    }

    /// The Keychain item of the app's sign-in to `server`, and of the tokens
    /// kept beside it to revoke.
    static func store(for server: URL) -> SessionStore {
        SessionStore(account: "session " + server.absoluteString, storage: KeychainStorage(service: service))
    }

    private func store(for server: URL) -> SessionStore {
        Self.store(for: server)
    }

    /// The app's sign-in to `server`, if it holds one it still uses.
    func session(for server: URL) -> Session? {
        guard !ended.contains(server) else { return nil }
        if let cached = cache[server] { return cached }
        let loaded = store(for: server).load().flatMap { $0.server == server ? $0 : nil }
        cache[server] = loaded
        return loaded
    }

    /// Whether the console opens on `server` without asking: the app holds a
    /// sign-in, or the console's own page keeps one.
    func opens(_ server: URL) -> Bool {
        session(for: server) != nil || pageSignIn.contains(server.absoluteString)
    }

    /// The servers this device opens, newest sign-in first.
    var servers: [URL] { known.filter(opens) }

    /// Signs in to the server a probe found: its own page in Safari's sheet,
    /// or the default browser on a Mac, asking for full access. The session
    /// replaces any the app held for that server, whose token is revoked
    /// behind it: the sign-in does not wait on the server for that.
    func signIn(_ probe: ServerProbe.Result) async throws {
        let server = probe.origin
        // Known before the browser opens: a token minted for a repository
        // other than the one asked for is kept under this server to revoke,
        // and a start retries revocations on the servers it knows alone.
        remember(server)
        let session: Session
        do {
            session = try await ConsoleSignIn.session(probe)
        } catch {
            forgetIfUnused(server)
            throw error
        }
        let old = await store(for: server).load()
        try await store(for: server).save(session, replacing: old)
        changed(server)
        ended.remove(server)
        setPageSignIn(false, for: server)
        remember(server)
        Task { await self.revokePending(on: [server]) }
    }

    /// The console's own page signs in to `server`, which cannot grant the
    /// app full access.
    func usePageSignIn(on server: URL) {
        setPageSignIn(true, for: server)
        remember(server)
        changed(server)
    }

    /// The page said the sign-in holding `tokenID` ended: it signed out, or
    /// the server refused the token. The app stops using it and revokes it,
    /// a revocation the server does not answer now being tried again at the
    /// next start. A message about another token, such as a late one from a
    /// page the app has since replaced, changes nothing.
    func signedOut(of server: URL, tokenID: String?) async {
        guard let session = session(for: server), tokenID == nil || session.tokenID == tokenID else { return }
        do {
            try await store(for: server).signOut(session)
        } catch {
            // Neither kept to revoke nor revoked: it stays in the Keychain for
            // the next sign-in to retire, and is never handed out again.
            ended.insert(server)
        }
        changed(server)
        await revokePending(on: [server])
    }

    /// The page renamed the repository the app's sign-in to `server` opens
    /// (decision 0287): the sign-in keeps its token and takes the new name,
    /// which the page reads at its next load and every credential change
    /// sends. A message about another token, such as a late one from a page
    /// the app has since replaced, changes nothing. Answers the sign-in as it
    /// stands after, nil where nothing changed or the Keychain kept the old.
    func renamed(on server: URL, tokenID: String?, repository: String) async -> Session? {
        guard var session = session(for: server), tokenID == nil || session.tokenID == tokenID,
            session.repository != repository
        else { return nil }
        session.repository = repository
        do {
            try await store(for: server).save(session)
        } catch {
            return nil
        }
        changed(server)
        return session
    }

    /// Signs out of `server` and forgets it: the sign-in revoked, the page's
    /// own sign-in door and the server's console state dropped.
    func forget(_ server: URL) async {
        if let session = await store(for: server).load() {
            try? await store(for: server).signOut(session)
        }
        setPageSignIn(false, for: server)
        ended.remove(server)
        changed(server)
        await revokePending(on: [server])
        if (try? await store(for: server).pendingRevocations())?.isEmpty == true {
            known.removeAll { $0 == server }
            defaults.set(known.map(\.absoluteString), forKey: Self.knownKey)
        }
    }

    /// Tokens the app stopped using that their server has not revoked yet;
    /// each is tried again at every start.
    var unrevoked: [Session] {
        known.flatMap { (try? store(for: $0).pendingRevocations()) ?? [] }
    }

    func revokePending(on servers: [URL]? = nil) async {
        for server in servers ?? known {
            _ = try? await store(for: server).revokePending()
        }
        revision += 1
    }

    private func changed(_ server: URL) {
        cache[server] = nil
        revision += 1
    }

    private func remember(_ server: URL) {
        known = [server] + known.filter { $0 != server }
        defaults.set(known.map(\.absoluteString), forKey: Self.knownKey)
    }

    /// Drops `server` from the known list when nothing is held for it: no
    /// sign-in, no page sign-in and no token waiting to be revoked, as after
    /// a sign-in that failed before the server minted anything.
    private func forgetIfUnused(_ server: URL) {
        guard store(for: server).read() == .missing, !pageSignIn.contains(server.absoluteString),
            (try? store(for: server).pendingRevocations())?.isEmpty == true
        else { return }
        known.removeAll { $0 == server }
        defaults.set(known.map(\.absoluteString), forKey: Self.knownKey)
    }

    private func setPageSignIn(_ on: Bool, for server: URL) {
        if on { pageSignIn.insert(server.absoluteString) } else { pageSignIn.remove(server.absoluteString) }
        defaults.set(Array(pageSignIn).sorted(), forKey: Self.pageSignInKey)
    }
}
