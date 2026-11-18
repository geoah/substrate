import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime
import SubstrateTesting
import Testing

@testable import ChatKit

extension ChatRig {
    /// A device signed in to a server several share.
    init(marks server: MarkServer, platform: ChatPlatform = .macos, base: URL = temporaryFolder("chat"), push: PushRegistrar? = nil) {
        self.init([], platform: platform, base: base, transport: server, push: push)
    }

    /// Signed in to a server that keeps marks: the list and the person's
    /// marks read, and the watch following them.
    func signIn(following server: MarkServer) async throws {
        try await signIn()
        try await harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.marks != nil && state.marksKnown && server.count("/api/v1/changes", and: Kinds.threadMark) > 0
                && state.feedBatch.isEmpty && state.feedApplying == nil
        }
    }

    func row(_ id: String) async -> ThreadRow? {
        await harness.presentation.list.sections.flatMap(\.rows).first { $0.id == id }
    }

    /// The chat shown in a window, once the rows drawn hold its newest
    /// message.
    func open(_ thread: String, scene: String = "s") async throws -> ChildKey {
        await harness.send(.show(thread: thread, scene: scene))
        let key = try #require(await harness.state.scenes[scene]?.current)
        try await harness.until { harness in
            let state = await harness.state
            guard let rows = state.transcripts[thread]?.records else { return false }
            return state.thread(thread)?.lastMessage.map { last in rows.contains { $0.id == last } } ?? !rows.isEmpty
        }
        return key
    }

    /// The window's view says whether somebody looking at it sees the end.
    func look(_ key: ChildKey, scene: String = "s", _ shown: Bool = true) async {
        await harness.send(.end(scene: scene, key: key, shown: shown))
    }

    /// Something the app started sleeps until `date` on its clock. Only
    /// where nothing else can be waiting for the same instant: a test moves
    /// the clock past what was waiting before it starts what it times.
    func sleeping(until date: Date) async throws {
        try await harness.until { _ in clock.deadlines.contains(date) }
    }

    /// The end of the chat is on screen, and stays there until it has been
    /// for a second: the clock moves to that second and no further than
    /// the next, so a read by sight is still waiting to be written after.
    func see(_ key: ChildKey, scene: String = "s") async throws {
        await look(key, scene: scene)
        let thread = try #require(await harness.state.conversations[key]?.thread)
        try await harness.until(advancing: 1) { harness in await harness.state.sights[thread]?.read == true }
    }

    var scheduled: [NotificationRequest] {
        notifications.calls.compactMap { if case let .schedule(request) = $0 { request } else { nil } }
    }

    var badge: Int? {
        notifications.calls.compactMap { if case let .badge(count) = $0 { count } else { nil } }.last
    }
}

extension ThreadMark {
    /// Without when the server took the position: what a test names.
    fileprivate var plain: ThreadMark {
        var out = self
        out.readAt = nil
        return out
    }
}

private func thread(_ properties: [String: JSONValue]) -> ChatThread {
    var all: [String: JSONValue] = ["mode": "chat", "status": "ok", "finishedAt": "2026-10-31T10:00:05Z"]
    all.merge(properties) { _, new in new }
    return ChatThread(record: Record(id: "t1", kind: Kinds.thread, properties: all, version: 3))
}

private func message(_ id: String) -> JSONValue { ["ref": .string("\(Kinds.message)/\(id)")] }

private func markRecord(_ thread: String, readThrough: String?, unread: Bool? = nil, version: Int, readAt: String? = nil) -> Record {
    var properties: [String: JSONValue] = ["thread": ["ref": .string("\(Kinds.thread)/\(thread)")]]
    if let readThrough { properties["readThrough"] = message(readThrough) }
    if let readAt { properties["readAt"] = .string(readAt) }
    if let unread { properties["markedUnread"] = .bool(unread) }
    return Record(id: "\(thread).me", kind: Kinds.threadMark, properties: properties, version: version)
}

@Suite("What is unread", .timeLimit(.minutes(10)))
struct UnreadRule {
    /// Decision 0313's rule, case by case.
    @Test func aSettledChatIsUnreadByThePersonsMarkOfIt() {
        var state = ChatAppState()
        let settled = thread(["lastMessage": message("m2")])
        // The server keeps marks, and says nothing of this one yet: not
        // unread, so a list read without its marks shows no dot it may
        // have to take back.
        state.marks = [:]
        #expect(!state.unread(settled))
        // No mark: the person never opened it.
        state.marks = ["t1": .none]
        #expect(state.unread(settled))
        // Read through its newest message.
        state.marks = ["t1": ThreadMark(readThrough: "m2", version: 1)]
        #expect(!state.unread(settled))
        // Read through an earlier one.
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        #expect(state.unread(settled))
        // Marked unread, whatever was read.
        state.marks = ["t1": ThreadMark(readThrough: "m2", markedUnread: true, version: 2)]
        #expect(state.unread(settled))
        // A thread that names no newest message is unread only when marked so.
        let bare = thread([:])
        state.marks = ["t1": .none]
        #expect(!state.unread(bare))
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        #expect(!state.unread(bare))
        state.marks = ["t1": ThreadMark(markedUnread: true, version: 1)]
        #expect(state.unread(bare))
        // A chat that is running is working, not unread.
        state.marks = ["t1": ThreadMark(markedUnread: true, version: 1)]
        #expect(!state.unread(thread(["status": "running", "lastMessage": message("m2")])))
    }

    @Test func onAServerWithoutMarksTheDevicesOwnMapSaysIt() {
        var state = ChatAppState()
        let settled = thread(["lastMessage": message("m2")])
        #expect(state.marks == nil)
        #expect(state.unread(settled))
        state.seen["t1"] = "2026-10-31T10:00:05Z"
        #expect(!state.unread(settled))
        state.seen["t1"] = "2026-10-31T09:00:00Z"
        #expect(state.unread(settled))
    }

    @Test func whatTheDeviceMovedReadsOverTheServersMark() {
        var state = ChatAppState()
        let settled = thread(["lastMessage": message("m2")])
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        state.marking["t1"] = MarkChange(readThrough: "m2")
        #expect(!state.unread(settled))
        // On its way, with Mark as Unread made after it.
        state.markSending["t1"] = MarkChange(readThrough: "m2")
        state.marking["t1"] = MarkChange(markedUnread: true)
        #expect(state.unread(settled))
        #expect(state.mark("t1", last: "m2") == ThreadMark(readThrough: "m2", markedUnread: true, version: 1))
        // Seeing the end clears a Mark as Unread not yet sent.
        #expect(MarkChange(markedUnread: true).then(MarkChange(readThrough: "m2")) == MarkChange(readThrough: "m2"))
        #expect(MarkChange(readThrough: "m2").then(MarkChange(markedUnread: true)) == MarkChange(readThrough: "m2", markedUnread: true))
    }

    /// A read this device made and could not write says nothing of a thread
    /// that has moved on since: what the server holds by now is believed.
    @Test func aReadOfAMessageThatIsNoLongerTheNewestIsLeftToTheServer() {
        var state = ChatAppState()
        let moved = thread(["lastMessage": message("m3")])
        state.marking["t1"] = MarkChange(readThrough: "m2", held: true)
        // Another device read through the newer reply.
        state.marks = ["t1": ThreadMark(readThrough: "m3", version: 4)]
        #expect(!state.unread(moved))
        // Or read it and then marked the chat unread: the old read here
        // does not take that back.
        state.marks = ["t1": ThreadMark(readThrough: "m3", markedUnread: true, version: 5)]
        #expect(state.unread(moved))
        // Nobody read the newer reply: unread, as it is.
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        #expect(state.unread(moved))
        // A Mark as Unread made with the old read is still laid over.
        state.marks = ["t1": ThreadMark(readThrough: "m3", version: 4)]
        state.marking["t1"] = MarkChange(readThrough: "m2", markedUnread: true)
        #expect(state.unread(moved))
    }

