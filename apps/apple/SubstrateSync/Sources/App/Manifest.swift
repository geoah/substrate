import Foundation
import SubstrateKit

/// What Substrate Sync asks a substrate for (docs/plans/app-setup.md): to
/// write the apple package's kinds; and, set up by the owner's approval, the
/// two packages at the versions it writes, this phone's Health account and
/// the webhook it delivers to, whose `webhook:` scope the approval adds.
///
/// The applehealth package ships code, and a package an approval installs
/// reaches no further than the approved scopes (decision 0215), or the
/// server blocks its row `reaches-past-scopes`. So the request also carries
/// every read and write that code needs, which `ReachTests` holds to the
/// closures in kinds/providers.substrate.reamde.dev/: the ingest function
/// writes every applehealth kind, the account and the deliveries included
/// (internal kinds, which a glob never reaches, so each is named), and the
/// adapter writes health sleep and workouts and the observations kinds. A
/// server that cannot set those up gets the password sign-in and Set Up
/// instead.
enum SyncManifest {
    static let appName = "Substrate Sync"
    /// The Info.plist key a release stamps with the URL of its client ID
    /// metadata document (`SUBSTRATE_SYNC_CLIENT_ID`).
    static let clientIDKey = "SubstrateSyncClientID"
    /// A local build's client id: a loopback one, which nothing fetches.
    static let localClientID = "http://127.0.0.1/substrate-sync"
    /// The Info.plist key a release stamps with the https redirect it claims
    /// through its associated domain (`SUBSTRATE_SYNC_REDIRECT`).
    static let redirectKey = "SubstrateSyncRedirect"

    static let scopes = [
        "write:\(SyncEngine.appleKinds)*",
        "write:\(SyncEngine.healthAccountKind)",
        "write:\(SyncEngine.healthExportKind)",
        "write:\(AppModel.healthProvider)/*",
        "write:substrate.reamde.dev/health/sleep",
        "write:substrate.reamde.dev/health/workout",
        "write:substrate.reamde.dev/observations/*",
    ]

    /// The labels and purposes of the kinds Sync's scopes reach, as the
    /// apple, applehealth, health and observations packages declare them
    /// (a kind without a label reads as its name), so the review names what
    /// the approval page will: the kinds a glob reaches by their labels, the
    /// supporting ones counted, the internal ones named one by one.
    static let kindLabels: [String: KindLabel] = {
        var labels = [
            SyncEngine.healthAccountKind: KindLabel(singular: "Account", plural: "Accounts", purpose: "internal"),
            SyncEngine.healthExportKind: KindLabel(singular: "Delivery", plural: "Deliveries", purpose: "internal"),
            "substrate.reamde.dev/health/sleep": KindLabel(singular: "Sleep", plural: "Sleep"),
            "substrate.reamde.dev/health/workout": KindLabel(singular: "Workout", plural: "Workouts"),
            "substrate.reamde.dev/observations/metric": KindLabel(singular: "Metric", plural: "Metrics"),
            "substrate.reamde.dev/observations/observation": KindLabel(singular: "Observation", plural: "Observations", purpose: "supporting"),
            "substrate.reamde.dev/observations/source": KindLabel(singular: "Source", plural: "Sources"),
            "substrate.reamde.dev/observations/sourcelink": KindLabel(singular: "Source link", plural: "Source links", purpose: "supporting"),
            "substrate.reamde.dev/observations/trace": KindLabel(singular: "Trace", plural: "Traces", purpose: "supporting"),
        ]
        let health: [(String, String, String, String?)] = [
            ("metricbatch", "Metric batch", "Metric batches", "supporting"),
            ("workout", "Workout", "Workouts", nil),
            ("stateofmind", "State of mind", "States of mind", nil),
            ("medication", "Medication", "Medications", nil),
            ("symptom", "Symptom", "Symptoms", nil),
            ("cycleevent", "Cycle tracking entry", "Cycle tracking entries", nil),
            ("ecg", "ECG", "ECGs", nil),
            ("heartratenotification", "Heart rate notification", "Heart rate notifications", nil),
        ]
        for (name, singular, plural, purpose) in health {
            labels["\(AppModel.healthProvider)/\(name)"] = KindLabel(singular: singular, plural: plural, purpose: purpose)
        }
        let apple: [(String, String, String, String?)] = [
            ("addressbook", "Address book", "Address books", "supporting"),
            ("contact", "Contact", "Contacts", nil),
            ("eventstore", "Event store", "Event stores", "supporting"),
            ("calendar", "Calendar", "Calendars", "supporting"),
            ("event", "Event", "Events", nil),
            ("reminderlist", "Reminders list", "Reminders lists", "supporting"),
            ("reminder", "Reminder", "Reminders", nil),
            ("visitstore", "Visit store", "Visit stores", "supporting"),
            ("visit", "Visit", "Visits", nil),
            ("share", "Share", "Shares", nil),
        ]
        for (name, singular, plural, purpose) in apple {
            labels[SyncEngine.appleKinds + name] = KindLabel(singular: singular, plural: plural, purpose: purpose)
        }
        return labels
    }()

    /// The first release whose approval sets up packages, an account and a
    /// webhook.
    private static let release = ServerRequirement(minimumVersion: "0.114.0")

    /// What a server needs to sign Sync in: that release, and the verbs and
    /// the detail types the request uses, which a build between releases
    /// passes on by listing them in its metadata. A release below it is too
    /// old even when it does.
    static var requirement: ServerRequirement { make(account: "", deviceName: "", timeZone: "").requirement }

    /// One scope in the approval page's words.
    static func scopeWords(_ scope: String) -> String {
        make(account: "", deviceName: "", timeZone: "").scopeWords(scope)
    }

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
                .webhook(trigger: SyncEngine.trigger, deliversTo: AppModel.healthProvider),
            ],
            requirement: release,
            claimedRedirect: AppManifest.claimedRedirect(infoKey: redirectKey, bundle: bundle),
            passwordFallback: .belowRequirement,
            kindLabels: kindLabels)
    }
}
