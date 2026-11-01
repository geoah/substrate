import Foundation

/// Why a flat record could not become a model value. A source that meets one
/// skips the record and lists it (`UnreadableRecord`); it never crashes.
public enum CodecError: Error, Hashable, Sendable, CustomStringConvertible {
    /// The record is of another kind than the one asked for.
    case wrongKind(expected: String, found: String, id: String)
    /// A required property is missing, or holds a value this build does not
    /// know, such as a new `measurement` (§3.4 rule 7).
    case unsupported(kind: String, id: String, property: String, value: String?)
    /// A property holds the wrong JSON type or a malformed spelling: a decimal
    /// as a JSON number, a duration that is not ISO 8601, a reference to
    /// another kind.
    case invalid(kind: String, id: String, property: String, value: String)

    public var description: String {
        switch self {
        case let .wrongKind(expected, found, id):
            "\(found)/\(id) is not a \(expected)"
        case let .unsupported(kind, id, property, value?):
            "\(kind)/\(id): \(property) \(value) is not supported"
        case let .unsupported(kind, id, property, nil):
            "\(kind)/\(id): \(property) is missing"
        case let .invalid(kind, id, property, value):
            "\(kind)/\(id): \(property) \(value) is not valid"
        }
    }
}

/// A record a source skipped, for Settings' "Records that could not be read".
public struct UnreadableRecord: Codable, Hashable, Sendable {
    /// Where it came from: a bundled file, `local-exercises.json`, a history file.
    public var source: String
    public var kind: String?
    public var id: String?
    public var reason: String

    public init(source: String, kind: String? = nil, id: String? = nil, reason: String) {
        self.source = source
        self.kind = kind
        self.id = id
        self.reason = reason
    }
}
