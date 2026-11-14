import Foundation
import SubstrateRuntime

/// A reply still being written, closed for display. What its text so far
/// would draw as raw markers is closed or held back until it is whole, so
/// the page only ever grows: nothing is drawn that the next piece takes
/// away or turns from a marker into styling.
///
/// - An emphasis, a strikethrough or a code span left open is closed where
///   the text ends. One with nothing in it yet, and a marker at the very end
///   that may still grow or may be half of the one that closes (`*` before
///   `**`, one backtick of two), is left out until more arrives.
/// - A link reads as its label until its address is whole:
///   `[the notes](htt` reads "the notes".
/// - A table is a header row as soon as its first line has a cell: the line
///   under it that says where the columns are is written for it, in place of
///   whatever part of that line has arrived.
/// - A task item's box is drawn once its brackets are there, and a line that
///   is only the start of a block (a bullet with nothing after it, one or
///   two dashes of a rule, part of a fence's closing line) waits.
/// - Inside code nothing is touched: an unclosed fence is code already.
///
/// The text of a finished reply is never passed here: a marker its writer
/// left open is drawn as the stored reply draws it.
public extension Markdown {
    static func closed(_ text: String) -> String {
        Meter.shared.time(.close) { closing(text) }
    }

    private static func closing(_ text: String) -> String {
        guard text.count <= limit else { return text }
        var lines = text.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n")

        // Inside a fence the text is code to its end. The fence's own
        // closing line arrives a backtick at a time: until it is whole it
        // would draw as a line of the code.
        var fenced: String?
        for line in lines.dropLast() {
            let stripped = unquoted(line)
            if let marker = fenced {
                if closes(stripped, marker) { fenced = nil }
            } else if let opened = fence(stripped) {
                fenced = opened.marker
            }
        }
        let last = lines[lines.count - 1]
        if let marker = fenced {
            let stripped = unquoted(last)
            if closes(stripped, marker) { return text }
            if !stripped.isEmpty, marker.hasPrefix(stripped) { lines.removeLast() }
            return lines.joined(separator: "\n")
        }
        if fence(unquoted(last)) != nil { return lines.joined(separator: "\n") }
        if isIndentedCode(lines) { return lines.joined(separator: "\n") }

        // The open text: the last line, with the lines above it that it
        // continues (a paragraph's, a list item's, a quote's).
        var first = lines.count - 1
        while first > 0 {
            let current = lines[first]
            let above = lines[first - 1]
            if above.trimmingCharacters(in: .whitespaces).isEmpty { break }
            if startsBlock(current), !(isQuote(current) && isQuote(above)) { break }
            if isWhole(above) { break }
            first -= 1
        }
        var tail = closedInline(lines[first...].joined(separator: "\n"), table: isRow(last))
        if isPending(tail) { tail = "" }
        lines.replaceSubrange(first..., with: tail.components(separatedBy: "\n"))

        // A table's first line, and the line under it.
        let ended = lines.count > 1 && lines[lines.count - 1].isEmpty
        let body = ended ? Array(lines.dropLast()) : lines
        let run = body.reversed().prefix(while: isRow).count
        guard run == 1 || run == 2 else { return lines.joined(separator: "\n") }
        let header = body.count - run
        let columns = cells(body[header]).count
        guard header == 0 || body[header - 1].trimmingCharacters(in: .whitespaces).isEmpty,
            cells(body[header]).contains(where: { !$0.isEmpty })
        else { return lines.joined(separator: "\n") }
        let rule = "|" + String(repeating: " --- |", count: columns)
        if run == 1 {
            lines.insert(rule, at: header + 1)
        } else if body[header + 1].allSatisfy({ "|:- ".contains($0) }), (delimiterRow(body[header + 1])?.count ?? 0) < columns {
            // Part of it has arrived: not yet a line the parser takes for one,
            // or one for fewer columns than the header names.
            lines[header + 1] = rule
        }
        return lines.joined(separator: "\n")
    }

    // MARK: Lines

    /// A line without its indentation and the marks of the quotes it is in.
    private static func unquoted(_ line: String) -> String {
        var rest = Substring(line.trimmingCharacters(in: .whitespaces))
        while rest.first == ">" {
            rest = rest.dropFirst()
            rest = rest.drop { $0 == " " }
        }
        return String(rest)
    }

    private static func closes(_ stripped: String, _ marker: String) -> Bool {
        stripped.hasPrefix(marker) && stripped.allSatisfy { $0 == marker.first }
    }

    private static func isQuote(_ line: String) -> Bool { line.trimmingCharacters(in: .whitespaces).hasPrefix(">") }

    private static func isRow(_ line: String) -> Bool { line.trimmingCharacters(in: .whitespaces).hasPrefix("|") }

    private static func isItem(_ line: String) -> Bool { listMarker(String(line.drop { $0 == " " })) != nil }

