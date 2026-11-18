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

/// A value a test's concurrent work reads, under a lock.
final class Holder<T>: @unchecked Sendable {
    private let lock = NSLock()
    private var held: T?

    var value: T? {
        get { lock.withLock { held } }
        set { lock.withLock { held = newValue } }
    }
}

/// A switch a test's concurrent work reads, under a lock.
final class Gate: @unchecked Sendable {
    private let lock = NSLock()
    private var open: Bool

    init(open: Bool) { self.open = open }

    var isOpen: Bool { lock.withLock { open } }
    func close() { lock.withLock { open = false } }
}

/// The device record as the server holds it: the key the last put that
/// landed wrote. A put whose answer is lost lands later, when the test says.
final class DeviceServer: HTTPTransport, @unchecked Sendable {
    let inner: StubTransport
    let path: String
    private let lock = NSLock()
    private var held: String?
    private var losing = false
    private var late: [String] = []

    init(_ inner: StubTransport, path: String) {
        self.inner = inner
        self.path = path
    }

    /// The key the record holds now.
    var key: String? { lock.withLock { held } }

    /// The next put reaches the server but its answer is lost, and it has
    /// not landed yet.
    func loseNextPut() { lock.withLock { losing = true } }

    /// The puts whose answers were lost land now, in the order they were
    /// sent.
    func landLate() {
        lock.withLock {
            if let last = late.last { held = last }
            late = []
        }
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        if request.httpMethod == "PUT", request.url.map(StubTransport.path) == path {
            let body = (request.httpBody.flatMap { try? JSONSerialization.jsonObject(with: $0) } as? [String: Any])
            let key = (body?["properties"] as? [String: Any])?["pushKey"] as? String ?? ""
            let lost = lock.withLock { () -> Bool in
                if losing {
                    losing = false
                    late.append(key)
                    return true
                }
                held = key
                return false
            }
            if lost {
                _ = try await inner.data(for: request)
                throw URLError(.timedOut)
            }
        }
        return try await inner.data(for: request)
    }
}

/// A secure store over memory that runs `hook` as each write lands, as a
/// Keychain whose write suspends: the hook may move a test's clock.
final class HookStorage: SessionStorage, @unchecked Sendable {
    let inner: MemoryStorage
    let hook: @Sendable (Data, String) -> Void

    init(_ inner: MemoryStorage, hook: @escaping @Sendable (Data, String) -> Void) {
        self.inner = inner
        self.hook = hook
    }

    var location: String { inner.location }
    func read(account: String) async -> StoredItem { inner.read(account: account) }

    func write(_ data: Data, account: String) async throws {
        try inner.write(data, account: account)
        hook(data, account)
    }

    func delete(account: String) async throws { try inner.delete(account: account) }
}

/// A count a test's concurrent work keeps.
final class Counter: @unchecked Sendable {
    private let lock = NSLock()
    private var counted = 0

    @discardableResult
    func next() -> Int {
        lock.withLock {
            counted += 1
            return counted
        }
    }

    var count: Int { lock.withLock { counted } }
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
    let limit: Double
    private let lock = NSLock()
    private var held = false

    /// - Parameter limit: the request's own time limit, as the push
    ///   transport's: the request is answered nothing until then, whatever
    ///   cancels it, and then times out.
    init(_ inner: StubTransport, holding path: String, for limit: Double) {
        self.inner = inner
        self.path = path
        self.limit = limit
    }

    var holding: Bool { lock.withLock { held } }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        if request.url.map(StubTransport.path) == path {
            lock.withLock { held = true }
            await PushRegistrarTests.uncancellable(seconds: limit)
            throw URLError(.timedOut)
        }
        return try await inner.data(for: request)
    }
}

/// A transport that writes down when each request starts and ends, holding
/// those that `stall` names for as long as it says: whatever cancels them,
/// or, with `cancellable`, failing at once when cancelled, as a URLSession
/// request does.
final class OrderedTransport: HTTPTransport, @unchecked Sendable {
    let inner: StubTransport
    let stall: @Sendable (URLRequest) -> Double?
    let cancellable: Bool
    private let lock = NSLock()
    private var log: [String] = []

    init(_ inner: StubTransport, cancellable: Bool = false, stall: @escaping @Sendable (URLRequest) -> Double?) {
        self.inner = inner
        self.cancellable = cancellable
        self.stall = stall
    }

