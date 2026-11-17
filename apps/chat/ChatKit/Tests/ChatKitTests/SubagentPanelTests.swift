import Foundation
import SubstrateCore
import Testing

@testable import ChatKit

private let architect = "substrate.reamde.dev/assistant/architect"
private let librarian = "substrate.reamde.dev/assistant/librarian"
private let architectCallable = "\(Kinds.agent)/\(architect)"
private let librarianCallable = "\(Kinds.agent)/\(librarian)"
private let places = "mneme.rxtx.dev/places/place"
private let brands = "mneme.rxtx.dev/commerce/brand"

private func at(_ text: String) -> Date { Timestamp.parse(text)! }

private func row(_ id: String, _ properties: [String: JSONValue], createdAt: String) -> Record {
    Record(id: id, kind: Kinds.message, properties: properties, createdAt: createdAt, updatedAt: createdAt)
}

/// The assistant row that asked for `calls`, each `(id, name, callable, arguments)`.
private func asking(_ calls: [(String, String, String, String)], turn: Int, createdAt: String) -> Record {
    row(
        "a\(turn)",
        [
            "role": "assistant", "content": "", "turn": .number(JSONNumber(turn)),
            "toolCalls": .array(
                calls.map { .object(["id": .string($0.0), "name": .string($0.1), "callable": .string($0.2), "arguments": .string($0.3)]) }),
        ], createdAt: createdAt)
}

private func answering(_ call: String, name: String, turn: Int, content: String, createdAt: String, extra: [String: JSONValue] = [:]) -> Record {
    var properties: [String: JSONValue] = [
        "role": "tool", "content": .string(content), "toolCallId": .string(call), "name": .string(name), "ok": true,
        "turn": .number(JSONNumber(turn)),
    ]
    properties.merge(extra) { _, new in new }
    return row("t\(turn)", properties, createdAt: createdAt)
}

private func panels(_ rows: [Record]) -> [SubagentPanel] {
    TurnPresenter.present(transcript(of: rows), agent: nil, liveKey: nil, titles: [:]).flatMap { turn -> [SubagentPanel] in
        guard case let .assistant(reply) = turn.body else { return [] }
        return reply.tools.compactMap(\.subagent)
    }
}

private let request =
    "The owner asked for this; please apply it. Create kinds to track trips. A trip (name, destination, start/end dates, summary, status planned/ongoing/done) with a timeline of what was planned and what was actually done."

private let reply = """
    ## Done

    I added **three kinds** in a new `trips` package: [trip](substrate://x/y), flight and hotel stay. Each links to its trip.

    - trip
    - flight
    """

