import Foundation

/// The readers of what is no programming language with keywords: JSON,
/// YAML, a shell, HTML and XML, CSS, and a diff.
extension CodeLexers {
    private static let quote = UInt8(ascii: "\"")
    private static let apostrophe = UInt8(ascii: "'")
    private static let backslash = UInt8(ascii: "\\")
    private static let hash = UInt8(ascii: "#")
    private static let slash = UInt8(ascii: "/")
    private static let star = UInt8(ascii: "*")
    private static let colon = UInt8(ascii: ":")
    private static let space = UInt8(ascii: " ")

    /// The byte after the quote that closes the string opening at `from`,
    /// on this line. A backslash escapes where `escapes` says so; in YAML's
    /// single quotes two quotes are one.
    private static func quoted(_ s: CodeScan, from: Int, escapes: Bool = true, doubled: Bool = false) -> Int? {
        let mark = s.line[from]
        var i = from + 1
        while i < s.end {
            let c = s.line[i]
            if escapes, c == backslash {
                i += 2
            } else if c == mark {
                if doubled, s.byte(i + 1) == mark {
                    i += 2
                } else {
                    return i + 1
                }
            } else {
                i += 1
            }
        }
        return nil
    }

    /// A block comment from `from`, which is inside it or at its `/*`:
    /// painted to its end, or to the line's with the state saying it runs
    /// on. False where the line ended in it.
    private static func blockComment(_ s: inout CodeScan, _ state: inout LexState, from: Int) -> Bool {
        guard let close = s.find([star, slash], from: state.mode == .comment ? from : from + 2) else {
            s.paint(from, s.end, .comment)
            state.mode = .comment
            s.at = s.end
            return false
        }
        s.paint(from, close + 2, .comment)
        s.at = close + 2
        state.mode = .code
        return true
    }

    // MARK: JSON

    /// Keys, strings, numbers and the three words; `//` and `/* */` where a
    /// writer put them (JSON with comments).
    static func json(_ s: inout CodeScan, _ state: inout LexState) {
        if state.mode == .comment {
            guard blockComment(&s, &state, from: 0) else { return }
        } else {
            state = LexState()
        }
        while s.at < s.end {
            let c = s.line[s.at]
            let from = s.at
            if c == quote {
                guard let end = quoted(s, from: from) else { return }
                s.paint(from, end, s.byte(s.pastSpaces(end)) == colon ? .name : .string)
                s.at = end
            } else if c == slash, s.byte(from + 1) == slash {
                s.paint(from, s.end, .comment)
                return
            } else if c == slash, s.byte(from + 1) == star {
                guard blockComment(&s, &state, from: from) else { return }
            } else if c.isDigit || (c == UInt8(ascii: "-") && s.byte(from + 1)?.isDigit == true) {
                let end = numberEnd(s, from: c.isDigit ? from : from + 1)
                s.paint(from, end, .number)
                s.at = end
            } else if c.isLetter {
                var end = from
                while end < s.end, s.line[end].inName { end += 1 }
                if ["true", "false", "null"].contains(s.text(from, end)) { s.paint(from, end, .keyword) }
                s.at = end
            } else {
                s.at += 1
            }
        }
    }

    // MARK: YAML

    private static let yamlWords: Set<String> = ["true", "false", "null", "yes", "no", "on", "off", "~"]

