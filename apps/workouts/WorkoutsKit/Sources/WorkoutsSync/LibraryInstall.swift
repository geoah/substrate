import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import WorkoutsCore
import SubstrateCore

/// The exercise library seeded into one server and repository: its
/// movements and their exercises, written into the training package as
/// ordinary records the coach and the console read (decision L1).
///
/// THE SERVER IS THE MARKER. The repository's `substrate.reamde.dev/training/library/exercises`
/// record holds the library version it has whole, written only after a run
/// that left nothing behind; a run names the version it writes in
/// `seeding` before its first write. A run writes the records that first appeared
/// after that version (each record's library code says when), so a record
/// the owner deleted never comes back and a run cut short is finished, on
/// any device. A repository seeded before the marker existed reads its
/// version from the library's movements, which only a seeding writes.
///
/// A RECORD THE REPOSITORY HOLDS is the owner's. It is enriched once, and
/// only when it is plainly the library's exercise (the same name or alias,
/// or the same Hevy code): it gains the movement, the muscles and the codes
/// it lacks, never a field that changes how its sets count or run (load
/// basis, implements, laterality, measurement, rest), and once it carries
/// the library's code it is never filled again, so a value the owner clears
/// stays cleared.
public final class LibraryInstall: @unchecked Sendable {
    struct File: Codable {
        var format = 2
        var version: Int
    }

    /// What a run did.
    public struct Outcome: Sendable, Equatable {
        public var written = 0
        public var filled = 0
        /// Records left for the next run: a create or a fill that met a
        /// record written meanwhile.
        public var deferred = 0
        public var current: Bool { written == 0 && filled == 0 && deferred == 0 }
    }

    public static let fileName = "library.json"
    /// The marker's id in `substrate.reamde.dev/training/library`.
    public static let markerID = "exercises"
    /// The fields a held record may gain: what it is and what it works,
    /// nothing that changes how its sets are counted or run.
    static let fillable: Set<String> = ["movement", "muscles", "codes", "aliases", "force", "mechanic", "primaryMuscles",
                                        "secondaryMuscles"]

    public let url: URL
    private let writingOptions: Data.WritingOptions
    // Guards `file`; `@unchecked Sendable` rests on it.
    private let lock = NSLock()
    private var file: File?

    public init(directory: URL, writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting) {
        url = directory.appendingPathComponent(Self.fileName)
        self.writingOptions = writingOptions
        guard let data = try? Data(contentsOf: url) else { return }
        file = try? JSONDecoder().decode(File.self, from: data)
    }

    /// The library version this device saw the repository hold whole, a
    /// cache of the marker that saves the reads.
    /// A file from a build before the marker (format 1) says nothing the
    /// server can vouch for, and is read again from the marker.
    public var installedVersion: Int? { lock.withLock { file.flatMap { $0.format >= 2 ? $0.version : nil } } }