    /// A line that begins a block of its own: no text above it runs on into
    /// it.
    private static func startsBlock(_ line: String) -> Bool {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        return isRow(line) || trimmed.hasPrefix(">") || heading(trimmed) != nil || isItem(line) || fence(trimmed) != nil
    }

    /// A line that is a block by itself: no text below it continues it.
    private static func isWhole(_ line: String) -> Bool {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        return isRow(line) || heading(trimmed) != nil || isRule(trimmed) || fence(unquoted(line)) != nil
    }

    /// The last line is indented code: four spaces in, after a blank line,
    /// and not under a list item, whose continuation is indented too.
    private static func isIndentedCode(_ lines: [String]) -> Bool {
        guard let last = lines.last, last.prefix(while: { $0 == " " }).count >= 4, !isItem(last) else { return false }
        var above = lines.count - 2
        guard above < 0 || lines[above].trimmingCharacters(in: .whitespaces).isEmpty || lines[above].hasPrefix("    ") else { return false }
        while above >= 0, lines[above].trimmingCharacters(in: .whitespaces).isEmpty || lines[above].hasPrefix("    ") { above -= 1 }
        return above < 0 || !(isItem(lines[above]) || lines[above].hasPrefix(" "))
    }

    /// The open text is only the start of a block, which nothing says the
    /// kind of yet: a bullet's mark with nothing after it (a rule begins
    /// the same way, and so does a line of bold), one or two dashes, or a
    /// table's first pipe.
    private static func isPending(_ tail: String) -> Bool {
        guard !tail.contains("\n") else { return false }
        let trimmed = unquoted(tail)
        return ["-", "--", "*", "+", "|", "_", "__"].contains(trimmed)
    }

    // MARK: Inline

    private struct Open {
        var marker: Character
        var count: Int
        var at: Int
    }

