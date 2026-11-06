import Foundation

/// A record or engine id: a string with a type, so a set id is never passed
/// where a session id belongs. Encodes as the bare string.
public protocol StringID: RawRepresentable, Codable, Hashable, Comparable, Sendable,
    CustomStringConvertible, ExpressibleByStringLiteral
where RawValue == String {
    init(rawValue: String)
}

public extension StringID {
    init(_ rawValue: String) {
        self.init(rawValue: rawValue)
    }

    init(stringLiteral value: String) {
        self.init(rawValue: value)
    }

    var description: String { rawValue }

    static func < (lhs: Self, rhs: Self) -> Bool { lhs.rawValue < rhs.rawValue }
}

/// An exercise's id: minted (`ExerciseID.minted`), derived from a key
/// (`DerivedMintedID`), or a slug an earlier build or another writer chose
/// (`chest-press-machine`).
public struct ExerciseID: StringID {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

/// A movement's id: the library's, derived from its slug, or the slug an
/// earlier build wrote it under (`chest-press`).
public struct MovementID: StringID {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

/// A routine's id: minted (`RoutineID.minted`), derived from a key, or a
/// slug an earlier build or another writer chose (`full-body-1`).
public struct RoutineID: StringID {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

/// A setup's id, a slug the writer chooses: `hotel-gym-chest-press`.
public struct SetupID: StringID {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

/// A session's id: a lowercase UUIDv7 of its start, or derived from an
/// importer's key.
public struct SessionID: StringID {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

/// A set record's id: a lowercase UUIDv7 of its end, or derived from an
/// importer's key.
public struct SetID: StringID {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

/// An engine-local row id (a UUIDv7), never written to a record.
public struct RowID: StringID {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

/// A journal event's id (a UUIDv7).
public struct EventID: StringID {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

public extension ExerciseID {
    /// A new exercise's id: a UUIDv7 of `date`, the shape a server takes
    /// from an app for a record it creates. A name is never slugged into an
    /// id, which would go stale on a rename.
    static func minted(at date: Date = Date()) -> ExerciseID { ExerciseID(UUIDv7.make(at: date)) }
}

public extension RoutineID {
    /// A new routine's id, minted as `ExerciseID.minted` is.
    static func minted(at date: Date = Date()) -> RoutineID { RoutineID(UUIDv7.make(at: date)) }
}
