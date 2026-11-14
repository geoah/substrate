import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
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

/// Chat's list on a runtime of its own, as the link runs it, against a
/// scripted server, a folder of its own, fake notifications and fake
/// background work.
struct ChatRig {
    let server: ChatServer
    let clock = TestClock()
    let notifications = RecordedNotifications()
    let background = ScriptedBackground()
    let base: URL
    let storage: ChatStorage
    let preferences = MemoryPreferenceStore()
    let harness: ControllerHarness<ChatApp>
    let scopes: [String]?

    /// - Parameter base: the folder the device keeps its repositories in; a
    ///   rig over another's is the app launched again.
    init(
        _ routes: [ChatServer.Route] = baseRoutes(), scopes: [String]? = nil, platform: ChatPlatform = .macos,
        base: URL = temporaryFolder("chat")
    ) {
        server = ChatServer(routes)
        self.scopes = scopes
        self.base = base
        storage = ChatStorage(base: base)
        let client = SubstrateClient(server: origin, token: "secret", transport: server)
        let environment = AppEnvironment(
            clock: clock, ids: SequentialIDs(), locale: Locale(identifier: "en_US"),
            timeZone: TimeZone(identifier: "UTC")!, client: .fixed(client),
            devices: DeviceServices(transport: server, notifications: notifications, background: background))
        harness = ControllerHarness(
            ChatApp(storage: storage, preferences: preferences, platform: platform), clock: clock,
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
}