    /// Keys, quoted strings, comments, anchors and tags, and a value that
    /// is one of YAML's own words or a number. A block scalar's lines are
    /// text, whatever they hold, and are left plain.
    static func yaml(_ s: inout CodeScan, _ state: inout LexState) {
        let indent = s.pastSpaces(0)
        if state.mode == .scalar {
            if indent == s.end || indent > state.depth { return }
        }
        state = LexState()
        var i = indent
        guard i < s.end else { return }
        if s.line[i] == hash {
            s.paint(i, s.end, .comment)
            return
        }
        if indent == 0, s.has(Array("---".utf8), at: 0) || s.has(Array("...".utf8), at: 0), s.byte(3) == nil || s.byte(3) == space {
            s.paint(0, 3, .meta)
            i = 3
        }
        // A sequence's dashes, one for each level opened on this line.
        while s.byte(i) == UInt8(ascii: "-"), s.byte(i + 1) == nil || s.byte(i + 1) == space { i = s.pastSpaces(i + 1) }
        if let key = yamlKey(s, from: i) {
            s.paint(i, key.end, .name)
            i = key.colon + 1
        }
        var flow = 0
        while true {
            i = s.pastSpaces(i)
            guard i < s.end else { return }
            let c = s.line[i]
            if c == hash, i == 0 || s.line[i - 1] == space {
                s.paint(i, s.end, .comment)
                return
            }
            if c == quote || c == apostrophe {
                guard let end = quoted(s, from: i, escapes: c == quote, doubled: c == apostrophe) else { return }
                s.paint(i, end, .string)
                i = end
                continue
            }
            if "[{".utf8.contains(c) {
                flow += 1
                i += 1
                continue
            }
            if "]},".utf8.contains(c), flow > 0 {
                if c != UInt8(ascii: ",") { flow -= 1 }
                i += 1
                continue
            }
            // A value's end: the line's, a comment's start, or inside a
            // flow collection the mark that ends it.
            var end = i
            while end < s.end {
                let b = s.line[end]
                if b == hash, s.line[end - 1] == space { break }
                if flow > 0, "]},".utf8.contains(b) { break }
                end += 1
            }
            var last = end
            while last > i, s.line[last - 1] == space { last -= 1 }
            let word = s.text(i, last)
            if c == UInt8(ascii: "&") || c == UInt8(ascii: "*") || c == UInt8(ascii: "!") {
                // An anchor, an alias, a tag: up to its first space.
                var stop = i
                while stop < last, s.line[stop] != space { stop += 1 }
                if stop > i + 1 || c == UInt8(ascii: "!") { s.paint(i, stop, .meta) }
                i = stop
                continue
            }
            if flow == 0, c == UInt8(ascii: "|") || c == UInt8(ascii: ">"), word.dropFirst().allSatisfy({ "+-0123456789".contains($0) }) {
                state.mode = .scalar
                state.depth = indent
            } else if yamlWords.contains(word.lowercased()), word == word.lowercased() || word == word.uppercased() || word == word.capitalized {
                s.paint(i, last, .keyword)
            } else if yamlNumber(word) {
                s.paint(i, last, .number)
            }
            i = end
        }
    }

    /// A mapping's key at `from`: where it ends and where its colon is. The
    /// colon is a key's only before a space or the line's end, so a time
    /// (`12:30`) and an address (`http://…`) are none.
    private static func yamlKey(_ s: CodeScan, from: Int) -> (end: Int, colon: Int)? {
        guard let first = s.byte(from) else { return nil }
        if first == quote || first == apostrophe {
            guard let end = quoted(s, from: from, escapes: first == quote, doubled: first == apostrophe) else { return nil }
            let after = s.pastSpaces(end)
            guard s.byte(after) == colon, s.byte(after + 1) == nil || s.byte(after + 1) == space else { return nil }
            return (end, after)
        }
        guard !"[]{}&*!|>%@`#,?".utf8.contains(first) else { return nil }
        var i = from
        while i < s.end {
            let c = s.line[i]
            if c == colon, s.byte(i + 1) == nil || s.byte(i + 1) == space {
                var end = i
                while end > from, s.line[end - 1] == space { end -= 1 }
                return end > from ? (end, i) : nil
            }
            if c == hash, i > 0, s.line[i - 1] == space { return nil }
            i += 1
        }
        return nil
    }

