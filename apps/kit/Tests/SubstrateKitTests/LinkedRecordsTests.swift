import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateDevice
import SubstrateLocal
import SubstrateRuntime
import SubstrateScreens
import SubstrateTesting
import Testing

private let noteKind = "example.com/notes/note"

private func note(_ id: String, _ title: String, version: Int = 1) -> Record {
    Record(id: id, kind: noteKind, properties: ["title": .string(title)], version: version)
}

private let online = LinkSnapshot(
    phase: .signedIn, server: URL(string: "https://ada.example.com")!, repository: "ada.example.com",
    online: true)

/// The instant the first refusal in a test is met.
private let refusedAt = Date(timeIntervalSince1970: 1_793_696_400)

/// Why the notes are not being updated once a refusal came within a minute
/// of the last read again.
private let notResumed = "the server could not resume their history even after reading them again"

private func linked() -> LinkedRecords {
    LinkedRecords(kinds: [noteKind], cache: nil, outbox: nil, words: StatusWords(app: "Notes", device: "this iPhone", kept: "notes"))
}

/// The component's events applied by value, each with the effects it asks
/// for, as an app's root runs them.
private struct Stepper {
    let records = linked()
    var state = LinkedRecordsState()

    mutating func step(_ event: LinkedRecords.Event) -> [LinkedRecords.Effect] {
        let previous = state
        state = records.apply(event, to: state)
        return records.effects(of: event, on: state, from: previous)
    }
}

@Suite("The fold", .timeLimit(.minutes(10)))
struct FoldTests {
    @Test func waitingWritesLieOverTheServersCopyAndARefusedOneDoesNot() {
        let key = RecordFold.key(noteKind, "n1")
        let server = [key: note("n1", "Milk")]
        let rename = OutboxEntry(method: .patch, kind: noteKind, recordID: "n1", properties: ["title": "Oat milk"])
        var refused = OutboxEntry(method: .patch, kind: noteKind, recordID: "n1", properties: ["pinned": true])
        refused.parked = true
        let create = OutboxEntry(method: .put, kind: noteKind, recordID: "n2", properties: ["title": "Bread"])
        let folded = RecordFold.fold(server, [rename, refused, create])
        #expect(folded[key]?.properties["title"] == "Oat milk")
        #expect(folded[key]?.properties["pinned"] == nil)
        #expect(folded[RecordFold.key(noteKind, "n2")]?.version == 0)
        let gone = OutboxEntry(method: .delete, kind: noteKind, recordID: "n1")
        #expect(RecordFold.fold(server, [gone])[key] == nil)
        // A patch of a record already gone makes no record.
        #expect(RecordFold.fold([:], [rename])[key] == nil)
    }

    @Test func aCreateOverARecordTheServerHoldsWritesNothingOverIt() {
        let key = RecordFold.key(noteKind, "n1")
        // Its earlier try landed, its answer lost, and another device
        // renamed it since.
        let held = [key: note("n1", "Oat milk", version: 2)]
        let create = RecordWrites(mint: { UUID() }).create(noteKind, "n1", properties: ["title": "Milk"]).entries
        #expect(RecordFold.fold(held, create)[key] == held[key])
        #expect(RecordFold.fold([:], create)[key]?.properties["title"] == "Milk")
    }
}

@Suite("Linked records", .timeLimit(.minutes(10)))
struct LinkedRecordsTests {
    @Test func signedInItReadsThenFollowsAndSignedOutItStops() {
        var stepper = Stepper()
        #expect(stepper.step(.loaded([], cursor: nil, queue: [])).isEmpty)
        #expect(stepper.step(.linked(online)) == [.read(over: [noteKind])])
        let cursor = WatchCursor(seq: 3, generation: "g")
        #expect(stepper.step(.listed([note("n1", "Milk")], cursor: cursor)) == [.follow(cursor, over: [noteKind])])
        #expect(stepper.state.records.count == 1)
        // Opened, interrupted and opened again: the follower does it, and
        // nothing starts another.
        #expect(stepper.step(.watchOpened(cursor)).isEmpty)
        #expect(stepper.step(.watchInterrupted("The connection dropped.")).isEmpty)
        #expect(stepper.step(.watchOpened(cursor)).isEmpty)
        #expect(stepper.step(.linked(LinkSnapshot(phase: .signedOut))) == [.stopFollowing])
        #expect(stepper.state.phase == .idle)
    }

