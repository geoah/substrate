import Foundation

/// A JSON number as its decimal digits and a power of ten, so a number read
/// is written again with every digit it had, whatever its size: a `Decimal`
/// holds 38 digits and exponents to ±128, a `Double` 17 digits, and JSON
/// bounds neither. Equal numbers are equal however they were spelled
/// (`1.50`, `15e-1`). There is no NaN and no infinity, as there is none in
/// JSON.
public struct JSONNumber: Hashable, Sendable, CustomStringConvertible {
    /// Without leading or trailing zeros; empty for zero, so each value has
    /// one spelling and equality is the fields'.
    private let digits: String
    private let exponent: Int
    private let negative: Bool

    private init(negative: Bool, digits: Substring, exponent: Int) {
        let leading = digits.drop { $0 == "0" }
        let trailing = leading.reversed().prefix { $0 == "0" }.count
        let kept = leading.dropLast(trailing)
        self.digits = String(kept)
        self.exponent = kept.isEmpty ? 0 : exponent + trailing
        self.negative = negative && !kept.isEmpty
    }

    /// JSON's number grammar and nothing else: no `+`, no leading zero, no
    /// bare point, no NaN. Nil too for an exponent past ±1,000,000,000,
    /// which no number a server writes comes near.
    public init?(_ text: some StringProtocol) {
        var bytes = Substring(text).utf8[...]
        let negative = bytes.first == UInt8(ascii: "-")
        if negative { bytes = bytes.dropFirst() }
        let whole = bytes.prefix { $0 >= 0x30 && $0 <= 0x39 }
        guard !whole.isEmpty, whole.count == 1 || whole.first != UInt8(ascii: "0") else { return nil }
        bytes = bytes.dropFirst(whole.count)
        var fraction = bytes.prefix(0)
        if bytes.first == UInt8(ascii: ".") {
            fraction = bytes.dropFirst().prefix { $0 >= 0x30 && $0 <= 0x39 }
            guard !fraction.isEmpty else { return nil }
            bytes = bytes.dropFirst(1 + fraction.count)
        }
        var power = 0
        if bytes.first == UInt8(ascii: "e") || bytes.first == UInt8(ascii: "E") {
            bytes = bytes.dropFirst()
            let sign = bytes.first == UInt8(ascii: "-") ? -1 : 1
            if bytes.first == UInt8(ascii: "-") || bytes.first == UInt8(ascii: "+") { bytes = bytes.dropFirst() }
            let digits = bytes.prefix { $0 >= 0x30 && $0 <= 0x39 }
            guard !digits.isEmpty else { return nil }
            for digit in digits {
                power = power * 10 + Int(digit - 0x30)
                guard power <= Self.largestExponent else { return nil }
            }
            power *= sign
            bytes = bytes.dropFirst(digits.count)
        }
        guard bytes.isEmpty else { return nil }
        let all = String(decoding: whole, as: UTF8.self) + String(decoding: fraction, as: UTF8.self)
        self.init(negative: negative, digits: Substring(all), exponent: power - fraction.count)
    }

    public init(_ value: Int) {
        let text = String(value)
        self.init(negative: value < 0, digits: text.drop { $0 == "-" }, exponent: 0)
    }

    /// Every digit `value` holds. A NaN is no number, and making one of it
    /// is a programmer's error, as dividing by zero is.
    public init(_ value: Decimal) {
        precondition(!value.isNaN, "a JSON number is never NaN")
        // Decimal's own text is plain digits, never an exponent.
        self.init(value.description)!
    }

    /// The shortest digits that read back as `value` (0.1, never
    /// 0.1000000000000000055); nil for an infinity or a NaN.
    public init?(exactly value: Double) {
        guard value.isFinite else { return nil }
        self.init("\(value)".replacingOccurrences(of: "+", with: ""))
    }

    private static let largestExponent = 1_000_000_000

    // MARK: Reading the number

    /// JSON text: plain digits within 128 places of the point (`0.0000001`,
    /// `12345678901234567890`), an exponent past them (`1e200`), so the
    /// text of a number that fits a `Decimal` is the one it always had.
    public var description: String {
        guard !digits.isEmpty else { return "0" }
        let sign = negative ? "-" : ""
        let count = digits.count
        if exponent >= 0, count + exponent <= Self.plainPlaces {
            return sign + digits + String(repeating: "0", count: exponent)
        }
        if exponent < 0, -exponent <= Self.plainPlaces {
            let point = count + exponent
            if point > 0 { return sign + digits.prefix(point) + "." + digits.dropFirst(point) }
            return sign + "0." + String(repeating: "0", count: -point) + digits
        }
        let rest = digits.dropFirst()
        return sign + digits.prefix(1) + (rest.isEmpty ? "" : "." + rest) + "e" + String(exponent + count - 1)
    }

    private static let plainPlaces = 128

    /// The number when a `Decimal` holds it exactly; nil past Decimal's
    /// 38 digits or its exponents.
    public var decimal: Decimal? {
        guard digits.count <= 38, let value = Decimal(string: description, locale: Locale(identifier: "en_US_POSIX")),
            !value.isNaN, JSONNumber(value) == self
        else { return nil }
        return value
    }

    /// The nearest `Double`, for arithmetic and display; nil past Double's
    /// range, where the nearest would be an infinity.
    public var double: Double? {
        guard let value = Double(description), value.isFinite else { return nil }
        return value
    }

    /// The number with its fraction dropped, toward zero; nil where that
    /// is outside `Int`'s range. Read from the digits, so every `Int` is
    /// exact, Int.min and Int.max included.
    public var int: Int? {
        guard !digits.isEmpty else { return 0 }
        let whole = digits.count + exponent
        guard whole > 0 else { return 0 }
        guard whole <= 19 else { return nil }
        let text = exponent >= 0 ? digits + String(repeating: "0", count: exponent) : String(digits.prefix(whole))
        return Int((negative ? "-" : "") + text)
    }

    /// The integer this spells, or nil when it has a fraction or does not
    /// fit an `Int`.
    public var exactInt: Int? {
        exponent >= 0 ? int : nil
    }
}