    /// A whole scalar that YAML reads as a number: an integer, a decimal
    /// with one point, an exponent, a hexadecimal. A version (`1.2.3`) and
    /// a date are none.
    static func yamlNumber(_ word: String) -> Bool {
        var rest = Substring(word)
        if rest.first == "-" || rest.first == "+" { rest = rest.dropFirst() }
        guard let first = rest.first, first.isASCII, first.isNumber else { return false }
        if rest.hasPrefix("0x") { return rest.count > 2 && rest.dropFirst(2).allSatisfy(\.isHexDigit) }
        var points = 0
        var exponent = false
        var previous: Character = " "
        for ch in rest {
            if ch.isASCII, ch.isNumber || ch == "_" {
            } else if ch == ".", points == 0, !exponent {
                points += 1
            } else if ch == "e" || ch == "E", !exponent {
                exponent = true
            } else if ch == "-" || ch == "+", previous == "e" || previous == "E" {
            } else {
                return false
            }
            previous = ch
        }
        return previous.isNumber
    }

    // MARK: A shell

    private static let shellWords: Set<String> = [
        "if", "then", "else", "elif", "fi", "for", "while", "until", "do", "done", "case", "esac", "select", "function", "return", "exit", "break",
        "continue", "export", "local", "readonly", "declare", "typeset", "source", "unset",
    ]
    /// After these a command follows, as after `;` and `|`.
    private static let shellLeads: Set<String> = ["if", "then", "else", "elif", "do", "while", "until", "!", "time"]
    private static let shellBreaks = Set(" ;|&()<>\"'$`".utf8)

    /// Comments, quoted strings (which run over lines), variables, and the
    /// shell's own words where a command stands. A here-document's body is
    /// text and is left plain.
    static func shell(_ s: inout CodeScan, _ state: inout LexState) {
        switch state.mode {
        case .heredoc:
            let line = s.text(0, s.end)
            if (state.depth == 1 ? String(line.drop { $0 == " " }) : line) == state.word { state = LexState() }
            return
        case let .string(mark):
            guard let end = shellQuoteEnd(s, from: 0, mark: mark) else {
                s.paint(0, s.end, .string)
                return
            }
            s.paint(0, end, .string)
            s.at = end
            state = LexState()
        default:
            state = LexState()
        }
        /// The next word names a command.
        var command = s.at == 0
        /// A `for`, `case` or `select` is open on this line: `in` is its.
        var loops = false
        var heredoc: (word: String, strips: Bool)?
        defer {
            if let heredoc, state.mode == .code {
                state.mode = .heredoc
                state.word = heredoc.word
                state.depth = heredoc.strips ? 1 : 0
            }
        }
        while s.at < s.end {
            let c = s.line[s.at]
            let from = s.at
            if c == space {
                s.at += 1
            } else if c == hash, from == 0 || " ;|&(".utf8.contains(s.line[from - 1]) {
                s.paint(from, s.end, .comment)
                return
            } else if c == quote || c == apostrophe {
                guard let end = shellQuoteEnd(s, from: from + 1, mark: c) else {
                    s.paint(from, s.end, .string)
                    state.mode = .string(c)
                    return
                }
                s.paint(from, end, .string)
                s.at = end
                command = false
            } else if c == UInt8(ascii: "$") {
                let next = s.byte(from + 1)
                if next == UInt8(ascii: "{"), let close = s.find([UInt8(ascii: "}")], from: from + 2) {
                    s.paint(from, close + 1, .name)
                    s.at = close + 1
                } else if next == UInt8(ascii: "(") {
                    s.at = from + 2
                    command = true
                    continue
                } else if let next, next.startsName || next.isDigit || "@*#?!$-".utf8.contains(next) {
                    var end = from + 2
                    if next.startsName { while end < s.end, s.line[end].inName { end += 1 } }
                    s.paint(from, end, .name)
                    s.at = end
                } else {
                    s.at += 1
                }
                command = false
            } else if c == backslash {
                s.at += 2
            } else if ";|&(){}".utf8.contains(c) {
                s.at += 1
                command = true
            } else if c == UInt8(ascii: "<"), s.byte(from + 1) == c, s.byte(from + 2) != c, s.byte(from - 1) != c {
                var i = from + 2
                let strips = s.byte(i) == UInt8(ascii: "-")
                if strips { i += 1 }
                i = s.pastSpaces(i)
                if s.byte(i) == quote || s.byte(i) == apostrophe { i += 1 }
                var end = i
                while end < s.end, s.line[end].inName { end += 1 }
                if end > i { heredoc = (s.text(i, end), strips) }
                s.at = max(end, from + 2)
            } else if shellBreaks.contains(c) {
                s.at += 1
            } else {
                var end = from
                while end < s.end, !shellBreaks.contains(s.line[end]) { end += 1 }
                let word = s.text(from, end)
                s.at = end
                if command {
                    if shellWords.contains(word) || shellLeads.contains(word) {
                        if word != "!" { s.paint(from, end, .keyword) }
                        if ["for", "case", "select"].contains(word) { loops = true }
                        command = shellLeads.contains(word)
                    } else if !(word.first?.isLetter == true && word.contains("=")) {
                        // An assignment before a command leaves the command
                        // still to come.
                        command = false
                    }
                } else if word == "in", loops {
                    s.paint(from, end, .keyword)
                    loops = false
                }
            }
        }
    }

