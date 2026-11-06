import Foundation
import SubstrateCore

/// The exercise library this build carries (`Resources/Bundled/library.json`,
/// from `apps/workouts/scripts/exercise-library.py`): movements and their
/// exercises, one per equipment version, which the app seeds into the
/// repository (`LibraryInstall`). Each record carries the library's code,
/// `substrate-workouts-library`: its slug (`chest-press-machine`), which
/// builds before the ids were derived wrote it under as its id, and as its
/// `version` the library version the record first appeared in. Its id is
/// derived from the slug (`DerivedMintedID`).
public struct ExerciseLibrary: Sendable {
    public static let system = "substrate-workouts-library"

    public var version: Int
    /// Movements first, then exercises: the order they are written in, since
    /// an exercise names its movement.
    public var records: [FlatRecord]

    public init(version: Int, records: [FlatRecord]) {
        self.version = version
        self.records = records
    }

    /// The library this build carries.
    public static func bundled(_ bundle: Bundle? = nil) throws -> ExerciseLibrary {
        guard let url = (bundle ?? .module).url(forResource: "library", withExtension: "json", subdirectory: "Bundled") else {
            throw CatalogError.missingResource("Bundled/library.json")
        }
        let file = try JSONDecoder().decode(File.self, from: Data(contentsOf: url))
        return ExerciseLibrary(version: file.version, records: file.records)
    }

    private struct File: Decodable {
        var version: Int
        var records: [FlatRecord]
    }

    /// The library version a record first appeared in, from its own code.
    public static func since(_ properties: JSONObject) -> Int? {
        guard case let .array(codes)? = properties["codes"] else { return nil }
        for case let .object(coding) in codes where coding["system"] == .string(system) {
            if case let .string(version)? = coding["version"] { return Int(version) }
        }
        return nil
    }

    /// The code a record names in `system`, from its `codes`.
    public static func code(_ system: String, in properties: JSONObject) -> String? {
        guard case let .array(codes)? = properties["codes"] else { return nil }
        for case let .object(coding) in codes where coding["system"] == .string(system) {
            if case let .string(code)? = coding["code"] { return code }
        }
        return nil
    }

    /// A bundled record's slug: its library code, else its sample code.
    public static func slug(_ properties: JSONObject) -> String? {
        code(system, in: properties) ?? code(SampleRoutine.system, in: properties)
    }

    /// The movements and exercises decoded, as a catalogue holds them.
    public func catalog(codec: RecordCodec = RecordCodec()) throws -> Catalog {
        var catalog = Catalog()
        for record in records {
            switch try codec.decode(record) {
            case let .movement(movement): catalog.movements[movement.id] = movement
            case let .exercise(exercise): catalog.exercises[exercise.id] = exercise
            default: continue
            }
        }
        catalog.resolveMovements()
        return catalog
    }
}

/// The records a repository holds, found for a bundled record (the
/// library's or the sample's) by its id, else by its slug: a held record
/// naming that slug as its library or sample code, or one at the slug as its
/// id, which is where a build before the derived ids wrote it. So a
/// repository seeded under slug ids is not seeded again, the sample's
/// exercise and the library's of one slug stay one record, and an owner's
/// record at a slug is met as it was before.
public struct HeldRecords: Sendable {
    private var byID: [String: FlatRecord] = [:]
    private var bySlug: [String: FlatRecord] = [:]

    public init(_ records: [FlatRecord]) {
        for record in records {
            byID["\(record.kind)/\(record.id)"] = byID["\(record.kind)/\(record.id)"] ?? record
        }
        // A code says what the record is; an id equal to a slug only that
        // an earlier build wrote it there, so a code is found first.
        for record in records {
            for slug in [
                ExerciseLibrary.code(ExerciseLibrary.system, in: record.properties), ExerciseLibrary.code(SampleRoutine.system, in: record.properties),
            ]
            .compactMap(\.self) {
                bySlug["\(record.kind)/\(slug)"] = bySlug["\(record.kind)/\(slug)"] ?? record
            }
        }
        for record in records {
            bySlug["\(record.kind)/\(record.id)"] = bySlug["\(record.kind)/\(record.id)"] ?? record
        }
    }

    /// The held record that is `record`, a bundled one.
    public func matching(_ record: FlatRecord) -> FlatRecord? {
        matching(kind: record.kind, id: record.id, slug: ExerciseLibrary.slug(record.properties))
    }

    public func matching(kind: String, id: String, slug: String?) -> FlatRecord? {
        byID["\(kind)/\(id)"] ?? slug.flatMap { bySlug["\(kind)/\($0)"] }
    }
}
