import Foundation
import SubstrateKit
import Testing
import WorkoutsCore
@testable import WorkoutsSync

private let notFound = #"{"error":{"code":"not_found","message":"not found"}}"#
private let codec = RecordCodec()

private func entry(_ method: OutboxEntry.Method, _ kind: String, _ id: String, session: String?, guarded: Bool = false) -> OutboxEntry {
    OutboxEntry(method: method, kind: kind, recordID: id, properties: [:], guarded: guarded, session: session)
}

/// The outbox while a write is on the network (PR #1100 review): the actor
/// takes other calls at every await, so the queue changes under a flush.
@Suite("Outbox in flight")
struct OutboxInFlightTests {
    @Test("a session deleted while another's create is in flight loses none of the other's writes")
    func dropWhileSending() async throws {
        let server = ScriptedServer()
        let gate = server.hold("PUT", "/api/v1/ada.example.com/training/session/a")
        server.answer("PUT", "/api/v1/ada.example.com/training/session/b", 422, #"{"error":{"code":"validation","message":"props.sport: no"}}"#)
        server.on { $0.method == "PUT" ? (200, record(1)) : nil }
        server.on { $0.method == "DELETE" ? (200, "{}") : nil }
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        // B's writes wait at the head of the queue, parked by a refusal.
        try await outbox.enqueue([entry(.put, codec.kinds.session, "b", session: "b"), entry(.put, codec.kinds.set, "b-1", session: "b")])
        await outbox.flush(api(server))
        try await outbox.enqueue([entry(.put, codec.kinds.session, "a", session: "a")] + (1 ... 5).map { entry(.put, codec.kinds.set, "a-\($0)", session: "a") })

        let flush = Task { await outbox.flush(api(server)) }
        await gate.arrived()
        // B deleted from History while A's create is on the wire.
        try await outbox.purge(session: SessionID(rawValue: "b"), codec: codec)
        gate.release()
        #expect(await flush.value == .sent)
        #expect(await outbox.entries.isEmpty)
        let sentSets = server.methodsAndPaths.filter { $0.hasPrefix("PUT /api/v1/ada.example.com/training/set/a-") }
        #expect(sentSets.count == 5, "a set of A was lost: \(server.methodsAndPaths)")
        #expect(server.methodsAndPaths.last == "DELETE /api/v1/ada.example.com/training/session/b")
    }

    @Test("a session discarded while its create is in flight is purged after the create lands, at the version it answered")
    func discardWhileItsCreateIsInFlight() async throws {
        let server = ScriptedServer()
        let gate = server.hold("PUT", "/api/v1/ada.example.com/training/session/a")
        server.on { $0.method == "PUT" ? (200, record(4)) : nil }
        server.on { $0.method == "DELETE" ? (200, "{}") : nil }
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.enqueue([entry(.put, codec.kinds.session, "a", session: "a"), entry(.put, codec.kinds.set, "a-1", session: "a")])
        let flush = Task { await outbox.flush(api(server)) }
        await gate.arrived()
        // The server holds nothing yet, but the create on the wire may land.
        #expect(try await outbox.discard(session: SessionID(rawValue: "a"), codec: codec))
        gate.release()
        #expect(await flush.value == .sent)
        #expect(server.methodsAndPaths == [
            "PUT /api/v1/ada.example.com/training/session/a",
            "DELETE /api/v1/ada.example.com/training/session/a",
        ])
        #expect(server.sent.last?.query?.contains("ifVersion=4") == true)
        #expect(await outbox.entries.isEmpty)
        #expect(await outbox.version(of: codec.kinds.session, "a") == nil)
    }

    @Test("a reset while a write is in flight empties the queue for good and does not trap")
    func resetWhileSending() async throws {
        let server = ScriptedServer()
        let gate = server.hold("PUT", "/api/v1/ada.example.com/training/session/a")
        server.on { $0.method == "PUT" ? (200, record(1)) : nil }
        let directory = temporaryDirectory()
        let outbox = Outbox(directory: directory, writingOptions: .atomic)
        try await outbox.enqueue([entry(.put, codec.kinds.session, "a", session: "a"), entry(.put, codec.kinds.set, "a-1", session: "a")])
        let flush = Task { await outbox.flush(api(server)) }
        await gate.arrived()
        try await outbox.reset()
        gate.release()
        _ = await flush.value
        #expect(await outbox.entries.isEmpty)
        #expect(await outbox.versions.isEmpty, "the landed create came back after the reset")
        #expect(server.methodsAndPaths == ["PUT /api/v1/ada.example.com/training/session/a"])
        #expect(await Outbox(directory: directory, writingOptions: .atomic).entries.isEmpty)
    }

    @Test("a write queued while a flush runs goes in the same run")
    func enqueueWhileSending() async throws {
        let server = ScriptedServer()
        let gate = server.hold("PUT", "/api/v1/ada.example.com/training/session/a")
        server.on { $0.method == "PUT" ? (200, record(1)) : nil }
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.enqueue([entry(.put, codec.kinds.session, "a", session: "a")])
        let flush = Task { await outbox.flush(api(server)) }
        await gate.arrived()
        try await outbox.enqueue([entry(.put, codec.kinds.exercise, "row", session: nil)])
        gate.release()
        #expect(await flush.value == .sent)
        #expect(server.methodsAndPaths == [
            "PUT /api/v1/ada.example.com/training/session/a",
            "PUT /api/v1/ada.example.com/training/exercise/row",
        ])
    }
}

