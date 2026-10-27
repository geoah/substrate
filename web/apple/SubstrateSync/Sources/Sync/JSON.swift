import Foundation

/// A JSON value with its object keys in the order they were written. The
/// Health body is held byte for byte against a golden file the Go suite
/// reads, so the encoding cannot depend on a dictionary's order or on
/// Foundation's number formatting.
indirect enum JSON: Equatable, Sendable {
    case null
    case bool(Bool)
    case int(Int)
    case double(Double)
    case string(String)
    case array([JSON])
    case object([(String, JSON)])

    static func == (a: JSON, b: JSON) -> Bool {
        switch (a, b) {
        case (.null, .null): true
        case let (.bool(x), .bool(y)): x == y
        case let (.int(x), .int(y)): x == y
        case let (.double(x), .double(y)): x == y
        case let (.string(x), .string(y)): x == y
        case let (.array(x), .array(y)): x == y
        case let (.object(x), .object(y)): x.count == y.count && zip(x, y).allSatisfy { $0.0 == $1.0 && $0.1 == $1.1 }
        default: false
        }
    }

    /// Compact JSON: no whitespace, `/` and non-ASCII written as they are,
    /// which is what Health Auto Export sends.
    var encoded: Data { Data(text.utf8) }

    var text: String {
        var out = ""
        write(into: &out)
        return out
    }

    private func write(into out: inout String) {
        switch self {
        case .null: out += "null"
        case let .bool(b): out += b ? "true" : "false"
        case let .int(i): out += String(i)
        case let .double(d): out += JSON.number(d)
        case let .string(s): JSON.quote(s, into: &out)
        case let .array(items):
            out += "["
            for (i, item) in items.enumerated() {
                if i > 0 { out += "," }
                item.write(into: &out)
            }
            out += "]"
        case let .object(pairs):
            out += "{"
            for (i, (key, value)) in pairs.enumerated() {
                if i > 0 { out += "," }
                JSON.quote(key, into: &out)
                out += ":"
                value.write(into: &out)
            }
            out += "}"
        }
    }

    /// A finite double in its shortest round-trip spelling, an integral one
    /// without a fraction ("72", not "72.0"); JSON has no NaN or infinity.
    static func number(_ d: Double) -> String {
        guard d.isFinite else { return "null" }
        if d == d.rounded(), abs(d) < 1e15 { return String(Int64(d)) }
        return "\(d)"
    }

    private static func quote(_ s: String, into out: inout String) {
        out += "\""
        for scalar in s.unicodeScalars {
            switch scalar {
            case "\"": out += "\\\""
            case "\\": out += "\\\\"
            case "\n": out += "\\n"
            case "\r": out += "\\r"
            case "\t": out += "\\t"
            case let c where c.value < 0x20:
                out += String(format: "\\u%04x", c.value)
            default: out.unicodeScalars.append(scalar)
            }
        }
        out += "\""
    }

    /// The value as a Foundation object, for a request body sent through
    /// JSONSerialization.
    var foundation: Any {
        switch self {
        case .null: NSNull()
        case let .bool(b): b
        case let .int(i): i
        case let .double(d): d
        case let .string(s): s
        case let .array(items): items.map(\.foundation)
        case let .object(pairs): Dictionary(pairs.map { ($0.0, $0.1.foundation) }, uniquingKeysWith: { _, b in b })
        }
    }

    var keys: [String] {
        if case let .object(pairs) = self { return pairs.map(\.0) }
        return []
    }
}

/// Rounds to a fixed number of decimals, the way Health Auto Export writes
/// an average ("108.333333").
func rounded(_ value: Double, _ places: Int) -> Double {
    let scale = pow(10, Double(places))
    return (value * scale).rounded() / scale
}

/// An object builder that leaves out what Apple left empty: a nil, an empty
/// string or an empty list is never written, because the kinds read absence
/// as "not set" and a cleared key is a PATCH to null.
struct Fields {
    private(set) var pairs: [(String, JSON)] = []

    mutating func set(_ key: String, _ value: String?) {
        if let value, !value.isEmpty { pairs.append((key, .string(value))) }
    }

    mutating func set(_ key: String, _ value: Int?) {
        if let value { pairs.append((key, .int(value))) }
    }

    mutating func set(_ key: String, _ value: Double?) {
        if let value, value.isFinite { pairs.append((key, .double(value))) }
    }

    mutating func set(_ key: String, _ value: Bool?) {
        if let value { pairs.append((key, .bool(value))) }
    }

    mutating func set(_ key: String, _ value: JSON?) {
        guard let value else { return }
        switch value {
        case let .array(items) where items.isEmpty: return
        case let .object(fields) where fields.isEmpty: return
        default: pairs.append((key, value))
        }
    }

    var json: JSON { .object(pairs) }
}
