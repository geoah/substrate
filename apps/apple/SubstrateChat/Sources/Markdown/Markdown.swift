import Foundation
import SubstrateKit

/// The block structure of a reply. Inline spans (emphasis, code, links) stay
/// as text here and are rendered by `AttributedString(markdown:)`, which
/// reads them but not blocks.
indirect enum MarkdownBlock: Hashable, Sendable {
    case paragraph(String)
    case heading(level: Int, text: String)
    /// `closed` is false while a streaming reply has not yet sent the fence's
    /// end: it still renders as code.
    case code(language: String?, text: String, closed: Bool)
    case quote([MarkdownBlock])
    case list(ordered: Bool, start: Int, items: [MarkdownListItem])
    case table(header: [String], alignments: [MarkdownAlignment], rows: [[String]])
    case rule
}

struct MarkdownListItem: Hashable, Sendable {
    /// A task item's box: nil on a plain item.
    var checked: Bool?
    var blocks: [MarkdownBlock]
}

enum MarkdownAlignment: Hashable, Sendable { case leading, center, trailing }

/// A small CommonMark-shaped block parser: what a model writes, read the way
/// a person expects, and safe on a prefix of itself, because a streaming
/// reply is parsed again with every flush. It never throws and never drops
/// text: what it cannot place is a paragraph.
enum Markdown {
    /// Parsing is bounded: a reply longer than this renders its head as
    /// Markdown and the rest as plain text.
    static let limit = 200_000

    static func parse(_ text: String) -> [MarkdownBlock] {
        if text.count > limit {
            let head = String(text.prefix(limit))
            return parse(lines: lines(head)) + [.paragraph(String(text.dropFirst(limit)))]
        }
        return parse(lines: lines(text))
    }

    private static func lines(_ text: String) -> [String] {
        text.replacingOccurrences(of: "\r\n", with: "\n")
            .replacingOccurrences(of: "\t", with: "    ")
            .components(separatedBy: "\n")
    }

    static func parse(lines: [String]) -> [MarkdownBlock] {
        var blocks: [MarkdownBlock] = []
        var paragraph: [String] = []
        var i = 0

        func flush() {
            if !paragraph.isEmpty {
                blocks.append(.paragraph(paragraph.joined(separator: "\n")))
                paragraph = []
            }
        }

        while i < lines.count {
            let line = lines[i]
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            let indent = indentation(line)

            if trimmed.isEmpty {
                flush()
                i += 1
                continue
            }
            // Indented code, only where it cannot be a paragraph's continuation.
            if indent >= 4, paragraph.isEmpty {
                var code: [String] = []
                while i < lines.count, indentation(lines[i]) >= 4 || lines[i].trimmingCharacters(in: .whitespaces).isEmpty {
                    code.append(lines[i].trimmingCharacters(in: .whitespaces).isEmpty ? "" : String(lines[i].dropFirst(4)))
                    i += 1
                }
                while code.last == "" { code.removeLast() }
                blocks.append(.code(language: nil, text: code.joined(separator: "\n"), closed: true))
                continue
            }
            if indent < 4, let fence = fence(trimmed) {
                flush()
                var code: [String] = []
                var closed = false
                i += 1
                while i < lines.count {
                    let t = lines[i].trimmingCharacters(in: .whitespaces)
                    if t.hasPrefix(fence.marker), t.allSatisfy({ $0 == fence.marker.first }) {
                        closed = true
                        i += 1
                        break
                    }
                    code.append(strip(lines[i], upTo: indent))
                    i += 1
                }
                blocks.append(.code(language: fence.language, text: code.joined(separator: "\n"), closed: closed))
                continue
            }
            if indent < 4, let heading = heading(trimmed) {
                flush()
                blocks.append(.heading(level: heading.level, text: heading.text))
                i += 1
                continue
            }
            // A setext underline turns the paragraph above it into a heading.
            if !paragraph.isEmpty, indent < 4, !trimmed.isEmpty,
               trimmed.allSatisfy({ $0 == "=" }) || (trimmed.allSatisfy({ $0 == "-" }) && trimmed.count >= 2)
            {
                let text = paragraph.joined(separator: " ")
                paragraph = []
                blocks.append(.heading(level: trimmed.first == "=" ? 1 : 2, text: text))
                i += 1
                continue
            }
            if indent < 4, isRule(trimmed) {
                flush()
                blocks.append(.rule)
                i += 1
                continue
            }
            if indent < 4, trimmed.hasPrefix(">") {
                flush()
                var quoted: [String] = []
                while i < lines.count {
                    let t = lines[i].trimmingCharacters(in: .whitespaces)
                    guard t.hasPrefix(">") else { break }
                    var rest = t.dropFirst()
                    if rest.first == " " { rest = rest.dropFirst() }
                    quoted.append(String(rest))
                    i += 1
                }
                blocks.append(.quote(parse(lines: quoted)))
                continue
            }
            if indent < 4, let marker = listMarker(line),
               paragraph.isEmpty || !marker.ordered || marker.number == 1
            {
                flush()
                let (list, next) = parseList(lines, from: i)
                blocks.append(list)
                i = next
                continue
            }
            if indent < 4, paragraph.isEmpty, i + 1 < lines.count, line.contains("|"),
               let alignments = delimiterRow(lines[i + 1]), !alignments.isEmpty
            {
                let header = cells(line)
                var rows: [[String]] = []
                i += 2
                while i < lines.count {
                    let t = lines[i].trimmingCharacters(in: .whitespaces)
                    guard !t.isEmpty, t.contains("|") else { break }
                    rows.append(cells(lines[i]))
                    i += 1
                }
                let width = max(header.count, alignments.count)
                func pad(_ row: [String]) -> [String] {
                    Array((row + Array(repeating: "", count: max(0, width - row.count))).prefix(width))
                }
                let aligned = alignments + Array(repeating: MarkdownAlignment.leading, count: max(0, width - alignments.count))
                blocks.append(.table(header: pad(header), alignments: Array(aligned.prefix(width)), rows: rows.map(pad)))
                continue
            }
            paragraph.append(trimmed)
            i += 1
        }
        flush()
        return blocks
    }

