import Foundation

/// One record as the API serves it: its identity, its properties and its
/// stamps. The rest of the envelope (labels, finalizers, provenance) is not
/// kept.
public struct Record: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var kind: String
    public var properties: [String: JSONValue]
    public var annotations: [String: JSONValue]?
    public var version: Int
    public var createdAt: String
    public var updatedAt: String
    public var deletedAt: String?

    public subscript(property: String) -> JSONValue? { properties[property] }

    public var createdDate: Date { Timestamp.parse(createdAt) ?? .distantPast }
    public var updatedDate: Date { Timestamp.parse(updatedAt) ?? createdDate }

    enum CodingKeys: String, CodingKey {
        case id, kind, properties, annotations, version, createdAt, updatedAt, deletedAt
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        properties = (try? c.decodeIfPresent([String: JSONValue].self, forKey: .properties)) ?? [:]
        annotations = try? c.decodeIfPresent([String: JSONValue].self, forKey: .annotations)
        version = (try? c.decodeIfPresent(Int.self, forKey: .version)) ?? 0
        createdAt = (try? c.decodeIfPresent(String.self, forKey: .createdAt)) ?? ""
        updatedAt = (try? c.decodeIfPresent(String.self, forKey: .updatedAt)) ?? ""
        deletedAt = try? c.decodeIfPresent(String.self, forKey: .deletedAt)
    }

    public init(id: String, kind: String, properties: [String: JSONValue], version: Int = 1,
         createdAt: String = "", updatedAt: String = "", annotations: [String: JSONValue]? = nil)
    {
        self.id = id
        self.kind = kind
        self.properties = properties
        self.version = version
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.annotations = annotations
    }
}

/// The server's RFC 3339 stamps carry up to nine fractional digits, which
/// `ISO8601DateFormatter` does not read; the fraction is cut to three first.
public enum Timestamp {
    private static let withFraction: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()

    private static let plain: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()

    private static let lock = NSLock()

    public static func parse(_ text: String) -> Date? {
        guard !text.isEmpty else { return nil }
        var s = text
        if let dot = s.firstIndex(of: ".") {
            var end = s.index(after: dot)
            while end < s.endIndex, s[end].isNumber { end = s.index(after: end) }
            let digits = s.distance(from: s.index(after: dot), to: end)
            if digits > 3 { s.removeSubrange(s.index(dot, offsetBy: 4) ..< end) }
        }
        lock.lock()
        defer { lock.unlock() }
        return withFraction.date(from: s) ?? plain.date(from: s)
    }

    /// RFC 3339 in UTC with milliseconds, which `parse` reads back.
    public static func format(_ date: Date) -> String {
        lock.lock()
        defer { lock.unlock() }
        return withFraction.string(from: date)
    }
}

public extension Record {
    /// The record's heading: the derived `title`, else the property kinds
    /// most often title themselves from.
    var title: String? { recordTitle(.object(properties)) }

    var reference: RecordReference { RecordReference(kind: kind, id: id) }
}

/// A heading from a record's properties.
public func recordTitle(_ properties: JSONValue?) -> String? {
    properties?["title"]?.text ?? properties?["name"]?.text ?? properties?["summary"]?.text ?? properties?["subject"]?.text
}

/// A kind reference split in three: the authority (the one segment with a
/// dot), the package and the name (decision 0047).
public struct KindReference: Hashable, Sendable {
    public var authority: String
    public var package: String
    public var name: String

    public init?(_ reference: String) {
        let parts = reference.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        guard parts.count == 3, parts[0].contains("."), !parts[1].isEmpty, !parts[2].isEmpty else { return nil }
        (authority, package, name) = (parts[0], parts[1], parts[2])
    }

    public var reference: String { "\(authority)/\(package)/\(name)" }
}

/// A typed pointer at one record, stored as `{ref: <kind>/<id>}`
/// (decision 0044).
public struct RecordReference: Hashable, Sendable, Codable, Identifiable {
    public var kind: String
    public var recordID: String

    public init(kind: String, id: String) {
        self.kind = kind
        recordID = id
    }

    /// From a stored path: the kind is its first three segments and the id
    /// the rest, since some ids (an agent's) hold slashes of their own.
    public init?(path: String) {
        let parts = path.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        guard parts.count >= 4, KindReference(parts[0 ..< 3].joined(separator: "/")) != nil else { return nil }
        let id = parts[3...].joined(separator: "/")
        guard !id.isEmpty else { return nil }
        self.init(kind: parts[0 ..< 3].joined(separator: "/"), id: id)
    }

    /// From a property's value: `{ref: <path>}` as served, or the bare path.
    public init?(_ value: JSONValue?) {
        guard let path = value?["ref"]?.text ?? value?.text else { return nil }
        self.init(path: path)
    }

    public var path: String { "\(kind)/\(recordID)" }
    public var id: String { path }
}
