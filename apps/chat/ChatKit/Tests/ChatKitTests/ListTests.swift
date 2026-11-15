import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

extension MarkServer {
    /// `count` chats, `c001` the oldest: each settled a moment after the
    /// one before it.
    func chats(_ count: Int, read: Bool = true) {
        for n in 1 ... count {
            let id = String(format: "c%03d", n)
            chat(id, readThrough: read ? "\(id)-m2" : nil)
        }
    }
}

extension ChatRig {
    /// The chats the list draws, in order, and under which heading each.
    func listed() async -> [String] { await harness.presentation.list.sections.flatMap(\.rows).map(\.id) }

    func section(_ title: String) async -> [String] {
        await harness.presentation.list.sections.first { $0.title == title }?.rows.map(\.id) ?? []
    }

    /// Signed in to a server that keeps the flags, with its lists read.
    func signIn(flagged server: MarkServer) async throws {
        try await signIn(following: server)
        try await harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.flags && !state.refreshing && server.count("referencedBy") > 0
        }
    }

    /// Whatever the watch reported has been read again.
    func caughtUp() async throws {
        try await harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.feedBatch.isEmpty && state.feedApplying == nil && !state.refreshing && state.markSending.isEmpty && state.marking.isEmpty
        }
    }
}

@Suite("The list's window", .timeLimit(.minutes(10)))
struct ListWindow {
    @Test func theListGrowsAtItsEndWithTheServersCursorAndListsNoChatTwice() async throws {
        let server = MarkServer()
        server.chats(250)
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        #expect(await rig.listed() == Array(server.listed.prefix(100)))
        #expect(await rig.harness.presentation.list.more)
        // Reaching the end reads the next page, from the cursor.
        await rig.harness.send(.more(archived: false))
        try await rig.harness.until { harness in await harness.state.chats.count == 200 }
        #expect(await rig.listed() == Array(server.listed.prefix(200)))
        #expect(server.count("after=") == 1)
        await rig.harness.send(.more(archived: false))
        try await rig.harness.until { harness in await harness.state.chats.count == 250 }
        #expect(await rig.listed() == server.listed)
        // The list ended there: nothing more is offered, and asking reads nothing.
        #expect(await rig.harness.presentation.list.more == false)
        let mark = await rig.harness.effectsStarted
        await rig.harness.send(.more(archived: false))
        try await rig.harness.untilIdle(since: mark, advancing: 1)
        #expect(server.count("after=") == 2)
        // The folder keeps the newest window, as it did.
        #expect(await rig.harness.state.cached.threads.count == 100)
    }

    @Test func aChatThatMovesKeepsAGrownWindowWholeAndReadsItsFirstPageAlone() async throws {
        let server = MarkServer()
        server.chats(250)
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        await rig.harness.send(.more(archived: false))
        try await rig.harness.until { harness in await harness.state.chats.count == 200 }
        let pages = server.count("after=")
        // A chat far down the window says more: the feed tells, and the
        // list's first page is read again.
        server.settle("c060")
        try await rig.harness.until(advancing: 1) { harness in await harness.state.chats.first?.id == "c060" }
        try await rig.caughtUp()
        let listed = await rig.listed()
        #expect(listed == Array(server.listed.prefix(200)))
        #expect(Set(listed).count == 200 && server.count("after=") == pages)
        // And one past the window comes into it, once, at its head.
        server.settle("c003")
        try await rig.harness.until(advancing: 1) { harness in await harness.state.chats.first?.id == "c003" }
        try await rig.caughtUp()
        #expect(await rig.listed().filter { $0 == "c003" }.count == 1)
        #expect(await rig.listed().count == 201)
    }

    @Test func aPageAskedForBeforeTheOrderMovedLosesNoChatAndDoublesNone() async throws {
        let server = MarkServer()
        server.chats(180)
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        // The next page is on its way while a chat it would hold moves to
        // the list's head, and one already held moves too.
        server.hold("after=")
        await rig.harness.send(.more(archived: false))
        try await rig.harness.until { harness in await harness.state.window.reading }
        server.settle("c040")
        server.settle("c150")
        server.letGo("after=")
        try await rig.harness.until(advancing: 1) { harness in await harness.state.chats.count >= 180 }
        try await rig.caughtUp()
        let listed = await rig.listed()
        #expect(Set(listed).count == listed.count)
        #expect(listed == server.listed)
    }

