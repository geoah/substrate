import Foundation
import SubstrateCore
import SubstrateAPI
import SubstrateAuth

/// What Substrate Today asks a substrate for (app.json, which a test
/// holds this to): `full`, alone, as Substrate Chat does. The pane reads
/// across every package and talks to the assistant, which reads everything,
/// so no narrower request reaches it; a `full` request carries no package
/// details, so the app sets nothing up and publishes nothing.
public enum TodayManifest {
    public static let clientIDKey = "SubstrateTodayClientID"
    /// A local build's client id: a loopback one, which nothing fetches.
    public static let localClientID = "http://127.0.0.1/substrate-today"
    public static let redirectKey = "SubstrateTodayRedirect"

    public static let scopes = [ScopeWords.full]

    /// A server whose metadata lacks `full` refuses this request with an
    /// error page, so it is too old rather than a browser away.
    public static let requirement = ServerRequirement(minimumVersion: "0.114.0", scopeVerbs: [ScopeWords.full])

    /// A `full` request sets up no package, so the link checks none.
    public static var packages: [String: Int] { [:] }

    public static func manifest(clientID: String, claimedRedirect: URL? = nil, softwareVersion: String = AppManifest.softwareVersion())
        -> AppManifest
    {
        AppManifest(
            appName: TodayApp.appName, clientID: clientID, scopes: scopes, requirement: requirement,
            claimedRedirect: claimedRedirect, passwordFallback: .never, names: [TodayKinds.packageID: "Today"],
            kindLabels: [TodayKinds.item: KindLabel(singular: "Item", plural: "Items")], softwareVersion: softwareVersion)
    }

    /// This build's manifest: a release's stamped client id and claimed
    /// redirect, else the local client id and the loopback receiver.
    public static var current: AppManifest {
        manifest(
            clientID: AppManifest.clientID(infoKey: clientIDKey, localDefault: localClientID),
            claimedRedirect: AppManifest.claimedRedirect(infoKey: redirectKey))
    }
}