/// The outbox's disk (PR #1100 review): a change it cannot keep is said,
/// never swallowed, and a file it cannot read is set aside, never replaced
/// by an empty queue.
@Suite("Outbox on disk")
struct OutboxDiskTests {
    @Test("a change that cannot be saved is refused and leaves the queue as it was")
    func saveFails() async throws {
        let directory = temporaryDirectory()
        let outbox = Outbox(directory: directory, writingOptions: .atomic)
        try await outbox.enqueue([entry(.put, codec.kinds.session, "a", session: "a")])
        // The file's place is taken by a folder: every write fails.
        try FileManager.default.removeItem(at: directory.appendingPathComponent(Outbox.fileName))
        try FileManager.default.createDirectory(at: directory.appendingPathComponent(Outbox.fileName), withIntermediateDirectories: true)
        await #expect(throws: OutboxNotSaved.self) {
            try await outbox.enqueue([entry(.put, codec.kinds.set, "a-1", session: "a")])
        }
        await #expect(throws: OutboxNotSaved.self) {
            try await outbox.purge(session: SessionID(rawValue: "a"), codec: codec)
        }
        #expect(await outbox.entries.map(\.recordID) == ["a"])
        #expect(await outbox.saveProblem != nil)
    }

    @Test("an unreadable queue is set aside and said, not replaced by an empty one")
    func unreadable() async throws {
        let directory = temporaryDirectory()
        let url = directory.appendingPathComponent(Outbox.fileName)
        try Data(#"{"format":1,"entries":[{"id":"#.utf8).write(to: url)
        let outbox = Outbox(directory: directory, writingOptions: .atomic, now: Date(timeIntervalSince1970: 1_800_000_000))
        #expect(await outbox.entries.isEmpty)
        let unreadable = try #require(await outbox.unreadable)
        #expect(unreadable.source == "outbox.unreadable-1800000000.json")
        #expect(FileManager.default.fileExists(atPath: directory.appendingPathComponent(unreadable.source).path))
    }

    @Test("a queue from before parking reads its refused writes as parked")
    func olderFile() async throws {
        let directory = temporaryDirectory()
        let held = OutboxEntry(method: .put, kind: codec.kinds.session, recordID: "a", properties: [:], session: "a")
        var json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(held)) as! [String: Any]
        json["parked"] = nil
        json["lastError"] = "props.sport: no"
        let file: [String: Any] = ["format": 1, "entries": [json], "versions": [String: Int]()]
        try JSONSerialization.data(withJSONObject: file).write(to: directory.appendingPathComponent(Outbox.fileName))
        #expect(await Outbox(directory: directory, writingOptions: .atomic).parked.map(\.recordID) == ["a"])
    }
}

