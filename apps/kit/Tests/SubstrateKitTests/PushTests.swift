import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateTesting
import Testing

@testable import SubstrateAuth

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// The push wire's fixtures, as `internal/pushwire` writes them: the kit's
/// copies, held byte for byte to the originals by `lint:apps`.
enum PushWire {
    static func data(_ name: String) throws -> Data {
        let url = try #require(Bundle.module.url(forResource: name, withExtension: "json", subdirectory: "Resources/pushwire"))
        return try Data(contentsOf: url)
    }

    static func object(_ name: String, in folder: String = "pushwire") throws -> [String: Any] {
        let url = try #require(Bundle.module.url(forResource: name, withExtension: "json", subdirectory: "Resources/\(folder)"))
        return try #require(try JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any])
    }

    static func array(_ name: String) throws -> [[String: Any]] {
        try #require(try JSONSerialization.jsonObject(with: data(name)) as? [[String: Any]])
    }

    /// What a push fixture's Apple payload hands the app, as the system
    /// flattens it into a notification's `userInfo`.
    static func userInfo(_ name: String) throws -> [String: String] {
        let apple = try #require(try object(name)["apple"] as? [String: Any])
        let payload = try #require(apple["payload"] as? [String: Any])
        return PushPayload.flatten(payload)
    }
}

@Suite("The push relay's addresses and wire")
struct PushRelayTests {
    @Test func everyRelayBuildsTheAddressesTheWireDoes() throws {
        for row in try PushWire.array("endpoints") {
            let relay = try #require(row["relay"] as? String)
            if row["refused"] as? Bool == true {
                #expect(PushRelay.endpoint(relay, path: PushRelay.pushPath) == nil, "\(relay) is refused")
            } else {
                #expect(PushRelay.endpoint(relay, path: PushRelay.registerPath) == row["register"] as? String, "\(relay)")
                #expect(PushRelay.endpoint(relay, path: PushRelay.pushPath) == row["push"] as? String, "\(relay)")
            }
        }
    }

    @Test func twoRelaysAreOneWhereTheyPushToOneAddress() {
        #expect(PushRelay.same("https://push.example.com", "https://push.example.com/"))
        #expect(PushRelay.same("https://example.com/push/", "https://example.com/push"))
        #expect(!PushRelay.same("https://push.example.com", "https://example.com/push"))
        #expect(!PushRelay.same("https://push.example.com", "https://Push.example.com"))
        #expect(!PushRelay.same("http://push.example.com", "http://push.example.com"))
        #expect(!PushRelay.same("", ""))
    }

    @Test func theRegisterRequestIsTheWiresAndCarriesNoToken() async throws {
        let fixture = try PushWire.object("register")
        let request = try #require(fixture["request"] as? [String: String])
        let response = try #require(fixture["response"] as? [String: Any])
        let body = try #require(response["body"] as? [String: String])
        let transport = StubTransport().on(
            "POST", "/v1/register", json: String(decoding: try JSONSerialization.data(withJSONObject: body), as: UTF8.self))
        let sent = PushRelay.RegisterRequest(
            platform: try #require(request["platform"]), token: try #require(request["token"]), topic: try #require(request["topic"]),
            environment: try #require(request["environment"]), publicKey: try #require(request["publicKey"]),
            generation: try #require(request["generation"]))
        let key = try await PushRelay.register(sent, relay: "https://push.example.com/", transport: transport)
        #expect(key == body["pushKey"])
        let made = try #require(transport.requests.first)
        #expect(made.url?.absoluteString == "https://push.example.com/v1/register")
        #expect(made.value(forHTTPHeaderField: "Authorization") == nil)
        let wrote = try #require(try JSONSerialization.jsonObject(with: made.httpBody ?? Data()) as? [String: String])
        #expect(wrote == request)
    }

    @Test func aRedirectFromTheRelayIsRefused() async throws {
        let transport = StubTransport().on("POST", "/v1/register", status: 307, headers: ["Location": "https://elsewhere.example.com/v1/register"])
        let request = PushRelay.RegisterRequest(platform: "apns", token: "ab", topic: "t", environment: "development", publicKey: "k", generation: "g")
        await #expect(throws: Redirected.self) {
            _ = try await PushRelay.register(request, relay: "https://push.example.com", transport: transport)
        }
        #expect(transport.requests.count == 1)
    }

