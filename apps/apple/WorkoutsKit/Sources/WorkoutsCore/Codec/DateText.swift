import Foundation

/// Substrate's `datetime` spelling, RFC 3339. Parsed and written by hand on
/// the proleptic Gregorian calendar, so no formatter, locale or time zone
/// database is involved and the codec stays `Sendable` and deterministic.
public enum DateText {
    /// `YYYY-MM-DDTHH:MM:SS[.fraction](Z|±HH:MM)`, any offset, the fraction kept.
    public static func parse(_ text: String) -> Date? {
        let chars = Array(text.utf8)
        func number(_ start: Int, _ length: Int) -> Int? {
            guard start + length <= chars.count else { return nil }
            var value = 0
            for byte in chars[start ..< start + length] {
                guard (0x30 ... 0x39).contains(byte) else { return nil }
                value = value * 10 + Int(byte - 0x30)
            }
            return value
        }
        func byte(_ index: Int, is expected: Character...) -> Bool {
            index < chars.count && expected.contains(Character(UnicodeScalar(chars[index])))
        }

        guard let year = number(0, 4), byte(4, is: "-"), let month = number(5, 2), byte(7, is: "-"),
              let day = number(8, 2), byte(10, is: "T", "t"), let hour = number(11, 2), byte(13, is: ":"),
              let minute = number(14, 2), byte(16, is: ":"), let second = number(17, 2)
        else { return nil }
        guard (1 ... 12).contains(month), (1 ... daysIn(month: month, year: year)).contains(day),
              hour < 24, minute < 60, second < 61 else { return nil }

        var index = 19
        var fraction = 0.0
        if byte(index, is: ".") {
            index += 1
            let start = index
            while index < chars.count, (0x30 ... 0x39).contains(chars[index]) {
                index += 1
            }
            guard index > start, let value = Double("0." + String(decoding: chars[start ..< index], as: UTF8.self)) else { return nil }
            fraction = value
        }

        var offset = 0
        if byte(index, is: "Z", "z"), index + 1 == chars.count {
            offset = 0
        } else if byte(index, is: "+", "-"), index + 6 == chars.count, byte(index + 3, is: ":"),
                  let hours = number(index + 1, 2), let minutes = number(index + 4, 2), hours < 24, minutes < 60
        {
            offset = (hours * 3600 + minutes * 60) * (byte(index, is: "-") ? -1 : 1)
        } else {
            return nil
        }

        let days = daysFromCivil(year: year, month: month, day: day)
        let seconds = days * 86400 + hour * 3600 + minute * 60 + second - offset
        return Date(timeIntervalSince1970: Double(seconds) + fraction)
    }

    /// UTC with `Z` and whole seconds, the fraction floored: `2026-10-27T13:30:00Z`.
    public static func format(_ date: Date) -> String {
        let seconds = Int(date.timeIntervalSince1970.rounded(.down))
        let days = seconds >= 0 ? seconds / 86400 : (seconds - 86399) / 86400
        let rest = seconds - days * 86400
        let (year, month, day) = civilFromDays(days)
        return pad(year, 4) + "-" + pad(month, 2) + "-" + pad(day, 2) + "T"
            + pad(rest / 3600, 2) + ":" + pad(rest % 3600 / 60, 2) + ":" + pad(rest % 60, 2) + "Z"
    }

    private static func pad(_ value: Int, _ width: Int) -> String {
        let digits = String(value)
        return String(repeating: "0", count: max(0, width - digits.count)) + digits
    }

    private static func daysIn(month: Int, year: Int) -> Int {
        switch month {
        case 2: (year % 4 == 0 && year % 100 != 0) || year % 400 == 0 ? 29 : 28
        case 4, 6, 9, 11: 30
        default: 31
        }
    }

    // Howard Hinnant's days_from_civil and civil_from_days.
    private static func daysFromCivil(year: Int, month: Int, day: Int) -> Int {
        let y = month <= 2 ? year - 1 : year
        let era = (y >= 0 ? y : y - 399) / 400
        let yearOfEra = y - era * 400
        let dayOfYear = (153 * (month > 2 ? month - 3 : month + 9) + 2) / 5 + day - 1
        let dayOfEra = yearOfEra * 365 + yearOfEra / 4 - yearOfEra / 100 + dayOfYear
        return era * 146_097 + dayOfEra - 719_468
    }

    private static func civilFromDays(_ days: Int) -> (Int, Int, Int) {
        let z = days + 719_468
        let era = (z >= 0 ? z : z - 146_096) / 146_097
        let dayOfEra = z - era * 146_097
        let yearOfEra = (dayOfEra - dayOfEra / 1460 + dayOfEra / 36524 - dayOfEra / 146_096) / 365
        let dayOfYear = dayOfEra - (365 * yearOfEra + yearOfEra / 4 - yearOfEra / 100)
        let mp = (5 * dayOfYear + 2) / 153
        let day = dayOfYear - (153 * mp + 2) / 5 + 1
        let month = mp < 10 ? mp + 3 : mp - 9
        return (yearOfEra + era * 400 + (month <= 2 ? 1 : 0), month, day)
    }
}
