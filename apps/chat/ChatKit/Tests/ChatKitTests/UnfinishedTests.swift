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
        func card(_ status: String, _ reason: String?) -> FailurePresentation? {
            let thread = ChatThread(
                record: Record(id: "t1", kind: Kinds.thread, properties: ["status": .string(status), "reason": reason.map(JSONValue.string) ?? .null]))
            var context = FailurePresenter.Context()
            context.agent = "Moss"
            return thread.runFailure.map { FailurePresenter.present($0, context) }
        }
        let restart = card("stopped", "stopped: the server shut down during the turn")
        #expect(restart?.sentence == "The server restarted while Moss was working." && restart?.tone == .warn)
        #expect(restart?.button?.title == "Try Again")
        let stopped = card("stopped", "stopped by api")
        #expect(stopped?.sentence == "Moss was stopped before it finished." && stopped?.tone == .quiet && stopped?.button?.title == "Continue")
        #expect(card("overbudget", "max turns reached (8)")?.note == "It used all 8 of its turns.")
        #expect(card("overbudget", "max tool calls reached (32)")?.note == "It used all 32 of its tool calls.")
        #expect(card("overbudget", "deadline exceeded after a last turn")?.note == "It ran out of time.")
        // A reason the app does not know is the server's, under Details.
        let unknown = card("overbudget", "a new cap (3)")
        #expect(unknown?.sentence == "Moss ran out of room before finishing." && unknown?.note == nil)
        #expect(unknown?.details == FailureDetails(title: "Details", label: "The server said", text: "a new cap (3)"))
        #expect(card("overbudget", nil)?.button?.title == "Continue")
        let failed = card("error", "llm: overloaded\nstack…")
        #expect(failed?.sentence == "Moss couldn't finish this reply." && failed?.tone == .bad && failed?.button?.title == "Try Again")
        #expect(card("error", nil)?.details == nil)
        for status in ["ok", "running", ""] {
            #expect(card(status, nil) == nil)
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
                + [.init(method: "POST", path: "/continue", answer: .stream(replyStream, holdAfter: 2))])
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        let card = try #require(await rig.card(key))
        #expect(card.sentence == "The server restarted while Helper was working.")
        #expect(card.button == FailureButton(title: "Try Again", action: .resume))

        let revision = await rig.conversation(key)?.composer.draftRevision
        try await rig.send(.draft("half a thought"), to: key)
        try await rig.send(.continueTurn, to: key)
        try await rig.harness.until { _ in rig.server.count("/continue") == 1 }
        // No message of the person's is written, and the half-typed one stays.
        #expect(rig.server.count("/chat") == 0)
        #expect(await rig.turns(key).contains("Continue where you left off.") == false)
        #expect(await rig.live(key)?.draft == "half a thought")
        #expect(await rig.conversation(key)?.composer.draftRevision == revision)
        // The turn it started is running: the card is gone.
        try await rig.harness.until { _ in await rig.card(key) == nil }

        rig.server.route("GET", "/llm/thread/t1", .json(200, threadJSON(version: 4)))
        rig.server.release()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        #expect(await rig.card(key) == nil)
        #expect(await rig.live(key)?.draft == "half a thought")
    }

    @Test func itSaysNothingOfATurnThatFinished() async throws {
        let rig = ChatRig(routes("ok"))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        #expect(await rig.conversation(key)?.failures == ScreenFailures())
    }

    @Test func continueWaitsOnWhatHoldsTheComposer() async throws {
        let rig = ChatRig(routes("stopped", "stopped by api"))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        await rig.runtime.linkChanged(LinkSnapshot(phase: .signedIn, server: origin, repository: repository, online: false))
        try await rig.harness.until { _ in await rig.card(key)?.button?.blocked == "Offline" }
        #expect(await rig.card(key)?.sentence == "Helper was stopped before it finished.")
        #expect(await rig.card(key)?.button?.enabled(at: Date()) == false)
        try await rig.send(.continueTurn, to: key)
        await rig.harness.settle()
        #expect(rig.server.count("/chat") == 0 && rig.server.count("/continue") == 0)
    }

    @Test func aFailureIsSaidOnceInOnePlace() async throws {
        let rig = ChatRig(routes("error", "llm: overloaded"))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.turns(key) == ["Hi", "Hello"] }
        let screen = try #require(await rig.conversation(key))
        #expect(screen.failures.turns.count == 1 && screen.failures.strip == nil)
        #expect(screen.failures.turns[screen.turns.last?.id ?? ""]?.sentence == "Helper couldn't finish this reply.")
        #expect(screen.status.isEmpty)
    }
}
