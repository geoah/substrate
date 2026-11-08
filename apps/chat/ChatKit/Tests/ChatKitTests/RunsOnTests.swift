import Foundation
import SubstrateCore
import SubstrateScreens
import Testing

@testable import ChatKit

// An agent on `runsOn` (decision record 0242), read as the server chooses:
// a new chat takes the first entry whose model record and provider row are
// live and whose row has a key of its own, or is served by the operator with
// the model on its list; readiness never moves it; a continued chat stays on
// the row its thread is stamped with.

private func page(_ records: [String]) -> String {
    #"{"head":10,"generation":"g","records":["# + records.joined(separator: ",") + "]}"
}

private func record(_ kind: String, _ id: String, _ properties: String, deleted: Bool = false) -> String {
    let gone = deleted ? #","deletedAt":"2026-10-31T09:00:00Z""# : ""
    return
        #"{"id":"\#(id)","kind":"\#(kind)","version":1,"createdAt":"2026-10-30T10:00:00Z","updatedAt":"2026-10-30T10:00:00Z"\#(gone),"properties":\#(properties)}"#
}

private let opus = "anthropic:claude-opus-5-5"
/// A record id may hold a slash: nothing reads the row or the model out of it.
private let gpt5 = "openai/gpt-5"

private let agentOnModels = page([
    record(
        Kinds.agent, helper,
        #"{"description":"Helps.","runsOn":[{"ref":"substrate.reamde.dev/llm/model/\#(opus)"},{"ref":"substrate.reamde.dev/llm/model/\#(gpt5)"}]}"#)
])

private func model(_ id: String, row: String, model: String, deleted: Bool = false) -> String {
    record(
        Kinds.model, id, #"{"provider":{"ref":"substrate.reamde.dev/llm/provider/\#(row)"},"model":"\#(model)"}"#,
        deleted: deleted)
}

private let bothModels = page([
    model(opus, row: "anthropic", model: "claude-opus-5-5"), model(gpt5, row: "openai", model: "gpt-5"),
])

private func provider(_ id: String, keyed: Bool) -> String {
    record(Kinds.provider, id, keyed ? #"{"wire":"openai","apiKey":"sealed"}"# : #"{"wire":"openai"}"#)
}

/// The operator's slot for a row it serves, with the models its key covers.
private func served(_ row: String, models: [String], readiness: String = "ready") -> String {
    let list = models.map { "\"\($0)\"" }.joined(separator: ",")
    return
        #"{"id":"llm/\#(row)","slot":"llm","label":"\#(row)","source":"operator","operatorOffers":true,"readiness":"\#(readiness)","models":[\#(list)]}"#
}

/// A chat that ran on Anthropic's Opus.
private let stamped =
    #"{"id":"t1","kind":"substrate.reamde.dev/llm/thread","version":3,"createdAt":"2026-10-31T10:00:00Z","updatedAt":"2026-10-31T10:00:05Z","properties":{"mode":"chat","status":"ok","agent":{"ref":"substrate.reamde.dev/core/agent/x.example.com/pkg/helper"},"provider":"anthropic","model":"claude-opus-5-5","finishedAt":"2026-10-31T10:00:05Z","lastReply":"Hello"}}"#

private func routes(
    providers: [String], models: String = bothModels, operatorItems: [String]? = nil, threads: String = emptyPage
) -> [ChatServer.Route] {
    var out: [ChatServer.Route] = []
    if let operatorItems {
        out.append(
            .init(
                method: "GET", path: "/api/v1/operator/defaults",
                answer: .json(200, #"{"operator":{"name":"Example"},"items":["# + operatorItems.joined(separator: ",") + "]}")))
    }
    return out + [
        .init(method: "GET", path: "/llm/thread/t1", answer: .json(200, stamped)),
        .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, agentOnModels)),
        .init(method: "GET", path: "/api/v1/records", query: "llm/provider", answer: .json(200, page(providers))),
        .init(method: "GET", path: "/api/v1/records", query: "llm/model", answer: .json(200, models)),
    ] + baseRoutes(threads: threads)
}

/// Settings' row for the helper: what a new chat runs on, and the key it
/// lacks.
private func assistant(_ rig: ChatRig) async throws -> AssistantRow {
    try #require(await rig.harness.presentation.settings.assistants.rows.first { $0.id == helper })
}

private let anthropicKeyless = provider("anthropic", keyed: false)
private let openaiKeyed = provider("openai", keyed: true)
private let openaiKeyless = provider("openai", keyed: false)

@Suite("A new chat with an agent on runsOn", .timeLimit(.minutes(10)))
struct RunsOnNewChatTests {
    @Test func takesTheFirstEntryThatCanRun() async throws {
        let rig = ChatRig(routes(providers: [anthropicKeyless, openaiKeyed]))
        try await rig.signIn()
        let key = try await rig.newChat()
        let screen = try #require(await rig.conversation(key))
        #expect(screen.hero?.keyless == nil && screen.composer.keyless == nil)
        let row = try await assistant(rig)
        #expect(row.runsOn == [LabeledValue("Provider", "OpenAI"), LabeledValue("Model", "gpt-5", verbatim: true)])
        #expect(row.keyless == nil)
    }

    @Test func asksForTheFirstEntrysKeyWhereNoneCanRun() async throws {
        let rig = ChatRig(routes(providers: [anthropicKeyless, openaiKeyless]))
        try await rig.signIn()
        let key = try await rig.newChat()
        let hero = try #require(await rig.conversation(key)?.hero)
        #expect(hero.keyless?.provider == "anthropic" && hero.keyless?.title == "Add an API key for Anthropic")
        let row = try await assistant(rig)
        #expect(row.keyless?.id == "anthropic")
        #expect(row.runsOn.first == LabeledValue("Provider", "Anthropic"))
    }

    @Test func runsOnTheOperatorsKeyForAModelOnItsListWhateverItsReadiness() async throws {
        // The vendor refused the operator's key: that stops a run, it never
        // passes an entry over.
        let rig = ChatRig(
            routes(
                providers: [anthropicKeyless, openaiKeyless],
                operatorItems: [served("anthropic", models: ["claude-opus-5-5"], readiness: "failing")]))
        try await rig.signIn()
        let key = try await rig.newChat()
        #expect(try #require(await rig.conversation(key)).hero?.keyless == nil)
        let row = try await assistant(rig)
        #expect(row.runsOn == [LabeledValue("Provider", "Anthropic"), LabeledValue("Model", "claude-opus-5-5", verbatim: true)])
        #expect(await rig.harness.state.keys == .served(["anthropic": true], models: ["anthropic": ["claude-opus-5-5"]]))
    }

    @Test func passesOverAModelOffTheOperatorsList() async throws {
        let sonnetOnly = served("anthropic", models: ["claude-sonnet-5-5"])
        let rig = ChatRig(routes(providers: [anthropicKeyless, openaiKeyed], operatorItems: [sonnetOnly]))
        try await rig.signIn()
        #expect(try await assistant(rig).runsOn.first == LabeledValue("Provider", "OpenAI"))
        // With nothing else to run on, a key of the row's own would open it.
        let stuck = ChatRig(routes(providers: [anthropicKeyless, openaiKeyless], operatorItems: [sonnetOnly]))
        try await stuck.signIn()
        #expect(try await assistant(stuck).keyless?.id == "anthropic")
    }

    @Test func passesOverADeletedRecord() async throws {
        let anthropicKeyed = provider("anthropic", keyed: true)
        let missing = ChatRig(
            routes(providers: [anthropicKeyed, openaiKeyed], models: page([model(gpt5, row: "openai", model: "gpt-5")])))
        try await missing.signIn()
        #expect(try await assistant(missing).runsOn.first == LabeledValue("Provider", "OpenAI"))
        let tombstoned = ChatRig(
            routes(
                providers: [anthropicKeyed, openaiKeyed],
                models: page([
                    model(opus, row: "anthropic", model: "claude-opus-5-5", deleted: true),
                    model(gpt5, row: "openai", model: "gpt-5"),
                ])))
        try await tombstoned.signIn()
        #expect(try await assistant(tombstoned).runsOn.first == LabeledValue("Provider", "OpenAI"))
    }

    @Test func passesOverADeletedRow() async throws {
        let rig = ChatRig(routes(providers: [openaiKeyed]))
        try await rig.signIn()
        #expect(try await assistant(rig).runsOn.first == LabeledValue("Provider", "OpenAI"))
        #expect(try await assistant(rig).keyless == nil)
    }
}

@Suite("A continued chat with an agent on runsOn", .timeLimit(.minutes(10)))
struct RunsOnContinuedTests {
    @Test func staysOnItsOwnRowWhereALaterEntryHasAKey() async throws {
        let rig = ChatRig(routes(providers: [anthropicKeyless, openaiKeyed], threads: page([stamped])))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        let screen = try #require(await rig.conversation(key))
        #expect(screen.composer.keyless?.provider == "anthropic")
        // A new chat would run on OpenAI's key.
        #expect(try await assistant(rig).keyless == nil)
        #expect(await rig.harness.state.provider(of: helper, thread: "t1")?.id == "anthropic")
    }
}

@Suite("The model records a session reads", .timeLimit(.minutes(10)))
struct RunsOnReadTests {
    @Test func aFullSessionReadsAndWatchesThem() async throws {
        let rig = ChatRig(routes(providers: [anthropicKeyless, openaiKeyed]))
        try await rig.signIn()
        #expect(await rig.harness.state.models?.keys.sorted() == [gpt5, opus].sorted())
        try await rig.harness.until { _ in rig.server.count("/api/v1/changes") > 0 }
        #expect(rig.server.count("/api/v1/changes", query: Kinds.model) > 0)
    }

    /// An earlier build's scoped session asked for no scope over them, and
    /// the watch refuses a kind the token cannot read.
    @Test func aScopedSessionNeitherReadsNorWatchesThem() async throws {
        let rig = ChatRig(routes(providers: [anthropicKeyless, openaiKeyed]), scopes: ChatManifest.earliestRequest)
        try await rig.signIn()
        try await rig.harness.until { _ in rig.server.count("/api/v1/changes") > 0 }
        #expect(!rig.server.requests.contains { $0.contains(Kinds.model) })
        #expect(await rig.harness.state.models == nil)
        // Which entry runs cannot be told, so no key is asked for.
        let key = try await rig.newChat()
        #expect(try #require(await rig.conversation(key)).hero?.keyless == nil)
    }

    /// The refusal a server older than the kind answers the records read
    /// with: an undeclared filter kind is an unknown type.
    @Test func aServerOlderThanTheKindStillListsItsAgents() async throws {
        let older = ChatServer.Route(
            method: "GET", path: "/api/v1/records", query: "llm/model",
            answer: .json(
                422,
                #"{"error":{"code":"validation","message":"substrate: validation failed: unknown type \"substrate.reamde.dev/llm/model\""}}"#))
        let rig = ChatRig([older] + routes(providers: [anthropicKeyless, openaiKeyed]))
        try await rig.signIn()
        #expect(await rig.harness.state.agents.count == 1)
        #expect(await rig.harness.state.models == [:])
    }

    @Test func onlyThatRefusalReadsAsNoModels() {
        #expect(
            ChatApp.modelKindUnknown(
                ServerError(
                    status: 422, code: "validation",
                    message: #"substrate: validation failed: unknown type "substrate.reamde.dev/llm/model""#)))
        #expect(
            !ChatApp.modelKindUnknown(
                ServerError(status: 422, code: "validation", message: #"substrate: validation failed: unknown type "x.example.com/a/b""#)))
        #expect(!ChatApp.modelKindUnknown(ServerError(status: 404, code: "not_found", message: "not found")))
    }

    /// The watch opens at the earliest head of the reads in one history, the
    /// models' included, so a model written between the reads is not missed.
    @Test func theWatchOpensAtTheModelsHeadWhenItIsTheEarliest() async throws {
        let earlier = #"{"head":5,"generation":"g","records":["# + model(opus, row: "anthropic", model: "claude-opus-5-5") + "]}"
        let rig = ChatRig(routes(providers: [anthropicKeyless, openaiKeyed], models: earlier))
        try await rig.signIn()
        try await rig.harness.until { _ in rig.server.count("/api/v1/changes") > 0 }
        #expect(rig.server.requests.contains { $0.contains("/api/v1/changes") && $0.contains("from=5") })
    }

    @Test func theCacheKeepsThemAndAnEarlierFileHasNone() throws {
        let held = CachedState(models: [Record(id: opus, kind: Kinds.model, properties: ["model": "claude-opus-5-5"])])
        let read = try JSONDecoder().decode(CachedState.self, from: JSONEncoder().encode(held))
        #expect(read.models?.map(\.id) == [opus])
        let earlier = try JSONDecoder().decode(
            CachedState.self,
            from: Data(
                #"{"format":1,"agents":[],"providers":[],"threads":[],"automated":[],"openings":{},"interactions":[],"requests":[],"seen":{},"notified":[],"baselined":false}"#
                    .utf8))
        #expect(earlier.models == nil)
    }
}

@Suite("Whether a row can take a model")
struct AvailabilityTests {
    private func row(_ id: String, keyed: Bool) -> Provider {
        Provider(record: Record(id: id, kind: Kinds.provider, properties: keyed ? ["apiKey": "sealed"] : [:]))
    }

    @Test func aKeyOfTheRowsOwnTakesAnyModel() {
        let keys = ProviderKeys.served(["openai": true], models: ["openai": ["gpt-5"]])
        #expect(keys.availability(row("openai", keyed: true), model: "o3") == .yes)
        #expect(ProviderKeys.rows.availability(row("openai", keyed: true), model: "o3") == .yes)
    }

    @Test func theOperatorsKeyTakesOnlyTheModelsOnItsList() {
        let keys = ProviderKeys.served(["openai": true], models: ["openai": ["gpt-5"]])
        #expect(keys.availability(row("openai", keyed: false), model: "gpt-5") == .yes)
        #expect(keys.availability(row("openai", keyed: false), model: "o3") == .no(.notServed("openai", served: ["gpt-5"])))
        #expect(keys.availability(row("gemini", keyed: false), model: "gemini-2.5-pro") == .no(.noKey("gemini")))
    }

    @Test func withoutTheOperatorsReadOnlyTheRowsOwnKeyCounts() {
        #expect(ProviderKeys.rows.availability(row("openai", keyed: false), model: "gpt-5") == .no(.noKey("openai")))
    }

    /// What an earlier build kept says the operator serves the row, but not
    /// which models: that waits for the next read.
    @Test func anEarlierBuildsReadCannotTellTheModels() {
        let keys = ProviderKeys.read(["openai": true, "gemini": false])
        #expect(keys.availability(row("openai", keyed: false), model: "gpt-5") == .unknown)
        #expect(keys.availability(row("gemini", keyed: false), model: "gemini-2.5-pro") == .no(.noKey("gemini")))
    }

    @Test func theRowAKeyWouldOpenIsTheFirstEntrysThatNamesALiveRow() {
        let choice = RunsOnChoice(entries: [
            RunsOnEntry(id: "gone", availability: .no(.noRecord)),
            RunsOnEntry(id: "x", availability: .no(.noProvider("deleted"))),
            RunsOnEntry(id: "y", availability: .no(.notServed("anthropic", served: []))),
            RunsOnEntry(id: "z", availability: .no(.noKey("openai"))),
        ])
        #expect(choice.canStart == false && choice.chosen == nil)
        #expect(choice.keyProvider == "anthropic")
        // One that cannot be told yet holds the answer open.
        let open = RunsOnChoice(entries: [RunsOnEntry(id: "a", availability: .unknown), RunsOnEntry(id: "b", availability: .no(.noKey("openai")))])
        #expect(open.canStart == nil && open.chosen?.id == "a" && open.keyProvider == nil)
    }
}
