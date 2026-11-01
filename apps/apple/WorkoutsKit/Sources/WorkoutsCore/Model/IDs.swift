import Foundation

/// A record or engine id: a string with a type, so a set id is never passed
/// where a session id belongs. Encodes as the bare string.
public protocol StringID: RawRepresentable, Codable, Hashable, Comparable, Sendable,
    CustomStringConvertible, ExpressibleByStringLiteral where RawValue == String
{
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

/// An exercise's id, a slug the writer chooses: `chest-press-machine`.
public struct ExerciseID: StringID {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

/// A routine's id, a slug the writer chooses: `fully-body-collection-1`.
public struct RoutineID: StringID {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

/// A setup's id, a slug the writer chooses: `hotel-gym-chest-press`.
public struct SetupID: StringID {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

/// A session's id: a lowercase UUIDv7 of its start, or an importer's key.
public struct SessionID: StringID {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

/// A set record's id: a lowercase UUIDv7 of its end, or an importer's key.
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
    /// The id of an exercise added on the phone (§7.6): the name lowercased,
    /// diacritics folded, every run of other characters one `-`, trimmed, and
    /// `-2`, `-3` and so on when the slug is taken. Only ASCII letters and
    /// digits survive, so the id is a safe path segment everywhere.
    static func slug(for name: String, taken: Set<ExerciseID> = []) -> ExerciseID {
        let folded = name.folding(options: [.diacriticInsensitive, .caseInsensitive], locale: Locale(identifier: "en_US_POSIX"))
        var slug = ""
        var pendingDash = false
        for scalar in folded.unicodeScalars {
            if scalar.isASCII, CharacterSet.alphanumerics.contains(scalar) {
                if pendingDash, !slug.isEmpty { slug.append("-") }
                pendingDash = false
                slug.unicodeScalars.append(scalar)
            } else {
                pendingDash = true
            }
        }
        let base = slug.isEmpty ? "exercise" : slug
        var candidate = ExerciseID(base)
        var suffix = 2
        while taken.contains(candidate) {
            candidate = ExerciseID("\(base)-\(suffix)")
            suffix += 1
        }
        return candidate
    }
}
