import Foundation

/// The small rules every sentence of the pane follows: sentence case,
/// numbers as words up to fifteen, lists with "and", clock times as 24-hour
/// "HH:mm", and only real times: never a duration assumed for a task.
enum Words {
    static let numbers = [
        "No", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten", "Eleven", "Twelve", "Thirteen",
        "Fourteen", "Fifteen",
    ]

    /// "Three", or "23" past fifteen.
    static func num(_ n: Int) -> String { numbers.indices.contains(n) ? numbers[n] : String(n) }

    /// "Three things", "One thing".
    static func things(_ n: Int, _ one: String, _ many: String? = nil) -> String { "\(num(n)) \(n == 1 ? one : many ?? one + "s")" }

    /// "a, b and c".
    static func list(_ items: [String]) -> String {
        guard items.count > 1 else { return items.first ?? "" }
        return items.dropLast().joined(separator: ", ") + " and " + items[items.count - 1]
    }

    static func cap(_ text: String) -> String { text.isEmpty ? text : text.prefix(1).uppercased() + text.dropFirst() }

    /// The first letter lowered, for a title used inside a sentence, unless
    /// the word is written in capitals (an acronym) or the next letter is.
    static func low(_ text: String) -> String {
        guard let first = text.first, first.isUppercase else { return text }
        let second = text.dropFirst().first
        if let second, second.isUppercase { return text }
        return first.lowercased() + text.dropFirst()
    }

    /// A sentence ends with a full stop unless it already ends one.
    static func dot(_ text: String) -> String {
        guard let last = text.last else { return text }
        return ".?!".contains(last) ? text : text + "."
    }

    /// "14:20".
    static func clock(_ date: Date, _ calendar: Calendar) -> String {
        let parts = calendar.dateComponents([.hour, .minute], from: date)
        return String(format: "%02d:%02d", parts.hour ?? 0, parts.minute ?? 0)
    }

    static let weekdays = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"]
    static let months = [
        "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December",
    ]

    static func weekday(_ date: Date, _ calendar: Calendar) -> String { weekdays[(calendar.component(.weekday, from: date) + 6) % 7] }

    /// "Thursday 19 November".
    static func longDate(_ date: Date, _ calendar: Calendar) -> String {
        "\(weekday(date, calendar)) \(calendar.component(.day, from: date)) \(months[calendar.component(.month, from: date) - 1])"
    }

    /// "Thu 19 Nov".
    static func shortDate(_ date: Date, _ calendar: Calendar) -> String {
        "\(weekday(date, calendar).prefix(3)) \(calendar.component(.day, from: date)) \(months[calendar.component(.month, from: date) - 1].prefix(3))"
    }

    /// "19 November".
    static func dayMonth(_ date: Date, _ calendar: Calendar) -> String {
        "\(calendar.component(.day, from: date)) \(months[calendar.component(.month, from: date) - 1])"
    }

    /// A day named from today: "today", "yesterday", "tomorrow", "Tuesday"
    /// within a week either way, else "5 November".
    static func day(_ date: Date, now: Date, _ calendar: Calendar) -> String {
        let from = calendar.startOfDay(for: now)
        let to = calendar.startOfDay(for: date)
        let days = calendar.dateComponents([.day], from: from, to: to).day ?? 0
        switch days {
        case 0: return "today"
        case -1: return "yesterday"
        case 1: return "tomorrow"
        case -6 ... 6: return weekday(date, calendar)
        default: return dayMonth(date, calendar)
        }
    }

    /// "today at 05:09", "on Wednesday at 21:37", "on 22 October".
    static func when(_ date: Date, now: Date, _ calendar: Calendar) -> String {
        let named = day(date, now: now, calendar)
        let time = clock(date, calendar)
        switch named {
        case "today", "yesterday", "tomorrow": return "\(named) at \(time)"
        default: return weekdays.contains(named) ? "on \(named) at \(time)" : "on \(named)"
        }
    }

    /// The real time until a fixed point: "now", "in 8 min", "in 2 h 40".
    static func until(_ date: Date, now: Date) -> String {
        let minutes = Int((date.timeIntervalSince(now) / 60).rounded(.up))
        if minutes <= 0 { return "now" }
        if minutes < 60 { return "in \(minutes) min" }
        let rest = minutes % 60
        return "in \(minutes / 60) h" + (rest > 0 ? String(format: " %02d", rest) : "")
    }

    static func words(_ name: String) -> [Substring] {
        name.split(whereSeparator: { $0 == " " || $0 == ";" || $0 == "-" || $0 == "_" || $0 == "/" }).filter {
            $0.first?.isLetter == true || $0.first?.isNumber == true
        }
    }

    /// Two initials: the first letters of a name's first two words (a
    /// project), or of its first and last (a person); one word gives its
    /// first two letters. A project whose first word is a short acronym in
    /// capitals ("EU AI Act") is drawn by it.
    static func initials(_ name: String, last: Bool) -> String {
        let words = words(name)
        guard let first = words.first else { return "?" }
        if !last, (2 ... 3).contains(first.count), first.allSatisfy({ $0.isUppercase || $0.isNumber }), first.contains(where: \.isLetter) {
            return String(first.prefix(2))
        }
        if words.count > 1, let other = last ? words.last : words.dropFirst().first, let a = first.first, let b = other.first {
            return (String(a) + String(b)).uppercased()
        }
        return String(first.prefix(2)).uppercased()
    }

    /// The first two letters of a name's first word: what a project takes
    /// when another already has its initials.
    static func firstTwo(_ name: String) -> String {
        guard let first = words(name).first else { return "?" }
        return String(first.prefix(2)).uppercased()
    }

    /// An amount as people write it: "200 mg", "1,000 IU", "5%", "1
    /// capsule", "2 tablets". A unit of measure is cased as it is written;
    /// a unit that is counted takes its plural.
    static func amount(_ value: Decimal, _ unit: String?) -> String {
        let number = grouped(value)
        guard let unit, !unit.isEmpty else { return number }
        switch unit.lowercased() {
        case "iu": return "\(number) IU"
        case "percent", "%": return "\(number)%"
        case "mg", "mcg", "g", "ml", "kg", "l": return "\(number) \(unit.lowercased())"
        case "units", "unit": return "\(number) \(value == 1 ? "unit" : "units")"
        default:
            let one = unit.lowercased()
            if value == 1 { return "\(number) \(one)" }
            return "\(number) \(one.hasSuffix("y") ? one.dropLast() + "ies" : one + "s")"
        }
    }

    /// "1,000", "0.5", "12,500.25": thousands apart, as a label writes them.
    static func grouped(_ value: Decimal) -> String {
        let plain = NSDecimalNumber(decimal: value).stringValue
        let negative = plain.hasPrefix("-")
        let parts = plain.drop(while: { $0 == "-" }).split(separator: ".", maxSplits: 1, omittingEmptySubsequences: false)
        let whole = parts.first.map(String.init) ?? plain
        var out = ""
        for (index, digit) in whole.enumerated() {
            if index > 0, (whole.count - index) % 3 == 0 { out.append(",") }
            out.append(digit)
        }
        if parts.count > 1 { out += "." + parts[1] }
        return (negative ? "-" : "") + out
    }

    /// A hue from a stable hash of an id, so one thing is the same colour
    /// on every device and at every launch.
    static func hue(_ id: String) -> Int {
        var hash: UInt32 = 2_166_136_261
        for byte in id.utf8 {
            hash ^= UInt32(byte)
            hash = hash &* 16_777_619
        }
        return Int(hash % 360)
    }
}