    /// The open text with what is open in it closed or left out.
    private static func closedInline(_ text: String, table: Bool) -> String {
        let chars = Array(text)
        var open: [Open] = []
        var code: Open?
        var labels: [Int] = []
        /// A link whose address is being written: its `[` and its `]`.
        var address: (label: Int, close: Int)?
        /// A label the last character closed, which an address may follow.
        var closedLabel: (label: Int, close: Int)?
        var angle: Int?
        var drop = Set<Int>()
        var cut = chars.count
        var appended = ""

        func run(_ at: Int) -> Int {
            var n = 0
            while at + n < chars.count, chars[at + n] == chars[at] { n += 1 }
            return n
        }
        func word(_ c: Character?) -> Bool { c.map { $0.isLetter || $0.isNumber } ?? false }
        // A bracket that follows a letter or a digit is an index (`a[i]`),
        // not a link's label.
        func opensLabel(_ at: Int) -> Bool { at == 0 || !word(chars[at - 1]) }

        var i = lineStart(chars, 0, &cut, &appended)
        while i < cut {
            let c = chars[i]
            if let span = code {
                if c == "`" {
                    let n = run(i)
                    if n == span.count { code = nil }
                    i += n
                } else {
                    i += 1
                }
                continue
            }
            if c == "\\" {
                // An escape whose character has not arrived.
                if i + 1 == chars.count { drop.insert(i) }
                i += 2
                continue
            }
            if address != nil {
                if c == ")" || c == "\n" {
                    address = nil
                    if !labels.isEmpty { labels.removeLast() }
                }
                if c == "\n" { i = lineStart(chars, i + 1, &cut, &appended) } else { i += 1 }
                continue
            }
            closedLabel = nil
            switch c {
            case "`":
                let n = run(i)
                code = Open(marker: "`", count: n, at: i)
                i += n
            case "*", "_", "~":
                let n = run(i)
                let before: Character? = i > 0 && chars[i - 1] != "\n" ? chars[i - 1] : nil
                let after: Character? = i + n < chars.count ? chars[i + n] : nil
                let last = i + n == chars.count
                // One tilde is itself (`~/tasks`); two strike through.
                var left = c == "~" ? n - n % 2 : n
                if c == "~", n % 2 == 1, last { drop.insert(i + n - 1) }
                let canClose = before.map { !$0.isWhitespace } ?? false
                let canOpen = after.map { !$0.isWhitespace && $0 != "\n" } ?? false
                // An underscore inside a word is a letter of it.
                if c == "_", word(before), word(after) { left = 0 }
                while left > 0, canClose, let match = open.lastIndex(where: { $0.marker == c && $0.count <= left }) {
                    left -= open[match].count
                    open.removeSubrange(match...)
                }
                if left > 0 {
                    let at = i + n - left - (c == "~" ? n % 2 : 0)
                    if last {
                        // The start of a marker, or part of the one that
                        // closes: left out until it is whole.
                        drop.formUnion(at ..< at + left)
                    } else if canOpen, !(word(before) && word(after)) {
                        // Between two letters or digits it is a sign (`5*3`).
                        if c == "~" {
                            open.append(Open(marker: c, count: 2, at: at))
                        } else {
                            if left >= 2 { open.append(Open(marker: c, count: 2, at: at)) }
                            if left % 2 == 1 { open.append(Open(marker: c, count: 1, at: at + left - 1)) }
                        }
                    }
                }
                i += n
            case "[":
                labels.append(i)
                i += 1
            case "]":
                if let label = labels.last {
                    if i + 1 < chars.count, chars[i + 1] == "(" {
                        address = (label, i)
                        i += 2
                        continue
                    }
                    labels.removeLast()
                    if i + 1 == chars.count { closedLabel = (label, i) }
                }
                i += 1
            case "<":
                angle = i
                i += 1
            case ">", " ":
                angle = nil
                i += 1
            case "|" where table:
                // Each cell is its own: nothing stays open past its end.
                open = []
                labels = []
                angle = nil
                i += 1
            case "\n":
                angle = nil
                i = lineStart(chars, i + 1, &cut, &appended)
            default:
                i += 1
            }
        }

        if let link = address {
            // The address is not whole: the label, without its brackets.
            cut = min(cut, link.close)
            drop.insert(link.label)
            if link.label > 0, chars[link.label - 1] == "!" { drop.insert(link.label - 1) }
        } else if let link = closedLabel, opensLabel(link.label), chars[(link.label + 1) ..< link.close].contains(where: \.isLetter) {
            // An address may follow: the label, until the next piece says.
            drop.insert(link.label)
            drop.insert(link.close)
        } else if let label = labels.last, opensLabel(label), label + 1 < cut, chars[(label + 1) ..< cut].contains(where: \.isLetter) {
            drop.insert(label)
        } else if let label = labels.last, opensLabel(label), label + 1 == chars.count {
            drop.insert(label)
        }
        // An address in angle brackets, until its closing one.
        if let angle, angle + 1 < cut {
            let rest = String(chars[(angle + 1) ..< cut])
            if "https://".hasPrefix(rest) || "mailto:".hasPrefix(rest) || rest.hasPrefix("http://") || rest.hasPrefix("https://")
                || rest.hasPrefix("mailto:")
            {
                drop.insert(angle)
            }
        } else if let angle, angle + 1 == chars.count {
            drop.insert(angle)
        }

        var closers = ""
        if let span = code {
            // Backticks at the end, fewer than opened it: part of its close.
            var end = cut
            while end > span.at + span.count, chars[end - 1] == "`" { end -= 1 }
            if chars[(span.at + span.count) ..< end].allSatisfy(\.isWhitespace) {
                cut = min(cut, span.at)
            } else {
                cut = min(cut, end)
                closers += String(repeating: "`", count: span.count)
            }
        }
        for marker in open.reversed() where marker.at < cut {
            let content = (min(marker.at + marker.count, cut) ..< cut).filter { !drop.contains($0) }.map { chars[$0] }
            if content.allSatisfy(\.isWhitespace) {
                drop.formUnion(marker.at ..< marker.at + marker.count)
            } else {
                closers += String(repeating: marker.marker, count: marker.count)
            }
        }
        var kept = (0 ..< cut).filter { !drop.contains($0) }.map { chars[$0] }
        // A closing marker touches the text it closes.
        var end = kept.count
        if !closers.isEmpty {
            while end > 0, kept[end - 1].isWhitespace { end -= 1 }
        }
        kept.insert(contentsOf: closers, at: end)
        return String(kept) + appended
    }

    /// Where a line's own text starts: past its indentation, the marks of
    /// its quotes, a list item's marker and its box, a heading's hashes. A
    /// box whose brackets are all there is drawn at once, so the space the
    /// parser reads after it is written for it; one still arriving waits.
    private static func lineStart(_ chars: [Character], _ from: Int, _ cut: inout Int, _ appended: inout String) -> Int {
        var i = from
        func spaces() { while i < chars.count, chars[i] == " " { i += 1 } }
        spaces()
        while i < chars.count, chars[i] == ">" {
            i += 1
            spaces()
        }
        var end = i
        while end < chars.count, chars[end] != "\n" { end += 1 }
        if let marker = listMarker(String(chars[i ..< end])) {
            i = min(i + marker.content, end)
            spaces()
            let rest = String(chars[i ..< end])
            if end == chars.count {
                if ["[ ]", "[x]", "[X]"].contains(rest) {
                    appended = " "
                    return end
                }
                if ["[", "[ ", "[x", "[X"].contains(rest) {
                    cut = i
                    return end
                }
            }
            for box in ["[ ] ", "[x] ", "[X] "] where rest.hasPrefix(box) { i += box.count }
        } else if i < end, chars[i] == "#" {
            while i < end, chars[i] == "#" { i += 1 }
        }
        return i
    }
}
