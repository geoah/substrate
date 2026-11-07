import Foundation
import SubstrateCore
import Testing

@testable import ChatKit

private func message(_ id: String, _ props: [String: JSONValue]) -> Record {
    Record(id: id, kind: Kinds.message, properties: props, createdAt: "2026-11-07T10:00:00Z")
}

private let searchCallable = "function:providers.substrate.reamde.dev:firecrawl:websearch"
private let librarian = "substrate.reamde.dev/assistant/librarian"

private let searchOutput = #"""
    {"effects":0,"output":{"results":[
    {"title":"I Tested the Best Robot Vacuums for 2026","url":"https://uk.pcmag.com/robot-vacuums/74630","snippet":"Six days ago, the Dyson..."},
    {"title":"","url":"https://www.youtube.com/watch?v=1"},
    {"title":"Sneaky","url":"javascript:alert(1)"}]}}
    """#

private func search(_ output: String? = searchOutput, query: String? = "quiet robot vacuum", ok: Bool? = true) -> ToolCall {
    ToolCall(
        id: "c1", name: "web_search", callable: searchCallable,
        arguments: query.map { #"{"query":"\#($0)"}"# } ?? "{}", output: output, ok: ok)
}

/// The tool rows one assistant turn presents.
private func rows(_ presented: [TurnPresentation]) -> [ToolRowPresentation] {
    presented.flatMap { turn -> [ToolRowPresentation] in
        if case let .assistant(assistant) = turn.body { return assistant.tools }
        return []
    }
}

private func row(_ call: ToolCall, agent: Agent? = nil) -> ToolRowPresentation {
    TurnPresenter.tool(call, id: "a#0", agent: agent, decided: [], titles: [:])
}

@Suite("The host functions' words", .timeLimit(.minutes(10)))
struct HostFunctionWords {
    private func words(_ function: String, _ arguments: String) -> String {
        toolSummary(ToolCall(id: "c", name: "x", arguments: arguments, output: "{}", ok: true), ResolvedTool(function: function))
    }

    @Test func waitEditAndVocabularyAreSaidAsTheConsoleSaysThem() {
        #expect(words(HostFunction.wait, #"{"jobs":["c1"]}"#) == "Waited for work running in the background")
        let task = "ada.example.com/tasks/task"
        #expect(words(HostFunction.edit, #"{"op":"view","kind":"\#(task)","id":"t1","property":"notes"}"#) == "Read part of the notes of a task")
        #expect(words(HostFunction.edit, #"{"op":"replace","kind":"\#(task)","edits":[{"old":"a","new":"b"}]}"#) == "Edited a task")
        #expect(
            words(HostFunction.edit, #"{"op":"replace","kind":"\#(task)","edits":[{"old":"a","new":"b"},{"old":"c","new":"d"}]}"#)
                == "Edited a task in 2 places")
        // The engine takes the edits encoded as a string too.
        #expect(
            words(HostFunction.edit, #"{"op":"replace","edits":"[{\"old\":\"a\",\"new\":\"b\"},{\"old\":\"c\",\"new\":\"d\"}]"}"#)
                == "Edited a record in 2 places")
        let documents =
            #"[{"kind":"substrate.reamde.dev/core/kind","metadata":{"id":"ada.example.com/garden/plant"}},{"kind":"substrate.reamde.dev/core/kind","metadata":{"id":"ada.example.com/garden/bed"}},{"kind":"substrate.reamde.dev/core/trait","metadata":{"id":"ada.example.com/garden/x"}}]"#
        #expect(words(HostFunction.vocabulary, #"{"plan":true,"documents":\#(documents)}"#) == "Checked a change to your collections (plant and bed)")
        #expect(words(HostFunction.vocabulary, #"{"documents":\#(documents)}"#) == "Changed your collections (plant and bed)")
        #expect(words(HostFunction.vocabulary, #"{"documents":[]}"#) == "Changed your collections")
    }

    @Test func anAgentTheAppCouldNotReadStillNamesEveryHostFunction() {
        #expect(resolveTool(agent: nil, name: "edit", callable: nil).function == HostFunction.edit)
        #expect(resolveTool(agent: nil, name: "vocabulary", callable: nil).function == HostFunction.vocabulary)
        #expect(resolveTool(agent: nil, name: "wait", callable: "function:substrate.reamde.dev:core:wait").function == HostFunction.wait)
    }
}

@Suite("A tool result's shape", .timeLimit(.minutes(10)))
struct ResultShapes {
    @Test func aListOfLinksKeepsItsWebPagesAndTitlesOneWithoutATitleByItsSite() {
        #expect(
            toolResult(search())
                == .webHits([
                    WebHit(
                        title: "I Tested the Best Robot Vacuums for 2026", url: "https://uk.pcmag.com/robot-vacuums/74630",
                        snippet: "Six days ago, the Dyson..."),
                    WebHit(title: "youtube.com", url: "https://www.youtube.com/watch?v=1"),
                ]))
        #expect(
            toolResult(search(#"{"output":{"hits":[{"url":"https://a.example.com/x","description":"About x"}]}}"#))
                == .webHits([
                    WebHit(title: "a.example.com", url: "https://a.example.com/x", snippet: "About x")
                ]))
    }

    @Test func aSearchIsSaidInWordsFromWhatItWasAskedAndFound() {
        #expect(toolResultSummary(search()) == "Searched for “quiet robot vacuum”, 2 results")
        #expect(toolResultSummary(search(nil, ok: nil)) == "Searched for “quiet robot vacuum”")
        #expect(toolResultSummary(search(query: nil)) == "Found 2 results")
        #expect(toolResultSummary(search(#"{"output":{"results":[]}}"#, query: nil)) == "Found 0 results")
        #expect(toolResultSummary(search(#"{"error":"down"}"#, ok: false)) == nil)
    }

    @Test func nothingIsDrawnForAFailedCallOrAListOfSomethingElse() {
        #expect(toolResult(search(ok: false)) == nil)
        #expect(toolResult(search(#"{"output":{"results":[{"name":"x"}]}}"#)) == nil)
        #expect(toolResult(search(#"{"output":{"url":"https://example.com"}}"#)) == nil)
    }

    @Test func aPageIsItsAddressAndTitleAndSaysWhereItRead() {
        let page = search(
            #"{"output":{"url":"https://www.formula1.com/en/latest","title":"F1's youngest world champions","document":"doc1","content":"xxxx"}}"#,
            query: nil)
        #expect(toolResult(page) == .webPage(WebPage(url: "https://www.formula1.com/en/latest", title: "F1's youngest world champions")))
        #expect(toolResultSummary(page) == "Read formula1.com")
    }

    @Test func onlyAWebAddressIsALink() {
        #expect(webURL("javascript:alert(1)") == nil)
        #expect(webURL("mailto:ada@example.com") == nil)
        #expect(webURL("/relative") == nil)
        #expect(webURL("https://") == nil)
        #expect(webURL(" HTTPS://Example.COM ") == "https://example.com/")
        #expect(webHost("https://www.bbc.co.uk/weather") == "bbc.co.uk")
        #expect(webHost("http://localhost:8080/x") == "localhost:8080")
        #expect(webHost("not a url") == "not a url")
    }

    @Test func aSearchCallDrawsItsHitsAsNumberedPillsAndDoesNotRepeatItsPayload() throws {
        let presented = row(search())
        #expect(presented.summary == "Searched for “quiet robot vacuum”, 2 results")
        let sources = try #require(presented.sources)
        #expect(sources.label == "Results")
        #expect(sources.heading == nil)
        #expect(
            sources.pills == [
                SourcePill(
                    index: 1, url: "https://uk.pcmag.com/robot-vacuums/74630", host: "uk.pcmag.com",
                    title: "I Tested the Best Robot Vacuums for 2026", snippet: "Six days ago, the Dyson..."),
                // A title that is only the site is not said twice.
                SourcePill(index: 2, url: "https://www.youtube.com/watch?v=1", host: "youtube.com", title: nil, snippet: nil),
            ])
        #expect(!presented.details.contains { if case .output = $0 { true } else { false } })
        let empty = row(search(#"{"output":{"results":[]}}"#))
        #expect(empty.sources?.pills == [])
        #expect(empty.sources?.empty == "No results.")
    }

    @Test func aPageReadIsOnePillWithoutANumber() {
        let presented = row(search(#"{"output":{"url":"https://www.formula1.com/en/latest","title":"F1","content":"x"}}"#, query: nil))
        #expect(presented.summary == "Read formula1.com")
        #expect(presented.sources?.pills == [SourcePill(index: nil, url: "https://www.formula1.com/en/latest", host: "formula1.com", title: "F1")])
    }

    @Test func theShapeIsTheFunctionsAnswerNeverAHostFunctionsOrASubAgents() {
        // `query` answers records, and a sub-agent its reply: neither is
        // read for pages whatever its payload holds.
        let hosted = ToolCall(
            id: "c", name: "query", callable: "function:substrate.reamde.dev:core:query", arguments: "{}",
            output: #"{"output":{"results":[{"url":"https://a.example.com"}]}}"#, ok: true)
        #expect(row(hosted).sources == nil)
        let asked = ToolCall(
            id: "c", name: "librarian", callable: "agent:substrate.reamde.dev:assistant:librarian", arguments: "{}",
            output: #"{"output":{"results":[{"url":"https://a.example.com"}]}}"#, ok: true)
        #expect(row(asked).sources == nil)
    }
}

@Suite("A reply's sources", .timeLimit(.minutes(10)))
struct ReplySources {
    @Test func eachPageIsListedOnceInTheOrderTheReplyFirstCitesIt() {
        let text = [
            "Norris won ([Formula 1](https://www.formula1.com/a)).",
            "",
            "- also [Wikipedia](https://en.wikipedia.org/wiki/F1)",
            "- and **[Formula 1 again](https://www.formula1.com/a)**",
            "",
            "A record [Q4](substrate://ada.example.com/tasks/task/q4) is not a page, nor is [mail](mailto:a@example.com).",
            "",
            "```",
            "[code](https://code.example.com)",
            "```",
            "",
            "`[span](https://span.example.com)` and ![a picture](https://img.example.com/p.png) and <https://auto.example.com/x>",
            "",
            "> quoted [BBC](https://www.bbc.co.uk/news \"title\")",
        ].joined(separator: "\n")
        #expect(
            replyLinks(text) == [
                ReplyLink(url: "https://www.formula1.com/a", text: "Formula 1"),
                ReplyLink(url: "https://en.wikipedia.org/wiki/F1", text: "Wikipedia"),
                ReplyLink(url: "https://auto.example.com/x", text: "https://auto.example.com/x"),
                ReplyLink(url: "https://www.bbc.co.uk/news", text: "BBC"),
            ])
        #expect(replyLinks("Just words, and `code`.") == [])
        #expect(
            replyLinks("[**Bold** title](https://a.example.com/x_(y)) and [\\[x\\]](https://b.example.com)") == [
                ReplyLink(url: "https://a.example.com/x_(y)", text: "Bold title"),
                ReplyLink(url: "https://b.example.com/", text: "[x]"),
            ])
    }

    private func assistant(_ turn: Turn, live: Bool) -> AssistantTurn? {
        if case let .assistant(assistant) = TurnPresenter.present(turn, agent: nil, live: live, decided: [], titles: [:]).body { return assistant }
        return nil
    }

    @Test func aFinishedReplyShowsItsSourcesAndAStreamingOneDoesNot() throws {
        let turn = Turn(
            id: "a", role: .assistant,
            content: "See [the race](https://www.formula1.com/a) and [the table](https://www.formula1.com/b).")
        let finished = try #require(assistant(turn, live: false))
        let sources = try #require(finished.sources)
        #expect(sources.heading == "Sources")
        #expect(sources.pills.map(\.index) == [1, 2])
        #expect(sources.pills.map(\.title) == ["the race", "the table"])
        #expect(sources.pills.allSatisfy { $0.host == "formula1.com" })
        let streaming = try #require(assistant(turn, live: true))
        #expect(streaming.sources == nil)
        let plain = try #require(assistant(Turn(id: "b", role: .assistant, content: "No links here."), live: false))
        #expect(plain.sources == nil)
    }
}

@Suite("A call that runs in the background", .timeLimit(.minutes(10)))
struct BackgroundCalls {
    private let assistantRow = message(
        "a",
        [
            "role": "assistant", "content": "", "turn": 1,
            "toolCalls": [["id": "c3", "name": "librarian", "callable": "agent:substrate.reamde.dev:assistant:librarian", "arguments": "{}"]],
        ])

    private func toolRow(_ state: String) -> Record {
        message(
            "t",
            [
                "role": "tool", "toolCallId": "c3", "name": "librarian", "callable": "agent:substrate.reamde.dev:assistant:librarian",
                "content": #"{"job":"c3","status":"working"}"#, "ok": true, "turn": 2,
                "job": ["state": .string(state), "startedAt": "2026-11-07T10:00:01Z"],
            ])
    }

    private func ended(ok: Bool, output: String) -> Record {
        let envelope: JSONValue = [
            "event": "toolCompleted", "toolCallId": "c3", "name": "librarian", "ok": .bool(ok), "output": .string(output),
            "callable": "agent:substrate.reamde.dev:assistant:librarian",
        ]
        return message("s", ["role": "system", "content": .string(envelope.jsonText), "turn": 3])
    }

    @Test func aWorkingCallSaysSoAndShowsItsHandleAsNothing() throws {
        let presented = rows(TurnPresenter.present(transcript(of: [assistantRow, toolRow("working")]), agent: nil, liveKey: nil, titles: [:]))
        let working = try #require(presented.first)
        #expect(working.status == .working)
        #expect(working.statusLabel == "Working in the background")
        #expect(working.summary == "Asked Librarian")
        #expect(working.details.isEmpty)
    }

    @Test func itsEndSettlesTheCallWithWhatItFoundAndSaysSoInTheThread() throws {
        let turns = transcript(of: [
            assistantRow, toolRow("completed"), ended(ok: true, output: #"{"reply":"Two notes mention the boiler.","thread":"child1"}"#),
        ])
        let presented = TurnPresenter.present(turns, agent: nil, liveKey: nil, titles: [:])
        let call = try #require(rows(presented).first)
        #expect(call.status == .done)
        #expect(call.statusLabel == nil)
        #expect(call.details == [.reply(heading: "Librarian said", text: "Two notes mention the boiler.")])
        #expect(presented.last?.body == .jobEnded(OutcomeLine(text: "Librarian finished in the background", done: true)))
    }

    @Test func aJobThatDidNotFinishReadsAsFailedNotAsRawJSON() throws {
        let turns = transcript(of: [assistantRow, toolRow("canceled"), ended(ok: false, output: #"{"error":"stopped by the owner"}"#)])
        let presented = TurnPresenter.present(turns, agent: nil, liveKey: nil, titles: [:])
        let call = try #require(rows(presented).first)
        #expect(call.status == .failed)
        #expect(call.details == [.failed("stopped by the owner")])
        #expect(presented.last?.body == .jobEnded(OutcomeLine(text: "Librarian didn't finish in the background", done: false)))
        // Its end out of the window: the row's own state still says so.
        let alone = rows(TurnPresenter.present(transcript(of: [assistantRow, toolRow("failed")]), agent: nil, liveKey: nil, titles: [:]))
        #expect(alone.first?.status == .failed)
    }

    @Test func aFunctionsEndIsNamedByTheFunction() {
        let notice = JobNotice(callID: "c1", name: "web_search", callable: searchCallable, ok: true)
        #expect(TurnPresenter.jobName(notice, agent: nil) == "Websearch")
        #expect(TurnPresenter.jobName(JobNotice(callID: "c1", name: "deep_research", ok: true), agent: nil) == "Deep research")
    }

    @Test func theLiveCardTakesTheJobsEndInPlaceOfItsHandle() {
        var live = LiveOverlay()
        live.pushToolStart(ToolCall(id: "c1", name: "web_search", callable: searchCallable, arguments: #"{"query":"q"}"#), seq: 1)
        live.settleTool(ToolCall(id: "c1", name: "web_search", arguments: ""), output: #"{"job":"c1","status":"working"}"#, ok: true, job: "working", seq: 2)
        #expect(live.turns[0].tools[0].job == "working")
        live.settleTool(ToolCall(id: "c1", name: "web_search", arguments: ""), output: searchOutput, ok: true, job: "completed", seq: 3)
        #expect(live.turns.count == 1)
        #expect(live.turns[0].tools[0].job == "completed")
        #expect(row(live.turns[0].tools[0]).summary == "Searched for “q”, 2 results")
        let event = AgentEvent.parse(#"{"kind":"toolFinished","id":"c1","ok":true,"output":"{}","job":"working","turn":1}"#)
        #expect(event?.job == "working")
    }
}

@Suite("A write held for review", .timeLimit(.minutes(10)))
struct HeldWrites {
    static let request = "01h2xcejqtf2nbrexx3vqjhp41"
    static let refusal =
        #"{"error":"substrate: held for review: held for review as substrate.reamde.dev/core/recordpatchrequest/\#(request) — policy review gates patch ada.example.com/tasks/task for agent substrate.reamde.dev/assistant/moss"}"#

    private func write(_ output: String = HeldWrites.refusal) -> ToolCall {
        ToolCall(
            id: "w1", name: "write", callable: "function:substrate.reamde.dev:core:write",
            arguments: #"{"op":"patch","kind":"ada.example.com/tasks/task","id":"t1","input":{"properties":{"due":"2026-11-09"}}}"#,
            output: output, ok: false)
    }

    @Test func itWaitsForTheOwnerInsteadOfFailing() {
        let presented = row(write())
        #expect(presented.status == .held)
        #expect(presented.statusLabel == "Waiting for you")
        #expect(presented.attachment == .suggestion(Self.request))
        #expect(!presented.details.contains { if case .failed = $0 { true } else { false } })
    }

    @Test func theStampedRequestDecidesOnTheRow() {
        var stamped = write(#"{"error":"substrate: held for review"}"#)
        stamped.changes = [ChangeStamp(seq: 9, op: "put", kind: Kinds.request, id: "mnopqrstuvwx")]
        #expect(row(stamped).status == .held)
        #expect(row(stamped).attachment == .suggestion("mnopqrstuvwx"))
    }

    @Test func anyOtherFailureStillFails() {
        let refused = row(write(#"{"error":"t1 is not in agent moss's writes allowlist"}"#))
        #expect(refused.status == .failed)
        #expect(refused.statusLabel == nil)
        #expect(refused.attachment == nil)
        #expect(refused.details == [.failed("t1 is not in agent moss's writes allowlist")])
        // A request path that names no minted id is no request.
        let forged = row(write(#"{"error":"held for review as substrate.reamde.dev/core/recordpatchrequest/../x"}"#))
        #expect(forged.status == .failed)
        let other = row(write(#"{"error":"held for review as x.example.com/pkg/thing/01h2xcejqtf2nbrexx3vqjhp41"}"#))
        #expect(other.status == .failed)
    }
}

@Suite("A streamed turn's calls", .timeLimit(.minutes(10)))
struct StreamedCalls {
    /// A search, a write the owner's policy holds and a sub-agent sent to
    /// work in the background, streamed and held there.
    static let stream = [
        #"{"kind":"thread","thread":"t1"}"#,
        #"{"kind":"toolStarted","id":"c1","tool":"web_search","callable":"function:providers.substrate.reamde.dev:firecrawl:websearch","args":"{\"query\":\"quiet robot vacuum\"}","turn":1}"#,
        #"{"kind":"toolFinished","id":"c1","tool":"web_search","callable":"function:providers.substrate.reamde.dev:firecrawl:websearch","ok":true,"output":"{\"effects\":0,\"output\":{\"results\":[{\"title\":\"Robot vacuums\",\"url\":\"https://uk.pcmag.com/robot-vacuums\"}]}}","turn":1}"#,
        #"{"kind":"toolStarted","id":"c2","tool":"write","callable":"function:substrate.reamde.dev:core:write","args":"{\"op\":\"patch\",\"kind\":\"ada.example.com/tasks/task\",\"id\":\"t1\"}","turn":1}"#,
        #"{"kind":"toolFinished","id":"c2","tool":"write","callable":"function:substrate.reamde.dev:core:write","ok":false,"output":"{\"error\":\"substrate: held for review: held for review as substrate.reamde.dev/core/recordpatchrequest/01h2xcejqtf2nbrexx3vqjhp41 — policy review gates patch\"}","turn":1}"#,
        #"{"kind":"toolStarted","id":"c3","tool":"librarian","callable":"agent:substrate.reamde.dev:assistant:librarian","args":"{}","turn":1}"#,
        #"{"kind":"toolFinished","id":"c3","tool":"librarian","callable":"agent:substrate.reamde.dev:assistant:librarian","ok":true,"output":"{\"job\":\"c3\",\"status\":\"working\"}","job":"working","turn":1}"#,
        #"{"kind":"done","result":{"reply":"","thread":"t1","status":"ok","turns":2}}"#,
    ]

    @Test func eachCallReadsAsWhatItDidWhileTheTurnStreams() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(Self.stream, holdAfter: 7))])
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Find me a quiet one"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in rows(await rig.conversation(key)?.turns ?? []).count == 3 }
        let screen = try #require(await rig.conversation(key))
        let calls = rows(screen.turns)
        #expect(calls[0].summary == "Searched for “quiet robot vacuum”, 1 result")
        #expect(calls[0].sources?.pills.map(\.host) == ["uk.pcmag.com"])
        #expect(calls[1].status == .held)
        #expect(calls[1].statusLabel == "Waiting for you")
        #expect(calls[1].attachment == .suggestion(HeldWrites.request))
        #expect(screen.cards[HeldWrites.request]?.suggestion != nil)
        #expect(calls[2].status == .working)
        #expect(calls[2].statusLabel == "Working in the background")
        rig.server.release()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
    }
}

@Suite("What a run's end says of its reply", .timeLimit(.minutes(10)))
struct RunEnds {
    static let kind = "ada.example.com/tasks/task"
    static let stream = [
        #"{"kind":"thread","thread":"t1"}"#,
        #"{"kind":"delta","text":"See [Q4](substrate://ada.example.com/tasks/task/q4) and [Ghost](substrate://ada.example.com/tasks/task/ghost).","turn":1}"#,
        #"{"kind":"done","result":{"reply":"See them.","thread":"t1","status":"ok","turns":2,"mentions":[{"kind":"ada.example.com/tasks/task","id":"q4","text":"Q4"},{"kind":"ada.example.com/tasks/task","id":"ghost","text":"Ghost","missing":true},{"kind":"ada.example.com/tasks/task"}]}}"#,
    ]

    @Test func theDoneEventsMentionsReadTolerantly() {
        let done = AgentEvent.parse(Self.stream[2])
        #expect(
            done?.result?.mentions == [
                Mention(kind: Self.kind, id: "q4", text: "Q4"), Mention(kind: Self.kind, id: "ghost", text: "Ghost", missing: true),
            ])
        #expect(AgentEvent.parse(#"{"kind":"done","result":{"reply":"x","status":"ok"}}"#)?.result?.mentions == nil)
    }

    @Test func aRepliesLinksStandFromTheRunsEndBeforeItsRowsLand() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(Self.stream))])
        try await rig.signIn()
        // The handover's read waits, so the overlay is all there is.
        rig.server.hold("/llm/thread/t1")
        let key = try await rig.newChat()
        try await rig.send(.draft("What's due?"), to: key)
        try await rig.send(.send, to: key)
        @Sendable func reply() async -> AssistantTurn? {
            for turn in await rig.conversation(key)?.turns ?? [] {
                if case let .assistant(assistant) = turn.body { return assistant }
            }
            return nil
        }
        try await rig.harness.until { _ in await reply()?.linked.isEmpty == false }
        #expect(await rig.live(key)?.phase == .handingOver)
        let held = try #require(await reply())
        #expect(held.linked == [RecordReference(kind: Self.kind, id: "q4")])
        #expect(held.missing == ["\(Self.kind)/ghost"])
        rig.server.letGo("/llm/thread/t1")
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
    }

    @Test func aSubAgentsCallLinksItsOwnConversation() {
        let asked = ToolCall(
            id: "c", name: "librarian", callable: "agent:substrate.reamde.dev:assistant:librarian", arguments: "{}",
            output: #"{"reply":"Two notes.","thread":"child1","status":"ok"}"#, ok: true)
        #expect(row(asked).thread == ThreadLink(thread: "child1", label: "Open its conversation"))
        var stamped = asked
        stamped.output = #"{"error":"budget"}"#
        stamped.ok = false
        stamped.subagentWrites = SubagentWrites(thread: "child2", records: 1, kinds: [], moreKinds: 0)
        #expect(row(stamped).thread?.thread == "child2")
        #expect(row(search()).thread == nil)
    }
}