    /// The byte after the quote that closes a shell string, from inside it:
    /// nothing escapes in single quotes.
    private static func shellQuoteEnd(_ s: CodeScan, from: Int, mark: UInt8) -> Int? {
        var i = from
        while i < s.end {
            if mark == quote, s.line[i] == backslash {
                i += 2
            } else if s.line[i] == mark {
                return i + 1
            } else {
                i += 1
            }
        }
        return nil
    }

    // MARK: HTML and XML

    private static func inTagName(_ c: UInt8) -> Bool { c.inName || c == UInt8(ascii: "-") || c == colon || c == UInt8(ascii: ".") }

    /// Tags' names, attributes and their quoted values, comments, and a
    /// doctype. The body of a `script` or a `style` is another language and
    /// is left plain.
    static func html(_ s: inout CodeScan, _ state: inout LexState) {
        while s.at < s.end {
            switch state.mode {
            case .markupComment:
                guard let close = s.find(Array("-->".utf8), from: s.at) else {
                    s.paint(s.at, s.end, .comment)
                    return
                }
                s.paint(s.at, close + 3, .comment)
                s.at = close + 3
                state = LexState()
            case .rawText:
                guard let close = s.find(Array("</\(state.word)".utf8), from: s.at) else { return }
                s.at = close
                state = LexState()
            case .tag:
                s.at = s.pastSpaces(s.at)
                guard s.at < s.end else { return }
                let c = s.line[s.at]
                if c == UInt8(ascii: ">") {
                    s.at += 1
                    let raw = state.word == "script" || state.word == "style"
                    let word = state.word
                    state = LexState()
                    if raw {
                        state.mode = .rawText
                        state.word = word
                    }
                } else if c == slash {
                    s.at += 1
                    state.word = ""
                } else if c == quote || c == apostrophe {
                    guard let end = quoted(s, from: s.at, escapes: false) else { return }
                    s.paint(s.at, end, .string)
                    s.at = end
                } else if c == UInt8(ascii: "=") {
                    s.at += 1
                } else if c == UInt8(ascii: "{") {
                    // An expression in braces, as JSX and templates write
                    // them: not markup.
                    s.at = s.find([UInt8(ascii: "}")], from: s.at).map { $0 + 1 } ?? s.end
                } else {
                    var end = s.at
                    while end < s.end, !" =>/\"'".utf8.contains(s.line[end]) { end += 1 }
                    if end == s.at { end += 1 } else { s.paint(s.at, end, .name) }
                    s.at = end
                }
            default:
                guard let open = s.find([UInt8(ascii: "<")], from: s.at) else { return }
                if s.has(Array("<!--".utf8), at: open) {
                    s.at = open
                    state.mode = .markupComment
                } else if s.byte(open + 1) == UInt8(ascii: "!") || s.byte(open + 1) == UInt8(ascii: "?") {
                    guard let close = s.find([UInt8(ascii: ">")], from: open) else { return }
                    s.paint(open, close + 1, .meta)
                    s.at = close + 1
                } else {
                    let closing = s.byte(open + 1) == slash
                    let start = open + (closing ? 2 : 1)
                    var end = start
                    if s.byte(start)?.isLetter == true { while end < s.end, inTagName(s.line[end]) { end += 1 } }
                    guard end > start else {
                        s.at = open + 1
                        continue
                    }
                    s.paint(start, end, .keyword)
                    s.at = end
                    state.mode = .tag
                    state.word = closing ? "" : s.text(start, end).lowercased()
                }
            }
        }
    }

