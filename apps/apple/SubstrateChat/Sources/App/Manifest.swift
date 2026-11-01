import Foundation
import SubstrateKit

/// What Substrate Chat asks a substrate for (decisions 0194 and 0195), and
/// nothing it sets up: `agents:*` talks to the agents (their chats, questions
/// and turns), `decide:*` applies or dismisses what they suggest, the
/// provider rows say whether an agent can answer at all, and `read:*` opens
/// the records a chat links to. The owner may leave any of them out on the
/// approval page; a pill the app may not read still names its record from
/// the chat.
enum ChatManifest {
    static let appName = "Substrate Chat"
    /// The Info.plist key a release stamps with the URL of its client ID
    /// metadata document (`SUBSTRATE_CHAT_CLIENT_ID`).
    static let clientIDKey = "SubstrateChatClientID"
    /// A local build's client id: a loopback one, which nothing fetches.
    static let localClientID = "http://127.0.0.1/substrate-chat"

    static let scopes = ["agents:*", "decide:*", "read:\(Kinds.provider)", "read:*"]

    /// What every build asked for before the request was kept beside the
    /// session. Frozen: a scope added later is one such a session was never
    /// asked for.
    static let earliestRequest = ["agents:*", "decide:*", "read:substrate.reamde.dev/llm/provider", "read:*"]

    /// A server whose metadata lacks the agent verbs refuses this request
    /// with an error page, so it is too old rather than a browser away; one
    /// with no app authorization at all takes the password.
    static let requirement = ServerRequirement(minimumVersion: "0.114.0", scopeVerbs: ["agents", "decide"])

    static let manifest = AppManifest(
        appName: appName,
        clientID: AppManifest.clientID(infoKey: clientIDKey, localDefault: localClientID),
        scopes: scopes,
        requirement: requirement,
        passwordFallback: .withoutAppAuthorization)

    /// The request the status check asks about for an app session: every
    /// scope but those the last approval asked for and the owner left out,
    /// which are theirs to add from Settings and never a reason to ask
    /// again. A scope a later build adds is asked about.
    static func checked(session: Session, requested: [String]?) -> AppManifest {
        guard let held = session.scopes else { return manifest }
        let asked = requested ?? earliestRequest
        var checked = manifest
        checked.scopes = scopes.filter { held.contains($0) || !asked.contains($0) }
        return checked
    }
}