/// A write the server refuses for itself (PR #1100 review): parked with its
/// words, holding only its session, until the owner retries or discards it.
@Suite("Parked writes")
struct ParkedTests {
    @Test("a Finish for a session purged on the server is parked, retried on request, and discarded with its session")
    func purgedOnTheServer() async throws {
        let server = ScriptedServer()
        server.answer("GET", "/api/v1/ada.example.com/training/session/a", 404, notFound)
        server.on { $0.method == "PUT" ? (200, record(1)) : nil }
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.enqueue([
            entry(.patch, codec.kinds.session, "a", session: "a", guarded: true),
            entry(.put, codec.kinds.set, "a-late", session: "a"),
            entry(.put, codec.kinds.session, "b", session: "b"),
        ])
        guard case let .refused(words) = await outbox.flush(api(server)) else { Issue.record("not refused"); return }
        #expect(words.contains("not on your substrate"))
        let parked = try #require(await outbox.parked.first)
        #expect(parked.recordID == "a")
        #expect(server.methodsAndPaths.contains("PUT /api/v1/ada.example.com/training/session/b"))
        #expect(!server.methodsAndPaths.contains { $0.contains("PATCH") }, "a guarded patch went out for a record that is gone")

        try await outbox.retry(parked.id)
        await outbox.flush(api(server))
        #expect(server.methodsAndPaths.filter { $0 == "GET /api/v1/ada.example.com/training/session/a" }.count == 2)

        try await outbox.discard(try #require(await outbox.parked.first).id)
        #expect(await outbox.entries.isEmpty)
        #expect(await outbox.flush(api(server)) == .sent)
    }
}

/// The history exchange (PR #1100 review): a session this phone uploaded and
/// the server no longer holds is the owner's delete; a session deleted on the
/// phone is never imported again; a server edit reaches the phone.
@Suite("History exchange")
struct HistoryExchangeTests {
    func finished() throws -> SessionRecords {
        let rehome = Rehome(authority: recordedAuthority)
        func records(_ name: String) throws -> [FlatRecord] {
            try JSONDecoder().decode(WorkoutsAPI.Page.self, from: Fixtures.data(name)).records.map { try WorkoutsAPI.record($0, rehome: rehome) }
        }
        return try #require(codec.sessionRecords(from: records("list-sessions") + records("list-sets")).first)
    }

    /// The server's copy of a session, every record at `version`.
    func snapshot(_ records: SessionRecords, version: Int = 1, status: SessionStatus = .finished) -> ServerSnapshot {
        var session = codec.encode(records.session)
        session.properties["status"] = .string(status.rawValue)
        session.version = version
        let sets = records.sets.map { set -> FlatRecord in
            var record = codec.encode(set)
            record.version = version
            return record
        }
        return ServerSnapshot(sessions: [session.id: session], sets: [session.id: sets])
    }

    func versions(_ records: SessionRecords, _ version: Int) -> [String: Int] {
        var out = ["\(codec.kinds.session)/\(records.id.rawValue)": version]
        for set in records.sets { out["\(codec.kinds.set)/\(set.id.rawValue)"] = version }
        return out
    }

    @Test("a session uploaded here and gone from the server is removed from the phone, never sent again")
    func deletedOnTheServer() throws {
        let mine = try finished()
        let plan = HistoryExchange.plan(server: ServerSnapshot(), local: [mine], queued: [], versions: versions(mine, 3), tombstones: [], codec: codec)
        #expect(plan.removals == [mine.id])
        #expect(plan.uploads.isEmpty)
    }

    @Test("a session this repository never held is uploaded")
    func neverSent() throws {
        let mine = try finished()
        let plan = HistoryExchange.plan(server: ServerSnapshot(), local: [mine], queued: [], versions: [:], tombstones: [], codec: codec)
        #expect(plan.uploads.map(\.id) == [mine.id])
        #expect(plan.removals.isEmpty)
    }

    @Test("a session deleted on the phone is purged on the server, not imported, while its purge is not queued")
    func deletedOnThePhone() throws {
        let theirs = try finished()
        let id = theirs.id.rawValue
        let plan = HistoryExchange.plan(server: snapshot(theirs), local: [], queued: [], versions: [:], tombstones: [id], codec: codec)
        #expect(plan.imports.isEmpty)
        #expect(plan.purges == [theirs.id])
        // With its purge waiting, nothing more.
        let waiting = HistoryExchange.plan(server: snapshot(theirs), local: [], queued: [id], versions: [:], tombstones: [id], codec: codec)
        #expect(waiting.isEmpty)
    }

