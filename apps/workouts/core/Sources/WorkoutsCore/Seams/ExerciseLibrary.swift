import Foundation

/// The exercise library this build carries (`Resources/Bundled/library.json`,
/// from `apps/apple/scripts/exercise-library.py`): movements and their
/// exercises, one per equipment version, which the app seeds into the
/// repository (`LibraryInstall`). Each record carries the library's code,
/// `substrate-workouts-library`, whose `version` is the library version the
/// record first appeared in.
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