    /// The line's anchor follows the same belief: a read this device could
    /// not write does not put the line above a reply read elsewhere since.
    @Test func theLineGoesAfterTheLaterOfWhatWasSeenHereAndElsewhere() {
        var state = ChatAppState()
        state.chats = [thread(["lastMessage": message("m3")]).record]
        state.marking["t1"] = MarkChange(readThrough: "m2", held: true)
        // Read through the newest elsewhere, then marked unread: no line.
        state.marks = ["t1": ThreadMark(readThrough: "m3", markedUnread: true, version: 5)]
        #expect(state.unread(ChatThread(record: state.chats[0])))
        #expect(state.newFrom("t1") == nil)
        // The server is behind what this device saw: both are named, and
        // the rows say which is later.
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        #expect(state.newFrom("t1") == .after(["m1", "m2"]))
        // Never opened anywhere.
        state.marking = [:]
        state.marks = ["t1": ThreadMark.none]
        #expect(state.newFrom("t1") == .start)
    }

    @Test func aWriteNeverCarriesReadAt() {
        #expect(MarkChange(readThrough: "m2", due: Date()).properties == ["readThrough": "m2"])
        #expect(MarkChange(markedUnread: true).properties == ["markedUnread": true])
    }
}

@Suite("Reading a chat moves the mark", .timeLimit(.minutes(10)))
struct ReadingMovesTheMark {
    @Test func theMarkMovesOnceTheEndHasBeenOnScreenForASecond() async throws {
        let server = MarkServer()
        server.chat("t1")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        #expect(await rig.row("t1")?.unread == true)
        #expect(await rig.harness.presentation.unread == 1)

        // Opened, and nothing says its end is on screen: it stays unread.
        let key = try await rig.open("t1")
        await rig.harness.advance(by: 3)
        #expect(await rig.row("t1")?.unread == true)

        let looked = rig.clock.now
        await rig.look(key)
        try await rig.sleeping(until: looked.addingTimeInterval(1))
        await rig.harness.advance(by: 0.9)
        #expect(await rig.row("t1")?.unread == true)
        await rig.harness.advance(by: 0.1)
        // The dot goes at once on this device, before anything is written.
        try await rig.harness.until { _ in await rig.row("t1")?.unread == false }
        #expect(await rig.harness.presentation.unread == 0)
        #expect(server.writes.isEmpty)
        let read = looked.addingTimeInterval(1)
        #expect(await rig.harness.state.marking["t1"] == MarkChange(readThrough: "t1-m2", due: read.addingTimeInterval(2)))

        // The write waits two seconds for a newer read to replace it.
        try await rig.sleeping(until: read.addingTimeInterval(2))
        await rig.harness.advance(by: 1.9)
        #expect(server.writes.isEmpty)
        await rig.harness.advance(by: 0.1)
        try await rig.harness.until { _ in server.mark("t1")?.readThrough == "t1-m2" }
        #expect(server.writes.count == 1)
        #expect(server.writes[0].keys.sorted() == ["readThrough", "thread"])
        #expect(server.writes[0]["thread"] as? String == "t1")
        try await rig.harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.markSending.isEmpty && state.marks?["t1"]?.plain == ThreadMark(readThrough: "t1-m2", version: 1)
        }
        // Read already: looking at it longer writes nothing more.
        await rig.harness.advance(by: 5)
        #expect(server.writes.count == 1)
        #expect(await rig.row("t1")?.unread == false)
    }

    @Test func aChatScrolledAwayFromItsEndIsNotReadByAReplyBelow() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m2")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let key = try await rig.open("t1")
        // The reader scrolled up: the view says the end is not on screen.
        await rig.look(key, false)
        let reply = server.settle("t1")
        try await rig.harness.until(advancing: 1) { harness in
            await harness.state.transcripts["t1"]?.records.contains { $0.id == reply } == true
        }
        await rig.harness.advance(by: 5)
        #expect(await rig.row("t1")?.unread == true)
        #expect(await rig.harness.state.sights.isEmpty)
        #expect(server.writes.isEmpty)
        // Back at the end: read.
        try await rig.see(key)
        try await rig.harness.until { _ in await rig.row("t1")?.unread == false }
    }

    @Test func aReplyThatSettlesWhileItsEndIsOnScreenIsReadWithoutAnyScroll() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m2")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let key = try await rig.open("t1")
        try await rig.see(key)
        #expect(server.writes.isEmpty)
        let reply = server.settle("t1")
        // The reply's rows land, and a second after that it is read.
        try await rig.harness.until(advancing: 1) { harness in await harness.state.marking["t1"]?.readThrough == reply }
        #expect(await rig.row("t1")?.unread == false)
        try await rig.harness.until(advancing: 2) { _ in server.mark("t1")?.readThrough == reply }
        // On screen the whole time: nothing notified it.
        #expect(rig.scheduled.isEmpty)
    }

    @Test func aWindowThatIsNotInFrontReadsNothing() async throws {
        let server = MarkServer()
        server.chat("t1")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let key = try await rig.open("t1")
        await rig.harness.advance(by: 3)
        let looked = rig.clock.now
        await rig.look(key)
        try await rig.sleeping(until: looked.addingTimeInterval(1))
        // The window goes behind another before the second is up.
        await rig.harness.advance(by: 0.5)
        await rig.harness.send(.sceneActive(scene: "s", active: false))
        await rig.harness.advance(by: 5)
        #expect(await rig.row("t1")?.unread == true)
        #expect(await rig.harness.state.sights.isEmpty)
        // In front again, the second is counted from the start.
        let back = rig.clock.now
        await rig.harness.send(.sceneActive(scene: "s", active: true))
        try await rig.sleeping(until: back.addingTimeInterval(1))
        await rig.harness.advance(by: 0.9)
        #expect(await rig.row("t1")?.unread == true)
        // The whole app leaves the front.
        await rig.harness.send(.phase(.background))
        await rig.harness.advance(by: 5)
        #expect(await rig.row("t1")?.unread == true)
        #expect(server.writes.isEmpty)
    }

    @Test func aReadWaitingToBeWrittenGoesOutOnLeavingTheChat() async throws {
        for leave in [ChatApp.Command.back(scene: "s"), .sceneActive(scene: "s", active: false), .phase(.inactive), .phase(.background)] {
            let server = MarkServer()
            server.chat("t1")
            let rig = ChatRig(marks: server)
            try await rig.signIn(following: server)
            let key = try await rig.open("t1")
            try await rig.see(key)
            try await rig.harness.until { harness in await harness.state.marking["t1"]?.due != nil }
            #expect(server.writes.isEmpty)
            // No clock moves: the write does not wait out its two seconds.
            await rig.harness.send(leave)
            try await rig.harness.until { _ in server.mark("t1")?.readThrough == "t1-m2" }
            #expect(server.writes.count == 1)
        }
    }

    /// The engine alone, on instants the test names: a reply read while an
    /// earlier read still waits replaces it, and one write goes out, two
    /// seconds after the later one.
    @Test func twoRepliesReadInARowAreOneWrite() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        let key = ChildKey("k")
        func rows(_ ids: [String]) -> [Record] {
            ids.map { Record(id: $0, kind: Kinds.message, properties: ["role": "assistant", "content": .string($0)]) }
        }
        var state = signedInState()
        state.recordsRevision = 8
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        state.chats = [thread(["lastMessage": message("m2")]).record]
        state.transcripts["t1"] = .init(records: rows(["m1", "m2"]), turns: [], version: 3, full: false)
        state.conversations[key] = .init(seed: .init(thread: "t1", agent: helper, draft: "", listen: false), thread: "t1", agent: helper)
        state.scenes["s"] = .init(current: key)
        let start = Date(timeIntervalSince1970: 1_793_696_400)

        state = try app.apply(.init(.endShown(scene: "s", key: key, shown: true)), to: state)
        #expect(state.asks.contains(.sight(thread: "t1", message: "m2")))
        state = try app.apply(.init(.sighted(thread: "t1", message: "m2", at: start)), to: state)
        #expect(state.marking["t1"] == MarkChange(readThrough: "m2", due: start.addingTimeInterval(2)))
        #expect(state.asks.contains(.marksWait(until: start.addingTimeInterval(2))))
        #expect(!state.unread(ChatThread(record: state.chats[0])))

        // The next reply settles and its rows land: a second on screen is
        // counted again, for it.
        var moved = thread(["lastMessage": message("m3")]).record
        moved.version = 4
        state = try app.apply(.init(.threadFetched(moved)), to: state)
        #expect(state.unread(ChatThread(record: state.chats[0])))
        #expect(!state.asks.contains { if case .sight = $0 { true } else { false } })
        state = try app.apply(
            .init(.rowsRead(thread: "t1", epoch: 0, version: 4, rows: rows(["m1", "m2", "m3"]), full: false)), to: state)
        #expect(state.asks.contains(.sight(thread: "t1", message: "m3")))
        state = try app.apply(.init(.sighted(thread: "t1", message: "m3", at: start.addingTimeInterval(1.5))), to: state)
        #expect(state.marking["t1"] == MarkChange(readThrough: "m3", due: start.addingTimeInterval(3.5)))
        #expect(state.asks.contains(.marksWait(until: start.addingTimeInterval(3.5))))

        // The first read's two seconds are up: nothing goes. Then the
        // second's are: one write, of the later message.
        state = try app.apply(.init(.marksDue(at: start.addingTimeInterval(2))), to: state)
        #expect(!state.asks.contains { if case .writeMark = $0 { true } else { false } })
        state = try app.apply(.init(.marksDue(at: start.addingTimeInterval(3.5))), to: state)
        #expect(state.asks.filter { if case .writeMark = $0 { true } else { false } } == [.writeMark(thread: "t1", MarkChange(readThrough: "m3"))])
        // A sight that ended (the chat scrolled away) reads nothing late.
        var away = state
        away.sights["t1"] = nil
        away.markSending = [:]
        away = try app.apply(.init(.sighted(thread: "t1", message: "m3", at: start)), to: away)
        #expect(away.marking.isEmpty)
    }

    @Test func aWriteThatFailsIsTriedAgainAndNeverShown() async throws {
        let server = MarkServer()
        server.chat("t1")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let key = try await rig.open("t1")
        server.failWrites(1)
        try await rig.see(key)
        try await rig.harness.until(advancing: 2) { harness in await harness.state.marking["t1"]?.held == true }
        #expect(server.writes.count == 1)
        #expect(server.mark("t1") == nil)
        // It reads as it was left here, and nothing says a write failed.
        #expect(await rig.row("t1")?.unread == false)
        let shown = await rig.harness.presentation
        #expect(shown.list.failure == nil)
        #expect(shown.conversations[key]?.failures.strip == nil)
        // Not on the clock: it waits for the next chance.
        await rig.harness.advance(by: 30)
        #expect(server.writes.count == 1)
        // Kept across a launch.
        #expect(rig.storage.state(try await rig.lease())?.marking?["t1"]?.readThrough == "t1-m2")
        // The next read the server answers is the chance.
        await rig.harness.send(.refresh)
        try await rig.harness.until(advancing: 1) { _ in server.mark("t1")?.readThrough == "t1-m2" }
        #expect(server.writes.count == 2)
        try await rig.harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.marking.isEmpty && state.markSending.isEmpty
        }
        #expect(await rig.row("t1")?.unread == false)
    }

    @Test func aReadThatCouldNotBeWrittenDoesNotHideOneMadeElsewhereSince() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m1")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let key = try await rig.open("t1")
        server.failWrites(1)
        try await rig.see(key)
        try await rig.harness.until(advancing: 2) { harness in await harness.state.marking["t1"]?.held == true }
        await rig.harness.send(.back(scene: "s"))
        // A reply settles, and is read on another device, while this one
        // still holds the read it could not write.
        let reply = server.settle("t1")
        server.write("t1", readThrough: reply)
        try await rig.harness.until(advancing: 0.5) { harness in
            let state = await harness.state
            return state.marks?["t1"]?.readThrough == reply && state.thread("t1")?.lastMessage == reply
        }
        #expect(await rig.row("t1")?.unread == false)
        await rig.harness.advance(by: 10)
        #expect(rig.scheduled.isEmpty)
        // The held write goes out at the next chance, and moves nothing back.
        await rig.harness.send(.refresh)
        try await rig.harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.marking.isEmpty && state.markSending.isEmpty && !state.refreshing
        }
        #expect(server.mark("t1")?.readThrough == reply)
        #expect(await rig.row("t1")?.unread == false)
    }

    @Test func aWriteTheServerRefusesForGoodIsDropped() async throws {
        let server = MarkServer()
        server.chat("t1")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let key = try await rig.open("t1")
        server.refuseWrites(422)
        try await rig.see(key)
        try await rig.harness.until(advancing: 2) { _ in server.writes.count == 1 }
        try await rig.harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.marking.isEmpty && state.markSending.isEmpty
        }
        // The chat reads as the server says, and nothing is tried again.
        #expect(await rig.row("t1")?.unread == true)
        await rig.harness.send(.refresh)
        try await rig.harness.until(advancing: 1) { harness in await !harness.state.refreshing }
        await rig.harness.advance(by: 5)
        #expect(server.writes.count == 1)
    }
}

