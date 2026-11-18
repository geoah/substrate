import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing

@testable import ChatKit

let origin = URL(string: "https://s.example.com")!
let repository = "r.example.com"
let helper = "x.example.com/pkg/helper"
let identity = "https://s.example.com|r.example.com"

func threadJSON(version: Int, status: String = "ok") -> String {
    #"{"id":"t1","kind":"substrate.reamde.dev/llm/thread","version":\#(version),"createdAt":"2026-10-31T10:00:00Z","updatedAt":"2026-10-31T10:00:05Z","properties":{"mode":"chat","status":"\#(status)","agent":{"ref":"substrate.reamde.dev/core/agent/x.example.com/pkg/helper"},"finishedAt":"2026-10-31T10:00:05Z","lastReply":"Hello"}}"#
}

/// The newest-first page the transcript read answers with.
let rowsJSON =
    #"{"head":10,"generation":"g","records":[{"id":"m2","kind":"substrate.reamde.dev/llm/message","version":1,"createdAt":"2026-10-31T10:00:04Z","updatedAt":"2026-10-31T10:00:04Z","properties":{"role":"assistant","content":"Hello","turn":1,"thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}}},{"id":"m1","kind":"substrate.reamde.dev/llm/message","version":1,"createdAt":"2026-10-31T10:00:01Z","updatedAt":"2026-10-31T10:00:01Z","properties":{"role":"user","content":"Hi","turn":0,"thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}}}]}"#

let agentsJSON =
    #"{"head":10,"generation":"g","records":[{"id":"x.example.com/pkg/helper","kind":"substrate.reamde.dev/core/agent","version":1,"createdAt":"2026-10-30T10:00:00Z","updatedAt":"2026-10-30T10:00:00Z","properties":{"description":"Helps."}}]}"#

let emptyPage = #"{"head":10,"generation":"g","records":[]}"#

let otherAgent = "x.example.com/pkg/other"

/// One agent, as a list carries it.
func agentJSON(_ id: String) -> String {
    #"{"id":"\#(id)","kind":"substrate.reamde.dev/core/agent","version":1,"createdAt":"2026-10-30T10:00:00Z","updatedAt":"2026-10-30T10:00:00Z","properties":{"description":"Helps."}}"#
}

/// The answer to a cursor the server can no longer resume.
let compactedJSON =
    #"{"error":{"code":"compacted","message":"the cursor can no longer be resumed: list again","head":10,"generation":"g"}}"#

let replyStream = [
    #"{"kind":"thread","thread":"t1"}"#,
    #"{"kind":"delta","text":"Hel","turn":1}"#,
    #"{"kind":"delta","text":"lo","turn":1}"#,
    #"{"kind":"done","result":{"reply":"Hello","thread":"t1","status":"ok","turns":2}}"#,
]

/// A stream's events, as a test writes them.
extension AgentEvent {
    static func delta(_ text: String, turn: Int? = nil, at: Int? = nil) -> AgentEvent {
        var event = AgentEvent(kind: .delta)
        (event.text, event.turn, event.at) = (text, turn, at)
        return event
    }

    static func reasoning(_ text: String, turn: Int? = nil, at: Int? = nil) -> AgentEvent {
        var event = AgentEvent(kind: .reasoning)
        (event.text, event.turn, event.at) = (text, turn, at)
        return event
    }

    static func toolStarted(_ id: String, _ name: String = "query", turn: Int? = nil, callable: String? = nil, args: String = "") -> AgentEvent {
        var event = AgentEvent(kind: .toolStarted)
        (event.id, event.tool, event.turn, event.callable, event.args) = (id, name, turn, callable, args)
        return event
    }

    static func toolFinished(_ id: String, _ name: String = "query", turn: Int? = nil, output: String = "{}", ok: Bool = true, job: String? = nil)
        -> AgentEvent
    {
        var event = AgentEvent(kind: .toolFinished)
        (event.id, event.tool, event.turn, event.output, event.ok, event.job) = (id, name, turn, output, ok, job)
        return event
    }
}

extension LiveRows {
    /// Folds events in order, at instants a second apart from `start`.
    mutating func hear(_ events: [AgentEvent], from start: Date = Date(timeIntervalSince1970: 100)) {
        for (index, event) in events.enumerated() { apply(event, seq: turns.count + index + 1, now: start.addingTimeInterval(Double(index))) }
    }
}