    // MARK: Lists

    private struct Marker {
        var indent: Int
        var ordered: Bool
        var number: Int
        var bullet: Character
        /// Where the item's content starts, from the line's first column.
        var content: Int
    }

    private static func listMarker(_ line: String) -> Marker? {
        let indent = indentation(line)
        guard indent < 4 else { return nil }
        let rest = Array(line.dropFirst(indent))
        guard let first = rest.first else { return nil }
        if "-*+".contains(first) {
            guard rest.count == 1 || rest[1] == " " else { return nil }
            // `***` and `- - -` are rules, not items.
            if isRule(String(rest)) { return nil }
            let spaces = rest.dropFirst().prefix(while: { $0 == " " }).count
            return Marker(indent: indent, ordered: false, number: 0, bullet: first,
                          content: indent + 1 + (spaces == 0 || spaces > 4 ? 1 : spaces))
        }
        let digits = rest.prefix(while: \.isNumber)
        guard !digits.isEmpty, digits.count <= 9, rest.count > digits.count else { return nil }
        let delimiter = rest[digits.count]
        guard delimiter == "." || delimiter == ")" else { return nil }
        guard rest.count == digits.count + 1 || rest[digits.count + 1] == " " else { return nil }
        let spaces = rest.dropFirst(digits.count + 1).prefix(while: { $0 == " " }).count
        return Marker(indent: indent, ordered: true, number: Int(String(digits)) ?? 1, bullet: delimiter,
                      content: indent + digits.count + 1 + (spaces == 0 || spaces > 4 ? 1 : spaces))
    }