@Suite("Marks from another device", .timeLimit(.minutes(10)))
struct MarksFromElsewhere {
    @Test func aChatReadOnOneDeviceIsReadOnTheOther() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m2")
        let phone = ChatRig(marks: server)
        let pad = ChatRig(marks: server)
        try await phone.signIn(following: server)
        try await pad.signIn(following: server)

        // A reply settles while neither shows the chat: both say so.
        let reply = server.settle("t1")
        for device in [phone, pad] {
            try await device.harness.until(advancing: 1) { _ in
                await device.row("t1")?.unread == true && device.scheduled.count == 1 && device.badge == 1
            }
        }
        // The notification's five seconds pass on the tablet: it is shown.
        await pad.harness.advance(by: 6)
        let shown = try #require(pad.scheduled.first)
        #expect(shown.thread == "t1")

        // The chat is read on the phone. Nobody touches the tablet.
        let key = try await phone.open("t1")
        try await phone.see(key)
        try await phone.harness.until(advancing: 2) { _ in server.mark("t1")?.readThrough == reply }
        try await pad.harness.until(advancing: 1) { _ in await pad.row("t1")?.unread == false }
        // Its dot went, its notification was taken back, its badge is 0.
        #expect(await pad.harness.presentation.unread == 0)
        try await pad.harness.until { _ in pad.notifications.calls.contains(.cancelThread("t1")) && pad.badge == 0 }
        #expect(pad.notifications.calls.contains(.cancel(shown.id)))
        // The phone took its own back when the chat came on screen.
        #expect(phone.notifications.pending.isEmpty)
        #expect(phone.badge == 0)
    }

    @Test func aReplysNotificationWaitsFiveSecondsForAMark() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m2")
        server.chat("t2", readThrough: "t2-m2")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)

        let reply = server.settle("t1", reply: "Read on the Mac")
        try await rig.harness.until(advancing: 0.5) { _ in rig.scheduled.count == 1 }
        let held = try #require(rig.scheduled.first)
        let wait = try #require(held.fireAt).timeIntervalSince(rig.clock.now)
        #expect(wait > 4 && wait <= 5)
        #expect(held.body == "Read on the Mac")
        // Read on the device beside this one within the five seconds.
        server.write("t1", readThrough: reply)
        try await rig.harness.until(advancing: 0.5) { _ in rig.notifications.pending[held.id] == nil }
        #expect(rig.notifications.calls.contains(.cancel(held.id)))
        #expect(await rig.row("t1")?.unread == false)

        // One nobody reads stays, to be shown when its five seconds pass.
        server.settle("t2", reply: "Read by nobody")
        try await rig.harness.until(advancing: 0.5) { _ in rig.scheduled.count == 2 }
        let kept = rig.scheduled[1]
        await rig.harness.advance(by: 10)
        #expect(rig.notifications.pending[kept.id] != nil)
        #expect(await rig.row("t2")?.unread == true)
    }

    @Test func aReplyAlreadyReadElsewhereIsNeverNotified() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m2")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        // Settled and read on another device before this one heard of
        // either: the two arrive together.
        let reply = server.settle("t1")
        server.write("t1", readThrough: reply)
        try await rig.harness.until(advancing: 0.5) { harness in
            let state = await harness.state
            return state.marks?["t1"]?.readThrough == reply && state.thread("t1")?.lastMessage == reply
        }
        await rig.harness.advance(by: 10)
        #expect(rig.scheduled.isEmpty)
        #expect(await rig.row("t1")?.unread == false)
    }

    @Test func anOlderMarkArrivingLateNeverBringsADotBack() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m1")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        #expect(await rig.row("t1")?.unread == true)
        let stale = ChatAppStep.Marks(threads: ["t1"], records: [markRecord("t1", readThrough: "t1-m1", version: 1)])
        let key = try await rig.open("t1")
        try await rig.see(key)
        try await rig.harness.until { _ in await rig.row("t1")?.unread == false }

        // A read that left the server before the chat was read lands while
        // the write still waits: the chat stays read.
        await rig.harness.deliver(.init(.marksRead(stale)))
        #expect(await rig.row("t1")?.unread == false)
        try await rig.harness.until(advancing: 2) { _ in server.mark("t1")?.readThrough == "t1-m2" }
        try await rig.harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.markSending.isEmpty && state.marks?["t1"]?.version == 2
        }
        // And after the write landed: the older answer, and one that found
        // no mark at all, are dropped.
        await rig.harness.deliver(.init(.marksRead(stale)))
        await rig.harness.deliver(.init(.marksRead(ChatAppStep.Marks(threads: ["t1"], records: []))))
        #expect(await rig.row("t1")?.unread == false)
        #expect(await rig.harness.state.marks?["t1"]?.plain == ThreadMark(readThrough: "t1-m2", version: 2))

        // Mark as Unread on another device does bring it back, and the
        // open chat here does not read it away again.
        server.write("t1", markedUnread: true)
        try await rig.harness.until(advancing: 0.5) { _ in await rig.row("t1")?.unread == true }
        await rig.harness.advance(by: 10)
        #expect(await rig.row("t1")?.unread == true)
        #expect(server.mark("t1")?.markedUnread == true)
    }

    /// A reply whose chat's mark the server has not said yet may have been
    /// read elsewhere: it waits for the mark, and is notified when the mark
    /// says nobody read it.
    @Test func aReplyWaitsForItsChatsMarkBeforeItNotifies() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        state.recordsRevision = 8
        state.marks = [:]
        state.baselined = true
        let settled = thread(["lastMessage": message("m2"), "lastReply": "Hello", "agent": ["ref": .string("\(Kinds.agent)/\(helper)")]]).record
        // The list read brought the thread and no marks (their read failed).
        state = try app.apply(.init(.threadsRead(.init(chats: [settled], automated: []))), to: state)
        #expect(!state.asks.contains { if case .notifyHeld = $0 { true } else { false } })
        #expect(state.notified.isEmpty)
        #expect(!state.unread(ChatThread(record: settled)))
        // The marks land: nobody read it.
        var unread = try app.apply(.init(.marksRead(.init(threads: ["t1"], records: []))), to: state)
        #expect(unread.asks.contains { if case .notifyHeld(_, seconds: 5) = $0 { true } else { false } })
        #expect(unread.unread(ChatThread(record: settled)))
        // Said once.
        unread = try app.apply(.init(.marksRead(.init(threads: ["t1"], records: []))), to: unread)
        #expect(!unread.asks.contains { if case .notifyHeld = $0 { true } else { false } })
        // Or the marks land and it was read on another device: nothing.
        let read = try app.apply(
            .init(.marksRead(.init(threads: ["t1"], records: [markRecord("t1", readThrough: "m2", version: 1)]))), to: state)
        #expect(!read.asks.contains { if case .notifyHeld = $0 { true } else { false } })
        #expect(read.notified.count == 1)
    }

    /// The server keeps marks and this device has read none yet (the first
    /// read failed): a reply is not notified by the device's own map, which
    /// knows nothing of the other devices. It waits for the marks.
    @Test func aReplyWaitsForTheFirstReadOfTheMarks() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        state.recordsRevision = 8
        state.baselined = true
        #expect(state.marks == nil && state.readsMarks)
        let settled = thread(["lastMessage": message("m2"), "lastReply": "Hello", "agent": ["ref": .string("\(Kinds.agent)/\(helper)")]]).record
        state = try app.apply(.init(.threadsRead(.init(chats: [settled], automated: []))), to: state)
        #expect(!state.asks.contains { if case .notify = $0 { true } else if case .notifyHeld = $0 { true } else { false } })
        #expect(state.notified.isEmpty)
        // Read on another device already.
        let read = try app.apply(
            .init(.marksRead(.init(threads: ["t1"], records: [markRecord("t1", readThrough: "m2", version: 1)]))), to: state)
        #expect(!read.asks.contains { if case .notifyHeld = $0 { true } else { false } })
        // Or by nobody.
        let unread = try app.apply(.init(.marksRead(.init(threads: ["t1"], records: []))), to: state)
        #expect(unread.asks.contains { if case .notifyHeld = $0 { true } else { false } })
    }

    /// The marks this device waited for arrive with a newer row of the
    /// thread: the reply that waited is judged against the row that came
    /// with them, never the older one, so one read elsewhere stays quiet.
    @Test func aReplyThatWaitedIsJudgedAgainstTheRowItsMarkCameWith() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        let agent: JSONValue = ["ref": .string("\(Kinds.agent)/\(helper)")]
        var state = signedInState()
        state.recordsRevision = 8
        state.baselined = true
        let older = thread(["lastMessage": message("m2"), "lastReply": "Hello", "agent": agent]).record
        state = try app.apply(.init(.threadsRead(.init(chats: [older], automated: []))), to: state)
        #expect(state.notified.isEmpty)
        // Another device got and read a newer reply; one refresh brings both.
        var newer = thread(["lastMessage": message("m3"), "lastReply": "More", "agent": agent, "finishedAt": "2026-10-31T10:05:00Z"]).record
        newer.version = 4
        let marks = ChatAppStep.Marks(threads: ["t1"], records: [markRecord("t1", readThrough: "m3", version: 2)])
        state = try app.apply(.init(.threadsRead(.init(chats: [newer], automated: [], marks: marks))), to: state)
        #expect(!state.asks.contains { if case .notify = $0 { true } else if case .notifyHeld = $0 { true } else { false } })
        #expect(!state.unread(ChatThread(record: newer)))
    }

    /// A reply waited for marks a server then turned out not to keep (a
    /// rollback): it is notified by the device's own map after all.
    @Test func aReplyThatWaitedForMarksIsNotifiedWhenTheServerKeepsNone() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        state.recordsRevision = 8
        state.baselined = true
        let settled = thread(["lastMessage": message("m2"), "lastReply": "Hello", "agent": ["ref": .string("\(Kinds.agent)/\(helper)")]]).record
        state = try app.apply(.init(.threadsRead(.init(chats: [settled], automated: []))), to: state)
        #expect(state.notified.isEmpty)
        let older = try app.apply(.init(.featuresRead(agents: 2, records: 7)), to: state)
        #expect(older.asks.contains { if case .notify = $0 { true } else { false } })
        let unserved = try app.apply(.init(.marksRead(.init(threads: [], records: [], unserved: true))), to: state)
        #expect(unserved.asks.contains { if case .notify = $0 { true } else { false } })
        // Said once: the document read again changes nothing.
        let again = try app.apply(.init(.featuresRead(agents: 2, records: 7)), to: older)
        #expect(!again.asks.contains { if case .notify = $0 { true } else { false } })
    }

    /// A reply settles, and is read elsewhere, between this device's read
    /// of the threads and its read of their marks: the mark reads through
    /// a message the rows have not heard of. With no feed to put it right
    /// (the refresh job), the threads are read once more.
    @Test func aMarkThatOutranTheListItWasReadWithIsNotAnUnreadDot() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m2")
        let rig = ChatRig(marks: server, platform: .ios)
        try await rig.signIn(following: server)
        await rig.harness.send(.phase(.background))
        try await rig.harness.until { _ in rig.background.scheduled.count == 1 }
        server.afterNextThreadList {
            let reply = server.settle("t1")
            server.write("t1", readThrough: reply)
        }
        rig.background.grant(job: ChatApp.refreshJob, id: "r")
        try await rig.harness.until { _ in rig.background.finished["r"] == true }
        await rig.harness.settle()
        #expect(await rig.harness.state.thread("t1")?.lastMessage == "t1-m3")
        #expect(await rig.row("t1")?.unread == false)
        #expect(rig.badge ?? 0 == 0)
        #expect(rig.scheduled.isEmpty)
    }

    /// A chat this device just started has no mark, and the app does not
    /// wait for the server to say so: its reply, settled after the chat
    /// was left, is unread and notified.
    @Test func aNewChatsReplySettledAfterItWasLeftIsUnreadAndNotified() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        state.recordsRevision = 8
        state.marks = [:]
        state.baselined = true
        let key = ChildKey("k")
        state.conversations[key] = .init(seed: .init(thread: nil, agent: helper, draft: "", listen: false), agent: helper)
        let report = ChatAppStep.Report(
            thread: "t1", agent: helper, owns: true, active: true, streaming: true, outcomes: 0, name: "Helper",
            settles: 0, named: "t1", read: nil, draft: nil, sent: false, failure: nil)
        state = try app.apply(.init(.conversation(key, report)), to: state)
        #expect(state.marks?["t1"] == ThreadMark.none)
        let settled = thread(["lastMessage": message("m2"), "lastReply": "Hello", "agent": ["ref": .string("\(Kinds.agent)/\(helper)")]]).record
        state = try app.apply(.init(.threadFetched(settled)), to: state)
        #expect(state.unread(ChatThread(record: settled)))
        #expect(state.asks.contains { if case .notifyHeld = $0 { true } else { false } })
    }

    @Test func theRefreshJobNotifiesOnlyWhatNoDeviceRead() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m2")
        server.chat("t2", readThrough: "t2-m2")
        let rig = ChatRig(marks: server, platform: .ios)
        try await rig.signIn(following: server)
        // In a pocket: the watch is closed, and both chats get a reply. One
        // is read on the Mac.
        await rig.harness.send(.phase(.background))
        try await rig.harness.until { _ in rig.background.scheduled.count == 1 }
        let read = server.settle("t1")
        server.write("t1", readThrough: read)
        server.settle("t2", reply: "Still to read")
        rig.background.grant(job: ChatApp.refreshJob, id: "r")
        try await rig.harness.until { _ in rig.background.finished["r"] == true }
        await rig.harness.settle()
        #expect(rig.scheduled.map(\.thread) == ["t2"])
        #expect(await rig.row("t1")?.unread == false)
        #expect(await rig.row("t2")?.unread == true)
        #expect(rig.badge == 1)
    }
}

