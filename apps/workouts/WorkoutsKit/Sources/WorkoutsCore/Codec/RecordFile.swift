import Foundation
import SubstrateCore

/// A file of flat records: the generated bundled, fixture and preview files
/// (`{"source", "records"}`) and `local-exercises.json` (`{"format", "records"}`).
public struct RecordFile: Codable, Hashable, Sendable {
    /// The repository-relative YAML a generated file was made from.
    public var source: String?
    public var format: Int?
    public var records: [FlatRecord]

    public init(source: String? = nil, format: Int? = nil, records: [FlatRecord]) {
        self.source = source
        self.format = format
        self.records = records
    }

    /// The file as JSON, records in order and each record's properties in
    /// declaration order.
    public var json: JSONValue {
        var object = JSONObject()
        if let source { object["source"] = .string(source) }
        if let format { object["format"] = .number(Decimal(format)) }
        object["records"] = .array(records.map(\.json))
        return .object(object)
    }
}