    // MARK: CSS

    private static func inStyleName(_ c: UInt8) -> Bool { c.inName || c == UInt8(ascii: "-") }

    /// Comments, strings, at-rules, a declaration's property, and the
    /// numbers and colours of its value. A selector is left plain: its
    /// words are the page's, not the language's. `state.depth` is how many
    /// braces are open and `state.extra` whether a value is.
    static func css(_ s: inout CodeScan, _ state: inout LexState, loose: Bool) {
        if state.mode == .comment {
            guard blockComment(&s, &state, from: 0) else { return }
        }
        /// Nothing of a statement has been read yet.
        var fresh = state.extra == 0
        while s.at < s.end {
            let c = s.line[s.at]
            let from = s.at
            if c == space {
                s.at += 1
                continue
            }
            if c == slash, s.byte(from + 1) == star {
                guard blockComment(&s, &state, from: from) else { return }
                continue
            }
            if loose, c == slash, s.byte(from + 1) == slash, from == 0 || s.line[from - 1] == space {
                s.paint(from, s.end, .comment)
                return
            }
            if c == quote || c == apostrophe {
                guard let end = quoted(s, from: from) else { return }
                s.paint(from, end, .string)
                s.at = end
                fresh = false
                continue
            }
            if "{};".utf8.contains(c) {
                if c == UInt8(ascii: "{") { state.depth += 1 }
                if c == UInt8(ascii: "}") { state.depth = max(0, state.depth - 1) }
                state.extra = 0
                fresh = true
                s.at += 1
                continue
            }
            if c == UInt8(ascii: "@"), s.byte(from + 1)?.isLetter == true {
                var end = from + 1
                while end < s.end, inStyleName(s.line[end]) { end += 1 }
                s.paint(from, end, .keyword)
                s.at = end
                fresh = false
                continue
            }
            if state.extra == 1 {
                // In a value: a colour, a number with its unit.
                if c == hash {
                    var end = from + 1
                    while end < s.end, s.line[end].isDigit || s.line[end].isLetter { end += 1 }
                    let digits = s.line[(from + 1) ..< end]
                    if [3, 4, 6, 8].contains(digits.count), digits.allSatisfy({ $0.isDigit || ($0 | 0x20 >= 0x61 && $0 | 0x20 <= 0x66) }) {
                        s.paint(from, end, .number)
                    }
                    s.at = max(end, from + 1)
                    continue
                }
                let signed = (c == UInt8(ascii: "-") || c == UInt8(ascii: "+")) && from > 0 && !inStyleName(s.line[from - 1])
                let digits = signed ? from + 1 : from
                if s.byte(digits)?.isDigit == true || (s.byte(digits) == UInt8(ascii: ".") && s.byte(digits + 1)?.isDigit == true),
                    from == 0 || !inStyleName(s.line[from - 1])
                {
                    var end = digits
                    while end < s.end, s.line[end].isDigit || s.line[end] == UInt8(ascii: ".") { end += 1 }
                    while end < s.end, s.line[end].isLetter || s.line[end] == UInt8(ascii: "%") { end += 1 }
                    s.paint(from, end, .number)
                    s.at = end
                    continue
                }
            }
            if c.startsName || c == UInt8(ascii: "-") || c == UInt8(ascii: "$") {
                var end = from + 1
                while end < s.end, inStyleName(s.line[end]) { end += 1 }
                let after = s.pastSpaces(end)
                // A property: the first word of a statement inside braces
                // (or a variable anywhere), before a colon, with no brace
                // after it on the line, which would make it a selector
                // (`a:hover {`).
                let declares = state.depth > 0 || c == UInt8(ascii: "$") || s.has(Array("--".utf8), at: from)
                if fresh, declares, s.byte(after) == colon, s.byte(after + 1) != colon, s.find([UInt8(ascii: "{")], from: after) == nil {
                    s.paint(from, end, .name)
                    state.extra = 1
                    s.at = after + 1
                } else {
                    s.at = end
                }
                fresh = false
                continue
            }
            s.at += 1
            fresh = false
        }
    }