@Suite("Mark as Unread and Mark as Read", .timeLimit(.minutes(10)))
struct MarkingByHand {
    @Test func markAsUnreadIsAFlagThatNeverMovesThePosition() async throws {
        let server = MarkServer()
        server.chat("t1", messages: 4, readThrough: "t1-m4")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        #expect(await rig.row("t1")?.unread == false)
        await rig.harness.send(.markUnread("t1"))
        // At once here, and written at once.
        #expect(await rig.row("t1")?.unread == true)
        try await rig.harness.until { _ in server.mark("t1")?.markedUnread == true }
        #expect(server.writes.count == 1)
        #expect(server.writes[0].keys.sorted() == ["markedUnread", "thread"])
        #expect(server.mark("t1")?.readThrough == "t1-m4")
        // Unread already: nothing more is written.
        await rig.harness.send(.markUnread("t1"))
        await rig.harness.settle()
        #expect(server.writes.count == 1)

        // Opening it and seeing its end clears it.
        let key = try await rig.open("t1")
        // Nothing new stands below what was read: no line.
        #expect(await rig.conversation(key)?.newLine == nil)
        try await rig.see(key)
        try await rig.harness.until { _ in await rig.row("t1")?.unread == false }
        try await rig.harness.until(advancing: 2) { _ in server.mark("t1")?.markedUnread == false }
        #expect(server.writes[1].keys.sorted() == ["readThrough", "thread"])
    }

