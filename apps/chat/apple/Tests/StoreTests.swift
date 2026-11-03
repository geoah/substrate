import Foundation
import Testing
import SubstrateKit
@testable import SubstrateChat

/// A server scripted per test: each request is answered by the first route
/// whose method and path match, with a JSON body, an error envelope, or an
/// ndjson stream that can be held open until the test releases it.
final class ScriptedServer: URLProtocol, @unchecked Sendable {
    enum Answer {
        case json(Int, String)
        case stream([String], holdAfter: Int? = nil)
    }

    struct Route {
        let method: String
        let path: String
        let query: String?
        let answer: Answer
    }

    nonisolated(unsafe) static var routes: [Route] = []
    nonisolated(unsafe) static var requests: [String] = []
    nonisolated(unsafe) static var released = false
    private static let lock = NSLock()

    static func reset(_ routes: [Route]) {
        lock.withLock {
            self.routes = routes
            requests = []
            released = false
        }
    }

    static func release() { lock.withLock { released = true } }

    static func count(_ path: String, query: String? = nil) -> Int {
        lock.withLock {
            requests.count { $0.contains(path) && (query.map($0.contains) ?? true) }
        }
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() {}

    override func startLoading() {
        let url = request.url!
        let path = url.path(percentEncoded: true)
        let query = url.query(percentEncoded: false) ?? ""
        let method = request.httpMethod ?? "GET"
        let route = Self.lock.withLock { () -> Route? in
            Self.requests.append("\(method) \(path)?\(query)")
            return Self.routes.first { $0.method == method && path.hasSuffix($0.path) && ($0.query.map(query.contains) ?? true) }
        }
        guard let route else {
            return answer(404, body: #"{"error":{"code":"not_found","message":"no route"}}"#)
        }
        switch route.answer {
        case let .json(status, body):
            answer(status, body: body)
        case let .stream(lines, holdAfter):
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: "HTTP/1.1",
                                           headerFields: ["Content-Type": "application/x-ndjson"])!
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            DispatchQueue.global().async {
                for (i, line) in lines.enumerated() {
                    if let hold = holdAfter, i == hold {
                        while !Self.lock.withLock({ Self.released }) { Thread.sleep(forTimeInterval: 0.02) }
                    }
                    Thread.sleep(forTimeInterval: 0.01)
                    self.client?.urlProtocol(self, didLoad: Data((line + "\n").utf8))
                }
                self.client?.urlProtocolDidFinishLoading(self)
            }
        }
    }

    private func answer(_ status: Int, body: String) {
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1",
                                       headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
}

private let agent = "x.example.com/pkg/helper"

private func threadJSON(version: Int, status: String = "ok") -> String {
    #"{"id":"t1","kind":"substrate.reamde.dev/llm/thread","version":\#(version),"createdAt":"2026-10-31T10:00:00Z","updatedAt":"2026-10-31T10:00:05Z","properties":{"mode":"chat","status":"\#(status)","agent":{"ref":"substrate.reamde.dev/core/agent/x.example.com/pkg/helper"},"finishedAt":"2026-10-31T10:00:05Z","lastReply":"Hello"}}"#
}

/// The newest-first page the transcript read answers with.
private let rowsJSON = #"{"head":10,"generation":"g","records":[{"id":"m2","kind":"substrate.reamde.dev/llm/message","version":1,"createdAt":"2026-10-31T10:00:04Z","updatedAt":"2026-10-31T10:00:04Z","properties":{"role":"assistant","content":"Hello","turn":1,"thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}}},{"id":"m1","kind":"substrate.reamde.dev/llm/message","version":1,"createdAt":"2026-10-31T10:00:01Z","updatedAt":"2026-10-31T10:00:01Z","properties":{"role":"user","content":"Hi","turn":0,"thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}}}]}"#

private let replyStream = [
    #"{"kind":"thread","thread":"t1"}"#,
    #"{"kind":"delta","text":"Hel","turn":1}"#,
    #"{"kind":"delta","text":"lo","turn":1}"#,
    #"{"kind":"done","result":{"reply":"Hello","thread":"t1","status":"ok","turns":2}}"#,
]

private func routes(chat: ScriptedServer.Answer, version: Int = 3) -> [ScriptedServer.Route] {
    [
        .init(method: "POST", path: "/chat", query: nil, answer: chat),
        .init(method: "GET", path: "/llm/thread/t1", query: nil, answer: .json(200, threadJSON(version: version))),
        .init(method: "GET", path: "/api/v1/records", query: "llm/message", answer: .json(200, rowsJSON)),
    ]
}

/// The store's rules (DESIGN.md, "The store and its concurrency rules")
/// against a scripted server. Serialized: the transport is process-wide.
@Suite(.serialized) @MainActor struct StoreRules {
    let store: ChatStore

    init() {
        let scripted = [ScriptedServer.self] as [AnyClass]
        API.session = URLSessionTransport.makeSession(timeout: 5, protocolClasses: scripted)
        API.streaming = URLSessionTransport.makeSession(timeout: 5, protocolClasses: scripted)
        let id = UUID().uuidString.lowercased()
        store = ChatStore(session: Session(server: URL(string: "https://\(id).example.com")!,
                                           repository: "\(id).example.com", secret: "secret", tokenID: "token"))
    }

    private func eventually(_ seconds: Double = 5, _ condition: () -> Bool) async -> Bool {
        let deadline = Date().addingTimeInterval(seconds)
        while Date() < deadline {
            if condition() { return true }
            try? await Task.sleep(for: .milliseconds(20))
        }
        return condition()
    }

    @Test func aSendStreamsThenHandsOverWithoutDoubling() async {
        ScriptedServer.reset(routes(chat: .stream(replyStream)))
        defer { store.close(forget: true) }
        let c = store.newConversation(agent: agent)
        c.draft = "Hi"
        c.send()
        #expect(await eventually { c.settles == 1 })
        #expect(c.threadID == "t1")
        #expect(c.phase == .idle)
        #expect(c.overlay.turns.isEmpty)
        #expect(c.turns.map(\.content) == ["Hi", "Hello"])
        // The thread finds its conversation, the one that minted it.
        #expect(store.conversation(thread: "t1") === c)
    }

    @Test func noReadPublishesUnderALiveOverlay() async {
        ScriptedServer.reset(routes(chat: .stream(replyStream, holdAfter: 2)))
        defer { store.close(forget: true) }
        let c = store.newConversation(agent: agent)
        c.draft = "Hi"
        c.send()
        #expect(await eventually { c.threadID == "t1" && c.overlay.turns.contains { $0.role == .assistant } })
        // An ordinary read of the thread while it streams: refused outright.
        store.load("t1", force: true)
        try? await Task.sleep(for: .milliseconds(300))
        #expect(ScriptedServer.count("/api/v1/records", query: "llm/message") == 0)
        #expect(store.rows["t1"] == nil)
        ScriptedServer.release()
        #expect(await eventually { c.settles == 1 })
        #expect(c.turns.map(\.content) == ["Hi", "Hello"])
    }

    @Test func aRefusalGivesTheTextBackAndNothingRuns() async {
        let refusal = #"{"error":{"code":"conflict","message":"a turn is already running on this thread"}}"#
        ScriptedServer.reset(routes(chat: .json(409, refusal)))
        defer { store.close(forget: true) }
        let c = store.newConversation(agent: agent)
        c.draft = "Hi"
        c.send()
        #expect(await eventually { !c.outcomes.isEmpty })
        #expect(c.outcomes.first?.kind == .refused)
        #expect(c.outcomes.first?.reason == "a turn is already running on this thread")
        #expect(c.draft == "Hi")
        #expect(c.phase == .idle)
        #expect(c.overlay.turns.isEmpty)
        #expect(c.threadID == nil)
    }

    @Test func aGatewayAnswerIsUnknownDeliveryNeverARefusal() async {
        ScriptedServer.reset(routes(chat: .json(502, "<html>bad gateway</html>")))
        defer { store.close(forget: true) }
        let c = store.newConversation(agent: agent)
        c.draft = "Hi"
        c.send()
        #expect(await eventually(4) { c.outcomes.contains { $0.kind == .unknown } })
        #expect(!c.outcomes.contains { $0.kind == .refused })
        // Nothing is sent again on its own.
        #expect(ScriptedServer.count("/chat") == 1)
    }

    @Test func olderRowsNeverReplaceNewerOnes() {
        defer { store.close(forget: true) }
        let newer = [Record(id: "a", kind: Kinds.message, properties: ["role": "user", "content": "new"])]
        let older = [Record(id: "b", kind: Kinds.message, properties: ["role": "user", "content": "old"])]
        store.publish("t1", records: newer, version: 5, full: false)
        store.publish("t1", records: older, version: 3, full: false)
        #expect(store.rows["t1"]?.first?.id == "a")
    }

    @Test func aClosedSessionPublishesNothing() {
        store.close(forget: true)
        store.publish("t1", records: [Record(id: "a", kind: Kinds.message, properties: [:])], version: 1, full: false)
        #expect(store.rows["t1"] == nil)
    }

    @Test func anAppTokenListsTheCardsUnderTheThreadsItKnows() async {
        let empty = #"{"head":10,"generation":"g","records":[]}"#
        let interaction = #"{"head":10,"generation":"g","records":[{"id":"i1","kind":"substrate.reamde.dev/llm/interaction","version":1,"createdAt":"2026-10-31T10:00:02Z","updatedAt":"2026-10-31T10:00:02Z","properties":{"state":"pending","thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}}}]}"#
        let named = #"llm/interaction"],"properties":{"state":{"eq":"pending"},"thread":{"in":["t1"]}}"#
        let forbidden = #"{"error":{"code":"forbidden","message":"an app's token lists it under filter.properties.thread"}}"#
        ScriptedServer.reset([
            .init(method: "GET", path: "/api/v1/records", query: "llm/thread", answer: .json(200, #"{"head":10,"generation":"g","records":[\#(threadJSON(version: 3))]}"#)),
            .init(method: "GET", path: "/api/v1/records", query: named, answer: .json(200, interaction)),
            .init(method: "GET", path: "/api/v1/records", query: "llm/interaction", answer: .json(403, forbidden)),
            .init(method: "GET", path: "/api/v1/records", query: "llm/message", answer: .json(200, rowsJSON)),
            .init(method: "GET", path: "/api/v1/records", query: nil, answer: .json(200, empty)),
        ])
        let id = UUID().uuidString.lowercased()
        let app = ChatStore(session: Session(server: URL(string: "https://\(id).example.com")!, repository: "\(id).example.com",
                                             secret: "secret", clientID: "http://127.0.0.1/substrate-chat", scopes: ["agents:*"]))
        defer { app.close(forget: true) }
        await app.refreshAll()
        #expect(app.refreshError == nil)
        #expect(app.interactions.map(\.id) == ["i1"])
        #expect(ScriptedServer.count("/api/v1/records", query: "llm/interaction")
            == ScriptedServer.count("/api/v1/records", query: named))
        // Without `decide:` the suggestions are not asked for at all.
        #expect(ScriptedServer.count("/api/v1/records", query: "recordpatchrequest") == 0)
    }

    @Test func aWindowLeavingKeepsAConversationAnotherShows() {
        defer { store.close(forget: true) }
        let c = store.conversation(thread: "t1")
        let a = UUID(), b = UUID()
        store.retain(c, scene: a)
        store.retain(c, scene: b)
        store.release(c, scene: a)
        #expect(store.conversation(thread: "t1") === c)
        store.release(c, scene: b)
        #expect(store.conversation(thread: "t1") !== c)
    }
}