    @Test func whereMoreChatsMovedThanAPageHoldsTheWindowStartsOverFromItsFirstPage() async throws {
        let server = MarkServer()
        server.chats(300)
        let rig = ChatRig(marks: server, platform: .ios)
        try await rig.signIn(following: server)
        await rig.harness.send(.more(archived: false))
        try await rig.harness.until { harness in await harness.state.chats.count == 200 }
        // Away from the front, where a phone hears nothing: 120 chats move,
        // some of them from past the window.
        await rig.harness.send(.phase(.inactive))
        await rig.harness.send(.phase(.background))
        for n in 60 ..< 180 { server.settle(String(format: "c%03d", n)) }
        await rig.harness.send(.phase(.active))
        try await rig.harness.until(advancing: 1) { harness in
            let state = await harness.state
            return !state.refreshing && state.chats.first?.id == "c179" && state.feedBatch.isEmpty && state.feedApplying == nil
        }
        // What lies between the page and the rows held was never read: the
        // window is the page, and grows again from its cursor with no hole.
        #expect(await rig.listed() == Array(server.listed.prefix(100)))
        await rig.harness.send(.more(archived: false))
        try await rig.harness.until { harness in await harness.state.chats.count == 200 }
        #expect(await rig.listed() == Array(server.listed.prefix(200)))
    }

    @Test func aRowIsMadeOnceAndAPageMakesItsOwnRowsAlone() async throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos, verifiesParts: false)
        var state = ChatAppState()
        state.loaded = true
        let now = Timestamp.parse("2026-11-14T10:00:30Z")!
        func thread(_ n: Int) -> Record {
            Record(
                id: "t\(n)", kind: Kinds.thread, properties: ["mode": "chat", "status": "ok", "subject": .string("Chat \(n)")], version: 1,
                createdAt: "2026-11-01T08:00:00Z", updatedAt: Timestamp.format(now.addingTimeInterval(Double(-n) * 3600)))
        }
        func present() -> [String] {
            app.present(PresenterInput(state: state, now: now, locale: Locale(identifier: "en_GB"))).list.sections.flatMap(\.rows).map(\.id)
        }
        state.chats = (1 ... 100).map(thread)
        #expect(present().count == 100 && app.kept.made("row/") == 100)
        // A page more: its rows are made, and none of the hundred above.
        state.chats += (101 ... 150).map(thread)
        #expect(present().count == 150 && app.kept.made("row/") == 150)
        // One chat moves: its row alone.
        state.chats[40].properties["subject"] = "Renamed"
        #expect(present().count == 150 && app.kept.made("row/") == 151)
    }
}

@Suite("A transcript's older rows", .timeLimit(.minutes(10)))
struct OlderRowsOfAChat {
    @Test func theRowsBeforeTheOnesHeldAreReadAboveThemAndStayThroughTheNextRead() async throws {
        let server = MarkServer()
        server.chat("big", messages: 700, readThrough: "big-m700")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let key = try await rig.open("big")
        try await rig.harness.until { harness in await harness.state.transcripts["big"]?.records.count == 500 }
        #expect(await rig.conversation(key)?.older == OlderRows(label: "Earlier messages", loading: false))
        let first = try #require(await rig.conversation(key)?.turns.first?.id)
        // Reaching the top reads the rows before the first one held, a
        // page of them, and says which turn stood first before they came.
        await rig.harness.send(.olderRows(thread: "big"))
        try await rig.harness.until { harness in await harness.state.transcripts["big"]?.records.count == 699 }
        #expect(await rig.conversation(key)?.above == RowsAbove(turn: first, count: 1))
        #expect(await rig.conversation(key)?.older != nil)
        let second = try #require(await rig.conversation(key)?.turns.first?.id)
        await rig.harness.send(.olderRows(thread: "big"))
        try await rig.harness.until { harness in await harness.state.transcripts["big"]?.records.count == 700 }
        let screen = try #require(await rig.conversation(key))
        // The transcript reached its first row, in order.
        #expect(screen.older == nil)
        #expect(screen.above == RowsAbove(turn: second, count: 2))
        #expect(await rig.harness.state.transcripts["big"]?.records.map(\.id) == (1 ... 700).map { "big-m\($0)" })
        #expect(screen.turns.count == 700)
        // A reply settles: the newest rows are read again, and the ones
        // read in above them stay.
        server.settle("big")
        try await rig.harness.until(advancing: 1) { harness in await harness.state.transcripts["big"]?.records.last?.id == "big-m701" }
        #expect(await rig.harness.state.transcripts["big"]?.records.count == 701)
        #expect(await rig.conversation(key)?.older == nil)
    }