    @Test func aChatMarkedUnreadWhileItIsOpenStaysUnreadUntilItIsLeft() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m2")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let key = try await rig.open("t1")
        await rig.look(key)
        await rig.harness.send(.markUnread("t1"))
        try await rig.harness.until { _ in server.mark("t1")?.markedUnread == true }
        // Its end is on screen the whole time.
        await rig.harness.advance(by: 10)
        #expect(await rig.row("t1")?.unread == true)
        #expect(server.mark("t1")?.markedUnread == true)
        // Left and opened again: read.
        await rig.harness.send(.back(scene: "s"))
        let again = try await rig.open("t1")
        try await rig.see(again)
        try await rig.harness.until(advancing: 2) { _ in server.mark("t1")?.markedUnread == false }
        #expect(await rig.row("t1")?.unread == false)
    }

    @Test func markAsReadReadsThroughTheNewestMessage() async throws {
        let server = MarkServer()
        server.chat("t1", messages: 4, readThrough: "t1-m2")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        #expect(await rig.row("t1")?.unread == true)
        await rig.harness.send(.markRead("t1"))
        #expect(await rig.row("t1")?.unread == false)
        try await rig.harness.until { _ in server.mark("t1")?.readThrough == "t1-m4" }
        #expect(server.writes[0].keys.sorted() == ["readThrough", "thread"])
    }

    /// The server takes writes in the order they were made: a Mark as
    /// Unread made while a read is on its way goes out after it, never
    /// ahead of it, where the read would clear it.
    @Test func markAsUnreadWaitsForAReadAlreadyOnItsWay() throws {
        let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
        var state = signedInState()
        state.recordsRevision = 8
        state.chats = [thread(["lastMessage": message("m2")]).record]
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        let read = MarkChange(readThrough: "m2")
        state.markSending["t1"] = read
        state = try app.apply(.init(.markedUnread("t1")), to: state)
        #expect(state.marking["t1"] == MarkChange(markedUnread: true))
        #expect(!state.asks.contains { if case .writeMark = $0 { true } else { false } })
        #expect(state.unread(ChatThread(record: state.chats[0])))
        // The read lands: now the flag goes.
        state = try app.apply(.init(.markWritten(thread: "t1", sent: read, markRecord("t1", readThrough: "m2", version: 2))), to: state)
        #expect(state.asks.contains(.writeMark(thread: "t1", MarkChange(markedUnread: true))))
        #expect(state.markSending["t1"] == MarkChange(markedUnread: true))
        // The read failed instead: the two go out together, later.
        var failed = signedInState()
        failed.recordsRevision = 8
        failed.chats = state.chats
        failed.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        failed.markSending["t1"] = read
        failed.marking["t1"] = MarkChange(markedUnread: true)
        failed = try app.apply(.init(.markWriteFailed(thread: "t1", sent: read, .later)), to: failed)
        #expect(failed.asks.contains(.writeMark(thread: "t1", MarkChange(readThrough: "m2", markedUnread: true))))
    }
}

@Suite("Where a chat opens", .timeLimit(.minutes(10)))
struct NewLineOnOpening {
    @Test func aChatWithUnreadMessagesOpensWithALineAboveTheFirst() async throws {
        let server = MarkServer()
        server.chat("t1", messages: 6, readThrough: "t1-m2")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let key = try await rig.open("t1")
        #expect(await rig.conversation(key)?.newLine == NewLine(turn: "t1-m3", label: "New", spoken: "New messages"))
        // It stays while the chat is open, read or not.
        try await rig.see(key)
        try await rig.harness.until(advancing: 2) { _ in server.mark("t1")?.readThrough == "t1-m6" }
        #expect(await rig.row("t1")?.unread == false)
        #expect(await rig.conversation(key)?.newLine?.turn == "t1-m3")
        // And is gone the next time.
        await rig.harness.send(.back(scene: "s"))
        let again = try await rig.open("t1")
        #expect(await rig.conversation(again)?.newLine == nil)
    }