    @Test func aReadIsFollowedFromTheWatchsBookmarkAndOneReadWithoutItIsNot() {
        var stepper = Stepper()
        _ = stepper.step(.loaded([], cursor: nil, queue: []))
        #expect(stepper.step(.linked(online)) == [.read(over: [noteKind])])
        // The watch could not open: the list is shown, and nothing follows it.
        #expect(stepper.step(.listed([note("n1", "Milk")], cursor: nil)).isEmpty)
        #expect(stepper.step(.watchInterrupted("The server could not be reached.")).isEmpty)
        #expect(stepper.state.records.count == 1 && stepper.state.cursor == nil)
        // Open at last, and read again after its bookmark: followed from it.
        let bookmark = WatchCursor(seq: 9, generation: "g")
        #expect(stepper.step(.listed([note("n1", "Oat milk", version: 2)], cursor: bookmark)) == [.follow(bookmark, over: [noteKind])])
        #expect(stepper.state.phase == .following)
        // A relist reads again, even one from the read itself.
        _ = stepper.step(.refreshAsked)
        #expect(stepper.step(.relistNeeded(at: refusedAt)) == [.read(over: [noteKind])])
    }

    @Test func aNewTokenReplacesTheRunningWatchAndRead() {
        var stepper = Stepper()
        let cursor = WatchCursor(seq: 3, generation: "g")
        _ = stepper.step(.loaded([], cursor: cursor, queue: []))
        #expect(stepper.step(.linked(online)) == [.follow(cursor, over: [noteKind])])
        // An approval: a new token, which the old watch does not carry.
        var approved = online
        approved.scopes = ["write:example.com/notes/*", "read:example.com/people/*"]
        #expect(stepper.step(.linked(approved)) == [.follow(cursor, over: [noteKind])])
        #expect(stepper.state.phase == .following)
        // Interrupted, it follows again at once with the new one.
        _ = stepper.step(.watchInterrupted("The connection dropped."))
        approved.accessEnds = Date(timeIntervalSince1970: 2_000_000_000)
        #expect(stepper.step(.linked(approved)) == [.follow(cursor, over: [noteKind])])
        // A read with no cursor yet starts over as a read.
        var reading = Stepper()
        _ = reading.step(.loaded([], cursor: nil, queue: []))
        _ = reading.step(.linked(online))
        #expect(reading.step(.linked(approved)) == [.read(over: [noteKind])])
        // The same token moving nothing else starts nothing.
        var moved = approved
        moved.waiting = 3
        #expect(stepper.step(.linked(moved)).isEmpty)
    }

    @Test func newKindsCloseTheReadAndOpenItAgainKeepingWhatIsStillRead() {
        let taskKind = "example.com/notes/task"
        var stepper = Stepper()
        let cursor = WatchCursor(seq: 3, generation: "g")
        _ = stepper.step(.loaded([note("n1", "Milk")], cursor: cursor, queue: []))
        #expect(stepper.step(.linked(online)) == [.follow(cursor, over: [noteKind])])
        let waiting = OutboxEntry(method: .patch, kind: noteKind, recordID: "n1", properties: ["title": "Oat milk"])
        _ = stepper.step(.wrote(LocalWrite(entries: [waiting])))
        // Over a second kind: read again from the start, the note still shown
        // with its write laid over, the watch replaced by the read.
        #expect(stepper.step(.rekinded([noteKind, taskKind])) == [.read(over: [noteKind, taskKind])])
        #expect(stepper.records.kinds(in: stepper.state) == [noteKind, taskKind])
        #expect(stepper.state.cursor == nil && stepper.state.phase == .reading)
        #expect(stepper.state.records[RecordFold.key(noteKind, "n1")]?.properties["title"] == "Oat milk")
        // A record of the new kind is kept; telling it the same kinds again moves nothing.
        _ = stepper.step(.listed([note("n1", "Milk"), Record(id: "t1", kind: taskKind, properties: [:])], cursor: cursor))
        #expect(stepper.state.records.count == 2)
        #expect(stepper.step(.rekinded([noteKind, taskKind])).isEmpty)
        // Dropping the note kind drops its records, and its write from the
        // state, never from the outbox, which the read does not own.
        _ = stepper.step(.rekinded([taskKind]))
        #expect(stepper.state.records.keys.sorted() == [RecordFold.key(taskKind, "t1")])
        #expect(stepper.state.queue.isEmpty)
    }

