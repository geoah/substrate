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
}
