import Foundation

/// The kinds this app reads and writes, each named in full on every surface
/// (decision 0101). The agent row is core's; the runtime rows it produces are
/// the seeded `llm` package's (decision 0077).
enum Kinds {
    static let core = "substrate.reamde.dev/core"
    static let llm = "substrate.reamde.dev/llm"
    static let agent = "\(core)/agent"
    static let request = "\(core)/recordpatchrequest"
    static let provider = "\(llm)/provider"
    static let thread = "\(llm)/thread"
    static let message = "\(llm)/message"
    static let interaction = "\(llm)/interaction"
    /// The interaction kind before record 0077 moved it out of core: a
    /// stamp written then still spells it, and must still find its card.
    static let interactionBeforeMove = "\(core)/interaction"
}

/// One record as the API serves it. Only what a chat reads is kept; the
/// rest of the envelope (labels, finalizers, provenance) never reaches disk.
struct Record: Codable, Hashable, Sendable, Identifiable {
    var id: String
    var kind: String
    var properties: [String: JSONValue]
    var annotations: [String: JSONValue]?
    var version: Int
    var createdAt: String
    var updatedAt: String
    var deletedAt: String?

    subscript(property: String) -> JSONValue? { properties[property] }

    var createdDate: Date { Timestamp.parse(createdAt) ?? .distantPast }
    var updatedDate: Date { Timestamp.parse(updatedAt) ?? createdDate }

    enum CodingKeys: String, CodingKey {
        case id, kind, properties, annotations, version, createdAt, updatedAt, deletedAt
    }

    init(from decoder: Decoder) throws {
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

    init(id: String, kind: String, properties: [String: JSONValue], version: Int = 1,
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

/// One page of `GET /api/v1/records`. `head` and `generation` are the change
/// feed's handoff: a watch resumed from them sees every write after the read.
struct Page: Decodable, Sendable {
    var records: [Record]
    var cursor: String?
    var head: Int
    var generation: String

    enum CodingKeys: String, CodingKey { case records, cursor, head, generation }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        records = try c.decodeIfPresent([Record].self, forKey: .records) ?? []
        cursor = try? c.decodeIfPresent(String.self, forKey: .cursor)
        head = (try? c.decodeIfPresent(Int.self, forKey: .head)) ?? 0
        generation = (try? c.decodeIfPresent(String.self, forKey: .generation)) ?? ""
    }
}

/// The server's RFC 3339 stamps carry up to nine fractional digits, which
/// `ISO8601DateFormatter` does not read; the fraction is cut to three first.
enum Timestamp {
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

    static func parse(_ text: String) -> Date? {
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
}