    @Test func aReadAgainThatFailedBeforeTheAppClosedIsTriedOnceConnected() {
        var stepper = Stepper()
        let key = RecordFold.key(noteKind, "n1")
        let cursor = WatchCursor(seq: 8, generation: "g")
        _ = stepper.step(.loaded([note("n1", "Milk")], cursor: cursor, stale: [key, "example.com/other/thing/x"], queue: []))
        #expect(stepper.state.stale == [key])
        #expect(stepper.step(.linked(online)) == [.reread([key]), .follow(cursor, over: [noteKind])])
    }

    @Test func aWriteThatLandedIsLaidOverUntilReadAndOneDiscardedIsNot() {
        var stepper = Stepper()
        _ = stepper.step(.loaded([note("n1", "Milk"), note("n2", "Bread")], cursor: nil, queue: []))
        let rename = OutboxEntry(method: .patch, kind: noteKind, recordID: "n1", properties: ["title": "Oat milk"])
        let drop = OutboxEntry(method: .patch, kind: noteKind, recordID: "n2", properties: ["title": "Rye"])
        #expect(stepper.step(.wrote(LocalWrite(entries: [rename]))) == [.enqueue(LocalWrite(entries: [rename]))])
        _ = stepper.step(.wrote(LocalWrite(entries: [drop])))
        _ = stepper.step(.enqueued([rename.id, drop.id], queue: [rename, drop], landed: []))
        // The first landed, the second was discarded: both left the queue.
        let effects = stepper.step(.queueReported([], landed: [rename.id]))
        #expect(stepper.state.records[RecordFold.key(noteKind, "n1")]?.properties["title"] == "Oat milk")
        #expect(stepper.state.records[RecordFold.key(noteKind, "n2")]?.properties["title"] == "Bread")
        #expect(effects == [.reread([RecordFold.key(noteKind, "n1"), RecordFold.key(noteKind, "n2")])])
    }

    @Test func aReadAgainThatFailedIsTriedWhenTheNetworkComesBack() {
        var stepper = Stepper()
        _ = stepper.step(.loaded([], cursor: WatchCursor(seq: 1, generation: "g"), queue: []))
        _ = stepper.step(.linked(online))
        let key = RecordFold.key(noteKind, "n1")
        _ = stepper.step(.rereadFailed([key]))
        var offline = online
        offline.online = false
        _ = stepper.step(.linked(offline))
        #expect(stepper.step(.linked(online)).contains(.reread([key])))
        _ = stepper.step(.changed(key, note("n1", "Milk")))
        #expect(stepper.state.stale.isEmpty)
    }