    @Test func everyRefusalOfTheRelayReadsByItsCode() async throws {
        let request = PushRelay.RegisterRequest(platform: "apns", token: "ab", topic: "t", environment: "development", publicKey: "k", generation: "g")
        for row in try PushWire.array("errors") {
            let status = try #require(row["status"] as? Int)
            let body = try JSONSerialization.data(withJSONObject: try #require(row["body"]))
            let code = try #require(((row["body"] as? [String: Any])?["error"] as? [String: Any])?["code"] as? String)
            let transport = StubTransport().on("POST", "/v1/register", status: status, json: String(decoding: body, as: UTF8.self))
            do {
                _ = try await PushRelay.register(request, relay: "https://push.example.com", transport: transport)
                Issue.record("\(status) \(code) was taken for a key")
            } catch let refusal as PushRelay.Refusal {
                #expect(refusal.status == status && refusal.code == code)
                #expect(refusal.stops == (code == "topic_refused" || code == "environment_refused"))
            }
        }
    }

    @Test func thePublishersDocumentIsReadLeniently() async throws {
        let client = "https://apps.example.com/apps/chat/client.json"
        func document(_ push: String) -> String {
            #"{"authority":"example.com","name":"Example","apps":[{"client_id":"https://apps.example.com/apps/sync/client.json","name":"Sync","push":{"relay":"https://other.example.com"}},{"client_id":"\#(client)","name":"Chat"\#(push)}]}"#
        }
        func read(_ json: String, status: Int = 200) async throws -> String? {
            let transport = StubTransport().on("GET", "/.well-known/substrate/authority.json", status: status, json: json)
            let relay = try await PushRelay.published(clientID: client, transport: transport)
            #expect(transport.requests.first?.url?.host == "apps.example.com")
            #expect(transport.requests.first?.value(forHTTPHeaderField: "Authorization") == nil)
            return relay
        }
        #expect(try await read(document(#","push":{"relay":"https://push.example.com"}"#)) == "https://push.example.com")
        #expect(try await read(document("")) == nil)
        for wrong in [
            #","push":"x""#, #","push":null"#, #","push":{"relay":1}"#, #","push":{"relay":"http://push.example.com"}"#,
            #","push":{"relay":"https://push.example.com?x=1"}"#,
        ] {
            #expect(try await read(document(wrong)) == nil, "\(wrong) names no relay")
        }
        #expect(try await read("", status: 404) == nil)
        await #expect(throws: (any Error).self) { _ = try await read("", status: 500) }
        await #expect(throws: (any Error).self) { _ = try await read("<html>a portal</html>") }
        // A loopback client id, a local build's, has no publisher.
        #expect(try await PushRelay.published(clientID: "http://127.0.0.1/substrate-chat", transport: StubTransport()) == nil)
    }

    @Test func aRefusalNamesItsReason() throws {
        let body = Data(#"{"error":{"code":"forbidden","reason":"no_relay","message":"this token's app names no push relay"}}"#.utf8)
        let error = try #require(ServerError(status: 403, envelope: body))
        #expect(error.reason == "no_relay" && error.code == "forbidden")
        #expect(ServerError(status: 403, envelope: Data(#"{"error":{"code":"forbidden","message":"no"}}"#.utf8))?.reason == nil)
    }
}

@Suite("Which Apple environment a build is")
struct PushEnvironmentTests {
    static func profile(_ entitlements: String) -> Data {
        // A profile is a signed CMS envelope with the property list in clear.
        var data = Data([0x30, 0x82, 0x1F, 0x00, 0x06, 0x09])
        data.append(
            Data(
                """
                <?xml version="1.0" encoding="UTF-8"?>
                <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
                <plist version="1.0"><dict><key>Name</key><string>a profile</string>
                <key>Entitlements</key><dict>\(entitlements)</dict></dict></plist>
                """.utf8))
        data.append(Data([0xA0, 0x82, 0x0E, 0x00, 0x31]))
        return data
    }

    @Test func aSimulatorIsAlwaysDevelopment() {
        #expect(PushEnvironment.of(profile: nil, simulator: true) == .development)
        #expect(PushEnvironment.of(profile: Self.profile("<key>aps-environment</key><string>production</string>"), simulator: true) == .development)
    }

    @Test func aBuildWithNoProfileIsProduction() {
        #expect(PushEnvironment.of(profile: nil, simulator: false) == .production)
    }

    @Test func aProfileSaysItsEnvironment() {
        #expect(PushEnvironment.of(profile: Self.profile("<key>aps-environment</key><string>development</string>"), simulator: false) == .development)
        #expect(PushEnvironment.of(profile: Self.profile("<key>aps-environment</key><string>production</string>"), simulator: false) == .production)
        #expect(PushEnvironment.of(profile: Self.profile("<key>get-task-allow</key><true/>"), simulator: false) == nil)
        #expect(PushEnvironment.of(profile: Data("not a profile".utf8), simulator: false) == nil)
    }
}

@Suite("What a push says, read from a notification")
struct PushPayloadTests {
    @Test func aPushsValuesArriveUnderSubstrate() throws {
        let info = try PushWire.userInfo("push-reply")
        let push = try #require(PushPayload(info))
        #expect(push.event == .reply && push.thread == "8f2c41d07a" && push.generation == "jPFx1d3VWfLE3W0iVXra0A")
        #expect(push.eventID.count > 32)
        #expect(info[PushPayload.Key.version] == "1")
        #expect(info["aps"] == nil)
        for name in ["push-failed", "push-request-card-1", "push-request-card-2", "push-read", "push-reply-largest"] {
            #expect(PushPayload(try PushWire.userInfo(name)) != nil, "\(name)")
        }
        #expect(PushPayload(["thread": "t1"]) == nil)
    }

    @Test func aTapOpensAPushsChatOnlyForThisInstallsRegistration() throws {
        let info = try PushWire.userInfo("push-reply")
        let current = "jPFx1d3VWfLE3W0iVXra0A"
        #expect(PushPayload.thread(of: info, key: "thread") { $0 == current } == "8f2c41d07a")
        #expect(PushPayload.thread(of: info, key: "thread") { $0 == "pending" || $0 == current } == "8f2c41d07a")
        #expect(PushPayload.thread(of: info, key: "thread") { $0 == "another" } == nil)
        // A local notification names its thread at the top.
        #expect(PushPayload.thread(of: ["thread": "t1"], key: "thread") { _ in false } == "t1")
        // So does a push the extension filled in, and its generation still
        // decides.
        var filled = info
        filled["thread"] = "8f2c41d07a"
        #expect(PushPayload.thread(of: filled, key: "thread") { $0 == current } == "8f2c41d07a")
        #expect(PushPayload.thread(of: filled, key: "thread") { _ in false } == nil)
    }
}

/// A value a test sets after the closure that reads it is made.
final class Box<T>: @unchecked Sendable {
    var value: T?
}

/// A transport that runs `hook` before each request reaches the stub.
final class HookTransport: HTTPTransport, @unchecked Sendable {
    let inner: StubTransport
    let hook: @Sendable (URLRequest) async -> Void

    init(_ inner: StubTransport, hook: @escaping @Sendable (URLRequest) async -> Void) {
        self.inner = inner
        self.hook = hook
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        await hook(request)
        return try await inner.data(for: request)
    }
}

/// A transport that holds one path until the request is cancelled, as a
/// network that never answers does.
final class SlowTransport: HTTPTransport, @unchecked Sendable {
    let inner: StubTransport
    let path: String
    private let lock = NSLock()
    private var held = false

    init(_ inner: StubTransport, holding path: String) {
        self.inner = inner
        self.path = path
    }

    var holding: Bool { lock.withLock { held } }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        if request.url.map(StubTransport.path) == path {
            lock.withLock { held = true }
            try await Task.sleep(for: .seconds(60))
        }
        return try await inner.data(for: request)
    }
}

/// A substrate, a publisher and a relay that agree, for the registrar.
final class PushWorld: @unchecked Sendable {
    static let client = "https://apps.example.com/apps/chat/client.json"
    static let tokenID = "71ac09be44"
    static let devicePath = "/api/v1/substrate.reamde.dev/core/device/71ac09be44"
    static let record =
        #"{"id":"71ac09be44","kind":"substrate.reamde.dev/core/device","version":1,"createdAt":"2026-11-18T10:00:00Z","updatedAt":"2026-11-18T10:00:00Z","properties":{"name":"iPhone","platform":"ios","pushKey":"v1.k","notifies":["replies","requests"]}}"#