    @Test func aReadOfTheOlderRowsIsOnePageAndSaysWhenMoreStand() async throws {
        let server = MarkServer()
        server.chat("big", messages: 900, readThrough: "big-m900")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let key = try await rig.open("big")
        try await rig.harness.until { harness in await harness.state.transcripts["big"]?.records.count == 500 }
        await rig.harness.send(.olderRows(thread: "big"))
        try await rig.harness.until { harness in await harness.state.transcripts["big"]?.records.count == 699 }
        #expect(await rig.conversation(key)?.older?.loading == false)
        // Asked twice at once, it is read once: the row at the ordinal
        // named comes again with each page, and is the one row twice read.
        let reads = server.count("lte")
        await rig.harness.send(.olderRows(thread: "big"))
        await rig.harness.send(.olderRows(thread: "big"))
        try await rig.harness.until { harness in await harness.state.transcripts["big"]?.records.count == 898 }
        #expect(server.count("lte") == reads + 1)
        #expect(await rig.conversation(key)?.older != nil)
        let ids = await rig.harness.state.transcripts["big"]?.records.map(\.id) ?? []
        #expect(ids == (3 ... 900).map { "big-m\($0)" })
    }
}

@Suite("Pinned and archived chats", .timeLimit(.minutes(10)))
struct PinsAndArchives {
    @Test func theListsAreReadByThePersonsMarksAndMergedByTheThread() async throws {
        let server = MarkServer(records: 9)
        server.chats(150)
        // An old chat pinned, one archived, one archived that waits on a
        // person, and one both pinned and archived.
        server.write("c002", pinned: true)
        server.write("c140", archived: true)
        server.write("c003", archived: true)
        server.wait("c003", on: 1)
        server.write("c141", pinned: true, archived: true)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        let listed = await rig.listed()
        // Pinned, however old, in a section of its own above the rest.
        #expect(await rig.harness.presentation.list.sections.first?.title == "Pinned")
        #expect(await rig.section("Pinned") == ["c002"])
        // Archived chats are out of the list: the one that waits is in it.
        #expect(!listed.contains("c140") && !listed.contains("c141"))
        #expect(listed.contains("c003"))
        let waits = await rig.row("c003")
        #expect(waits?.mark == .attention && waits?.archived == true)
        // Each chat once, in one section.
        #expect(Set(listed).count == listed.count)
        // The page is full of chats not archived: the filter is the server's.
        #expect(listed.count == 102)
        #expect(await rig.harness.presentation.list.flags)
    }

    @Test func pinAndUnpinReadSoAtOnceAndAreWrittenToTheMark() async throws {
        let server = MarkServer(records: 9)
        server.chats(5)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        #expect(await rig.section("Pinned").isEmpty)
        server.failWrites(1)
        await rig.harness.send(.pin(thread: "c002", pinned: true))
        // At once, whatever becomes of the write.
        #expect(await rig.section("Pinned") == ["c002"])
        #expect(await rig.row("c002")?.pinned == true)
        #expect(await rig.listed().filter { $0 == "c002" }.count == 1)
        // The write that failed is made again, and lands.
        await rig.harness.send(.refresh)
        try await rig.harness.until(advancing: 1) { _ in server.mark("c002")?.pinned == true }
        try await rig.caughtUp()
        #expect(await rig.section("Pinned") == ["c002"])
        // A pin moves no position and no other flag.
        #expect(server.mark("c002")?.readThrough == "c002-m2" && server.mark("c002")?.archived == nil)
        await rig.harness.send(.pin(thread: "c002", pinned: false))
        #expect(await rig.section("Pinned").isEmpty)
        try await rig.harness.until(advancing: 1) { _ in server.mark("c002")?.pinned == false }
    }

    @Test func archiveTakesTheRowAwayAtOnceAndUndoPutsBackExactlyWhatWasThere() async throws {
        let server = MarkServer(records: 9)
        server.chats(6, read: false)
        server.write("c004", pinned: true)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        let before = await rig.harness.presentation.list.sections
        #expect(await rig.harness.presentation.unread == 6)
        await rig.harness.send(.archive(thread: "c004", archived: true))
        // Gone at once, with no question asked, out of the count too.
        #expect(await rig.listed().contains("c004") == false)
        #expect(await rig.harness.presentation.unread == 5)
        #expect(await rig.harness.presentation.list.undo == UndoPresentation(thread: "c004", words: "Archived “Chat c004”", action: "Undo"))
        try await rig.harness.until(advancing: 1) { _ in server.mark("c004")?.archived == true }
        try await rig.caughtUp()
        // The lists read again by the feed leave it out: the server's own.
        #expect(await rig.listed().contains("c004") == false)
        // Undo: back where it was, pinned as it was, as it was.
        await rig.harness.send(.undoArchive)
        #expect(await rig.harness.presentation.list.sections == before)
        #expect(await rig.harness.presentation.list.undo == nil)
        try await rig.harness.until(advancing: 1) { _ in server.mark("c004")?.archived == false }
        try await rig.caughtUp()
        #expect(await rig.harness.presentation.list.sections == before)
        #expect(server.mark("c004")?.pinned == true)
    }

