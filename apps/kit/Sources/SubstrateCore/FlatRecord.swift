import Foundation

/// A record as substrate's REST API speaks it: `{"kind", "id", "properties",
/// "version"?}`. Other top-level server fields, such as `createdAt`, are
/// ignored on decode and never sent.
public struct FlatRecord: Hashable, Sendable {
    public var kind: String
    public var id: String
    public var properties: JSONObject
    /// The server's record version; only a routine keeps it (`routineVersion`).
    public var version: Int?

    public init(kind: String, id: String, properties: JSONObject, version: Int? = nil) {
        self.kind = kind
        self.id = id
        self.properties = properties
        self.version = version
    }

    /// The record as JSON, keys in the order `kind`, `id`, `properties`, `version`.
    public var json: JSONValue {
        var object: JSONObject = ["kind": .string(kind), "id": .string(id), "properties": .object(properties)]
        if let version { object["version"] = .number(Decimal(version)) }
        return .object(object)
    }

    /// Reads a record from its JSON object.
    public init(json: JSONValue) throws(CodecError) {
        guard case let .object(object) = json,
            case let .string(kind)? = object["kind"],
            case let .string(id)? = object["id"]
        else {
            throw .invalid(kind: "?", id: "?", property: "record", value: "not a flat record")
        }
        guard case let .object(properties)? = object["properties"] else {
            throw .invalid(kind: kind, id: id, property: "properties", value: "not an object")
        }
        var version: Int?
        if let raw = object["version"] {
            guard case let .number(number) = raw, let integer = number.exactInt else {
                throw .invalid(kind: kind, id: id, property: "version", value: "not an integer")
            }
            version = integer
        }
        self.init(kind: kind, id: id, properties: properties, version: version)
    }
}

extension FlatRecord: Codable {
    public init(from decoder: any Decoder) throws {
        try self.init(json: JSONValue(from: decoder))
    }

    public func encode(to encoder: any Encoder) throws {
        try json.encode(to: encoder)
    }
}
