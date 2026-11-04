import Foundation
import SubstrateAPI
import SubstrateAuth

/// What Substrate Tasks asks a substrate for (apps/tasks/app.json, which a
/// test holds this to): its package as a `package` detail at the version
/// this build carries or later, which the approval installs from the
/// publisher's authority document, and `write:` on its kinds, which reads
/// them too. The app applies no vocabulary.
public enum TasksManifest {
    public static let clientIDKey = "SubstrateTasksClientID"
    /// A local build's client id: a loopback one, which nothing fetches.
    public static let localClientID = "http://127.0.0.1/substrate-tasks"
    public static let redirectKey = "SubstrateTasksRedirect"

    public static let scopes = ["write:\(TasksKinds.packageID)/*"]

    /// The version this build was written against: the package's floor. A
    /// build whose resources cannot be read asks for version 1.
    public static let version = (try? TasksPackage.bundled())?.version ?? 1

    public static let details = [AuthorizationDetail.package(TasksKinds.packageID, atLeast: version)]

    public static let requirement = ServerRequirement(scopeVerbs: ["write"], detailTypes: [AuthorizationDetail.Types.package])

    /// The package each build needs, at its version, as the link checks it.
    public static var packages: [String: Int] { [TasksKinds.packageID: version] }

    /// - Parameter version: the package's floor, where the build read its
    ///   closure from a bundle of its own (Android's copied-out resources).
    public static func manifest(
        clientID: String, claimedRedirect: URL? = nil, softwareVersion: String = AppManifest.softwareVersion(),
        version: Int = version
    ) -> AppManifest {
        AppManifest(
            appName: TasksApp.appName, clientID: clientID, scopes: scopes,
            details: [AuthorizationDetail.package(TasksKinds.packageID, atLeast: version)], requirement: requirement,
            claimedRedirect: claimedRedirect, passwordFallback: .never, names: [TasksKinds.packageID: "Tasks"],
            kindLabels: [
                TasksKinds.task: KindLabel(singular: "Task", plural: "Tasks"),
                TasksKinds.list: KindLabel(singular: "Task list", plural: "Task lists"),
            ],
            softwareVersion: softwareVersion)
    }

    /// This build's manifest: a release's stamped client id and claimed
    /// redirect, else the local client id and the loopback receiver.
    public static var current: AppManifest {
        manifest(
            clientID: AppManifest.clientID(infoKey: clientIDKey, localDefault: localClientID),
            claimedRedirect: AppManifest.claimedRedirect(infoKey: redirectKey))
    }
}