    @Test func undoStandsForAFewSecondsAndThenTheChatStaysArchived() async throws {
        let server = MarkServer(records: 9)
        server.chats(3)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        await rig.harness.send(.archive(thread: "c002", archived: true))
        #expect(await rig.harness.presentation.list.undo != nil)
        try await rig.harness.until(advancing: ChatApp.undoSeconds) { harness in await harness.presentation.list.undo == nil }
        // Pressed too late, Undo undoes nothing.
        await rig.harness.send(.undoArchive)
        try await rig.caughtUp()
        #expect(server.mark("c002")?.archived == true)
        #expect(await rig.listed() == ["c003", "c001"])
    }

    @Test func aChatThatComesToWaitOnAPersonIsListedCountedAndNotifiedThoughArchived() async throws {
        let server = MarkServer(records: 9)
        server.chats(140)
        server.write("c005", archived: true)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        #expect(await rig.listed().contains("c005") == false)
        // Its run comes to a question: the feed tells of the thread, and
        // the list of what waits holds it, old and archived as it is.
        server.wait("c005", on: 1)
        try await rig.harness.until(advancing: 1) { harness in await harness.presentation.list.sections.flatMap(\.rows).contains { $0.id == "c005" } }
        #expect(await rig.row("c005")?.mark == .attention)
        // Answered: it leaves the list again, and its archive stood.
        server.wait("c005", on: 0)
        try await rig.harness.until(advancing: 1) { harness in
            await harness.presentation.list.sections.flatMap(\.rows).contains { $0.id == "c005" } == false
        }
        #expect(server.mark("c005")?.archived == true)
    }

    @Test func aReplyInAnArchivedChatNotifiesNobodyAndCountsNowhere() async throws {
        let server = MarkServer(records: 9)
        server.chats(4)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        #expect(await rig.harness.presentation.unread == 0)
        // Archived here, its row is still held: no list the server answers
        // holds it now, so it is read by name when the feed tells of it.
        await rig.harness.send(.archive(thread: "c002", archived: true))
        try await rig.harness.until(advancing: 1) { _ in server.mark("c002")?.archived == true }
        try await rig.caughtUp()
        server.settle("c002")
        server.settle("c003")
        try await rig.harness.until(advancing: 1) { _ in rig.scheduled.contains { $0.thread == "c003" } }
        try await rig.caughtUp()
        let state = await rig.harness.state
        // The app heard of the reply, and said nothing of it.
        #expect(state.thread("c002")?.lastMessage == "c002-m3")
        #expect(rig.scheduled.contains { $0.thread == "c002" } == false)
        #expect(await rig.harness.presentation.unread == 1)
        #expect(rig.badge == 1)
        // The one test of it, for whatever posts a notification of a thread.
        #expect(state.notifies(try #require(state.thread("c002"))) == false)
        #expect(state.notifies(try #require(state.thread("c003"))))
    }

    @Test func archivingAChatTakesBackWhatItNotified() async throws {
        let server = MarkServer(records: 9)
        server.chats(3)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        server.settle("c002")
        try await rig.harness.until(advancing: 1) { _ in rig.scheduled.contains { $0.thread == "c002" } }
        await rig.harness.send(.archive(thread: "c002", archived: true))
        try await rig.harness.until(advancing: 1) { _ in rig.notifications.calls.contains { if case .cancelThread("c002") = $0 { true } else { false } } }
        #expect(rig.badge == 0)
    }

    @Test func speakingInAnArchivedChatEndsItsArchiveAndNeverHoldsTheMessage() async throws {
        let server = MarkServer(records: 9)
        server.chats(3)
        server.write("c002", archived: true)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        await rig.harness.send(.showArchived(scene: "s", shown: true))
        try await rig.harness.until { harness in await harness.presentation.list.archived?.rows.map(\.id) == ["c002"] }
        let key = try await rig.open("c002")
        try await rig.send(.draft("And the tomatoes?"), to: key)
        try await rig.send(.send, to: key)
        // The message left with no wait for the mark: this server has no
        // chat route, and was asked for it all the same.
        try await rig.harness.until(advancing: 1) { _ in server.count("/chat") > 0 && server.mark("c002")?.archived == false }
        #expect(server.writes.contains { $0["archived"] as? Bool == false && $0["thread"] as? String == "c002" })
        try await rig.caughtUp()
        #expect(await rig.listed().contains("c002"))
    }

