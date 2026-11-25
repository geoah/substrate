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
    public static let appName = "Substrate Chat"
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
        appName: appName,
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
        grants?.full == false ? "Substrate Chat wasn't approved to read these. Allow it with Settings → Change Access…" : nil
    }
}
