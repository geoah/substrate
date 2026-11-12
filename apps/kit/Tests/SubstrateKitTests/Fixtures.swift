import Foundation

@testable import SubstrateAPI
@testable import SubstrateAuth

/// A discovery document as internal/api's discovery.go serves it.
func discoveryJSON(version: String = "v0.114.0", totp: Bool = true) -> String {
    #"""
    {
      "versions": [{"name": "v1", "status": "served"}],
      "server": {"version": "\#(version)", "build": "0123456789abcdef0123456789abcdef01234567"},
      "changelog": {"horizon": 0},
      "features": [
        {"name": "records", "stability": "stable", "revision": 1, "surfaces": ["rest"]},
        {"name": "triggers", "stability": "stable", "revision": 1, "surfaces": ["rest"]},
        {"name": "auth", "stability": "beta", "revision": 1, "surfaces": ["rest"],
         "notice": "Before 1.0, sign-in names a person, who then picks a repository."},
        {"name": "apps", "stability": "beta", "revision": 1, "surfaces": ["rest"]},
        {"name": "search", "stability": "beta", "revision": 1, "surfaces": ["rest"]},
        {"name": "agents", "stability": "experimental", "revision": 1, "surfaces": ["rest"]}
      ],
      "surfaces": {"rest": {"endpoint": "/api/v1", "compatibility": "supported"}},
      "grammar": {
        "kind": "<authority>/<package>/<name>",
        "record": "<authority>/<package>/<kind>/<id>",
        "collection": "/api/v1/records",
        "recordPath": "/api/v1/{authority}/{package}/{kind}/{id}",
        "actors": ["api", "console", "cli", "bundle:<authority>:<package>", "function:<authority>:<package>:<name>",
                   "agent:<authority>:<package>:<name>", "app:<host>", "substrate"]
      },
      "endpoints": {"register": "/register", "login": "/login", "tokens": "/tokens", "password": "/password",
                    "totp": "/totp", "authorize": "/oauth/authorize", "token": "/oauth/token"},
      "registration": {"inviteRequired": false, "totpRequired": \#(totp)}
    }
    """#
}

/// The OAuth metadata a substrate at `issuer` serves; `endpoints` moves its
/// endpoints elsewhere, as a server handing the sign-in on would.
func metadataJSON(
    issuer: String = "https://ada.example.com", details: [String] = ["package", "account", "webhook"],
    verbs: [String] = ["read", "write", "agents", "decide"], endpoints: String? = nil
) -> String {
    let scopes = verbs.map { "\"\($0):*\"" }.joined(separator: ",")
    let types = details.map { "\"\($0)\"" }.joined(separator: ",")
    let at = endpoints ?? issuer
    return #"""
        {
          "issuer": "\#(issuer)",
          "authorization_endpoint": "\#(at)/oauth/authorize",
          "token_endpoint": "\#(at)/oauth/token",
          "revocation_endpoint": "\#(at)/oauth/revoke",
          "response_types_supported": ["code"],
          "response_modes_supported": ["query"],
          "grant_types_supported": ["authorization_code"],
          "code_challenge_methods_supported": ["S256"],
          "token_endpoint_auth_methods_supported": ["none"],
          "revocation_endpoint_auth_methods_supported": ["none"],
          "scopes_supported": [\#(scopes)],
          "authorization_details_types_supported": [\#(types)],
          "authorization_response_iss_parameter_supported": true,
          "client_id_metadata_document_supported": true
        }
        """#
}

/// Substrate Sync's request, as docs/plans/app-setup.md writes it.
func syncManifest() -> AppManifest {
    AppManifest(
        appName: "Substrate Sync",
        clientID: "https://sync.substrate.example.com/client.json",
        scopes: [
            "write:providers.substrate.reamde.dev/apple/*",
            "read:providers.substrate.reamde.dev/applehealth/account",
            "read:providers.substrate.reamde.dev/applehealth/export",
        ],
        details: [
            .package("providers.substrate.reamde.dev/apple", atLeast: 3),
            .package("providers.substrate.reamde.dev/applehealth", atLeast: 5),
            .account(
                kind: "providers.substrate.reamde.dev/applehealth/account", id: "substrate-sync-1a2b3c4d",
                properties: ["displayName": "Ada's iPhone", "timezone": "Europe/London"]),
            .webhook(trigger: "applehealth-webhook", deliversTo: "providers.substrate.reamde.dev/applehealth"),
        ],
        requirement: ServerRequirement(minimumVersion: "0.114.0"),
        passwordFallback: .belowRequirement,
        kindLabels: [
            "providers.substrate.reamde.dev/applehealth/account": KindLabel(singular: "Account", plural: "Accounts"),
            "providers.substrate.reamde.dev/applehealth/export": KindLabel(singular: "Delivery", plural: "Deliveries"),
        ],
        softwareVersion: "0.120.0 (20261103.101500)")
}

/// The server's block sentences, as internal/appsetup/blocks.go words them:
/// what an owner sees, so a fixture says it too.
enum Blocks {
    static let quarantined = "Your copy of this package is quarantined, so approving cannot set it up. Its provider page says why."
    static let notShipped = "This server does not offer this package, so approving cannot set it up."

    /// The package's own row: the catalog ships it below the version needed.
    static func serverUpdate(shipped: Int, need: Int) -> String {
        "This server ships version \(shipped); the app needs version \(need). The server needs an update first."
    }

    /// The row of a package whose requirement the catalog ships too old.
    static func serverUpdateFirst(need: Int, shipped: Int) -> String {
        "It needs a package first at version \(need), and this server ships version \(shipped). The server needs an update first."
    }
}

/// The labels and purposes Substrate Sync's manifest carries
/// (SubstrateSync/Sources/App/Manifest.swift), so the gallery words a scope
/// as the app does.
let syncKindLabels: [String: KindLabel] = {
    var labels = [
        "providers.substrate.reamde.dev/applehealth/account": KindLabel(singular: "Account", plural: "Accounts"),
        "providers.substrate.reamde.dev/applehealth/export": KindLabel(singular: "Delivery", plural: "Deliveries"),
    ]
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
        labels["providers.substrate.reamde.dev/apple/" + name] = KindLabel(singular: singular, plural: plural, purpose: purpose)
    }
    return labels
}()
