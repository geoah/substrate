import SubstrateKit
import WebKit

/// The console's sign-in, done by the app (decision 0204): the substrate's
/// own sign-in and approval page in Safari's sheet, or the default browser on
/// a Mac, asking for `full`, and the session handed back to the page that
/// asked (`shellSignIn` in `web/console/src/lib/shell.ts`). The page keeps
/// the session as it keeps a password sign-in's; the app keeps nothing.
@MainActor
final class ConsoleSignIn: NSObject, WKScriptMessageHandlerWithReply {
    static let handlerName = "substrateShellSignIn"
    /// The Info key a release stamps with the URL of the app's client ID
    /// metadata document (`SUBSTRATE_CONSOLE_CLIENT_ID`).
    static let clientIDKey = "SubstrateConsoleClientID"
    /// A local build's client id: a loopback one, which the approval page
    /// says it cannot verify.
    static let localClientID = "http://127.0.0.1/substrate-console"

    static let manifest = AppManifest(
        appName: "Substrate",
        clientID: AppManifest.clientID(infoKey: clientIDKey, localDefault: localClientID),
        scopes: [ScopeWords.full])

    /// What the page is answered: the session, or why there is none.
    enum Outcome: Sendable {
        case signedIn(secret: String, repository: String, tokenID: String)
        case cancelled
        case failed(String)

        var reply: [String: Any] {
            switch self {
            case let .signedIn(secret, repository, tokenID):
                ["secret": secret, "repository": repository, "tokenId": tokenID]
            case .cancelled: ["cancelled": true]
            case let .failed(text): ["error": text]
            }
        }
    }

    private let server: URL
    private var running: Task<Outcome, Never>?

    init(server: URL) {
        self.server = server
    }

    func userContentController(
        _ controller: WKUserContentController, didReceive message: WKScriptMessage
    ) async -> (Any?, String?) {
        // A second ask replaces the first, whose browser tab will never answer.
        running?.cancel()
        let task = Task { await signIn() }
        running = task
        let outcome = await task.value
        if running == task { running = nil }
        return (outcome.reply, nil)
    }

    /// Stops a sign-in waiting on the browser.
    func cancel() {
        running?.cancel()
    }

    private func signIn() async -> Outcome {
        let manifest = Self.manifest
        do {
            let probe = try await ServerProbe().probe(server, requirement: manifest.requirement, app: manifest.appName)
            guard probe.verdict.isCompatible, let oauth = OAuthClient(manifest: manifest, probe: probe) else {
                return .failed("\(ServerAddress.display(server)) can't sign this app in yet: it needs an update first.")
            }
            let session = try await oauth.signIn(server: probe.origin)
            // The page signs out by deleting its token record, so a session
            // without the record's id is one it could never end.
            guard let tokenID = session.tokenID else {
                try? await session.revoke()
                return .failed("\(ServerAddress.display(server)) didn't say which token it made. Update the server and try again.")
            }
            return .signedIn(secret: session.secret, repository: session.repository, tokenID: tokenID)
        } catch is WebAuthenticator.Cancelled {
            return .cancelled
        } catch where isCancellation(error) {
            return .cancelled
        } catch {
            return .failed(explain(error, app: manifest.appName, server: server))
        }
    }
}