    var events: [String] { lock.withLock { log } }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let name = "\(request.httpMethod ?? "GET") \(request.url.map(StubTransport.path) ?? "")"
        lock.withLock { log.append("start " + name) }
        defer { lock.withLock { log.append("end " + name) } }
        if let seconds = stall(request) {
            if cancellable {
                try await Task.sleep(for: .seconds(seconds))
            } else {
                await PushRegistrarTests.uncancellable(seconds: seconds)
            }
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

    /// The world's clock and its generations, for a registrar over another
    /// transport.
    var clock: Date { lock.withLock { instant } }

    func mint() -> String {
        lock.withLock {
            minted += 1
            return "g\(minted)"
        }
    }

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
        // Past the renewal, and past the window of the put before it.
        world.pass(180)
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
        #expect(await Self.forgotten(world.registrar))
        let (current, pending) = (await world.registrar.accepts("g1"), await world.registrar.accepts("g2"))
        #expect(!current && !pending)
    }

    /// A delete the server does not answer leaves the registration held:
    /// the sign-in leaving accepts nothing, and should the app end before
    /// the session is removed, the next launch deletes it again. Once the
    /// session is, the server deletes the device with its token.
    @Test func aSignOutWhoseDeleteFailsKeepsTheRegistrationToDeleteAgain() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.pass(300)
        world.transport.replace("DELETE", PushWorld.devicePath, status: 502, json: #"{"error":{"code":"upstream","message":"no"}}"#)
        let registrar = world.registrar
        let session = try #require(await world.sessions.load())
        await registrar.signOut(session)
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 1)
        #expect(!(await registrar.accepts("g1")))
        #expect(await registrar.state()?.held == true)
        await registrar.retired(session)
        #expect(await registrar.state()?.held == true)
        // The app ended before the session was removed: still signed in.
        world.transport.replace("DELETE", PushWorld.devicePath, status: 204)
        let relaunched = world.registrar
        _ = await relaunched.sync(PushAsk(listed: true, notifies: [], permitted: true))
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 2)
        #expect(await relaunched.state()?.held == false)
    }

    /// Sign Out waits for an attempt stuck on the network at most its bound
    /// and stops nothing: the attempt ends at its request's own limit,
    /// saves and puts nothing for the sign-in leaving, and none starts for
    /// it after.
    @Test func aSignOutWaitsAtMostItsBoundAndNoAttemptStartsForItAfter() async throws {
        let world = PushWorld()
        let slow = SlowTransport(world.transport, holding: "/v1/register", for: 2)
        let registrar = PushRegistrar(
            sessions: world.sessions, device: world.device, transport: slow, buildRelay: "https://push.example.com", clientID: PushWorld.client)
        registrar.control.signOutBound = .milliseconds(300)
        let attempt = Task { await registrar.sync(PushWorld.ask) }
        #expect(await Self.eventually { slow.holding })
        let started = ContinuousClock.now
        await registrar.signOut(try #require(await world.sessions.load()))
        #expect(ContinuousClock.now - started < .seconds(1))
        _ = await attempt.value
        // Queued after the sign-out's forgetting, so it runs after it.
        let asked = world.transport.requests.count
        #expect(await registrar.sync(PushWorld.ask).standing == .unknown)
        #expect(world.transport.requests.count == asked)
        #expect(await Self.forgotten(registrar))
        #expect(world.sent("PUT", PushWorld.devicePath).isEmpty)
    }

    /// A sign-out's delete starts only once the put in flight has ended:
    /// one queue, each piece of work to its end, in order.
    @Test func aSignOutsDeleteStartsOnlyOnceThePutInFlightHasEnded() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.device.set(token: String(repeating: "cd", count: 32))
        let ordered = OrderedTransport(world.transport) { $0.httpMethod == "PUT" ? 1.5 : nil }
        let registrar = PushRegistrar(
            sessions: world.sessions, device: world.device, transport: ordered, buildRelay: "https://push.example.com", clientID: PushWorld.client)
        registrar.control.signOutBound = .milliseconds(300)
        let attempt = Task { await registrar.sync(PushWorld.ask) }
        #expect(await Self.eventually { ordered.events.contains("start PUT \(PushWorld.devicePath)") })
        let started = ContinuousClock.now
        await registrar.signOut(try #require(await world.sessions.load()))
        #expect(ContinuousClock.now - started < .seconds(1))
        _ = await attempt.value
        #expect(await Self.eventually { ordered.events.contains("end DELETE \(PushWorld.devicePath)") })
        let events = ordered.events
        let putEnded = try #require(events.firstIndex(of: "end PUT \(PushWorld.devicePath)"))
        let deleteStarted = try #require(events.firstIndex(of: "start DELETE \(PushWorld.devicePath)"))
        #expect(putEnded < deleteStarted)
        #expect(await Self.forgotten(registrar))
    }

    /// A caller that stops waiting does not stop its attempt: the put it
    /// sent ends and is kept.
    @Test func aCallerThatStopsWaitingDoesNotStopTheAttempt() async throws {
        let world = PushWorld()
        let ordered = OrderedTransport(world.transport, cancellable: true) { $0.httpMethod == "PUT" ? 1 : nil }
        let registrar = PushRegistrar(
            sessions: world.sessions, device: world.device, transport: ordered, buildRelay: "https://push.example.com", clientID: PushWorld.client)
        let caller = Task { await registrar.sync(PushWorld.ask) }
        #expect(await Self.eventually { ordered.events.contains("start PUT \(PushWorld.devicePath)") })
        caller.cancel()
        let facts = await caller.value
        #expect(facts.standing == .registered)
        #expect(await registrar.state()?.current != nil)
    }

    /// Every request the registrar sends goes over a session that ends it
    /// within 20 seconds, and a busy server is not waited out: an attempt
    /// always ends, so the queue moves on.
    @Test func everyRequestEndsWithinItsLimitAndABusyServerIsNotWaitedOut() async throws {
        let plain = PushRegistrar(sessions: SessionStore(storage: MemoryStorage()), device: nil, buildRelay: nil, clientID: "")
        let transport = try #require(plain.transport as? URLSessionTransport)
        #expect(transport.session.configuration.timeoutIntervalForResource == 20)
        #expect(transport.session.configuration.timeoutIntervalForRequest == 20)
        let world = PushWorld()
        world.transport.replace(
            "PUT", PushWorld.devicePath, status: 503, headers: ["Retry-After": "5"], json: #"{"error":{"code":"unavailable","message":"busy"}}"#)
        let started = ContinuousClock.now
        let facts = await world.registrar.sync(PushWorld.ask)
        #expect(facts.standing == .unavailable && ContinuousClock.now - started < .seconds(2))
        #expect(world.sent("PUT", PushWorld.devicePath).count == 1)
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
            if request.httpMethod == "DELETE" || request.url.map(StubTransport.path) == "/v1/register" { await Self.uncancellable(seconds: 3) }
        }
        let registrar = PushRegistrar(
            sessions: world.sessions, device: world.device, transport: stuck, buildRelay: "https://push.example.com", clientID: PushWorld.client)
        registrar.control.signOutBound = .milliseconds(300)
        // Registered once over the plain transport, so the sign-out has a
        // device to delete.
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.device.set(token: String(repeating: "cd", count: 32))
        let attempt = Task { await registrar.sync(PushWorld.ask) }
        #expect(await Self.eventually { world.transport.requests.count(where: { $0.url.map(StubTransport.path) == "/api/v1/push/key" }) >= 2 })
        let signing = ContinuousClock.now
        await registrar.signOut(try #require(await world.sessions.load()))
        #expect(ContinuousClock.now - signing < .seconds(1))
        // Nothing is accepted for the sign-in from now on; its delete and
        // its forgetting run when the attempt before them has ended.
        #expect(await registrar.verdict("g1") == .refused)
        _ = await attempt.value
        #expect(await Self.forgotten(registrar))
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 1)
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
            // The person signs out while the attempt reads its key: what
            // refuses its put is the sign-in leaving.
            if request.url.map(StubTransport.path) == "/api/v1/push/key" { box.value?.control.leave(session.tokenID) }
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

    /// A sign-out queues behind an attempt stuck on the network and returns
    /// within its bound; the next sign-in's attempt runs once the stuck one
    /// has ended and the sign-out has forgotten its registrations, and
    /// registers.
    @Test func theNextSignInRegistersOnceWhatWasQueuedBeforeItHasEnded() async throws {
        let world = PushWorld()
        let registers = Counter()
        let stalling = HookTransport(world.transport) { request in
            if request.url.map(StubTransport.path) == "/v1/register", registers.next() == 1 { await Self.uncancellable(seconds: 2) }
        }
        let registrar = PushRegistrar(
            sessions: world.sessions, device: world.device, transport: stalling, buildRelay: "https://push.example.com", clientID: PushWorld.client)
        registrar.control.signOutBound = .milliseconds(300)
        let stalled = Task { await registrar.sync(PushWorld.ask) }
        #expect(await Self.eventually { registers.count >= 1 })
        let signing = ContinuousClock.now
        await registrar.signOut(try #require(await world.sessions.load()))
        #expect(ContinuousClock.now - signing < .seconds(1))
        // The next sign-in, with a token of its own.
        var signedIn = try #require(await world.sessions.load())
        signedIn.tokenID = "0b5e1a7c33"
        signedIn.secret = "another"
        try await world.sessions.save(signedIn)
        let device = "/api/v1/substrate.reamde.dev/core/device/0b5e1a7c33"
        let record = PushWorld.record.replacingOccurrences(of: PushWorld.tokenID, with: "0b5e1a7c33")
        world.transport.on("PUT", device, status: 201, json: record)
        world.transport.on("GET", device, json: record)
        let facts = await registrar.sync(PushWorld.ask)
        #expect(facts.standing == .registered)
        #expect(await registrar.state()?.tokenID == "0b5e1a7c33")
        // The stuck attempt ended before: it put nothing for the sign-in
        // that left.
        let woke = await stalled.value
        #expect(!woke.held)
        #expect(world.sent("PUT", PushWorld.devicePath).isEmpty)
    }

    /// The work loses its claim once the deadline has passed or the caller
    /// was cancelled, however late the timer runs; the fallback's claim is
    /// anyone's until the work's is taken.
    @Test func theWorkLosesItsClaimOnceTheDeadlinePassesWhateverTheTimerDid() async throws {
        let late = Claim(deadline: .now + .milliseconds(50))
        #expect(late.open)
        try await Task.sleep(for: .milliseconds(100))
        #expect(!late.open && !late.take())
        #expect(late.fall() && !late.fall())
        let cancelled = Claim(deadline: .now + .seconds(30))
        cancelled.cancel()
        #expect(!cancelled.open && !cancelled.take() && cancelled.fall())
        let won = Claim(deadline: .now + .seconds(30))
        #expect(won.take() && !won.fall() && !won.open)
    }

    /// An attempt does what is wanted when it runs, not what was wanted when
    /// it was asked for: the switch-off said last wins though its attempt
    /// ran first, and the permission is read as the attempt runs.
    @Test func anAttemptDoesWhatIsWantedWhenItRunsNotWhenItWasAsked() async throws {
        let world = PushWorld()
        let registrar = world.registrar
        #expect(await registrar.sync(PushWorld.ask).held)
        let puts = world.sent("PUT", PushWorld.devicePath).count
        // Work ahead holds the queue while an attempt is asked for with
        // "on" wanted; the switch goes off before that attempt runs.
        let ahead = registrar.control.enqueue { await Self.uncancellable(seconds: 0.5) }
        registrar.want(listed: true, notifies: ["replies"], permitted: { true })
        let early = Task { await registrar.sync(tokenID: PushWorld.tokenID) }
        try await Task.sleep(for: .milliseconds(100))
        registrar.want(listed: true, notifies: [], permitted: { true })
        let off = await registrar.sync(tokenID: PushWorld.tokenID)
        let ran = await early.value
        _ = await ahead.value
        #expect(ran.standing == .switchedOff && off.standing == .switchedOff && !off.held)
        #expect(world.sent("PUT", PushWorld.devicePath).count == puts)
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 1)

        let allowed = Gate(open: true)
        registrar.want(listed: true, notifies: ["replies", "requests"], permitted: { allowed.isOpen })
        allowed.close()
        #expect(await registrar.sync(tokenID: PushWorld.tokenID).standing == .notPermitted)
    }

    /// A's attempt queued, then A signs out and B signs in before the queue
    /// drains: what was queued for A does nothing for B, and A's forgetting
    /// leaves B alone.
    @Test func whatWasQueuedForOneSignInLeavesTheNextUntouched() async throws {
        let world = PushWorld()
        let registers = Counter()
        let stalling = HookTransport(world.transport) { request in
            if request.url.map(StubTransport.path) == "/v1/register", registers.next() == 1 { await Self.uncancellable(seconds: 1.5) }
        }
        let registrar = PushRegistrar(
            sessions: world.sessions, device: world.device, transport: stalling, buildRelay: "https://push.example.com", clientID: PushWorld.client)
        registrar.control.signOutBound = .milliseconds(200)
        registrar.want(listed: true, notifies: ["replies", "requests"], permitted: { true })
        let inFlight = Task { await registrar.sync(tokenID: PushWorld.tokenID) }
        #expect(await Self.eventually { registers.count >= 1 })
        let queued = Task { await registrar.sync(tokenID: PushWorld.tokenID) }
        try await Task.sleep(for: .milliseconds(100))
        let signedOut = try #require(await world.sessions.load())
        await registrar.signOut(signedOut)
        // B signs in, and says what it wants, before the queue drains.
        var signedIn = signedOut
        signedIn.tokenID = "0b5e1a7c33"
        signedIn.secret = "another"
        try await world.sessions.save(signedIn)
        registrar.want(listed: true, notifies: ["replies", "requests"], permitted: { true })
        _ = await inFlight.value
        _ = await queued.value
        #expect(await Self.forgotten(registrar))
        let device = "/api/v1/substrate.reamde.dev/core/device/0b5e1a7c33"
        #expect(world.transport.requests.allSatisfy { $0.value(forHTTPHeaderField: "Authorization") != "Bearer another" })
        #expect(world.sent("PUT", device).isEmpty)
        #expect(await world.sessions.load()?.tokenID == "0b5e1a7c33")
        // B's own attempt registers B.
        let record = PushWorld.record.replacingOccurrences(of: PushWorld.tokenID, with: "0b5e1a7c33")
        world.transport.on("PUT", device, status: 201, json: record)
        world.transport.on("GET", device, json: record)
        #expect(await registrar.sync(tokenID: "0b5e1a7c33").standing == .registered)
        #expect(await registrar.state()?.tokenID == "0b5e1a7c33")
    }

    /// A sign-out's forgetting removes only what is kept for its own
    /// sign-in, never another's.
    @Test func aSignOutForgetsOnlyWhatIsKeptForItsOwnSignIn() async throws {
        let world = PushWorld()
        let registrar = world.registrar
        let another = PushState(
            tokenID: "0b5e1a7c33",
            current: .init(
                generation: "gB", relay: "https://push.example.com", publicKey: "key", deviceToken: String(repeating: "ab", count: 32),
                environment: .development, notifies: ["replies"], registeredAt: Date(), pushKey: "v1.k"))
        let kept = try JSONEncoder().encode(another)
        try world.storage.write(kept, account: registrar.account)
        await registrar.signOut(try #require(await world.sessions.load()))
        #expect(world.storage.stored(account: registrar.account) == kept)
        #expect(world.sent("DELETE", "/api/v1/substrate.reamde.dev/core/device/0b5e1a7c33").isEmpty)
    }

    /// A put that lands after the delete that gave its registration up: a
    /// delete sent within a put's lifetime is not final, so the next sync
    /// once that lifetime is over deletes again, a relaunch's included.
    @Test func aPutThatLandsAfterADeleteThatFoundNothingIsDeletedAgain() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        let off = PushAsk(listed: true, notifies: [], permitted: true)
        // The put has not landed yet: the delete finds nothing.
        world.pass(10)
        world.transport.replace("DELETE", PushWorld.devicePath, status: 404, json: #"{"error":{"code":"not_found","message":"no"}}"#)
        let first = await world.registrar.sync(off)
        #expect(first.standing == .switchedOff && !first.held)
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 1)
        #expect(await world.registrar.state()?.deleteAgainAt != nil)
        // It lands now. Within its lifetime nothing more is sent.
        world.pass(60)
        _ = await world.registrar.sync(off)
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 1)
        // Past it, the next sync, after a relaunch, deletes again, for good.
        world.pass(60)
        world.transport.replace("DELETE", PushWorld.devicePath, status: 204)
        let relaunched = world.registrar
        _ = await relaunched.sync(off)
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 2)
        #expect(await relaunched.state()?.deleteAgainAt == nil)
        world.pass(600)
        _ = await relaunched.sync(off)
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 2)
    }

    /// An earlier registration's put lands after a newer one's: the newer
    /// is not settled until no earlier put can land, and the next sync after
    /// that puts it again, the same key and generation, so the server holds
    /// the newer key.
    @Test func aNewerRegistrationIsPutAgainOnceAnEarlierPutCanNoLongerLand() async throws {
        let world = PushWorld()
        let server = DeviceServer(world.transport, path: PushWorld.devicePath)
        let registrar = PushRegistrar(
            sessions: world.sessions, device: world.device, transport: server, buildRelay: "https://push.example.com", clientID: PushWorld.client,
            now: { world.clock }, generation: { world.mint() })
        // The first registration's put reaches the server, its answer lost.
        server.loseNextPut()
        let first = await registrar.sync(PushWorld.ask)
        #expect(first.standing == .unavailable && first.held)
        // Thirty seconds on, Apple's token changes: a newer registration,
        // with a key of its own, is put and answered.
        world.pass(30)
        world.device.set(token: String(repeating: "cd", count: 32))
        world.transport.replace("POST", "/v1/register", json: #"{"pushKey":"v1.newer"}"#)
        #expect(await registrar.sync(PushWorld.ask).standing == .registered)
        #expect(server.key == "v1.newer")
        #expect(await registrar.state()?.putAgainAt != nil)
        // The earlier put lands now, over it.
        server.landLate()
        #expect(server.key == "v1.k")
        let puts = world.sent("PUT", PushWorld.devicePath).count
        // Before the earlier put's lifetime is over nothing is sent.
        world.pass(30)
        #expect(await registrar.sync(PushWorld.ask).standing == .registered)
        #expect(world.sent("PUT", PushWorld.devicePath).count == puts)
        // After it, the next sync puts the newer one again, and it is settled.
        world.pass(70)
        #expect(await registrar.sync(PushWorld.ask).standing == .registered)
        #expect(world.sent("PUT", PushWorld.devicePath).count == puts + 1)
        #expect(server.key == "v1.newer")
        #expect(await registrar.state()?.putAgainAt == nil)
        world.pass(600)
        _ = await registrar.sync(PushWorld.ask)
        #expect(world.sent("PUT", PushWorld.devicePath).count == puts + 1)
    }

    /// A sync asked for one sign-in, run after it signed out and the next
    /// signed in and said its wish, does nothing for the next: the sign-in
    /// is named as the sync is asked for, never when it runs.
    @Test func aSyncAskedForOneSignInDoesNothingWhenItRunsForTheNext() async throws {
        let world = PushWorld()
        let registrar = world.registrar
        let signedOut = try #require(await world.sessions.load())
        registrar.want(listed: true, notifies: ["replies", "requests"], permitted: { true })
        await registrar.signOut(signedOut)
        var signedIn = signedOut
        signedIn.tokenID = "0b5e1a7c33"
        signedIn.secret = "another"
        try await world.sessions.save(signedIn)
        registrar.want(listed: true, notifies: ["replies", "requests"], permitted: { true })
        let device = "/api/v1/substrate.reamde.dev/core/device/0b5e1a7c33"
        let record = PushWorld.record.replacingOccurrences(of: PushWorld.tokenID, with: "0b5e1a7c33")
        world.transport.on("PUT", device, status: 201, json: record)
        // A's effect, deferred until now.
        let late = await registrar.sync(tokenID: signedOut.tokenID)
        #expect(late.standing == .unknown && !late.held)
        #expect(world.transport.requests.allSatisfy { $0.value(forHTTPHeaderField: "Authorization") != "Bearer another" })
        #expect(world.sent("PUT", device).isEmpty)
        // B's own registers B.
        #expect(await registrar.sync(tokenID: signedIn.tokenID).standing == .registered)
    }

    /// A save that suspends between a put's stamp and its send: the put is
    /// stamped again once the save is done, so the two-minute rules count
    /// from the true send.
    @Test func aPutIsStampedWhenItIsSentThoughItsSaveSuspends() async throws {
        let world = PushWorld()
        let start = world.clock
        let armed = Gate(open: true)
        let slow = HookStorage(world.storage) { data, _ in
            guard armed.isOpen, let state = try? JSONDecoder().decode(PushState.self, from: data), !state.puts.isEmpty else { return }
            armed.close()
            world.pass(30)
        }
        let sessions = SessionStore(account: world.sessions.account, storage: slow)
        let registrar = PushRegistrar(
            sessions: sessions, device: world.device, transport: world.transport, buildRelay: "https://push.example.com", clientID: PushWorld.client,
            now: { world.clock }, generation: { world.mint() })
        #expect(await registrar.sync(PushWorld.ask).standing == .registered)
        let stamped = try #require(await registrar.state()?.puts.first?.at)
        #expect(stamped == start.addingTimeInterval(30))
        // A switch-off 100 seconds after the true send is not final.
        world.pass(100)
        #expect(await registrar.sync(PushAsk(listed: true, notifies: [], permitted: true)).standing == .switchedOff)
        #expect(await registrar.state()?.deleteAgainAt == PushState.landsBy(stamped))
    }

    /// A sign-out the app ends before the session is removed keeps what is
    /// left to retire: the next launch, still signed in, deletes again once
    /// the put can no longer land. Once the session is removed and the
    /// delete was final, the state goes.
    @Test func aSignOutInterruptedBeforeTheSessionIsRetiredStillDeletesAfterARelaunch() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        world.pass(10)
        let session = try #require(await world.sessions.load())
        await world.registrar.signOut(session)
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 1)
        #expect(await world.registrar.state()?.deleteAgainAt != nil)
        // The app ends here: the session is never removed.
        world.pass(130)
        let relaunched = world.registrar
        let off = PushAsk(listed: true, notifies: [], permitted: true)
        #expect(await relaunched.sync(off).standing == .switchedOff)
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 2)
        #expect(await relaunched.state()?.deleteAgainAt == nil)
        // Signed out for good now: the delete was final, so the state goes.
        await relaunched.signOut(session)
        await relaunched.retired(session)
        #expect(world.storage.stored(account: relaunched.account) == nil)
    }

    /// The recent puts are one per registration and at most eight: fifty
    /// retries of one keep one entry, and a renewal keeps only its own, the
    /// expired one dropped before any save.
    @Test func theRecentPutsStayBoundedThroughRetriesAndARenewal() async throws {
        let world = PushWorld()
        let registrar = world.registrar
        world.transport.replace("PUT", PushWorld.devicePath, status: 500, json: #"{"error":{"code":"internal","message":"no"}}"#)
        for _ in 0 ..< 50 {
            world.pass(1)
            _ = await registrar.sync(PushWorld.ask)
        }
        #expect(world.sent("PUT", PushWorld.devicePath).count == 50)
        #expect(await registrar.state()?.puts.count == 1)
        world.transport.replace("PUT", PushWorld.devicePath, status: 200, json: PushWorld.record)
        world.pass(PushRegistrar.renewal + 60)
        #expect(await registrar.sync(PushWorld.ask).standing == .registered)
        let kept = try #require(await registrar.state())
        #expect(kept.puts.map(\.generation) == [try #require(kept.current?.generation)])

        var many = PushState(tokenID: PushWorld.tokenID)
        let now = Date(timeIntervalSince1970: 1_763_459_000)
        for index in 0 ..< 12 {
            let made = PushState.Registration(
                generation: "g\(index)", relay: "https://push.example.com", publicKey: "key", deviceToken: "ab", environment: .development,
                notifies: ["replies"], registeredAt: now)
            many.sending(made, at: now.addingTimeInterval(Double(index)))
        }
        #expect(many.puts.map(\.generation) == (4 ..< 12).map { "g\($0)" })
    }

    /// A state an earlier build saved, without the recent puts, still reads:
    /// as none.
    @Test func aStateSavedWithoutRecentPutsStillReads() throws {
        let state = PushState(
            tokenID: PushWorld.tokenID,
            current: .init(
                generation: "g1", relay: "https://push.example.com", publicKey: "key", deviceToken: "ab", environment: .development,
                notifies: ["replies"], registeredAt: Date(timeIntervalSince1970: 1_763_459_000), pushKey: "v1.k"))
        var object = try #require(try JSONSerialization.jsonObject(with: JSONEncoder().encode(state)) as? [String: Any])
        object["puts"] = nil
        object["deleteAgainAt"] = nil
        object["putAgainAt"] = nil
        let saved = try JSONSerialization.data(withJSONObject: object)
        let read = try JSONDecoder().decode(PushState.self, from: saved)
        #expect(read.puts.isEmpty && read.current?.generation == "g1" && read.held)
    }

    /// A retirement the Keychain would not keep is not reported: the stored
    /// state, still held, is, and the next sync deletes again.
    @Test func aRetirementThatWasNotSavedLeavesTheRegistrationHeld() async throws {
        let world = PushWorld()
        #expect(await world.registrar.sync(PushWorld.ask).held)
        let off = PushAsk(listed: true, notifies: [], permitted: true)
        world.pass(300)
        world.storage.failWrites(of: world.registrar.account, with: MemoryStorage.locked)
        let unsaved = await world.registrar.sync(off)
        #expect(unsaved.standing == .switchedOff && unsaved.held)
        #expect(world.sent("DELETE", PushWorld.devicePath).count == 1)
        world.storage.failWrites(of: world.registrar.account, with: nil)
        let saved = await world.registrar.sync(off)
        #expect(!saved.held && world.sent("DELETE", PushWorld.devicePath).count == 2)
        #expect(await world.registrar.state()?.held == false)
    }

    /// A caller cancelled at any point before the work claims gets the
    /// fallback: the cancellation is in place before the work starts.
    @Test func aCallerCancelledBeforeTheWorkClaimsGetsTheFallback() async throws {
        for _ in 0 ..< 20 {
            let caller = Holder<Task<String, Never>>()
            let task = Task {
                await Deadline.race(until: .now + .seconds(30), or: "fallback") {
                    while caller.value == nil { await Task.yield() }
                    caller.value?.cancel()
                    return "won"
                }
            }
            caller.value = task
            let answered = await task.value
            #expect(answered == "fallback")
        }
    }

    /// A deadline already passed, or a caller already cancelled, starts no
    /// work at all.
    @Test func aDeadlinePassedOrACallerCancelledStartsNothing() async throws {
        let started = Counter()
        let late = await Deadline.race(until: .now - .seconds(1), or: "fallback") {
            started.next()
            return "ran"
        }
        #expect(late == "fallback")
        let caller = Task {
            withUnsafeCurrentTask { $0?.cancel() }
            return await Deadline.race(until: .now + .seconds(30), or: "fallback") {
                started.next()
                return "ran"
            }
        }
        let cancelled = await caller.value
        #expect(cancelled == "fallback")
        try await Task.sleep(for: .milliseconds(100))
        #expect(started.count == 0)
    }

    /// A cancellation handler that takes its time holds neither the
    /// deadline's answer nor a cancelled caller's: the fallback comes first.
    @Test func aCancellationHandlerThatStallsHoldsNoCaller() async throws {
        let stalling: @Sendable () async -> String = {
            await withTaskCancellationHandler {
                await Self.uncancellable(seconds: 3)
                return "late"
            } onCancel: {
                Thread.sleep(forTimeInterval: 2)
            }
        }
        var started = ContinuousClock.now
        let timed = await Deadline.race(until: .now + .milliseconds(200), or: "fallback", stalling)
        #expect(timed == "fallback" && ContinuousClock.now - started < .seconds(1))
        let caller = Task { await Deadline.race(until: .now + .seconds(30), or: "fallback", stalling) }
        try await Task.sleep(for: .milliseconds(100))
        started = ContinuousClock.now
        caller.cancel()
        let cancelled = await caller.value
        #expect(cancelled == "fallback" && ContinuousClock.now - started < .seconds(1))
    }

    /// Work that finishes after the answer was given acts on nothing: what
    /// it would publish it checks against `Race.live`.
    @Test func workThatFinishesAfterTheAnswerPublishesNothing() async throws {
        let published = Counter()
        let late = await Deadline.race(until: .now + .milliseconds(200), or: "fallback") { race in
            await Self.uncancellable(seconds: 1)
            if race.live { published.next() }
            return "late"
        }
        #expect(late == "fallback")
        let early = await Deadline.race(until: .now + .seconds(30), or: "fallback") { race in
            if race.live { published.next() }
            return "done"
        }
        #expect(early == "done")
        try await Task.sleep(for: .seconds(1.5))
        #expect(published.count == 1)
    }

    /// Whether `condition` holds within `seconds`: a wait that fails the
    /// test, never one that hangs it.
    static func eventually(within seconds: Double = 10, _ condition: @Sendable () -> Bool) async -> Bool {
        let until = ContinuousClock.now + .seconds(seconds)
        while !condition() {
            guard ContinuousClock.now < until else { return false }
            try? await Task.sleep(for: .milliseconds(10))
        }
        return true
    }

    /// Whether, once the queue has drained, nothing of the sign-in is held
    /// or accepted: what a sign-out leaves until its session is retired.
    static func forgotten(_ registrar: PushRegistrar) async -> Bool {
        await registrar.control.enqueue {}.value
        guard let kept = await registrar.state() else { return true }
        return !kept.held && kept.current == nil && kept.pending.isEmpty && kept.previous.isEmpty
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