    @Test func theLineStandsAboveTheFirstTurnAfterTheMessageRead() {
        func row(_ id: String, _ role: String, call: String? = nil) -> Record {
            var properties: [String: JSONValue] = ["role": .string(role), "content": .string(id)]
            if role == "assistant", let call { properties["toolCalls"] = [["id": .string(call), "name": "search"]] }
            if role == "tool", let call { properties["toolCallId"] = .string(call) }
            return Record(id: id, kind: Kinds.message, properties: properties)
        }
        let rows = [row("m1", "user"), row("m2", "assistant", call: "c1"), row("m3", "tool", call: "c1"), row("m4", "assistant"), row("m5", "user")]
        let held = ChatAppState.Held(records: rows, turns: transcript(of: rows), version: 1, full: false)
        #expect(ChatApp.newLine(held, from: .after(["m1"]))?.turn == "m2")
        // A tool's row is part of the turn that called it, not a new one.
        #expect(ChatApp.newLine(held, from: .after(["m2"]))?.turn == "m4")
        #expect(ChatApp.newLine(held, from: .after(["m4"]))?.turn == "m5")
        // Of the server's position and one this device saw since, the later.
        #expect(ChatApp.newLine(held, from: .after(["m1", "m4"]))?.turn == "m5")
        #expect(ChatApp.newLine(held, from: .after(["m4", "m1"]))?.turn == "m5")
        // Nothing below, a message the rows do not hold, nothing new.
        #expect(ChatApp.newLine(held, from: .after(["m5"])) == nil)
        #expect(ChatApp.newLine(held, from: .after(["m0"])) == nil)
        #expect(ChatApp.newLine(held, from: nil) == nil)
        // Never opened: everything held is new.
        #expect(ChatApp.newLine(held, from: .start)?.turn == "m1")
    }

    @Test func aChatNeverOpenedOpensWithTheLineAboveItsFirstTurn() async throws {
        let server = MarkServer()
        server.chat("t1", messages: 6)
        server.chat("t2", messages: 2, readThrough: "t2-m2")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        let key = try await rig.open("t1")
        #expect(await rig.conversation(key)?.newLine?.turn == "t1-m1")
        // A chat only marked unread has nothing new in it: no line.
        await rig.harness.send(.markUnread("t2"))
        let other = try await rig.open("t2", scene: "o")
        #expect(await rig.row("t2")?.unread == true)
        #expect(await rig.conversation(other)?.newLine == nil)
    }
}

@Suite("The waiting mark", .timeLimit(.minutes(10)))
struct WaitingOnThePerson {
    private func cardReads(_ server: MarkServer) -> Int {
        [Kinds.interaction, Kinds.request, Kinds.approval].map { server.count("/api/v1/records", and: $0) }.reduce(0, +)
    }

    @Test func theRowReadsWhatWaitsFromTheThread() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m2")
        server.chat("t2", readThrough: "t2-m2")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        #expect(await rig.row("t1")?.mark == nil)
        // Nothing waits, so no list of questions, suggestions or approvals
        // is read with the chats.
        let before = cardReads(server)
        await rig.harness.send(.refresh)
        try await rig.harness.until(advancing: 1) { harness in await !harness.state.refreshing }
        #expect(cardReads(server) == before)

        // A question is asked in one chat: its row says so from the thread,
        // and the cards are read under that thread alone.
        server.wait("t1", on: 1)
        try await rig.harness.until(advancing: 0.5) { _ in await rig.row("t1")?.mark == .attention }
        #expect(await rig.row("t2")?.mark == nil)
        await rig.harness.send(.refresh)
        try await rig.harness.until(advancing: 1) { harness in
            let refreshing = await harness.state.refreshing
            return !refreshing && cardReads(server) > before
        }
        let asked = server.requests.filter { $0.contains("/api/v1/records") && $0.contains(Kinds.interaction) }.suffix(1)
        #expect(asked.allSatisfy { $0.contains(#""thread":{"in":["t1"]}"#) })

        // Answered: the mark goes with the thread's count.
        server.wait("t1", on: 0)
        try await rig.harness.until(advancing: 0.5) { _ in await rig.row("t1")?.mark == nil }
    }

    @Test func onAServerWithoutTheCountTheThreeListsAreReadAsBefore() async throws {
        let server = MarkServer(records: 7)
        server.chat("t1")
        let rig = ChatRig(marks: server)
        try await rig.signIn()
        let before = cardReads(server)
        #expect(before >= 3)
        await rig.harness.send(.refresh)
        try await rig.harness.until(advancing: 1) { harness in await !harness.state.refreshing }
        #expect(cardReads(server) == before + 3)
        // Unfiltered: the whole lists, as before.
        #expect(server.requests.filter { $0.contains("/api/v1/records") && $0.contains(Kinds.interaction) }.allSatisfy { !$0.contains(#""thread""#) })
    }
}

@Suite("A server without marks", .timeLimit(.minutes(10)))
struct OlderServer {
    @Test func theAppBehavesAsItDidBeforeMarks() async throws {
        let server = MarkServer(records: 7)
        server.chat("t1")
        let rig = ChatRig(marks: server)
        try await rig.signIn()
        try await rig.harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.recordsRevision == 7 && state.watching
        }
        // At the first sign-in everything is history.
        #expect(await rig.row("t1")?.unread == false)
        let state = await rig.harness.state
        #expect(state.marks == nil && !state.marksKnown)

        // A reply settles off screen: unread by the device's map, and
        // notified at once.
        server.settle("t1")
        try await rig.harness.until(advancing: 0.5) { _ in await rig.row("t1")?.unread == true && rig.scheduled.count == 1 }
        #expect(rig.scheduled[0].fireAt == nil)
        // Opening it reads it, whatever is on screen and for however long.
        let key = try await rig.open("t1")
        #expect(await rig.row("t1")?.unread == false)
        #expect(await rig.conversation(key)?.newLine == nil)
        // Mark as Unread and Mark as Read are the device's own.
        await rig.harness.send(.markUnread("t1"))
        #expect(await rig.row("t1")?.unread == true)
        await rig.harness.send(.markRead("t1"))
        #expect(await rig.row("t1")?.unread == false)
        await rig.look(key)
        await rig.harness.advance(by: 10)
        // Nothing ever named the mark kind: no read, no write, no watch.
        #expect(server.count(Kinds.threadMark) == 0)
        #expect(server.writes.isEmpty)
        #expect(await rig.harness.state.listFailure == nil)
        #expect(server.count("/api/v1/changes") > 0)
    }

    /// A server whose document says it keeps marks and which does not know
    /// the kind (a repository whose package was not upgraded).
    @Test func aServerThatDoesNotKnowTheKindIsOneWithoutMarks() async throws {
        let threads = #"{"head":10,"generation":"g","records":[\#(threadJSON(version: 3))]}"#
        let document =
            #"{"server":{"version":"dev"},"features":[{"name":"agents","stability":"experimental","revision":2},{"name":"records","stability":"stable","revision":8}]}"#
        let unknown = #"{"error":{"code":"not_found","message":"unknown kind substrate.reamde.dev/llm/threadmark"}}"#
        let rig = ChatRig(
            [
                .init(method: "GET", path: "/.well-known/substrate/server.json", answer: .json(200, document)),
                .init(method: "GET", path: "/api/v1/records", query: "llm/threadmark", answer: .json(404, unknown)),
            ] + baseRoutes(threads: threads))
        try await rig.signIn()
        try await rig.harness.until { harness in await harness.state.marksUnserved }
        let state = await rig.harness.state
        #expect(state.marks == nil && !state.readsMarks)
        #expect(state.listFailure == nil)
        // The watch never names the kind.
        #expect(rig.server.count("/api/v1/changes", query: "threadmark") == 0)
    }

    @Test func aServerThatStopsKeepingMarksLeavesTheDotsWhereTheyWere() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m1")
        server.chat("t2", readThrough: "t2-m2")
        let rig = ChatRig(marks: server)
        try await rig.signIn(following: server)
        #expect(await rig.row("t1")?.unread == true)
        #expect(await rig.row("t2")?.unread == false)
        // Rolled back to a release from before marks.
        server.become(records: 7)
        await rig.harness.send(.refresh)
        try await rig.harness.until(advancing: 1) { harness in
            let state = await harness.state
            return state.marks == nil && !state.refreshing && state.recordsRevision == 7
        }
        #expect(await rig.row("t1")?.unread == true)
        #expect(await rig.row("t2")?.unread == false)
        #expect(await rig.harness.presentation.unread == 1)
    }
}

