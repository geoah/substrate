import Foundation

/// Substrate's `decimal` spelling: a JSON string of plain digits, such as
/// "9.5". Read with the POSIX locale; written as the shortest plain digits.
public enum DecimalText {
    private static let posix = Locale(identifier: "en_US_POSIX")

    /// The value of `text` when it is an optional `-`, digits, and an
    /// optional `.` and digits; nil for anything else (exponents, spaces,
    /// a comma, a trailing word), which `Decimal(string:)` would half-accept.
    public static func parse(_ text: String) -> Decimal? {
        var digitsBefore = 0
        var digitsAfter = 0
        var sawPoint = false
        for (index, character) in text.enumerated() {
            switch character {
            case "-" where index == 0:
                continue
            case "." where !sawPoint && digitsBefore > 0:
                sawPoint = true
            case "0" ... "9":
                if sawPoint { digitsAfter += 1 } else { digitsBefore += 1 }
            default:
                return nil
            }
        }
        guard digitsBefore > 0, !sawPoint || digitsAfter > 0 else { return nil }
        return Decimal(string: text, locale: posix)
    }

    /// No exponent, no trailing zeros, "0" for zero: 9.50 is "9.5".
    public static func format(_ value: Decimal) -> String {
        var text = value.description
        if text.contains(".") {
            while text.hasSuffix("0") { text.removeLast() }
            if text.hasSuffix(".") { text.removeLast() }
        }
        return text == "-0" ? "0" : text
    }
}
