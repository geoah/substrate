import Foundation

/// Any JSON value. A number keeps its decimal digits as a `Decimal`, so a
/// decimal property read and written again is the one the server sent; an
/// object keeps its keys in the order they were set (`JSONObject`) and
/// compares as a map. Record properties are whatever their kind declares,
/// and the engine writes some of them in shapes a client reads tolerantly:
/// an unexpected shape reads as absent, never as a crash.
public enum JSONValue: Hashable, Sendable {
    case null
    case bool(Bool)
    case number(Decimal)
    case string(String)
    case array([JSONValue])
    case object(JSONObject)
}

// MARK: - Reading a value

public extension JSONValue {
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

    /// The number as a `Double`, for arithmetic and display; nil for a
    /// value that is not a number or does not fit one.
    var number: Double? {
        guard case let .number(n) = self, let d = Double(n.description), d.isFinite else { return nil }
        return d
    }

    /// The number with every digit it was written with.
    var decimal: Decimal? {
        if case let .number(n) = self { return n }
        return nil
    }

    /// The number with its fraction dropped; nil where it does not fit.
    var int: Int? { number.flatMap { Int(exactly: $0.rounded(.towardZero)) } }

    var bool: Bool? {
        if case let .bool(b) = self { return b }
        return nil
    }

    var array: [JSONValue]? {
        if case let .array(a) = self { return a }
        return nil
    }

    /// An object's members as a dictionary, their order dropped.
    var object: [String: JSONValue]? {
        if case let .object(o) = self { return o.dictionary }
        return nil
    }

    var stringValue: String? {
        if case let .string(value) = self { value } else { nil }
    }

    /// An object with its keys in order.
    var objectValue: JSONObject? {
        if case let .object(value) = self { value } else { nil }
    }

    var arrayValue: [JSONValue]? {
        if case let .array(value) = self { value } else { nil }
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
}

// MARK: - JSON text

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

    /// The value as UTF-8 JSON text, keys in their order and each number as
    /// its plain digits. `pretty` indents by two spaces, as Python's output
    /// does.
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

    /// Compact JSON text, keys sorted, for display and for query parameters.
    var jsonText: String {
        if case let .string(s) = self { return s }
        guard JSONSerialization.isValidJSONObject(any) || isScalar,
              let data = try? JSONSerialization.data(withJSONObject: any, options: [.sortedKeys, .withoutEscapingSlashes, .fragmentsAllowed])
        else { return "" }
        return String(decoding: data, as: UTF8.self)
    }

    /// Parses JSON text, or nil when it is not JSON.
    static func parse(_ text: String?) -> JSONValue? {
        guard let text, let data = text.data(using: .utf8),
              let value = try? JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed])
        else { return nil }
        return JSONValue(any: value)
    }

    private var isScalar: Bool {
        switch self {
        case .array, .object: false
        default: true
        }
    }
}

// MARK: - JSONSerialization

public extension JSONValue {
    /// From `JSONSerialization`'s output, for a body decoded loosely.
    init(any: Any?) {
        switch any {
        case nil, is NSNull: self = .null
        case let n as NSNumber: self = JSONValue.number(n)
        case let s as String: self = .string(s)
        case let a as [Any]: self = .array(a.map { JSONValue(any: $0) })
        case let o as [String: Any]: self = .object(JSONObject(o.mapValues { JSONValue(any: $0) }))
        default: self = .null
        }
    }

    /// For `JSONSerialization`, which writes request bodies: a whole number
    /// as an integer, since the server reads a version or a count into one.
    var any: Any {
        switch self {
        case .null: NSNull()
        case let .bool(b): b
        case .number:
            if let n = number { n.rounded() == n && abs(n) < 1e15 ? Int(n) as Any : n } else { NSNull() }
        case let .string(s): s
        case let .array(a): a.map(\.any)
        case let .object(o): o.dictionary.mapValues(\.any)
        }
    }

    /// A Bool arrives as an NSNumber too, and only its type tells them
    /// apart: CFBoolean on Apple systems; elsewhere a `char`, which
    /// JSONSerialization never answers for a number.
    private static func number(_ n: NSNumber) -> JSONValue {
        #if canImport(Darwin)
        if CFGetTypeID(n) == CFBooleanGetTypeID() { return .bool(n.boolValue) }
        #else
        if String(cString: n.objCType) == "c" { return .bool(n.boolValue) }
        #endif
        if let decimal = n as? NSDecimalNumber { return .number(decimal.decimalValue) }
        return .number(Decimal(digitsOf: n.doubleValue))
    }
}

public extension Decimal {
    /// The decimal a `Double` reads as: its shortest round-trip digits, not
    /// the binary fraction `Decimal(_: Double)` spells out (0.1, never
    /// 0.1000000000000000512). NaN for a value that is not finite.
    init(digitsOf double: Double) {
        guard double.isFinite else {
            self = .nan
            return
        }
        self = Decimal(string: "\(double)", locale: Locale(identifier: "en_US_POSIX")) ?? Decimal(double)
    }

    /// The integer this spells, or nil when it has a fraction or does not fit.
    var exactInt: Int? {
        guard !isNaN, isFinite else { return nil }
        var value = self
        var rounded = Decimal()
        NSDecimalRound(&rounded, &value, 0, .plain)
        guard rounded == self, rounded >= Decimal(Int.min), rounded <= Decimal(Int.max) else { return nil }
        return NSDecimalNumber(decimal: rounded).intValue
    }
}

// MARK: - Codable

extension JSONValue: Codable {
    public init(from decoder: any Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Decimal.self) {
            self = .number(value)
        } else if let value = try? container.decode(Double.self) {
            // Past Decimal's range, a number still reads as one.
            self = .number(Decimal(digitsOf: value))
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

// Never ExpressibleByNilLiteral: in a `JSONValue?`, `nil` would then read as
// `.null` wherever the context allows both, and an absent property would be
// written as a null one.
extension JSONValue: ExpressibleByStringLiteral, ExpressibleByBooleanLiteral, ExpressibleByIntegerLiteral,
    ExpressibleByArrayLiteral, ExpressibleByDictionaryLiteral
{
    public init(stringLiteral value: String) { self = .string(value) }
    public init(booleanLiteral value: Bool) { self = .bool(value) }
    public init(integerLiteral value: Int) { self = .number(Decimal(value)) }
    public init(arrayLiteral elements: JSONValue...) { self = .array(elements) }
    public init(dictionaryLiteral elements: (String, JSONValue)...) { self = .object(JSONObject(elements)) }
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
