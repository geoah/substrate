import Foundation
import SubstrateCore

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

    /// How many letters of a description a card shows: about three lines
    /// of a phone's width.
    static let excerptLimit = 120
    /// The same for a row's own line, which is narrower: its flag button
    /// and its icon take room a card's text has. Longer, and the view's
    /// three-line cap cuts it a second time, wherever the line happens to
    /// break.
    static let lineExcerptLimit = 80

    /// A text a little longer than its excerpt is shown whole: a button
    /// that showed a dozen more words would be a tap for nothing. Only one
    /// longer than this is cut to `excerptLimit` with the rest behind
    /// "Show all".
    static let excerptWholeLimit = 200
    /// The same pair for the current item's description, which has a
    /// card's width and is what the owner is doing now.
    static let leadWholeLimit = 240
    static let leadExcerptLimit = 160

    /// A description as it is drawn: its Markdown, less what is not for
    /// reading. A record's path in brackets ("(source: <path>)") is where
    /// the words came from, which the task's origin holds already; and the
    /// mark at the head of a quoted line is not a word.
    static func prose(_ text: String) -> String {
        var out = ""
        var rest = Substring(text)
        // The innermost brackets first: the nearest close, and the last open before it.
        while let close = rest.firstIndex(of: ")") {
            let after = rest.index(after: close)
            if let open = rest[..<close].lastIndex(of: "("), namesOnlyRecords(rest[rest.index(after: open) ..< close]) {
                // The space before it goes with it.
                var head = rest[..<open]
                while head.last == " " { head = head.dropLast() }
                out += head
            } else {
                out += rest[..<after]
            }
            rest = rest[after...]
        }
        out += rest
        let lines = out.split(separator: "\n", omittingEmptySubsequences: false).map { line -> Substring in
            var rest = line.drop(while: { $0 == " " })
            guard rest.first == ">" else { return line }
            while rest.first == ">" { rest = rest.dropFirst().drop(while: { $0 == " " }) }
            return rest
        }
        return lines.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Whether what stands between two brackets only names records:
    /// "source: <path>", "<path>", "sources: <path>, <path> and <path>".
    static func namesOnlyRecords(_ inside: Substring) -> Bool {
        var words = inside.split(whereSeparator: { $0 == " " || $0 == "," || $0 == ";" || $0 == "\n" })
        // A label of one or two words that ends with a colon.
        if let colon = words.firstIndex(where: { $0.hasSuffix(":") }), colon <= 1 { words.removeFirst(colon + 1) }
        words.removeAll { $0 == "and" }
        return !words.isEmpty && words.allSatisfy { RecordPaths.isWhole(String($0)) }
    }

    /// A description as plain words, for a line that draws no Markdown: its
    /// marks of emphasis and of code are taken out, and a link is its label.
    static func plain(_ text: String) -> String {
        var out = ""
        var rest = Substring(prose(text))
        while let open = rest.firstIndex(of: "[") {
            // "[label](target)": the label stays.
            guard let label = rest[open...].firstIndex(of: "]"), rest[label...].dropFirst().first == "(",
                let end = rest[label...].firstIndex(of: ")")
            else {
                out += rest[...open]
                rest = rest[rest.index(after: open)...]
                continue
            }
            out += rest[..<open]
            out += rest[rest.index(after: open) ..< label]
            rest = rest[rest.index(after: end)...]
        }
        out += rest
        for mark in ["**", "__", "`"] { out = out.replacingOccurrences(of: mark, with: "") }
        return out
    }

    /// What a cut left open is closed: a link with no end loses its
    /// bracket and what there is of its target, and a mark of emphasis or
    /// of code with no partner is taken out, so none is drawn as written.
    static func closed(_ cut: String) -> String {
        var out = cut
        if let open = out.lastIndex(of: "[") {
            let tail = out[open...]
            let whole = tail.firstIndex(of: "]").map { label in tail[label...].dropFirst().first == "(" && tail[label...].contains(")") } ?? false
            if !whole {
                // The label's words stay; a target that was cut does not.
                var label = String(tail.dropFirst())
                if let target = label.range(of: "](") { label = String(label[..<target.lowerBound]) }
                out = String(out[..<open]) + label
            }
        }
        for mark in ["**", "`"] where out.components(separatedBy: mark).count % 2 == 0 {
            if let last = out.range(of: mark, options: .backwards) { out.removeSubrange(last) }
        }
        // One star alone, once the pairs of two are counted out.
        let single = out.replacingOccurrences(of: "**", with: "").filter { $0 == "*" }.count
        if single % 2 == 1, let last = out.lastIndex(of: "*") {
            let before = last > out.startIndex ? out[out.index(before: last)] : " "
            let after = out.index(after: last) < out.endIndex ? out[out.index(after: last)] : " "
            if before != "*", after != "*" { out.remove(at: last) }
        }
        return out
    }

    /// How long a text is as one paragraph, which is what `excerpt` cuts.
    static func letters(_ text: String) -> Int { prose(text).split(whereSeparator: \.isWhitespace).joined(separator: " ").count }

    /// The opening words of a long text, as one paragraph: cut at a word
    /// and ended with an ellipsis. A card or a row shows this much of a
    /// description; the whole of it is on the thing's own page. It is
    /// still Markdown: a cut never leaves a mark or a link open.
    static func excerpt(_ text: String, limit: Int = excerptLimit) -> String {
        let words = prose(text).split(whereSeparator: \.isWhitespace)
        let flat = words.joined(separator: " ")
        guard flat.count > limit else { return flat }
        var out = ""
        for word in words {
            guard out.count + word.count + (out.isEmpty ? 0 : 1) <= limit else { break }
            out += (out.isEmpty ? "" : " ") + word
        }
        // One unbroken run longer than the limit has no word to cut at.
        if out.isEmpty { out = String(flat.prefix(limit)) }
        out = closed(out)
        while let last = out.last, " .,;:([{-\u{2013}\u{2014}".contains(last) { out.removeLast() }
        return out + "\u{2026}"
    }

    /// A person's name from what a contact holds. `name` is taken where
    /// it has a capital; a `name` all in lower case is a handle. Then the
    /// given and family names, each read as a handle where it has no
    /// capital, then a `title` that has a capital, then the nickname or the
    /// handle as a name.
    static func personName(name: String?, given: String?, family: String?, title: String?, nickname: String?) -> String? {
        func kept(_ text: String?) -> String? {
            let trimmed = text?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            return trimmed.isEmpty ? nil : trimmed
        }
        if let name = kept(name), name.contains(where: \.isUppercase) { return name }
        let parts = [kept(given), kept(family)].compactMap { $0 }.map(written)
        if !parts.isEmpty { return parts.joined(separator: " ") }
        if let title = kept(title), title.contains(where: \.isUppercase) { return title }
        return (kept(nickname) ?? kept(name) ?? kept(title)).map(handleName)
    }

    /// A part of a name as it is written, or as a name where a source
    /// filled it with a handle or an address: one with no capital in it
    /// ("christopher", "dan@example.com").
    static func written(_ part: String) -> String { part.contains(where: \.isUppercase) ? part : handleName(part) }

    /// A handle as a name: the part before an address's "@", each of its
    /// words with a capital. "Christopher" for "christopher", "Dan" for
    /// "dan@example.com", "Ana Lima" for "ana.lima@example.com".
    static func handleName(_ handle: String) -> String {
        let local = handle.split(separator: "@").first.map(String.init) ?? handle
        let words = local.split(whereSeparator: { ".-_+".contains($0) }).map { cap(String($0)) }
        return words.isEmpty ? cap(local) : words.joined(separator: " ")
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
