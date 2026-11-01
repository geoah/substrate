import Foundation
import SubstrateKit
import XCTest

@testable import SubstrateSync

/// A scripted server: each route answers what the test gave it, a 404
/// otherwise, and every request is kept for the test to read.
final class StubTransport: HTTPTransport, @unchecked Sendable {
    private let lock = NSLock()
    private var routes: [String: (Int, Data)] = [:]
    private var requests: [URLRequest] = []

    func on(_ method: String, _ path: String, status: Int = 200, json: String) {
        lock.lock()
        defer { lock.unlock() }
        routes["\(method) \(path)"] = (status, Data(json.utf8))
    }

    func sent(_ method: String, _ path: String) -> [URLRequest] {
        lock.lock()
        defer { lock.unlock() }
        return requests.filter { $0.httpMethod == method && $0.url.map(Self.path) == path }
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        guard let url = request.url else { throw URLError(.badURL) }
        let (status, body) = answer(request, url)
        return (body, HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: [:])!)
    }

    private func answer(_ request: URLRequest, _ url: URL) -> (Int, Data) {
        lock.lock()
        defer { lock.unlock() }
        requests.append(request)
        return routes["\(request.httpMethod ?? "GET") \(Self.path(url))"]
            ?? (404, Data(#"{"error":{"code":"not_found","message":"no such record"}}"#.utf8))
    }

    static func path(_ url: URL) -> String {
        URLComponents(url: url, resolvingAgainstBaseURL: false)?.percentEncodedPath ?? url.path
    }
}

/// What Sync asks a server for, as the approval page and the check read it.
final class ManifestTests: XCTestCase {
    let server = URL(string: "https://ada.example.com")!
    let manifest = SyncManifest.make(account: "substrate-sync-1a2b3c4d", deviceName: "Ada's iPhone", timeZone: "Europe/London")

    func testTheManifestAsksForWhatSyncWritesAndWhatItsApprovalSetsUp() throws {
        XCTAssertEqual(manifest.appName, "Substrate Sync")
        // The app's Info.plist key is empty outside a release.
        XCTAssertEqual(manifest.clientID, "http://127.0.0.1/substrate-sync")
        XCTAssertEqual(manifest.scope, "write:providers.substrate.reamde.dev/apple/* "
            + "read:providers.substrate.reamde.dev/applehealth/account read:providers.substrate.reamde.dev/applehealth/export")
        XCTAssertEqual(try AuthorizationDetail.json(manifest.details), #"[{"package":"providers.substrate.reamde.dev/apple","type":"package","version":3},"#
            + #"{"package":"providers.substrate.reamde.dev/applehealth","type":"package","version":5},"#
            + #"{"id":"substrate-sync-1a2b3c4d","kind":"providers.substrate.reamde.dev/applehealth/account","#
            + #""properties":{"displayName":"Ada's iPhone","timezone":"Europe/London"},"type":"account"},"#
            + #"{"trigger":"applehealth-webhook","type":"webhook"}]"#)
        XCTAssertEqual(AppModel.appleVersion, 3)
        XCTAssertEqual(AppModel.healthVersion, 5)
        // The account is the one the install names, which Set Up made under
        // a password session, so a move finds it in place.
        XCTAssertEqual(SyncManifest.make(account: LocalState().healthAccount, deviceName: "d", timeZone: "UTC").details[2].id,
                       LocalState().healthAccount)
    }

    func testAServerThatSetsUpGetsTheFlowAndAnyOtherThePassword() {
        let full = AuthorizationServerMetadata(issuer: server, scopesSupported: ["read:*", "write:*", "agents:*", "decide:*"],
                                               authorizationDetailsTypesSupported: ["package", "account", "webhook"])
        let scopesOnly = AuthorizationServerMetadata(issuer: server, scopesSupported: ["read:*", "write:*", "agents:*", "decide:*"])
        func probe(_ version: String, _ metadata: AuthorizationServerMetadata?) -> ServerProbe.Result {
            ServerProbe.Result(origin: server, discovery: Discovery(server: .init(version: version)), metadata: metadata,
                               verdict: SyncManifest.requirement.verdict(version: version, metadata: metadata))
        }
        XCTAssertEqual(manifest.door(for: probe("v0.114.0", full)), .app)
        XCTAssertEqual(manifest.door(for: probe("dev", full)), .app)
        XCTAssertEqual(manifest.door(for: probe("v0.113.0", full)), .password)
        XCTAssertEqual(manifest.door(for: probe("v0.114.0", scopesOnly)), .password)
        XCTAssertEqual(manifest.door(for: probe("v0.112.0", nil)), .password)
        // An older server would approve the scopes alone, which the app would
        // read as set up: it is sent no details.
        XCTAssertTrue(manifest.details(for: scopesOnly).isEmpty)
        XCTAssertEqual(manifest.details(for: full), manifest.details)
        XCTAssertTrue(manifest.canMoveToAppAccess(probe("v0.114.0", full)))
    }
}

/// The two kinds of session, kept where every build kept one, and read by
/// the app and the share extension alike.
final class SessionKindTests: XCTestCase {
    /// A session every build before app access kept still signs in: a
    /// password one, full access, with its own token id; and written back,
    /// such a build still reads it.
    func testAPasswordSessionKeptByAnEarlierBuildStillReads() throws {
        let store = SessionStore(service: SessionStore.sync.service, account: "session-test-\(UUID().uuidString)")
        defer { store.clear() }
        // As the earlier build's JSONEncoder wrote its four fields.
        let earlier = #"{"server":"https:\/\/ada.example.com","repository":"ada.example.com","secret":"sub_x","tokenID":"tok_1"}"#
        try store.write(Data(earlier.utf8), account: store.account)
        guard case let .found(session) = store.read() else { return XCTFail("the earlier build's session did not read") }
        XCTAssertFalse(session.isApp)
        XCTAssertNil(session.grants)
        XCTAssertEqual(session.server.absoluteString, "https://ada.example.com")
        XCTAssertEqual(session.repository, "ada.example.com")
        XCTAssertEqual(session.secret, "sub_x")
        XCTAssertEqual(session.tokenID, "tok_1")

        struct Earlier: Decodable {
            let server: URL
            let repository: String
            let secret: String
            let tokenID: String
        }
        try store.save(session)
        guard case let .found(data) = store.data(account: store.account) else { return XCTFail("the session was not kept") }
        XCTAssertEqual(try JSONDecoder().decode(Earlier.self, from: data).tokenID, "tok_1")
    }

    func testAnAppSessionKeepsWhatItWasApproved() throws {
        let store = SessionStore(service: SessionStore.sync.service, account: "session-test-\(UUID().uuidString)")
        defer { store.clear() }
        let session = Session(server: URL(string: "https://ada.example.com")!, repository: "ada.example.com", secret: "sub_app_x",
                              clientID: SyncManifest.localClientID, scopes: SyncManifest.scopes + ["webhook:applehealth-webhook"],
                              expiresAt: Date(timeIntervalSinceReferenceDate: 815_000_000),
                              revocation: URL(string: "https://ada.example.com/oauth/revoke"))
        try store.save(session)
        guard case let .found(read) = store.read() else { return XCTFail("the app session did not read") }
        XCTAssertEqual(read, session)
        XCTAssertTrue(read.isApp)
        XCTAssertEqual(read.grants?.delivers(SyncEngine.trigger), true)
        XCTAssertNil(read.tokenID)
    }

    /// The phone's state is bound to a repository by its identity, which the
    /// kit lowercases and an earlier build kept as the address was typed.
    func testTheStateStaysBoundToTheRepositoryItWasPushedTo() {
        let session = Session(server: URL(string: "https://ada.example.com")!, repository: "ada.example.com", secret: "s", tokenID: "t")
        var state = LocalState()
        state.repository = "https://Ada.Example.com|ada.example.com"
        XCTAssertTrue(state.isBound(to: session))
        state.repository = "https://bob.example.com|bob.example.com"
        XCTAssertFalse(state.isBound(to: session))
        state.repository = nil
        XCTAssertFalse(state.isBound(to: session))
    }
}

/// Health goes to the webhook door with the app's own token under an app
/// session, and with the trigger's key under a password one.
final class DeliveryTests: XCTestCase {
    let server = URL(string: "https://ada.example.com")!
    let door = "/webhooks/ada.example.com/applehealth-webhook"
    let trigger = "/api/v1/substrate.reamde.dev/core/trigger/applehealth-webhook"
    let account = "/api/v1/providers.substrate.reamde.dev/applehealth/account/substrate-sync-1a2b3c4d"

    var headers: [(String, String)] {
        HealthDelivery.headers(grouping: .minutes, account: "substrate-sync-1a2b3c4d", automationID: "a", sessionID: "s")
    }

    func testAnAppSessionDeliversWithItsTokenAndNeverReadsTheTrigger() async throws {
        let stub = StubTransport()
        stub.on("POST", door, status: 202, json: #"{"fire":"hook-1"}"#)
        let session = Session(server: server, repository: "ada.example.com", secret: "sub_app_x", clientID: SyncManifest.localClientID,
                              scopes: SyncManifest.scopes + ["webhook:applehealth-webhook"])
        let client = Client(session, transport: stub)
        let key = try await SyncEngine.deliveryKey(client, session)
        XCTAssertNil(key)
        XCTAssertTrue(stub.sent("GET", trigger).isEmpty)
        let fire = try await client.deliver(authority: session.repository, trigger: SyncEngine.trigger, body: Data("{}".utf8),
                                            headers: headers, key: key)
        XCTAssertEqual(fire, "hook-1")
        let sent = try XCTUnwrap(stub.sent("POST", door).first)
        XCTAssertEqual(sent.value(forHTTPHeaderField: "Authorization"), "Bearer sub_app_x")
        XCTAssertEqual(sent.value(forHTTPHeaderField: "X-Substrate-Account"), "substrate-sync-1a2b3c4d")
        XCTAssertEqual(sent.value(forHTTPHeaderField: "Content-Type"), "application/json")
        XCTAssertEqual(sent.value(forHTTPHeaderField: "automation-aggregation"), "minutes")
    }

    func testAPasswordSessionDeliversWithTheTriggersKey() async throws {
        let stub = StubTransport()
        stub.on("GET", trigger, json: #"{"id":"applehealth-webhook","properties":{"enabled":true,"source":{"webhook":{"key":"k3y"}}}}"#)
        stub.on("POST", door, status: 202, json: #"{"fire":"hook-2"}"#)
        let session = Session(server: server, repository: "ada.example.com", secret: "sub_password", tokenID: "tok_1")
        let client = Client(session, transport: stub)
        let key = try await SyncEngine.deliveryKey(client, session)
        XCTAssertEqual(key, "k3y")
        try await client.deliver(authority: session.repository, trigger: SyncEngine.trigger, body: Data("{}".utf8), headers: headers, key: key)
        XCTAssertEqual(stub.sent("POST", door).first?.value(forHTTPHeaderField: "Authorization"), "Bearer k3y")

        // An unarmed trigger stops Health before a day is read.
        let off = StubTransport()
        off.on("GET", trigger, json: #"{"id":"applehealth-webhook","properties":{"enabled":false,"source":{"webhook":{"key":"k3y"}}}}"#)
        do {
            _ = try await SyncEngine.deliveryKey(Client(session, transport: off), session)
            XCTFail("a disabled trigger gave a key")
        } catch SyncError.notArmed {}
    }

    /// The door refusing an app's delivery is a review for the owner, not a
    /// sign-out and not Set Up.
    func testAnAppsRefusedDeliveryAsksForApproval() async throws {
        let stub = StubTransport()
        stub.on("POST", door, status: 404, json: #"{"error":{"code":"not_found","message":"no such webhook"}}"#)
        let session = Session(server: server, repository: "ada.example.com", secret: "sub_app_x", clientID: SyncManifest.localClientID,
                              scopes: SyncManifest.scopes)
        do {
            try await Client(session, transport: stub).deliver(authority: session.repository, trigger: SyncEngine.trigger,
                                                               body: Data("{}".utf8), headers: headers, key: nil)
            XCTFail("a refused delivery answered a fire")
        } catch {
            XCTAssertTrue(needsApproval(error), "\(error)")
            XCTAssertFalse(isSignedOut(error))
        }
    }

    /// An app session never recreates its Health account: a missing one is
    /// the owner's to approve. A password session makes it, as Set Up does.
    func testOnlyAPasswordSessionMakesAMissingAccount() async throws {
        let app = Session(server: server, repository: "ada.example.com", secret: "sub_app_x", clientID: SyncManifest.localClientID,
                          scopes: SyncManifest.scopes)
        let stub = StubTransport()
        let missing = try await SyncEngine.healthAccount(Client(app, transport: stub), app, id: "substrate-sync-1a2b3c4d") { "Ada's iPhone" }
        XCTAssertEqual(missing, .missing)
        XCTAssertTrue(stub.sent("PUT", account).isEmpty)

        let password = Session(server: server, repository: "ada.example.com", secret: "sub_password", tokenID: "tok_1")
        let legacy = StubTransport()
        legacy.on("PUT", account, json: #"{"id":"substrate-sync-1a2b3c4d","properties":{}}"#)
        let created = try await SyncEngine.healthAccount(Client(password, transport: legacy), password, id: "substrate-sync-1a2b3c4d") {
            "Ada's iPhone"
        }
        XCTAssertEqual(created, .created)
        let body = try XCTUnwrap(legacy.sent("PUT", account).first?.httpBody)
        let properties = try XCTUnwrap((try JSONSerialization.jsonObject(with: body) as? [String: Any])?["properties"] as? [String: Any])
        XCTAssertEqual(properties["displayName"] as? String, "Ada's iPhone")

        let present = StubTransport()
        present.on("GET", account, json: #"{"id":"substrate-sync-1a2b3c4d","properties":{"displayName":"Ada's iPhone"}}"#)
        let found = try await SyncEngine.healthAccount(Client(app, transport: present), app, id: "substrate-sync-1a2b3c4d") { "x" }
        XCTAssertEqual(found, .present)
    }
}
