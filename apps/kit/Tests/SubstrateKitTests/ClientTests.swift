import Foundation
import SubstrateTesting
import Testing

@testable import SubstrateAPI
@testable import SubstrateAuth
@testable import SubstrateCore
@testable import SubstrateDevice
@testable import SubstrateDeviceApple
@testable import SubstrateKit
@testable import SubstrateUI

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

@Suite(.timeLimit(.minutes(10))) struct Requests {
    let server = URL(string: "https://ada.example.com")!

    func client(_ stub: StubTransport, token: String? = "sub_x") -> SubstrateClient {
        var client = SubstrateClient(server: server, token: token, transport: stub)
        client.pause = { _ in }
        return client
    }

    @Test func anIdIsOneSegmentEncodedOnce() async throws {
        #expect(segment("x.example.com/llm/helper") == "x.example.com%2Fllm%2Fhelper")
        #expect(segment("a b+c&é~") == "a%20b%2Bc%26%C3%A9~")
        let stub = StubTransport()
        stub.on(
            "GET", "/api/v1/substrate.reamde.dev/core/agent/x.example.com%2Fllm%2Fhelper",
            json: #"{"id":"x.example.com/llm/helper","kind":"substrate.reamde.dev/core/agent","properties":{"name":"Helper"},"version":1}"#)
        let record = try await client(stub).record("substrate.reamde.dev/core/agent", "x.example.com/llm/helper")
        #expect(record?["name"] == "Helper")
        #expect(stub.requests.first?.value(forHTTPHeaderField: "Authorization") == "Bearer sub_x")
    }

    @Test func aTraitsImplementorsAreReadWithTheirDeclarations() async throws {
        let stub = StubTransport()
        stub.on(
            "GET", "/api/v1/substrate.reamde.dev/core/trait/substrate.reamde.dev%2Fcore%2Flifecycle/implementors",
            json: #"""
                {"items":[{"identity":"ada.example.com/work/chore","label":{"singular":"Chore","plural":"Chores"},
                "definition":{"traits":["substrate.reamde.dev/core/lifecycle(status: state)"],
                "properties":{"state":{"type":"enum","values":["todo","closed"],"done":["closed"]}}}},
                {"identity":"substrate.reamde.dev/tasks/task","definition":{}}]}
                """#)
        let kinds = try await client(stub).implementors(of: KindStatus.lifecycle)
        #expect(kinds.map(\.identity) == ["ada.example.com/work/chore", "substrate.reamde.dev/tasks/task"])
        #expect(kinds[0].label == KindInfo.Label(singular: "Chore", plural: "Chores"))
        #expect(kinds[1].label == nil)
        #expect(KindStatus(definition: kinds[0].definition)?.property == "state")
        let none = StubTransport()
        none.on("GET", "/api/v1/substrate.reamde.dev/core/trait/lifecycle/implementors", json: "{}")
        #expect(try await client(none).implementors(of: "lifecycle").isEmpty)
    }

    @Test func aDerivedIDIsTheSameUUIDv8ForTheSameName() {
        let id = DerivedID.uuid("collection:substrate.reamde.dev/tasks/task")
        #expect(id == DerivedID.uuid("collection:substrate.reamde.dev/tasks/task"))
        #expect(id != DerivedID.uuid("collection:ada.example.com/work/chore"))
        #expect(id == id.lowercased())
        #expect(UUID(uuidString: id) != nil)
        let parts = id.split(separator: "-")
        #expect(parts.map(\.count) == [8, 4, 4, 4, 12])
        #expect(parts[2].first == "8")
        #expect("89ab".contains(parts[3].first!))
        // RFC 9562's name-based UUIDv8: SHA-256 of the name, its first 128
        // bits, version and variant set. The web kit is held to this text.
        #expect(DerivedID.uuid("preference") == "bc5ece60-fff8-8ba4-a305-bc10aebbf972")
    }

    @Test func aMissingOrDeletedRecordIsAbsent() async throws {
        let stub = StubTransport()
        stub.on(
            "GET", "/api/v1/a.example.com/tasks/task/gone",
            json: #"{"id":"gone","kind":"a.example.com/tasks/task","properties":{},"version":2,"deletedAt":"2026-10-31T10:00:00Z"}"#)
        #expect(try await client(stub).record("a.example.com/tasks/task", "gone") == nil)
        #expect(try await client(stub).record("a.example.com/tasks/task", "never") == nil)
    }

    @Test func theRecordReaderNoLongerEncodesTwice() async throws {
        let stub = StubTransport()
        stub.on(
            "GET", "/api/v1/substrate.reamde.dev/core/agent/x.example.com%2Fllm%2Fhelper",
            json: #"{"id":"x.example.com/llm/helper","kind":"substrate.reamde.dev/core/agent","properties":{},"version":1}"#)
        stub.on("GET", "/api/v1/a.example.com/x/y/z", status: 403, json: #"{"error":{"code":"forbidden","message":"no"}}"#)
        let reader = HTTPRecordReader(client: client(stub))
        let agent = try await reader.record(RecordReference(kind: "substrate.reamde.dev/core/agent", id: "x.example.com/llm/helper"))
        #expect(agent?.id == "x.example.com/llm/helper")
        await #expect(throws: RecordReadError.notAllowed) { try await reader.record(RecordReference(kind: "a.example.com/x/y", id: "z")) }
    }

    @Test func aWriteCarriesItsPropertiesAndVersion() async throws {
        let stub = StubTransport()
        stub.on("PUT", "/api/v1/a.example.com/tasks/task/t1", json: #"{"id":"t1","kind":"a.example.com/tasks/task","properties":{},"version":1}"#)
        stub.on("PATCH", "/api/v1/a.example.com/tasks/task/t1", json: #"{"id":"t1","kind":"a.example.com/tasks/task","properties":{},"version":2}"#)
        let c = client(stub)
        try await c.put("a.example.com/tasks/task", "t1", properties: ["title": "Write", "points": 3], ifVersion: 0)
        try await c.clear("a.example.com/tasks/task", "t1", keys: ["points"])
        let put = try #require(stub.sent("PUT", "/api/v1/a.example.com/tasks/task/t1").first?.httpBody)
        #expect(String(decoding: put, as: UTF8.self).contains(#""ifVersion":0"#))
        #expect(String(decoding: put, as: UTF8.self).contains(#""points":3"#))
        let patch = try #require(stub.sent("PATCH", "/api/v1/a.example.com/tasks/task/t1").first?.httpBody)
        #expect(String(decoding: patch, as: UTF8.self) == #"{"properties":{"points":null}}"#)
    }

    /// A number the server sent goes back with every digit, past what a
    /// Double holds, and an object's keys in the order they were set.
    @Test func aPatchWritesBackEveryDigitItReadAndEveryKeyInOrder() async throws {
        let stub = StubTransport()
        stub.on(
            "GET", "/api/v1/a.example.com/x/y/r",
            json:
                #"{"id":"r","kind":"a.example.com/x/y","properties":{"count":9007199254740993,"far":1e200,"near":1e-200,"big":123456789012345678901234567890123456},"version":3}"#
        )
        stub.on("PATCH", "/api/v1/a.example.com/x/y/r", json: #"{"id":"r","kind":"a.example.com/x/y","properties":{},"version":4}"#)
        let c = client(stub)
        let read = try #require(try await c.record("a.example.com/x/y", "r"))
        let spec: JSONValue = ["z": read["count"]!, "a": read["far"]!, "m": read["near"]!, "b": read["big"]!]
        try await c.patch("a.example.com/x/y", "r", properties: ["spec": spec], ifVersion: read.version)
        let patch = try #require(stub.sent("PATCH", "/api/v1/a.example.com/x/y/r").first?.httpBody)
        #expect(
            String(decoding: patch, as: UTF8.self)
                == #"{"properties":{"spec":{"z":9007199254740993,"a":1e200,"m":1e-200,"b":123456789012345678901234567890123456}},"ifVersion":3}"#)
    }

    @Test func aBusyServerIsWaitedOutOnlyWhereARetryIsSafe() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/records", status: 503, headers: ["Retry-After": "2"], json: #"{"error":{"code":"unavailable","message":"busy"}}"#)
        stub.on("GET", "/api/v1/records", json: #"{"records":[],"head":7,"generation":"g1"}"#)
        let waits = WaitRecorder()
        var c = client(stub)
        c.pause = { await waits.add($0) }
        let page = try await c.list(ListQuery(kinds: ["a.example.com/tasks/task"]))
        #expect(page.head == 7)
        #expect(await waits.all == [2])
        #expect(stub.sent("GET", "/api/v1/records").count == 2)

        let posts = StubTransport()
        posts.on("POST", "/api/v1/records", status: 429, json: #"{"error":{"code":"rate_limited","message":"slow down"}}"#)
        await #expect(throws: ServerError.self) { try await client(posts).create("a.example.com/tasks/task", properties: [:]) }
        #expect(posts.sent("POST", "/api/v1/records").count == 1)
    }

    /// One list page of tasks, as the server answers it.
    func page(_ ids: [String], cursor: String?, head: Int, generation: String) -> String {
        let rows = ids.map { #"{"id":"\#($0)","kind":"a.example.com/tasks/task","properties":{},"version":1}"# }.joined(separator: ",")
        return #"{"records":[\#(rows)],"cursor":\#(cursor.map { "\"\($0)\"" } ?? "null"),"head":\#(head),"generation":"\#(generation)"}"#
    }

    func compacted(head: Int, generation: String) -> String {
        #"{"error":{"code":"compacted","message":"the cursor's history was replaced: list again from the first page","head":\#(head),"generation":"\#(generation)"}}"#
    }

    /// The `after` each list request carried, nil for a first page.
    func afters(_ stub: StubTransport) -> [String?] {
        stub.sent("GET", "/api/v1/records").map { request in
            request.url.flatMap { URLComponents(url: $0, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "after" }?.value }
        }
    }

    @Test func aWalkToldCompactedOnAContinuationListsAgainFromTheFirstPageAlone() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/records", json: page(["a", "b"], cursor: "p1", head: 7, generation: "g1"))
        stub.on("GET", "/api/v1/records", status: 410, json: compacted(head: 9, generation: "g2"))
        // The history the walk starts over in: b is gone, c is new.
        stub.on("GET", "/api/v1/records", json: page(["a", "c"], cursor: "q1", head: 9, generation: "g2"))
        stub.on("GET", "/api/v1/records", json: page(["d"], cursor: nil, head: 9, generation: "g2"))
        let all = try await client(stub).listAll(ListQuery(kinds: ["a.example.com/tasks/task"], first: 2))
        // Nothing read before the restart is kept, and nothing is read twice.
        #expect(all.records.map(\.id) == ["a", "c", "d"])
        #expect(all.head == 9 && all.generation == "g2")
        #expect(all.cursor == nil)
        #expect(afters(stub) == [nil, "p1", nil, "q1"])
    }

    @Test func aWalkToldCompactedAgainAfterItsRestartThrowsIt() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/records", json: page(["a"], cursor: "p1", head: 7, generation: "g1"))
        stub.on("GET", "/api/v1/records", status: 410, json: compacted(head: 9, generation: "g2"))
        stub.on("GET", "/api/v1/records", json: page(["a"], cursor: "q1", head: 9, generation: "g2"))
        stub.on("GET", "/api/v1/records", status: 410, json: compacted(head: 11, generation: "g3"))
        do {
            _ = try await client(stub).listAll(ListQuery(kinds: ["a.example.com/tasks/task"], first: 1))
            Issue.record("a walk refused twice answered")
        } catch let error as ServerError {
            #expect(error.isCompacted)
            #expect(error.head == 11 && error.generation == "g3")
        }
        // One restart, then the refusal.
        #expect(afters(stub) == [nil, "p1", nil, "q1"])
    }

    /// A walk begun from a cursor the caller saved, refused on its first
    /// read, starts over from the list's first page rather than failing.
    @Test func aWalkFromASavedCursorRefusedAtOnceListsFromTheFirstPage() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/records", status: 410, json: compacted(head: 9, generation: "g2"))
        stub.on("GET", "/api/v1/records", json: page(["a"], cursor: "q1", head: 9, generation: "g2"))
        stub.on("GET", "/api/v1/records", json: page(["b"], cursor: nil, head: 9, generation: "g2"))
        let all = try await client(stub).listAll(ListQuery(kinds: ["a.example.com/tasks/task"], first: 1, after: "saved"))
        #expect(all.records.map(\.id) == ["a", "b"])
        #expect(all.head == 9 && all.cursor == nil)
        #expect(afters(stub) == ["saved", nil, "q1"])
    }

    /// The restart of a walk begun from a saved cursor is the list's first
    /// page, never the saved cursor, which the same history refuses again.
    @Test func aWalkFromASavedCursorRefusedLaterListsFromTheFirstPageNotTheSavedCursor() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/records", json: page(["a"], cursor: "p1", head: 7, generation: "g1"))
        stub.on("GET", "/api/v1/records", status: 410, json: compacted(head: 9, generation: "g2"))
        stub.on("GET", "/api/v1/records", json: page(["c"], cursor: "q1", head: 9, generation: "g2"))
        stub.on("GET", "/api/v1/records", json: page(["d"], cursor: nil, head: 9, generation: "g2"))
        let all = try await client(stub).listAll(ListQuery(kinds: ["a.example.com/tasks/task"], first: 1, after: "saved"))
        #expect(all.records.map(\.id) == ["c", "d"])
        #expect(all.head == 9 && all.generation == "g2")
        #expect(afters(stub) == ["saved", "p1", nil, "q1"])
    }

    @Test func aWalkFromASavedCursorRefusedAgainAfterItsRestartThrowsIt() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/records", status: 410, json: compacted(head: 9, generation: "g2"))
        stub.on("GET", "/api/v1/records", json: page(["a"], cursor: "q1", head: 9, generation: "g2"))
        stub.on("GET", "/api/v1/records", status: 410, json: compacted(head: 11, generation: "g3"))
        do {
            _ = try await client(stub).listAll(ListQuery(kinds: ["a.example.com/tasks/task"], first: 1, after: "saved"))
            Issue.record("a walk refused twice answered")
        } catch let error as ServerError {
            #expect(error.isCompacted)
            #expect(error.head == 11 && error.generation == "g3")
        }
        // The saved cursor refused, the restart, then the refusal.
        #expect(afters(stub) == ["saved", nil, "q1"])
    }

    @Test func anyOtherRefusalOfAWalkIsThrownAsItIs() async throws {
        // A continuation refused as a fresh request would be (another filter
        // or order) is the caller's to see, not a restart.
        let refused = StubTransport()
        refused.on("GET", "/api/v1/records", json: page(["a"], cursor: "p1", head: 7, generation: "g1"))
        refused.on(
            "GET", "/api/v1/records", status: 422, json: #"{"error":{"code":"validation","message":"the cursor was minted for another filter"}}"#)
        await #expect(throws: ServerError(status: 422, code: "validation", message: "the cursor was minted for another filter")) {
            try await client(refused).listAll(ListQuery(kinds: ["a.example.com/tasks/task"], first: 1))
        }
        #expect(afters(refused) == [nil, "p1"])

        // A first page with no `after` has no position to drop.
        let first = StubTransport()
        first.on("GET", "/api/v1/records", status: 410, json: compacted(head: 9, generation: "g2"))
        await #expect(throws: ServerError.self) { try await client(first).listAll(ListQuery(kinds: ["a.example.com/tasks/task"])) }
        #expect(afters(first) == [nil])
    }

    @Test func aRefusalBecomesTheErrorThatSaysWhatItMeans() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/a/expired", status: 401, json: #"{"error":{"code":"unauthorized","message":"token expired"}}"#)
        stub.on(
            "GET", "/api/v1/a/scope", status: 403, headers: ["WWW-Authenticate": #"Bearer error="insufficient_scope""#],
            json: #"{"error":{"code":"forbidden","message":"the token's scopes do not reach this"}}"#)
        stub.on("GET", "/api/v1/a/refused", status: 422, json: #"{"error":{"code":"validation","message":"bad","problems":["title is required"]}}"#)
        stub.on("GET", "/api/v1/a/gateway", status: 502, json: "<html><body>Bad Gateway</body></html>")
        stub.on("GET", "/api/v1/a/plain", status: 502, json: "upstream connect error")
        let c = client(stub)
        await #expect(throws: SessionExpired.self) { try await c.send("GET", "api/v1/a/expired") }
        await #expect(throws: InsufficientScope(message: "the token's scopes do not reach this")) { try await c.send("GET", "api/v1/a/scope") }
        do {
            try await c.send("GET", "api/v1/a/refused")
            Issue.record("a refusal answered")
        } catch let error as ServerError {
            #expect(error.code == "validation")
            #expect(error.text == "bad\ntitle is required")
            #expect(error.isPermanent)
        }
        await #expect(throws: TransportError(status: 502, text: "The server answered 502.")) { try await c.send("GET", "api/v1/a/gateway") }
        await #expect(throws: TransportError(status: 502, text: "The server answered 502: upstream connect error")) {
            try await c.send("GET", "api/v1/a/plain")
        }
    }

    @Test func anErrorReadsAsASentence() {
        let host = URL(string: "https://ada.example.com")
        let approval = explain(InsufficientScope(message: nil), app: "Substrate Chat", server: host)
        #expect(approval == "Substrate Chat needs your approval to do that on ada.example.com. Review its access to approve it.")
        #expect(!approval.contains("Sign in"))
        #expect(
            explain(SessionExpired(), app: "Substrate Chat", server: host)
                == "Ada.example.com no longer accepts Substrate Chat's sign-in. Sign in again to carry on.")
        #expect(explain(URLError(.timedOut), app: "X", server: host) == "Ada.example.com took too long to answer.")
        #expect(explain(URLError(.cannotFindHost), app: "X", server: host) == "Couldn't find ada.example.com. Check the address.")
        #expect(explain(ServerError(status: 500, message: "boom"), app: "X", server: host) == "Ada.example.com ran into a problem (500): boom")
        #expect(explain(ServerError(status: 409, message: "taken"), app: "X", server: host) == "taken")
        #expect(isUnreachable(URLError(.cannotConnectToHost)) && !isUnreachable(SessionExpired()))
        #expect(isCancellation(URLError(.cancelled)) && isCancellation(CancellationError()))
    }

    /// The two-step sign-in: a login without a code, of an account that has
    /// a second factor, is refused with `totp_required`, which says to ask
    /// for the code; any other 401 is the name, the password or the code.
    @Test func aLoginTheServerAsksACodeOfSaysSo() async throws {
        let stub = StubTransport()
        stub.on("POST", "/login", status: 401, json: #"{"error":{"code":"totp_required","message":"a code is required"}}"#)
        stub.on("POST", "/login", status: 401, json: #"{"error":{"code":"auth","message":"invalid repository, password or code"}}"#)
        stub.on("POST", "/login", status: 201, json: #"{"token":{"id":"tok-1"},"secret":"sub_x","repository":"ada.example.com"}"#)
        let c = client(stub, token: nil)
        let door = Discovery(server: .init(version: "v0.122.0"))
        do {
            _ = try await c.login(name: "ada", password: "pw", totp: nil, label: "test", door: door)
            Issue.record("a login the server asked a code of succeeded")
        } catch let refused as LoginRefused {
            #expect(refused.codeRequired)
        }
        do {
            _ = try await c.login(name: "ada", password: "pw", totp: "000000", label: "test", door: door)
            Issue.record("a refused login succeeded")
        } catch let refused as LoginRefused {
            #expect(!refused.codeRequired)
            #expect(refused.localizedDescription == "invalid repository, password or code")
        }
        let result = try await c.login(name: "ada", password: "pw", totp: "123456", label: "test", door: door)
        #expect(result.secret == "sub_x")
        #expect(result.identity == nil)
    }

    /// A person who may open several repositories and named none is refused
    /// with the repositories to pick from, typed; the same sign-in naming the
    /// one picked, with the same factors, signs in. A conflict of another
    /// kind stays the server's error.
    @Test func aLoginOfAPersonInSeveralRepositoriesOffersThem() async throws {
        let stub = StubTransport()
        stub.on(
            "POST", "/login", status: 409,
            json:
                #"{"error":{"code":"conflict","reason":"repository_required","message":"name one","repositories":[{"id":"rnd-a","name":"ada.example.com"},{"id":"rnd-n","name":"notes.example.com"}]}}"#
        )
        stub.on(
            "POST", "/login", status: 201, json: #"{"token":{"id":"tok-1"},"secret":"sub_x","repository":"notes.example.com","identity":"ada.example.com"}"#)
        stub.on("POST", "/login", status: 409, json: #"{"error":{"code":"conflict","message":"another conflict"}}"#)
        let c = client(stub, token: nil)
        let door = Discovery(server: .init(version: "v1.0.0"), features: [.init(name: "auth", stability: "stable", revision: 3)])
        var picked: RepositoryChoiceRequired.Choice?
        do {
            _ = try await c.login(name: "ada.example.com", password: "pw", totp: "123456", label: "test", door: door)
            Issue.record("a sign-in naming no repository of several succeeded")
        } catch let choice as RepositoryChoiceRequired {
            #expect(choice.repositories.map(\.name) == ["ada.example.com", "notes.example.com"])
            picked = choice.repositories.last
        }
        let result = try await c.login(
            name: "ada.example.com", repository: picked?.id, password: "pw", totp: "123456", label: "test", door: door)
        #expect(result.repository == "notes.example.com")
        let bodies = try stub.sent("POST", "/login").map {
            try JSONSerialization.jsonObject(with: $0.httpBody ?? Data()) as? [String: String] ?? [:]
        }
        #expect(bodies.last?["repository"] == "rnd-n")
        #expect(bodies.last?["totpCode"] == "123456")
        do {
            _ = try await c.login(name: "ada.example.com", password: "pw", totp: nil, label: "test", door: door)
            Issue.record("a conflict succeeded")
        } catch let server as ServerError {
            #expect(server.status == 409 && server.message == "another conflict")
        }
    }

    /// A door that names a person (decision 0274) is sent `identity`, and the
    /// repository where the sign-in names one; an older door is sent the
    /// name as `repository`, which it reads as the sign-in name.
    @Test func aLoginNamesThePersonWhereTheDoorDoes() async throws {
        let stub = StubTransport()
        stub.on(
            "POST", "/login", status: 201, json: #"{"token":{"id":"tok-1"},"secret":"sub_x","repository":"notes.example.com","identity":"ada.example.com"}"#)
        stub.on("POST", "/login", status: 201, json: #"{"token":{"id":"tok-2"},"secret":"sub_y","repository":"ada.example.com"}"#)
        let c = client(stub, token: nil)
        let current = Discovery(server: .init(version: "v1.0.0"), features: [.init(name: "auth", stability: "stable", revision: 3)])
        let result = try await c.login(
            name: "ada.example.com", repository: "notes.example.com", password: "pw", totp: nil, label: "test", door: current)
        #expect(result.identity == "ada.example.com")
        let older = Discovery(server: .init(version: "v0.123.1"), features: [.init(name: "auth", stability: "stable", revision: 2)])
        _ = try await c.login(name: "ada.example.com", password: "pw", totp: nil, label: "test", door: older)
        let bodies = try stub.sent("POST", "/login").map {
            try JSONSerialization.jsonObject(with: $0.httpBody ?? Data()) as? [String: String] ?? [:]
        }
        #expect(bodies.count == 2)
        #expect(bodies.first?["identity"] == "ada.example.com")
        #expect(bodies.first?["repository"] == "notes.example.com")
        #expect(bodies.last?["identity"] == nil)
        #expect(bodies.last?["repository"] == "ada.example.com")
    }

    @Test func aWebhookDeliveryCarriesTheTokenAndWaitsOutABusyDoor() async throws {
        let stub = StubTransport()
        let path = "/webhooks/ada.example.com/applehealth-webhook"
        stub.on("POST", path, status: 503, headers: ["Retry-After": "1"], json: #"{"error":{"code":"unavailable","message":"busy"}}"#)
        stub.on("POST", path, status: 202, json: #"{"fire":"f-123"}"#)
        let fire = try await client(stub, token: "sub_app_x").deliver(
            authority: "ada.example.com", trigger: "applehealth-webhook",
            body: Data("{}".utf8),
            headers: ["X-Account": "substrate-sync-1a2b3c4d"])
        #expect(fire == "f-123")
        let sent = stub.sent("POST", path)
        #expect(sent.count == 2)
        #expect(sent.allSatisfy { $0.value(forHTTPHeaderField: "Authorization") == "Bearer sub_app_x" })
        #expect(sent[0].value(forHTTPHeaderField: "X-Account") == "substrate-sync-1a2b3c4d")

        let revoked = StubTransport()
        revoked.on("POST", path, status: 401, json: #"{"error":{"code":"unauthorized","message":"token revoked"}}"#)
        await #expect(throws: SessionExpired.self) {
            try await client(revoked).deliver(authority: "ada.example.com", trigger: "applehealth-webhook", body: Data())
        }
    }

    @Test func blobsParkedDeliveriesAndTheCheckSpeakTheirShapes() async throws {
        let stub = StubTransport()
        stub.on("PUT", "/api/v1/blobs", json: #"{"digest":"blob-sha256-abc","size":3}"#)
        stub.on(
            "GET", "/api/v1/substrate.reamde.dev/core/trigger/applehealth-webhook/parked",
            json:
                #"{"items":[{"fireId":"f0","lastError":"still filing","pending":true},{"fireId":"f1","lastError":"delivery accepted: waiting"},{"fireId":"f2","lastError":"bad day"}]}"#
        )
        stub.on(
            "POST", "/api/v1/authorizations/check",
            json: #"""
                {"scopes":{"missing":["write:providers.substrate.reamde.dev/apple/place"]},
                 "setup":[{"type":"package","action":"upgrade","package":{"id":"providers.substrate.reamde.dev/apple","required":3,"held":2,"shipped":3}}],
                 "expiresAt":"2026-12-01T10:00:00Z"}
                """#)
        let c = client(stub)
        let blob = try await c.putBlob(Data("abc".utf8), mediaType: "image/png", name: "a photo.png")
        #expect(blob.digest == "blob-sha256-abc")
        #expect(stub.requests[0].url.flatMap { URLComponents(url: $0, resolvingAgainstBaseURL: false)?.percentEncodedQuery } == "name=a%20photo.png")
        let parked = try await c.parked(trigger: "applehealth-webhook")
        // The server's mark decides; a row without one, from a server that
        // marks none, by its words.
        #expect(parked.map(\.refused) == [false, false, true])
        let session = Session(
            server: try #require(URL(string: "https://ada.example.com")), repository: "ada.example.com",
            secret: "sub_app_x", clientID: "c", scopes: [])
        let result = try await c.checkAuthorization(syncManifest().check(for: session))
        #expect(result.missingScopes == ["write:providers.substrate.reamde.dev/apple/place"])
        #expect(result.setup.first?.package?.held == 2)
        #expect(result.expiresAt == Timestamp.parse("2026-12-01T10:00:00Z"))
        let body = try #require(stub.sent("POST", "/api/v1/authorizations/check").first?.httpBody)
        let sent = try #require(try JSONSerialization.jsonObject(with: body) as? [String: Any])
        #expect((sent["scope"] as? String)?.hasPrefix("write:providers.substrate.reamde.dev/apple/*") == true)
        #expect((sent["details"] as? [Any])?.count == 4)
    }
}

actor WaitRecorder {
    var all: [TimeInterval] = []
    func add(_ wait: TimeInterval) { all.append(wait) }
}
