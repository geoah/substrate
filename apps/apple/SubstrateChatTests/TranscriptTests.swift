import Foundation
import Testing
import SubstrateKit
@testable import SubstrateChat

private func message(_ id: String, _ props: [String: JSONValue]) -> Record {
    Record(id: id, kind: Kinds.message, properties: props, createdAt: "2026-10-31T10:00:00Z")
}

@Suite struct TranscriptFold {
    @Test func toolRowsFoldIntoTheirCallsByID() {
        let rows = [
            message("u", ["role": "user", "content": "find it", "turn": 1]),
            message("a", ["role": "assistant", "content": "", "turn": 2, "toolCalls": [
                ["id": "c1", "name": "query", "arguments": "{\"q\":\"a\"}"],
                ["id": "c2", "name": "query", "arguments": "{\"q\":\"b\"}"],
            ]]),
            // Settled out of order: pairing by name would settle the wrong card.
            message("t2", ["role": "tool", "toolCallId": "c2", "name": "query", "content": "{\"records\":[]}", "turn": 3]),
            message("t1", ["role": "tool", "toolCallId": "c1", "name": "query", "content": "{\"error\":\"nope\"}", "turn": 4]),
        ]
        let turns = transcript(of: rows)
        #expect(turns.count == 2)
        #expect(turns[1].tools[0].ok == false)
        #expect(turns[1].tools[0].output == "{\"error\":\"nope\"}")
        #expect(turns[1].tools[1].ok == true)
    }

    @Test func anOrphanToolRowGetsItsOwnTurn() {
        let rows = [
            message("a", ["role": "assistant", "content": "hi", "turn": 1]),
            message("t", ["role": "tool", "toolCallId": "gone", "name": "write", "content": "{}", "turn": 2]),
        ]
        let turns = transcript(of: rows)
        #expect(turns.count == 2)
        #expect(turns[0].tools.isEmpty)
        #expect(turns[1].tools.first?.id == "gone")
    }

    @Test func aDeclaredOKWinsOverThePayload() {
        let row = message("t", ["role": "tool", "content": "{\"error\":\"x\"}", "ok": true])
        #expect(toolOK(row))
        let shaped = message("t", ["role": "tool", "content": "{\"error\":\"x\",\"records\":[]}"])
        #expect(toolOK(shaped))
    }

    @Test func summaryAndSystemTurnsStandAlone() {
        let rows = [
            message("s", ["role": "summary", "content": "folded", "covers": ["from": "a", "through": "b"], "tokensBefore": 9000]),
            message("d", ["role": "system", "content": "{\"event\":\"proposalDecision\",\"decision\":\"accepted\",\"request\":\"substrate.reamde.dev/core/recordpatchrequest/abcdefghijkl\",\"op\":\"patch\"}"]),
        ]
        let turns = transcript(of: rows)
        #expect(turns[0].role == .summary)
        #expect(turns[0].compaction?.from == "a")
        #expect(turns[0].compaction?.tokensBefore == 9000)
        let notice = decisionNotice(turns[1])
        #expect(notice?.requestID == "abcdefghijkl")
        #expect(notice?.accepted == true)
    }

    @Test func aStampedRequestIsFoundWhateverTheAlias() {
        var call = ToolCall(id: "c", name: "file", callable: nil, arguments: "", output: "{\"id\":\"abcdefghijkl\"}", ok: true)
        #expect(requestID(of: call) == nil)
        call.changes = [ChangeStamp(seq: 4, op: "put", kind: Kinds.request, id: "mnopqrstuvwx")]
        #expect(requestID(of: call) == "mnopqrstuvwx")
        let live = ToolCall(id: "c", name: "propose", callable: "function:substrate.reamde.dev:core:propose",
                            arguments: "", output: "{\"id\":\"abcdefghijkl\"}", ok: true)
        #expect(requestID(of: live) == "abcdefghijkl")
        let impostor = ToolCall(id: "c", name: "propose", callable: "function:x.example.com:pkg:propose",
                                arguments: "", output: "{\"id\":\"abcdefghijkl\"}", ok: true)
        #expect(requestID(of: impostor) == nil)
    }

    @Test func aTriggeredOpeningReadsAsWhatFired() {
        let opening = "{\"change\":{\"op\":\"put\",\"kind\":\"x.example.com/tasks/task\",\"id\":\"t1\"},\"record\":{\"kind\":\"x.example.com/tasks/task\",\"id\":\"t1\",\"properties\":{\"name\":\"Call mum\"}}}"
        #expect(threadTitle(subject: nil, opening: opening) == "When Call mum changed")
        #expect(threadTitle(subject: "  Trip  ", opening: opening) == "Trip")
        #expect(threadTitle(subject: nil, opening: "  \n") == "New chat")
        #expect(threadTitle(subject: nil, opening: "first line\nsecond") == "first line")
    }
}

