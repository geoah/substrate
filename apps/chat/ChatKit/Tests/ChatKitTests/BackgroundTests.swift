import Foundation
@testable import ChatKit
import SubstrateDevice
import SubstrateRuntime
import SubstrateTesting
import Testing

/// The refresh job, as the system runs it: scheduled with the repository
/// and the link generation, checked against both before it writes, and
/// stopped when the system takes the run back or its budget runs out.
@Suite("Background work")
struct BackgroundRefresh {
    let threads = #"{"head":10,"generation":"g","records":[\#(threadJSON(version: 3))]}"#

    /// The refresh job the app last scheduled.
    func job(_ rig: ChatRig) throws -> BackgroundJob {
        try #require(rig.background.scheduled.last { $0.name == ChatApp.refreshJob })
    }

    @Test func aJobScheduledBeforeASignOutDoesNothingAfterTheNextSignIn() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        await rig.harness.send(.phase(.background))
        try await rig.harness.until { _ in rig.background.scheduled.count == 1 }
        let before = try job(rig)
        #expect(before.payload == ["repository": identity, "generation": "0"])
        await rig.harness.send(.phase(.active))
        await rig.signOut()
        try await rig.harness.until { _ in (try? rig.generation) == 1 }

        // Signed in again, with a list the job would land.
        rig.server.route("GET", "/api/v1/records", query: "llm/thread", .json(200, threads))
        try await rig.signIn()
        try await rig.harness.until { await $0.state.openings["t1"] != nil }
        await rig.harness.settle()
        let read = rig.server.count("/api/v1/records", query: "llm/thread")
        rig.background.grant(BackgroundRun(id: "old", job: ChatApp.refreshJob, payload: before.payload))
        try await rig.harness.until { _ in rig.background.finished["old"] != nil }
        await rig.harness.settle()
        #expect(rig.server.count("/api/v1/records", query: "llm/thread") == read)
        // Nothing was read, so nothing could be saved or notified.
        #expect(!rig.harness.effects.contains { if case .backgroundRefresh = $0.ask { true } else { false } })
        // It scheduled the job again, for this sign-in.
        #expect(try job(rig).payload == ["repository": identity, "generation": "1"])

        // A job this sign-in scheduled runs.
        rig.background.grant(job: ChatApp.refreshJob, id: "new")
        try await rig.harness.until { _ in rig.background.finished["new"] == true }
        #expect(rig.server.count("/api/v1/records", query: "llm/thread") > read)
    }

    @Test func aRunTheSystemTakesBackStopsAndLandsNothing() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        await rig.harness.send(.phase(.background))
        try await rig.harness.until { _ in rig.background.scheduled.count == 1 }
        rig.server.route("GET", "/api/v1/records", query: "llm/thread", .json(200, threads))
        rig.server.hold("/api/v1/records")
        let read = rig.server.count("/api/v1/records", query: "llm/thread")
        rig.background.grant(job: ChatApp.refreshJob, id: "r")
        try await rig.harness.until { _ in rig.server.count("/api/v1/records", query: "llm/thread") > read }
        rig.background.expire("r")
        try await rig.harness.until { _ in rig.background.finished["r"] == false }
        rig.server.letGo("/api/v1/records")
        await rig.harness.settle()
        let state = await rig.harness.state
        #expect(state.chats.isEmpty)
        #expect(state.backgroundRun == nil)
        #expect(!rig.notifications.calls.contains { if case .schedule = $0 { true } else { false } })
    }

    @Test func aRunPastItsBudgetIsAnsweredAndLandsNothing() async throws {
        let rig = ChatRig()
        try await rig.signIn()
        await rig.harness.send(.phase(.background))
        try await rig.harness.until { _ in rig.background.scheduled.count == 1 }
        rig.server.route("GET", "/api/v1/records", query: "llm/thread", .json(200, threads))
        rig.server.hold("/api/v1/records")
        let read = rig.server.count("/api/v1/records", query: "llm/thread")
        rig.background.grant(job: ChatApp.refreshJob, id: "r", budget: 30)
        try await rig.harness.until { _ in rig.server.count("/api/v1/records", query: "llm/thread") > read }
        // Answered short of the budget, before the system takes it back.
        await rig.harness.advance(by: 24)
        try await rig.harness.until { _ in rig.background.finished["r"] == false }
        rig.server.letGo("/api/v1/records")
        await rig.harness.settle()
        #expect(await rig.harness.state.chats.isEmpty)
        #expect(rig.background.expired.isEmpty)
    }
}
