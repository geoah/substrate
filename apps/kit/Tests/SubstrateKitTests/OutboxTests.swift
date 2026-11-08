import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateLocal
import SubstrateTesting
import Testing

// Ported from WorkoutsSync's outbox tests: the ones that hold the queue
// whatever an app writes through it. Workouts' plan of a session's writes
// (OutboxPlan) stays there.

private let entryKind = "example.com/notes/entry"
private let noteKind = "example.com/notes/note"

private func path(_ kind: String, _ id: String) -> String { "/api/v1/\(kind)/\(id)" }

private func answered(_ version: Int) -> String { #"{"id":"x","kind":"k","properties":{},"version":\#(version)}"# }

private func writer(_ server: ScriptedServer) -> SubstrateClient {
    var client = SubstrateClient(server: URL(string: "http://localhost:8080")!, token: "substrate_tok_test", transport: server)
    client.pause = { _ in }
    return client
}

private func openOutbox(_ root: URL = temporaryFolder()) throws -> Outbox {
    Outbox(folder: try RepositoryFolder(root: root))
}

private func entry(
    _ method: OutboxEntry.Method, _ kind: String, _ id: String, batch: String? = nil, guarded: Bool = false,
    properties: JSONObject = [:]
) -> OutboxEntry {
    OutboxEntry(method: method, kind: kind, recordID: id, properties: properties, guarded: guarded, batch: batch)
}

/// Takes away the right to read `file`, answering whether a read now fails:
/// a process as root reads it whatever its mode, and a test there cannot
/// make the device fail a read this way.
func refuseReads(of file: URL) -> Bool {
    try? FileManager.default.setAttributes([.posixPermissions: 0], ofItemAtPath: file.path)
    return (try? Data(contentsOf: file)) == nil
}

func allowReads(of file: URL) {
    try? FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
}

private func edit(_ id: String, _ patch: JSONObject, base: Int) -> OutboxEntry {
    OutboxEntry(method: .patch, kind: noteKind, recordID: id, properties: patch, guarded: true, baseVersion: base)
}

/// A write refused at its `revision`, as the server words a stale delivery.
private let staleRevision = #"""
    {"error":{"code":"validation","message":"substrate: validation failed",\#
    "problems":["props.revision: 3 is below the stored revision 4; a rewrite never lowers it, so this is a stale delivery"],\#
    "problemDetails":[{"path":"props.revision","message":"3 is below the stored revision 4; a rewrite never lowers it, so this is a stale delivery"}]}}
    """#

private func stored(_ kind: String, _ id: String, _ properties: String, version: Int) -> String {
    #"{"id":"\#(id)","kind":"\#(kind)","properties":\#(properties),"version":\#(version)}"#
}

/// A create's `ifVersion: 0` met a row.
private let taken = #"{"error":{"code":"conflict","message":"version conflict: ifVersion 0, stored 2"}}"#

private func ifVersion(_ sent: ScriptedServer.Sent) -> JSONValue? {
    guard case let .object(body)? = sent.body else { return nil }
    return body["ifVersion"]
}

@Suite("The outbox", .timeLimit(.minutes(10)))
struct OutboxTests {
    @Test func aConflictOnAGuardedPatchReadsTheRecordAndSendsTheChangeAgainOnTop() async throws {
        let server = ScriptedServer()
        server.answer("GET", path(entryKind, "s1"), 200, #"{"id":"s1","kind":"\#(entryKind)","properties":{},"version":5}"#)
        server.on { sent in
            guard sent.method == "PATCH" else { return nil }
            if case let .object(body)? = sent.body, body["ifVersion"] == .number(5) { return (200, answered(6)) }
            return (409, #"{"error":{"code":"conflict","message":"version conflict: ifVersion 1, stored 5"}}"#)
        }
        server.on { $0.method == "PUT" ? (200, answered(1)) : nil }
        let outbox = try openOutbox()
        try await outbox.enqueue([
            entry(.put, entryKind, "s1", batch: "s1"),
            entry(.patch, entryKind, "s1", batch: "s1", guarded: true, properties: ["notes": .string("x")]),
        ])
        #expect(await outbox.flush(writer(server)) == .sent)
        // The create answered version 1, which somebody moved to 5: the
        // first patch is refused, the record read, and the patch sent again.
        #expect(
            server.methodsAndPaths == [
                "PUT \(path(entryKind, "s1"))", "PATCH \(path(entryKind, "s1"))", "GET \(path(entryKind, "s1"))",
                "PATCH \(path(entryKind, "s1"))",
            ])
        #expect(await outbox.version(of: entryKind, "s1") == 6)
    }

    @Test func aRefusalAtTheRevisionWhereTheStoredRecordHoldsALaterOneIsANewerWriteLandedFirstAndIsDropped() async throws {
        let server = ScriptedServer()
        server.answer("PUT", path(entryKind, "a"), 422, staleRevision)
        server.answer("GET", path(entryKind, "a"), 200, stored(entryKind, "a", #"{"revision":4}"#, version: 7))
        let outbox = try openOutbox()
        try await outbox.enqueue([entry(.put, entryKind, "a", batch: "s", properties: ["revision": 3])])
        #expect(await outbox.flush(writer(server)) == .sent)
        #expect(server.methodsAndPaths == ["PUT \(path(entryKind, "a"))", "GET \(path(entryKind, "a"))"])
        #expect(await outbox.entries.isEmpty)
        #expect(await outbox.landed.isEmpty)
    }

    @Test func aRefusalAtTheRevisionWhereTheStoredRecordHoldsNoLaterOneIsParked() async throws {
        let server = ScriptedServer()
        server.answer("PUT", path(entryKind, "a"), 422, staleRevision)
        server.answer("GET", path(entryKind, "a"), 200, stored(entryKind, "a", #"{"revision":3}"#, version: 7))
        let outbox = try openOutbox()
        try await outbox.enqueue([entry(.put, entryKind, "a", batch: "s", properties: ["revision": 3])])
        guard case .refused = await outbox.flush(writer(server)) else {
            Issue.record("not refused")
            return
        }
        #expect(await outbox.parked.map(\.recordID) == ["a"])
    }

    @Test func aRefusalNamingTheRevisionInItsWordsAloneIsParkedUnread() async throws {
        let server = ScriptedServer()
        server.answer(
            "PUT", path(entryKind, "a"), 422,
            #"{"error":{"code":"validation","message":"revision 3 is below the stored revision 4"}}"#)
        let outbox = try openOutbox()
        try await outbox.enqueue([entry(.put, entryKind, "a", batch: "s", properties: ["revision": 3])])
        guard case .refused = await outbox.flush(writer(server)) else {
            Issue.record("not refused")
            return
        }
        #expect(server.methodsAndPaths == ["PUT \(path(entryKind, "a"))"])
        #expect(await outbox.parked.map(\.recordID) == ["a"])
    }

    @Test func aRefusalAtTheRevisionWhoseRecordCannotBeReadWaitsForTheNetwork() async throws {
        let server = ScriptedServer()
        server.answer("PUT", path(entryKind, "a"), 422, staleRevision)
        server.answer("GET", path(entryKind, "a"), -1, "")
        let outbox = try openOutbox()
        try await outbox.enqueue([entry(.put, entryKind, "a", batch: "s", properties: ["revision": 3])])
        guard case .offline = await outbox.flush(writer(server)) else {
            Issue.record("not offline")
            return
        }
        #expect(await outbox.entries.map(\.parked) == [false])
    }

    @Test func withoutTheNetworkNothingIsLostAndTheQueueSurvivesARelaunch() async throws {
        let server = ScriptedServer()
        server.on { _ in (-1, "") }
        let root = temporaryFolder()
        let first = try openOutbox(root)
        try await first.enqueue((1 ... 5).map { entry(.put, entryKind, "e\($0)", batch: "g") })
        guard case .offline = await first.flush(writer(server)) else {
            Issue.record("not offline")
            return
        }
        #expect(await first.entries.count == 5)
        #expect(try await openOutbox(root).entries.count == 5)
    }

    @Test func aRefusalParksTheWriteHoldsTheRestOfItsBatchAndLetsAnotherGo() async throws {
        let server = ScriptedServer()
        server.answer(
            "PUT", path(entryKind, "bad"), 422,
            #"{"error":{"code":"validation","message":"substrate: validation failed","problems":["props.sport: expected one of strength"]}}"#)
        server.on { $0.method == "PUT" ? (200, answered(1)) : nil }
        let outbox = try openOutbox()
        try await outbox.enqueue([
            entry(.put, entryKind, "bad", batch: "bad"), entry(.put, entryKind, "bad-1", batch: "bad"),
            entry(.put, entryKind, "good", batch: "good"),
        ])
        guard case let .refused(words) = await outbox.flush(writer(server)) else {
            Issue.record("not refused")
            return
        }
        #expect(words.contains("props.sport"))
        #expect(await outbox.entries.map(\.recordID) == ["bad", "bad-1"])
        #expect(await outbox.parked.map(\.recordID) == ["bad"])
        #expect(!server.methodsAndPaths.contains("PUT \(path(entryKind, "bad-1"))"))
        #expect(server.methodsAndPaths.contains("PUT \(path(entryKind, "good"))"))
        // Parked, it is not sent again on its own, and the state still says so.
        let before = server.sent.count
        guard case .refused = await outbox.flush(writer(server)) else {
            Issue.record("a parked write stopped being reported")
            return
        }
        #expect(server.sent.count == before)
        // Retried, it goes; discarded, its batch goes with it.
        let parked = try #require(await outbox.parked.first)
        try await outbox.discard(parked.id)
        #expect(await outbox.entries.isEmpty)
    }

    @Test func aRefusedTokenStopsTheRunAndAsksToSignInKeepingEverything() async throws {
        let server = ScriptedServer()
        server.on { _ in (401, #"{"error":{"code":"auth","message":"invalid token"}}"#) }
        let outbox = try openOutbox()
        try await outbox.enqueue([entry(.put, entryKind, "a"), entry(.put, entryKind, "b"), entry(.put, entryKind, "c")])
        #expect(await outbox.flush(writer(server)) == .signedOut)
        // The first refused, the rest are never sent, and all three wait.
        #expect(server.methodsAndPaths == ["PUT \(path(entryKind, "a"))"])
        #expect(await outbox.entries.map(\.recordID) == ["a", "b", "c"])
    }

    @Test func aDiscardDropsTheBatchAndABatchTheServerHoldsIsPurgedAtItsVersion() async throws {
        let outbox = try openOutbox()
        let purge = entry(.purge, entryKind, "s", batch: "s", guarded: true)
        try await outbox.enqueue([entry(.put, entryKind, "s", batch: "s"), entry(.put, entryKind, "s-1", batch: "s")])
        #expect(try await outbox.discard(batch: "s", purge: purge) == false)
        #expect(await outbox.entries.isEmpty)

        let server = ScriptedServer()
        server.on { $0.method == "PUT" ? (200, answered(3)) : nil }
        server.on { $0.method == "DELETE" ? (200, "{}") : nil }
        try await outbox.enqueue([entry(.put, entryKind, "s", batch: "s")])
        await outbox.flush(writer(server))
        try await outbox.purge(batch: "s", with: purge)
        #expect(await outbox.flush(writer(server)) == .sent)
        let sent = try #require(server.sent.last)
        #expect(sent.method == "DELETE")
        #expect(sent.query?.contains("purge=true") == true)
        #expect(sent.query?.contains("ifVersion=3") == true)
        #expect(await outbox.version(of: entryKind, "s") == nil)
    }

    @Test func aRestorableDeleteIsADeleteWithoutPurgeAndAConflictSendsItAgainWithoutAVersion() async throws {
        let server = ScriptedServer()
        server.on { $0.method == "PUT" ? (200, answered(3)) : nil }
        server.on { sent in
            guard sent.method == "DELETE" else { return nil }
            return sent.query?.contains("ifVersion") == true
                ? (409, #"{"error":{"code":"conflict","message":"version conflict"}}"#) : (200, "{}")
        }
        let outbox = try openOutbox()
        try await outbox.enqueue([entry(.put, entryKind, "s", batch: "s")])
        await outbox.flush(writer(server))
        try await outbox.purge(batch: "s", with: entry(.delete, entryKind, "s", batch: "s", guarded: true))
        #expect(await outbox.flush(writer(server)) == .sent)
        let deletes = server.sent.filter { $0.method == "DELETE" }
        #expect(deletes.map(\.query) == ["ifVersion=3", nil])
        #expect(await outbox.version(of: entryKind, "s") == nil)
        #expect(await outbox.entries.isEmpty)
    }

    @Test func aFileThatCannotBeReadIsSetAsideNotEmptied() async throws {
        let root = temporaryFolder()
        let folder = try RepositoryFolder(root: root)
        try folder.write(Data("{".utf8), to: "outbox.json", in: .outbox)
        let outbox = Outbox(folder: folder)
        #expect(await outbox.unreadable?.area == .outbox)
        #expect(await outbox.entries.isEmpty)
        #expect(folder.setAside.count == 1)
    }

    /// A file there that the device could not read (a lock, an I/O error)
    /// is neither set aside nor written over: the queue refuses every
    /// change, and says why, until a read again succeeds, and then every
    /// write the file held is still there, with the new one after them.
    @Test func aFileTheDeviceCannotReadIsNeverWrittenOver() async throws {
        let root = temporaryFolder()
        try await openOutbox(root).enqueue([entry(.put, entryKind, "a"), entry(.put, entryKind, "b")])
        let folder = try RepositoryFolder(root: root)
        let file = folder.url("outbox.json", in: .outbox)
        let held = try Data(contentsOf: file)
        guard refuseReads(of: file) else { return }

        let outbox = Outbox(folder: folder)
        #expect(await outbox.unavailable != nil)
        #expect(await outbox.summary.unavailable != nil)
        #expect(await outbox.unreadable == nil)
        await #expect(throws: OutboxNotSaved.self) { try await outbox.enqueue([entry(.put, entryKind, "c")]) }
        await #expect(throws: OutboxNotSaved.self) { try await outbox.saw(["\(entryKind)/a": 1]) }
        #expect(await outbox.flush(writer(ScriptedServer())) == .idle)
        #expect(folder.setAside.isEmpty)

        allowReads(of: file)
        #expect(try Data(contentsOf: file) == held)
        try await outbox.enqueue([entry(.put, entryKind, "c")])
        #expect(await outbox.unavailable == nil)
        #expect(await outbox.entries.map(\.recordID) == ["a", "b", "c"])
        #expect(await Outbox(folder: folder).entries.map(\.recordID) == ["a", "b", "c"])
    }

    /// A reset forgets the queue, and a file the device could not read is
    /// set aside for a look first, never written over.
    @Test func aResetSetsAsideAFileTheDeviceCannotRead() async throws {
        let root = temporaryFolder()
        try await openOutbox(root).enqueue([entry(.put, entryKind, "a")])
        let folder = try RepositoryFolder(root: root)
        let file = folder.url("outbox.json", in: .outbox)
        let held = try Data(contentsOf: file)
        guard refuseReads(of: file) else { return }
        let outbox = Outbox(folder: folder)
        try await outbox.reset()
        let aside = try #require(await outbox.unreadable)
        allowReads(of: folder.url(aside.name, in: .outbox))
        #expect(try folder.read(aside.name, in: .outbox) == held)
        try await outbox.enqueue([entry(.put, entryKind, "b")])
        #expect(await Outbox(folder: folder).entries.map(\.recordID) == ["b"])
    }

    @Test func anEntryWorkoutsWroteReadsItsSessionAsItsBatch() throws {
        let json = #"{"id":"6E1C3A4E-3D1B-4B8E-9E55-2F7A2B0C9D11","method":"PUT","kind":"k","recordID":"r","session":"s1"}"#
        let read = try JSONDecoder().decode(OutboxEntry.self, from: Data(json.utf8))
        #expect(read.batch == "s1")
        #expect(!read.parked)
    }
}

@Suite("The outbox while a write is in flight", .timeLimit(.minutes(10)))
struct OutboxInFlightTests {
    @Test func aBatchPurgedWhileAnothersCreateIsInFlightLosesNoneOfTheOthersWrites() async throws {
        let server = ScriptedServer()
        let held = server.hold("PUT", path(entryKind, "a"))
        server.answer("PUT", path(entryKind, "b"), 422, #"{"error":{"code":"validation","message":"props.sport: no"}}"#)
        server.on { $0.method == "PUT" ? (200, answered(1)) : nil }
        server.on { $0.method == "DELETE" ? (200, "{}") : nil }
        let outbox = try openOutbox()
        try await outbox.enqueue([entry(.put, entryKind, "b", batch: "b"), entry(.put, entryKind, "b-1", batch: "b")])
        await outbox.flush(writer(server))
        try await outbox.enqueue([entry(.put, entryKind, "a", batch: "a")] + (1 ... 5).map { entry(.put, entryKind, "a-\($0)", batch: "a") })

        let flush = Task { await outbox.flush(writer(server)) }
        await held.arrived()
        try await outbox.purge(batch: "b", with: entry(.purge, entryKind, "b", batch: "b", guarded: true))
        held.release()
        #expect(await flush.value == .sent)
        #expect(await outbox.entries.isEmpty)
        #expect(server.methodsAndPaths.filter { $0.hasPrefix("PUT \(path(entryKind, "a-"))") }.count == 5)
        #expect(server.methodsAndPaths.last == "DELETE \(path(entryKind, "b"))")
    }

    @Test func aBatchDiscardedWhileItsCreateIsInFlightIsPurgedAfterAtTheVersionItAnswered() async throws {
        let server = ScriptedServer()
        let held = server.hold("PUT", path(entryKind, "a"))
        server.on { $0.method == "PUT" ? (200, answered(4)) : nil }
        server.on { $0.method == "DELETE" ? (200, "{}") : nil }
        let outbox = try openOutbox()
        try await outbox.enqueue([entry(.put, entryKind, "a", batch: "a"), entry(.put, entryKind, "a-1", batch: "a")])
        let flush = Task { await outbox.flush(writer(server)) }
        await held.arrived()
        #expect(try await outbox.discard(batch: "a", purge: entry(.purge, entryKind, "a", batch: "a", guarded: true)))
        held.release()
        #expect(await flush.value == .sent)
        #expect(server.methodsAndPaths == ["PUT \(path(entryKind, "a"))", "DELETE \(path(entryKind, "a"))"])
        #expect(server.sent.last?.query?.contains("ifVersion=4") == true)
        #expect(await outbox.entries.isEmpty)
    }

    @Test func aResetWhileAWriteIsInFlightEmptiesTheQueueForGood() async throws {
        let server = ScriptedServer()
        let held = server.hold("PUT", path(entryKind, "a"))
        server.on { $0.method == "PUT" ? (200, answered(1)) : nil }
        let root = temporaryFolder()
        let outbox = try openOutbox(root)
        try await outbox.enqueue([entry(.put, entryKind, "a", batch: "a"), entry(.put, entryKind, "a-1", batch: "a")])
        let flush = Task { await outbox.flush(writer(server)) }
        await held.arrived()
        try await outbox.reset()
        held.release()
        _ = await flush.value
        #expect(await outbox.entries.isEmpty)
        #expect(server.methodsAndPaths == ["PUT \(path(entryKind, "a"))"])
        #expect(try await openOutbox(root).entries.isEmpty)
    }
}

@Suite("Guarded edits in the outbox", .timeLimit(.minutes(10)))
struct OutboxEditTests {
    let notePath = path(noteKind, "day-1")

    @Test func anEditIsAPatchOfWhatChangedUnderTheVersionTheOwnerEdited() async throws {
        let server = ScriptedServer()
        server.answer("PATCH", notePath, 200, answered(5))
        let outbox = try openOutbox()
        try await outbox.edit(edit("day-1", ["name": .string("Renamed"), "folder": .null], base: 4))
        #expect(await outbox.flush(writer(server)) == .sent)
        guard case let .object(body)? = server.sent.first?.body, case let .object(properties)? = body["properties"] else {
            Issue.record("no body")
            return
        }
        #expect(body["ifVersion"] == .number(4))
        #expect(properties["name"] == .string("Renamed") && properties["folder"] == .null)
        #expect(await outbox.version(of: noteKind, "day-1") == 5)
    }

    @Test func aConflictIsParkedForTheOwnerAndKeepMineSendsItWithoutAVersion() async throws {
        let server = ScriptedServer()
        server.answer("PATCH", notePath, 409, #"{"error":{"code":"conflict","message":"version conflict: ifVersion 4, stored 6"}}"#)
        let outbox = try openOutbox()
        try await outbox.edit(edit("day-1", ["name": .string("Renamed")], base: 4))
        guard case .refused = await outbox.flush(writer(server)) else {
            Issue.record("not refused")
            return
        }
        #expect(server.methodsAndPaths == ["PATCH \(notePath)"], "one try, no read, no second write")
        let parked = try #require(await outbox.parked.first)
        #expect(parked.conflict)
        _ = await outbox.flush(writer(server))
        #expect(server.sent.count == 1)

        let again = ScriptedServer()
        again.answer("PATCH", notePath, 200, answered(7))
        try await outbox.resolve(parked.id, keepMine: true)
        #expect(await outbox.flush(writer(again)) == .sent)
        guard case let .object(body)? = again.sent.first?.body else {
            Issue.record("no body")
            return
        }
        #expect(body["ifVersion"] == nil)
        #expect(await outbox.entries.isEmpty)
    }

    @Test func useTheirsDropsTheEdit() async throws {
        let server = ScriptedServer()
        server.answer("PATCH", notePath, 409, #"{"error":{"code":"conflict","message":"version conflict"}}"#)
        let outbox = try openOutbox()
        try await outbox.edit(edit("day-1", ["name": .string("Renamed")], base: 4))
        _ = await outbox.flush(writer(server))
        let parked = try #require(await outbox.parked.first)
        try await outbox.resolve(parked.id, keepMine: false)
        #expect(await outbox.entries.isEmpty)
    }

    @Test func offlineEditsWaitOnDiskAndGoAsOnePatchUnderTheFirstOnesVersion() async throws {
        let root = temporaryFolder()
        let offline = ScriptedServer()
        offline.on { _ in (-1, "") }
        var outbox = try openOutbox(root)
        try await outbox.edit(edit("day-1", ["name": .string("First"), "notes": .string("a")], base: 4))
        guard case .offline = await outbox.flush(writer(offline)) else {
            Issue.record("not offline")
            return
        }
        try await outbox.edit(edit("day-1", ["name": .string("Second"), "folder": .null], base: 4))
        #expect(await outbox.entries.count == 1)

        outbox = try openOutbox(root)
        #expect(await outbox.waiting(kind: noteKind)["day-1"]?.properties["name"] == .string("Second"))

        let server = ScriptedServer()
        server.answer("PATCH", notePath, 200, answered(5))
        #expect(await outbox.flush(writer(server)) == .sent)
        #expect(server.sent.count == 1)
        guard case let .object(body)? = server.sent.first?.body, case let .object(properties)? = body["properties"] else {
            Issue.record("no body")
            return
        }
        #expect(body["ifVersion"] == .number(4))
        #expect(properties["name"] == .string("Second") && properties["notes"] == .string("a") && properties["folder"] == .null)
    }

    @Test func anEditAfterTheAppsOwnWriteAssertsThatWritesVersion() async throws {
        let server = ScriptedServer()
        server.on { sent in
            guard sent.method == "PATCH", case let .object(body)? = sent.body else { return nil }
            return body["ifVersion"] == .number(4) ? (200, answered(5)) : body["ifVersion"] == .number(5) ? (200, answered(6)) : (409, "{}")
        }
        let outbox = try openOutbox()
        try await outbox.edit(edit("day-1", ["name": .string("One")], base: 4))
        #expect(await outbox.flush(writer(server)) == .sent)
        try await outbox.edit(edit("day-1", ["name": .string("Two")], base: 4))
        #expect(await outbox.flush(writer(server)) == .sent)
        #expect(await outbox.version(of: noteKind, "day-1") == 6)
    }

    @Test func afterKeepMineALaterEditMadeOnTheOldVersionStillMeetsAConflict() async throws {
        let server = ScriptedServer()
        server.on { sent in
            guard sent.method == "PATCH", case let .object(body)? = sent.body else { return nil }
            switch body["ifVersion"] {
            case nil: return (200, answered(6))
            case let .number(version)? where version.exactInt == 6: return (200, answered(7))
            default: return (409, #"{"error":{"code":"conflict","message":"version conflict"}}"#)
            }
        }
        let outbox = try openOutbox()
        try await outbox.edit(edit("day-1", ["slots": .array([])], base: 4))
        _ = await outbox.flush(writer(server))
        let parked = try #require(await outbox.parked.first)
        try await outbox.resolve(parked.id, keepMine: true)
        try await outbox.edit(edit("day-1", ["notes": .string("mine")], base: 4))
        #expect(await outbox.entries.count == 2)
        _ = await outbox.flush(writer(server))
        let second = try #require(await outbox.parked.first)
        #expect(second.conflict)
        #expect(second.properties?["notes"] == .string("mine"))
    }

    @Test func anEditNeverJoinsAParkedOne() async throws {
        let server = ScriptedServer()
        server.answer("PATCH", notePath, 409, #"{"error":{"code":"conflict","message":"version conflict"}}"#)
        let outbox = try openOutbox()
        try await outbox.edit(edit("day-1", ["name": .string("One")], base: 4))
        _ = await outbox.flush(writer(server))
        try await outbox.edit(edit("day-1", ["name": .string("Two")], base: 4))
        #expect(await outbox.entries.count == 2)
    }

    @Test func aPurgeOutsideAnyBatchIsWithdrawnUntilItGoes() async throws {
        let outbox = try openOutbox()
        try await outbox.edit(edit("day-1", ["name": .string("One")], base: 4))
        try await outbox.delete(OutboxEntry(method: .purge, kind: noteKind, recordID: "day-1"))
        #expect(await outbox.entries.map(\.method) == [.purge])
        #expect(await outbox.waitingDeletes == ["\(noteKind)/day-1"])
        try await outbox.withdrawDelete(noteKind, "day-1")
        #expect(await outbox.entries.isEmpty)
    }
}

/// A create is sent once: a retry whose earlier try landed, its answer lost,
/// meets the row and reads it, so whatever another device wrote since stands,
/// and an edit queued on the create goes on the version the create made.
@Suite("Creates in the outbox", .timeLimit(.minutes(10)))
struct OutboxCreateTests {
    let notePath = path(noteKind, "day-1")
    let writes = RecordWrites(mint: { UUID() })

    /// The note as the server holds it.
    func held(_ name: String, version: Int) -> String {
        stored(noteKind, "day-1", #"{"name":"\#(name)"}"#, version: version)
    }

    @Test func aCreateCreatesOnlyAndAnEditOfARecordNotSentYetIsOnVersionZero() {
        let create = writes.create(noteKind, "day-1", properties: ["name": "Milk"])
        let edit = writes.edit(noteKind, "day-1", properties: ["notes": "2 l"], seen: nil)
        #expect(create.entries.map(\.baseVersion) == [0])
        #expect(edit.entries.map(\.baseVersion) == [0])
    }

    @Test func aCreateWhoseEarlierTryLandedCountsAsLandedAndIsNeverSentAgain() async throws {
        // The earlier try landed and its answer was lost; another device
        // renamed the note since.
        let server = ScriptedServer()
        server.answer("PUT", notePath, 409, taken)
        server.answer("GET", notePath, 200, held("Oat milk", version: 2))
        let outbox = try openOutbox()
        let create = writes.create(noteKind, "day-1", properties: ["name": "Milk"])
        try await outbox.enqueue(create.entries)
        #expect(await outbox.flush(writer(server)) == .sent)
        #expect(server.methodsAndPaths == ["PUT \(notePath)", "GET \(notePath)"])
        #expect(server.sent.first.flatMap(ifVersion) == .number(0))
        #expect(await outbox.entries.isEmpty)
        #expect(await outbox.landed == Set(create.entries.map(\.id)))
        #expect(await outbox.version(of: noteKind, "day-1") == 2)
    }

    @Test func anEditQueuedOnACreateGoesOnTheVersionTheCreateAnswered() async throws {
        let server = ScriptedServer()
        server.answer("PUT", notePath, 200, answered(1))
        server.on { sent in
            guard sent.method == "PATCH" else { return nil }
            return ifVersion(sent) == .number(1) ? (200, answered(2)) : (409, taken)
        }
        let outbox = try openOutbox()
        try await outbox.enqueue(writes.create(noteKind, "day-1", properties: ["name": "Milk"]).entries)
        try await outbox.enqueue(writes.edit(noteKind, "day-1", properties: ["notes": "2 l"], seen: nil).entries)
        #expect(await outbox.flush(writer(server)) == .sent)
        #expect(server.methodsAndPaths == ["PUT \(notePath)", "PATCH \(notePath)"])
        #expect(await outbox.version(of: noteKind, "day-1") == 2)
    }

    @Test func anEditQueuedOnACreateWhoseEarlierTryLandedGoesOnTheVersionTheCreateMade() async throws {
        let server = ScriptedServer()
        server.answer("PUT", notePath, 409, taken)
        server.answer("GET", notePath, 200, held("Milk", version: 1))
        server.on { sent in
            guard sent.method == "PATCH" else { return nil }
            return ifVersion(sent) == .number(1) ? (200, answered(2)) : (409, taken)
        }
        let outbox = try openOutbox()
        try await outbox.enqueue(writes.create(noteKind, "day-1", properties: ["name": "Milk"]).entries)
        try await outbox.enqueue(writes.edit(noteKind, "day-1", properties: ["notes": "2 l"], seen: nil).entries)
        #expect(await outbox.flush(writer(server)) == .sent)
        #expect(server.methodsAndPaths == ["PUT \(notePath)", "GET \(notePath)", "PATCH \(notePath)"])
        #expect(await outbox.version(of: noteKind, "day-1") == 2)
    }

    @Test func anEditQueuedOnACreateParksWhereAnotherDeviceMovedTheRecordBeforeTheRetry() async throws {
        let server = ScriptedServer()
        server.answer("PUT", notePath, 409, taken)
        server.answer("GET", notePath, 200, held("Oat milk", version: 2))
        server.answer("PATCH", notePath, 409, #"{"error":{"code":"conflict","message":"version conflict: ifVersion 1, stored 2"}}"#)
        let outbox = try openOutbox()
        try await outbox.enqueue(writes.create(noteKind, "day-1", properties: ["name": "Milk"]).entries)
        let edit = writes.edit(noteKind, "day-1", properties: ["name": "Whole milk"], seen: nil)
        try await outbox.enqueue(edit.entries)
        guard case .refused = await outbox.flush(writer(server)) else {
            Issue.record("not refused")
            return
        }
        #expect(server.methodsAndPaths == ["PUT \(notePath)", "GET \(notePath)", "PATCH \(notePath)"])
        #expect(server.sent.last.flatMap(ifVersion) == .number(1))
        let parked = await outbox.parked
        #expect(parked.map(\.id) == edit.entries.map(\.id))
        #expect(parked.first?.conflict == true)
    }

    @Test func aCreateRefusedWithAnotherCodeWaitsUnread() async throws {
        let server = ScriptedServer()
        server.answer("PUT", notePath, 409, #"{"error":{"code":"parked","message":"the retried delivery failed again"}}"#)
        let outbox = try openOutbox()
        try await outbox.enqueue(writes.create(noteKind, "day-1", properties: ["name": "Milk"]).entries)
        _ = await outbox.flush(writer(server))
        #expect(server.methodsAndPaths == ["PUT \(notePath)"])
        #expect(await outbox.entries.count == 1)
        #expect(await outbox.landed.isEmpty)
    }

    @Test func anEditAfterACreateWentQueuesBehindItAndGoesOnTheVersionTheCreateMade() async throws {
        // Workouts' routine create: outside any batch, where an edit joins a
        // create that never went.
        let create = OutboxEntry(
            method: .put, kind: noteKind, recordID: "day-1", properties: ["name": "Copy"], guarded: true, baseVersion: 0)
        let gone = ScriptedServer()
        gone.on { _ in (-1, "") }
        let outbox = try openOutbox()
        try await outbox.enqueue([create])
        guard case .offline = await outbox.flush(writer(gone)) else {
            Issue.record("not offline")
            return
        }
        // The create may have landed with its answer lost: the rename is
        // not folded into it.
        try await outbox.edit(edit("day-1", ["name": "Mine"], base: 0))
        #expect(await outbox.entries.map(\.method) == [.put, .patch])
        let server = ScriptedServer()
        server.answer("PUT", notePath, 409, taken)
        server.answer("GET", notePath, 200, held("Copy", version: 1))
        server.on { sent in
            guard sent.method == "PATCH" else { return nil }
            return ifVersion(sent) == .number(1) ? (200, answered(2)) : (409, taken)
        }
        #expect(await outbox.flush(writer(server)) == .sent)
        guard case let .object(body)? = server.sent.last?.body, case let .object(properties)? = body["properties"] else {
            Issue.record("no patch")
            return
        }
        #expect(properties["name"] == "Mine")
    }

    @Test func anEditAfterTheDevicesOwnChangeOfACreateGoesOnThatChange() async throws {
        let server = ScriptedServer()
        server.answer("PUT", notePath, 200, answered(1))
        server.on { sent in
            guard sent.method == "PATCH" else { return nil }
            let version = ifVersion(sent)
            return version == nil ? (200, answered(2)) : version == .number(2) ? (200, answered(3)) : (409, taken)
        }
        let outbox = try openOutbox()
        try await outbox.enqueue(writes.create(noteKind, "day-1", properties: ["name": "Milk"]).entries)
        try await outbox.enqueue(writes.change(noteKind, "day-1", properties: ["position": 2], version: nil).entries)
        try await outbox.enqueue(writes.edit(noteKind, "day-1", properties: ["notes": "2 l"], seen: nil).entries)
        #expect(await outbox.flush(writer(server)) == .sent)
        #expect(server.sent.last.flatMap(ifVersion) == .number(2))
        #expect(await outbox.version(of: noteKind, "day-1") == 3)
    }

    @Test func anEditOfACreateAnotherDeviceChangedStillParksAfterTheDevicesOwnChange() async throws {
        let server = ScriptedServer()
        server.answer("PUT", notePath, 200, answered(1))
        server.on { sent in
            guard sent.method == "PATCH" else { return nil }
            return ifVersion(sent) == nil ? (200, answered(3)) : (409, taken)
        }
        let outbox = try openOutbox()
        try await outbox.enqueue(writes.create(noteKind, "day-1", properties: ["name": "Milk"]).entries)
        #expect(await outbox.flush(writer(server)) == .sent)
        // Another device renamed it, and this one read that.
        try await outbox.saw([RecordFold.key(noteKind, "day-1"): 2])
        try await outbox.enqueue(writes.change(noteKind, "day-1", properties: ["position": 2], version: nil).entries)
        try await outbox.enqueue(writes.edit(noteKind, "day-1", properties: ["name": "Whole milk"], seen: nil).entries)
        guard case .refused = await outbox.flush(writer(server)) else {
            Issue.record("not refused")
            return
        }
        #expect(server.sent.last.flatMap(ifVersion) == .number(1))
        #expect(await outbox.parked.first?.conflict == true)
    }

    @Test func aCreateWhoseRowWasDeletedSinceCountsAsLandedAndItsEditParksOnThe404() async throws {
        let server = ScriptedServer()
        server.answer("PUT", notePath, 409, taken)
        server.answer("GET", notePath, 404, #"{"error":{"code":"not_found","message":"record day-1"}}"#)
        server.answer(
            "PATCH", notePath, 404, #"{"error":{"code":"not_found","message":"record day-1 is deleted; a put restores it"}}"#)
        let outbox = try openOutbox()
        let create = writes.create(noteKind, "day-1", properties: ["name": "Milk"])
        try await outbox.enqueue(create.entries)
        let edit = writes.edit(noteKind, "day-1", properties: ["notes": "2 l"], seen: nil)
        try await outbox.enqueue(edit.entries)
        guard case .refused = await outbox.flush(writer(server)) else {
            Issue.record("not refused")
            return
        }
        #expect(server.methodsAndPaths == ["PUT \(notePath)", "GET \(notePath)", "PATCH \(notePath)"])
        #expect(await outbox.landed == Set(create.entries.map(\.id)))
        let parked = await outbox.parked
        #expect(parked.map(\.id) == edit.entries.map(\.id))
        #expect(parked.first?.conflict == false)
    }
}

/// Workouts' format 1 spelling, as its `LegacyKinds` respells it: the
/// package was `~/training` on the device.
private let legacy = OutboxUpgrade(respell: respellKind, respellProperties: respellObject)

private func respellKind(_ text: String) -> String {
    text.hasPrefix("~/training") ? "substrate.reamde.dev/training" + text.dropFirst("~/training".count) : text
}

private func respellObject(_ object: JSONObject) -> JSONObject {
    var out = JSONObject()
    for (key, value) in object { out[key] = respellValue(value) }
    return out
}

private func respellValue(_ value: JSONValue) -> JSONValue {
    switch value {
    case let .string(text): .string(respellKind(text))
    case let .array(items): .array(items.map(respellValue))
    case let .object(object): .object(respellObject(object))
    default: value
    }
}

@Suite("The outbox's files and reports", .timeLimit(.minutes(10)))
struct OutboxFileTests {
    let session = "01a16363-cf80-73ed-85fd-704f6ce38d73"

    func legacyFolder() throws -> RepositoryFolder {
        let folder = try RepositoryFolder(root: temporaryFolder())
        let fixture = try #require(Bundle.module.url(forResource: "legacy-outbox", withExtension: "json", subdirectory: "Resources"))
        try folder.write(Data(contentsOf: fixture), to: "outbox.json", in: .outbox)
        return folder
    }

    @Test func aFormatOneFileIsReadThroughTheAppsUpgradeAndWrittenBackOnce() async throws {
        let folder = try legacyFolder()
        let outbox = Outbox(folder: folder, upgrade: legacy)
        #expect(await outbox.unreadable == nil)
        let entries = await outbox.entries
        #expect(entries.count == 22)
        #expect(entries.first?.kind == "substrate.reamde.dev/training/session" && entries.first?.recordID == session)
        // Workouts' `session` is the batch.
        #expect(entries.dropFirst().dropLast().allSatisfy { $0.kind == "substrate.reamde.dev/training/set" && $0.batch == session })
        // What the old build knew of `~/training` is not known of the moved
        // kinds: Finish goes without its guard, and no version or chain stays.
        #expect(entries.last?.method == .patch && entries.last?.guarded == false && entries.last?.baseVersion == nil)
        #expect(await outbox.versions.isEmpty)
        #expect(await outbox.version(of: "substrate.reamde.dev/training/exercise", "leg-press-machine") == nil)

        let rewritten = String(decoding: try #require(try folder.read("outbox.json", in: .outbox)), as: UTF8.self)
        #expect(rewritten.contains(#""format":2"#))
        #expect(!rewritten.contains("~/"))
        #expect(rewritten.contains(#""versions":{}"#) && rewritten.contains(#""chains":{}"#))
        // Written with both names, so a build of Workouts from before the
        // kit's outbox reads its sessions back.
        #expect(rewritten.contains(#""batch":"\#(session)""#) && rewritten.contains(#""session":"\#(session)""#))

        let again = Outbox(folder: folder, upgrade: legacy)
        #expect(await again.entries == entries)
        #expect(String(decoding: try #require(try folder.read("outbox.json", in: .outbox)), as: UTF8.self) == rewritten)
    }

    @Test func aMovedRecordsWritesTheAppDerivesAgainAreDroppedAndTheRestIsLeftAlone() async throws {
        let folder = try RepositoryFolder(root: temporaryFolder())
        let file = #"""
            {"entries": [
              {"id": "00000000-0000-0000-0000-000000000001", "method": "PUT", "kind": "~/training/session", "recordID": "s", "session": "s"},
              {"id": "00000000-0000-0000-0000-000000000002", "method": "DELETE", "kind": "~/training/session", "recordID": "t", "session": "t"},
              {"id": "00000000-0000-0000-0000-000000000003", "method": "PATCH", "kind": "~/training/routine", "recordID": "r",
               "properties": {"notes": "kept"}, "guarded": true, "baseVersion": 3},
              {"id": "00000000-0000-0000-0000-000000000004", "method": "PATCH", "kind": "substrate.reamde.dev/training/routine", "recordID": "q",
               "guarded": true, "baseVersion": 4}
            ],
            "versions": {"~/training/routine/r": 3, "substrate.reamde.dev/training/routine/q": 4},
            "chains": {"~/training/routine/r": {"3": 4}, "substrate.reamde.dev/training/routine/q": {"4": 5}}}
            """#
        try folder.write(Data(file.utf8), to: "outbox.json", in: .outbox)
        let upgrade = OutboxUpgrade(
            respell: respellKind, respellProperties: respellObject,
            rederives: { $0.batch != nil && $0.method != OutboxEntry.Method.purge })
        let outbox = Outbox(folder: folder, upgrade: upgrade)

        let entries = await outbox.entries
        // The session's write goes, its purge stays.
        #expect(entries.map(\OutboxEntry.recordID) == ["t", "r", "q"])
        #expect(entries[0].kind == "substrate.reamde.dev/training/session" && entries[0].method == .purge)
        #expect(entries[1].kind == "substrate.reamde.dev/training/routine" && !entries[1].guarded && entries[1].baseVersion == nil)
        #expect(entries[1].properties == ["notes": .string("kept")])
        // A record already in this build's spelling keeps everything.
        #expect(entries[2].guarded && entries[2].baseVersion == 4)
        #expect(await outbox.versions == ["substrate.reamde.dev/training/routine/q": 4])
        #expect(await outbox.version(of: "substrate.reamde.dev/training/routine", "r") == nil)
    }

    @Test func aFormatOneFileWithoutAnUpgradeIsSetAsideNeverDropped() async throws {
        let folder = try legacyFolder()
        let outbox = Outbox(folder: folder)
        #expect(await outbox.entries.isEmpty)
        let aside = try #require(await outbox.unreadable)
        #expect(try folder.read(aside.name, in: .outbox) != nil)
        #expect(await outbox.summary.unreadable == aside)
    }

    @Test func theQueueReportsEachChangeAndOnlyTheAppsMoveTheRevision() async throws {
        let server = ScriptedServer()
        server.on { $0.method == "PUT" ? (200, answered(1)) : nil }
        let outbox = try openOutbox()
        var reports = await outbox.changes().makeAsyncIterator()
        #expect(await reports.next() == OutboxSummary())
        try await outbox.enqueue([entry(.put, entryKind, "a")])
        let queued = try #require(await reports.next())
        #expect(queued.revision == 1 && queued.waiting == 1 && queued.sendable == 1 && !queued.flushing)
        await outbox.flush(writer(server))
        var last = queued
        while last.flushing || last.waiting > 0 {
            last = try #require(await reports.next())
        }
        // The flush moved the counts and the state, never the revision.
        #expect(last.state == .sent && last.revision == 1)
    }
}