@Suite struct LiveOverlayFold {
    @Test func deltasAppendAndAToolRoundClosesTheTurn() {
        var live = LiveOverlay()
        live.pushDelta("Hel", seq: 1)
        live.pushDelta("lo", seq: 2)
        live.pushToolStart(ToolCall(id: "c1", name: "query", arguments: ""), seq: 3)
        live.settleTool(ToolCall(id: "c1", name: "query", arguments: ""), output: "{}", ok: true, seq: 4)
        live.pushDelta("After", seq: 5)
        #expect(live.turns.count == 2)
        #expect(live.turns[0].content == "Hello")
        #expect(live.turns[0].tools[0].ok == true)
        #expect(live.turns[1].content == "After")
    }

    @Test func anUnclaimedFinishOpensASettledCard() {
        var live = LiveOverlay()
        live.settleTool(ToolCall(id: "x", name: "query", arguments: ""), output: "{\"error\":\"budget\"}", ok: false, seq: 1)
        #expect(live.turns.count == 1)
        #expect(live.turns[0].tools[0].ok == false)
    }

    @Test func reasoningCountsItsSpanAndTextEndsThinking() {
        var live = LiveOverlay()
        let t0 = Date(timeIntervalSince1970: 100)
        live.pushReasoning("Let me ", seq: 1, now: t0)
        live.pushReasoning("think", seq: 2, now: t0.addingTimeInterval(2.5))
        #expect(live.turns[0].thinking)
        #expect(live.turns[0].reasoningMillis == 2500)
        live.pushDelta("Answer", seq: 3)
        #expect(!live.turns[0].thinking)
        #expect(live.turns[0].reasoning == "Let me think")
        #expect(live.turns[0].content == "Answer")
    }

    @Test func truncationMarksTheOverlayIncomplete() {
        var live = LiveOverlay()
        live.apply(AgentEvent(kind: .truncated), seq: 1)
        #expect(live.incomplete)
    }

    @Test func replayIsDroppedBySetMembershipNotACutoff() {
        // A resolution written while the reply streamed took a later ordinal.
        let held: Set<Int> = [1, 2, 5]
        var delta = AgentEvent(kind: .delta)
        delta.turn = 3
        #expect(!isReplayed(delta, held: held))
        delta.turn = 2
        #expect(isReplayed(delta, held: held))
        #expect(!isReplayed(AgentEvent(kind: .done), held: held))
    }
}

@Suite struct StreamLines {
    @Test func heartbeatsGarbageAndUnknownKindsReadAsNothing() {
        #expect(AgentEvent.parse("{}")?.kind == .heartbeat)
        #expect(AgentEvent.parse("{\"kind\":\"_heartbeat\"}") == nil)
        #expect(AgentEvent.parse("") == nil)
        #expect(AgentEvent.parse("not json") == nil)
        #expect(AgentEvent.parse("{\"kind\":\"future\"}") == nil)
    }

    @Test func fieldsReadTolerantly() {
        let e = AgentEvent.parse("{\"kind\":\"toolFinished\",\"id\":\"c\",\"ok\":false,\"turn\":\"7\",\"output\":\"{}\"}")
        #expect(e?.kind == .toolFinished)
        #expect(e?.ok == false)
        #expect(e?.turn == nil)
    }

    @Test func aDoneWithTextAndNoResultIsAFailure() {
        #expect(AgentEvent.parse("{\"kind\":\"done\",\"text\":\"boom\"}")?.failure == "boom")
        #expect(AgentEvent.parse("{\"kind\":\"done\",\"result\":{\"reply\":\"ok\",\"status\":\"ok\"}}")?.failure == nil)
        #expect(AgentEvent.parse("{\"kind\":\"error\",\"error\":\"lost\"}")?.failure == "lost")
    }

    @Test func watchLines() {
        #expect(WatchLine.parse("{\"bookmark\":12,\"generation\":\"g1\"}") == .bookmark(seq: 12, generation: "g1"))
        #expect(WatchLine.parse("{}") == nil)
        #expect(WatchLine.parse("{\"seq\":13,\"ts\":\"t\",\"kind\":\"k\",\"recordId\":\"r\",\"op\":\"put\"}")
            == .row(ChangeRow(seq: 13, kind: "k", recordID: "r", op: "put")))
        #expect(WatchLine.parse("{\"error\":{\"code\":\"compacted\",\"message\":\"gone\"}}") == .failed(code: "compacted", message: "gone"))
    }
}

