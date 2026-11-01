import Foundation

/// Substrate's `duration` spelling, ISO 8601.
public enum ISODuration {
    /// Seconds from `P[nW][nD][T[nH][nM][n[.f]S]]`, a day being 24 hours and a
    /// fraction of a second rounded to the nearest second (half up). Nil for
    /// anything else: years and months, a sign, an empty `P` or `T`.
    public static func parse(_ text: String) -> Seconds? {
        var scanner = Substring(text)
        guard scanner.popFirst() == "P", !scanner.isEmpty else { return nil }
        var total = 0
        var components = 0

        func add(_ number: Int, times unit: Int) -> Bool {
            let (product, overflowA) = number.multipliedReportingOverflow(by: unit)
            let (sum, overflowB) = total.addingReportingOverflow(product)
            total = sum
            components += 1
            return !overflowA && !overflowB
        }

        // The date part: weeks, then days.
        for (designator, unit) in [("W", 604_800), ("D", 86400)] as [(Character, Int)] {
            if let (number, rest) = integer(followedBy: designator, in: scanner) {
                guard add(number, times: unit) else { return nil }
                scanner = rest
            }
        }
        if scanner.first == "T" {
            scanner.removeFirst()
            var timeComponents = 0
            for (designator, unit) in [("H", 3600), ("M", 60)] as [(Character, Int)] {
                if let (number, rest) = integer(followedBy: designator, in: scanner) {
                    guard add(number, times: unit) else { return nil }
                    scanner = rest
                    timeComponents += 1
                }
            }
            if let end = scanner.firstIndex(of: "S") {
                guard let seconds = DecimalText.parse(String(scanner[..<end])), seconds >= 0 else { return nil }
                var rounded = Decimal()
                var value = seconds
                NSDecimalRound(&rounded, &value, 0, .plain)
                guard let whole = rounded.exactInt, add(whole, times: 1) else { return nil }
                scanner = scanner[scanner.index(after: end)...]
                timeComponents += 1
            }
            guard timeComponents > 0 else { return nil }
        }
        guard scanner.isEmpty, components > 0 else { return nil }
        return total
    }

    /// Substrate's one stored spelling: days, then a time part, zero parts
    /// left out, `PT0S` for zero (`PT1M30S`, `P1DT12H`). Durations are never
    /// negative; a negative one is a bug upstream and is written as zero rather
    /// than as a value substrate refuses.
    public static func format(_ seconds: Seconds) -> String {
        let total = max(0, seconds)
        guard total > 0 else { return "PT0S" }
        let days = total / 86400
        let hours = total % 86400 / 3600
        let minutes = total % 3600 / 60
        let rest = total % 60
        var text = "P"
        if days > 0 { text += "\(days)D" }
        if hours > 0 || minutes > 0 || rest > 0 {
            text += "T"
            if hours > 0 { text += "\(hours)H" }
            if minutes > 0 { text += "\(minutes)M" }
            if rest > 0 { text += "\(rest)S" }
        }
        return text
    }

    /// Leading ASCII digits immediately followed by `designator`.
    private static func integer(followedBy designator: Character, in text: Substring) -> (Int, Substring)? {
        let digits = text.prefix { $0.isASCII && $0.isNumber }
        guard !digits.isEmpty, text.dropFirst(digits.count).first == designator,
              let number = Int(digits) else { return nil }
        return (number, text.dropFirst(digits.count + 1))
    }
}