    @Test func theArchivedChatsAreReadWhenAWindowListsThemAndGrowAtTheirEnd() async throws {
        let server = MarkServer(records: 9)
        server.chats(140)
        for n in 1 ... 130 { server.write(String(format: "c%03d", n), archived: true) }
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        #expect(await rig.listed().count == 10)
        // Offered by the server's revision, read only when shown.
        #expect(await rig.harness.presentation.list.archived?.rows.isEmpty == true)
        #expect(await rig.harness.presentation.scenes["s"]?.archived != true)
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: nil))
        await rig.harness.send(.showArchived(scene: "s", shown: true))
        try await rig.harness.until { harness in await harness.presentation.list.archived?.rows.count == 100 }
        #expect(await rig.harness.presentation.scenes["s"]?.archived == true)
        #expect(await rig.harness.presentation.list.archived?.more == true)
        await rig.harness.send(.more(archived: true))
        try await rig.harness.until { harness in await harness.presentation.list.archived?.rows.count == 130 }
        #expect(await rig.harness.presentation.list.archived?.more == false)
        let archived = await rig.harness.presentation.list.archived?.rows.map(\.id) ?? []
        #expect(Set(archived).count == 130 && archived.first == "c130")
        // Unarchive: out of that list at once, and back among the chats
        // where its place is inside the window.
        await rig.harness.send(.archive(thread: "c130", archived: false))
        #expect(await rig.harness.presentation.list.archived?.rows.contains { $0.id == "c130" } == false)
        #expect(await rig.listed().contains("c130"))
        try await rig.harness.until(advancing: 1) { _ in server.mark("c130")?.archived == false }
        try await rig.caughtUp()
        #expect(await rig.listed() == Array(server.listed.filter { server.mark($0)?.archived != true }.prefix(100)))
    }

    @Test func aChatArchivedOnAnotherDeviceLeavesTheListAndOneUnarchivedComesBack() async throws {
        let server = MarkServer(records: 9)
        server.chats(160)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        // The window was first read as every chat, before the server's
        // document said it keeps the flags: it grows by the cursor of the
        // list it is read as now, which is the one the server takes back.
        await rig.harness.send(.more(archived: false))
        try await rig.harness.until { harness in await harness.state.chats.count == 160 }
        #expect(await rig.harness.presentation.list.failure == nil)
        // One on the first page, and one past it, where no list read
        // reaches: its mark is read by name.
        server.write("c150", archived: true)
        server.write("c010", archived: true)
        try await rig.harness.until(advancing: 1) { harness in
            let listed = await harness.presentation.list.sections.flatMap(\.rows).map(\.id)
            return !listed.contains("c150") && !listed.contains("c010")
        }
        server.write("c010", archived: false)
        try await rig.harness.until(advancing: 1) { harness in await harness.presentation.list.sections.flatMap(\.rows).contains { $0.id == "c010" } }
        #expect(await rig.listed().filter { $0 == "c010" }.count == 1)
    }

    @Test func withEveryChatArchivedTheListIsEmptyAndStillHearsOfOneThatComesBack() async throws {
        let server = MarkServer(records: 9)
        server.chats(2)
        server.write("c001", archived: true)
        server.write("c002", archived: true)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        #expect(await rig.listed().isEmpty)
        #expect(await rig.harness.presentation.list.archived != nil)
        // The watch follows the marks with no chat listed at all.
        #expect(server.count("/api/v1/changes", and: Kinds.threadMark) > 0)
        server.write("c001", archived: false)
        try await rig.harness.until(advancing: 1) { harness in await harness.presentation.list.sections.flatMap(\.rows).map(\.id) == ["c001"] }
    }

    @Test func aWriteThatRacesTheListReadsNeverBringsTheRowBack() async throws {
        let server = MarkServer(records: 9)
        server.chats(4)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        // The archive's write fails, and the lists are read while the
        // server still holds the chat as one of the chats, mark and all.
        server.failWrites(1)
        await rig.harness.send(.archive(thread: "c003", archived: true))
        try await rig.harness.until { _ in server.writes.count == 1 }
        server.settle("c001")
        try await rig.harness.until(advancing: 1) { harness in await harness.state.chats.first?.id == "c001" }
        #expect(await rig.harness.state.chats.contains { $0.id == "c003" })
        #expect(await rig.listed().contains("c003") == false)
        // The next chance writes it, and the lists agree.
        await rig.harness.send(.refresh)
        try await rig.harness.until(advancing: 1) { _ in server.mark("c003")?.archived == true }
        try await rig.caughtUp()
        #expect(await rig.listed() == ["c001", "c004", "c002"])
    }

    @Test func aFlagTheServerRefusesForGoodIsSaidAndTheChatReadsAsTheServerSays() async throws {
        let server = MarkServer(records: 9)
        server.chats(3)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        server.refuseWrites(422)
        await rig.harness.send(.archive(thread: "c002", archived: true))
        try await rig.harness.until(advancing: 1) { harness in await harness.presentation.list.failure != nil }
        let strip = try #require(await rig.harness.presentation.list.failure)
        #expect(strip.sentence == "Couldn't save that change to the chat.")
        #expect(strip.button == FailureButton(title: "Try Again", action: .refresh))
        #expect(await rig.listed().contains("c002"))
        #expect(await rig.harness.presentation.list.undo == nil)
        // Try Again writes it again.
        server.refuseWrites(nil)
        await rig.harness.send(.refresh)
        try await rig.harness.until(advancing: 1) { _ in server.mark("c002")?.archived == true }
        try await rig.caughtUp()
        #expect(await rig.harness.presentation.list.failure == nil)
        #expect(await rig.listed() == ["c003", "c001"])
    }

    @Test func untilAMarkIsReadTheListTheServerAnsweredAChatInSaysWhichItIs() {
        var state = ChatAppState()
        state.recordsRevision = 9
        func thread(_ id: String, minutes: Int) -> Record {
            Record(
                id: id, kind: Kinds.thread, properties: ["mode": "chat", "status": "ok"], version: 1,
                createdAt: "2026-11-01T08:00:00Z", updatedAt: String(format: "2026-11-14T10:%02d:00Z", minutes))
        }
        // No mark read: a chat is in no dictionary of them.
        state.marks = [:]
        state.chats = [thread("kept", minutes: 30)]
        state.pinned = [thread("old-pin", minutes: 1)]
        state.archive.rows = [thread("shelved", minutes: 20)]
        state.window.read = true
        #expect(state.threads.map(\.id) == ["kept", "old-pin"])
        #expect(state.archivedThreads.map(\.id) == ["shelved"])
        #expect(state.isPinned("old-pin") && state.archived("shelved") && !state.archived("kept"))
        // The marks, once read, say it: a pinned chat its person archived
        // is with the archived ones, and keeps its pin.
        state.marks = ["old-pin": ThreadMark(version: 1, pinned: true, archived: true), "kept": .none, "shelved": ThreadMark(version: 1, archived: true)]
        #expect(state.threads.map(\.id) == ["kept"])
        #expect(Set(state.archivedThreads.map(\.id)) == ["shelved", "old-pin"])
        #expect(state.isPinned("old-pin"))
        // A mark with no position is no sign the chat was opened: unread still.
        state.chats[0].properties["lastMessage"] = ["ref": .string("\(Kinds.message)/m2")]
        state.chats[0].properties["finishedAt"] = "2026-11-14T10:30:00Z"
        state.marks?["kept"] = ThreadMark(version: 1, pinned: true)
        #expect(state.unread(state.threads[0]) && state.newFrom("kept") == .start)
    }
}

