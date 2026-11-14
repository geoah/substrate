import Foundation
import Testing

@testable import ChatKit

/// The helper on a keyless `openai` row.
private func keylessRoutes(threads: String = emptyPage, status: String = "ok") -> [ChatServer.Route] {
    let agents =
        #"{"head":10,"generation":"g","records":[{"id":"x.example.com/pkg/helper","kind":"substrate.reamde.dev/core/agent","version":1,"createdAt":"2026-10-30T10:00:00Z","updatedAt":"2026-10-30T10:00:00Z","properties":{"description":"Helps.","provider":{"ref":"substrate.reamde.dev/llm/provider/openai"}}}]}"#
    let providers =
        #"{"head":10,"generation":"g","records":[{"id":"openai","kind":"substrate.reamde.dev/llm/provider","version":1,"createdAt":"2026-10-30T10:00:00Z","updatedAt":"2026-10-30T10:00:00Z","properties":{"wire":"openai"}}]}"#
    return [
        .init(method: "GET", path: "/api/v1/records", query: "core/agent", answer: .json(200, agents)),
        .init(method: "GET", path: "/api/v1/records", query: "llm/provider", answer: .json(200, providers)),
    ] + baseRoutes(threads: threads, status: status)
}

/// The matching over the server's text, which a server that names causes
/// makes unnecessary: kept while a server that does not is still in use.
@Suite("A failed run read off the server's text", .timeLimit(.minutes(10)))
struct FailureWordsTests {
    /// What the agent loop sends after a provider's 401: its own wrapping
    /// first, the provider's answer inside.
    static let refusedKey =
        "substrate: validation failed: agent x.example.com/pkg/helper: llm: error, status code: 401, "
        + "status: 401 Unauthorized, message: Incorrect API key provided: sk-…abcd."

    static func card(_ text: String, provider: String?) -> FailurePresentation {
        var context = FailurePresenter.Context()
        context.agent = "Helper"
        context.provider = provider
        context.providerPage = provider.map { _ in "https://s.example.com/data/substrate.reamde.dev/llm/provider/openai" }
        return FailurePresenter.present(Failure(.reply, .run(status: "error", cause: nil), said: text), context)
    }

    @Test func aRefusedKeySaysSoAndKeepsTheServersWordsApart() {
        let card = Self.card(Self.refusedKey, provider: "OpenAI")
        #expect(card.tone == .bad)
        #expect(card.sentence == "OpenAI refused the API key.")
        #expect(card.button?.title == "Replace Key")
        #expect(card.button?.action == .open("https://s.example.com/data/substrate.reamde.dev/llm/provider/openai"))
        #expect(card.details == FailureDetails(title: "Details", label: "The server said", text: Self.refusedKey))
        #expect(!card.sentence.contains("substrate:") && card.note?.contains("substrate:") != true)
    }

    @Test func eachProvidersRefusalReadsAsOne() {
        for text in [
            "anthropic: authentication_error: invalid x-api-key", "API key not valid. Please pass a valid API key. API_KEY_INVALID",
            "401 Unauthorized",
        ] {
            #expect(FailureFallback.cause(text) == .keyRefused, "\(text)")
        }
        #expect(FailureFallback.cause("error, status code: 429, message: Rate limit reached") == .limited)
        #expect(FailureFallback.cause("You exceeded your current quota") == .limited)
        #expect(FailureFallback.cause("agent x: tool y: no such record") == .other)
        #expect(FailureFallback.cause("a run of 4012 tokens") == .other)
    }

    @Test func theTextNamesTheCauseAServerWouldHaveSent() {
        #expect(FailureFallback.cause(status: "error", reason: "401 Unauthorized") == .providerKeyRefused)
        #expect(FailureFallback.cause(status: "error", reason: "status code: 429, message: Rate limit reached") == .providerRateLimited)
        #expect(FailureFallback.cause(status: "error", reason: "You exceeded your current quota") == .providerBilling)
        #expect(FailureFallback.cause(status: "error", reason: "agent x: tool y: no such record") == nil)
        #expect(FailureFallback.cause(status: "overbudget", reason: "max turns reached (8)") == .turnLimit)
        #expect(FailureFallback.cause(status: "overbudget", reason: "max tool calls reached (32)") == .toolCallLimit)
        #expect(FailureFallback.cause(status: "overbudget", reason: "deadline exceeded after a last turn") == .deadline)
        #expect(FailureFallback.cause(status: "stopped", reason: "stopped: the server shut down during the turn") == .shutdown)
        #expect(FailureFallback.cause(status: "stopped", reason: "stopped by api") == .owner)
        #expect(FailureFallback.cause(status: "ok", reason: "401 Unauthorized") == nil)
    }

