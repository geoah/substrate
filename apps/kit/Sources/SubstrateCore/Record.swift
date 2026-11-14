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

    public init(
        id: String, kind: String, properties: [String: JSONValue], version: Int = 1,
        createdAt: String = "", updatedAt: String = "", annotations: [String: JSONValue]? = nil
    ) {
        self.id = id
        self.kind = kind
        self.properties = properties
        self.version = version
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.annotations = annotations
    }
}

public extension Record {
    /// The record's heading: the derived `title`, else the property kinds
    /// most often title themselves from.
    var title: String? { recordTitle(.object(JSONObject(properties))) }

    var reference: RecordReference { RecordReference(kind: kind, id: id) }
}

/// A heading from a record's properties.
public func recordTitle(_ properties: JSONValue?) -> String? {
    properties?["title"]?.text ?? properties?["name"]?.text ?? properties?["summary"]?.text ?? properties?["subject"]?.text
}

/// A kind reference split in three: the authority (the one segment with a
/// dot, or `~`), the package and the name (decision 0047).
public struct KindReference: Hashable, Sendable {
    /// The authority of the repository's own packages (decision record
    /// 0259): what it declares and the samples it copies sit under `~`,
    /// whatever the repository is called. The one authority without a dot.
    public static let local = "~"

    public var authority: String
    public var package: String
    public var name: String

    public init?(_ reference: String) {
        let parts = reference.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
        guard parts.count == 3, parts[0] == Self.local || parts[0].contains("."), !parts[1].isEmpty, !parts[2].isEmpty
        else { return nil }
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

/// What a record is called before it is read: a slug id names it, a minted
/// one says only what kind of thing it is.
public func fallbackName(_ reference: RecordReference) -> String {
    isMintedID(reference.recordID) ? kindDisplayName(reference.kind) : reference.recordID
}

/// Whether `id` has a shape a server mints or takes from an app as minted,
/// so it names nothing: 26 lowercase Crockford base32 characters, the first
/// 0 to 7; a UUID's canonical lowercase text; or the 12 lowercase base32
/// characters servers minted before either.
func isMintedID(_ id: String) -> Bool {
    let chars = Array(id.utf8)
    func all(_ range: Range<Int>, _ allowed: (UInt8) -> Bool) -> Bool { chars[range].allSatisfy(allowed) }
    let lowerHex: (UInt8) -> Bool = { (0x30 ... 0x39).contains($0) || (0x61 ... 0x66).contains($0) }
    switch chars.count {
    case 12:
        // RFC 4648 base32, lowercase: a to z and 2 to 7.
        return all(0 ..< 12) { (0x61 ... 0x7A).contains($0) || (0x32 ... 0x37).contains($0) }
    case 26:
        // Crockford base32, lowercase: no i, l, o or u.
        let crockford = Set("0123456789abcdefghjkmnpqrstvwxyz".utf8)
        return (0x30 ... 0x37).contains(chars[0]) && all(1 ..< 26) { crockford.contains($0) }
    case 36:
        let dashes: Set = [8, 13, 18, 23]
        return chars.indices.allSatisfy { dashes.contains($0) ? chars[$0] == 0x2D : lowerHex(chars[$0]) }
    default:
        return false
    }
}
