import Foundation

/// Any JSON value. A number keeps its decimal text (`JSONNumber`), so a
/// number read and written again is the one the server sent, every digit
/// of it; an object keeps its keys in the order they were set
/// (`JSONObject`) and compares as a map. Record properties are whatever
/// their kind declares, and the engine writes some of them in shapes a
/// client reads tolerantly: an unexpected shape reads as absent, never as a
/// crash.
public enum JSONValue: Hashable, Sendable {
    case null
    case bool(Bool)
    case number(JSONNumber)
    case string(String)
    case array([JSONValue])
    case object(JSONObject)

    /// A number with every digit `value` holds.
    public static func number(_ value: Decimal) -> JSONValue {
        .number(JSONNumber(value))
    }
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
        if case let .number(n) = self { return n.double }
        return nil
    }

    /// The number with every digit it was written with; nil past what a
    /// `Decimal` holds exactly.
    var decimal: Decimal? {
        if case let .number(n) = self { return n.decimal }
        return nil
    }

    /// The number with its fraction dropped; nil where it does not fit.
    var int: Int? {
        if case let .number(n) = self { return n.int }
        return nil
    }

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
    /// Parses JSON text, each number kept as written and each object's keys
    /// in the order they were written. Throws `DecodingError.dataCorrupted`
    /// for anything RFC 8259 does not allow.
    init(data: Data) throws {
        var reader = JSONReader(bytes: [UInt8](data))
        self = try reader.document()
    }

    /// Any `Encodable` as a JSON value, through `JSONEncoder` with ISO 8601 dates.
    init(encoding value: some Encodable) throws {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        try self.init(data: encoder.encode(value))
    }

    /// The value as UTF-8 JSON text, keys in their order and each number
    /// with every digit it has (`JSONNumber.description`). `pretty` indents
    /// by two spaces, as Python's output does.
    func data(pretty: Bool = true) -> Data {
        var out = ""
        JSONWriter(pretty: pretty).write(self, into: &out, depth: 0)
        if pretty { out.append("\n") }
        return Data(out.utf8)
    }

    /// Compact JSON text, keys in their order, for display and for query
    /// parameters; a string is its own text, unquoted.
    var jsonText: String {
        if case let .string(s) = self { return s }
        return String(decoding: data(pretty: false), as: UTF8.self)
    }

    /// Parses JSON text, or nil when it is not JSON.
    static func parse(_ text: String?) -> JSONValue? {
        guard let text else { return nil }
        return try? JSONValue(data: Data(text.utf8))
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

    /// For `JSONSerialization`, with each object's order dropped: a whole
    /// number as an `Int`, else as an `NSDecimalNumber` with every digit,
    /// else the nearest `Double`, and a number past Double's range as
    /// `NSNull`. JSON text with every digit and every key in order is
    /// `data(pretty:)`.
    var any: Any {
        switch self {
        case .null: NSNull()
        case let .bool(b): b
        case let .number(n):
            if let i = n.exactInt { i } else if let d = n.decimal { NSDecimalNumber(decimal: d) } else { n.double ?? NSNull() }
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
        if let decimal = n as? NSDecimalNumber, !decimal.decimalValue.isNaN { return .number(decimal.decimalValue) }
        if let int = Int(exactly: n.int64Value), NSNumber(value: n.int64Value) == n { return .number(JSONNumber(int)) }
        return JSONNumber(exactly: n.doubleValue).map(JSONValue.number) ?? .null
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
        guard !isNaN else { return nil }
        return JSONNumber(self).exactInt
    }
}

// MARK: - Codable

/// A Decoder hands a number over only as a Decimal (38 digits, exponents to
/// ±128) or, past Decimal's range, as a Double, and does not say what it
/// dropped, so through one a number keeps what those hold, and one past a
/// Double's range is refused. `JSONValue(data:)` reads every digit. An
/// Encoder takes a number only as a Decimal or a Double, so one neither
/// holds exactly is refused rather than rounded; `data(pretty:)` writes it.
extension JSONValue: Codable {
    public init(from decoder: any Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Decimal.self), !value.isNaN {
            self = .number(value)
        } else if let value = try? container.decode(Double.self), let number = JSONNumber(exactly: value) {
            self = .number(number)
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
        case let .number(value):
            if let decimal = value.decimal {
                try container.encode(decimal)
            } else if let double = value.double, JSONNumber(exactly: double) == value {
                try container.encode(double)
            } else {
                throw EncodingError.invalidValue(value, .init(codingPath: encoder.codingPath, debugDescription:
                    "\(value) is neither a Decimal nor a Double, which is all an Encoder writes; JSONValue.data(pretty:) writes every digit"))
            }
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
    public init(integerLiteral value: Int) { self = .number(JSONNumber(value)) }
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
        case let .number(number): out.append(number.description)
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

/// Reads RFC 8259 JSON, which `JSONDecoder` cannot do losslessly: it hands a
/// number over only as a binary type and an object's keys in no order.
private struct JSONReader {
    let bytes: [UInt8]
    var at = 0

    /// `JSONDecoder`'s own bound, so text that nests deeper is refused
    /// rather than run out of stack.
    static let deepest = 512

    init(bytes: [UInt8]) {
        self.bytes = bytes
        if bytes.starts(with: [0xEF, 0xBB, 0xBF]) { at = 3 }
    }

    /// A container still open: an array's items so far, or an object's
    /// members so far and the key whose value is being read.
    private enum Open {
        case array([JSONValue])
        case object(JSONObject, key: String)

        /// Each mutation empties `self` before it changes the bound copy,
        /// so the copy is the only reference and changes in place: a
        /// container read whole is linear, not quadratic.
        mutating func add(_ value: JSONValue) {
            switch self {
            case var .array(items):
                self = .array([])
                items.append(value)
                self = .array(items)
            case .object(var object, let key):
                self = .array([])
                object[key] = value
                self = .object(object, key: key)
            }
        }

        mutating func read(_ key: String) {
            guard case let .object(object, _) = self else { return }
            self = .array([])
            self = .object(object, key: key)
        }

        var value: JSONValue {
            switch self {
            case let .array(items): .array(items)
            case let .object(object, _): .object(object)
            }
        }
    }

    /// Reads with a stack of its own rather than by recursion, so text
    /// nested to the bound fits the small stack a task runs on.
    mutating func document() throws -> JSONValue {
        var open: [Open] = []
        reading: while true {
            skipSpace()
            guard at < bytes.count else { throw refused("the end of the text where a value belongs") }
            var value: JSONValue
            switch bytes[at] {
            case UInt8(ascii: "{"):
                at += 1
                skipSpace()
                if take(UInt8(ascii: "}")) {
                    value = .object(JSONObject())
                } else {
                    guard open.count < Self.deepest else { throw refused("nesting deeper than \(Self.deepest)") }
                    open.append(.object(JSONObject(), key: try key()))
                    continue reading
                }
            case UInt8(ascii: "["):
                at += 1
                skipSpace()
                if take(UInt8(ascii: "]")) {
                    value = .array([])
                } else {
                    guard open.count < Self.deepest else { throw refused("nesting deeper than \(Self.deepest)") }
                    open.append(.array([]))
                    continue reading
                }
            case UInt8(ascii: "\""): value = try .string(string())
            case UInt8(ascii: "t"): value = try word("true", .bool(true))
            case UInt8(ascii: "f"): value = try word("false", .bool(false))
            case UInt8(ascii: "n"): value = try word("null", .null)
            default: value = try .number(number())
            }
            while let last = open.indices.last {
                skipSpace()
                open[last].add(value)
                if take(UInt8(ascii: ",")) {
                    if case .object = open[last] {
                        skipSpace()
                        open[last].read(try key())
                    }
                    continue reading
                }
                if case .array = open[last] {
                    guard take(UInt8(ascii: "]")) else { throw refused("an array without its closing bracket") }
                } else {
                    guard take(UInt8(ascii: "}")) else { throw refused("an object without its closing brace") }
                }
                value = open.removeLast().value
            }
            skipSpace()
            guard at == bytes.count else { throw refused("text after the value") }
            return value
        }
    }

    /// A member's key and its colon.
    private mutating func key() throws -> String {
        guard at < bytes.count, bytes[at] == UInt8(ascii: "\"") else { throw refused("a key that is not a string") }
        let key = try string()
        skipSpace()
        guard take(UInt8(ascii: ":")) else { throw refused("a key without a colon") }
        return key
    }

    private mutating func string() throws -> String {
        at += 1
        var out: [UInt8] = []
        while at < bytes.count {
            let byte = bytes[at]
            at += 1
            switch byte {
            case UInt8(ascii: "\""):
                guard let text = String(validating: out, as: UTF8.self) else { throw refused("a string that is not UTF-8") }
                return text
            case UInt8(ascii: "\\"):
                try escape(into: &out)
            case 0 ..< 0x20:
                throw refused("a control character inside a string")
            default:
                out.append(byte)
            }
        }
        throw refused("a string without its closing quote")
    }

    private mutating func escape(into out: inout [UInt8]) throws {
        guard at < bytes.count else { throw refused("a string without its closing quote") }
        let byte = bytes[at]
        at += 1
        let simple: [UInt8: UInt8] = [
            UInt8(ascii: "\""): 0x22, UInt8(ascii: "\\"): 0x5C, UInt8(ascii: "/"): 0x2F, UInt8(ascii: "b"): 0x08,
            UInt8(ascii: "f"): 0x0C, UInt8(ascii: "n"): 0x0A, UInt8(ascii: "r"): 0x0D, UInt8(ascii: "t"): 0x09,
        ]
        if let plain = simple[byte] {
            out.append(plain)
            return
        }
        guard byte == UInt8(ascii: "u") else { throw refused("an unknown escape") }
        var unit = try hex()
        if (0xD800 ..< 0xDC00).contains(unit) {
            guard take(UInt8(ascii: "\\")), take(UInt8(ascii: "u")) else { throw refused("half a surrogate pair") }
            let low = try hex()
            guard (0xDC00 ..< 0xE000).contains(low) else { throw refused("half a surrogate pair") }
            unit = 0x10000 + ((unit - 0xD800) << 10) + (low - 0xDC00)
        }
        guard let scalar = Unicode.Scalar(unit) else { throw refused("half a surrogate pair") }
        out.append(contentsOf: Array(String(Character(scalar)).utf8))
    }

    private mutating func hex() throws -> UInt32 {
        guard at + 4 <= bytes.count, let unit = UInt32(String(decoding: bytes[at ..< at + 4], as: UTF8.self), radix: 16) else {
            throw refused("a \\u escape without four hex digits")
        }
        at += 4
        return unit
    }

    private mutating func number() throws -> JSONNumber {
        let start = at
        while at < bytes.count, Self.numberBytes.contains(bytes[at]) { at += 1 }
        guard at > start, let number = JSONNumber(String(decoding: bytes[start ..< at], as: UTF8.self)) else {
            at = start
            throw refused("something that is not a value")
        }
        return number
    }

    private static let numberBytes = Set("0123456789+-.eE".utf8)

    private mutating func word(_ text: String, _ value: JSONValue) throws -> JSONValue {
        let word = Array(text.utf8)
        guard bytes[at...].starts(with: word) else { throw refused("something that is not a value") }
        at += word.count
        return value
    }

    private mutating func take(_ byte: UInt8) -> Bool {
        guard at < bytes.count, bytes[at] == byte else { return false }
        at += 1
        return true
    }

    private mutating func skipSpace() {
        while at < bytes.count {
            switch bytes[at] {
            case 0x20, 0x09, 0x0A, 0x0D: at += 1
            default: return
            }
        }
    }

    private func refused(_ what: String) -> DecodingError {
        .dataCorrupted(.init(codingPath: [], debugDescription: "Not JSON: \(what), at byte \(at)."))
    }
}
