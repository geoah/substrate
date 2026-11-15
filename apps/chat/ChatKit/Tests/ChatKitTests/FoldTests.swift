import Foundation
import SubstrateCore
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

/// One `llm/message` row, as a read answers it.
func messageRow(
    _ id: String, _ role: String, _ content: String = "", turn: Int, calls: [String] = [], answers: String? = nil,
    reasoning: String? = nil, cutBy: String? = nil, ok: Bool? = nil, createdAt: String = "2026-10-31T10:00:00Z"
) -> Record {
    var properties: [String: JSONValue] = ["role": .string(role), "content": .string(content), "turn": .number(JSONNumber(turn))]
    if !calls.isEmpty {
        properties["toolCalls"] = .array(calls.map { .object(["id": .string($0), "name": "query", "arguments": "{}"]) })
    }
    if let answers { properties["toolCallId"] = .string(answers) }
    if let reasoning { properties["reasoning"] = .string(reasoning) }
    if let cutBy { properties["cutBy"] = .string(cutBy) }
    if let ok { properties["ok"] = .bool(ok) }
    return Record(id: id, kind: Kinds.message, properties: properties, createdAt: createdAt, updatedAt: createdAt)
}

/// A text cut into pieces of `size` characters.
private func pieces(_ text: String, _ size: Int) -> [String] {
    var out: [String] = []
    var rest = Substring(text)
    while !rest.isEmpty {
        out.append(String(rest.prefix(size)))
        rest = rest.dropFirst(size)
    }
    return out
}

/// The pieces of `text` as `delta` events of `turn`, each at its position
/// where `positions` says the server sends one.
private func deltas(_ text: String, turn: Int, size: Int = 3, positions: Bool = true) -> [AgentEvent] {
    var at = 0
    return pieces(text, size).map { piece in
        defer { at += piece.unicodeScalars.count }
        return .delta(piece, turn: turn, at: positions ? at : nil)
    }
}

@Suite("A streamed text is written where it belongs", .timeLimit(.minutes(10)))
struct TextRunsTests {
    static let text = "The balcony plants need water twice a week: 🌱 the tomatoes more, e\u{301}ven in May 🇬🇷."

    private func written(_ writes: [(String, Int)]) -> (text: String, runs: TextRuns) {
        var text = ""
        var runs = TextRuns()
        for (piece, at) in writes { runs.write(piece, at: at, into: &text) }
        return (text, runs)
    }

    /// The pieces of the text, each with the code points before it.
    private func placed(_ size: Int) -> [(String, Int)] {
        var at = 0
        return pieces(Self.text, size).map { piece in
            defer { at += piece.unicodeScalars.count }
            return (piece, at)
        }
    }

    @Test func piecesInOrderAreTheTextAndPositionsCountCodePoints() {
        for size in [1, 2, 5, 40] {
            let out = written(placed(size))
            #expect(out.text == Self.text && out.runs.whole && out.runs.end == Self.text.unicodeScalars.count)
        }
        // An emoji of several code points cut between two pieces is whole
        // again once both are written.
        var text = ""
        var runs = TextRuns()
        runs.write("\u{1F1EC}", at: 0, into: &text)
        runs.write("\u{1F1F7}", at: 1, into: &text)
        #expect(text == "🇬🇷" && text.count == 1)
    }

    @Test func aReplayIsWrittenOverWhatIsHeldAndNothingIsEmptiedOrDoubled() {
        let all = placed(4)
        for held in 0 ... all.count {
            var text = ""
            var runs = TextRuns()
            for (piece, at) in all.prefix(held) { runs.write(piece, at: at, into: &text) }
            // The stream is joined again: every piece from the start, each
            // in turn, and the text never gets shorter on the way.
            var shortest = text.unicodeScalars.count
            for (piece, at) in all {
                let before = text
                let grew = runs.write(piece, at: at, into: &text)
                #expect(text.hasPrefix(before) && grew == (text != before))
                shortest = min(shortest, text.unicodeScalars.count)
            }
            #expect(text == Self.text && runs.whole)
            #expect(shortest >= all.prefix(held).reduce(0) { $0 + $1.0.unicodeScalars.count })
            // And as one piece, as a server at `agents` 5 replays it.
            #expect(runs.write(Self.text, at: 0, into: &text) == false && text == Self.text)
        }
    }

