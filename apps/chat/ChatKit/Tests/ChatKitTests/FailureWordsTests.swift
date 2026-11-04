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

@Suite("A failed run's card", .timeLimit(.minutes(10)))
struct FailureWordsTests {
    /// What the agent loop sends after a provider's 401: its own wrapping
    /// first, the provider's answer inside.
    static let refusedKey =
        "substrate: validation failed: agent x.example.com/pkg/helper: llm: error, status code: 401, "
        + "status: 401 Unauthorized, message: Incorrect API key provided: sk-…abcd."

    @Test func aRefusedKeySaysSoAndKeepsTheServersWordsApart() {
        let note = TurnFailureWords.note(Self.refusedKey, agent: "Helper", provider: "OpenAI")
        #expect(note.kind == .failed)
        #expect(note.title == "OpenAI refused the API key")
        #expect(note.message == "Helper couldn't answer. Replace the key on OpenAI's provider record, then send again.")
        #expect(note.detail == Self.refusedKey)
        #expect(!note.message.contains("substrate:") && note.title?.contains("substrate:") == false)
    }

    @Test func eachProvidersRefusalReadsAsOne() {
        for text in [
            "anthropic: authentication_error: invalid x-api-key", "API key not valid. Please pass a valid API key. API_KEY_INVALID",
            "401 Unauthorized",
        ] {
            #expect(TurnFailureWords.cause(text) == .keyRefused, "\(text)")
        }
        #expect(TurnFailureWords.cause("error, status code: 429, message: Rate limit reached") == .limited)
        #expect(TurnFailureWords.cause("You exceeded your current quota") == .limited)
        #expect(TurnFailureWords.cause("agent x: tool y: no such record") == .other)
        #expect(TurnFailureWords.cause("a run of 4012 tokens") == .other)
    }

    @Test func aProviderUnknownToTheAppStillReadsWhole() {
        let limited = TurnFailureWords.note("status code: 429", agent: "Helper", provider: nil)
        #expect(limited.title == "The provider is turning requests down")
        let other = TurnFailureWords.note("agent x: tool y: no such record", agent: "Helper", provider: nil)
        #expect(other.title == "Helper couldn't answer" && other.message == "The run ended in an error.")
        #expect(other.detail == "agent x: tool y: no such record")
        let key = TurnFailureWords.note("401 Unauthorized", agent: "Helper", provider: nil)
        #expect(key.title == "The provider refused the API key")
        #expect(key.message == "Helper couldn't answer. Replace the key on the provider record, then send again.")
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
        let note = try #require(await rig.conversation(key)?.status.first)
        #expect(note.title == "OpenAI refused the API key" && note.detail == "401 Unauthorized")
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
