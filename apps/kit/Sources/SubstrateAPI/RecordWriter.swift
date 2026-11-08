import Foundation
import SubstrateCore

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// The record calls an outbox sends its writes through: a put or a patch
/// with the properties as the app spelled them (key order and decimal
/// digits kept), a live record and its version, and a purge. The client is
/// one; an app whose writes need another spelling brings its own.
public protocol RecordWriter: Sendable {
    /// A PUT merges, a PATCH merges and clears what it sets to null; with
    /// `ifVersion`, only over that version (0 creates only). Answers the
    /// record's new version.
    func write(_ method: String, _ kind: String, _ id: String, properties: JSONObject, ifVersion: Int?) async throws -> Int
    /// The live record's version, nil when there is none.
    func version(_ kind: String, _ id: String) async throws -> Int?
    /// The live record as the server holds it, nil when there is none.
    func stored(_ kind: String, _ id: String) async throws -> Record?
    /// Deletes and purges the record at once, so a create replayed later
    /// starts afresh.
    func purge(_ kind: String, _ id: String, ifVersion: Int?) async throws
    /// Deletes the record and keeps it, so it can be restored; one already
    /// gone is not an error.
    func delete(_ kind: String, _ id: String, ifVersion: Int?) async throws
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
        return (version.int ?? 0)
    }

    public func version(_ kind: String, _ id: String) async throws -> Int? {
        try await record(kind, id)?.version
    }

    public func stored(_ kind: String, _ id: String) async throws -> Record? {
        try await record(kind, id)
    }

    public func purge(_ kind: String, _ id: String, ifVersion: Int?) async throws {
        var query = [("purge", "true")]
        if let ifVersion { query.append(("ifVersion", String(ifVersion))) }
        try await send("DELETE", Self.recordPath(kind, id), query: query, accept: [404])
    }

    public func delete(_ kind: String, _ id: String, ifVersion: Int?) async throws {
        try await send("DELETE", Self.recordPath(kind, id), query: ifVersion.map { [("ifVersion", String($0))] } ?? [], accept: [404])
    }
}
