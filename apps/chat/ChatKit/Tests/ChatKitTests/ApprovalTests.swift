import Foundation
import SubstrateCore
import Testing

@testable import ChatKit

private let approvalID = "01h2xcejqtf2nbrexx3vqjhp42"
private let secretary = "substrate.reamde.dev/assistant/secretary"

private func approvalJSON(state: String = "pending", version: Int = 1, requestedIn: Bool = false) -> String {
    let sub = requestedIn ? #","requestedIn":{"ref":"substrate.reamde.dev/llm/thread/t9"}"# : ""
    return
        #"{"id":"\#(approvalID)","kind":"substrate.reamde.dev/llm/approval","version":\#(version),"createdAt":"2026-11-09T10:00:00Z","updatedAt":"2026-11-09T10:00:00Z","properties":{"state":"\#(state)","thread":{"ref":"substrate.reamde.dev/llm/thread/t1"}\#(sub),"agent":{"ref":"substrate.reamde.dev/core/agent/\#(secretary)"},"function":{"ref":"substrate.reamde.dev/core/function/providers.substrate.reamde.dev/google/sendmail"},"tool":"gmail_send","arguments":{"to":["ada@example.com"],"subject":"Lunch","body":"Friday works.","cc":null}}}"#
}

private func page(_ records: String...) -> String {
    #"{"head":10,"generation":"g","records":[\#(records.joined(separator: ","))]}"#
}

private let threads = page(threadJSON(version: 3))

