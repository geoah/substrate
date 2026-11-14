import Foundation
import SubstrateAPI
import SubstrateCore
import Testing

@testable import ChatKit

/// What a measurement streams into and a gallery draws: the fixtures read
/// as the server's answers do, and say what their names say.
@Suite struct FixtureTests {
    private func records(_ page: String) throws -> [Record] {
        try ValueCoding.decode(RecordPage.self, from: try #require(JSONValue.parse(page))).records
    }

    @Test func theListIsAHundredSettledChatsNewestFirst() throws {
        let threads = try records(ChatFixtures.threads()).map(ChatThread.init(record:))
        #expect(threads.count == 100)
        #expect(threads.prefix(2).map(\.id) == [ChatFixtures.long, ChatFixtures.full])
        #expect(threads.allSatisfy { !$0.running && $0.agentID == ChatFixtures.agent && $0.subject != nil })
        #expect(threads.map(\.activeAt) == threads.map(\.activeAt).sorted(by: >))
        // A read of the one thread says what the list said.
        let one = try #require(JSONValue.parse(ChatFixtures.thread(ChatFixtures.long)))
        #expect(ChatThread(record: try ValueCoding.decode(Record.self, from: one)).subject == threads[0].subject)
    }

    @Test func aTranscriptFoldsIntoItsTurnsAndATurnAnsweredStandsAtItsEnd() throws {
        let rows = try records(ChatFixtures.transcript(ChatFixtures.long, rows: 200))
        let turns = transcript(of: rows.reversed())
        #expect(turns.count == 200)
        #expect(turns.first?.role == .user && turns.last?.role == .assistant)
        #expect(newestTurn(rows) == 199)
        let after = try records(ChatFixtures.transcript(ChatFixtures.long, rows: 200, message: "Hi", reply: ChatFixtures.reply))
        let folded = transcript(of: after.reversed())
        #expect(folded.count == 202)
        #expect(folded.suffix(2).map(\.content) == ["Hi", ChatFixtures.reply])
        #expect(try records(ChatFixtures.transcript("t-new", rows: 0, message: "Hi", reply: "Hello")).count == 2)
    }

    @Test func theLongReplyIsSixteenKilobytesOfEveryBlock() {
        let reply = ChatFixtures.reply(bytes: 16 * 1024)
        #expect(reply.utf8.count >= 16 * 1024)
        #expect(reply.hasPrefix(ChatFixtures.reply))
        var kinds = Set<String>()
        for block in Markdown.parse(ChatFixtures.reply) {
            switch block {
            case .paragraph: kinds.insert("paragraph")
            case .heading: kinds.insert("heading")
            case .code: kinds.insert("code")
            case .quote: kinds.insert("quote")
            case .list: kinds.insert("list")
            case .table: kinds.insert("table")
            case .rule: kinds.insert("rule")
            }
        }
        #expect(kinds == ["paragraph", "heading", "code", "quote", "list", "table", "rule"])
    }

    @Test func aStreamNamesItsThreadWritesTheTextInPiecesAndSettles() {
        let events = ChatFixtures.stream(thread: "t1", text: ChatFixtures.reply, turn: 3, piece: 7).compactMap(AgentEvent.parse)
        #expect(events.first?.kind == .thread && events.first?.thread == "t1")
        #expect(events.last?.kind == .done && events.last?.result?.status == "ok")
        let deltas = events.filter { $0.kind == .delta }
        #expect(deltas.allSatisfy { $0.turn == 3 })
        #expect(deltas.compactMap(\.text).joined() == ChatFixtures.reply)
    }

    @Test func aRunWrittenLineByLineFoldsToTheRowsItLeaves() throws {
        let run = ChatFixtures.run(thread: "t-live")
        // Every line reads, and the pieces of each text stand end to end.
        let events = try run.lines.map { try #require(AgentEvent.parse($0), "unread: \($0)") }
        #expect(events.first?.kind == .thread && events.last?.kind == .done)
        var live = LiveRows()
        live.send(run.message, seq: 1, baseline: -1, at: Date())
        live.hear(Array(events.dropFirst().dropLast()))
        #expect(live.turns.map(\.id) == ["sent-1", "turn-1", "turn-3"] && !live.incomplete)
        #expect(live.turns[1].reasoning == run.thinking && live.turns[1].content == run.first && live.turns[2].content == run.second)
        // Every page reads, each holding the one before, and the last is
        // the stream's turns as rows.
        var held = 0
        for (_, page) in run.rows {
            let rows = try records(page)
            #expect(rows.count > held)
            held = rows.count
        }
        let last = try #require(run.rows.last)
        let folded = TurnFold.fold(StoredTurns(transcript(of: try records(last.page).reversed())), live)
        #expect(folded.left.isEmpty && folded.turns.map(\.id) == ["sent-1", "turn-1", "turn-3"])
        #expect(folded.turns.map(\.content) == [run.message, run.first, run.second])
        #expect(folded.turns[1].tools.map(\.ok) == [true] && folded.turns[1].reasoning == run.thinking)
    }
}
