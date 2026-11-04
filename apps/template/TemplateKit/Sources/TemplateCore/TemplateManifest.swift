import Foundation
import SubstrateAPI
import SubstrateAuth

/// What Substrate Template asks a substrate for (app.json, which a test
/// holds this to): its package as a `package` detail at the version this
/// build was written against or later, which the approval installs from the
/// publisher's authority document, and `write:` on its kinds, which reads
/// them too.
public enum TemplateManifest {
    public static let clientIDKey = "SubstrateTemplateClientID"
    /// A local build's client id: a loopback one, which nothing fetches.
    public static let localClientID = "http://127.0.0.1/substrate-template"
    public static let redirectKey = "SubstrateTemplateRedirect"

    public static let scopes = ["write:\(TemplateKinds.packageID)/*"]

    /// The package's version this build was written against: its floor.
    public static let version = 1

    public static let details = [AuthorizationDetail.package(TemplateKinds.packageID, atLeast: version)]

    public static let requirement = ServerRequirement(scopeVerbs: ["write"], detailTypes: [AuthorizationDetail.Types.package])

    /// The package each build needs, at its version, as the link checks it.
    public static var packages: [String: Int] { [TemplateKinds.packageID: version] }

    public static func manifest(clientID: String, claimedRedirect: URL? = nil, softwareVersion: String = AppManifest.softwareVersion())
        -> AppManifest
    {
        AppManifest(
            appName: TemplateApp.appName, clientID: clientID, scopes: scopes, details: details, requirement: requirement,
            claimedRedirect: claimedRedirect, passwordFallback: .never, names: [TemplateKinds.packageID: "Template"],
            kindLabels: [TemplateKinds.item: KindLabel(singular: "Item", plural: "Items")], softwareVersion: softwareVersion)
    }

    /// This build's manifest: a release's stamped client id and claimed
    /// redirect, else the local client id and the loopback receiver.
    public static var current: AppManifest {
        manifest(
            clientID: AppManifest.clientID(infoKey: clientIDKey, localDefault: localClientID),
            claimedRedirect: AppManifest.claimedRedirect(infoKey: redirectKey))
    }
}