@Suite("The device's own marks, after the server keeps them", .timeLimit(.minutes(10)))
struct MovingToTheServersMarks {
    /// The first launch against a server that gained marks: the device's
    /// own map is not sent anywhere, and what the server says replaces it
    /// in one step, so the list never shows every chat as unread.
    @Test func theFirstLaunchShowsWhatTheServerSaysAndNothingElse() async throws {
        let server = MarkServer(records: 7)
        for id in ["t1", "t2", "t3"] { server.chat(id) }
        let base = temporaryFolder("chat")
        let before = ChatRig(marks: server, base: base)
        try await before.signIn()
        try await before.harness.until(advancing: 1) { harness in await harness.state.watching }
        // Unread on this device alone, by its own map.
        server.settle("t2")
        try await before.harness.until(advancing: 0.5) { _ in await before.row("t2")?.unread == true }
        let lease = try await before.lease()
        try await before.harness.until(advancing: 1) { _ in
            before.storage.state(lease)?.threads.first { $0.id == "t2" }?.version == 2
        }
        await before.runtime.close()

        // The server is upgraded: its migration reads every chat from
        // before for every person. Then one chat gets a reply.
        server.become(records: 8)
        server.write("t1", readThrough: "t1-m2")
        server.write("t2", readThrough: "t2-m3")
        server.write("t3", readThrough: "t3-m2")
        let reply = server.settle("t1")

        let after = ChatRig(marks: server, base: base)
        server.hold("/api/v1/records")
        await after.runtime.linkChanged(LinkSnapshot(phase: .signedIn, server: origin, repository: repository, online: true))
        try await after.harness.until { await $0.state.opened }
        // Drawn from the disk first, as it was left.
        #expect(await after.row("t2")?.unread == true)
        // Every list drawn in between is watched: the chat nobody touched
        // is never unread, so the list never shows every chat so.
        let presentations = await after.runtime.presentations()
        let seen = Seen()
        let watching = Task {
            for await tree in presentations { seen.add(tree.list.sections.flatMap(\.rows).count { $0.unread && $0.id == "t3" }) }
        }
        defer { watching.cancel() }
        server.letGo("/api/v1/records")
        try await after.harness.until(advancing: 1) { harness in await harness.state.marks?["t1"]?.readThrough == "t1-m2" }
        // What the server knows is unread shows, and what only this device
        // called unread no longer does.
        #expect(await after.row("t1")?.unread == true)
        #expect(await after.row("t2")?.unread == false)
        #expect(await after.harness.state.thread("t1")?.lastMessage == reply)
        #expect(await after.row("t3")?.unread == false)
        await after.harness.settle()
        #expect(!seen.counts.isEmpty && seen.counts.allSatisfy { $0 == 0 })
        // The device's map went nowhere.
        #expect(server.writes.isEmpty)
    }

    @Test func theDotsDrawFromTheDiskBeforeTheServerIsAsked() async throws {
        let server = MarkServer()
        server.chat("t1", readThrough: "t1-m1")
        server.chat("t2", readThrough: "t2-m2")
        let base = temporaryFolder("chat")
        let first = ChatRig(marks: server, base: base)
        try await first.signIn(following: server)
        let lease = try await first.lease()
        try await first.harness.until(advancing: 1) { _ in first.storage.state(lease)?.marks?["t1"] != nil }
        await first.runtime.close()

        let again = ChatRig(marks: server, base: base)
        server.hold("/api/v1/records")
        await again.runtime.linkChanged(LinkSnapshot(phase: .signedIn, server: origin, repository: repository, online: true))
        try await again.harness.until { await $0.state.opened }
        #expect(await again.row("t1")?.unread == true)
        #expect(await again.row("t2")?.unread == false)
        #expect(await again.harness.presentation.unread == 1)
        // The first list read brings the marks with it.
        server.letGo("/api/v1/records")
        try await again.harness.until(advancing: 1) { harness in await harness.state.marksKnown }
        #expect(server.count(Kinds.threadMark, and: #""readBy":{"eq":"me"}"#) >= 2)
    }
}

@Suite("Asking to notify", .timeLimit(.minutes(10)))
struct AskingToNotify {
    @Test func thePermissionIsOfferedAfterTheFirstReplySettlesNotWhileItStreams() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream, holdAfter: 2))])
        rig.notifications.set(.notAsked)
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.provisional.turns.contains { $0.role == .assistant } == true }
        // Streaming: nothing is asked.
        await rig.harness.settle()
        #expect(await rig.harness.presentation.notificationOffer == nil)
        #expect(!rig.notifications.calls.contains(.requestPermission))
        rig.server.release()
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        // Settled: the app says what it would notify, and the system is
        // asked only on a yes.
        try await rig.harness.until { harness in await harness.presentation.notificationOffer != nil }
        let offer = try #require(await rig.harness.presentation.notificationOffer)
        #expect(offer.message.contains("finishes a reply"))
        #expect(!rig.notifications.calls.contains(.requestPermission))
        await rig.harness.send(.notifications(allow: true))
        try await rig.harness.until { _ in rig.notifications.calls.contains(.requestPermission) }
        #expect(await rig.harness.presentation.notificationOffer == nil)
        #expect(await rig.harness.state.preferences.askedNotifications)
    }

    @Test func notNowAsksNothingAndSettingsOffersItLater() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        rig.notifications.set(.notAsked)
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { harness in await harness.presentation.notificationOffer != nil }
        await rig.harness.send(.notifications(allow: false))
        #expect(await rig.harness.presentation.notificationOffer == nil)
        await rig.harness.settle()
        #expect(!rig.notifications.calls.contains(.requestPermission))
        // Settings still offers it.
        await rig.harness.send(.readSettings)
        try await rig.harness.until { harness in await harness.state.permission == .notAsked }
        let rows = await rig.harness.presentation.settings.sections.flatMap(\.rows)
        #expect(rows.contains { $0.label == "Turn On Notifications" })
    }

    @Test func aDeviceAlreadyAskedIsNotOffered() async throws {
        let rig = ChatRig(baseRoutes() + [.init(method: "POST", path: "/chat", answer: .stream(replyStream))])
        try await rig.signIn()
        let key = try await rig.newChat()
        try await rig.send(.draft("Hi"), to: key)
        try await rig.send(.send, to: key)
        try await rig.harness.until { _ in await rig.live(key)?.settles == 1 }
        try await rig.harness.until { harness in await harness.state.preferences.askedNotifications }
        #expect(await rig.harness.presentation.notificationOffer == nil)
        #expect(!rig.notifications.calls.contains(.requestPermission))
    }
}

/// What the seeded runs found (`SeededRun`), each as the engine alone, on
/// what the test names.
@Suite("What the seeded runs found", .timeLimit(.minutes(10)))
struct FoundBySeededRuns {
    private let app = ChatApp(storage: ChatStorage(base: nil), preferences: MemoryPreferenceStore(), platform: .macos)
    private let agent: JSONValue = ["ref": .string("\(Kinds.agent)/\(helper)")]

    private func held(_ ask: ChatAppAsk) -> Bool { if case .notifyHeld = ask { true } else { false } }

    /// The screen that says the sign-in ended stands over every chat, and
    /// no view says its end left the screen: the second is not counted
    /// under it (seed 1107).
    @Test func aChatUnderTheScreenThatSaysTheSignInEndedIsNotRead() throws {
        let key = ChildKey("k")
        var state = signedInState()
        state.recordsRevision = 8
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        state.chats = [thread(["lastMessage": message("m2")]).record]
        let rows = ["m1", "m2"].map { Record(id: $0, kind: Kinds.message, properties: ["role": "assistant", "content": .string($0)]) }
        state.transcripts["t1"] = .init(records: rows, turns: [], version: 3, full: false)
        state.conversations[key] = .init(seed: .init(thread: "t1", agent: helper, draft: "", listen: false), thread: "t1", agent: helper)
        state.scenes["s"] = .init(current: key)
        state.accessEnded = true
        state = try app.apply(.init(.endShown(scene: "s", key: key, shown: true)), to: state)
        #expect(state.seeing.isEmpty && state.sights.isEmpty)
        #expect(!state.asks.contains { if case .sight = $0 { true } else { false } })
        // Signed in again, with the chat still at its end.
        state.accessEnded = false
        state = try app.apply(.init(.endShown(scene: "s", key: key, shown: true)), to: state)
        #expect(state.asks.contains(.sight(thread: "t1", message: "m2")))
    }

