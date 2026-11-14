import Foundation
import SubstrateRuntime

public extension MeterPoint {
    /// A reply still being written made into the blocks it draws: its
    /// parse, and what closes it for display around that.
    static let write = MeterPoint("write")
}

/// A reply still being written, as the blocks it draws now. What its text
/// so far would draw as raw markers is closed or held back until it is
/// whole, so the page only ever grows: nothing is drawn that the next piece
/// takes away or turns from a marker into styling.
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
/// Whether the text ends inside code, a list item or a quote is the
/// parser's to say and nobody else's, so the text is parsed as it stands
/// first. Where it ends in code that is the answer. Otherwise its last line
/// is put right as text where a block's start is at stake (a table, a box,
/// a mark that waits) and parsed again only if that changed it, and what is
/// open inline is closed in the one block still being written: its text is
/// exactly what the inline renderer is given.
///
/// The text of a finished reply is never passed here: a marker its writer
/// left open is drawn as the stored reply draws it.
public extension Markdown {
    static func parse(writing text: String) -> [MarkdownBlock] {
        Meter.shared.time(.write) { written(text) }
    }

    private static func written(_ text: String) -> [MarkdownBlock] {
        guard text.count <= limit else { return parse(text) }
        var lines = lines(text)
        var blocks = Meter.shared.time(.parse) { parse(lines: lines) }
        if case let .code(_, code, closed)? = innermost(blocks) {
            // A fence's closing line arrives a mark at a time: until it is
            // whole it would draw as a line of the code. So would the line
            // that has only begun: a line's end is not a line more to draw
            // until something is on it.
            guard !closed, let last = code.split(separator: "\n", omittingEmptySubsequences: false).last else { return blocks }
            let marks = last.trimmingCharacters(in: .whitespaces)
            guard marks.isEmpty || marks == "`" || marks == "``" || marks == "~" || marks == "~~" else { return blocks }
            return replacingLast(blocks) { block in
                guard case let .code(language, code, closed) = block else { return block }
                let kept = code.split(separator: "\n", omittingEmptySubsequences: false).dropLast()
                return .code(language: language, text: kept.joined(separator: "\n"), closed: closed)
            }
        }

        // The last line, where it is the start of a block.
        let given = lines
        var held = false
        var lineEnded = lines.count > 1 && lines[lines.count - 1].isEmpty
        if !lineEnded, let last = lines.last {
            let bare = unquoted(last)
            if ["-", "--", "*", "+", "|", "_", "__"].contains(bare) {
                // Nothing says what it starts yet: a bullet, a rule and a
                // line of bold begin alike, and a table with one pipe.
                lines.removeLast()
                lineEnded = true
                held = true
            } else if let marker = listMarker(bare) {
                let rest = String(bare.dropFirst(min(marker.content, bare.count))).trimmingCharacters(in: .whitespaces)
                if ["[ ]", "[x]", "[X]"].contains(rest) {
                    // The box is whole: the space the parser reads after it.
                    lines[lines.count - 1] = last + " "
                } else if ["[", "[x", "[X"].contains(rest) {
                    lines.removeLast()
                    lineEnded = true
                    held = true
                }
            }
        }
        // A table's first line, and the line under it.
        let ended = lines.count > 1 && lines[lines.count - 1].isEmpty
        let body = ended ? Array(lines.dropLast()) : lines
        let run = body.reversed().prefix(while: isRow).count
        if run == 1 || run == 2 {
            let header = body.count - run
            let columns = cells(body[header]).count
            // Where the parser starts a table: at the text's start, under a
            // blank line, and under a heading, which is a block by itself.
            let above = header > 0 ? body[header - 1].trimmingCharacters(in: .whitespaces) : ""
            let headed = header > 0 && body[header - 1].prefix(while: { $0 == " " }).count < 4 && heading(above) != nil
            if above.isEmpty || headed, cells(body[header]).contains(where: { !$0.isEmpty }) {
                let rule = "|" + String(repeating: " --- |", count: columns)
                if run == 1 {
                    lines.insert(rule, at: header + 1)
                } else if body[header + 1].allSatisfy({ "|:- ".contains($0) }), (delimiterRow(body[header + 1])?.count ?? 0) < columns {
                    // Part of it has arrived: not yet a line the parser
                    // takes for one, or one for fewer columns than the
                    // header names.
                    lines[header + 1] = rule
                    lineEnded = true
                }
            }
        }
        if lines != given { blocks = Meter.shared.time(.parse) { parse(lines: lines) } }

        // What is open inline, in the block still being written. A blank
        // line ends a paragraph, and a line's end a heading or a table's row:
        // what such a block left open is its writer's, and stays. A line
        // held back stood after whatever blank line there is: one is
        // enough then, where the text's own end takes two.
        let blank = lines.reversed().prefix { $0.trimmingCharacters(in: .whitespaces).isEmpty }.count
        let blankEnded = blank >= (held ? 1 : 2) && blank < lines.count
        return closingLast(blocks, paragraph: !blankEnded, line: !lineEnded)
    }