    @Test("a finished session the phone lacks is imported, with the versions it was read at")
    func imported() throws {
        let theirs = try finished()
        let plan = HistoryExchange.plan(server: snapshot(theirs, version: 7), local: [], queued: [], versions: [:], tombstones: [], codec: codec)
        #expect(plan.imports.map(\.id) == [theirs.id])
        #expect(plan.seen == versions(theirs, 7))
    }

    @Test("a server edit to a session on the phone is merged; one the phone wrote itself is not")
    func merged() throws {
        let mine = try finished()
        let same = HistoryExchange.plan(server: snapshot(mine, version: 2), local: [mine], queued: [], versions: versions(mine, 2), tombstones: [], codec: codec)
        #expect(same.isEmpty)
        // The owner corrected a set's load on the server.
        var edited = snapshot(mine, version: 2)
        let id = mine.id.rawValue
        edited.sets[id]?[0].version = 3
        edited.sets[id]?[0].properties["reps"] = .number(99)
        let plan = HistoryExchange.plan(server: edited, local: [mine], queued: [], versions: versions(mine, 2), tombstones: [], codec: codec)
        let merged = try #require(plan.merges.first)
        #expect(merged.sets.contains { $0.reps == 99 })
        #expect(plan.seen["\(codec.kinds.set)/\(mine.sets.sorted { $0.id.rawValue < $1.id.rawValue }.first!.id.rawValue)"] != nil)
        // A session with a write waiting stays the phone's.
        let waiting = HistoryExchange.plan(server: edited, local: [mine], queued: [id], versions: versions(mine, 2), tombstones: [], codec: codec)
        #expect(waiting.isEmpty)
    }

    @Test("a session finished here and held unfinished there is finished again above the server's revision")
    func finishedAgain() throws {
        let mine = try finished()
        var server = snapshot(mine, version: 4, status: .active)
        server.sessions[mine.id.rawValue]?.properties["revision"] = .number(9)
        let plan = HistoryExchange.plan(server: server, local: [mine], queued: [], versions: versions(mine, 2), tombstones: [], codec: codec)
        let finish = try #require(plan.finishes.first)
        #expect(finish.method == .patch && finish.guarded)
        #expect(finish.properties?["revision"] == .number(10))
        #expect(plan.uploads.isEmpty && plan.removals.isEmpty && plan.merges.isEmpty)
    }
}

/// The package's owner (PR #1100 review): an owner's edit to a kind moves the
/// kind's version, not the package's, so the app compares each kind with
/// what it recorded after its own apply, and a refusal of the server's guard
/// reads as one.
@Suite("Package ownership")
struct PackageOwnershipTests {
    let packagePath = "/api/v1/substrate.reamde.dev/core/package/ada.example.com%2Ftraining"
    let bundlePath = "/api/v1/substrate.reamde.dev/core/bundle/ada.example.com%2Ftraining"

    func kindPath(_ name: String) -> String { "/api/v1/substrate.reamde.dev/core/kind/ada.example.com%2Ftraining%2F\(name)" }