    /// A launch that does not know yet that the server keeps marks notifies
    /// by the device's own map. The first marks it learns take that back
    /// where they say the reply was read (seeds 1174, 20163).
    @Test func theFirstMarksTakeBackWhatTheDevicesMapNotified() throws {
        var state = signedInState()
        state.baselined = true
        let settled = thread(["lastMessage": message("m2"), "lastReply": "Hello", "agent": agent]).record
        state = try app.apply(.init(.threadsRead(.init(chats: [settled], automated: []))), to: state)
        #expect(state.asks.contains { if case .notify = $0 { true } else { false } })
        state = try app.apply(.init(.featuresRead(agents: 3, records: 8)), to: state)
        #expect(state.asks.contains(.readMarks(["t1"])))
        let read = try app.apply(
            .init(.marksRead(.init(threads: ["t1"], records: [markRecord("t1", readThrough: "m2", version: 1)]))), to: state)
        #expect(read.asks.contains(.cancelGroup("t1")))
        // Read by nobody: it stands.
        let unread = try app.apply(.init(.marksRead(.init(threads: ["t1"], records: []))), to: state)
        #expect(!unread.asks.contains(.cancelGroup("t1")))
    }

    /// A read of the marks alone brings one ahead of the row held: another
    /// device read a reply this one has no row of. The chat is not called
    /// unread for the older message, its row is read again by a read of
    /// its own, and what that read brings is told by the rule (seeds 1058,
    /// 1131).
    @Test func aMarkAheadOfTheRowHeldBringsNoDotBack() throws {
        var state = signedInState()
        state.recordsRevision = 8
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        state.chats = [thread(["lastMessage": message("m1")]).record]
        #expect(!state.unread(ChatThread(record: state.chats[0])))
        let ahead = ChatAppStep.Marks(threads: ["t1"], records: [markRecord("t1", readThrough: "m2", version: 2, readAt: "2026-10-31T10:05:00Z")])
        state = try app.apply(.init(.marksRead(ahead)), to: state)
        #expect(state.marks?["t1"]?.readThrough == "m2" && state.rowsBehind == ["t1"])
        #expect(!state.unread(ChatThread(record: state.chats[0])))
        #expect(state.asks.contains(.rereadBehind(thread: "t1", mark: 2)))

        // The row it speaks of arrives, by any read: read.
        var newer = thread(["lastMessage": message("m2"), "finishedAt": "2026-10-31T10:04:00Z"]).record
        newer.version = 4
        let caught = try app.apply(.init(.threadsRead(.init(chats: [newer], automated: []))), to: state)
        #expect(caught.rowsBehind.isEmpty && !caught.unread(ChatThread(record: caught.chats[0])))

        // A list read that started before the mark was learned lands the
        // older row, with the older mark it read: no dot comes back.
        let old = ChatAppStep.Marks(threads: ["t1"], records: [markRecord("t1", readThrough: "m1", version: 1)])
        let stale = try app.apply(.init(.threadsRead(.init(chats: state.chats, automated: [], marks: old))), to: state)
        #expect(stale.marks?["t1"]?.readThrough == "m2" && stale.rowsBehind == ["t1"])
        #expect(!stale.unread(ChatThread(record: stale.chats[0])))
        // Nor by one row read on its own before it.
        let fetched = try app.apply(.init(.threadFetched(state.chats[0])), to: state)
        #expect(fetched.rowsBehind == ["t1"] && !fetched.unread(ChatThread(record: fetched.chats[0])))

        // The read made for it answers the same row: the position was
        // taken by a device that held an older row itself, and the chat is
        // unread. One made for an earlier mark settles nothing.
        let earlier = try app.apply(.init(.rowReread(thread: "t1", state.chats[0], mark: 1)), to: state)
        #expect(earlier.rowsBehind == ["t1"])
        let same = try app.apply(.init(.rowReread(thread: "t1", state.chats[0], mark: 2)), to: state)
        #expect(same.rowsBehind.isEmpty && same.unread(ChatThread(record: same.chats[0])))
        // One that failed is asked for again at the next chance.
        let again = try app.apply(.init(.phase(.active)), to: state)
        #expect(again.asks.contains(.rereadBehind(thread: "t1", mark: 2)))

        // A position from before the row settled is behind it, and unread
        // at once.
        var behind = signedInState()
        behind.recordsRevision = 8
        behind.marks = [:]
        behind.chats = [thread(["lastMessage": message("m2")]).record]
        let older = ChatAppStep.Marks(threads: ["t1"], records: [markRecord("t1", readThrough: "m1", version: 1, readAt: "2026-10-31T09:00:00Z")])
        behind = try app.apply(.init(.marksRead(older)), to: behind)
        #expect(behind.rowsBehind.isEmpty && behind.unread(ChatThread(record: behind.chats[0])))
    }

    /// A thread opened on its own is in no list the app reads: the row a
    /// mark is ahead of is read by itself there too.
    @Test func aThreadInNoListIsReadAgainByItself() throws {
        var state = signedInState()
        state.recordsRevision = 8
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1)]
        state.others = ["t1": thread(["lastMessage": message("m1"), "mode": "scheduled"]).record]
        let ahead = ChatAppStep.Marks(threads: ["t1"], records: [markRecord("t1", readThrough: "m2", version: 2, readAt: "2026-10-31T10:05:00Z")])
        state = try app.apply(.init(.marksRead(ahead)), to: state)
        #expect(state.rowsBehind == ["t1"] && state.asks.contains(.rereadBehind(thread: "t1", mark: 2)))
        // A list read, which does not hold it, leaves it waiting.
        state = try app.apply(.init(.threadsRead(.init(chats: [], automated: []))), to: state)
        #expect(state.rowsBehind == ["t1"])
        var newer = thread(["lastMessage": message("m2"), "mode": "scheduled", "finishedAt": "2026-10-31T10:04:00Z"]).record
        newer.version = 4
        state = try app.apply(.init(.rowReread(thread: "t1", newer, mark: 2)), to: state)
        #expect(state.rowsBehind.isEmpty && state.others["t1"]?.version == 4)
    }

    /// A list read that started before a chat existed lands without its
    /// row, and the next one brings the row back beside a mark that reads
    /// it: the reply notified in between is taken back all the same (seed
    /// 5138).
    @Test func aReplyNotifiedIsTakenBackWhenItsRowComesBackRead() throws {
        var state = signedInState()
        state.recordsRevision = 8
        state.baselined = true
        state.marks = ["t1": .none]
        let settled = thread(["lastMessage": message("m2"), "lastReply": "Hello", "agent": agent]).record
        state = try app.apply(.init(.threadsRead(.init(chats: [settled], automated: []))), to: state)
        #expect(state.asks.contains(where: held) && state.announced == ["t1"])
        state = try app.apply(.init(.threadsRead(.init(chats: [], automated: []))), to: state)
        #expect(state.thread("t1") == nil && !state.asks.contains(.cancelGroup("t1")))
        let marks = ChatAppStep.Marks(threads: ["t1"], records: [markRecord("t1", readThrough: "m2", version: 1)])
        state = try app.apply(.init(.threadsRead(.init(chats: [settled], automated: [], marks: marks))), to: state)
        #expect(state.asks.contains(.cancelGroup("t1")) && state.announced.isEmpty)
    }

    /// A call that failed before any reply moves no `lastMessage` (the
    /// server passes over a message no reply came after), so the chat is
    /// not unread: the failure is notified all the same, and not where the
    /// position was taken after the thread settled.
    @Test func aFailedCallIsNotifiedThoughTheChatIsNotUnread() throws {
        var failed = thread(["status": "error", "lastMessage": message("m1"), "agent": agent, "finishedAt": "2026-10-31T10:00:05Z"]).record
        failed.version = 4
        var state = signedInState()
        state.recordsRevision = 8
        state.baselined = true
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 1, readAt: Timestamp.parse("2026-10-31T09:00:00Z"))]
        let told = try app.apply(.init(.threadsRead(.init(chats: [failed], automated: []))), to: state)
        #expect(!told.unread(ChatThread(record: failed)) && told.asks.contains(where: held))
        // Somebody looked at the chat after it failed.
        state.marks = ["t1": ThreadMark(readThrough: "m1", version: 2, readAt: Timestamp.parse("2026-10-31T10:01:00Z"))]
        let quiet = try app.apply(.init(.threadsRead(.init(chats: [failed], automated: []))), to: state)
        #expect(!quiet.asks.contains(where: held))
    }
}
