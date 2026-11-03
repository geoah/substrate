import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SubstrateCore

/// The record calls an outbox sends its writes through: a put or a patch
/// with the properties as the app spelled them (key order and decimal
/// digits kept), the version of a live record, and a purge. The client is
/// one; an app whose writes need another spelling brings its own.
public protocol RecordWriter: Sendable {
    /// A PUT merges, a PATCH merges and clears what it sets to null; with
    /// `ifVersion`, only over that version (0 creates only). Answers the
    /// record's new version.
    func write(_ method: String, _ kind: String, _ id: String, properties: JSONObject, ifVersion: Int?) async throws -> Int
    /// The live record's version, nil when there is none.
    func version(_ kind: String, _ id: String) async throws -> Int?
    /// Deletes and purges the record at once, so a create replayed later
    /// starts afresh.
    func purge(_ kind: String, _ id: String, ifVersion: Int?) async throws
}

extension SubstrateClient: RecordWriter {
    public func write(_ method: String, _ kind: String, _ id: String, properties: JSONObject, ifVersion: Int?) async throws -> Int {
        var body = JSONObject()
        body["properties"] = .object(properties)
        if let ifVersion { body["ifVersion"] = .number(Decimal(ifVersion)) }
        let response = try await send(method, Self.recordPath(kind, id), body: .encoding(JSONValue.object(body)))
        guard case let .object(object) = try JSONDecoder().decode(JSONValue.self, from: response.data),
              case let .number(version)? = object["version"]
        else { return 0 }
        return NSDecimalNumber(decimal: version).intValue
    }

    public func version(_ kind: String, _ id: String) async throws -> Int? {
        try await record(kind, id)?.version
    }

    public func purge(_ kind: String, _ id: String, ifVersion: Int?) async throws {
        var query = [("purge", "true")]
        if let ifVersion { query.append(("ifVersion", String(ifVersion))) }
        try await send("DELETE", Self.recordPath(kind, id), query: query, accept: [404])
    }
}