    @Test func aWatchRefusedAgainAtTheHeadItsReadGaveIsAFailureNotAnotherRead() {
        var stepper = Stepper()
        let saved = WatchCursor(seq: 1, generation: "g")
        _ = stepper.step(.loaded([], cursor: saved, queue: []))
        _ = stepper.step(.linked(online))
        // The saved position is refused: the kinds are read again.
        #expect(stepper.step(.relistNeeded(at: refusedAt)) == [.read(over: [noteKind])])
        let head = WatchCursor(seq: 9, generation: "g2")
        #expect(stepper.step(.listed([note("n1", "Milk")], cursor: head)) == [.follow(head, over: [noteKind])])
        // The watch at the head just read is refused too: no third read.
        #expect(!stepper.step(.relistNeeded(at: refusedAt)).contains(.read(over: [noteKind])))
        #expect(
            stepper.records.notice(stepper.state)?.text
                == "Your notes are not being updated: the server could not resume their history even after reading them again")
        // The app coming to the front tries again, from a read: the head the
        // server refused is never sent again.
        #expect(stepper.state.cursor == nil)
        #expect(stepper.step(.foregrounded) == [.read(over: [noteKind])])
    }

    /// One recovery a minute: the watch opening at the head the read gave
    /// (its bookmark) does not make the allowance whole again, so a refusal
    /// within the minute is the failure, never a second read.
    @Test func aWatchRefusedWithinAMinuteOfItsReadAgainIsAFailureEvenAfterItOpened() {
        var stepper = Stepper()
        _ = stepper.step(.loaded([], cursor: WatchCursor(seq: 1, generation: "g"), queue: []))
        _ = stepper.step(.linked(online))
        #expect(stepper.step(.relistNeeded(at: refusedAt)) == [.read(over: [noteKind])])
        let head = WatchCursor(seq: 9, generation: "g2")
        _ = stepper.step(.listed([note("n1", "Milk")], cursor: head))
        #expect(stepper.step(.watchOpened(head)).isEmpty)
        #expect(stepper.step(.relistNeeded(at: refusedAt.addingTimeInterval(59))).isEmpty)
        #expect(stepper.state.phase == .stopped(.refused, notResumed))
        #expect(stepper.state.cursor == nil)
    }

    /// The minute past, a refusal is a recovery again, whatever happened
    /// between: here the read again's watch never opened.
    @Test func aWatchRefusedAnHourAfterItsReadAgainIsReadAgain() {
        var stepper = Stepper()
        _ = stepper.step(.loaded([], cursor: WatchCursor(seq: 1, generation: "g"), queue: []))
        _ = stepper.step(.linked(online))
        #expect(stepper.step(.relistNeeded(at: refusedAt)) == [.read(over: [noteKind])])
        // Listed while the watch could not open, which it keeps trying.
        _ = stepper.step(.listed([note("n1", "Milk")], cursor: nil))
        _ = stepper.step(.watchInterrupted("The server could not be reached."))
        let later = refusedAt.addingTimeInterval(3600)
        #expect(stepper.step(.relistNeeded(at: later)) == [.read(over: [noteKind])])
        #expect(stepper.state.phase == .reading)
        // The read that recovery started is refused within its minute: the
        // failure, and no read asked for again.
        #expect(stepper.step(.relistNeeded(at: later.addingTimeInterval(30))).isEmpty)
        #expect(stepper.state.phase == .stopped(.refused, notResumed))
    }

    @Test func aWatchStoppedForGoodOpensAgainOnlyWhenItsReasonMayBeGone() {
        var stepper = Stepper()
        let cursor = WatchCursor(seq: 1, generation: "g")
        _ = stepper.step(.loaded([], cursor: cursor, queue: []))
        #expect(stepper.step(.linked(online)) == [.follow(cursor, over: [noteKind])])
        _ = stepper.step(.watchEnded(.refused, "The server is restarting."))
        var moved = online
        moved.waiting = 2
        #expect(stepper.step(.linked(moved)).isEmpty)
        #expect(stepper.step(.foregrounded) == [.follow(cursor, over: [noteKind])])
    }

    @Test func whatTheReadAndTheWatchSayIsALineInTheAppsWords() {
        var stepper = Stepper()
        _ = stepper.step(.loaded([], cursor: nil, queue: []))
        _ = stepper.step(.linked(online))
        #expect(stepper.records.notice(stepper.state) == NoticeLine("Reading your notes…", symbol: .sending, tone: .quiet))
        _ = stepper.step(.readFailed("The server could not be reached.", nil))
        #expect(stepper.records.notice(stepper.state)?.text == "Your notes are not being updated: The server could not be reached.")
        #expect(stepper.records.report(of: .readFailed("", .needsApproval)) == .needsApproval)
        #expect(stepper.records.report(of: .watchEnded(.signedOut, "")) == .accessEnded)
    }

    @Test func theOutboxSaysWhichWritesLanded() async throws {
        let server = ScriptedServer()
        server.on { $0.method == "PUT" ? (200, #"{"id":"n1","kind":"k","properties":{},"version":1}"#) : nil }
        server.on { $0.method == "PATCH" ? (400, #"{"error":{"code":"invalid","message":"no"}}"#) : nil }
        let outbox = Outbox(folder: try RepositoryFolder(root: temporaryFolder()))
        let put = OutboxEntry(method: .put, kind: noteKind, recordID: "n1", properties: ["title": "Milk"], batch: "a")
        let refused = OutboxEntry(method: .patch, kind: noteKind, recordID: "n2", properties: ["title": "Rye"], batch: "b")
        try await outbox.enqueue([put, refused])
        var client = SubstrateClient(server: URL(string: "http://localhost:8080")!, token: "substrate_tok_test", transport: server)
        client.pause = { _ in }
        _ = await outbox.flush(client)
        #expect(await outbox.landed == [put.id])
        try await outbox.discard(refused.id)
        #expect(await outbox.landed == [put.id])
    }
}

@Suite("Settings, composed", .timeLimit(.minutes(10)))
struct SettingsCompositionTests {
    @Test func aPackageRowNamesItsFullReference() {
        let status = StatusPresentation(
            title: "Substrate", symbol: .upToDate, tone: .positive, sentence: "Up to date.",
            packages: [PackageLine(id: "example.com/notes", row: LabeledValue("Notes package", "Version 1"))],
            actions: [], busy: false, footer: nil, refused: nil)
        let row = SettingsPresenter.status(status)[0].rows.first { $0.id == .status(.package("example.com/notes")) }
        #expect(row?.label == "Notes package")
        #expect(row?.detail == "example.com/notes")
    }

    @Test func aRefusedWriteIsANoticeOutsideSettings() {
        let parked = ParkedWrite(id: UUID(), method: .patch, kind: noteKind, recordID: "n1", words: "no", conflict: true)
        let status = StatusPresenter.present(
            .refused(waiting: 1, reason: "no"), words: StatusWords(app: "Notes", device: "this iPhone", kept: "notes"),
            host: "ada.example.com", packages: [], sending: false, waiting: 1, parked: [parked],
            locale: Locale(identifier: "en_US"), timeZone: TimeZone(identifier: "UTC")!)
        #expect(status.notice == NoticeLine("1 change needs you", symbol: .warning, tone: .caution))
    }

    @Test func everySettingsCommandFindsWhatAnswersIt() {
        typealias Route = SettingsRoute<NoApp>
        #expect(Route.route(.perform(row: .repository(.signOut)), settings: { _ in nil }, clearSaved: nil) == .link(.signOut))
        #expect(Route.route(.perform(row: .storage(.clear)), settings: { _ in nil }, clearSaved: nil) == .none)
        #expect(Route.route(.perform(row: .repository(.changeAccess)), settings: { _ in nil }, clearSaved: nil) == .changeAccess)
        #expect(Route.route(.perform(row: .about(.check)), settings: { _ in nil }, clearSaved: nil) == .checkForUpdates)
        let id = UUID()
        #expect(Route.route(.perform(row: .status(.action(.retry(id)))), settings: { _ in nil }, clearSaved: nil) == .link(.retry(id)))
    }

    @Test func aPlanIsReconciledUnderItsPrefixAlone() async {
        let notifications = RecordedNotifications()
        await notifications.schedule(NotificationRequest(id: "notes.old", title: "Old", fireAt: Date(timeIntervalSinceNow: 3600)))
        await notifications.schedule(NotificationRequest(id: "chat.reply", title: "Reply"))
        let plan = [NotificationRequest(id: "notes.new", title: "New", fireAt: Date(timeIntervalSinceNow: 7200))]
        await notifications.reconcile(plan, prefix: "notes.")
        #expect(Set(notifications.pending.keys) == ["notes.new", "chat.reply"])
    }
}
