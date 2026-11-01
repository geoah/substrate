import Foundation
import SubstrateKit
import WorkoutsCore

/// The requests the app makes, over SubstrateKit's client, with WorkoutsCore's
/// JSON on both sides: a decimal keeps its digits and an object its key
/// order, which SubstrateKit's own value (a `Double` per number) would not.
/// Every record crosses `Rehome` here and nowhere else.
public struct WorkoutsAPI: Sendable {
    public var client: SubstrateClient
    public var rehome: Rehome

    public init(client: SubstrateClient, authority: String) {
        self.client = client
        rehome = Rehome(authority: authority)
    }

    /// A record path under `/api/v1/`, the kind as the repository spells it.
    func path(_ kind: String, _ id: String) -> String { SubstrateClient.recordPath(rehome.remoteKind(kind), id) }

    // MARK: Records

    /// One live record, in the phone's spelling, or nil when there is none.
    public func get(_ kind: String, _ id: String) async throws -> FlatRecord? {
        let response = try await client.send("GET", path(kind, id), accept: [404])
        if response.status == 404 { return nil }
        let value = try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: response.data)
        if case let .object(object) = value, let deleted = object["deletedAt"], deleted != .null { return nil }
        return try Self.record(value, rehome: rehome)
    }

    /// Every live record of the kinds, in the phone's spelling, by list pages.
    public func list(_ kinds: [String], properties: [String: SubstrateKit.JSONValue] = [:], limit: Int = 5000) async throws -> [FlatRecord] {
        var query = ListQuery(kinds: kinds.map(rehome.remoteKind), properties: properties, first: 500)
        var out: [FlatRecord] = []
        while true {
            let response = try await client.send("GET", "api/v1/records", query: query.queryItems)
            let page = try JSONDecoder().decode(Page.self, from: response.data)
            out += try page.records.map { try Self.record($0, rehome: rehome) }
            guard let cursor = page.cursor, !cursor.isEmpty, out.count < limit else { return out }
            query.after = cursor
        }
    }

    /// A write: a PUT merges, a PATCH merges and clears what it sets to
    /// null. Answers the record's new version.
    @discardableResult
    public func write(_ method: String, _ kind: String, _ id: String, properties: JSONObject, ifVersion: Int? = nil) async throws -> Int {
        var body = JSONObject()
        body["properties"] = rehome.outbound(.object(properties))
        if let ifVersion { body["ifVersion"] = .number(Decimal(ifVersion)) }
        let response = try await client.send(method, path(kind, id), body: try Self.body(.object(body)))
        return try Self.version(of: response.data)
    }

    /// Deletes a record, purged at once so a replayed create starts afresh.
    public func purge(_ kind: String, _ id: String, ifVersion: Int?) async throws {
        var query = [("purge", "true")]
        if let ifVersion { query.append(("ifVersion", String(ifVersion))) }
        try await client.send("DELETE", path(kind, id), query: query, accept: [404])
    }

    // MARK: The package

    /// The repository's row of a package, as `properties`, or nil when it
    /// holds none (or the token may not see it).
    public func packageRow(_ id: String) async throws -> JSONObject? {
        let response = try await client.send("GET", SubstrateClient.recordPath("substrate.reamde.dev/core/package", id), accept: [404])
        if response.status == 404 { return nil }
        guard case let .object(object) = try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: response.data),
              case let .object(properties)? = object["properties"]
        else { return nil }
        if let deleted = object["deletedAt"], deleted != .null { return nil }
        return properties
    }

    /// Applies declarations, already in the repository's spelling.
    public func applyVocabulary(_ documents: [WorkoutsCore.JSONValue]) async throws {
        let body: WorkoutsCore.JSONValue = .object(["documents": .array(documents)])
        try await client.send("POST", "api/v1/vocabulary/apply", body: try Self.body(body), timeout: SubstrateClient.Timeout.serverWork,
                              retries: 0)
    }

    // MARK: Wire

    struct Page: Decodable {
        var records: [WorkoutsCore.JSONValue]
        var cursor: String?

        enum CodingKeys: String, CodingKey { case records, cursor }

        init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            records = try c.decodeIfPresent([WorkoutsCore.JSONValue].self, forKey: .records) ?? []
            cursor = try? c.decodeIfPresent(String.self, forKey: .cursor)
        }
    }

    /// A server record as the codec reads it: `kind`, `id`, `properties`
    /// and `version`, every other field (stamps, labels, property metadata)
    /// left behind, in the phone's spelling.
    static func record(_ value: WorkoutsCore.JSONValue, rehome: Rehome) throws -> FlatRecord {
        guard case let .object(object) = value else { throw CodecError.invalid(kind: "?", id: "?", property: "record", value: "not an object") }
        var flat = JSONObject()
        for key in ["kind", "id", "properties", "version"] {
            if let item = object[key] { flat[key] = item }
        }
        if flat["properties"] == nil { flat["properties"] = .object(JSONObject()) }
        return rehome.inbound(try FlatRecord(json: .object(flat)))
    }

    static func version(of data: Data) throws -> Int {
        guard case let .object(object) = try JSONDecoder().decode(WorkoutsCore.JSONValue.self, from: data),
              case let .number(number)? = object["version"]
        else { return 0 }
        return NSDecimalNumber(decimal: number).intValue
    }

    static func body(_ value: WorkoutsCore.JSONValue) throws -> RequestBody {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes]
        return .json(try encoder.encode(value))
    }
}