@Suite("A server that keeps no flags", .timeLimit(.minutes(10)))
struct ListWithoutFlags {
    @Test func belowRevisionNineEveryChatIsListedAndNeitherActionIsOfferedOrSent() async throws {
        let server = MarkServer(records: 8)
        server.chats(4)
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let list = await rig.harness.presentation.list
        #expect(list.flags == false && list.archived == nil)
        #expect(await rig.listed() == ["c004", "c003", "c002", "c001"])
        // Not offered, and where something asks all the same, nothing is sent.
        let mark = await rig.harness.effectsStarted
        await rig.harness.send(.pin(thread: "c002", pinned: true))
        await rig.harness.send(.archive(thread: "c003", archived: true))
        await rig.harness.send(.showArchived(scene: "s", shown: true))
        try await rig.harness.untilIdle(since: mark, advancing: 1)
        #expect(server.writes.isEmpty && server.count("referencedBy") == 0 && server.count("waitingOnPerson") == 0)
        #expect(await rig.listed().count == 4)
        #expect(await rig.harness.presentation.list.undo == nil)
        #expect(await rig.harness.presentation.scenes["s"]?.archived != true)
    }

    @Test func aServerThatStopsKeepingTheFlagsIsReadAsOneThatNeverDid() async throws {
        let server = MarkServer(records: 9)
        server.chats(4)
        server.write("c002", archived: true)
        server.write("c003", pinned: true)
        let base = temporaryFolder("chat")
        let rig = ChatRig(marks: server, base: base)
        try await rig.signIn(flagged: server)
        #expect(await rig.listed() == ["c003", "c004", "c001"])
        try await rig.harness.until(advancing: 1) { _ in FileManager.default.fileExists(atPath: rig.root.appendingPathComponent("state.json").path) }
        // Rolled back: the next launch asks for the lists the folder says
        // the server had, is refused the arm, and lists every chat.
        server.become(records: 8)
        let again = ChatRig(marks: server, base: base)
        try await again.signIn(following: server)
        try await again.harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.recordsRevision == 8 && !state.refreshing
        }
        #expect(await again.listed() == ["c004", "c003", "c002", "c001"])
        #expect(await again.harness.presentation.list.flags == false)
        #expect(await again.harness.presentation.list.sections.first?.title != "Pinned")
    }
}

