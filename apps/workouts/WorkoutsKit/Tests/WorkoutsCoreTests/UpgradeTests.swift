import Foundation
import Testing
@testable import WorkoutsCore
import SubstrateCore

/// The files the last release left on the phone, every training kind
/// spelled `~/training/…` (decision T3): each store reads them once in the
/// published spelling, writes them back at its moved format, and a second
/// load finds nothing to respell. `Fixtures/legacy/history-session.json`
/// is a history file as that build wrote it at Finish, with the recorded
/// session and its 20 sets.
@Suite("Upgrading from the ~/training spelling")
struct UpgradeTests {
    let codec = RecordCodec()
    let legacyPrefix = "~/training/"

    /// Slashes unescaped, so a kind or a path reads as it is spelled.
    static var plain: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes]
        return encoder
    }

    static func legacyHistory() throws -> Data {
        let url = try #require(Bundle.module.url(forResource: "history-session", withExtension: "json", subdirectory: "Fixtures/legacy"))
        return try Data(contentsOf: url)
    }

    func directory() -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("workouts-upgrade-\(UUID().uuidString)", isDirectory: true)
        try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    /// The text of `records` with the published package respelled as the
    /// old build spelled it, so a file reads as that build wrote it.
    func legacy(_ records: [FlatRecord]) throws -> [FlatRecord] {
        let text = String(decoding: try Self.plain.encode(records), as: UTF8.self)
            .replacingOccurrences(of: WorkoutsKinds.packageID + "/", with: legacyPrefix)
        return try JSONDecoder().decode([FlatRecord].self, from: Data(text.utf8))
    }

    @Test("the respell moves the package id, a kind and a record path and leaves every other string alone")
    func respell() {
        #expect(LegacyKinds.respell("~/training") == WorkoutsKinds.packageID)
        #expect(LegacyKinds.respell("~/training/set") == codec.kinds.set)
        #expect(LegacyKinds.respell("~/training/exercise/leg-press-machine") == "\(codec.kinds.exercise)/leg-press-machine")
        #expect(LegacyKinds.respell("substrate.reamde.dev/health/workout") == "substrate.reamde.dev/health/workout")
        #expect(LegacyKinds.respell("~/trainingcoach/workoutCoach") == "~/trainingcoach/workoutCoach")
        #expect(LegacyKinds.respell(codec.kinds.session) == codec.kinds.session)
        let value: JSONValue = .object(["exercise": .object(["ref": .string("~/training/exercise/x")]), "notes": .string("squat"),
                                        "refs": .array([.string("~/training/setup/s")])])
        #expect(LegacyKinds.respell(value) == .object(["exercise": .object(["ref": .string("\(codec.kinds.exercise)/x")]), "notes": .string("squat"),
                                                       "refs": .array([.string("\(codec.kinds.setup)/s")])]))
    }

    @Test("a history file from the last release reads in the published spelling, is rewritten as format 3, and a second load changes nothing")
    func history() throws {
        let data = try Self.legacyHistory()
        #expect(String(decoding: data, as: UTF8.self).contains("\"~/training/session\""))
        let store = HistoryStore(directory: directory(), codec: codec, writingOptions: .atomic)
        let id = SessionID("01a16363-cf80-73ed-85fd-704f6ce38d73")
        try FileManager.default.createDirectory(at: store.directory, withIntermediateDirectories: true)
        try data.write(to: store.url(for: id))

        let first = store.loadAll()
        #expect(first.unreadable.isEmpty, "\(first.unreadable)")
        let entry = try #require(first.entries.first)
        #expect(first.entries.count == 1 && entry.id == id)
        #expect(entry.records.sets.count == 20)
        #expect(entry.records.session.routine == RoutineID("day-1-strength-foundation"))
        #expect(entry.local.health.status == .saved)
        // Every kind and reference, as the codec writes it now.
        let encoded = codec.encode(entry.records)
        #expect(encoded.map(\.kind) == [codec.kinds.session] + Array(repeating: codec.kinds.set, count: 20))
        let text = String(decoding: try Self.plain.encode(encoded), as: UTF8.self)
        #expect(!text.contains("~/") && text.contains("\"\(codec.kinds.exercise)/leg-press-machine\""))

        // The file was written back once, at the current format.
        let rewritten = try Data(contentsOf: store.url(for: id))
        let file = try #require(try JSONDecoder().decode(JSONValue.self, from: rewritten).objectValue)
        #expect(file["format"] == .number(Decimal(WorkoutsFormats.historyFormat)) && WorkoutsFormats.historyFormat == 3)
        #expect(!String(decoding: rewritten, as: UTF8.self).contains("~/"))
        #expect(file["local"] != nil)

        let second = store.loadAll()
        #expect(second.entries == first.entries && second.unreadable.isEmpty)
        #expect(try Data(contentsOf: store.url(for: id)) == rewritten)
        #expect(try store.read(id) == entry)
    }

    @Test("a format 1 history file is respelled before its data-model migration, so the migration finds its kinds")
    func historyFormat1() throws {
        let data = try Self.legacyHistory()
        var file = try #require(try JSONDecoder().decode(JSONValue.self, from: data).objectValue)
        file["format"] = .number(1)
        var records = try #require(file["records"]?.arrayValue)
        var session = try #require(records[0].objectValue)
        var properties = try #require(session["properties"]?.objectValue)
        properties["sport"] = nil
        properties["timeZone"] = properties["timezone"]
        properties["timezone"] = nil
        session["properties"] = .object(properties)
        records[0] = .object(session)
        file["records"] = .array(records)
        let store = HistoryStore(directory: directory(), codec: codec, writingOptions: .atomic)
        let id = SessionID("01a16363-cf80-73ed-85fd-704f6ce38d73")
        try FileManager.default.createDirectory(at: store.directory, withIntermediateDirectories: true)
        try JSONValue.object(file).data().write(to: store.url(for: id))
        let load = store.loadAll()
        #expect(load.unreadable.isEmpty, "\(load.unreadable)")
        let entry = try #require(load.entries.first)
        #expect(entry.records.session.sport == .strength)
        #expect(entry.records.sets.count == 20)
    }

    @Test("a local-exercises file from the last release reads in the published spelling and is rewritten at format 2")
    func localExercises() throws {
        let bundled = try BundledCatalog().loadNow()
        let exercise = try #require(bundled.exercises.values.sorted { $0.id.rawValue < $1.id.rawValue }.first)
        let store = LocalExercises(directory: directory(), codec: codec, writingOptions: .atomic)
        let records = try legacy([codec.encode(exercise)])
        #expect(records.first?.kind == legacyPrefix + "exercise")
        try RecordFile(format: 1, records: records).json.data().write(to: store.url)

        let first = try store.load()
        #expect(first.unreadable.isEmpty, "\(first.unreadable)")
        var expected = exercise
        expected.isLocal = true
        #expect(first.exercises == [expected])
        let rewritten = try Data(contentsOf: store.url)
        let file = try JSONDecoder().decode(RecordFile.self, from: rewritten)
        #expect(file.format == WorkoutsFormats.recordFileFormat && file.records == [codec.encode(exercise)])
        #expect(try store.load().exercises == first.exercises)
        #expect(try Data(contentsOf: store.url) == rewritten)
    }

    @Test("a routine draft from the last release reads in the published spelling and is rewritten at format 2")
    func routineDraft() throws {
        let bundled = try BundledCatalog().loadNow()
        let routine = try #require(bundled.routines[SampleRoutine.id])
        var edited = routine
        edited.name = "Renamed"
        let records = try legacy([codec.encode(routine), codec.encode(edited)])
        let store = RoutineDrafts(directory: directory(), codec: codec, writingOptions: .atomic)
        let draft: JSONObject = ["format": .number(1), "isNew": .bool(false), "base": records[0].json, "routine": records[1].json]
        try JSONValue.object(draft).data().write(to: store.url)

        let editor = try #require(try store.load())
        #expect(editor.base == routine && editor.routine == edited && !editor.isNew)
        let rewritten = try Data(contentsOf: store.url)
        let file = try #require(try JSONDecoder().decode(JSONValue.self, from: rewritten).objectValue)
        #expect(file["format"] == .number(Decimal(WorkoutsFormats.draftFormat)))
        #expect(!String(decoding: rewritten, as: UTF8.self).contains("~/"))
        let again = try #require(try store.load())
        #expect(again.base == editor.base && again.routine == editor.routine)
        #expect(try Data(contentsOf: store.url) == rewritten)
    }
}