    @Test func aProviderUnknownToTheAppStillReadsWhole() {
        let limited = Self.card("status code: 429", provider: nil)
        #expect(limited.sentence == "The provider is limiting requests." && limited.button?.title == "Try Again")
        let other = Self.card("agent x: tool y: no such record", provider: nil)
        #expect(other.sentence == "Helper couldn't finish this reply." && other.note == nil)
        #expect(other.details?.text == "agent x: tool y: no such record")
        let key = Self.card("401 Unauthorized", provider: nil)
        #expect(key.sentence == "The provider refused the API key.")
        // No page to open: the one button is still there.
        #expect(key.button?.title == "Try Again")
    }

    @Test func anOpenedChatNamesItsProvider() async throws {
        let failed = threadJSON(version: 3, status: "error")
            .replacingOccurrences(of: #""lastReply":"Hello""#, with: #""reason":"401 Unauthorized""#)
        let threads = #"{"head":10,"generation":"g","records":[\#(failed)]}"#
        let rig = ChatRig(keylessRoutes(threads: threads, status: "error"))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        let card = try #require(await rig.card(key))
        #expect(card.sentence == "OpenAI refused the API key." && card.details?.text == "401 Unauthorized")
        #expect(
            card.button
                == FailureButton(title: "Replace Key", action: .open("https://s.example.com/data/substrate.reamde.dev/llm/provider/openai"), external: true))
    }
}

@Suite("A new chat on a keyless provider", .timeLimit(.minutes(10)))
struct KeylessHeroTests {
    @Test func theCalloutSitsAboveTheOpenersAndNotOverThem() async throws {
        let rig = ChatRig(keylessRoutes())
        try await rig.signIn()
        let key = try await rig.newChat()
        let screen = try #require(await rig.conversation(key))
        let hero = try #require(screen.hero)
        #expect(hero.prompts == ["What can you help me with?"])
        #expect(hero.keyless?.title == "Add an API key for OpenAI" && hero.keyless?.provider == "openai")
        // The composer's bar floats over the transcript: it carries no callout
        // while the hero does.
        #expect(screen.composer.keyless == nil)
    }

    @Test func aChatUnderWayKeepsTheCalloutOverTheComposer() async throws {
        let threads = #"{"head":10,"generation":"g","records":[\#(threadJSON(version: 3))]}"#
        let rig = ChatRig(keylessRoutes(threads: threads))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        let screen = try #require(await rig.conversation(key))
        #expect(screen.hero == nil)
        #expect(screen.composer.keyless?.title == "Add an API key for OpenAI")
    }
}

/// The operator's answer for the `openai` row (decision 0235).
private func operatorRoute(source: String, readiness: String) -> ChatServer.Route {
    let answer =
        #"{"operator":{"name":"Example"},"items":[{"id":"llm/openai","slot":"llm","label":"OpenAI","target":"substrate.reamde.dev/llm/provider/openai","source":"\#(source)","operatorOffers":true,"readiness":"\#(readiness)"}]}"#
    return .init(method: "GET", path: "/api/v1/operator/defaults", answer: .json(200, answer))
}

@Suite("A provider the operator keys", .timeLimit(.minutes(10)))
struct OperatorKeyTests {
    @Test func aRowWithoutAKeyOfItsOwnRunsOnTheOperatorsAndAsksForNone() async throws {
        let rig = ChatRig([operatorRoute(source: "operator", readiness: "ready")] + keylessRoutes())
        try await rig.signIn()
        let key = try await rig.newChat()
        let screen = try #require(await rig.conversation(key))
        #expect(screen.hero?.keyless == nil && screen.composer.keyless == nil)
        #expect(await rig.harness.state.keys == .served(["openai": true], models: ["openai": []]))
    }

    @Test func aRowNoKeyResolvesForStillAsksForOne() async throws {
        let rig = ChatRig([operatorRoute(source: "none", readiness: "not-set-up")] + keylessRoutes())
        try await rig.signIn()
        let key = try await rig.newChat()
        #expect(try #require(await rig.conversation(key)).hero?.keyless?.title == "Add an API key for OpenAI")
    }

    /// An app an earlier build signed in holds scopes, and the operator read
    /// refuses those: the row's own key says, as before.
    @Test func aScopedTokenNeverAsksTheOperator() async throws {
        let rig = ChatRig(
            [operatorRoute(source: "operator", readiness: "ready")] + keylessRoutes(), scopes: ChatManifest.earliestRequest)
        try await rig.signIn()
        let key = try await rig.newChat()
        #expect(try #require(await rig.conversation(key)).hero?.keyless?.title == "Add an API key for OpenAI")
        #expect(!rig.server.requests.contains { $0.contains("/operator/defaults") })
    }

    @Test func aFailedReadSaysNoRowLacksAKey() async throws {
        let failing = ChatServer.Route(
            method: "GET", path: "/api/v1/operator/defaults", answer: .json(500, #"{"error":{"code":"internal","message":"x"}}"#))
        let rig = ChatRig([failing] + keylessRoutes())
        try await rig.signIn()
        let key = try await rig.newChat()
        #expect(try #require(await rig.conversation(key)).hero?.keyless == nil)
    }
}
