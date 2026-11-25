import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore

/// What Substrate Chat asks a substrate for, and nothing it sets up: `full`
/// (decision 0204). A scoped token talks only to an agent whose reads its
/// grant covers, and no scope covers the core and llm kinds, so the owner's
/// assistant, which reads every kind, is out of a scoped app's reach; and
/// only a full-access token reads which provider keys the operator supplies
/// (decision 0235). A session an earlier build kept with scopes still works
/// with what it holds, and the status check asks the owner to approve
/// `full`.
public enum ChatManifest {
    /// Substrate Chat's own name.
    public static let ownName = "Substrate Chat"

    /// The name of the app the chat runs in, as its words say it ("Keep
    /// Substrate Chat open until it is sent."): Substrate Chat, or the app
    /// that shows a conversation of its own, which says its name once
    /// before it makes the chat (`runs(in:)`).
    public static var appName: String { host.name }

    /// An app that embeds a conversation says its own name: the chat's
    /// words then name that app, never Substrate Chat. One name for the
    /// process: an app is one app.
    public static func runs(in app: String) { host.name = app }

    private static let host = Host()

    /// The process's one name for the app, read from any thread.
    private final class Host: @unchecked Sendable {
        private let lock = NSLock()
        private var kept = ChatManifest.ownName

        var name: String {
            get { lock.withLock { kept } }
            set { lock.withLock { kept = newValue } }
        }
    }

    /// The Info.plist key a release stamps with the URL of its client ID
    /// metadata document (`SUBSTRATE_CHAT_CLIENT_ID`).
    public static let clientIDKey = "SubstrateChatClientID"
    /// A local build's client id: a loopback one, which nothing fetches.
    public static let localClientID = "http://127.0.0.1/substrate-chat"
    /// The Info.plist key a release stamps with the https redirect it claims
    /// through its associated domain (`SUBSTRATE_CHAT_REDIRECT`).
    public static let redirectKey = "SubstrateChatRedirect"

    public static let scopes = [ScopeWords.full]

    /// What every build asked for before the request was kept in the
    /// session (`AppModel.asked`). Frozen: a scope added later is one such a
    /// session was never asked for.
    public static let earliestRequest = ["agents:*", "decide:*", "read:substrate.reamde.dev/llm/provider", "read:*"]

    /// A server whose metadata lacks `full` refuses this request with an
    /// error page, so it is too old rather than a browser away; one with no
    /// app authorization at all takes the password.
    public static let requirement = ServerRequirement(minimumVersion: "0.114.0", scopeVerbs: [ScopeWords.full])

    public static let manifest = AppManifest(
        appName: ownName,
        clientID: AppManifest.clientID(infoKey: clientIDKey, localDefault: localClientID),
        scopes: scopes,
        requirement: requirement,
        claimedRedirect: AppManifest.claimedRedirect(infoKey: redirectKey),
        passwordFallback: .withoutAppAuthorization)

    /// An app session an earlier build kept names no request: every such
    /// build asked for `earliestRequest`, so a scope it holds out of that
    /// was left out by the owner, and is never asked about again.
    public static func asked(_ session: Session) -> Session {
        guard session.isApp, session.requestedScopes == nil else { return session }
        var asked = session
        asked.requestedScopes = earliestRequest
        return asked
    }

    /// What the record views tell a reader the token may not read for:
    /// where access is granted. A password sign-in reaches everything.
    public static func deniedHint(_ grants: Grants?) -> String? {
        grants?.full == false ? "\(appName) wasn't approved to read these. Allow it with Settings → Change Access…" : nil
    }
}
