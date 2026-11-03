import SubstrateCore

/// A record an import writes as it is, by kind and id: a body measurement.
public struct RecordWrite: Codable, Hashable, Sendable {
    public var kind: String
    public var id: String
    public var properties: JSONObject

    public init(kind: String, id: String, properties: JSONObject) {
        self.kind = kind
        self.id = id
        self.properties = properties
    }

    public init(_ record: FlatRecord) {
        self.init(kind: record.kind, id: record.id, properties: record.properties)
    }
}
