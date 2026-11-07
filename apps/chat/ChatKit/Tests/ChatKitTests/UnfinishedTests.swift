import Foundation
import SubstrateCore
import SubstrateRuntime
import Testing

@testable import ChatKit

/// A chat with the helper whose last turn ended as `status` says.
private func ended(_ id: String, _ subject: String, _ status: String, _ reason: String? = nil) -> String {
    let reason = reason.map { #","reason":"\#($0)""# } ?? ""
    return
        #"{"id":"\#(id)","kind":"substrate.reamde.dev/llm/thread","version":3,"createdAt":"2026-10-31T10:00:00Z","updatedAt":"2026-10-31T10:00:05Z","properties":{"mode":"chat","status":"\#(status)","subject":"\#(subject)"\#(reason),"agent":{"ref":"substrate.reamde.dev/core/agent/x.example.com/pkg/helper"},"finishedAt":"2026-10-31T10:00:05Z"}}"#
}

private func page(_ threads: String...) -> String {
    #"{"head":10,"generation":"g","records":[\#(threads.joined(separator: ","))]}"#
}

/// The routes of one chat, t1, whose last turn ended as `status` says: the
/// list's row and the thread's own read agree.
private func routes(_ status: String, _ reason: String? = nil) -> [ChatServer.Route] {
    let thread = ended("t1", "Reading list", status, reason)
    return [.init(method: "GET", path: "/llm/thread/t1", answer: .json(200, thread))] + baseRoutes(threads: page(thread), status: status)
}

@Suite("A chat whose last turn didn't finish", .timeLimit(.minutes(10)))
struct UnfinishedTurns {
    @Test func itSaysWhyInWordsAndNothingForOneThatFinished() {
        #expect(
            unfinishedTurn(status: "stopped", reason: "stopped: the server shut down during the turn", name: "Moss")
                == UnfinishedTurn(tone: .warn, title: "Moss didn’t finish", detail: "The server restarted while it was working."))
        #expect(
            unfinishedTurn(status: "stopped", reason: "stopped by api", name: "Moss")
                == UnfinishedTurn(tone: .quiet, title: "Moss was stopped before it finished"))
        #expect(unfinishedTurn(status: "overbudget", reason: "max turns reached (8)", name: "Moss")?.detail == "It used all 8 of its turns.")
        #expect(unfinishedTurn(status: "overbudget", reason: "max tool calls reached (32)", name: "Moss")?.detail == "It used all 32 of its tool calls.")
        #expect(unfinishedTurn(status: "overbudget", reason: "deadline exceeded after a last turn", name: "Moss")?.detail == "It ran out of time.")
        // A reason the app does not know is shown as the server wrote it.
        #expect(unfinishedTurn(status: "overbudget", reason: "a new cap (3)", name: "Moss")?.detail == "It reached a limit: a new cap (3)")
        #expect(unfinishedTurn(status: "overbudget", reason: nil, name: "Moss")?.detail == "It reached a limit.")
        #expect(
            unfinishedTurn(status: "error", reason: "llm: overloaded\nstack…", name: "Moss")
                == UnfinishedTurn(tone: .bad, title: "Moss ran into a problem and didn’t finish", detail: "What failed: llm: overloaded"))
        #expect(unfinishedTurn(status: "error", reason: nil, name: "Moss") == UnfinishedTurn(tone: .bad, title: "Moss ran into a problem and didn’t finish"))
        for status in ["ok", "running", ""] {
            #expect(unfinishedTurn(status: status, reason: nil, name: "Moss") == nil)
            #expect(unfinishedTone(status: status, reason: nil) == nil)
        }
    }

    @Test func theListMarksItByHowItEnded() async throws {
        let threads = page(
            ended("t1", "Cut by a restart", "stopped", "stopped: the server shut down during the turn"),
            ended("t2", "Stopped by me", "stopped", "stopped by api"), ended("t3", "Out of turns", "overbudget", "max turns reached (8)"),
            ended("t4", "Broke", "error", "llm: overloaded"), ended("t5", "Fine", "ok"))
        let rig = ChatRig(baseRoutes(threads: threads))
        try await rig.signIn()
        let rows = await rig.harness.presentation.list.sections.flatMap(\.rows)
        let marks = Dictionary(uniqueKeysWithValues: rows.map { ($0.title, $0.unfinished) })
        #expect(marks["Cut by a restart"] == ThreadRow.Unfinished(tone: .warn, words: "Didn’t finish"))
        #expect(marks["Stopped by me"] == ThreadRow.Unfinished(tone: .quiet, words: "Stopped"))
        #expect(marks["Out of turns"] == ThreadRow.Unfinished(tone: .warn, words: "Didn’t finish"))
        #expect(marks["Broke"] == ThreadRow.Unfinished(tone: .bad, words: "Failed"))
        #expect(marks["Fine"] == .some(nil))
    }

    @Test func itSaysWhyAndContinuesLeavingTheDraftAlone() async throws {
        let rig = ChatRig(
            routes("stopped", "stopped: the server shut down during the turn")
                + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 2))])
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        #expect(
            await rig.conversation(key)?.unfinished
                == UnfinishedNotice(
                    tone: .warn, title: "Helper didn’t finish", detail: "The server restarted while it was working.", action: "Continue",
                    blocked: nil, hint: "Sends “Continue where you left off.”"))

        try await rig.send(.draft("half a thought"), to: key)
        try await rig.send(.continueTurn, to: key)
        try await rig.harness.until { _ in rig.server.count("/chat") == 1 }
        let body = try #require(JSONValue.parse(rig.server.body("POST", "/chat")))
        #expect(body["thread"]?.string == "t1")
        #expect(body["message"]?.string == "Continue where you left off.")
        // The owner's own message, and the half-typed one stays.
        #expect(await rig.turns(key).dropFirst(2).first == "Continue where you left off.")
        #expect(await rig.conversation(key)?.composer.draft == "half a thought")
        // The turn it started is running: the notice is gone.
        #expect(await rig.conversation(key)?.unfinished == nil)

        rig.server.route("GET", "/llm/thread/t1", .json(200, threadJSON(version: 4)))
        rig.server.release()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(await rig.conversation(key)?.unfinished == nil)
        #expect(await rig.live(key)?.draft == "half a thought")
    }

    @Test func itSaysNothingOfATurnThatFinished() async throws {
        let rig = ChatRig(routes("ok"))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        #expect(await rig.conversation(key)?.unfinished == nil)
    }

    @Test func continueWaitsOnWhatHoldsTheComposer() async throws {
        let rig = ChatRig(routes("stopped", "stopped by api"))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        await rig.runtime.linkChanged(LinkSnapshot(phase: .signedIn, server: origin, repository: repository, online: false))
        try await rig.harness.until { _ in await rig.conversation(key)?.unfinished?.blocked == "Offline" }
        #expect(await rig.conversation(key)?.unfinished?.title == "Helper was stopped before it finished")
        try await rig.send(.continueTurn, to: key)
        await rig.harness.settle()
        #expect(rig.server.count("/chat") == 0)
    }

    @Test func aFailureIsSaidOnceUnlessTheCardSaysWhatToDo() async throws {
        let rig = ChatRig(routes("error", "llm: overloaded"))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        let screen = try #require(await rig.conversation(key))
        #expect(screen.unfinished?.title == "Helper ran into a problem and didn’t finish")
        #expect(screen.unfinished?.detail == "What failed: llm: overloaded")
        #expect(screen.status.isEmpty)

        // A refused key keeps its card, which says what to do about it.
        let refused = ChatRig(routes("error", "401 Unauthorized"))
        try await refused.signIn()
        await refused.harness.send(.show(thread: "t1", scene: "s"))
        let other = try #require(await refused.harness.state.scenes["s"]?.current)
        try await refused.harness.until { _ in await refused.turns(other) == ["Hi", "Hello"] }
        let both = try #require(await refused.conversation(other))
        #expect(both.status.map(\.title) == ["The provider refused the API key"])
        #expect(both.unfinished?.detail == "What failed: 401 Unauthorized")
    }
}
