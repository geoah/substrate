import Foundation
import SubstrateKit

/// What Substrate Sync asks a substrate for (docs/plans/app-setup.md): to
/// write the apple package's kinds, and to read the Health accounts and the
/// receipts of its deliveries; and, set up by the owner's approval, the two
/// packages at the versions it writes, this phone's Health account and the
/// webhook it delivers to, whose `webhook:` scope the approval adds. A server
/// that cannot set those up gets the password sign-in and Set Up instead.
enum SyncManifest {
    static let appName = "Substrate Sync"
    /// The Info.plist key a release stamps with the URL of its client ID
    /// metadata document (`SUBSTRATE_SYNC_CLIENT_ID`).
    static let clientIDKey = "SubstrateSyncClientID"
    /// A local build's client id: a loopback one, which nothing fetches.
    static let localClientID = "http://127.0.0.1/substrate-sync"

    static let scopes = [
        "write:\(SyncEngine.appleKinds)*",
        "read:\(SyncEngine.healthAccountKind)",
        "read:\(SyncEngine.healthExportKind)",
    ]

    /// The first release whose approval sets up packages, an account and a
    /// webhook. A build between releases passes when its metadata lists the
    /// verbs and the types, and a release below it is too old even when it
    /// does.
    static let requirement = ServerRequirement(minimumVersion: "0.114.0", scopeVerbs: ["read", "write"],
                                               detailTypes: AuthorizationDetail.Types.all)

    /// The manifest for this phone: its Health account, named for the
    /// install, with the device's name and time zone as the approval creates
    /// it. Built per use, since the name and the zone can change.
    static func make(account: String, deviceName: String, timeZone: String, bundle: Bundle = .main) -> AppManifest {
        AppManifest(
            appName: appName,
            clientID: AppManifest.clientID(infoKey: clientIDKey, localDefault: localClientID, bundle: bundle),
            scopes: scopes,
            details: [
                .package(AppModel.appleProvider, atLeast: AppModel.appleVersion),
                .package(AppModel.healthProvider, atLeast: AppModel.healthVersion),
                .account(kind: SyncEngine.healthAccountKind, id: account,
                         properties: ["displayName": .string(deviceName), "timezone": .string(timeZone)]),
                .webhook(trigger: SyncEngine.trigger),
            ],
            requirement: requirement,
            passwordFallback: .belowRequirement)
    }
}
