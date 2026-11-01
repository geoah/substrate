import Foundation

/// Any JSON value, as the record codec reads and writes it. Numbers keep
/// their digits as a `Decimal`; objects keep their key order for writing
/// (§3.4 rule 8) but compare and hash as unordered maps.
public enum JSONValue: Hashable, Sendable {
    case null
    case bool(Bool)
    case number(Decimal)
    case string(String)
    case array([JSONValue])
    case object(JSONObject)
}

/// A JSON object whose keys keep the order they were set in. Equality and
/// hashing ignore the order, as JSON does.
public struct JSONObject: Hashable, Sendable, Sequence, ExpressibleByDictionaryLiteral {
    public typealias Key = String
    public typealias Value = JSONValue

    public private(set) var keys: [String] = []
    private var storage: [String: JSONValue] = [:]

    public init() {}

    /// The pairs in order; a repeated key keeps its first place and last value.
    public init(_ pairs: [(String, JSONValue)]) {
        for (key, value) in pairs {
            self[key] = value
        }
    }

    public init(dictionaryLiteral elements: (String, JSONValue)...) {
        self.init(elements)
    }

    /// Setting a new key appends it; setting nil removes it.
    public subscript(key: String) -> JSONValue? {
        get { storage[key] }
        set {
            if let newValue {
                if storage.updateValue(newValue, forKey: key) == nil { keys.append(key) }
            } else if storage.removeValue(forKey: key) != nil {
                keys.removeAll { $0 == key }
            }
        }
    }

    public var count: Int { keys.count }
    public var isEmpty: Bool { keys.isEmpty }
    public var dictionary: [String: JSONValue] { storage }

    public func makeIterator() -> some IteratorProtocol<(key: String, value: JSONValue)> {
        keys.lazy.map { (key: $0, value: storage[$0]!) }.makeIterator()
    }

    public static func == (lhs: JSONObject, rhs: JSONObject) -> Bool { lhs.storage == rhs.storage }
    public func hash(into hasher: inout Hasher) { hasher.combine(storage) }
}

// MARK: - Reading and writing

public extension JSONValue {
    /// Parses JSON text. Object keys come back sorted, since the parser does
    /// not report their order.
    init(data: Data) throws {
        self = try JSONDecoder().decode(JSONValue.self, from: data)
    }

    /// Any `Encodable` as a JSON value, through `JSONEncoder` with ISO 8601 dates.
    init(encoding value: some Encodable) throws {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        try self.init(data: encoder.encode(value))
    }

    /// The value as UTF-8 JSON text, keys in their order. `pretty` indents by
    /// two spaces, as the generator's Python output does.
    func data(pretty: Bool = true) -> Data {
        var out = ""
        JSONWriter(pretty: pretty).write(self, into: &out, depth: 0)
        if pretty { out.append("\n") }
        return Data(out.utf8)
    }

    /// Decodes the value as `T` through `JSONDecoder` with ISO 8601 dates.
    func decode<T: Decodable>(_ type: T.Type) throws -> T {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return try decoder.decode(type, from: data(pretty: false))
    }

    var stringValue: String? {
        if case let .string(value) = self { value } else { nil }
    }

    var objectValue: JSONObject? {
        if case let .object(value) = self { value } else { nil }
    }

    var arrayValue: [JSONValue]? {
        if case let .array(value) = self { value } else { nil }
    }
}

extension JSONValue: Codable {
    public init(from decoder: any Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Decimal.self) {
            self = .number(value)
        } else if let value = try? container.decode(String.self) {
            self = .string(value)
        } else if let value = try? container.decode([JSONValue].self) {
            self = .array(value)
        } else {
            self = try .object(container.decode(JSONObject.self))
        }
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .null: try container.encodeNil()
        case let .bool(value): try container.encode(value)
        case let .number(value): try container.encode(value)
        case let .string(value): try container.encode(value)
        case let .array(value): try container.encode(value)
        case let .object(value): try container.encode(value)
        }
    }
}

extension JSONObject: Codable {
    private struct Name: CodingKey {
        var stringValue: String
        var intValue: Int? { nil }
        init(stringValue: String) { self.stringValue = stringValue }
        init?(intValue _: Int) { nil }
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: Name.self)
        self.init()
        for key in container.allKeys.sorted(by: { $0.stringValue < $1.stringValue }) {
            self[key.stringValue] = try container.decode(JSONValue.self, forKey: key)
        }
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: Name.self)
        for (key, value) in self {
            try container.encode(value, forKey: Name(stringValue: key))
        }
    }
}

/// Writes JSON with object keys in their stored order, which `JSONEncoder`
/// does not promise.
private struct JSONWriter {
    let pretty: Bool

    func write(_ value: JSONValue, into out: inout String, depth: Int) {
        switch value {
        case .null: out.append("null")
        case let .bool(flag): out.append(flag ? "true" : "false")
        case let .number(number): out.append(DecimalText.format(number))
        case let .string(text): writeString(text, into: &out)
        case let .array(items):
            guard !items.isEmpty else { out.append("[]"); return }
            out.append("[")
            for (index, item) in items.enumerated() {
                if index > 0 { out.append(",") }
                newline(into: &out, depth: depth + 1)
                write(item, into: &out, depth: depth + 1)
            }
            newline(into: &out, depth: depth)
            out.append("]")
        case let .object(object):
            guard !object.isEmpty else { out.append("{}"); return }
            out.append("{")
            for (index, (key, item)) in object.enumerated() {
                if index > 0 { out.append(",") }
                newline(into: &out, depth: depth + 1)
                writeString(key, into: &out)
                out.append(pretty ? ": " : ":")
                write(item, into: &out, depth: depth + 1)
            }
            newline(into: &out, depth: depth)
            out.append("}")
        }
    }

    private func newline(into out: inout String, depth: Int) {
        guard pretty else { return }
        out.append("\n")
        out.append(String(repeating: "  ", count: depth))
    }

    private func writeString(_ text: String, into out: inout String) {
        out.append("\"")
        for scalar in text.unicodeScalars {
            switch scalar {
            case "\"": out.append("\\\"")
            case "\\": out.append("\\\\")
            case "\n": out.append("\\n")
            case "\r": out.append("\\r")
            case "\t": out.append("\\t")
            case "\u{08}": out.append("\\b")
            case "\u{0C}": out.append("\\f")
            case let control where control.value < 0x20:
                let hex = String(control.value, radix: 16)
                out.append("\\u" + String(repeating: "0", count: 4 - hex.count) + hex)
            default: out.unicodeScalars.append(scalar)
            }
        }
        out.append("\"")
    }
}
