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

private let online = LinkSnapshot(phase: .signedIn, server: URL(string: "https://ada.example.com")!, repository: "ada.example.com",
                                  online: true)

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
}

@Suite("Linked records", .timeLimit(.minutes(10)))
struct LinkedRecordsTests {
    @Test func signedInItReadsThenFollowsAndSignedOutItStops() {
        var stepper = Stepper()
        #expect(stepper.step(.loaded([], cursor: nil, queue: [])).isEmpty)
        #expect(stepper.step(.linked(online)) == [.read])
        let cursor = WatchCursor(seq: 3, generation: "g")
        #expect(stepper.step(.listed([note("n1", "Milk")], cursor: cursor)) == [.follow(cursor)])
        #expect(stepper.state.records.count == 1)
        // Opened, interrupted and opened again: the follower does it, and
        // nothing starts another.
        #expect(stepper.step(.watchOpened(cursor)).isEmpty)
        #expect(stepper.step(.watchInterrupted("The connection dropped.")).isEmpty)
        #expect(stepper.step(.watchOpened(cursor)).isEmpty)
        #expect(stepper.step(.linked(LinkSnapshot(phase: .signedOut))) == [.stopFollowing])
        #expect(stepper.state.phase == .idle)
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

    @Test func aWatchStoppedForGoodOpensAgainOnlyWhenItsReasonMayBeGone() {
        var stepper = Stepper()
        let cursor = WatchCursor(seq: 1, generation: "g")
        _ = stepper.step(.loaded([], cursor: cursor, queue: []))
        #expect(stepper.step(.linked(online)) == [.follow(cursor)])
        _ = stepper.step(.watchEnded(.refused, "The server is restarting."))
        var moved = online
        moved.waiting = 2
        #expect(stepper.step(.linked(moved)).isEmpty)
        #expect(stepper.step(.foregrounded) == [.follow(cursor)])
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
        let status = StatusPresentation(title: "Substrate", symbol: .upToDate, tone: .positive, sentence: "Up to date.",
                                        packages: [PackageLine(id: "example.com/notes", row: LabeledValue("Notes package", "Version 1"))],
                                        actions: [], busy: false, footer: nil, refused: nil)
        let row = SettingsPresenter.status(status)[0].rows.first { $0.id == .status(.package("example.com/notes")) }
        #expect(row?.label == "Notes package")
        #expect(row?.detail == "example.com/notes")
    }

    @Test func aRefusedWriteIsANoticeOutsideSettings() {
        let parked = ParkedWrite(id: UUID(), method: .patch, kind: noteKind, recordID: "n1", words: "no", conflict: true)
        let status = StatusPresenter.present(.refused(waiting: 1, reason: "no"), words: StatusWords(app: "Notes", device: "this iPhone", kept: "notes"),
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
