import Foundation

/// A list read of `GET /api/v1/records`: the kinds in `filter.kinds`, the
/// property arms beside them, and the wire's compact `orderBy`.
public struct ListQuery: Sendable, Hashable {
    public var kinds: [String]
    public var properties: [String: JSONValue]
    public var first: Int
    public var after: String?
    public var orderBy: String?

    public init(kinds: [String], properties: [String: JSONValue] = [:], first: Int = 100, after: String? = nil,
                orderBy: String? = nil)
    {
        self.kinds = kinds
        self.properties = properties
        self.first = first
        self.after = after
        self.orderBy = orderBy
    }

    public var filter: JSONValue {
        var f: [String: JSONValue] = ["kinds": .array(kinds.map(JSONValue.string))]
        if !properties.isEmpty { f["properties"] = .object(properties) }
        return .object(f)
    }

    public var queryItems: [(String, String)] {
        var items = [("first", String(first)), ("filter", filter.jsonText)]
        if let after { items.append(("after", after)) }
        if let orderBy { items.append(("orderBy", orderBy)) }
        return items
    }
}

/// One page of `GET /api/v1/records`. `head` and `generation` are the change
/// feed's handoff: a watch resumed from them sees every write after the read.
public struct RecordPage: Decodable, Sendable {
    public var records: [Record]
    public var cursor: String?
    public var head: Int
    public var generation: String

    enum CodingKeys: String, CodingKey { case records, cursor, head, generation }

    public init(records: [Record], cursor: String? = nil, head: Int = 0, generation: String = "") {
        self.records = records
        self.cursor = cursor
        self.head = head
        self.generation = generation
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        records = try c.decodeIfPresent([Record].self, forKey: .records) ?? []
        cursor = c.value(.cursor, or: nil)
        head = c.value(.head, or: 0)
        generation = c.value(.generation, or: "")
    }
}

public extension SubstrateClient {
    /// A record's REST path: its kind reference, then its id as one segment.
    static func recordPath(_ kind: String, _ id: String) -> String { "api/v1/\(kind)/\(segment(id))" }

    /// One live record, or nil when there is none: a deleted record answers
    /// 200 with `deletedAt` and reads as absent here, as a 404 does.
    func record(_ kind: String, _ id: String, timeout: TimeInterval = Timeout.read) async throws -> Record? {
        let response = try await send("GET", Self.recordPath(kind, id), accept: [404], timeout: timeout)
        if response.status == 404 { return nil }
        let record = try response.decode(Record.self)
        return record.deletedAt == nil ? record : nil
    }

    /// A put merges per property and never prunes; `ifVersion` asserts the
    /// version read, and 0 creates only, leaving a row that exists (live or
    /// deleted) alone with a 409.
    @discardableResult
    func put(_ kind: String, _ id: String, properties: [String: JSONValue], ifVersion: Int? = nil) async throws -> Record {
        try await write("PUT", kind, id, properties: properties, ifVersion: ifVersion)
    }

    /// A patch merges per property; a `.null` clears one.
    @discardableResult
    func patch(_ kind: String, _ id: String, properties: [String: JSONValue], ifVersion: Int? = nil) async throws -> Record {
        try await write("PATCH", kind, id, properties: properties, ifVersion: ifVersion)
    }

    /// Clears properties: each one patched to null.
    func clear(_ kind: String, _ id: String, keys: [String]) async throws {
        guard !keys.isEmpty else { return }
        try await patch(kind, id, properties: Dictionary(uniqueKeysWithValues: keys.map { ($0, JSONValue.null) }))
    }

    /// Deletes a record; one already gone is not an error.
    func delete(_ kind: String, _ id: String) async throws {
        try await send("DELETE", Self.recordPath(kind, id), accept: [404])
    }

    /// Creates a record under an id the server assigns.
    @discardableResult
    func create(_ kind: String, properties: [String: JSONValue]) async throws -> Record {
        let body: JSONValue = ["kind": .string(kind), "properties": .object(properties)]
        return try await send("POST", "api/v1/records", body: .jsonValue(body)).decode(Record.self)
    }

    func list(_ query: ListQuery, timeout: TimeInterval = Timeout.read) async throws -> RecordPage {
        try await send("GET", "api/v1/records", query: query.queryItems, timeout: timeout).decode(RecordPage.self)
    }

    /// Every page of a read, up to `limit` rows, the first page's head kept.
    func listAll(_ query: ListQuery, limit: Int = 1000) async throws -> RecordPage {
        var all = try await list(query)
        var cursor = all.cursor
        while let next = cursor, all.records.count < limit {
            var q = query
            q.after = next
            let page = try await list(q)
            all.records += page.records
            cursor = page.cursor
        }
        all.cursor = cursor
        return all
    }

    private func write(_ method: String, _ kind: String, _ id: String, properties: [String: JSONValue],
                       ifVersion: Int?) async throws -> Record
    {
        var body: [String: JSONValue] = ["properties": .object(properties)]
        if let ifVersion { body["ifVersion"] = .number(Double(ifVersion)) }
        return try await send(method, Self.recordPath(kind, id), body: .jsonValue(.object(body))).decode(Record.self)
    }
}