    @Test func piecesInAnyOrderAndTwiceEndAsTheText() {
        let all = placed(3)
        var seed: UInt64 = 7
        for _ in 0 ..< 40 {
            var order = all + all.prefix(5)
            for index in order.indices.reversed() {
                seed = seed &* 6_364_136_223_846_793_005 &+ 1_442_695_040_888_963_407
                order.swapAt(index, Int(seed >> 33) % (index + 1))
            }
            let out = written(order)
            #expect(out.text == Self.text && out.runs.whole)
        }
    }

    @Test func aGapIsHeldOpenAndALaterReplayFillsIt() {
        var text = ""
        var runs = TextRuns()
        runs.write("The balcony", at: 0, into: &text)
        // The replay lost its middle: the live tail starts past what is held.
        #expect(runs.write("twice a week", at: 30, into: &text))
        #expect(text == "The balconytwice a week" && !runs.whole && runs.end == 42)
        // A piece that goes on from the tail, and one inside the head.
        runs.write(".", at: 42, into: &text)
        #expect(runs.write("balc", at: 4, into: &text) == false)
        #expect(text == "The balconytwice a week.")
        // The next replay holds it all.
        #expect(runs.write("The balcony plants need water twice a week.", at: 0, into: &text))
        #expect(text == "The balcony plants need water twice a week." && runs.whole)
    }

    @Test func aShorterPieceOverALongerTextLeavesTheRest() {
        var text = ""
        var runs = TextRuns()
        runs.write("Hello there", at: 0, into: &text)
        #expect(runs.write("Hello", at: 0, into: &text) == false)
        #expect(text == "Hello there")
        // What the server says now stands where it says it.
        #expect(runs.write("J", at: 0, into: &text) == false)
        #expect(text == "Jello there" && runs.whole)
    }
}

@Suite("A stream's events are provisional rows", .timeLimit(.minutes(10)))
struct LiveRowsTests {
    @Test func deltasAppendAndAToolRoundEndsNoTurnButTheNextReplyIsAnother() {
        var live = LiveRows()
        live.hear([
            .delta("Hel", turn: 1, at: 0), .delta("lo", turn: 1, at: 3), .toolStarted("c1", turn: 2), .toolFinished("c1", turn: 2),
            .delta("After", turn: 3, at: 0),
        ])
        #expect(live.turns.map(\.id) == ["turn-1", "turn-3"])
        #expect(live.turns[0].content == "Hello" && live.turns[0].tools[0].ok == true && live.turns[0].tools[0].ordinal == 2)
        #expect(live.turns[1].content == "After" && live.turns[1].ordinal == 3)
    }

    @Test func aStreamThatNamesNoTurnWritesToTheLastUntilItsToolsAreDone() {
        var live = LiveRows()
        live.hear([.delta("Hel"), .delta("lo"), .toolStarted("c1"), .toolFinished("c1"), .delta("After")])
        #expect(live.turns.map(\.content) == ["Hello", "After"])
        #expect(live.turns[0].tools.count == 1)
    }

    @Test func aStreamThatNamesNoTurnIsMadeAnewWhenItIsJoinedAgain() {
        let events: [AgentEvent] = [.delta("Hel"), .delta("lo"), .toolStarted("c1"), .toolFinished("c1"), .delta("After")]
        var live = LiveRows()
        live.hear(events)
        // Joined again at every point of the replay: the first reply is
        // never written into the last, and the whole is the stream heard
        // once.
        for count in 1 ... events.count {
            var again = live
            again.begin(replaying: true)
            again.hear(Array(events.prefix(count)))
            #expect(again.turns.first?.content == String("Hello".prefix(count == 1 ? 3 : 5)))
            if count == events.count {
                #expect(again.turns.map(\.content) == ["Hello", "After"])
                #expect(again.turns[0].tools.map(\.id) == ["c1"])
            }
        }
    }

