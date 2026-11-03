import Foundation
import SubstrateKit
import Testing
import WorkoutsCore
@testable import WorkoutsSync

/// A routine edited on the phone: nothing the editor does not show is lost
/// across a read, an edit and the write; the write is a guarded PATCH of
/// what changed; a conflict is parked for the owner, never merged over; an
/// edit made offline waits, and edits queued together go as one.
@Suite("Routine edits")
struct RoutineEditTests {
    let codec = RecordCodec()

    /// The routines as geoah.me's server answered on 2026-11-02:
    /// `floor-first-full-body` with the owner's words replaced (this
    /// repository is public), and `sample-every-feature`. Set
    /// `WORKOUTS_REAL_ROUTINES` to a folder of server answers
    /// (`<id>.json`) to run the same checks on the real records.
    static func serverRecords() throws -> [(name: String, json: WorkoutsCore.JSONValue)] {
        var out: [(String, WorkoutsCore.JSONValue)] = []
        for name in ["routine-floor-first", "routine-sample"] {
            out.append((name, try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: Fixtures.data(name))))
        }
        if let folder = ProcessInfo.processInfo.environment["WORKOUTS_REAL_ROUTINES"] {
            for url in try FileManager.default.contentsOfDirectory(at: URL(fileURLWithPath: folder), includingPropertiesForKeys: nil)
                where url.pathExtension == "json"
            {
                out.append((url.lastPathComponent, try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: Data(contentsOf: url))))
            }
        }
        return out
    }

    /// A server answer with fields this build does not know written at every
    /// level, as the coach or a later package would: on the routine, each
    /// group, each slot and each planned set.
    static func withUnknownFields(_ json: WorkoutsCore.JSONValue) throws -> WorkoutsCore.JSONValue {
        guard case var .object(record) = json, case var .object(properties)? = record["properties"] else { throw CocoaError(.coderReadCorrupt) }
        properties["coachRationale"] = .string("written by the coach")
        if case let .array(groups)? = properties["groups"] {
            properties["groups"] = .array(groups.map { group in
                guard case var .object(object) = group else { return group }
                object["futureGroupField"] = .number(3)
                return .object(object)
            })
        }
        if case let .array(slots)? = properties["slots"] {
            properties["slots"] = .array(slots.map { slot in
                guard case var .object(object) = slot else { return slot }
                object["coachCue"] = .string("cue")
                if case let .array(sets)? = object["sets"] {
                    object["sets"] = .array(sets.map { set in
                        guard case var .object(planned) = set else { return set }
                        planned["futureSetField"] = .object(["unit": .string("x")])
                        return .object(planned)
                    })
                }
                return .object(object)
            })
        }
        record["properties"] = .object(properties)
        return .object(record)
    }

    /// The declared defaults the server stores into every planned set
    /// (`role: working`, `structure: straight`) and the codec leaves out
    /// (§3.4 rule 4), filled in on both sides: the same record either way.
    /// The top-level `title` is the server's, from the kind's
    /// `displayTemplate`; the codec never reads or writes it.
    static func normalized(_ value: WorkoutsCore.JSONValue) -> WorkoutsCore.JSONValue {
        guard case var .object(properties) = value else { return value }
        properties["title"] = nil
        if case let .array(slots)? = properties["slots"] {
            properties["slots"] = .array(slots.map { slot in
                guard case var .object(object) = slot else { return slot }
                if case let .array(sets)? = object["sets"] {
                    object["sets"] = .array(sets.map { set in
                        guard case var .object(planned) = set else { return set }
                        if planned["role"] == nil { planned["role"] = .string("working") }
                        if planned["structure"] == nil { planned["structure"] = .string("straight") }
                        return .object(planned)
                    })
                }
                return .object(object)
            })
        }
        return .object(properties)
    }

    /// Every path at which two JSON values differ: `slots[3].sets[0].repsMax`.
    static func differences(_ a: WorkoutsCore.JSONValue?, _ b: WorkoutsCore.JSONValue?, at path: String = "") -> [String] {
        switch (a, b) {
        case let (.object(x)?, .object(y)?):
            let keys = Set(x.map(\.key)).union(y.map(\.key)).sorted()
            return keys.flatMap { differences(x[$0], y[$0], at: path.isEmpty ? $0 : "\(path).\($0)") }
        case let (.array(x)?, .array(y)?) where x.count == y.count:
            return zip(x, y).enumerated().flatMap { index, pair in differences(pair.0, pair.1, at: "\(path)[\(index)]") }
        default:
            return a == b ? [] : [path]
        }
    }

    /// Reads a server answer as the app does.
    func read(_ json: WorkoutsCore.JSONValue) throws -> (FlatRecord, Routine) {
        let record = try WorkoutsAPI.record(json)
        return (record, try codec.decodeRoutine(record))
    }

    /// Applies an edit and answers the paths that changed against the
    /// server's properties, and the PATCH's keys.
    func edited(_ json: WorkoutsCore.JSONValue, _ edit: (inout RoutineEditor) throws -> Void) throws -> (paths: [String], patch: Set<String>, after: WorkoutsCore.JSONValue) {
        let (record, routine) = try read(json)
        var editor = RoutineEditor(routine)
        try edit(&editor)
        let after = Self.normalized(.object(codec.encode(editor.routine).properties))
        let before = Self.normalized(.object(record.properties))
        return (Self.differences(before, after), Set(editor.patch(codec: codec).map(\.key)), after)
    }

    @Test("every routine reads and encodes back unchanged, unknown fields at every level included")
    func roundTrip() throws {
        for (name, json) in try Self.serverRecords() {
            let paths = try edited(Self.withUnknownFields(json)) { _ in }.paths
            #expect(paths.isEmpty, "\(name): \(paths)")
        }
    }

    @Test("a rename changes the name and nothing else")
    func rename() throws {
        for (name, json) in try Self.serverRecords() {
            let result = try edited(Self.withUnknownFields(json)) { $0.rename("Renamed on the phone") }
            #expect(result.paths == ["name"], "\(name): \(result.paths)")
            #expect(result.patch == ["name"], "\(name)")
        }
    }

    @Test("one set's rep range changes that field alone; the PATCH carries the slots whole")
    func repRange() throws {
        for (name, json) in try Self.serverRecords() {
            let (_, routine) = try read(json)
            let slot = try #require(routine.slots.first { $0.sets.contains { $0.repsMax != nil } }, "\(name)")
            let index = try #require(routine.slots.firstIndex { $0.key == slot.key })
            let set = try #require(slot.sets.firstIndex { $0.repsMax != nil })
            let result = try edited(Self.withUnknownFields(json)) { editor in
                editor.updateSet(slot.key, slot.sets[set].key) { $0.repsMax = ($0.repsMax ?? 0) + 3 }
            }
            #expect(result.paths == ["slots[\(index)].sets[\(set)].repsMax"], "\(name): \(result.paths)")
            #expect(result.patch.isSuperset(of: ["slots"]) && result.patch.isSubset(of: ["slots", "groups"]))
        }
    }

    @Test("members reordered in a group: the same slots, unchanged, in the new order")
    func reorderInGroup() throws {
        for (name, json) in try Self.serverRecords() {
            let (record, routine) = try read(Self.withUnknownFields(json))
            guard let group = routine.groups.first?.key else { continue }
            var editor = RoutineEditor(routine)
            let before = editor.members(of: group)
            editor.moveMembers(of: group, from: [0], to: before.count)
            #expect(editor.members(of: group) == Array(before.dropFirst()) + [before[0]], "\(name)")
            guard case let .array(was)? = Self.normalized(.object(record.properties)).objectValue?["slots"],
                  case let .array(now)? = Self.normalized(.object(codec.encode(editor.routine).properties)).objectValue?["slots"]
            else { Issue.record("no slots"); continue }
            #expect(Set(was) == Set(now), "\(name): a slot changed while moving")
            #expect(Set(editor.patch(codec: codec).map(\.key)) == ["slots", "groups"])
        }
    }

    @Test("an exercise moved out of a group and into another keeps every field it had")
    func acrossGroups() throws {
        let (_, routine) = try read(Self.withUnknownFields(try #require(Self.serverRecords().first).json))
        var editor = RoutineEditor(routine)
        let moving = try #require(editor.members(of: "a").last)
        let before = try #require(editor.slot(moving))
        editor.leaveGroup(moving)
        let index = try #require(editor.blocks.firstIndex { $0.slots == [moving] })
        editor.groupWithNext(index)
        let after = try #require(editor.slot(moving))
        #expect(after.group == "b")
        var expected = before
        expected.group = "b"
        #expect(after == expected)
        #expect(after.extra["coachCue"] == .string("cue"))
        #expect(editor.group("a")?.groupKind == .superset)
        #expect(editor.group("b")?.groupKind == .giantset)
        #expect(editor.group("b")?.extra["futureGroupField"] == .number(3))
    }

    // MARK: - The outbox

    func editEntry(_ patch: JSONObject = ["name": .string("Renamed")], base: Int = 4) -> (RoutineID, JSONObject, Int) {
        ("day-1", patch, base)
    }

    static let path = "/api/v1/substrate.reamde.dev/training/routine/day-1"

    @Test("an edit is a PATCH of what changed under the version the owner edited")
    func guardedPatch() async throws {
        let server = ScriptedServer()
        server.answer("PATCH", Self.path, 200, record(5))
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.edit(routine: "day-1", patch: ["name": .string("Renamed"), "folder": .null], baseVersion: 4, codec: codec)
        #expect(await outbox.flush(api(server)) == .sent)
        let sent = try #require(server.sent.first)
        guard case let .object(body)? = sent.body, case let .object(properties)? = body["properties"] else { Issue.record("no body"); return }
        #expect(body["ifVersion"] == .number(4))
        #expect(properties["name"] == .string("Renamed") && properties["folder"] == .null)
        #expect(await outbox.version(of: codec.kinds.routine, "day-1") == 5)
    }

    @Test("a conflict is parked for the owner: no re-read, nothing sent over the other writer's change")
    func conflict() async throws {
        let server = ScriptedServer()
        server.answer("PATCH", Self.path, 409, #"{"error":{"code":"conflict","message":"version conflict: ifVersion 4, stored 6"}}"#)
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.edit(routine: "day-1", patch: ["name": .string("Renamed")], baseVersion: 4, codec: codec)
        let state = await outbox.flush(api(server))
        guard case .refused = state else { Issue.record("not refused: \(state)"); return }
        #expect(server.methodsAndPaths == ["PATCH \(Self.path)"], "one try, no read, no second write")
        let parked = try #require(await outbox.parked.first)
        #expect(parked.conflict)
        // A second flush leaves it parked, unsent.
        _ = await outbox.flush(api(server))
        #expect(server.sent.count == 1)

        // Keep mine: sent again without a version.
        let again = ScriptedServer()
        again.answer("PATCH", Self.path, 200, record(7))
        try await outbox.resolve(parked.id, keepMine: true)
        #expect(await outbox.flush(api(again)) == .sent)
        guard case let .object(body)? = again.sent.first?.body else { Issue.record("no body"); return }
        #expect(body["ifVersion"] == nil)
        #expect(await outbox.entries.isEmpty)
    }

    @Test("use theirs drops the edit")
    func useTheirs() async throws {
        let server = ScriptedServer()
        server.answer("PATCH", Self.path, 409, #"{"error":{"code":"conflict","message":"version conflict"}}"#)
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.edit(routine: "day-1", patch: ["name": .string("Renamed")], baseVersion: 4, codec: codec)
        _ = await outbox.flush(api(server))
        let parked = try #require(await outbox.parked.first)
        try await outbox.resolve(parked.id, keepMine: false)
        #expect(await outbox.entries.isEmpty)
    }

    @Test("offline, edits wait on disk and go as one PATCH under the first one's version")
    func offline() async throws {
        let directory = temporaryDirectory()
        let offline = ScriptedServer()
        offline.on { _ in (-1, "") }
        var outbox = Outbox(directory: directory, writingOptions: .atomic)
        try await outbox.edit(routine: "day-1", patch: ["name": .string("First"), "notes": .string("a")], baseVersion: 4, codec: codec)
        guard case .offline = await outbox.flush(api(offline)) else { Issue.record("not offline"); return }
        try await outbox.edit(routine: "day-1", patch: ["name": .string("Second"), "folder": .null], baseVersion: 4, codec: codec)
        #expect(await outbox.entries.count == 1)

        // The app is killed and launched again: the queue is on disk.
        outbox = Outbox(directory: directory, writingOptions: .atomic)
        let waiting = await outbox.waitingRoutines(codec: codec)
        #expect(waiting["day-1"]?.properties["name"] == .string("Second"))

        let server = ScriptedServer()
        server.answer("PATCH", Self.path, 200, record(5))
        #expect(await outbox.flush(api(server)) == .sent)
        #expect(server.sent.count == 1)
        guard case let .object(body)? = server.sent.first?.body, case let .object(properties)? = body["properties"] else { Issue.record("no body"); return }
        #expect(body["ifVersion"] == .number(4))
        #expect(properties["name"] == .string("Second") && properties["notes"] == .string("a") && properties["folder"] == .null)
    }

    @Test("an edit after the app's own write lands asserts that write's version, not the stale cache's")
    func afterOwnWrite() async throws {
        let server = ScriptedServer()
        server.on { sent in
            guard sent.method == "PATCH", case let .object(body)? = sent.body else { return nil }
            return body["ifVersion"] == .number(4) ? (200, record(5)) : body["ifVersion"] == .number(5) ? (200, record(6)) : (409, "{}")
        }
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.edit(routine: "day-1", patch: ["name": .string("One")], baseVersion: 4, codec: codec)
        #expect(await outbox.flush(api(server)) == .sent)
        // The cache still says version 4.
        try await outbox.edit(routine: "day-1", patch: ["name": .string("Two")], baseVersion: 4, codec: codec)
        #expect(await outbox.flush(api(server)) == .sent)
        #expect(await outbox.version(of: codec.kinds.routine, "day-1") == 6)
    }

    @Test("after Keep My Edit, a later edit made on the old version still meets a conflict")
    func keepMineThenEdit() async throws {
        let server = ScriptedServer()
        // The coach wrote version 5; the owner's edit was on 4.
        server.on { sent in
            guard sent.method == "PATCH", case let .object(body)? = sent.body else { return nil }
            switch body["ifVersion"] {
            case nil: return (200, record(6))
            case .number(6)?: return (200, record(7))
            default: return (409, #"{"error":{"code":"conflict","message":"version conflict"}}"#)
            }
        }
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.edit(routine: "day-1", patch: ["slots": .array([])], baseVersion: 4, codec: codec)
        _ = await outbox.flush(api(server))
        let parked = try #require(await outbox.parked.first)
        try await outbox.resolve(parked.id, keepMine: true)
        // Made offline before Keep My Edit went: never merged into the
        // unguarded write.
        try await outbox.edit(routine: "day-1", patch: ["notes": .string("mine")], baseVersion: 4, codec: codec)
        #expect(await outbox.entries.count == 2)
        _ = await outbox.flush(api(server))
        let second = try #require(await outbox.parked.first)
        #expect(second.conflict, "the second edit was on version 4, and only the app's own guarded writes move it")
        #expect(second.properties?["notes"] == .string("mine"))
        guard case let .object(last)? = server.sent.last?.body else { Issue.record("no body"); return }
        #expect(last["ifVersion"] == .number(4))
    }

    @Test("an edit never joins a parked one")
    func notIntoParked() async throws {
        let server = ScriptedServer()
        server.answer("PATCH", Self.path, 409, #"{"error":{"code":"conflict","message":"version conflict"}}"#)
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.edit(routine: "day-1", patch: ["name": .string("One")], baseVersion: 4, codec: codec)
        _ = await outbox.flush(api(server))
        try await outbox.edit(routine: "day-1", patch: ["name": .string("Two")], baseVersion: 4, codec: codec)
        let entries = await outbox.entries
        #expect(entries.count == 2)
        #expect(entries.first?.properties?["name"] == .string("One"))
        // During the conflict, the catalogue shows the server's routine.
        #expect(await outbox.waitingRoutines(codec: codec)["day-1"]?.properties["name"] == .string("Two"))
        try await outbox.resolve(try #require(entries.first?.id), keepMine: false)
        #expect(await outbox.entries.count == 1)
    }

    @Test("a duplicate is created only where nothing is: ifVersion 0, and a refusal is parked, not merged")
    func createOnly() async throws {
        let server = ScriptedServer()
        server.answer("PUT", "/api/v1/substrate.reamde.dev/training/routine/day-1-copy", 409, #"{"error":{"code":"conflict","message":"version conflict"}}"#)
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        let copy = RoutineEditor.duplicate(RoutineEditorFixture.routine, taken: ["day-1"])
        try await outbox.enqueue([OutboxPlan.routineCreated(copy.routine, codec: codec)])
        guard case .refused = await outbox.flush(api(server)) else { Issue.record("not refused"); return }
        guard case let .object(body)? = server.sent.first?.body else { Issue.record("no body"); return }
        #expect(body["ifVersion"] == .number(0))
        let parked = try #require(await outbox.parked.first)
        #expect(!parked.conflict)

        // Created: the next edit of it asserts the version the create made.
        let fresh = ScriptedServer()
        fresh.on { sent in
            guard case let .object(body)? = sent.body else { return nil }
            if sent.method == "PUT" { return (200, record(1)) }
            return body["ifVersion"] == .number(1) ? (200, record(2)) : (409, "{}")
        }
        let other = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        try await other.enqueue([OutboxPlan.routineCreated(copy.routine, codec: codec)])
        #expect(await other.flush(api(fresh)) == .sent)
        try await other.edit(routine: copy.routine.id, patch: ["name": .string("Mine")], baseVersion: nil, codec: codec)
        #expect(await other.flush(api(fresh)) == .sent)
    }

    @Test("a duplicate is a PUT of the whole routine; an edit before it is sent goes into its body")
    func duplicate() async throws {
        let (_, routine) = try read(try #require(Self.serverRecords().first).json)
        let copy = RoutineEditor.duplicate(routine, taken: [routine.id])
        let outbox = Outbox(directory: temporaryDirectory(), writingOptions: .atomic)
        try await outbox.enqueue([OutboxPlan.routineCreated(copy.routine, codec: codec)])
        try await outbox.edit(routine: copy.routine.id, patch: ["name": .string("Mine"), "notes": .null], baseVersion: nil, codec: codec)
        let entries = await outbox.entries
        #expect(entries.count == 1)
        #expect(entries.first?.method == .put)
        #expect(entries.first?.properties?["name"] == .string("Mine"))
        #expect(entries.first?.properties?["notes"] == nil)
        #expect(entries.first?.properties?["slots"] == codec.encode(routine).properties["slots"])
    }

    @Test("a catalogue read lays waiting edits and creates over the server's routines")
    func overlay() throws {
        let (record, _) = try read(try #require(Self.serverRecords().first).json)
        let merged = RemoteCatalog.overlay([record], waiting: [
            record.id: (creates: false, properties: ["name": .string("Edited"), "folder": .null]),
            "new-one": (creates: true, properties: ["name": .string("New"), "slots": .array([])]),
            "gone": (creates: false, properties: ["name": .string("Nothing to edit")]),
        ], kind: codec.kinds.routine)
        #expect(merged.count == 2)
        #expect(merged[0].properties["name"] == .string("Edited"))
        #expect(merged[0].properties["folder"] == nil)
        #expect(merged[0].version == record.version, "the cache keeps the version the edit asserts")
        #expect(merged[1].id == "new-one")
    }
}

enum RoutineEditorFixture {
    static let routine = Routine(id: "day-1", version: 4, name: "Day 1",
                                 slots: [RoutineSlot(key: "a", exercise: "a", sets: [PlannedSet(key: "s1", repsMin: 8, repsMax: 12)])])
}
