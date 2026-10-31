import Foundation

/// Any JSON value. Record properties are whatever their kind declares, and
/// the engine writes some of them (`toolCalls`, `changes`, `covers`) in shapes
/// the app reads tolerantly: an unexpected shape reads as absent, never as a
/// crash.
enum JSONValue: Codable, Hashable, Sendable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() {
            self = .null
        } else if let b = try? c.decode(Bool.self) {
            self = .bool(b)
        } else if let n = try? c.decode(Double.self) {
            self = .number(n)
        } else if let s = try? c.decode(String.self) {
            self = .string(s)
        } else if let a = try? c.decode([JSONValue].self) {
            self = .array(a)
        } else {
            self = .object(try c.decode([String: JSONValue].self))
        }
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .null: try c.encodeNil()
        case let .bool(b): try c.encode(b)
        case let .number(n): try c.encode(n)
        case let .string(s): try c.encode(s)
        case let .array(a): try c.encode(a)
        case let .object(o): try c.encode(o)
        }
    }

    subscript(key: String) -> JSONValue? {
        if case let .object(o) = self { return o[key] }
        return nil
    }

    var string: String? {
        if case let .string(s) = self { return s }
        return nil
    }

    /// A string with something in it after trimming.
    var text: String? {
        guard let s = string?.trimmingCharacters(in: .whitespacesAndNewlines), !s.isEmpty else { return nil }
        return s
    }

    var number: Double? {
        if case let .number(n) = self, n.isFinite { return n }
        return nil
    }

    var int: Int? { number.map { Int($0) } }

    var bool: Bool? {
        if case let .bool(b) = self { return b }
        return nil
    }

    var array: [JSONValue]? {
        if case let .array(a) = self { return a }
        return nil
    }

    var object: [String: JSONValue]? {
        if case let .object(o) = self { return o }
        return nil
    }

    /// The path a reference property holds: served as `{ref: <path>}`,
    /// authored as the bare path, and both arrive here.
    var referencePath: String? {
        if let s = text { return s }
        return self["ref"]?.text
    }

    /// The last segment of a reference's path: the referenced record's id.
    var referenceID: String? {
        guard let path = referencePath else { return nil }
        let id = path.split(separator: "/").last.map(String.init) ?? path
        return id.isEmpty ? nil : id
    }

    /// From `JSONSerialization`'s output, for a body decoded loosely.
    init(any: Any?) {
        switch any {
        case nil, is NSNull: self = .null
        case let n as NSNumber:
            // A Bool arrives as an NSNumber too; CFBoolean tells them apart.
            if CFGetTypeID(n) == CFBooleanGetTypeID() { self = .bool(n.boolValue) } else { self = .number(n.doubleValue) }
        case let s as String: self = .string(s)
        case let a as [Any]: self = .array(a.map { JSONValue(any: $0) })
        case let o as [String: Any]: self = .object(o.mapValues { JSONValue(any: $0) })
        default: self = .null
        }
    }

    /// For `JSONSerialization`, which writes request bodies.
    var any: Any {
        switch self {
        case .null: NSNull()
        case let .bool(b): b
        case let .number(n): n.rounded() == n && abs(n) < 1e15 ? Int(n) as Any : n
        case let .string(s): s
        case let .array(a): a.map(\.any)
        case let .object(o): o.mapValues(\.any)
        }
    }

    /// Compact JSON text, keys sorted, for display and for query parameters.
    var jsonText: String {
        if case let .string(s) = self { return s }
        guard JSONSerialization.isValidJSONObject(any) || isScalar,
              let data = try? JSONSerialization.data(withJSONObject: any, options: [.sortedKeys, .withoutEscapingSlashes, .fragmentsAllowed])
        else { return "" }
        return String(decoding: data, as: UTF8.self)
    }

    private var isScalar: Bool {
        switch self {
        case .array, .object: false
        default: true
        }
    }

    /// Parses JSON text, or nil when it is not JSON.
    static func parse(_ text: String?) -> JSONValue? {
        guard let text, let data = text.data(using: .utf8),
              let value = try? JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed])
        else { return nil }
        return JSONValue(any: value)
    }
}

extension JSONValue: ExpressibleByStringLiteral, ExpressibleByBooleanLiteral, ExpressibleByIntegerLiteral,
    ExpressibleByArrayLiteral, ExpressibleByDictionaryLiteral, ExpressibleByNilLiteral
{
    init(stringLiteral value: String) { self = .string(value) }
    init(booleanLiteral value: Bool) { self = .bool(value) }
    init(integerLiteral value: Int) { self = .number(Double(value)) }
    init(arrayLiteral elements: JSONValue...) { self = .array(elements) }
    init(dictionaryLiteral elements: (String, JSONValue)...) {
        self = .object(Dictionary(elements) { _, last in last })
    }
    init(nilLiteral: ()) { self = .null }
}