    /// Seeds `library` where the repository is behind it. A refusal stops the
    /// run with the server's words; the next sync starts again from the
    /// marker.
    @discardableResult
    public func ensure(_ api: WorkoutsAPI, library: ExerciseLibrary) async throws -> Outcome {
        if installedVersion ?? 0 >= library.version { return Outcome() }
        let kinds = RecordCodec().kinds
        let marker = try await api.get(kinds.library, Self.markerID)
        let listed = try await api.list([kinds.movement, kinds.exercise])
        let held = Dictionary(listed.map { ("\($0.kind)/\($0.id)", $0) }, uniquingKeysWith: { first, _ in first })
        // No marker at all: a repository an earlier build seeded reads its
        // version from the library's movements, which only a seeding writes.
        // A marker without a version is a first run cut short.
        let from: Int = if let marker {
            if case let .number(version)? = marker.properties["version"] { version.int ?? 0 } else { 0 }
        } else {
            listed.filter { $0.kind == kinds.movement }.compactMap { ExerciseLibrary.since($0.properties) }.max() ?? 0
        }
        guard from < library.version else {
            try record(library.version)
            return Outcome()
        }
        try await api.write("PUT", kinds.library, Self.markerID,
                            properties: ["name": .string("Exercise library"), "seeding": .number(Decimal(library.version))])

        var outcome = Outcome()
        for record in library.records {
            let key = "\(record.kind)/\(record.id)"
            if let existing = held[key] {
                let patch = Self.fill(existing.properties, from: record.properties)
                guard !patch.isEmpty else { continue }
                do {
                    try await api.write("PATCH", record.kind, record.id, properties: patch, ifVersion: existing.version)
                    outcome.filled += 1
                } catch let error as ServerError where error.status == 409 {
                    outcome.deferred += 1
                }
            } else if (ExerciseLibrary.since(record.properties) ?? library.version) > from {
                do {
                    // A create: one written meanwhile is the owner's, never merged into.
                    try await api.write("PUT", record.kind, record.id, properties: record.properties, ifVersion: 0)
                    outcome.written += 1
                } catch let error as ServerError where error.status == 409 {
                    outcome.deferred += 1
                }
            }
        }
        guard outcome.deferred == 0 else { return outcome }
        try await api.write("PUT", kinds.library, Self.markerID,
                            properties: ["version": .number(Decimal(library.version)), "seeding": .null])
        try record(library.version)
        return outcome
    }

    /// The fields a held record lacks from `fillable`, and the library's codes
    /// it lacks, when the record is plainly the library's own (its name or an
    /// alias, or a shared Hevy code) and has not been enriched before. Nothing
    /// it holds changes.
    static func fill(_ held: JSONObject, from library: JSONObject) -> JSONObject {
        guard ExerciseLibrary.since(held) == nil, sameThing(held, library) else { return JSONObject() }
        var patch = JSONObject()
        for (key, value) in library where key != "codes" && fillable.contains(key) && held[key] == nil {
            patch[key] = value
        }
        guard case let .array(theirs)? = library["codes"] else { return patch }
        var codes: [JSONValue] = []
        if case let .array(mine)? = held["codes"] { codes = mine }
        let present = Set(codes.compactMap(identity))
        let missing = theirs.filter { identity($0).map { !present.contains($0) } ?? false }
        if !missing.isEmpty { patch["codes"] = .array(codes + missing) }
        return patch
    }

    /// The same exercise or movement: one's name is the other's name or an
    /// alias, case and spacing aside, or they share a Hevy code.
    static func sameThing(_ held: JSONObject, _ library: JSONObject) -> Bool {
        func names(_ object: JSONObject) -> Set<String> {
            var out = Set<String>()
            if case let .string(name)? = object["name"] { out.insert(normalized(name)) }
            if case let .array(aliases)? = object["aliases"] {
                for case let .string(alias) in aliases { out.insert(normalized(alias)) }
            }
            return out
        }
        func hevy(_ object: JSONObject) -> Set<String> {
            guard case let .array(codes)? = object["codes"] else { return [] }
            return Set(codes.compactMap { value -> String? in
                guard case let .object(coding) = value, coding["system"] == .string("hevy"), case let .string(code)? = coding["code"]
                else { return nil }
                return code
            })
        }
        guard case let .string(name)? = held["name"] else { return false }
        return names(library).contains(normalized(name)) || !hevy(held).isDisjoint(with: hevy(library))
    }

    private static func normalized(_ text: String) -> String {
        text.folding(options: [.caseInsensitive, .diacriticInsensitive], locale: Locale(identifier: "en_US_POSIX"))
            .split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }

    private static func identity(_ value: JSONValue) -> String? {
        guard case let .object(coding) = value, case let .string(system)? = coding["system"], case let .string(code)? = coding["code"]
        else { return nil }
        return "\(system)\u{1F}\(code)"
    }

    private func record(_ version: Int) throws {
        try lock.withLock {
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.withoutEscapingSlashes, .sortedKeys]
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try encoder.encode(File(version: version)).write(to: url, options: writingOptions)
            file = File(version: version)
        }
    }
}
