import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import Testing
import WorkoutsCore
@testable import WorkoutsSync
import SubstrateCore

/// The account folder the last release left on the phone, its outbox and
/// catalogue cache spelled `~/training/…` (decision T3): read once in the
/// published spelling, written back at the moved format, and sent to the
/// published paths rather than parked on a 404. `Fixtures/legacy-outbox.json`
/// is an outbox as that build queued a finished session: its create, its 20
/// sets, Finish under `ifVersion`, and the versions and chains it knew.
@Suite("Upgrading an account folder from the ~/training spelling")
struct UpgradeTests {
    let codec = RecordCodec()
    let session = "01a16363-cf80-73ed-85fd-704f6ce38d73"

    func legacyOutbox(in directory: URL) throws -> URL {
        let url = directory.appendingPathComponent(Outbox.fileName)
        try Fixtures.data("legacy-outbox").write(to: url)
        return url
    }

    @Test("the outbox reads every entry, version and chain in the published spelling, rewrites the file once, and a second load changes nothing")
    func outbox() async throws {
        let directory = temporaryDirectory()
        let url = try legacyOutbox(in: directory)
        #expect(String(decoding: try Data(contentsOf: url), as: UTF8.self).contains("\"~/training/set\""))

        let outbox = Outbox(directory: directory, writingOptions: .atomic)
        #expect(await outbox.unreadable == nil)
        let entries = await outbox.entries
        #expect(entries.count == 22)
        #expect(entries.first?.kind == codec.kinds.session && entries.first?.method == .put && entries.first?.recordID == session)
        #expect(entries.last?.kind == codec.kinds.session && entries.last?.method == .patch && entries.last?.guarded == true)
        #expect(entries.dropFirst().dropLast().allSatisfy { $0.kind == codec.kinds.set && $0.session == session })
        let plain = JSONEncoder()
        plain.outputFormatting = [.withoutEscapingSlashes]
        let text = String(decoding: try plain.encode(entries), as: UTF8.self)
        #expect(!text.contains("~/"))
        #expect(text.contains("\"\(codec.kinds.exercise)/leg-press-machine\"") && text.contains("\"\(codec.kinds.session)/\(session)\""))
        #expect(await outbox.version(of: codec.kinds.routine, "day-1-strength-foundation") == 1)
        #expect(await outbox.version(of: codec.kinds.exercise, "leg-press-machine") == 2)
        #expect(await outbox.versions.keys.allSatisfy { $0.hasPrefix(WorkoutsKinds.packageID + "/") })

        // Written back once, at the current format, every key respelled.
        let rewritten = try Data(contentsOf: url)
        let file = try #require(try JSONDecoder().decode(JSONValue.self, from: rewritten).objectValue)
        #expect(file["format"] == .number(Decimal(Outbox.fileFormat)) && Outbox.fileFormat == 2)
        #expect(!String(decoding: rewritten, as: UTF8.self).contains("~/"))
        #expect(file["chains"]?.objectValue?.keys.sorted() == ["\(codec.kinds.routine)/day-1-strength-foundation"])

        let again = Outbox(directory: directory, writingOptions: .atomic)
        #expect(await again.entries == entries)
        #expect(await again.versions == outbox.versions)
        #expect(try Data(contentsOf: url) == rewritten)
        // An edit's chain is kept under the new key, its versions as they were.
        #expect(file["chains"]?.objectValue?["\(codec.kinds.routine)/day-1-strength-foundation"] == .object(["1": .number(2)]))
    }

    @Test("the respelled writes go to the published paths and land, where the old spelling would have parked on a 404")
    func flush() async throws {
        let directory = temporaryDirectory()
        _ = try legacyOutbox(in: directory)
        let server = ScriptedServer()
        server.on { sent in
            guard sent.path.hasPrefix("/api/v1/\(WorkoutsKinds.packageID)/") else { return (404, notFoundBody) }
            if sent.method == "PUT" { return (200, record(1)) }
            guard sent.method == "PATCH", case let .object(body)? = sent.body, body["ifVersion"] == .number(1) else { return nil }
            return (200, record(2))
        }
        let outbox = Outbox(directory: directory, writingOptions: .atomic)
        #expect(await outbox.flush(api(server)) == .sent)
        #expect(await outbox.entries.isEmpty)
        #expect(await outbox.parked.isEmpty)
        let paths = server.methodsAndPaths
        #expect(paths.count == 22)
        #expect(paths.first == "PUT /api/v1/\(codec.kinds.session)/\(session)")
        #expect(paths.last == "PATCH /api/v1/\(codec.kinds.session)/\(session)")
        #expect(paths.allSatisfy { !$0.contains("~") })
        for sent in server.sent where sent.method == "PUT" {
            let text = String(decoding: try JSONEncoder().encode(sent.body), as: UTF8.self)
            #expect(!text.contains("~/"))
        }
    }

    @Test("a catalogue cache from the last release draws in the published spelling and is rewritten at format 2")
    func catalogCache() throws {
        let bundled = try BundledCatalog().loadNow()
        let routine = try #require(bundled.routines[SampleRoutine.id])
        let exercises = bundled.exercises.values.filter { routine.slots.map(\.exercise).contains($0.id) }
        let records = exercises.map(codec.encode) + [codec.encode(routine)]
        let plain = JSONEncoder()
        plain.outputFormatting = [.withoutEscapingSlashes]
        let text = String(decoding: try plain.encode(RecordFile(format: 1, records: records)), as: UTF8.self)
            .replacingOccurrences(of: WorkoutsKinds.packageID + "/", with: "~/training/")
        #expect(text.contains("\"~/training/routine\"") && !text.contains(WorkoutsKinds.packageID))
        let remote = RemoteCatalog(directory: temporaryDirectory(), codec: codec, writingOptions: .atomic)
        try FileManager.default.createDirectory(at: remote.cacheURL.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data(text.utf8).write(to: remote.cacheURL)

        let catalog = try remote.loadNow()
        #expect(catalog.unreadable.isEmpty, "\(catalog.unreadable)")
        #expect(catalog.routines[SampleRoutine.id] == routine)
        #expect(Set(catalog.exercises.keys) == Set(exercises.map(\.id)))
        let rewritten = try Data(contentsOf: remote.cacheURL)
        let file = try JSONDecoder().decode(RecordFile.self, from: rewritten)
        #expect(file.format == WorkoutsFormats.recordFileFormat && Set(file.records) == Set(records))
        #expect(try remote.loadNow().routines == catalog.routines)
        #expect(try Data(contentsOf: remote.cacheURL) == rewritten)
    }
}
