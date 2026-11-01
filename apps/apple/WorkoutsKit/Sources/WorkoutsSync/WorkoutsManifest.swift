import Foundation
import SubstrateKit
import WorkoutsCore

/// What Substrate Workouts asks a substrate for (decision 0203):
///
/// - `declare:~/training`, to install and upgrade its own package, the
///   training kinds, under the repository's own authority;
/// - `write:~/training/*`, to read and write its exercises, routines,
///   sessions and sets;
/// - `read:` the Health workout and the Apple Health account, to find the
///   row a finished session was saved to Apple Health as;
/// - and, set up on approval, `substrate.reamde.dev/health`, the catalogue
///   the package requires.
public enum WorkoutsManifest {
    public static let appName = "Substrate Workouts"
    /// The Info.plist key a release stamps with the URL of its client ID
    /// metadata document (`SUBSTRATE_WORKOUTS_CLIENT_ID`).
    public static let clientIDKey = "SubstrateWorkoutsClientID"
    /// A local build's client id: a loopback one, which nothing fetches.
    public static let localClientID = "http://127.0.0.1/substrate-workouts"

    public static let healthPackage = "substrate.reamde.dev/health"
    public static let healthWorkout = "substrate.reamde.dev/health/workout"
    public static let healthAccount = "providers.substrate.reamde.dev/applehealth/account"

    public static let declareScope = "declare:\(KindNames.packageID(authority: KindNames.home))"
    public static let writeScope = "write:\(KindNames.packageID(authority: KindNames.home))/*"

    public static let scopes = [declareScope, writeScope, "read:\(healthWorkout)", "read:\(healthAccount)"]

    /// The release of the health catalogue whose `workout` the package's
    /// `session.workout` names.
    public static let details = [AuthorizationDetail.package(healthPackage, atLeast: 1)]

    /// A server that lists `declare` and `package` details can take the
    /// request, whatever it calls its build; one that cannot is too old.
    public static let requirement = ServerRequirement(scopeVerbs: ["declare", "write"], detailTypes: [AuthorizationDetail.Types.package])

    public static func manifest(clientID: String) -> AppManifest {
        AppManifest(
            appName: appName,
            clientID: clientID,
            scopes: scopes,
            details: details,
            requirement: requirement,
            passwordFallback: .never,
            names: [healthPackage: "Health"],
            kindLabels: [
                healthWorkout: KindLabel(singular: "Workout", plural: "Workouts"),
                healthAccount: KindLabel(singular: "Apple Health account", plural: "Apple Health accounts", purpose: "internal"),
            ])
    }

    /// This build's manifest: a release's stamped client id, else the local one.
    public static var current: AppManifest {
        manifest(clientID: AppManifest.clientID(infoKey: clientIDKey, localDefault: localClientID))
    }
}

/// Where the app stands with its substrate, from what it knows: the
/// session, the package and the last flush. One value, which Settings and
/// the banner on Routines both say.
public enum SyncStatus: Sendable, Hashable {
    /// No server: the app runs on the routines it carries and keeps every
    /// session on the phone.
    case local
    /// Signed in; the package is being checked or installed.
    case preparing
    /// The token was refused or its access ended: sign in again.
    case signedOut
    /// The token lacks a scope the app needs, such as `declare:` when the
    /// owner left it out: the review asks again.
    case needsApproval
    /// The server cannot take what the app needs: update it.
    case serverTooOld
    /// The phone could not keep a change, or its queue, on disk.
    case notKept(String)
    /// The repository holds a newer training package than this build knows.
    case packageNewer(held: Int, known: Int)
    /// The owner changed these kinds of an older package: the upgrade waits
    /// for them to choose.
    case packageEdited(kinds: [String])
    /// The server refused the upgrade, which would remove what the package
    /// holds.
    case packageRefused(String)
    /// Ready, with this many writes still waiting.
    case ready(waiting: Int)
    /// Ready, and the approved access ends on this date.
    case accessEndsSoon(Date, waiting: Int)
    /// The server could not be reached; writes wait.
    case offline(waiting: Int, reason: String)
    /// The server refused a write, which waits, parked, with its words.
    case refused(waiting: Int, reason: String)
    /// Something else stopped the package check.
    case failed(String)

    public static func derive(signedIn: Bool, package: PackageState?, outbox: OutboxState, waiting: Int, known: Int,
                              issues: [AppIssue] = [], notKept: String? = nil) -> SyncStatus
    {
        guard signedIn else { return .local }
        if issues.contains(.accessEnded) { return .signedOut }
        switch outbox {
        case .signedOut: return .signedOut
        case .needsApproval: return .needsApproval
        default: break
        }
        if issues.contains(where: { if case .needsApproval = $0 { true } else { false } }) { return .needsApproval }
        if issues.contains(where: { if case .serverTooOld = $0 { true } else { false } }) { return .serverTooOld }
        if let notKept { return .notKept(notKept) }
        guard let package else { return .preparing }
        switch package {
        case let .newer(held): return .packageNewer(held: held, known: known)
        case let .edited(_, kinds): return .packageEdited(kinds: kinds)
        case let .refused(_, reason): return .packageRefused(reason)
        default: break
        }
        guard package.writable else { return .preparing }
        switch outbox {
        case let .offline(reason): return .offline(waiting: waiting, reason: reason)
        case let .refused(reason): return .refused(waiting: waiting, reason: reason)
        default: break
        }
        for case let .accessEndsSoon(date) in issues {
            return .accessEndsSoon(date, waiting: waiting)
        }
        return .ready(waiting: waiting)
    }

    /// One sentence for Settings.
    public var sentence: String {
        switch self {
        case .local:
            "Not signed in. Routines and exercises come with this app, and sessions stay on this iPhone."
        case .preparing:
            "Checking the training package on your substrate."
        case .signedOut:
            "Your access ended. Sign in again to send what waits."
        case .needsApproval:
            "Substrate Workouts needs your approval for more access. Review it to send what waits."
        case .serverTooOld:
            "Your substrate needs an update before Substrate Workouts can use it. Sessions wait on this iPhone until then."
        case let .notKept(reason):
            "A change could not be kept on this iPhone: \(reason)"
        case let .packageEdited(kinds):
            "The training package on your substrate was changed there (\(kinds.joined(separator: ", "))). This app's update would apply over those changes, so it waits for you."
        case let .packageRefused(reason):
            "Your substrate refused this app's update to the training package: \(reason) Sessions wait on this iPhone until the package is settled on your substrate."
        case let .accessEndsSoon(date, waiting):
            "Access ends \(date.formatted(date: .abbreviated, time: .omitted)). Renew it to keep syncing." + (waiting == 0 ? "" : " Sending \(waiting) \(waiting == 1 ? "change" : "changes").")
        case let .packageNewer(held, known):
            "Your substrate has version \(held) of the training package, newer than the version \(known) this app knows. Update the app; sessions wait on this iPhone until then."
        case let .ready(waiting):
            waiting == 0 ? "Up to date." : "Sending \(waiting) \(waiting == 1 ? "change" : "changes")."
        case let .offline(waiting, _):
            "Your substrate cannot be reached. \(waiting) \(waiting == 1 ? "change waits" : "changes wait") on this iPhone."
        case let .refused(waiting, reason):
            "Your substrate refused a change: \(reason) \(waiting) \(waiting == 1 ? "change waits" : "changes wait"); retry or discard it below."
        case let .failed(reason):
            "The training package could not be checked: \(reason)"
        }
    }
}