@Suite struct Words {
    @Test func agentNames() {
        #expect(agentName("x.example.com/llmsample/researchAssistant") == "Research assistant")
        #expect(agentName("agent:x.example.com:pkg:task_triage") == "Task triage")
    }

    @Test func toolSummaries() {
        let query = ResolvedTool(function: HostFunction.query)
        var call = ToolCall(id: "c", name: "query", arguments: "{\"q\":\"dentist\"}", output: "{\"records\":[{},{}]}", ok: true)
        #expect(toolSummary(call, query) == "Searched for “dentist”, found 2")
        call.arguments = "{\"filter\":{\"kinds\":[\"x.example.com/tasks/task\",\"x.example.com/people/person\"]}}"
        #expect(toolSummary(call, query) == "Looked through your tasks and people, found 2")
        let write = ToolCall(id: "c", name: "write", arguments: "{\"op\":\"create\",\"kind\":\"x.example.com/tasks/task\"}")
        #expect(toolSummary(write, ResolvedTool(function: HostFunction.write)) == "Saved a task")
        #expect(toolSummary(write, ResolvedTool(subagent: "x.example.com/pkg/researcher")) == "Asked Researcher")
    }

    @Test func aStampedCallableResolvesWhatRan() {
        let r = resolveTool(agent: nil, name: "lookup", callable: "agent:x.example.com:pkg:helper")
        #expect(r.subagent == "x.example.com/pkg/helper")
        let host = resolveTool(agent: nil, name: "query", callable: nil)
        #expect(host.function == HostFunction.query)
    }

    @Test func dayGroups() {
        let cal = Calendar(identifier: .gregorian)
        let now = cal.date(from: DateComponents(year: 2026, month: 10, day: 31, hour: 12))!
        #expect(DayGroup.of(now.addingTimeInterval(-3600), now: now, calendar: cal) == .today)
        #expect(DayGroup.of(now.addingTimeInterval(-86400), now: now, calendar: cal) == .yesterday)
        #expect(DayGroup.of(now.addingTimeInterval(-4 * 86400), now: now, calendar: cal) == .week)
        #expect(DayGroup.of(now.addingTimeInterval(-40 * 86400), now: now, calendar: cal) == .earlier)
    }

    @Test func timestampsWithNanoseconds() {
        #expect(Timestamp.parse("2026-10-31T14:12:03.123456789Z") != nil)
        #expect(Timestamp.parse("2026-10-31T14:12:03Z") != nil)
        #expect(Timestamp.parse("2026-10-31T14:12:03.5+02:00") != nil)
    }

    @Test func pathSegmentsEncodeSlashesOnce() {
        #expect(segment("x.example.com/pkg/agent") == "x.example.com%2Fpkg%2Fagent")
        let api = API(server: URL(string: "https://s.example.com")!)
        let url = try? api.client.url("api/v1/records", query: [("filter", "{\"kinds\":[\"a/b\"]}"), ("orderBy", "turn:desc,createdAt:desc")])
        #expect(url?.absoluteString == "https://s.example.com/api/v1/records?filter=%7B%22kinds%22%3A%5B%22a%2Fb%22%5D%7D&orderBy=turn%3Adesc%2CcreatedAt%3Adesc")
    }
}

@Suite struct ReplyMentions {
    @Test func aReplyRowCarriesWhatItLinks() {
        let kind = "ada.example.com/tasks/task"
        let turns = transcript(of: [
            message("a", ["role": "assistant", "content": "see it", "turn": 1, "mentions": [
                ["kind": .string(kind), "id": "q4", "text": "Q4", "unread": true],
                ["kind": .string(kind), "id": "ghost", "text": "Ghost", "missing": true],
                ["kind": .string(kind)],
            ]]),
        ])
        #expect(turns[0].mentions == [
            Mention(kind: kind, id: "q4", text: "Q4", unread: true),
            Mention(kind: kind, id: "ghost", text: "Ghost", missing: true),
        ])
    }

    @Test func aReplyWithoutLinksHasNone() {
        let turns = transcript(of: [message("a", ["role": "assistant", "content": "hi", "turn": 1])])
        #expect(turns[0].mentions == nil)
    }
}