    @Test func anUnclaimedFinishOpensASettledCard() {
        var live = LiveRows()
        live.hear([.toolFinished("x", turn: 4, output: #"{"error":"budget"}"#, ok: false)])
        #expect(live.turns.count == 1 && live.turns[0].id == "call-x")
        #expect(live.turns[0].tools[0].ok == false)
    }

    @Test func reasoningCountsItsSpanAndTextEndsThinking() {
        var live = LiveRows()
        let t0 = Date(timeIntervalSince1970: 100)
        live.apply(.reasoning("Let me ", turn: 1, at: 0), seq: 1, now: t0)
        live.apply(.reasoning("think", turn: 1, at: 7), seq: 2, now: t0.addingTimeInterval(2.5))
        #expect(live.turns[0].thinking)
        #expect(live.turns[0].reasoningMillis == 2500)
        live.apply(.delta("Answer", turn: 1, at: 0), seq: 3, now: t0.addingTimeInterval(3))
        #expect(!live.turns[0].thinking)
        #expect(live.turns[0].reasoning == "Let me think" && live.turns[0].content == "Answer")
        // Heard again a minute later, on a return to the front: the model
        // is not thinking again, and thought for no longer.
        live.begin()
        let moved = live.apply(.reasoning("Let me think", turn: 1, at: 0), seq: 4, now: t0.addingTimeInterval(60))
        #expect(!moved && !live.turns[0].thinking && live.turns[0].reasoningMillis == 2500)
    }

    @Test func truncationMarksItIncomplete() {
        var live = LiveRows()
        live.apply(AgentEvent(kind: .truncated), seq: 1, now: Date())
        #expect(live.incomplete)
    }

    @Test func replayIsDroppedBySetMembershipNotACutoff() {
        // A resolution written while the reply streamed took a later ordinal.
        let held: Set<Int> = [1, 2, 5]
        #expect(!isReplayed(.delta("x", turn: 3), held: held))
        #expect(isReplayed(.delta("x", turn: 2), held: held))
        #expect(!isReplayed(AgentEvent(kind: .done), held: held))
    }

    @Test func aReplyThatIsOnlyCallsIsATurnOfItsOwnKeyedByItsCall() {
        var live = LiveRows()
        // Reply 1 asks for two calls (ordinals 2 and 3); reply 4 streams no
        // text and asks for one (ordinal 5).
        live.hear([
            .delta("Looking", turn: 1, at: 0), .toolStarted("a", turn: 2), .toolStarted("b", turn: 3), .toolFinished("b", turn: 3),
            .toolFinished("a", turn: 2), .toolStarted("c", turn: 5), .toolFinished("c", turn: 5), .delta("Done", turn: 6, at: 0),
        ])
        #expect(live.turns.map(\.id) == ["turn-1", "call-c", "turn-6"])
        #expect(live.turns.map { $0.tools.map(\.id) } == [["a", "b"], ["c"], []])
    }

    /// A run with thinking, text, two rounds of calls, a job that ends
    /// later and a compaction, as a server at `agents` 5 sends it.
    static let run: [AgentEvent] =
        [.reasoning("Let me ", turn: 1, at: 0), .reasoning("look.", turn: 1, at: 7)]
        + deltas("I'll check the list. ", turn: 1)
        + [
            .toolStarted("a", turn: 2, args: #"{"q":"plants"}"#), .toolStarted("b", "web_search", turn: 3),
            .toolFinished("b", "web_search", turn: 3, output: #"{"job":"b","status":"working"}"#, job: "working"),
            .toolFinished("a", turn: 2, output: #"{"records":[]}"#),
            AgentEvent(kind: .compacted),
        ]
        + deltas("Two things 🌱 need doing.", turn: 5)
        + [.toolFinished("b", "web_search", turn: 3, output: #"{"results":[]}"#, job: "completed")]
        + deltas(" And a third.", turn: 5).map { event in
            var event = event
            event.at = (event.at ?? 0) + "Two things 🌱 need doing.".unicodeScalars.count
            return event
        }

    private func heard(_ events: [AgentEvent]) -> LiveRows {
        var live = LiveRows()
        live.hear(events)
        return live
    }

    /// What is drawn of the rows: everything but the clock and the pass.
    private func drawn(_ live: LiveRows) -> [Turn] {
        live.turns.map { turn in
            var turn = turn
            turn.reasoningMillis = nil
            turn.reasoningStartedAt = nil
            return turn
        }
    }

    @Test func aStreamJoinedAgainAtAnyPointIsTheStreamHeardOnce() {
        let whole = heard(Self.run)
        #expect(whole.turns.map(\.id) == ["turn-1", "summary-4", "turn-5"])
        for held in 0 ... Self.run.count {
            var live = heard(Array(Self.run.prefix(held)))
            // Joined again: every event from the turn's start, and at each
            // one nothing held is let go.
            live.begin()
            for (index, event) in Self.run.enumerated() {
                let before = live
                live.apply(event, seq: 100 + index, now: Date(timeIntervalSince1970: 500))
                for turn in before.turns {
                    let now = live.turns.first { $0.id == turn.id }
                    #expect(now?.content.hasPrefix(turn.content) == true, "\(turn.id) lost text at \(index) after \(held)")
                    #expect((now?.reasoning ?? "").hasPrefix(turn.reasoning ?? ""))
                    #expect(now?.tools.count ?? 0 >= turn.tools.count)
                }
            }
            #expect(drawn(live) == drawn(whole), "after \(held) events held")
            #expect(!live.incomplete)
        }
    }

    @Test func belowTheRevisionAReplayKeepsTheLongerTextAndNeverShowsLess() {
        // The same run from a server that sends no positions: each pass
        // starts at the turn's start, so a piece goes after the ones this
        // pass delivered.
        let plain = Self.run.map { event in
            var event = event
            event.at = nil
            return event
        }
        let whole = heard(plain)
        #expect(whole.turns.last?.content == "Two things 🌱 need doing. And a third.")
        for held in 0 ... plain.count {
            var live = heard(Array(plain.prefix(held)))
            // The replay reaches less far than what is held, then further.
            for reach in [held / 2, plain.count] {
                live.begin()
                let before = live
                for (index, event) in plain.prefix(reach).enumerated() {
                    live.apply(event, seq: 200 + index, now: Date(timeIntervalSince1970: 500))
                }
                for turn in before.turns {
                    #expect(live.turns.first { $0.id == turn.id }?.content.hasPrefix(turn.content) == true)
                }
            }
            #expect(drawn(live) == drawn(whole), "after \(held) events held")
        }
    }

    @Test func aReplayThatLostItsMiddleKeepsWhatWasHeldAndSaysSo() {
        // With positions: the head, a gap, the live tail; the next replay
        // fills the gap.
        var live = LiveRows()
        live.hear([.delta("The balcony", turn: 1, at: 0)])
        live.begin()
        live.hear([.delta("The bal", turn: 1, at: 0), AgentEvent(kind: .truncated), .delta("twice a week", turn: 1, at: 30)])
        #expect(live.turns[0].content == "The balconytwice a week" && live.incomplete)
        live.begin()
        live.hear([.delta("The balcony plants need water twice a week.", turn: 1, at: 0)])
        #expect(live.turns[0].content == "The balcony plants need water twice a week.")

        // Without them: what follows the loss goes after what is held,
        // never over it.
        var plain = LiveRows()
        plain.hear([.delta("The balcony")])
        plain.begin()
        plain.hear([.delta("The bal"), AgentEvent(kind: .truncated), .delta("twice a week")])
        #expect(plain.turns[0].content == "The balconytwice a week" && plain.incomplete)
    }

    @Test func aJobThatEndedIsNotWorkingAgainWhenItsFirstAnswerIsHeardAgain() {
        var live = LiveRows()
        live.hear([
            .toolStarted("c1", "web_search", turn: 2), .toolFinished("c1", "web_search", turn: 2, output: "handle", job: "working"),
            .toolFinished("c1", "web_search", turn: 2, output: "result", job: "completed"),
        ])
        live.begin()
        let moved = live.apply(.toolFinished("c1", "web_search", turn: 2, output: "handle", job: "working"), seq: 9, now: Date())
        #expect(!moved && live.turns[0].tools[0].job == "completed" && live.turns[0].tools[0].output == "result")
    }
}

@Suite("One fold of the rows and the stream", .timeLimit(.minutes(10)))
struct TurnFoldTests {
    /// The run of `LiveRowsTests.run` as its rows: the message, the reply
    /// with its two calls and their rows, the summary, the last reply.
    static let rows: [Record] = [
        messageRow("m0", "user", "What needs doing?", turn: 0),
        messageRow("m1", "assistant", "I'll check the list. ", turn: 1, calls: ["a", "b"], reasoning: "Let me look."),
        messageRow("m2", "tool", #"{"records":[]}"#, turn: 2, answers: "a"),
        messageRow("m3", "tool", #"{"results":[]}"#, turn: 3, answers: "b"),
        messageRow("m4", "summary", "Earlier: plants.", turn: 4),
        messageRow("m5", "assistant", "Two things 🌱 need doing. And a third.", turn: 5),
    ]

    private func live(sending message: String = "What needs doing?", baseline: Int? = -1, _ events: [AgentEvent]) -> LiveRows {
        var live = LiveRows()
        live.send(message, seq: 1, baseline: baseline, at: Timestamp.parse("2026-10-31T10:00:00Z")!)
        live.confirm()
        live.hear(events)
        return live
    }

    /// What a reader sees of the turns: who said what, and each call once.
    private func said(_ turns: [Turn]) -> [String] {
        turns.map { "\($0.role.rawValue):\($0.content)" + ($0.tools.isEmpty ? "" : "[\($0.tools.map(\.id).joined(separator: ","))]") }
    }

    @Test func eventsThenRowsAreOneListUnderTheIdsTheStreamGave() {
        let live = live(LiveRowsTests.run)
        // Before any row: the message, and what the stream drew.
        let before = TurnFold.fold(StoredTurns([]), live)
        #expect(before.turns.map(\.id) == ["sent-1", "turn-1", "summary-4", "turn-5"])
        #expect(before.entries.allSatisfy { $0.stored == nil })
        // The rows land: each takes its turn's place and id, and nothing
        // is said twice.
        let after = TurnFold.fold(StoredTurns(transcript(of: Self.rows)), live)
        #expect(after.turns.map(\.id) == ["sent-1", "turn-1", "summary-4", "turn-5"])
        #expect(after.turns.map(\.row) == ["m0", "m1", "m4", "m5"])
        #expect(said(after.turns) == said(transcript(of: Self.rows)))
        #expect(after.claims == ["m0": "sent-1", "m1": "turn-1", "m4": "summary-4", "m5": "turn-5"])
        #expect(after.left.isEmpty)
        // Kept for the rows, the ids stand without the stream.
        let settled = StoredTurns(transcript(of: Self.rows), aliases: after.claims)
        #expect(settled.turns.map(\.id) == after.turns.map(\.id))
        #expect(TurnFold.fold(settled, .empty).entries.map(\.stored) == [0, 1, 2, 3])
    }

    @Test func atEveryPointOfTheStreamAndEveryPrefixOfTheRowsNothingIsDoubledOrLost() {
        let all = LiveRowsTests.run
        let whole = said(transcript(of: Self.rows))
        for heard in 0 ... all.count {
            let live = live(Array(all.prefix(heard)))
            var longest = 0
            for held in 0 ... Self.rows.count {
                let folded = TurnFold.fold(StoredTurns(transcript(of: Array(Self.rows.prefix(held)))), live)
                let turns = folded.turns
                #expect(Set(turns.map(\.id)).count == turns.count, "an id twice at \(heard), \(held)")
                // Each call once, each ordinal once, the message once.
                let calls = turns.flatMap { $0.tools.map(\.id) }
                #expect(Set(calls).count == calls.count, "a call twice at \(heard), \(held)")
                let ordinals = turns.compactMap(\.ordinal)
                #expect(Set(ordinals).count == ordinals.count, "an ordinal twice at \(heard), \(held)")
                #expect(turns.count { $0.role == .user } == 1)
                // In the rows' order.
                #expect(ordinals == ordinals.sorted(), "out of order at \(heard), \(held): \(ordinals)")
                // More rows never show less.
                let text = turns.reduce(0) { $0 + $1.content.count }
                #expect(text >= longest, "less text with more rows at \(heard), \(held)")
                longest = text
            }
            // With every row read, it is the rows' fold whatever was heard.
            #expect(said(TurnFold.fold(StoredTurns(transcript(of: Self.rows)), live).turns) == whole)
        }
    }

    @Test func rowsThenLateEventsOfTheSameTurnsChangeNothing() {
        // The rows were read first; a replay of their turns arrives after,
        // which nothing dropped on the way.
        var late = LiveRows()
        late.hear(LiveRowsTests.run)
        let stored = StoredTurns(transcript(of: Self.rows))
        let folded = TurnFold.fold(stored, late)
        #expect(said(folded.turns) == said(stored.turns))
        #expect(folded.left.isEmpty)
    }

    @Test func aRowWrittenByAnotherHandStandsWhereItsOrdinalPutsIt() {
        // A decision landed while reply 3 streams: its row took ordinal 4,
        // and is read before the reply's.
        let rows = [
            messageRow("m0", "user", "Go on", turn: 0), messageRow("m1", "assistant", "", turn: 1, calls: ["a"]),
            messageRow("m2", "tool", "{}", turn: 2, answers: "a"), messageRow("m4", "system", "decided", turn: 4),
        ]
        let live = live(sending: "Go on", [.toolStarted("a", turn: 2), .toolFinished("a", turn: 2)] + deltas("Almost", turn: 3))
        let folded = TurnFold.fold(StoredTurns(transcript(of: rows)), live)
        #expect(folded.turns.map(\.id) == ["sent-1", "call-a", "turn-3", "m4"])
        // The reply's row lands between them, where the stream's turn stood.
        let read = Array(rows.prefix(3)) + [messageRow("m3", "assistant", "Almost there", turn: 3), rows[3]]
        let done = TurnFold.fold(StoredTurns(transcript(of: read)), live)
        #expect(done.turns.map(\.id) == ["sent-1", "call-a", "turn-3", "m4"])
        #expect(done.turns[2].content == "Almost there")
    }

    @Test func aCallTheRowsHoldUnsettledTakesWhatTheStreamSaidOfIt() {
        // The reply's row is read with one call answered; the other still
        // runs, and its end arrives on the stream.
        let rows = [
            messageRow("m0", "user", "Go on", turn: 0), messageRow("m1", "assistant", "", turn: 1, calls: ["a", "b"]),
            messageRow("m2", "tool", "{}", turn: 2, answers: "a"),
        ]
        var live = LiveRows()
        live.hear([.toolStarted("b", turn: 3)])
        var folded = TurnFold.fold(StoredTurns(transcript(of: rows)), live)
        #expect(folded.turns.count == 2 && folded.turns[1].tools.map(\.running) == [false, true])
        #expect(folded.left.isEmpty)
        // And held, so a read that lets go of the rest keeps it.
        var aliases: [String: String] = [:]
        var kept = live
        kept.hear([.toolFinished("b", turn: 3, output: "found")])
        TurnFold.settle(rows, &kept, aliases: &aliases, ended: false)
        #expect(kept.turns.map { $0.tools.map(\.output) } == [["found"]])
        #expect(TurnFold.fold(StoredTurns(transcript(of: rows), aliases: aliases), kept).turns[1].tools.map(\.output) == ["{}", "found"])
        live.hear([.toolFinished("b", turn: 3, output: "found")])
        folded = TurnFold.fold(StoredTurns(transcript(of: rows)), live)
        #expect(folded.turns[1].tools.map(\.output) == ["{}", "found"] && folded.entries[1].stored == nil)
    }

    @Test func theMessageSentIsItsRowPastWhatWasHeldAndNeverAnOlderOneOfTheSameWords() {
        let older = [messageRow("m0", "user", "Yes", turn: 0), messageRow("m1", "assistant", "Good.", turn: 1)]
        let mine = messageRow("m2", "user", "Yes", turn: 2, createdAt: "2026-10-31T10:00:01Z")
        // The rows were held up to ordinal 1 when "Yes" was sent again.
        var live = live(sending: "Yes", baseline: 1, [])
        var folded = TurnFold.fold(StoredTurns(transcript(of: older)), live)
        #expect(folded.turns.map(\.id) == ["m0", "m1", "sent-1"])
        folded = TurnFold.fold(StoredTurns(transcript(of: older + [mine])), live)
        #expect(folded.turns.map(\.id) == ["m0", "m1", "sent-1"] && folded.claims == ["m2": "sent-1"])
        // With no rows held at the send, a row written since is it, and
        // one written long before is not.
        live = self.live(sending: "Yes", baseline: nil, [])
        let old = messageRow("m0", "user", "Yes", turn: 0, createdAt: "2026-10-30T09:00:00Z")
        #expect(TurnFold.fold(StoredTurns(transcript(of: [old])), live).turns.map(\.id) == ["m0", "sent-1"])
        #expect(TurnFold.fold(StoredTurns(transcript(of: [old, mine])), live).turns.map(\.id) == ["m0", "sent-1"])
    }

    /// Where the server takes a message's id (`agents` 3), the message is
    /// drawn under it from the press and its row has it: the two are paired
    /// by that id alone, whatever the words and wherever the row stands.
    @Test func aMessageSentUnderAnIdIsTheRowOfThatIdAndNoOther() {
        let id = "0190d6f0-0000-7000-8000-000000000001"
        var live = LiveRows()
        live.send("Yes", id: id, seq: 1, baseline: nil, at: Timestamp.parse("2026-10-31T10:00:00Z")!)
        #expect(live.turns.map(\.id) == [id] && live.sent.isEmpty)
        // An older row of the same words, written a moment ago, is not it.
        let twin = messageRow("m0", "user", "Yes", turn: 0, createdAt: "2026-10-31T09:59:59Z")
        var folded = TurnFold.fold(StoredTurns(transcript(of: [twin])), live)
        #expect(folded.turns.map(\.id) == ["m0", id] && folded.claims.isEmpty)
        // Its row is: drawn once, under the id both have, with no alias.
        let mine = messageRow(id, "user", "Yes", turn: 2, createdAt: "2026-10-31T10:00:01Z")
        folded = TurnFold.fold(StoredTurns(transcript(of: [twin, messageRow("m1", "assistant", "Good.", turn: 1), mine])), live)
        #expect(folded.turns.map(\.id) == ["m0", "m1", id] && folded.claims.isEmpty)
        #expect(folded.entries.last?.stored == 2 && folded.left.isEmpty)
        // And a conversation's own read lets go of the turn and keeps no
        // name for it.
        var aliases: [String: String] = [:]
        TurnFold.settle([twin, mine], &live, aliases: &aliases, ended: false)
        #expect(live.isEmpty && aliases.isEmpty)
        // The same message once more (Send Again) is the turn held.
        live.send("Yes", id: id, seq: 2, baseline: nil, at: Timestamp.parse("2026-10-31T10:00:05Z")!)
        live.confirm()
        live.send("Yes", id: id, seq: 3, baseline: nil, at: Timestamp.parse("2026-10-31T10:00:09Z")!)
        #expect(live.turns.map(\.id) == [id] && live.turns[0].pending)
    }

    @Test func rowsAConversationReadLetGoOfWhatTheyHoldAndKeepItsIds() {
        var live = live(LiveRowsTests.run)
        var aliases: [String: String] = [:]
        // An attach read the rows part way through the run.
        TurnFold.settle(Array(Self.rows.prefix(3)), &live, aliases: &aliases, ended: false)
        #expect(aliases == ["m0": "sent-1", "m1": "turn-1"])
        // What is left is what no row holds: the job's end the stream
        // said, the marker, the last reply.
        #expect(live.turns.map(\.id) == ["turn-1~", "summary-4", "turn-5"])
        #expect(live.turns[0].tools.map(\.id) == ["b"] && live.turns[0].content.isEmpty)
        #expect(live.text.keys.sorted() == ["turn-5"])
        let folded = TurnFold.fold(StoredTurns(transcript(of: Array(Self.rows.prefix(3))), aliases: aliases), live)
        #expect(folded.turns.map(\.id) == ["sent-1", "turn-1", "summary-4", "turn-5"])
        // The run ended and its rows are read: nothing provisional is left.
        TurnFold.settle(Self.rows, &live, aliases: &aliases, ended: true)
        #expect(live.isEmpty && !live.incomplete)
        #expect(StoredTurns(transcript(of: Self.rows), aliases: aliases).turns.map(\.id) == ["sent-1", "turn-1", "summary-4", "turn-5"])
    }

    @Test func aReplyCutShortSaysSoAtTheTurnTheStreamDrew() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        for cut in CutBy.allCases {
            // The reply streamed, and its row says how it was cut.
            let live = live(sending: "Tell me", deltas("Half an ans", turn: 1))
            let rows = [messageRow("m0", "user", "Tell me", turn: 0), messageRow("m1", "assistant", "Half an ans", turn: 1, cutBy: cut.rawValue)]
            let folded = TurnFold.fold(StoredTurns(transcript(of: rows)), live)
            #expect(folded.turns.map(\.id) == ["sent-1", "turn-1"] && folded.turns[1].cutBy == cut.rawValue)
            let presented = folded.turns.map { TurnPresenter.present($0, agent: nil, live: false, decided: [], titles: [:]) }
            let idle = ConversationLive(
                thread: "t1", agent: helper, name: "Helper", draft: "", draftRevision: 0, draftReplaced: "", draftBefore: "",
                provisional: .empty, aliases: folded.claims, incomplete: false, liveKey: nil, awaitingFirstEvent: false, connected: false,
                sends: 1, phase: .idle, stopping: false, failures: ConversationFailures(), deleted: false, listenOnOpen: false, blocked: nil,
                keyless: nil, online: true)
            let card = app.failures(ChatAppState(), live: idle, row: nil, all: folded.turns, turns: presented).turns["turn-1"]
            switch cut {
            case .length:
                #expect(card?.sentence == "The reply stopped at the model's limit." && card?.button?.action == .resume)
            case .filter:
                #expect(card?.button?.action == .resume)
            case .stop, .failure:
                // The thread's row says why its run ended: the card is the
                // run's, drawn from that.
                #expect(card == nil)
            }
        }
    }
}