@Suite("A call to another assistant is a panel of its own", .timeLimit(.minutes(10)))
struct SubagentPanelTests {
    @Test func aFinishedCallSaysWhatWasAskedWhatItAnsweredWhatItChangedAndHowLongItTook() throws {
        let args = JSONValue.object(["input": .string(request)]).jsonText
        let output = JSONValue.object(["reply": .string(reply), "thread": "child1", "status": "ok"]).jsonText
        let rows = [
            asking([("c1", "architect", architectCallable, args)], turn: 1, createdAt: "2026-11-17T10:00:00Z"),
            answering(
                "c1", name: "architect", turn: 2, content: output, createdAt: "2026-11-17T10:05:00Z",
                extra: [
                    "ran": ["startedAt": "2026-11-17T10:00:01.5Z", "finishedAt": "2026-11-17T10:02:11.5Z"],
                    "subagentWrites": ["thread": "child1", "records": 17, "kinds": .array([.string(places), .string(brands)])],
                ]),
        ]
        let panel = try #require(panels(rows).first)
        #expect(panel.name == "Architect" && panel.state == .finished)
        // The call's own times, not its row's, which waited on the stretch.
        #expect(panel.clock.took == 130 && panel.clock.since == nil)
        #expect(panel.status(at: .distantFuture) == "Finished · 2 min 10 s")
        // A preamble too short to say what was asked runs on into the next
        // sentence, cut at a word.
        #expect(
            panel.asked
                == "The owner asked for this; please apply it. Create kinds to track trips. A trip (name, destination, start/end dates, summary, status planned/ongoing/done) with a timeline of what…"
        )
        #expect(panel.request == request && panel.askedLabel == "Asked")
        // The first paragraph that is not a heading, in plain words.
        #expect(panel.outcomeLabel == "Answered")
        #expect(panel.outcome == "I added three kinds in a new trips package: trip, flight and hotel stay. Each links to its trip.")
        #expect(panel.reply == reply.trimmingCharacters(in: .whitespacesAndNewlines))
        #expect(panel.changed == "Wrote 17 records in Places and Brands")
        #expect(panel.kinds == [places, brands] && panel.moreKinds == nil)
        #expect(panel.thread == ThreadLink(thread: "child1", label: "Open its conversation"))
        #expect(panel.raw.map(\.label) == ["Tool", "Arguments", "Output"] && panel.rawLabel == "Raw call")
        #expect(panel.stopped == nil && panel.failedCall == nil)
    }

    @Test func aCallThatWroteNothingSaysSoAndOneFromAnOlderServerIsTimedByItsRows() throws {
        let rows = [
            asking([("c1", "architect", architectCallable, #"{"input":"Look.","thread":"child0"}"#)], turn: 1, createdAt: "2026-11-17T10:00:00Z"),
            answering(
                "c1", name: "architect", turn: 2, content: #"{"reply":"Nothing to do.","thread":"child0","status":"ok"}"#,
                createdAt: "2026-11-17T10:00:45Z", extra: ["subagentWrites": ["records": 0]]),
        ]
        let panel = try #require(panels(rows).first)
        #expect(panel.changed == "Changed nothing" && panel.kinds.isEmpty)
        #expect(panel.clock.took == 45 && panel.askedLabel == "Followed up")
        #expect(panel.outcome == "Nothing to do.")
    }

    @Test func aRunThatRanOutOfTimeSaysTheServersSentenceAndTheCallItFailedOn() throws {
        let stopped =
            "architect ran out of time: it had 5 min, its own time limit. Its last call, vocabulary, failed: timeout. Its daily spend was not what stopped it."
        let output = JSONValue.object([
            "reply": "", "thread": "child1", "status": "overbudget", "cause": "deadline", "reason": "deadline exceeded",
            "failedCall": ["tool": "vocabulary", "error": "create index for ~/trips/flight: timeout"],
            "stopped": .string(stopped),
        ]).jsonText
        let rows = [
            asking([("c1", "architect", architectCallable, #"{"input":"Apply the trip kinds."}"#)], turn: 1, createdAt: "2026-11-17T10:00:00Z"),
            answering(
                "c1", name: "architect", turn: 2, content: output, createdAt: "2026-11-17T10:05:02Z",
                extra: ["ran": ["startedAt": "2026-11-17T10:00:01Z", "finishedAt": "2026-11-17T10:05:01Z"], "subagentWrites": ["records": 0]]),
        ]
        let panel = try #require(panels(rows).first)
        #expect(panel.state == .stopped && panel.stateWords == "Didn't finish")
        #expect(panel.outcomeLabel == "Stopped" && panel.outcome == stopped && panel.stopped == stopped)
        #expect(panel.failedCall == FailedCall(label: "Its last call, vocabulary, failed", error: "create index for ~/trips/flight: timeout"))
        #expect(panel.status(at: .distantFuture) == "Didn't finish · 5 min")
        #expect(panel.reply == nil)
    }

    @Test func anOlderServersStopIsSaidFromItsCauseOrItsStatus() {
        func said(_ payload: JSONValue) -> String { TurnPresenter.stoppedSentence(payload, name: "Architect", job: nil) }
        #expect(said(["status": "overbudget", "cause": "deadline"]) == "Architect ran out of time.")
        #expect(said(["status": "overbudget", "reason": "max turns reached (12)"]) == "Architect didn't finish. It used all 12 of its turns.")
        // What production answered before the server said why.
        #expect(said(["reply": "", "status": "overbudget"]) == "Architect didn't finish. It reached a limit.")
        #expect(said(["status": "error", "reason": "provider said no\ntrace"]) == "Architect didn't finish: provider said no")
    }

    @Test func aCallThatFailedBeforeTheHelperAnsweredSaysTheError() throws {
        var rows = [
            asking([("c1", "architect", architectCallable, #"{"input":"Go."}"#)], turn: 1, createdAt: "2026-11-17T10:00:00Z"),
            answering("c1", name: "architect", turn: 2, content: #"{"error":"tool-call budget exhausted (max 3)"}"#, createdAt: "2026-11-17T10:00:00Z"),
        ]
        rows[1].properties["ok"] = false
        let panel = try #require(panels(rows).first)
        #expect(panel.state == .failed && panel.outcomeLabel == "Failed" && panel.outcome == "tool-call budget exhausted (max 3)")
        #expect(panel.status(at: .distantFuture) == "Failed · less than a second")
    }

    @Test func aRunningCallCountsFromWhenItStartedByTheServersClock() throws {
        let rows = [asking([("c1", "architect", architectCallable, #"{"input":"Go."}"#)], turn: 1, createdAt: "2026-11-17T10:00:00Z")]
        let panel = try #require(panels(rows).first)
        #expect(panel.state == .running && panel.outcome == nil && panel.changed == nil)
        #expect(panel.clock.since == at("2026-11-17T10:00:00Z"))
        #expect(panel.status(at: at("2026-11-17T10:01:23Z")) == "Working · 1:23")
        #expect(panel.spoken(at: at("2026-11-17T10:01:23Z")) == "Working, for 1 min 23 s")
        // A device clock behind the server's counts from nothing, not back.
        #expect(panel.status(at: at("2026-11-17T09:59:00Z")) == "Working · 0:00")
    }

    @Test func aBackgroundCallIsTimedByItsJob() throws {
        let args = #"{"input":"Import these visited places."}"#
        var rows = [
            asking([("c1", "librarian", librarianCallable, args)], turn: 1, createdAt: "2026-11-17T03:56:00Z"),
            answering(
                "c1", name: "librarian", turn: 2, content: #"{"job":"c1","status":"working"}"#, createdAt: "2026-11-17T03:56:08Z",
                extra: [
                    "ran": ["startedAt": "2026-11-17T03:56:01Z", "finishedAt": "2026-11-17T03:56:02Z"],
                    "job": ["state": "working", "startedAt": "2026-11-17T03:56:08.348365Z"],
                ]),
        ]
        let working = try #require(panels(rows).first)
        #expect(working.state == .working && working.stateWords == "Working in the background")
        #expect(working.status(at: at("2026-11-17T03:56:50.4Z")) == "Working in the background · 0:42")

        rows[1].properties["job"] = ["state": "completed", "startedAt": "2026-11-17T03:56:08.348365Z", "finishedAt": "2026-11-17T03:56:46.887408Z"]
        rows[1].properties["subagentWrites"] = ["records": 21, "kinds": .array([.string(places), .string(brands)])]
        rows.append(
            row(
                "s3",
                [
                    "role": "system", "turn": 3,
                    "content": .string(
                        JSONValue.object([
                            "event": "toolCompleted", "toolCallId": "c1", "name": "librarian", "ok": true,
                            "output": #"{"reply":"Saved 21 places.","thread":"child2","status":"ok"}"#,
                        ]).jsonText),
                ], createdAt: "2026-11-17T03:56:47Z"))
        let done = try #require(panels(rows).first)
        #expect(done.state == .finished && done.outcome == "Saved 21 places.")
        #expect(done.status(at: .distantFuture) == "Finished · 39 s")
        #expect(done.changed == "Wrote 21 records in Places and Brands")
    }

    @Test func theStreamSaysWhenEachCallRanAndAReplayedStartIsKept() throws {
        let lines = [
            #"{"kind":"toolStarted","id":"c1","tool":"architect","callable":"\#(architectCallable)","args":"{\"input\":\"Left.\"}","turn":2,"startedAt":"2026-11-17T10:00:01Z"}"#,
            #"{"kind":"toolStarted","id":"c2","tool":"architect","callable":"\#(architectCallable)","args":"{\"input\":\"Right.\"}","turn":3,"startedAt":"2026-11-17T10:00:01Z"}"#,
            #"{"kind":"toolFinished","id":"c1","tool":"architect","turn":2,"ok":true,"output":"{\"reply\":\"Done.\",\"status\":\"ok\"}","startedAt":"2026-11-17T10:00:01.2Z","finishedAt":"2026-11-17T10:00:31.2Z"}"#,
        ]
        var live = LiveRows()
        live.hear(lines.compactMap(AgentEvent.parse))
        let calls = live.turns.flatMap(\.tools)
        #expect(calls.map(\.id) == ["c1", "c2"])
        #expect(calls[0].startedAt == at("2026-11-17T10:00:01.2Z") && calls[0].finishedAt == at("2026-11-17T10:00:31.2Z"))
        #expect(calls[1].startedAt == at("2026-11-17T10:00:01Z") && calls[1].finishedAt == nil)

        // Each has its own clock: one finished, one still running.
        let shown = live.turns.flatMap { turn in
            turn.tools.enumerated().compactMap { index, call in
                TurnPresenter.tool(call, id: "\(turn.id)#\(index)", agent: nil, decided: [], titles: [:]).subagent
            }
        }
        #expect(shown.map(\.state) == [.finished, .running])
        #expect(shown[0].status(at: .distantFuture) == "Finished · 30 s")
        #expect(shown[1].status(at: at("2026-11-17T10:01:00Z")) == "Working · 0:59")

        // Reopened: the rows hold the asking row, and the replayed start
        // is the call's own.
        let stored = StoredTurns(
            transcript(of: [
                asking(
                    [("c1", "architect", architectCallable, #"{"input":"Left."}"#), ("c2", "architect", architectCallable, #"{"input":"Right."}"#)],
                    turn: 1, createdAt: "2026-11-17T10:00:00Z")
            ]))
        #expect(stored.turns[0].tools[1].startedAt == at("2026-11-17T10:00:00Z"))
        var replay = LiveRows()
        replay.hear([AgentEvent.parse(lines[1])!])
        let folded = TurnFold.fold(stored, replay)
        #expect(folded.turns.flatMap(\.tools).first { $0.id == "c2" }?.startedAt == at("2026-11-17T10:00:01Z"))
    }

    @Test func aJobsEndEventSaysTheJobsTimes() throws {
        var live = LiveRows()
        live.hear(
            [
                #"{"kind":"toolStarted","id":"c1","tool":"librarian","callable":"\#(librarianCallable)","turn":2,"startedAt":"2026-11-17T10:00:00Z"}"#,
                #"{"kind":"toolFinished","id":"c1","tool":"librarian","turn":2,"ok":true,"job":"working","output":"{\"job\":\"c1\",\"status\":\"working\"}","startedAt":"2026-11-17T10:00:00Z","finishedAt":"2026-11-17T10:00:01Z"}"#,
                #"{"kind":"toolFinished","id":"c1","tool":"librarian","turn":2,"ok":true,"job":"completed","output":"{\"reply\":\"Saved.\",\"status\":\"ok\"}","startedAt":"2026-11-17T10:00:02Z","finishedAt":"2026-11-17T10:01:02Z"}"#,
            ].compactMap(AgentEvent.parse))
        let call = try #require(live.turns.first?.tools.first)
        #expect(call.startedAt == at("2026-11-17T10:00:00Z") && call.finishedAt == at("2026-11-17T10:00:01Z"))
        #expect(call.jobStartedAt == at("2026-11-17T10:00:02Z") && call.jobFinishedAt == at("2026-11-17T10:01:02Z"))
        let panel = try #require(TurnPresenter.tool(call, id: "x#0", agent: nil, decided: [], titles: [:]).subagent)
        #expect(panel.status(at: .distantFuture) == "Finished · 1 min")
    }

    @Test func otherCallsHaveNoPanel() {
        let call = ToolCall(id: "c", name: "query", callable: "\(Kinds.function)/\(HostFunction.query)", arguments: "{}", output: "{}", ok: true)
        #expect(TurnPresenter.tool(call, id: "x#0", agent: nil, decided: [], titles: [:]).subagent == nil)
    }

    @Test func aLongRequestIsCutAtASentenceOrAWord() {
        #expect(TurnPresenter.summary(of: "Short.", markdown: false) == "Short.")
        // "e.g." ends no sentence.
        let first = "Create three kinds in a new package (e.g. a trips package) with a trip, a flight and a hotel stay, each with its dates and status."
        let long = first + " Then link every booking you find in the owner's mail to the trip it belongs to."
        #expect(TurnPresenter.summary(of: long, markdown: false) == first)
        let one = String(repeating: "word ", count: 60)
        let cut = TurnPresenter.summary(of: one, markdown: false)
        #expect(cut.hasSuffix("…") && cut.count <= TurnPresenter.panelLine + 1)
    }

    @Test func aHeadingAboveItsParagraphLeavesTheParagraph() {
        #expect(TurnPresenter.summary(of: "# Result\nThe records were created.\n\nFurther details.") == "The records were created.")
        #expect(TurnPresenter.summary(of: "## Done\n\n**Three** kinds.") == "Three kinds.")
        // A reply that is only a heading still says it.
        #expect(TurnPresenter.summary(of: "## Done") == "Done")
    }

    @Test func durationsAndClocksAreSaidPlainly() {
        #expect(SubagentClock.elapsed(7) == "0:07" && SubagentClock.elapsed(83) == "1:23" && SubagentClock.elapsed(3723) == "1:02:03")
        #expect(SubagentClock.duration(0.4) == "less than a second" && SubagentClock.duration(45) == "45 s")
        #expect(SubagentClock.duration(130) == "2 min 10 s" && SubagentClock.duration(300) == "5 min" && SubagentClock.duration(3900) == "1 h 5 min")
        #expect(TurnPresenter.changedSentence(records: 1, kinds: [places], moreKinds: 2) == "Wrote 1 record in Places and 2 other kinds")
    }

    @Test func theHelpersFixtureDrawsEveryState() throws {
        let page = try #require(JSONValue.parse(ChatFixtures.helpers()))
        let rows = try (page["records"]?.array ?? []).map { try ValueCoding.decode(Record.self, from: $0) }
        let shown = panels(Array(rows.reversed()))
        #expect(shown.map(\.name) == ["Architect", "Secretary", "Librarian", "Architect", "Designer", "Librarian"])
        #expect(shown.map(\.state) == [.stopped, .finished, .finished, .running, .running, .working])
        #expect(shown[0].failedCall?.label == "Its last call, vocabulary, failed")
        #expect(shown[1].status(at: .distantFuture) == "Finished · 2 min 10 s" && shown[1].changed == "Changed nothing")
        #expect(shown[2].status(at: .distantFuture) == "Finished · 39 s" && shown[2].changed == "Wrote 17 records in Places and Brands")
        #expect(shown[3].askedLabel == "Followed up" && shown[3].clock.since != nil && shown[5].clock.since != nil)
    }
}
