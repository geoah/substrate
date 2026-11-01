import Foundation
@testable import SubstrateKit

/// A scripted server: each route answers its responses in turn, the last one
/// again once they run out, and every request is kept for the test to read.
final class StubTransport: HTTPTransport, @unchecked Sendable {
    struct Answer {
        var status: Int
        var headers: [String: String] = [:]
        var body: Data = Data()
    }

    private let lock = NSLock()
    private var routes: [String: [Answer]] = [:]
    private var failures: [String: Error] = [:]
    private(set) var requests: [URLRequest] = []

    @discardableResult
    func on(_ method: String, _ path: String, status: Int = 200, headers: [String: String] = [:], json: String = "")
        -> StubTransport
    {
        lock.lock()
        defer { lock.unlock() }
        routes["\(method) \(path)", default: []].append(Answer(status: status, headers: headers, body: Data(json.utf8)))
        return self
    }

    func fail(_ method: String, _ path: String, with error: Error) {
        lock.lock()
        defer { lock.unlock() }
        failures["\(method) \(path)"] = error
    }

    func sent(_ method: String, _ path: String) -> [URLRequest] {
        lock.lock()
        defer { lock.unlock() }
        return requests.filter { $0.httpMethod == method && $0.url.map(Self.path) == path }
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        guard let url = request.url else { throw URLError(.badURL) }
        let answer = try answer(for: request, url: url)
        let response = HTTPURLResponse(url: url, statusCode: answer.status, httpVersion: "HTTP/1.1", headerFields: answer.headers)!
        return (answer.body, response)
    }

    private func answer(for request: URLRequest, url: URL) throws -> Answer {
        let key = "\(request.httpMethod ?? "GET") \(Self.path(url))"
        lock.lock()
        defer { lock.unlock() }
        requests.append(request)
        if let error = failures[key] { throw error }
        let answers = routes[key] ?? []
        if answers.count > 1 { routes[key]?.removeFirst() }
        return answers.first ?? Answer(status: 404, body: Data(#"{"error":{"code":"not_found","message":"no route \#(key)"}}"#.utf8))
    }

    /// The path as sent, percent-encoding kept.
    static func path(_ url: URL) -> String {
        URLComponents(url: url, resolvingAgainstBaseURL: false)?.percentEncodedPath ?? url.path
    }
}

/// A discovery document as internal/api's discovery.go serves it.
func discoveryJSON(version: String = "v0.114.0", totp: Bool = true) -> String {
    #"""
    {
      "versions": [{"name": "v1", "status": "served"}],
      "server": {"version": "\#(version)", "build": "0123456789abcdef0123456789abcdef01234567"},
      "changelog": {"horizon": 0},
      "features": [
        {"name": "triggers", "stability": "stable", "surfaces": ["rest"]},
        {"name": "search", "stability": "beta", "surfaces": ["rest"]},
        {"name": "agents", "stability": "alpha", "surfaces": ["rest"]},
        {"name": "apps", "stability": "alpha", "surfaces": ["rest"]}
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
func metadataJSON(issuer: String = "https://ada.example.com", details: [String] = ["package", "account", "webhook"],
                  verbs: [String] = ["read", "write", "agents", "decide"], endpoints: String? = nil) -> String
{
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
        scopes: ["write:providers.substrate.reamde.dev/apple/*",
                 "read:providers.substrate.reamde.dev/applehealth/account",
                 "read:providers.substrate.reamde.dev/applehealth/export"],
        details: [
            .package("providers.substrate.reamde.dev/apple", atLeast: 3),
            .package("providers.substrate.reamde.dev/applehealth", atLeast: 5),
            .account(kind: "providers.substrate.reamde.dev/applehealth/account", id: "substrate-sync-1a2b3c4d",
                     properties: ["displayName": "Ada's iPhone", "timezone": "Europe/London"]),
            .webhook(trigger: "applehealth-webhook", deliversTo: "providers.substrate.reamde.dev/applehealth"),
        ],
        requirement: ServerRequirement(minimumVersion: "0.114.0"),
        passwordFallback: .belowRequirement,
        kindLabels: [
            "providers.substrate.reamde.dev/applehealth/account": KindLabel(singular: "Account", plural: "Accounts"),
            "providers.substrate.reamde.dev/applehealth/export": KindLabel(singular: "Delivery", plural: "Deliveries"),
        ])
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
