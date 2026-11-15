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

    /// The fixture's server answers a transcript's read as a server does:
    /// the newest rows it asked for, and the rows before the ones held
    /// where it names an ordinal, so the app's older rows can be drawn on
    /// a device with no server.
    @Test func aTranscriptIsAnsweredByTheWindowItsReadAskedFor() throws {
        func ordinals(_ page: String) throws -> [Int] {
            try JSONDecoder().decode(RecordPage.self, from: Data(page.utf8)).records.compactMap { $0["turn"]?.int }
        }
        // Whole, as before.
        #expect(try ordinals(ChatFixtures.transcript("t", rows: 6)) == [5, 4, 3, 2, 1, 0])
        // The newest 500 of 1,100, newest first.
        let newest = try ordinals(ChatFixtures.transcript(ChatFixtures.deep, rows: 1100, first: 500))
        #expect(newest.first == 1099 && newest.last == 600 && newest.count == 500)
        // The 200 at or before the first one held: it comes again, and the
        // app drops it by its id.
        let older = try ordinals(ChatFixtures.transcript(ChatFixtures.deep, rows: 1100, first: 200, through: 600))
        #expect(older.first == 600 && older.last == 401 && older.count == 200)
        // At the thread's start the read is short, which says there is no more.
        #expect(try ordinals(ChatFixtures.transcript(ChatFixtures.deep, rows: 1100, first: 200, through: 3)) == [3, 2, 1, 0])
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

    @Test func aCardsFixtureIsRowsThatAskAndARecordThatWaitsThenEnded() throws {
        for (asked, kind) in [(ChatFixtures.question(), Kinds.interaction), (ChatFixtures.approval(), Kinds.approval)] {
            let record = { (json: String) in try ValueCoding.decode(Record.self, from: try #require(JSONValue.parse(json))) }
            let pending = try record(asked.pending)
            let resolved = try record(asked.resolved)
            #expect(pending.kind == kind && pending.id == asked.card.id && asked.path == "\(kind)/\(pending.id)")
            #expect(ChatApp.waits(pending) && !ChatApp.waits(resolved) && resolved.version > pending.version)
            // The rows hold the call that asked, stamped with the record
            // it landed; once it ended, the line that says so follows.
            let waiting = transcript(of: try records(asked.rows).reversed())
            let call = try #require(waiting.last?.tools.first)
            #expect((interactionID(of: call) ?? approvalID(of: call)) == pending.id)
            let ended = transcript(of: try records(asked.rowsResolved).reversed())
            #expect(ended.contains { interactionNotice($0)?.interactionID == pending.id || approvalNotice($0)?.approvalID == pending.id })
            #expect(ended.last?.role == .assistant)
        }
        // And the notifications made of them take an answer.
        let made = ChatFixtures.notifications()
        #expect(
            made.requests.map(\.category) == [
                ChatNotifications.replyCategory, ChatNotifications.questionCategory(ChatFixtures.question().card.id), ChatNotifications.approvalCategory,
            ])
        #expect(made.categories.last?.actions.map(\.title) == ["Tuesday at 9:00", "Wednesday at 8:30", "Thursday at 10:15"])
        #expect(made.requests.last?.body?.contains("To: sam@example.com") == true)
    }

    @Test func aRunAMessageJoinsFoldsWithTheMessageWhereItsRowStands() throws {
        for stopped in [false, true] {
            let run = ChatFixtures.queued(thread: "t-live", stopped: stopped, id: "q1")
            let events = try run.lines.map { try #require(AgentEvent.parse($0), "unread: \($0)") }
            #expect(events[run.joins].kind == .messageQueued && events[run.joins].id == "q1")
            #expect(events.contains { $0.kind == .messageTaken } == !stopped)
            var live = LiveRows()
            live.send(run.message, seq: 1, baseline: -1, at: Date())
            live.hear(Array(events.dropFirst().dropLast()))
            #expect(live.turns.map(\.id) == (stopped ? ["sent-1", "turn-1", "q1"] : ["sent-1", "turn-1", "q1", "turn-4"]))
            let last = try #require(run.rows.last)
            let folded = TurnFold.fold(StoredTurns(transcript(of: try records(last.page).reversed())), live)
            #expect(folded.turns.map(\.role) == (stopped ? [.user, .assistant, .user] : [.user, .assistant, .user, .assistant]))
            #expect(folded.turns[2].content == run.joining && folded.turns[2].id == "q1")
        }
    }
}
