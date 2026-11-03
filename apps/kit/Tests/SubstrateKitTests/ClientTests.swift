import Foundation
import Testing
@testable import SubstrateKit

@Suite struct Requests {
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
        stub.on("GET", "/api/v1/substrate.reamde.dev/core/agent/x.example.com%2Fllm%2Fhelper",
                json: #"{"id":"x.example.com/llm/helper","kind":"substrate.reamde.dev/core/agent","properties":{"name":"Helper"},"version":1}"#)
        let record = try await client(stub).record("substrate.reamde.dev/core/agent", "x.example.com/llm/helper")
        #expect(record?["name"] == "Helper")
        #expect(stub.requests.first?.value(forHTTPHeaderField: "Authorization") == "Bearer sub_x")
    }

    @Test func aMissingOrDeletedRecordIsAbsent() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/a.example.com/tasks/task/gone", json: #"{"id":"gone","kind":"a.example.com/tasks/task","properties":{},"version":2,"deletedAt":"2026-10-31T10:00:00Z"}"#)
        #expect(try await client(stub).record("a.example.com/tasks/task", "gone") == nil)
        #expect(try await client(stub).record("a.example.com/tasks/task", "never") == nil)
    }

    @Test func theRecordReaderNoLongerEncodesTwice() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/substrate.reamde.dev/core/agent/x.example.com%2Fllm%2Fhelper",
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

    @Test func aRefusalBecomesTheErrorThatSaysWhatItMeans() async throws {
        let stub = StubTransport()
        stub.on("GET", "/api/v1/a/expired", status: 401, json: #"{"error":{"code":"unauthorized","message":"token expired"}}"#)
        stub.on("GET", "/api/v1/a/scope", status: 403, headers: ["WWW-Authenticate": #"Bearer error="insufficient_scope""#],
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
        #expect(explain(SessionExpired(), app: "Substrate Chat", server: host)
            == "Ada.example.com no longer accepts Substrate Chat's sign-in. Sign in again to carry on.")
        #expect(explain(URLError(.timedOut), app: "X", server: host) == "Ada.example.com took too long to answer.")
        #expect(explain(URLError(.cannotFindHost), app: "X", server: host) == "Couldn't find ada.example.com. Check the address.")
        #expect(explain(ServerError(status: 500, message: "boom"), app: "X", server: host) == "Ada.example.com ran into a problem (500): boom")
        #expect(explain(ServerError(status: 409, message: "taken"), app: "X", server: host) == "taken")
        #expect(isUnreachable(URLError(.cannotConnectToHost)) && !isUnreachable(SessionExpired()))
        #expect(isCancellation(URLError(.cancelled)) && isCancellation(CancellationError()))
    }

    @Test func aWebhookDeliveryCarriesTheTokenAndWaitsOutABusyDoor() async throws {
        let stub = StubTransport()
        let path = "/webhooks/ada.example.com/applehealth-webhook"
        stub.on("POST", path, status: 503, headers: ["Retry-After": "1"], json: #"{"error":{"code":"unavailable","message":"busy"}}"#)
        stub.on("POST", path, status: 202, json: #"{"fire":"f-123"}"#)
        let fire = try await client(stub, token: "sub_app_x").deliver(authority: "ada.example.com", trigger: "applehealth-webhook",
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
        stub.on("GET", "/api/v1/substrate.reamde.dev/core/trigger/applehealth-webhook/parked",
                json: #"{"items":[{"fireId":"f0","lastError":"still filing","pending":true},{"fireId":"f1","lastError":"delivery accepted: waiting"},{"fireId":"f2","lastError":"bad day"}]}"#)
        stub.on("POST", "/api/v1/authorizations/check", json: #"""
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
        let session = Session(server: try #require(URL(string: "https://ada.example.com")), repository: "ada.example.com",
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
