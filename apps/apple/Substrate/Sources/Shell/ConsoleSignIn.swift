import Foundation
import SubstrateKit

/// The Console app's sign-in (decision 0204): the kit's app authorization
/// flow asking for `full`, since the app is the whole console. A server that
/// cannot grant it keeps the console's own sign-in page (`passwordFallback`).
enum ConsoleSignIn {
    /// The Info key a release stamps with the URL of the app's client ID
    /// metadata document (`SUBSTRATE_CONSOLE_CLIENT_ID`).
    static let clientIDKey = "SubstrateConsoleClientID"
    /// A local build's client id: a loopback one, which the approval page
    /// says it cannot verify.
    static let localClientID = "http://127.0.0.1/substrate-console"

    static let manifest = AppManifest(
        appName: "Substrate",
        clientID: AppManifest.clientID(infoKey: clientIDKey, localDefault: localClientID),
        scopes: [ScopeWords.full],
        passwordFallback: .belowRequirement)

    struct NoTokenID: LocalizedError {
        let server: URL
        var errorDescription: String? {
            "\(ServerAddress.display(server)) didn't say which token it made. Update the server and try again."
        }
    }

    /// The session for a server a probe found. The page signs out by
    /// deleting its token record, so a session without the record's id is
    /// one it could never end: it is revoked and refused.
    @MainActor
    static func session(_ probe: ServerProbe.Result) async throws -> Session {
        guard let oauth = OAuthClient(manifest: manifest, probe: probe) else { throw URLError(.badServerResponse) }
        let session = try await oauth.signIn(server: probe.origin)
        guard session.tokenID != nil else {
            try? await session.revoke()
            throw NoTokenID(server: probe.origin)
        }
        return session
    }
}