    // MARK: A diff

    private static let diffHeads = [
        "diff ", "index ", "new file", "deleted file", "old mode", "new mode", "rename ", "copy ", "similarity ", "dissimilarity ", "Binary files",
    ].map { Array($0.utf8) }

    /// Each line by its first character: added, removed, a hunk's header, a
    /// file's. Inside a hunk the header's counts say how many lines are its
    /// own, so a removed line that starts with dashes of its own is not
    /// read as a file's header. `state.depth` and `state.extra` are the
    /// lines of the old and of the new file still to come.
    static func diff(_ s: inout CodeScan, _ state: inout LexState) {
        let first = s.byte(0)
        if s.has(Array("@@".utf8), at: 0) {
            let close = s.find(Array("@@".utf8), from: 2).map { $0 + 2 } ?? s.end
            s.paint(0, close, .meta)
            let counts = diffCounts(s.text(0, close))
            state.depth = counts.old
            state.extra = counts.new
            return
        }
        if state.depth > 0 || state.extra > 0 {
            switch first {
            case UInt8(ascii: "+"):
                s.paint(0, s.end, .added)
                state.extra = max(0, state.extra - 1)
                return
            case UInt8(ascii: "-"):
                s.paint(0, s.end, .removed)
                state.depth = max(0, state.depth - 1)
                return
            case space, nil:
                state.depth = max(0, state.depth - 1)
                state.extra = max(0, state.extra - 1)
                return
            case backslash:
                s.paint(0, s.end, .meta)
                return
            default:
                // Not a hunk's line: the hunk was cut short.
                state.depth = 0
                state.extra = 0
            }
        }
        let line = s.text(0, s.end)
        if line == "---" || line == "+++" || line.hasPrefix("--- ") || line.hasPrefix("+++ ") || diffHeads.contains(where: { s.has($0, at: 0) }) {
            s.paint(0, s.end, .meta)
        } else if first == UInt8(ascii: "+") {
            s.paint(0, s.end, .added)
        } else if first == UInt8(ascii: "-") {
            s.paint(0, s.end, .removed)
        }
    }

    /// A hunk header's line counts: `@@ -3,7 +3,9 @@` is 7 old and 9 new;
    /// a side with no count is one line.
    static func diffCounts(_ header: String) -> (old: Int, new: Int) {
        func count(_ sign: Character) -> Int {
            guard let side = header.split(separator: " ").first(where: { $0.first == sign }) else { return 0 }
            let parts = side.dropFirst().split(separator: ",")
            return parts.count > 1 ? Int(parts[1]) ?? 0 : 1
        }
        return (count("-"), count("+"))
    }
}
