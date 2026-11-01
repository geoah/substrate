import Foundation
import Testing
@testable import SubstrateKit

@Suite struct Status {
    static let now = Date(timeIntervalSince1970: 1_800_000_000)
    let server = URL(string: "https://ada.example.com")!

    func appSession(expiresAt: Date? = nil, webhook: Bool = true) -> Session {
        var scopes = syncManifest().scopes
        if webhook { scopes.append("webhook:applehealth-webhook") }
        return Session(server: server, repository: "ada.example.com", secret: "sub_app_x",
                       clientID: syncManifest().clientID, scopes: scopes, expiresAt: expiresAt)
    }

    func run(_ stub: StubTransport, session: Session, manifest: AppManifest = syncManifest()) async -> AppStatus {
        var client = SubstrateClient(session, transport: stub)
        client.pause = { _ in }
        return await AppStatusCheck(manifest: manifest, session: session, client: client, now: { Self.now }).run()
    }

    func server(version: String = "v0.114.0", details: [String] = AuthorizationDetail.Types.all, metadata: Bool = true,
                check: String? = nil, checkStatus: Int = 200) -> StubTransport
    {
        let stub = StubTransport()
        stub.on("GET", "/.well-known/substrate/server.json", json: discoveryJSON(version: version))
        if metadata {
            stub.on("GET", "/.well-known/oauth-authorization-server", json: metadataJSON(details: details))
        } else {
            stub.on("GET", "/.well-known/oauth-authorization-server", status: 404, json: "404 page not found")
        }
        if let check { stub.on("POST", "/api/v1/authorizations/check", status: checkStatus, json: check) }
        return stub
    }