private func asked() -> ToolCall {
    var call = ToolCall(
        id: "c1", name: "gmail_send", callable: "substrate.reamde.dev/core/function/providers.substrate.reamde.dev/google/sendmail",
        arguments: #"{"to":["ada@example.com"],"subject":"Lunch","body":"Friday works."}"#,
        output: #"{"output":{"approval":"\#(approvalID)","status":"waiting for the owner's approval"}}"#, ok: true)
    call.changes = [ChangeStamp(seq: 4, op: "put", kind: Kinds.approval, id: approvalID)]
    return call
}

private func decided(_ decision: String, ok: Bool? = nil, error: String? = nil) -> Turn {
    var env = #"{"event":"approvalDecided","approval":"substrate.reamde.dev/llm/approval/\#(approvalID)","decision":"\#(decision)","tool":"gmail_send""#
    if let ok { env += #","ok":\#(ok)"# }
    if let error { env += #","error":"\#(error)""# }
    return Turn(id: "s1", role: .system, content: env + "}")
}

@Suite("A call that waits for the owner's approval", .timeLimit(.minutes(10)))
struct ApprovalRows {
    @Test func theRowAsksAndTheCardIsUnderIt() {
        let row = TurnPresenter.tool(asked(), id: "a#0", agent: nil, decided: [], titles: [:])
        #expect(row.status == .held)
        #expect(row.statusLabel == "Asked for your approval")
        #expect(row.attachment == .approval(approvalID))
        // The handle is not a result, and the approval is not a change the
        // call made.
        #expect(!row.details.contains { if case .output = $0 { true } else { false } })
        #expect(row.chips.isEmpty)
    }

    @Test func theOutcomeSaysWhatTheOwnerDecidedAndHowItEnded() {
        let body = { (turn: Turn) in TurnPresenter.present(turn, agent: nil, live: false, decided: [], titles: [:]).body }
        #expect(body(decided("approved", ok: true)) == .approvalEnded(OutcomeLine(text: "You approved gmail_send, and it ran", done: true)))
        #expect(
            body(decided("approved", ok: false, error: "quota"))
                == .approvalEnded(OutcomeLine(text: "You approved gmail_send, but it didn't work: quota", done: false)))
        #expect(body(decided("denied")) == .approvalEnded(OutcomeLine(text: "You declined gmail_send", done: false)))
    }

    @Test func thePendingCardListsExactlyWhatWouldRun() throws {
        let record = try JSONDecoder().decode(Record.self, from: Data(approvalJSON().utf8))
        let card = CardPresenter.approval(approvalID, card: nil, pending: record, outcome: nil)
        #expect(card.pending)
        #expect(card.tone == .waiting)
        #expect(card.title == "Waiting for your approval")
        #expect(card.asks == "Secretary wants to run")
        #expect(card.tool == "gmail_send")
        // An empty argument is left out; the rest in the order the server wrote.
        #expect(card.arguments.map(\.label) == ["Body", "Subject", "To"])
        #expect(card.arguments.map(\.value) == ["Friday works.", "Lunch", "ada@example.com"])
    }

    @Test func aDecidedCardOffersNothingAndSaysWhereItEnded() throws {
        let failed = try JSONDecoder().decode(Record.self, from: Data(approvalJSON(state: "failed", version: 4).utf8))
        var held = ChatAppState.Card()
        held.record = failed
        let card = CardPresenter.approval(approvalID, card: held, pending: nil, outcome: nil)
        #expect(!card.pending)
        #expect(card.tone == .failed)
        // A card whose record cannot be read says what the transcript does.
        let told = CardPresenter.approval(
            approvalID, card: nil, pending: nil, outcome: approvalNotice(decided("denied")))
        #expect(told.tone == .declined)
        #expect(!told.pending && !told.loading)
    }
}

@Suite("Approvals on the server", .timeLimit(.minutes(10)))
struct ApprovalReads {
    @Test func theyAreListedCountedAndWatched() async throws {
        let rig = ChatRig(
            [.init(method: "GET", path: "/api/v1/records", query: "llm/approval", answer: .json(200, page(approvalJSON())))]
                + baseRoutes(threads: threads))
        try await rig.signIn()
        #expect(await rig.harness.state.approvals.map(\.id) == [approvalID])
        #expect(await rig.harness.state.attention("t1") == 1)
        try await rig.harness.until { _ in rig.server.count("/api/v1/changes", query: Kinds.approval) > 0 }
    }

    /// The refusal a server older than the kind answers with: the chats load,
    /// and the watch never names the kind, which it would refuse whole.
    @Test func aServerOlderThanTheKindLoadsAndIsNeverWatched() async throws {
        let older = ChatServer.Route(
            method: "GET", path: "/api/v1/records", query: "llm/approval",
            answer: .json(
                422,
                #"{"error":{"code":"validation","message":"substrate: validation failed: unknown type \"substrate.reamde.dev/llm/approval\""}}"#))
        let rig = ChatRig([older] + baseRoutes(threads: threads))
        try await rig.signIn()
        #expect(await rig.harness.state.refreshError == nil)
        #expect(await rig.harness.state.approvals.isEmpty)
        try await rig.harness.until { _ in rig.server.count("/api/v1/changes") > 0 }
        #expect(rig.server.count("/api/v1/changes", query: Kinds.approval) == 0)
    }

    /// An app's token lists the cards only under the threads it names, so
    /// with none it asks nothing: that is no answer about the kind, and the
    /// watch must not name it to an older server.
    @Test func aScopedSessionWithNoChatsNeverWatchesTheKind() async throws {
        let older = ChatServer.Route(
            method: "GET", path: "/api/v1/records", query: "llm/approval",
            answer: .json(
                422,
                #"{"error":{"code":"validation","message":"substrate: validation failed: unknown type \"substrate.reamde.dev/llm/approval\""}}"#))
        let rig = ChatRig([older] + baseRoutes(), scopes: ["agents:*"])
        try await rig.signIn()
        try await rig.harness.until { _ in rig.server.count("/api/v1/changes") > 0 }
        #expect(rig.server.count("/api/v1/records", query: "llm/approval") == 0)
        #expect(rig.server.count("/api/v1/changes", query: Kinds.approval) == 0)
        #expect(await rig.harness.state.approvalsKnown == false)
    }

    @Test func approvingSendsTheStateAloneOnTheVersionRead() async throws {
        let rig = ChatRig(
            [
                .init(method: "GET", path: "/api/v1/records", query: "llm/approval", answer: .json(200, page(approvalJSON(version: 2)))),
                .init(method: "PATCH", path: "/llm/approval/\(approvalID)", answer: .json(200, approvalJSON(state: "approved", version: 3))),
                .init(method: "GET", path: "/llm/approval/\(approvalID)", answer: .json(200, approvalJSON(state: "running", version: 4))),
            ] + baseRoutes(threads: threads))
        try await rig.signIn()
        await rig.harness.send(.decideApproval(approvalID, approve: true))
        try await rig.harness.until { _ in rig.server.count("PATCH") > 0 }
        let body = try #require(rig.server.body("PATCH", "/llm/approval/\(approvalID)"))
        let sent = try JSONDecoder().decode(JSONValue.self, from: Data(body.utf8))
        #expect(sent["properties"] == ["state": "approved"])
        #expect(sent["ifVersion"] == 2)
        // The card follows the call the engine now runs.
        try await rig.harness.until { harness in await harness.state.cards[approvalID]?.record?["state"]?.string == "running" }
    }

    @Test func aSubagentsAskWaitsAfterTheTranscript() async throws {
        let rig = ChatRig(
            [
                .init(
                    method: "GET", path: "/api/v1/records", query: "llm/approval",
                    answer: .json(200, page(approvalJSON(requestedIn: true))))
            ] + baseRoutes(threads: threads))
        try await rig.signIn()
        await rig.harness.send(.show(thread: "t1", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { _ in await rig.conversation(key)?.approvals == [approvalID] }
        let card = try #require(await rig.conversation(key)?.cards[approvalID]?.approval)
        #expect(card.pending)
    }
}