    let transport = StubTransport()
    let storage = MemoryStorage()
    let sessions: SessionStore
    let device = ScriptedPush()
    private let lock = NSLock()
    private var instant = Date(timeIntervalSince1970: 1_763_459_000)
    private var minted = 0

    init(app: Bool = true, build: String? = "https://push.example.com") {
        sessions = SessionStore(storage: storage)
        self.build = build
        let session = Session(
            server: URL(string: "https://s.example.com")!, repository: "r.example.com", secret: "secret", tokenID: Self.tokenID,
            clientID: app ? Self.client : nil, scopes: app ? ["full"] : nil)
        try? storage.write(try! JSONEncoder().encode(session), account: sessions.account)
        publish(#","push":{"relay":"https://push.example.com"}"#)
        serve(relay: "https://push.example.com")
        transport.on("POST", "/v1/register", json: #"{"pushKey":"v1.k"}"#)
        transport.on("PUT", Self.devicePath, status: 201, json: Self.record)
        transport.on("GET", Self.devicePath, json: Self.record)
        transport.on("DELETE", Self.devicePath, status: 204)
    }

    let build: String?

    var registrar: PushRegistrar {
        PushRegistrar(
            sessions: sessions, device: device, transport: transport, buildRelay: build, clientID: Self.client,
            now: { [self] in lock.withLock { instant } },
            generation: { [self] in
                lock.withLock {
                    minted += 1
                    return "g\(minted)"
                }
            })
    }

    func pass(_ seconds: TimeInterval) { lock.withLock { instant += seconds } }

    func publish(_ push: String) {
        transport.replace(
            "GET", "/.well-known/substrate/authority.json",
            json: #"{"authority":"example.com","apps":[{"client_id":"\#(Self.client)","name":"Chat"\#(push)}]}"#)
    }

    func serve(relay: String) {
        transport.replace("GET", "/api/v1/push/key", json: #"{"publicKey":"q1x3LkpublicKeyOfThisToken0000000000000000","relay":"\#(relay)"}"#)
    }

    func sent(_ method: String, _ path: String) -> [URLRequest] { transport.sent(method, path) }

    static let ask = PushAsk(listed: true, notifies: ["replies", "requests"], permitted: true)
}

@Suite("Registering with the publisher's relay")
struct PushRegistrarTests {
    @Test func anInstallRegistersWhereBuildDocumentAndServerAgree() async throws {
        let world = PushWorld()
        let facts = await world.registrar.sync(PushWorld.ask)
        #expect(facts.standing == .registered && facts.held && facts.environment == .development)
        #expect(facts.deviceToken == String(repeating: "ab", count: 32))
        let register = try #require(world.sent("POST", "/v1/register").first)
        #expect(register.value(forHTTPHeaderField: "Authorization") == nil)
        let body = try #require(try JSONSerialization.jsonObject(with: register.httpBody ?? Data()) as? [String: String])
        #expect(body["generation"] == "g1" && body["environment"] == "development" && body["topic"] == "dev.reamde.substrate.chat")
        #expect(body["publicKey"] == "q1x3LkpublicKeyOfThisToken0000000000000000" && body["platform"] == "apns")
        let put = try #require(world.sent("PUT", PushWorld.devicePath).first)
        #expect(put.value(forHTTPHeaderField: "Authorization") == "Bearer secret")
        let properties = try #require((try JSONSerialization.jsonObject(with: put.httpBody ?? Data()) as? [String: Any])?["properties"] as? [String: Any])
        #expect(properties["pushKey"] as? String == "v1.k" && properties["platform"] as? String == "ios" && properties["name"] as? String == "iPhone")
        #expect(properties["notifies"] as? [String] == ["replies", "requests"])
        #expect(properties["token"] == nil && properties["person"] == nil)
        // The server's own fixture of that put: the same body but the key.
        let fixture = try #require(try PushWire.object("device-put", in: "pushserver")["create"] as? [String: Any])
        var expected = try #require((fixture["body"] as? [String: Any])?["properties"] as? [String: Any])
        expected["pushKey"] = "v1.k"
        #expect(NSDictionary(dictionary: properties).isEqual(to: expected))
        let state = try #require(await world.registrar.state())
        #expect(state.current?.generation == "g1" && state.pending.isEmpty && state.tokenID == PushWorld.tokenID)
        #expect(state.current?.pushKey == "v1.k")
        #expect(await world.registrar.accepts("g1"))
        #expect(!(await world.registrar.accepts("g0")))
    }

    @Test func aRelayWithATrailingSlashIsTheSameRelay() async {
        let world = PushWorld(build: "https://push.example.com/")
        world.serve(relay: "https://push.example.com")
        world.publish(#","push":{"relay":"https://push.example.com/"}"#)
        #expect(await world.registrar.sync(PushWorld.ask).standing == .registered)
    }

    @Test func nothingRegistersWhereOneOfTheThreeNamesAnotherRelay() async {
        for differing in ["build", "document", "server"] {
            let world = PushWorld(build: differing == "build" ? "https://other.example.com" : "https://push.example.com")
            if differing == "document" { world.publish(#","push":{"relay":"https://other.example.com"}"#) }
            if differing == "server" { world.serve(relay: "https://other.example.com") }
            let facts = await world.registrar.sync(PushWorld.ask)
            #expect(facts.standing == .unmatched && !facts.held, "\(differing)")
            #expect(world.sent("POST", "/v1/register").isEmpty && world.sent("PUT", PushWorld.devicePath).isEmpty, "\(differing)")
        }
    }

    @Test func aRegistrationHeldIsGivenUpWhenTheRelaysNoLongerAgree() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.publish(#","push":{"relay":"https://moved.example.com"}"#)
        let facts = await world.registrar.sync(PushWorld.ask)
        #expect(facts.standing == .unmatched && !facts.held)
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 1)
        let state = try #require(await world.registrar.state())
        #expect(state.current == nil && state.pending.isEmpty && !state.held)
        // A push already on its way still opens its chat for a day.
        #expect(await world.registrar.accepts("g1"))
        world.pass(PushState.retirement + 1)
        #expect(!(await world.registrar.accepts("g1")))
    }

    @Test func aDocumentOrAServerThatCannotBeReadNowChangesNothing() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.transport.replace("GET", "/.well-known/substrate/authority.json", status: 502)
        let facts = await world.registrar.sync(PushWorld.ask)
        #expect(facts.standing == .unavailable && facts.held)
        #expect(world.sent("DELETE", PushWorld.devicePath).isEmpty)

        let fresh = PushWorld()
        fresh.transport.replace("GET", "/api/v1/push/key", status: 503, headers: ["Retry-After": "60"])
        let unread = await fresh.registrar.sync(PushWorld.ask)
        #expect(unread.standing == .unavailable && !unread.held)
        #expect(fresh.sent("POST", "/v1/register").isEmpty)
    }

    @Test func aDocumentThatNamesNoRelayOrAServerThatHasNoneGivesUp() async throws {
        for (case_, refusal) in [("document", ""), ("no_relay", "no_relay"), ("push_off", "push_off")] {
            let world = PushWorld()
            #expect(await world.registrar.sync(PushWorld.ask).held)
            if case_ == "document" {
                world.transport.replace("GET", "/.well-known/substrate/authority.json", status: 404)
            } else {
                world.transport.replace(
                    "GET", "/api/v1/push/key", status: 403, json: #"{"error":{"code":"forbidden","reason":"\#(refusal)","message":"no"}}"#)
            }
            let facts = await world.registrar.sync(PushWorld.ask)
            #expect(facts.standing == (case_ == "push_off" ? .off : .noRelay) && !facts.held, "\(case_)")
            #expect(world.sent("DELETE", PushWorld.devicePath).count == 1, "\(case_)")
        }
    }

    @Test func nothingIsRegisteredWithBothSwitchesOffOrNoPermission() async {
        let off = PushWorld()
        let facts = await off.registrar.sync(PushAsk(listed: true, notifies: [], permitted: true))
        #expect(facts.standing == .switchedOff && !facts.held)
        #expect(off.transport.requests.isEmpty && off.device.asks == 0)

        let held = PushWorld()
        #expect(await held.registrar.sync(PushWorld.ask).held)
        #expect(await held.registrar.sync(PushAsk(listed: true, notifies: [], permitted: true)).standing == .switchedOff)
        #expect(held.sent("DELETE", PushWorld.devicePath).count == 1)

        let refused = PushWorld()
        #expect(await refused.registrar.sync(PushWorld.ask).held)
        #expect(await refused.registrar.sync(PushAsk(listed: true, notifies: ["replies"], permitted: false)).standing == .notPermitted)
        #expect(refused.sent("DELETE", PushWorld.devicePath).count == 1)
    }

    @Test func aServerWithoutPushAPasswordSignInAndALocalBuildRegisterNowhere() async {
        let unlisted = PushWorld()
        #expect(await unlisted.registrar.sync(PushAsk(listed: false, notifies: ["replies"], permitted: true)).standing == .notListed)
        let password = PushWorld(app: false)
        #expect(await password.registrar.sync(PushWorld.ask).standing == .noRelay)
        for build in [nil, "none", ""] as [String?] {
            #expect(await PushWorld(build: build).registrar.sync(PushWorld.ask).standing == .noRelay)
        }
        for world in [unlisted, password] { #expect(world.sent("POST", "/v1/register").isEmpty) }
    }

    @Test func aRegistrationIsMadeAgainWhenSomethingItWasMadeFromChanged() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        // Nothing changed: the record is read, and nothing is made.
        #expect(await world.registrar.sync(PushWorld.ask).held)
        #expect(world.sent("POST", "/v1/register").count == 1)
        #expect(world.sent("GET", PushWorld.devicePath).count == 1)

        world.device.set(token: String(repeating: "cd", count: 32))
        _ = await world.registrar.sync(PushWorld.ask)
        #expect(world.sent("POST", "/v1/register").count == 2)
        #expect(await world.registrar.state()?.current?.generation == "g2")

        _ = await world.registrar.sync(PushAsk(listed: true, notifies: ["replies"], permitted: true))
        #expect(world.sent("POST", "/v1/register").count == 3)

        world.pass(PushRegistrar.renewal - 60)
        _ = await world.registrar.sync(PushAsk(listed: true, notifies: ["replies"], permitted: true))
        #expect(world.sent("POST", "/v1/register").count == 3)
        world.pass(120)
        _ = await world.registrar.sync(PushAsk(listed: true, notifies: ["replies"], permitted: true))
        #expect(world.sent("POST", "/v1/register").count == 4)

        // The record is gone: it is put again.
        world.transport.replace("GET", PushWorld.devicePath, status: 404, json: #"{"error":{"code":"not_found","message":"no"}}"#)
        _ = await world.registrar.sync(PushAsk(listed: true, notifies: ["replies"], permitted: true))
        #expect(world.sent("POST", "/v1/register").count == 5)
    }

    @Test func dueReadsEveryInput() {
        let made = PushState.Registration(
            generation: "g", relay: "https://push.example.com", publicKey: "k", deviceToken: "t", environment: .development,
            notifies: ["replies", "requests"], registeredAt: Date(timeIntervalSince1970: 0))
        let at = Date(timeIntervalSince1970: 3600)
        func due(
            relay: String = "https://push.example.com/", key: String = "k", token: String = "t", environment: PushEnvironment = .development,
            notifies: [String] = ["requests", "replies"], now: Date = at
        ) -> Bool {
            PushRegistrar.due(made, relay: relay, publicKey: key, deviceToken: token, environment: environment, notifies: notifies, now: now)
        }
        #expect(!due())
        #expect(due(relay: "https://other.example.com"))
        #expect(due(key: "k2"))
        #expect(due(token: "t2"))
        #expect(due(environment: .production))
        #expect(due(notifies: ["replies"]))
        #expect(due(now: Date(timeIntervalSince1970: PushRegistrar.renewal)))
    }

    @Test func aNewGenerationIsCurrentOnlyOnceTheServerTookTheDevice() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.device.set(token: String(repeating: "ef", count: 32))
        world.transport.replace("PUT", PushWorld.devicePath, status: 500, json: #"{"error":{"code":"internal","message":"no"}}"#)
        let facts = await world.registrar.sync(PushWorld.ask)
        #expect(facts.standing == .unavailable && facts.held)
        let state = try #require(await world.registrar.state())
        #expect(state.current?.generation == "g1" && state.pending.map(\.generation) == ["g2"])
        // Either is accepted while the new one waits: its put may have landed.
        #expect(await world.registrar.accepts("g1"))
        #expect(await world.registrar.accepts("g2"))

        // The same attempt is put again, under the key the relay sealed for
        // it: no second registration. 201 the first time, 200 after.
        world.transport.replace("PUT", PushWorld.devicePath, status: 200, json: PushWorld.record)
        #expect(await world.registrar.sync(PushWorld.ask).standing == .registered)
        #expect(world.sent("POST", "/v1/register").count == 2)
        let after = try #require(await world.registrar.state())
        #expect(after.current?.generation == "g2" && after.pending.isEmpty)
        // The one it replaced stays accepted for an event id's day, no longer.
        #expect(after.previous.map(\.generation) == ["g1"])
        let (first, second) = (await world.registrar.accepts("g1"), await world.registrar.accepts("g2"))
        #expect(first && second)
        world.pass(PushState.retirement + 1)
        #expect(!(await world.registrar.accepts("g1")))
    }

    /// The first put lands and its answer is lost; the app is launched again
    /// and the put fails once more; then a new attempt fails at the relay.
    /// The server pushes under the first attempt's generation all along: it
    /// stays accepted, and the install counts as registered, so the app posts
    /// nothing of its own beside the pushes.
    @Test func aPutWhoseAnswerWasLostStaysHeldThroughARelaunchAndAFailedRetry() async throws {
        let world = PushWorld()
        world.transport.replace("PUT", PushWorld.devicePath, status: 500, json: #"{"error":{"code":"internal","message":"no"}}"#)
        let lost = await world.registrar.sync(PushWorld.ask)
        #expect(lost.standing == .unavailable && lost.held)
        #expect(await world.registrar.accepts("g1"))
        #expect(await world.registrar.facts().held)

        // A relaunch: another registrar over the same Keychain.
        world.transport.replace("PUT", PushWorld.devicePath, status: 502, json: #"{"error":{"code":"upstream","message":"no"}}"#)
        let relaunched = world.registrar
        let retried = await relaunched.sync(PushWorld.ask)
        #expect(retried.standing == .unavailable && retried.held)
        #expect(world.sent("POST", "/v1/register").count == 1)
        let puts = world.sent("PUT", PushWorld.devicePath)
        #expect(puts.count == 2 && puts[0].httpBody == puts[1].httpBody)
        #expect(await relaunched.accepts("g1"))

        // Something changed, and the new attempt never reaches the server.
        world.device.set(token: String(repeating: "ef", count: 32))
        world.transport.replace("POST", "/v1/register", status: 502, json: #"{"error":{"code":"upstream","message":"no"}}"#)
        let failed = await relaunched.sync(PushWorld.ask)
        #expect(failed.standing == .unavailable && failed.held)
        let state = try #require(await relaunched.state())
        #expect(state.pending.map(\.generation) == ["g1"] && state.current == nil)
        #expect(await relaunched.accepts("g1"))
        #expect(!(await relaunched.accepts("g2")))

        // Then it goes through: the new one is current, and the first is
        // accepted for a day.
        world.transport.replace("POST", "/v1/register", json: #"{"pushKey":"v1.k2"}"#)
        world.transport.replace("PUT", PushWorld.devicePath, status: 200, json: PushWorld.record)
        #expect(await relaunched.sync(PushWorld.ask).standing == .registered)
        let after = try #require(await relaunched.state())
        #expect(after.current?.generation == "g3" && after.current?.pushKey == "v1.k2" && after.pending.isEmpty)
        #expect(await relaunched.accepts("g1"))
    }

    /// The device put is held to the server's fixture both ways: its create
    /// (`201`) and an update (`200`), which carries at least what the
    /// fixture's update does.
    @Test func thePutIsTheServersCreateThenItsUpdate() async throws {
        let fixture = try PushWire.object("device-put", in: "pushserver")
        let update = try #require(fixture["update"] as? [String: Any])
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).standing == .registered)
        world.device.set(token: String(repeating: "cd", count: 32))
        world.transport.replace("POST", "/v1/register", json: #"{"pushKey":"v1.Rm9yd2FyZA"}"#)
        world.transport.replace("PUT", PushWorld.devicePath, status: try #require(update["status"] as? Int), json: PushWorld.record)
        #expect(await world.registrar.sync(PushWorld.ask).standing == .registered)
        let second = try #require(world.sent("PUT", PushWorld.devicePath).last)
        let sent = try #require((try JSONSerialization.jsonObject(with: second.httpBody ?? Data()) as? [String: Any])?["properties"] as? [String: Any])
        let expected = try #require((update["body"] as? [String: Any])?["properties"] as? [String: Any])
        for (key, value) in expected { #expect(NSDictionary(dictionary: [key: value]).isEqual(to: [key: sent[key] as Any]), "\(key)") }
        #expect(await world.registrar.state()?.current?.pushKey == "v1.Rm9yd2FyZA")
    }

    @Test func aRelayThatRefusesTheAppStopsAndOneThatFailsKeepsWhatWasHeld() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.device.set(token: String(repeating: "ef", count: 32))
        world.transport.replace("POST", "/v1/register", status: 502, json: #"{"error":{"code":"upstream","message":"no"}}"#)
        let failed = await world.registrar.sync(PushWorld.ask)
        #expect(failed.standing == .unavailable && failed.held)
        #expect(await world.registrar.state()?.pending.isEmpty == true)

        world.transport.replace("POST", "/v1/register", status: 403, json: #"{"error":{"code":"environment_refused","message":"no"}}"#)
        let refused = await world.registrar.sync(PushWorld.ask)
        #expect(refused.standing == .refused && !refused.held)
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 1)
    }

    @Test func signingOutDeletesTheDeviceAndForgetsBothGenerations() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.device.set(token: String(repeating: "ef", count: 32))
        world.transport.replace("PUT", PushWorld.devicePath, status: 500, json: #"{"error":{"code":"internal","message":"no"}}"#)
        _ = await world.registrar.sync(PushWorld.ask)
        let session = try #require(await world.sessions.load())
        await world.registrar.signOut(session)
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 1)
        #expect(await world.registrar.state() == nil)
        let (current, pending) = (await world.registrar.accepts("g1"), await world.registrar.accepts("g2"))
        #expect(!current && !pending)
    }

    /// A delete the server does not answer is left to it, which deletes the
    /// device with its token: the sign-out forgets everything all the same.
    @Test func aSignOutWhoseDeleteFailsForgetsEverythingAllTheSame() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.transport.replace("DELETE", PushWorld.devicePath, status: 502, json: #"{"error":{"code":"upstream","message":"no"}}"#)
        await world.registrar.signOut(try #require(await world.sessions.load()))
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 1)
        #expect(await world.registrar.state() == nil)
        #expect(!(await world.registrar.accepts("g1")))
    }

    /// Sign Out never waits behind an attempt stuck on the network: the
    /// attempt is cancelled, saves nothing, and no attempt starts for the
    /// sign-in being signed out.
    @Test func aSignOutCancelsTheAttemptInFlightAndNoneStartsAfter() async throws {
        let world = PushWorld()
        let slow = SlowTransport(world.transport, holding: "/v1/register")
        let registrar = PushRegistrar(
            sessions: world.sessions, device: world.device, transport: slow, buildRelay: "https://push.example.com", clientID: PushWorld.client)
        let attempt = Task { await registrar.sync(PushWorld.ask) }
        while !slow.holding { try await Task.sleep(for: .milliseconds(10)) }
        let started = ContinuousClock.now
        await registrar.signOut(try #require(await world.sessions.load()))
        #expect(ContinuousClock.now - started < .seconds(3))
        _ = await attempt.value
        #expect(await registrar.state() == nil)
        #expect(world.sent("PUT", PushWorld.devicePath).isEmpty)
        let asked = world.transport.requests.count
        #expect(await registrar.sync(PushWorld.ask).standing == .unknown)
        #expect(world.transport.requests.count == asked)
    }

    @Test func aRedirectedDocumentOrAnUnknownRefusalIsNoAnswerNow() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.transport.replace("GET", "/.well-known/substrate/authority.json", status: 302, headers: ["Location": "https://elsewhere.example.com/x"])
        let redirected = await world.registrar.sync(PushWorld.ask)
        #expect(redirected.standing == .unavailable && redirected.held)
        #expect(world.transport.requests.allSatisfy { $0.url?.host != "elsewhere.example.com" })

        world.publish(#","push":{"relay":"https://push.example.com"}"#)
        world.transport.replace("GET", "/api/v1/push/key", status: 403, json: #"{"error":{"code":"forbidden","reason":"something_new","message":"no"}}"#)
        let unknown = await world.registrar.sync(PushWorld.ask)
        #expect(unknown.standing == .unavailable && unknown.held)
        #expect(world.sent("DELETE", PushWorld.devicePath).isEmpty)
    }

    /// A push state the Keychain does not answer for is no absent one: the
    /// attempt stops before the network and writes nothing over it.
    @Test func aPushStateThatCannotBeReadIsKeptAndNothingIsMadeOverIt() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        let kept = try #require(world.storage.stored(account: world.registrar.account))
        world.device.set(token: String(repeating: "cd", count: 32))
        world.storage.failReads(of: world.registrar.account, with: MemoryStorage.locked)
        let asked = world.transport.requests.count
        let facts = await world.registrar.sync(PushWorld.ask)
        #expect(facts.standing == .unreadable)
        #expect(await world.registrar.facts().standing == .unreadable)
        #expect(world.transport.requests.count == asked)
        world.storage.failReads(of: world.registrar.account, with: nil)
        #expect(world.storage.stored(account: world.registrar.account) == kept)
    }

    /// The relay sealed a key and the Keychain would not keep it: no put is
    /// sent for it, and the install does not count as registered.
    @Test func aKeyTheKeychainWouldNotKeepIsNeverPutNorHeld() async throws {
        let world = PushWorld()
        let hooked = HookTransport(world.transport) { request in
            if request.url.map(StubTransport.path) == "/v1/register" {
                world.storage.failWrites(of: world.registrar.account, with: MemoryStorage.locked)
            }
        }
        let registrar = PushRegistrar(
            sessions: world.sessions, device: world.device, transport: hooked, buildRelay: "https://push.example.com", clientID: PushWorld.client)
        let facts = await registrar.sync(PushWorld.ask)
        #expect(facts.standing == .unavailable && !facts.held)
        #expect(world.sent("POST", "/v1/register").count == 1)
        #expect(world.sent("PUT", PushWorld.devicePath).isEmpty)
        world.storage.failWrites(of: world.registrar.account, with: nil)
        #expect(await registrar.state()?.held == false)
    }

    /// A sign-out that fails leaves the person signed in: pushes come back
    /// at the next attempt.
    @Test func aSignOutThatFailsLetsPushesRegisterAgain() async throws {
        let world = PushWorld()
        let registrar = world.registrar
        #expect(await registrar.sync(PushWorld.ask).held)
        let session = try #require(await world.sessions.load())
        await registrar.signOut(session)
        #expect(await registrar.sync(PushWorld.ask).standing == .unknown)
        registrar.signOutFailed(session)
        #expect(await registrar.sync(PushWorld.ask).standing == .registered)
    }

    /// The sign-out's bounds hold against work that ignores its cancellation.
    @Test func theSignOutsBoundsAnswerAtTheirLimitWhateverTheWorkDoes() async throws {
        let started = ContinuousClock.now
        let finished = await PushControl.within(.milliseconds(200)) { await Self.uncancellable(seconds: 3) }
        #expect(!finished)
        #expect(ContinuousClock.now - started < .seconds(2))

        let world = PushWorld()
        let stuck = HookTransport(world.transport) { request in
            if request.httpMethod == "DELETE" || request.url.map(StubTransport.path) == "/v1/register" { await Self.uncancellable(seconds: 12) }
        }
        let registrar = PushRegistrar(
            sessions: world.sessions, device: world.device, transport: stuck, buildRelay: "https://push.example.com", clientID: PushWorld.client)
        // Registered once over the plain transport, so the sign-out has a
        // device to delete.
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.device.set(token: String(repeating: "cd", count: 32))
        let attempt = Task { await registrar.sync(PushWorld.ask) }
        while world.sent("POST", "/v1/register").count < 2 { try await Task.sleep(for: .milliseconds(10)) }
        let signing = ContinuousClock.now
        await registrar.signOut(try #require(await world.sessions.load()))
        #expect(ContinuousClock.now - signing < .seconds(9))
        #expect(await registrar.state() == nil)
        attempt.cancel()
    }

    /// A put a sign-out overtakes is never sent: the sign-out's delete is
    /// the last word.
    @Test func aSignOutUnderWayRefusesThePutOfAnAttempt() async throws {
        let world = PushWorld()
        world.transport.replace("PUT", PushWorld.devicePath, status: 500, json: #"{"error":{"code":"internal","message":"no"}}"#)
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.transport.replace("PUT", PushWorld.devicePath, status: 200, json: PushWorld.record)
        let puts = world.sent("PUT", PushWorld.devicePath).count
        let session = try #require(await world.sessions.load())
        let box = Box<PushRegistrar>()
        let hooked = HookTransport(world.transport) { request in
            // The person signs out while the attempt reads its key.
            // An attempt that does not stop for its cancellation is stood in
            // for: what refuses its put is the sign-in leaving.
            if request.url.map(StubTransport.path) == "/api/v1/push/key" { box.value?.control.leave(session.tokenID, cancelling: false) }
        }
        // On the world's clock, so the attempt is still the one wanted and
        // is put again rather than made anew.
        let racing = PushRegistrar(
            sessions: world.sessions, device: world.device, transport: hooked, buildRelay: "https://push.example.com", clientID: PushWorld.client,
            now: { Date(timeIntervalSince1970: 1_763_459_000) })
        box.value = racing
        _ = await racing.sync(PushWorld.ask)
        #expect(world.sent("PUT", PushWorld.devicePath).count == puts)
    }

    static func uncancellable(seconds: Double) async {
        await withCheckedContinuation { continuation in
            DispatchQueue.global().asyncAfter(deadline: .now() + seconds) { continuation.resume() }
        }
    }

    @Test func anotherSignInsRegistrationIsForgotten() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        var other = try #require(await world.sessions.load())
        other.tokenID = "0b5e1a7c33"
        try await world.sessions.save(other)
        #expect(!(await world.registrar.accepts("g1")))
        #expect(await world.registrar.facts().standing == .unknown)
    }
}