    /// The setup rows of Sync's request, each with `actions[i]`.
    func rows(_ actions: [String], held: Int = 3, shipped: Int = 3) -> String {
        let facts = [
            #""package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":\#(held),"shipped":\#(shipped)}"#,
            #""package":{"id":"providers.substrate.reamde.dev/applehealth","required":5,"held":5,"shipped":5}"#,
            #""account":{"kind":"providers.substrate.reamde.dev/applehealth/account","id":"substrate-sync-1a2b3c4d"}"#,
            #""webhook":{"trigger":"applehealth-webhook"}"#,
        ]
        let types = ["package", "package", "account", "webhook"]
        return "[" + zip(zip(types, actions), facts).map { #"{"type":"\#($0.0.0)","action":"\#($0.0.1)",\#($0.1)}"# }
            .joined(separator: ",") + "]"
    }

    @Test func aGrownRequestNeedsTheOwnersApproval() async throws {
        let stub = server(check: #"""
        {"scopes":{"missing":["write:providers.substrate.reamde.dev/apple/place"]},
         "setup":\#(rows(["upgrade", "none", "none", "none"], held: 2))}
        """#)
        let status = await run(stub, session: appSession())
        #expect(status.issues == [.needsApproval(changes: [
            Change(kind: .scope, words: "See, add, change and delete your Apple places"),
            Change(kind: .upgrade, words: "Update Apple", detail: "Version 2 to 3"),
        ])])
        #expect(status.serverVersion == "v0.114.0")
        let sent = try #require(stub.sent("POST", "/api/v1/authorizations/check").first)
        #expect(sent.value(forHTTPHeaderField: "Authorization") == "Bearer sub_app_x")
    }

    @Test func aMissingAccountAndAnOffWebhookAreNamedInTheConsolesWords() async {
        let stub = server(check: #"{"scopes":{"missing":[]},"setup":\#(rows(["none", "install", "create", "arm"]))}"#)
        let status = await run(stub, session: appSession())
        #expect(status.changes == [
            Change(kind: .install, words: "Install Apple Health"),
            Change(kind: .account, words: "Create the Apple Health account “Ada's iPhone”", detail: "Timezone: Europe/London"),
            Change(kind: .webhook, words: "Turn on the Apple Health webhook"),
        ])
    }

    @Test func rowsTheTokenCannotSeeAreNeverAReasonToAsk() async {
        let stub = server(check: #"{"scopes":{"missing":[]},"setup":\#(rows(["unknown", "unknown", "unknown", "unknown"]))}"#)
        let status = await run(stub, session: appSession())
        #expect(status.isClear)
    }

    @Test func aWebhookTheTokenDoesNotDeliverToIsAsked() async {
        let stub = server(check: #"{"scopes":{"missing":[]},"setup":\#(rows(["none", "none", "none", "unknown"]))}"#)
        let status = await run(stub, session: appSession(webhook: false))
        #expect(status.changes == [Change(kind: .scope, words: "Send data to your repository through the Apple Health webhook, for any Apple Health account, and see which deliveries could not be filed")])
    }

    @Test func aServerTooOldIsSaidAndAnAppSessionIsNotCheckedAgainstIt() async {
        let stub = server(version: "v0.113.0", details: [])
        let status = await run(stub, session: appSession())
        #expect(status.issues == [.serverTooOld(running: "v0.113.0", needed: "0.114.0", missing: [
            "install what an app needs when you approve it", "create an app's account when you approve it",
            "turn on an app's webhook when you approve it",
        ])])
        #expect(stub.sent("POST", "/api/v1/authorizations/check").isEmpty)
    }

    @Test func aPackageTheServerShipsTooOldIsAServerUpdate() async {
        let stub = server(check: #"""
        {"scopes":{"missing":[]},"setup":[{"type":"package","action":"blocked","problem":"this server ships version 2, the app needs 3: update the server",
          "package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":2,"shipped":2}}]}
        """#)
        let status = await run(stub, session: appSession())
        #expect(status.issues == [.serverTooOld(running: "v0.114.0", needed: nil, missing: ["install Apple version 3 or later"])])
    }

    @Test func accessEndingSoonOrEndedIsSaid() async {
        let soon = Self.now.addingTimeInterval(3 * 86400)
        let stub = server(check: #"{"scopes":{"missing":[]},"setup":[],"expiresAt":"\#(Timestamp.format(soon))"}"#)
        #expect(await run(stub, session: appSession()).issues == [.accessEndsSoon(soon)])
        // Far off is nothing to say.
        let later = server(check: #"{"scopes":{"missing":[]},"setup":[],"expiresAt":"2030-01-01T00:00:00Z"}"#)
        #expect(await run(later, session: appSession()).isClear)
        // Ended here already: the server is not asked.
        let ended = StubTransport()
        #expect(await run(ended, session: appSession(expiresAt: Self.now.addingTimeInterval(-1))).issues == [.accessEnded])
        #expect(ended.requests.isEmpty)
        // Ended on the server: revoked.
        let revoked = server(check: #"{"error":{"code":"unauthorized","message":"token revoked"}}"#, checkStatus: 401)
        #expect(await run(revoked, session: appSession()).issues == [.accessEnded])
    }

    @Test func aPasswordSessionMovesOnceTheServerCan() async {
        let legacy = Session(server: server, repository: "ada.example.com", secret: "sub_x", tokenID: "tok_1")
        #expect(await run(server(), session: legacy).issues == [.legacyAccess])
        // On a server Sync falls back for, the password session is the
        // right one, and nothing is said.
        #expect(await run(server(version: "v0.112.0", metadata: false), session: legacy).isClear)
        // An app that never falls back says the server is too old.
        var strict = syncManifest()
        strict.passwordFallback = .never
        let status = await run(server(version: "v0.112.0", metadata: false), session: legacy, manifest: strict)
        #expect(status.issues.first?.id == "serverTooOld")
    }

    @Test func anUnreachableServerIsSaidBesideWhatTheSessionKnows() async {
        let stub = StubTransport()
        stub.fail("GET", "/.well-known/substrate/server.json", with: URLError(.cannotConnectToHost))
        let soon = Self.now.addingTimeInterval(86400)
        let status = await run(stub, session: appSession(expiresAt: soon))
        #expect(status.issues == [
            .unreachable(message: "Ada.example.com isn't answering."),
            .accessEndsSoon(soon),
        ])
    }

    @Test func aFirstSignInPlansEveryRow() throws {
        let meta = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: Data(metadataJSON().utf8))
        let changes = syncManifest().plannedChanges(for: meta)
        #expect(changes.map(\.words) == [
            "See, add, change and delete your Apple records, including kinds added later",
            "See your Apple Health accounts",
            "See your Apple Health exports",
            "Send data to your repository through the Apple Health webhook, for any Apple Health account, and see which deliveries could not be filed",
            "Install Apple",
            "Install Apple Health",
            "Create the Apple Health account “Ada's iPhone”",
            "Turn on the Apple Health webhook",
        ])
        #expect(ReviewHeading(changes: changes, firstSignIn: true) == .firstSignIn)
        #expect(ReviewHeading(changes: [changes[0]], firstSignIn: false) == .moreAccess)
        #expect(ReviewHeading(changes: [changes[4]], firstSignIn: false) == .serverSetup)
        #expect(ReviewHeading(changes: changes, firstSignIn: false) == .moreAccessAndServerSetup)
        #expect(ReviewHeading(changes: [], firstSignIn: false).title == "Renew access")
    }
}