    func server(held: Int?, kindVersion: @escaping @Sendable (String) -> Int, applied: Locked<Int>) -> ScriptedServer {
        let server = ScriptedServer()
        server.on { [packagePath] sent in
            guard sent.method == "GET", sent.path == packagePath else { return nil }
            let version = applied.value > 0 ? (held ?? 0) + 1 : held
            guard let version else { return (404, notFound) }
            return (200, #"{"id":"ada.example.com/training","kind":"substrate.reamde.dev/core/package","properties":{"version":\#(version)},"version":1}"#)
        }
        server.answer("GET", bundlePath, 404, notFound)
        server.on { sent in
            guard sent.method == "GET", sent.path.hasPrefix("/api/v1/substrate.reamde.dev/core/kind/") else { return nil }
            let name = String(sent.path.split(separator: "%2F").last ?? "")
            return (200, #"{"id":"x","kind":"substrate.reamde.dev/core/kind","properties":{},"version":\#(kindVersion(name))}"#)
        }
        server.on { sent in
            guard sent.method == "POST" else { return nil }
            applied.value += 1
            return (200, #"{"records":[]}"#)
        }
        return server
    }

    func manifest(version: Int) throws -> PackageManifest {
        var manifest = try PackageManifest.bundled()
        manifest.version = version
        return manifest
    }

    @Test("an install records each kind's version, and a later upgrade over an owner's kind edit waits for the owner")
    func editedKind() async throws {
        let directory = temporaryDirectory()
        let applied = Locked(0)
        let installing = server(held: nil, kindVersion: { _ in 1 }, applied: applied)
        let first = PackageInstaller(api: api(installing), manifest: try manifest(version: 1), baselineDirectory: directory)
        #expect(try await first.ensure() == .current(held: 1))
        let baseline = try #require(PackageBaseline.load(from: directory))
        #expect(baseline.kinds["ada.example.com/training/set"] == 1)
        #expect(baseline.kinds.count == 5)

        // The owner edited the set kind in the console; this build ships 2.
        let upgraded = Locked(0)
        let edited = server(held: 1, kindVersion: { $0 == "set" ? 2 : 1 }, applied: upgraded)
        let next = PackageInstaller(api: api(edited), manifest: try manifest(version: 2), baselineDirectory: directory)
        #expect(try await next.ensure() == .edited(held: 1, kinds: ["ada.example.com/training/set"]))
        #expect(upgraded.value == 0, "the app applied over the owner's edit")
        // The owner chose to let it.
        #expect(try await next.ensure(overwritingEdits: true) == .current(held: 2))
        #expect(upgraded.value == 1)
    }

    @Test("an upgrade over kinds the app recorded goes through")
    func unchangedKinds() async throws {
        let directory = temporaryDirectory()
        try PackageBaseline(version: 1, kinds: Dictionary(uniqueKeysWithValues: ["exercise", "setup", "routine", "session", "set"].map {
            ("ada.example.com/training/\($0)", 1)
        })).save(to: directory, writingOptions: .atomic)
        let applied = Locked(0)
        let installer = PackageInstaller(api: api(server(held: 1, kindVersion: { _ in 1 }, applied: applied)), manifest: try manifest(version: 2),
                                         baselineDirectory: directory)
        #expect(try await installer.ensure() == .current(held: 2))
    }

    @Test("an apply the server refuses for removing what the package holds reads as refused, in its words")
    func refusedByTheGuard() async throws {
        let server = ScriptedServer()
        server.answer("GET", packagePath, 200, #"{"id":"ada.example.com/training","kind":"substrate.reamde.dev/core/package","properties":{"version":1},"version":1}"#)
        server.answer("GET", bundlePath, 404, notFound)
        server.on { $0.method == "GET" ? (404, notFound) : nil }
        server.answer("POST", "/api/v1/vocabulary/apply", 409,
                      #"{"error":{"code":"conflict","message":"an app's token only adds to a package, and this apply would remove what the repository holds: ada.example.com/training/set: properties.notes"}}"#)
        let installer = PackageInstaller(api: api(server), manifest: try manifest(version: 2), baselineDirectory: temporaryDirectory())
        guard case let .refused(held, reason) = try await installer.ensure() else { Issue.record("not refused"); return }
        #expect(held == 1)
        #expect(reason.contains("properties.notes"))
        #expect(!PackageState.refused(held: 1, reason: reason).writable)
    }
}

@Suite("Tombstones")
struct TombstoneTests {
    @Test("a delete is kept across a relaunch, and an unreadable list is set aside")
    func kept() throws {
        let directory = temporaryDirectory()
        let tombstones = SessionTombstones(directory: directory, writingOptions: .atomic)
        try tombstones.add(SessionID(rawValue: "a"))
        #expect(SessionTombstones(directory: directory, writingOptions: .atomic).contains(SessionID(rawValue: "a")))
        try Data("{".utf8).write(to: directory.appendingPathComponent(SessionTombstones.fileName))
        #expect(SessionTombstones(directory: directory, writingOptions: .atomic).unreadable != nil)
    }
}
