import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateCore
import SubstrateDevice
import WorkoutsCore

/// The requests the app makes, over SubstrateKit's client, with WorkoutsCore's
/// JSON on both sides: a decimal keeps its digits and an object its key
/// order, which SubstrateKit's own value (a `Double` per number) would not.
/// A kind is spelled the same on the phone and in the repository (the
/// package is published, decision record 0214), so nothing is respelled.
public struct WorkoutsAPI: Sendable {
    public var client: SubstrateClient

    public init(client: SubstrateClient) {
        self.client = client
    }

    /// A record path under `/api/v1/`.
    func path(_ kind: String, _ id: String) -> String { SubstrateClient.recordPath(kind, id) }

    // MARK: Records

    /// One live record, or nil when there is none.
    public func get(_ kind: String, _ id: String) async throws -> FlatRecord? {
        let response = try await client.send("GET", path(kind, id), accept: [404])
        if response.status == 404 { return nil }
        let value = try JSONDecoder().decode(JSONValue.self, from: response.data)
        if case let .object(object) = value, let deleted = object["deletedAt"], deleted != .null { return nil }
        return try Self.record(value)
    }

    /// Every live record of the kinds, by list pages, to the end of the
    /// list. A read that must stop sooner names a
    /// limit and is told when it was reached (`ListTruncated`), never handed
    /// a short list as if it were the whole. A walk the server can no longer
    /// continue starts over once (`SubstrateClient.walk`).
    public func list(_ kinds: [String], properties: [String: JSONValue] = [:], limit: Int? = nil) async throws -> [FlatRecord] {
        var out: [FlatRecord] = []
        try await client.walk(ListQuery(kinds: kinds, properties: properties, first: 500)) { query in
            let response = try await client.send("GET", "api/v1/records", query: query.queryItems)
            let page = try JSONDecoder().decode(Page.self, from: response.data)
            if query.after == nil { out = [] }
            out += try page.records.map { try Self.record($0) }
            guard let cursor = page.cursor, !cursor.isEmpty else { return nil }
            if let limit, out.count >= limit { throw ListTruncated(kinds: kinds, limit: limit) }
            return cursor
        }
        return out
    }

    /// A write: a PUT merges, a PATCH merges and clears what it sets to
    /// null. Answers the record's new version.
    @discardableResult
    public func write(_ method: String, _ kind: String, _ id: String, properties: JSONObject, ifVersion: Int? = nil) async throws -> Int {
        var body = JSONObject()
        body["properties"] = .object(properties)
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
    /// holds none (or the token may not see it: a token sees the rows of
    /// the packages its read and write scopes name).
    public func packageRow(_ id: String) async throws -> JSONObject? {
        let response = try await client.send("GET", SubstrateClient.recordPath("substrate.reamde.dev/core/package", id), accept: [404])
        if response.status == 404 { return nil }
        guard case let .object(object) = try JSONDecoder().decode(JSONValue.self, from: response.data),
            case let .object(properties)? = object["properties"]
        else { return nil }
        if let deleted = object["deletedAt"], deleted != .null { return nil }
        return properties
    }

    // MARK: Wire

    struct Page: Decodable {
        var records: [JSONValue]
        var cursor: String?

        enum CodingKeys: String, CodingKey { case records, cursor }

        init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            records = try c.decodeIfPresent([JSONValue].self, forKey: .records) ?? []
            cursor = try? c.decodeIfPresent(String.self, forKey: .cursor)
        }
    }

    /// A server record as the codec reads it: `kind`, `id`, `properties`
    /// and `version`, every other field (stamps, labels, property metadata)
    /// left behind.
    static func record(_ value: JSONValue) throws -> FlatRecord {
        guard case let .object(object) = value else { throw CodecError.invalid(kind: "?", id: "?", property: "record", value: "not an object") }
        var flat = JSONObject()
        for key in ["kind", "id", "properties", "version"] {
            if let item = object[key] { flat[key] = item }
        }
        if flat["properties"] == nil { flat["properties"] = .object(JSONObject()) }
        return try FlatRecord(json: .object(flat))
    }

    static func version(of data: Data) throws -> Int {
        guard case let .object(object) = try JSONDecoder().decode(JSONValue.self, from: data),
            case let .number(number)? = object["version"]
        else { return 0 }
        return number.int ?? 0
    }

    static func body(_ value: JSONValue) throws -> RequestBody {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes]
        return .json(try encoder.encode(value))
    }
}

/// A list read reached the limit it was given before the server's list
/// ended.
public struct ListTruncated: LocalizedError, Sendable {
    public let kinds: [String]
    public let limit: Int
    public var errorDescription: String? { "More than \(limit) records of \(kinds.joined(separator: ", ")); the read stopped there." }
}