/// The routes every test starts from: the agent, an empty list, the thread
/// and its rows.
func baseRoutes(threads: String = emptyPage, threadVersion: Int = 3, status: String = "ok") -> [ChatServer.Route] {
    [
        .init(method: "GET", path: "/llm/thread/t1", answer: .json(200, threadJSON(version: threadVersion, status: status))),
        .init(method: "GET", path: "/api/v1/records", query: "llm/message", answer: .json(200, rowsJSON)),
        .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, agentsJSON)),
        .init(method: "GET", path: "/api/v1/records", query: "llm/thread", answer: .json(200, threads)),
        .init(method: "GET", path: "/api/v1/records", answer: .json(200, emptyPage)),
        .init(method: "GET", path: "/api/v1/changes", answer: .stream([#"{"bookmark":10,"generation":"g"}"#], holdAfter: 1)),
    ]
}

/// The server's discovery document, saying how far its `agents` feature has
/// grown: 3 has the continue route. Without this route the document is not
/// read, and the chat calls nothing a revision added.
func featuresRoute(agents revision: Int) -> ChatServer.Route {
    .init(
        method: "GET", path: "/.well-known/substrate/server.json",
        answer: .json(200, #"{"server":{"version":"dev"},"features":[{"name":"agents","stability":"experimental","revision":\#(revision)}]}"#))
}

/// Chat's list on a runtime of its own, as the link runs it, against a
/// scripted server, a folder of its own, fake notifications and fake
/// background work.
struct ChatRig {
    let server: ChatServer
    let clock: TestClock
    let notifications = RecordedNotifications()
    let background = ScriptedBackground()
    let base: URL
    let storage: ChatStorage
    let preferences = MemoryPreferenceStore()
    let harness: ControllerHarness<ChatApp>
    let scopes: [String]?

    /// - Parameter base: the folder the device keeps its repositories in; a
    ///   rig over another's is the app launched again.
    /// - Parameter transport: a server of the test's own in place of the
    ///   scripted routes, such as one several rigs share (`MarkServer`).
    /// - Parameter clock: the clock, and `ids` the ids, of a world a launch
    ///   after a kill goes on in: it mints no id a launch before it did.
    /// - Parameter watch: told at every wait the app makes on the clock, as
    ///   it begins and as it ends (`WatchedClock`).
    init(
        _ routes: [ChatServer.Route] = baseRoutes(), scopes: [String]? = nil, platform: ChatPlatform = .macos,
        base: URL = temporaryFolder("chat"), transport: (any HTTPTransport)? = nil, clock: TestClock = TestClock(),
        ids: any IDSource = SequentialIDs(), watch: (@Sendable (String) -> Void)? = nil, page: Int = 100,
        push: PushRegistrar? = nil
    ) {
        server = ChatServer(routes)
        self.clock = clock
        self.scopes = scopes
        self.base = base
        storage = ChatStorage(base: base)
        let transport = transport ?? server
        var client = SubstrateClient(server: origin, token: "secret", transport: transport)
        // The client waits out a busy server's 429 or 503 before it asks
        // again: on the rig's clock, like every other wait.
        // A seeded run watches every wait the app makes on it.
        let waits: any RuntimeClock = watch.map { WatchedClock(clock: clock, watch: $0) } ?? clock
        client.pause = { seconds in try await waits.sleep(until: waits.now.addingTimeInterval(seconds)) }
        var environment = AppEnvironment(
            clock: waits, ids: ids, locale: Locale(identifier: "en_US"),
            timeZone: TimeZone(identifier: "UTC")!, client: .fixed(client),
            devices: DeviceServices(transport: transport, notifications: notifications, background: background))
        environment[PushRegistrarKey.self] = push
        harness = ControllerHarness(
            ChatApp(storage: storage, preferences: preferences, platform: platform, verifiesParts: true, page: page), clock: clock,
            environment: environment)
    }

    var runtime: Runtime<ChatApp> { harness.runtime }

    func signIn() async throws {
        await runtime.linkChanged(LinkSnapshot(phase: .signedIn, server: origin, repository: repository, scopes: scopes, online: true))
        try await harness.until { harness in
            let state = await harness.state
            return state.loaded && !state.refreshing
        }
    }

    /// Signed in, with the server's features read.
    func signIn(agents revision: Int) async throws {
        server.route("GET", "/.well-known/substrate/server.json", featuresRoute(agents: revision).answer)
        try await signIn()
        try await harness.until { harness in await harness.state.agentsRevision == revision }
    }

    func signOut() async {
        await runtime.linkChanged(LinkSnapshot(phase: .signedOut, generation: 1, online: true))
    }

    /// The repository's folder, where every build kept it.
    var root: URL { base.appendingPathComponent(ChatStorage.hash(identity), isDirectory: true) }

    /// The link generation the repository's folder holds.
    var generation: Int {
        get throws { LinkGeneration(folder: try RepositoryFolder(adopting: root)).current }
    }

    /// The session's lease on the folder, once it was read.
    func lease() async throws -> ChatStorage.Lease { try #require(await harness.state.disk) }

    /// A new chat with the helper in scene `s`, and its key.
    func newChat(_ scene: String = "s") async throws -> ChildKey {
        await harness.send(.newChat(agent: helper, listen: false, scene: scene))
        return try #require(await harness.state.scenes[scene]?.current)
    }

    func send(_ command: Conversation.Command, to key: ChildKey) async throws {
        try await runtime.send(command, to: [key])
    }

    func conversation(_ key: ChildKey) async -> ConversationScreen? {
        await harness.presentation.conversations[key]
    }

    func turns(_ key: ChildKey) async -> [String] {
        (await conversation(key)?.turns ?? []).map { turn in
            switch turn.body {
            case let .user(user): user.text
            case let .assistant(assistant): assistant.text ?? ""
            default: ""
            }
        }
    }

    func live(_ key: ChildKey) async -> ConversationState? { await runtime.state(of: key) }

    /// The failure card under the chat's newest turn.
    func card(_ key: ChildKey) async -> FailurePresentation? {
        guard let screen = await conversation(key), let last = screen.turns.last else { return nil }
        return screen.failures.turns[last.id]
    }

    /// The strip above the chat's composer.
    func strip(_ key: ChildKey) async -> FailurePresentation? { await conversation(key)?.failures.strip }
}
