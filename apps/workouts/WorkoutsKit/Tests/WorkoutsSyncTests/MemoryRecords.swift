import Foundation
import SubstrateAPI
import SubstrateAuth
import SubstrateDevice
import WorkoutsCore
@testable import WorkoutsSync
import SubstrateCore

/// A substrate's records in memory, behind a scripted server: the list read
/// (`GET /api/v1/records`, by `filter.kinds`), one record's read, and PUT,
/// PATCH and DELETE with `ifVersion`, as the write path answers them (a 409
/// for a version that moved, a create at version 0 over a live record).
/// Kinds are the repository's (`ada.example.com/training/...`).
final class MemoryRecords: @unchecked Sendable {
    struct Stored {
        var kind: String
        var id: String
        var properties: JSONObject
        var version: Int
    }

    let server = ScriptedServer()
    private let lock = NSLock()
    private var records: [String: Stored] = [:]
    /// Called before a write lands; answering a status refuses it.
    var refuse: (@Sendable (String, String, String) -> Int?)?

    init() {
        server.on { [unowned self] sent in self.answer(sent) }
    }

    var api: WorkoutsAPI { WorkoutsSyncTests.api(server) }

    func put(_ kind: String, _ id: String, _ properties: JSONObject) {
        lock.withLock { records["\(kind)/\(id)"] = Stored(kind: kind, id: id, properties: properties, version: 1) }
    }

    func delete(_ kind: String, _ id: String) { _ = lock.withLock { records.removeValue(forKey: "\(kind)/\(id)") } }

    func get(_ kind: String, _ id: String) -> Stored? { lock.withLock { records["\(kind)/\(id)"] } }

    func all(_ kind: String) -> [Stored] { lock.withLock { records.values.filter { $0.kind == kind }.sorted { $0.id < $1.id } } }

    var writes: [String] {
        server.methodsAndPaths.filter { !$0.hasPrefix("GET") }
    }

    private func answer(_ sent: ScriptedServer.Sent) -> (Int, String)? {
        let path = sent.path.removingPercentEncoding ?? sent.path
        if sent.method == "GET", path == "/api/v1/records" {
            let query = sent.query?.removingPercentEncoding ?? ""
            let listed = lock.withLock { records.values.filter { query.contains("\"\($0.kind)\"") }.sorted { $0.id < $1.id } }
            return (200, json(.object(["records": .array(listed.map(Self.value))])))
        }
        guard path.hasPrefix("/api/v1/") else { return nil }
        let rest = String(path.dropFirst("/api/v1/".count))
        guard let slash = rest.lastIndex(of: "/") else { return nil }
        let kind = String(rest[..<slash])
        let id = String(rest[rest.index(after: slash)...])
        let key = "\(kind)/\(id)"
        if let status = refuse?(sent.method, kind, id) {
            return (status, #"{"error":{"code":"refused","message":"refused"}}"#)
        }
        return lock.withLock { () -> (Int, String)? in
            let current = records[key]
            var ifVersion: Int?
            if case let .object(body)? = sent.body, case let .number(version)? = body["ifVersion"] {
                ifVersion = NSDecimalNumber(decimal: version).intValue
            }
            switch sent.method {
            case "GET":
                guard let current else { return (404, #"{"error":{"code":"not_found","message":"no record"}}"#) }
                return (200, json(Self.value(current)))
            case "PUT", "PATCH":
                if let ifVersion, ifVersion != (current?.version ?? 0) {
                    return (409, #"{"error":{"code":"conflict","message":"version moved"}}"#)
                }
                guard case let .object(body)? = sent.body, case let .object(properties)? = body["properties"] else { return (400, "{}") }
                var stored = current ?? Stored(kind: kind, id: id, properties: [:], version: 0)
                for (name, value) in properties {
                    if value == .null { stored.properties[name] = nil } else { stored.properties[name] = value }
                }
                stored.version += 1
                records[key] = stored
                return (200, json(Self.value(stored)))
            case "DELETE":
                records[key] = nil
                return (200, "{}")
            default:
                return nil
            }
        }
    }

    private static func value(_ stored: Stored) -> JSONValue {
        .object(["kind": .string(stored.kind), "id": .string(stored.id), "properties": .object(stored.properties),
                 "version": .number(Decimal(stored.version))])
    }

    private func json(_ object: JSONObject) -> String { json(.object(object)) }

    private func json(_ value: JSONValue) -> String {
        String(decoding: (try? JSONEncoder().encode(value)) ?? Data(), as: UTF8.self)
    }
}
