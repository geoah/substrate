import Foundation
import Observation

/// How the record views read: an app hands them one over the client it
/// already has, with its own session, timeouts and sign-in.
public protocol RecordReader: Sendable {
    /// One record; nil when there is none or it was deleted. A record the
    /// app may not read throws `RecordReadError.notAllowed`.
    func record(_ reference: RecordReference) async throws -> Record?
    /// A kind's declaration, where the app may read it.
    func declaration(of kind: String) async throws -> KindDeclaration?
    /// Where the record opens in full, in a browser.
    func webURL(for reference: RecordReference) -> URL?
    /// What to tell a reader who may not read a record: where access is
    /// granted.
    var deniedHint: String? { get }
}

public extension RecordReader {
    func declaration(of kind: String) async throws -> KindDeclaration? { nil }
    func webURL(for reference: RecordReference) -> URL? { nil }
    var deniedHint: String? { nil }
}

public enum RecordReadError: Error, Sendable, Equatable {
    /// The app's access does not reach the record's kind.
    case notAllowed
}

/// A reader over plain HTTP with a bearer token, for an app with no client
/// of its own: a `SubstrateClient`'s record read.
public struct HTTPRecordReader: RecordReader {
    public let client: SubstrateClient
    public let deniedHint: String?

    public init(client: SubstrateClient, deniedHint: String? = nil) {
        self.client = client
        self.deniedHint = deniedHint
    }

    public init(server: URL, token: String, session: URLSession = .shared, deniedHint: String? = nil) {
        self.init(client: SubstrateClient(server: server, token: token, transport: URLSessionTransport(session: session)),
                  deniedHint: deniedHint)
    }

    public var server: URL { client.server }
    public var token: String { client.token ?? "" }
    public var session: URLSession { (client.transport as? URLSessionTransport)?.session ?? URLSessionTransport.shared.session }

    public func record(_ reference: RecordReference) async throws -> Record? {
        do {
            return try await client.record(reference.kind, reference.recordID)
        } catch is SessionExpired {
            throw RecordReadError.notAllowed
        } catch is InsufficientScope {
            throw RecordReadError.notAllowed
        } catch let error as ServerError where error.status == 403 {
            throw RecordReadError.notAllowed
        }
    }

    public func declaration(of kind: String) async throws -> KindDeclaration? {
        try await record(RecordReference(kind: KindDeclaration.kind, id: kind)).flatMap(KindDeclaration.init(record:))
    }
}

/// The part of a kind's declaration the record views read: its names and
/// each property's type and words.
public struct KindDeclaration: Hashable, Sendable {
    public static let kind = "substrate.reamde.dev/core/kind"

    public var reference: String
    /// The declared label, which wins over the name built from words.
    public var label: String?
    public var description: String?
    public var properties: [String: PropertyDeclaration]

    public init?(record: Record) {
        guard record.kind == KindDeclaration.kind else { return nil }
        reference = record.id
        label = record["label"]?["singular"]?.text ?? record["label"]?.text
        description = record["description"]?.text
        var props: [String: PropertyDeclaration] = [:]
        for (name, value) in record["properties"]?.object ?? [:] {
            props[name] = PropertyDeclaration(value)
        }
        properties = props
    }

    public var displayName: String { label ?? kindDisplayName(reference) }
}

public struct PropertyDeclaration: Hashable, Sendable {
    public var type: String?
    public var description: String?
    /// An enum's values with their labels, where declared.
    public var values: [String: String]
    /// A reference's target kind.
    public var target: String?

    public init(_ value: JSONValue) {
        type = value["type"]?.text
        description = value["description"]?.text
        target = value["kind"]?.text
        var labels: [String: String] = [:]
        for item in value["values"]?.array ?? [] {
            if let v = item.text {
                labels[v] = v.splittingWords().map { $0.lowercased() }.joined(separator: " ").capitalizedFirst
            } else if let v = item["value"]?.text {
                labels[v] = item["label"]?.text ?? v.splittingWords().map { $0.lowercased() }.joined(separator: " ").capitalizedFirst
            }
        }
        values = labels
    }

    public func label(of value: String) -> String? { values[value] }
}

/// The records and declarations the views have read, shared by every pill
/// and panel of a scene: a name is read once, a record the app may not read
/// is asked for once.
@MainActor
@Observable
public final class RecordDirectory {
    public let reader: (any RecordReader)?
    private(set) var records: [String: Record] = [:]
    private(set) var denied: Set<String> = []
    private var missing: Set<String> = []
    private var asking: [String: Task<Record?, Error>] = [:]
    private var declarations: [String: KindDeclaration?] = [:]

    public init(reader: (any RecordReader)?) {
        self.reader = reader
    }

    /// A read record's heading.
    public func title(for reference: RecordReference) -> String? {
        records[reference.path]?.title
    }

    /// Reads the record once, for its name; quietly does nothing more.
    public func resolve(_ reference: RecordReference) async {
        _ = try? await record(reference)
    }

    /// The record, read once and shared: a second caller waits on the first
    /// read rather than starting its own.
    public func record(_ reference: RecordReference) async throws -> Record? {
        let key = reference.path
        if let held = records[key] { return held }
        if denied.contains(key) { throw RecordReadError.notAllowed }
        if missing.contains(key) { return nil }
        guard let reader else { return nil }
        let read = asking[key] ?? Task { try await reader.record(reference) }
        asking[key] = read
        defer { asking[key] = nil }
        do {
            let record = try await read.value
            if let record { records[key] = record } else { missing.insert(key) }
            return record
        } catch RecordReadError.notAllowed {
            denied.insert(key)
            throw RecordReadError.notAllowed
        }
    }

    /// Reads a record again, past what is held.
    public func reload(_ reference: RecordReference) async throws -> Record? {
        records[reference.path] = nil
        missing.remove(reference.path)
        denied.remove(reference.path)
        return try await record(reference)
    }

    public func declaration(of kind: String) async -> KindDeclaration? {
        if let held = declarations[kind] { return held }
        let read = try? await reader?.declaration(of: kind)
        declarations[kind] = .some(read ?? nil)
        return read ?? nil
    }

    /// Forgets everything: another account signed in.
    public func clear() {
        records = [:]
        denied = []
        missing = []
        declarations = [:]
    }
}