    // MARK: Blocks

    /// The block the text ends in: the last, and the last inside it.
    private static func innermost(_ blocks: [MarkdownBlock]) -> MarkdownBlock? {
        guard let last = blocks.last else { return nil }
        switch last {
        case let .quote(inner): return innermost(inner) ?? last
        case let .list(_, _, items): return items.last.flatMap { innermost($0.blocks) } ?? last
        default: return last
        }
    }

    /// The blocks with the one the text ends in replaced, or taken out
    /// where `change` answers none.
    private static func replacingLast(_ blocks: [MarkdownBlock], _ change: (MarkdownBlock) -> MarkdownBlock?) -> [MarkdownBlock] {
        guard let last = blocks.last else { return blocks }
        var out = blocks
        switch last {
        case let .quote(inner) where !inner.isEmpty:
            out[out.count - 1] = .quote(replacingLast(inner, change))
        case .list(let ordered, let start, var items) where items.last?.blocks.isEmpty == false:
            items[items.count - 1].blocks = replacingLast(items[items.count - 1].blocks, change)
            out[out.count - 1] = .list(ordered: ordered, start: start, items: items)
        default:
            if let changed = change(last) { out[out.count - 1] = changed } else { out.removeLast() }
        }
        return out
    }

    private static func closingLast(_ blocks: [MarkdownBlock], paragraph: Bool, line: Bool) -> [MarkdownBlock] {
        replacingLast(blocks) { block in
            switch block {
            case let .paragraph(text) where paragraph:
                // Nothing left to draw yet: a marker with nothing in it.
                let closed = closedInline(text)
                return closed.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? nil : .paragraph(closed)
            case let .heading(level, text) where line:
                return .heading(level: level, text: closedInline(text))
            case .table(var header, let alignments, var rows) where line:
                // Each cell is its own: only the last written can be open.
                if var row = rows.last, let cell = row.lastIndex(where: { !$0.isEmpty }) {
                    row[cell] = closedInline(row[cell])
                    rows[rows.count - 1] = row
                } else if rows.isEmpty, let cell = header.lastIndex(where: { !$0.isEmpty }) {
                    header[cell] = closedInline(header[cell])
                }
                return .table(header: header, alignments: alignments, rows: rows)
            default:
                return block
            }
        }
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

    private static func isRow(_ line: String) -> Bool { line.trimmingCharacters(in: .whitespaces).hasPrefix("|") }

    // MARK: Inline

    private struct Open {
        var marker: Character
        var count: Int
        var at: Int
    }

    /// A block's text with what is open in it closed or left out.
    static func closedInline(_ text: String) -> String {
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

        func run(_ at: Int) -> Int {
            var n = 0
            while at + n < chars.count, chars[at + n] == chars[at] { n += 1 }
            return n
        }
        func word(_ c: Character?) -> Bool { c.map { $0.isLetter || $0.isNumber } ?? false }
        // A bracket that follows a letter or a digit is an index (`a[i]`),
        // not a link's label.
        func opensLabel(_ at: Int) -> Bool { at == 0 || !word(chars[at - 1]) }

        var i = 0
        while i < chars.count {
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
                i += 1
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
                let canOpen = after.map { !$0.isWhitespace } ?? false
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
            case ">", " ", "\n":
                angle = nil
                i += 1
            default:
                i += 1
            }
        }

        if let link = address {
            // The address is not whole: the label, without its brackets.
            cut = link.close
            drop.insert(link.label)
            if link.label > 0, chars[link.label - 1] == "!" { drop.insert(link.label - 1) }
        } else if let link = closedLabel, opensLabel(link.label), chars[(link.label + 1) ..< link.close].contains(where: \.isLetter) {
            // An address may follow: the label, until the next piece says.
            drop.insert(link.label)
            drop.insert(link.close)
        } else if let label = labels.last, opensLabel(label), label + 1 < chars.count, chars[(label + 1)...].contains(where: \.isLetter) {
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
            if chars[min(span.at + span.count, end) ..< end].allSatisfy(\.isWhitespace) {
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
        return String(kept)
    }
}