@Suite("Searching the chats", .timeLimit(.minutes(10)))
struct ListSearch {
    private func server() -> MarkServer {
        let server = MarkServer(records: 9)
        server.chats(120)
        server.settle("c050", reply: "The balcony tomatoes want water every second day in August.")
        server.settle("c004", reply: "Tomatoes dislike wet leaves: water them at the base.")
        server.write("c004", archived: true)
        return server
    }

    @Test func theTitlesMatchAtOnceAndTheServersMessagesComeUnderThemByChat() async throws {
        let server = server()
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: nil))
        await rig.harness.send(.search(scene: "s", query: "c05"))
        // At once, from what the device holds: the titles.
        #expect(Set(await rig.harness.presentation.scenes["s"]?.found?.flatMap(\.rows).map(\.id) ?? []) == Set((50 ... 59).map { "c0\($0)" }))
        #expect(server.count("q=") == 0)
        await rig.harness.send(.search(scene: "s", query: "tomatoes"))
        #expect(await rig.harness.presentation.scenes["s"]?.searching == true)
        // After a pause the server is asked, once for what was typed last.
        try await rig.harness.until(advancing: 1) { harness in await harness.presentation.scenes["s"]?.messages?.count == 2 }
        #expect(server.count("q=") == 1 && server.count("mode=lexical") == 1)
        let scene = try #require(await rig.harness.presentation.scenes["s"])
        let groups = try #require(scene.messages)
        #expect(scene.searching == false && scene.nothingFound == nil)
        // By chat, with the words that matched; the archived one says so,
        // and is found though no list holds it.
        #expect(Set(groups.map(\.id)) == ["c050", "c004"])
        let listed = try #require(groups.first { $0.id == "c050" })
        let shelved = try #require(groups.first { $0.id == "c004" })
        #expect(listed.title == "Chat c050" && listed.archived == nil)
        #expect(shelved.title == "Chat c004" && shelved.archived == "Archived")
        #expect(listed.hits.first?.runs.first { $0.match }?.text == "tomatoes")
        #expect(shelved.hits.first?.runs.first { $0.match }?.text == "Tomatoes")
        #expect(listed.hits.first?.who == "Helper" && listed.hits.first?.id == "c050-m3")
    }

    @Test func aSearchThatFailsSaysSoInTheListsStripAndKeepsTheTitles() async throws {
        let server = server()
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: nil))
        server.failSearches(1)
        await rig.harness.send(.search(scene: "s", query: "c05"))
        try await rig.harness.until(advancing: 1) { harness in await harness.presentation.list.failure != nil }
        let strip = try #require(await rig.harness.presentation.list.failure)
        #expect(strip.sentence == "Couldn't search your messages. Chats are found by their titles.")
        #expect(await rig.harness.presentation.scenes["s"]?.found?.flatMap(\.rows).count == 10)
        // Try Again searches again.
        await rig.harness.send(.refresh)
        try await rig.harness.until(advancing: 1) { _ in server.count("q=") == 2 }
        try await rig.harness.until(advancing: 1) { harness in
            let shown = await harness.presentation
            return shown.list.failure == nil && shown.scenes["s"]?.searching == false
        }
        // A search cleared leaves nothing behind.
        await rig.harness.send(.search(scene: "s", query: ""))
        #expect(await rig.harness.presentation.scenes["s"]?.messages == nil)
    }

    @Test func aServerThatTakesNoSearchFromThisSignInLeavesTheDevicesOwn() async throws {
        let server = server()
        server.refuseSearch()
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: nil))
        await rig.harness.send(.search(scene: "s", query: "zzz"))
        try await rig.harness.until(advancing: 1) { harness in await harness.state.searchUnserved }
        let scene = try #require(await rig.harness.presentation.scenes["s"])
        #expect(scene.nothingFound == "No chat on this device matches “zzz”." && scene.messages == nil)
        #expect(await rig.harness.presentation.list.failure == nil)
        // And it is not asked again.
        let mark = await rig.harness.effectsStarted
        await rig.harness.send(.search(scene: "s", query: "tomatoes"))
        try await rig.harness.untilIdle(since: mark, advancing: 1)
        #expect(server.count("q=") == 1)
    }

    @Test func aHitOpensItsChatAtThatMessageAndReadsTheRowsBeforeTheOnesHeldToReachIt() async throws {
        let server = MarkServer(records: 9)
        server.chat("big", messages: 700, readThrough: "big-m700")
        server.older("big", messages: 1)
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: nil))
        // A message among the rows held.
        await rig.harness.send(.open(thread: "big", message: "big-m650", scene: "s"))
        let key = try #require(await rig.harness.state.scenes["s"]?.current)
        try await rig.harness.until { harness in await harness.presentation.conversations[key]?.opens == .turn("big-m650") }
        #expect(await rig.conversation(key)?.visit == 1)
        // One far above them: the rows before are read until they hold it.
        await rig.harness.send(.open(thread: "big", message: "big-m100", scene: "s"))
        try await rig.harness.until { harness in await harness.presentation.conversations[key]?.opens == .turn("big-m100") }
        #expect(await rig.conversation(key)?.visit == 2)
        #expect(await rig.harness.state.seeking.isEmpty)
        // As far as the message, and no further.
        let held = await rig.harness.state.transcripts["big"]?.records.map(\.id) ?? []
        #expect(held.contains("big-m100") && !held.contains("big-o0"))
    }

    @Test func aHitIsCutToTheWordsAroundTheFirstThatMatchedWithEachMarked() {
        let text = "We spoke about the garden for a long while before anything else.\nThe balcony tomatoes want water, and so do the tomato seedlings."
        let runs = SearchSnippet.runs(of: text, words: ["tomato"])
        #expect(runs.first == SearchRun(text: "…", match: false))
        #expect(runs.filter(\.match).map(\.text) == ["tomatoes", "tomato"])
        #expect(runs.map(\.text).joined().contains("balcony tomatoes want water"))
        #expect(!runs.map(\.text).joined().contains("\n"))
        // In any case, and never inside another word.
        #expect(SearchSnippet.runs(of: "Automatons and TOMATOES", words: ["tomatoes"]).filter(\.match).map(\.text) == ["TOMATOES"])
        // A word in another form of the one searched is marked whole.
        #expect(SearchSnippet.runs(of: "One tomato, two.", words: ["tomatoes"]).filter(\.match).map(\.text) == ["tomato"])
        #expect(SearchSnippet.runs(of: "She was planting late.", words: ["plants"]).filter(\.match).map(\.text) == ["planting"])
        // One the app cannot see the form of marks nothing: the message's start.
        #expect(SearchSnippet.runs(of: "It ran well.", words: ["running"]) == [SearchRun(text: "It ran well.", match: false)])
        #expect(ChatAppState.searchWords("\"balcony plants\" -lunch lay* OR tomato") == ["balcony", "plants", "lunch", "lay", "tomato"])
    }
}