    private static func parseList(_ lines: [String], from start: Int) -> (MarkdownBlock, Int) {
        guard let first = listMarker(lines[start]) else { return (.paragraph(lines[start]), start + 1) }
        var items: [MarkdownListItem] = []
        var i = start
        while i < lines.count, let marker = listMarker(lines[i]), marker.ordered == first.ordered,
              marker.bullet == first.bullet, marker.indent < first.content
        {
            var body = [String(lines[i].dropFirst(min(marker.content, lines[i].count)))]
            i += 1
            while i < lines.count {
                let line = lines[i]
                let t = line.trimmingCharacters(in: .whitespaces)
                if t.isEmpty {
                    // A blank line keeps the item open only when what follows
                    // is indented under it.
                    var j = i + 1
                    while j < lines.count, lines[j].trimmingCharacters(in: .whitespaces).isEmpty { j += 1 }
                    if j < lines.count, indentation(lines[j]) >= marker.content {
                        body.append(contentsOf: Array(repeating: "", count: j - i))
                        i = j
                        continue
                    }
                    break
                }
                if indentation(line) >= marker.content {
                    body.append(strip(line, upTo: marker.content))
                    i += 1
                    continue
                }
                if listMarker(line) != nil || fence(t) != nil || heading(t) != nil || t.hasPrefix(">") || isRule(t) {
                    break
                }
                // A lazy continuation of the item's paragraph.
                body.append(t)
                i += 1
            }
            var checked: Bool?
            if let head = body.first {
                for (box, value) in [("[ ] ", false), ("[x] ", true), ("[X] ", true)] where head.hasPrefix(box) {
                    checked = value
                    body[0] = String(head.dropFirst(box.count))
                }
            }
            items.append(MarkdownListItem(checked: checked, blocks: parse(lines: body)))
            // Blank lines between items keep the list going.
            var j = i
            while j < lines.count, lines[j].trimmingCharacters(in: .whitespaces).isEmpty { j += 1 }
            if j > i, j < lines.count, let next = listMarker(lines[j]), next.ordered == first.ordered,
               next.bullet == first.bullet, next.indent < first.content
            {
                i = j
            }
        }
        return (.list(ordered: first.ordered, start: first.number, items: items), i)
    }

    // MARK: Lines

    private static func indentation(_ line: String) -> Int { line.prefix(while: { $0 == " " }).count }

    private static func strip(_ line: String, upTo n: Int) -> String {
        String(line.dropFirst(min(n, indentation(line))))
    }

    private static func fence(_ trimmed: String) -> (marker: String, language: String?)? {
        for ch in ["`", "~"] {
            let run = trimmed.prefix(while: { String($0) == ch })
            guard run.count >= 3 else { continue }
            let info = trimmed.dropFirst(run.count).trimmingCharacters(in: .whitespaces)
            if ch == "`", info.contains("`") { return nil }
            let language = info.split(separator: " ").first.map(String.init)
            return (String(run), language?.isEmpty == false ? language : nil)
        }
        return nil
    }

    private static func heading(_ trimmed: String) -> (level: Int, text: String)? {
        let hashes = trimmed.prefix(while: { $0 == "#" }).count
        guard (1 ... 6).contains(hashes) else { return nil }
        let rest = trimmed.dropFirst(hashes)
        guard rest.isEmpty || rest.first == " " else { return nil }
        var text = rest.trimmingCharacters(in: .whitespaces)
        // A closing run of #s is not part of the text.
        if let end = text.lastIndex(where: { $0 != "#" }), text.last == "#", text[end] == " " {
            text = String(text[...end]).trimmingCharacters(in: .whitespaces)
        }
        return (hashes, text)
    }

    private static func isRule(_ trimmed: String) -> Bool {
        let chars = trimmed.filter { $0 != " " }
        guard chars.count >= 3, let c = chars.first, "-*_".contains(c) else { return false }
        return chars.allSatisfy { $0 == c }
    }

    private static func delimiterRow(_ line: String) -> [MarkdownAlignment]? {
        let t = line.trimmingCharacters(in: .whitespaces)
        guard t.contains("-"), t.allSatisfy({ "|:- ".contains($0) }) else { return nil }
        let parts = cells(t)
        guard !parts.isEmpty else { return nil }
        var out: [MarkdownAlignment] = []
        for p in parts {
            guard p.contains("-") else { return nil }
            let left = p.hasPrefix(":"), right = p.hasSuffix(":")
            out.append(left && right ? .center : right ? .trailing : .leading)
        }
        return out
    }

    private static func cells(_ line: String) -> [String] {
        var t = line.trimmingCharacters(in: .whitespaces)
        if t.hasPrefix("|") { t.removeFirst() }
        if t.hasSuffix("|"), !t.hasSuffix("\\|") { t.removeLast() }
        var out: [String] = []
        var current = ""
        var escaped = false
        var inCode = false
        for ch in t {
            if escaped {
                current.append(ch == "|" ? "|" : "\\\(ch)")
                escaped = false
            } else if ch == "\\" {
                escaped = true
            } else if ch == "`" {
                inCode.toggle()
                current.append(ch)
            } else if ch == "|", !inCode {
                out.append(current.trimmingCharacters(in: .whitespaces))
                current = ""
            } else {
                current.append(ch)
            }
        }
        out.append(current.trimmingCharacters(in: .whitespaces))
        return out
    }
}
