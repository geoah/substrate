import Foundation
import SubstrateLocal
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import WorkoutsCore

/// What Substrate Workouts asks a substrate for (decision record 0214):
///
/// - `package` details on `substrate.reamde.dev/health` (the catalogue
///   training requires), `substrate.reamde.dev/training` and
///   `substrate.reamde.dev/trainingcoach`, each at the version this build
///   was written against or later; the server installs them from the
///   binary's catalog and the publisher's authority document, and holds a
///   later upgrade that would break this build while it is connected;
/// - `write:substrate.reamde.dev/training/*`, to read and write its
///   exercises, routines, sessions and sets;
/// - `read:` the Health workout and the Apple Health account, to find the
///   row a finished session was saved to Apple Health as;
/// - `agents:substrate.reamde.dev/trainingcoach/*`, to talk to the coach;
/// - and every read the coach's own reach needs (decision record 0215: a
///   package's code reaches no further than the approved scopes, or the
///   row is blocked `reaches-past-scopes`), which `ClosureReach` computes
///   from the closure this build carries and a test holds the list to.
///
/// The app applies no vocabulary and respells no kind: the packages are
/// published, under one name in every repository.
public enum WorkoutsManifest {
    public static let appName = "Substrate Workouts"
    /// The Info.plist key a release stamps with the URL of its client ID
    /// metadata document (`SUBSTRATE_WORKOUTS_CLIENT_ID`).
    public static let clientIDKey = "SubstrateWorkoutsClientID"
    /// A local build's client id: a loopback one, which nothing fetches.
    public static let localClientID = "http://127.0.0.1/substrate-workouts"
    /// The Info.plist key a release stamps with the https redirect it
    /// claims through its associated domain (`SUBSTRATE_WORKOUTS_REDIRECT`).
    public static let redirectKey = "SubstrateWorkoutsRedirect"

    public static let healthPackage = "substrate.reamde.dev/health"
    public static let healthWorkout = "substrate.reamde.dev/health/workout"
    public static let healthSleep = "substrate.reamde.dev/health/sleep"
    public static let healthAccount = "providers.substrate.reamde.dev/applehealth/account"

    public static let writeScope = "write:\(WorkoutsKinds.packageID)/*"

    /// The coach's reads outside training, as its closure declares them
    /// (apps/workouts/scripts/workouts-package.py, COACH_READS): the two health
    /// kinds, every observation kind, and the clinical catalogue where the
    /// owner holds one.
    public static let coachReadScopes = [
        "read:\(healthWorkout)",
        "read:\(healthSleep)",
        "read:substrate.reamde.dev/observations/*",
        "read:substrate.reamde.dev/clinical/*",
    ]

    public static let scopes = [writeScope, "read:\(healthAccount)"] + coachReadScopes + [Coach.agentsScope]

    /// The versions this build was written against: each package's floor.
    /// A build whose resources cannot be read asks for version 1, which a
    /// test refuses before a release.
    public static let trainingVersion = (try? PackageManifest.bundled())?.version ?? 1
    public static let coachVersion = (try? PackageManifest.coach())?.version ?? 1

    /// The health catalogue first, which training requires; then the two
    /// published packages, in the order they land.
    public static let details = [
        AuthorizationDetail.package(healthPackage, atLeast: 1),
        AuthorizationDetail.package(WorkoutsKinds.packageID, atLeast: trainingVersion),
        AuthorizationDetail.package(Coach.package, atLeast: coachVersion),
    ]

    /// A server that takes `package` details and the `agents` verb can take
    /// the request, whatever it calls its build; one that cannot is too old.
    public static let requirement = ServerRequirement(scopeVerbs: ["write", "agents"], detailTypes: [AuthorizationDetail.Types.package])

    /// The training kinds' labels and purposes as `package/` declares them,
    /// so the approval and a refused write say "Training session" and
    /// "Workout routine", never the word built from the kind's name.
    public static let trainingLabels: [String: KindLabel] = {
        let kinds = WorkoutsKinds.v1
        return [
            kinds.session: KindLabel(singular: "Training session", plural: "Training sessions"),
            kinds.set: KindLabel(singular: "Set", plural: "Sets", purpose: "supporting"),
            kinds.routine: KindLabel(singular: "Workout routine", plural: "Workout routines"),
            kinds.exercise: KindLabel(singular: "Exercise", plural: "Exercises"),
            kinds.movement: KindLabel(singular: "Movement", plural: "Movements", purpose: "supporting"),
            kinds.setup: KindLabel(singular: "Exercise setup", plural: "Exercise setups", purpose: "supporting"),
            kinds.library: KindLabel(singular: "Seeded library", plural: "Seeded libraries", purpose: "internal"),
            kinds.bodyMeasurement: KindLabel(singular: "Body measurement", plural: "Body measurements"),
            "\(WorkoutsKinds.packageID)/memory": KindLabel(singular: "Coach memory", plural: "Coach memories"),
        ]
    }()

    public static func manifest(clientID: String, claimedRedirect: URL? = nil, softwareVersion: String = AppManifest.softwareVersion()) -> AppManifest {
        AppManifest(
            appName: appName,
            clientID: clientID,
            scopes: scopes,
            details: details,
            requirement: requirement,
            claimedRedirect: claimedRedirect,
            passwordFallback: .never,
            names: [healthPackage: "Health", WorkoutsKinds.packageID: "Training", Coach.package: "Workout coach"],
            kindLabels: trainingLabels.merging([
                healthWorkout: KindLabel(singular: "Workout", plural: "Workouts"),
                healthSleep: KindLabel(singular: "Sleep", plural: "Sleep"),
                healthAccount: KindLabel(singular: "Account", plural: "Accounts", purpose: "internal"),
            ]) { training, _ in training },
            softwareVersion: softwareVersion)
    }

    /// This build's manifest: a release's stamped client id and claimed
    /// redirect, else the local client id and the loopback receiver.
    public static var current: AppManifest {
        manifest(clientID: AppManifest.clientID(infoKey: clientIDKey, localDefault: localClientID),
                 claimedRedirect: AppManifest.claimedRedirect(infoKey: redirectKey))
    }
}