@Suite("The first screen", .timeLimit(.minutes(10)))
struct FirstScreenOfAWindow {
    private func rig(running: String? = nil) async throws -> (ChatRig, MarkServer) {
        let server = MarkServer(records: 9)
        server.chats(5)
        if let running { server.run(running) }
        let rig = ChatRig(marks: server)
        try await rig.signIn(flagged: server)
        return (rig, server)
    }

    @Test func aWindowOpensToTheChatItWasLeftInLessThanHalfAnHourAgo() async throws {
        let (rig, _) = try await rig(running: "c004")
        let left = rig.clock.now.addingTimeInterval(-20 * 60)
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: "c002", left: left, lands: true))
        #expect(await rig.harness.presentation.scenes["s"]?.thread == "c002")
    }

    @Test func leftLongerAgoItOpensToTheChatThatIsRunning() async throws {
        let (rig, _) = try await rig(running: "c004")
        let left = rig.clock.now.addingTimeInterval(-31 * 60)
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: "c002", left: left, lands: true))
        #expect(await rig.harness.presentation.scenes["s"]?.thread == "c004")
    }

    @Test func withNeitherItOpensToANewChat() async throws {
        let (rig, _) = try await rig()
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: "c002", left: nil, lands: true))
        let scene = try #require(await rig.harness.presentation.scenes["s"])
        let key = try #require(scene.current)
        #expect(scene.thread == nil)
        // With the assistant a new chat goes to, and nothing to wait for.
        let screen = await rig.conversation(key)
        #expect(screen?.agent == helper && screen?.hero != nil)
    }

    @Test func aRouteTheWindowTakesWinsOverAllThree() async throws {
        let (rig, _) = try await rig(running: "c004")
        await rig.harness.send(.route(.thread("c001")))
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: "c002", left: rig.clock.now, lands: true))
        #expect(await rig.harness.presentation.scenes["s"]?.current == nil)
        await rig.harness.send(.claimRoute(scene: "s"))
        #expect(await rig.harness.presentation.scenes["s"]?.thread == "c001")
    }

    @Test func aWindowThatDrewBeforeTheChatsWereHeldOpensOnceTheyAre() async throws {
        let server = MarkServer(records: 9)
        server.chats(3)
        server.run("c002")
        let rig = ChatRig(marks: server)
        // The window draws as soon as the session is there, before the
        // folder is read or the server has answered anything.
        await rig.runtime.linkChanged(LinkSnapshot(phase: .signedIn, server: origin, repository: repository, scopes: nil, online: true))
        await rig.harness.send(.sceneAppeared(scene: "s", restoring: nil, left: nil, lands: true))
        try await rig.signIn(flagged: server)
        #expect(await rig.harness.presentation.scenes["s"]?.thread == "c002")
    }
}
