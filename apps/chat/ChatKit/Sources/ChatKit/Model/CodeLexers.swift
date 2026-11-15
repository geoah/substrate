import Foundation

/// The readers behind `CodeHighlighter`, one line at a time. The languages
/// with braces, keywords and quoted strings share one reader told how each
/// differs (`Syntax`); JSON, YAML, a shell, HTML, CSS and a diff have their
/// own (`CodeLexersMarkup.swift`).
enum CodeLexers {
    static func lex(_ scan: inout CodeScan, _ state: inout LexState, _ language: CodeLanguage, loose: Bool) {
        switch language {
        case .swift: words(&scan, &state, .swift)
        case .go: words(&scan, &state, .go)
        case .python: words(&scan, &state, .python)
        case .javascript: words(&scan, &state, .javascript)
        case .typescript: words(&scan, &state, .typescript)
        case .rust: words(&scan, &state, .rust)
        case .kotlin: words(&scan, &state, .kotlin)
        case .sql: words(&scan, &state, .sql)
        case .json: json(&scan, &state)
        case .yaml: yaml(&scan, &state)
        case .shell: shell(&scan, &state)
        case .html: html(&scan, &state)
        case .css: css(&scan, &state, loose: loose)
        case .diff: diff(&scan, &state)
        }
    }

    // MARK: How the languages with keywords differ

    struct Syntax: Sendable {
        enum Single: Sendable { case none, string, character }
        enum Backtick: Sendable { case none, raw, template, name }
        enum Interpolation: Sendable { case none, parenthesis, brace }

        var keywords: Set<String>
        /// Its longest keyword: a longer word is never looked up.
        var longest: Int
        /// What starts a comment to the line's end; empty for none.
        var lineComment: [UInt8] = Array("//".utf8)
        /// `#` starts a comment to the line's end.
        var hashComment = false
        var blockComments = true
        var nestedComments = false
        /// What `'` starts.
        var single = Single.none
        /// A string in single quotes runs over lines, and so one in double.
        var singleSpans = false
        var doubleSpans = false
        /// `"` quotes a name, not a string.
        var doubleNames = false
        var escapes = true
        var backtick = Backtick.none
        /// Three quotes open a string that runs over lines.
        var triple = false
        /// Letters before a quote are part of the string (`r"…"`, `f"…"`).
        var prefixes = false
        var interpolation = Interpolation.none
        /// `@name` is an annotation, anywhere or only where a line starts.
        var annotations = false
        var annotationsLead = false
        /// `#word` for these words is the language's own.
        var directives: Set<String> = []
        /// `#"…"#` strings.
        var hashStrings = false
        /// Lifetimes, raw strings and `#[…]` attributes.
        var rust = false
        /// `$` is a letter of a name.
        var dollar = false
        /// A `/` after one of `regexAfter`, or after a mark that no value
        /// ends with, starts a regular expression.
        var regex = false
        var regexAfter: Set<String> = []
        /// Keywords are read in any case.
        var foldsCase = false
        /// Keywords that are also names people give things (`type`, `get`,
        /// `open`): one before a mark that follows a name (`type: string`,
        /// `get(key)`) is a name.
        var contextual: Set<String> = []

        init(_ keywords: String) {
            let words = keywords.split(separator: " ").map(String.init)
            self.keywords = Set(words)
            longest = words.map(\.utf8.count).max() ?? 0
        }

        static let swift: Syntax = {
            var s = Syntax(
                "actor any as associatedtype async await borrowing break case catch class consuming continue convenience default defer deinit didSet do else enum extension fallthrough false fileprivate final for func get guard if import in indirect init inout internal is isolated lazy let macro mutating nil nonisolated nonmutating open operator override private protocol public repeat required rethrows return self Self set some static struct subscript super switch throw throws true try typealias unowned var weak where while willSet"
            )
            s.nestedComments = true
            s.triple = true
            s.backtick = .name
            s.interpolation = .parenthesis
            s.annotations = true
            s.hashStrings = true
            s.directives = [
                "if", "else", "elseif", "endif", "available", "unavailable", "selector", "keyPath", "warning", "error", "sourceLocation", "file", "fileID",
                "filePath", "line", "column", "function",
            ]
            s.contextual = ["get", "set", "willSet", "didSet", "some", "any", "open", "macro", "isolated", "consuming", "borrowing", "operator"]
            return s
        }()

        static let go: Syntax = {
            var s = Syntax(
                "break case chan const continue default defer else fallthrough false for func go goto if import interface iota map nil package range return select struct switch true type var"
            )
            s.single = .character
            s.backtick = .raw
            return s
        }()

        static let python: Syntax = {
            var s = Syntax(
                "False None True and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield"
            )
            s.lineComment = []
            s.hashComment = true
            s.blockComments = false
            s.single = .string
            s.triple = true
            s.prefixes = true
            s.annotations = true
            s.annotationsLead = true
            return s
        }()

        private static let script =
            "async await break case catch class const continue debugger default delete do else export extends false finally for from function if import in instanceof let new null of return static super switch this throw true try typeof undefined var void while with yield"

        static let javascript: Syntax = {
            var s = Syntax(script)
            s.single = .string
            s.backtick = .template
            s.dollar = true
            s.regex = true
            s.regexAfter = ["return", "typeof", "case", "do", "else", "in", "of", "void", "delete", "throw", "new", "yield", "await", "instanceof"]
            s.contextual = ["of", "from", "async", "static"]
            return s
        }()

        static let typescript: Syntax = {
            var s = Syntax(
                script
                    + " abstract as declare enum implements infer interface is keyof namespace never override private protected public readonly satisfies type unknown"
            )
            s.single = .string
            s.backtick = .template
            s.dollar = true
            s.regex = true
            s.regexAfter = javascript.regexAfter
            s.annotations = true
            s.contextual = javascript.contextual.union([
                "type", "as", "is", "declare", "namespace", "readonly", "abstract", "keyof", "infer", "satisfies", "override", "never", "unknown",
            ])
            return s
        }()

        static let rust: Syntax = {
            var s = Syntax(
                "as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while"
            )
            s.nestedComments = true
            s.single = .character
            s.doubleSpans = true
            s.rust = true
            return s
        }()

        static let kotlin: Syntax = {
            var s = Syntax(
                "abstract annotation as break by catch class companion const constructor continue crossinline data do else enum false final finally for fun if import in infix init inline interface internal is lateinit noinline null object open operator out override package private protected public reified return sealed super suspend tailrec this throw true try typealias val var vararg when where while"
            )
            s.nestedComments = true
            s.single = .character
            s.triple = true
            s.backtick = .name
            s.interpolation = .brace
            s.annotations = true
            s.contextual = ["data", "open", "out", "by", "init", "annotation", "companion", "constructor", "where", "operator", "infix", "inline"]
            return s
        }()

        static let sql: Syntax = {
            var s = Syntax(
                "add all alter and as asc begin between by cascade case check column commit constraint create cross default delete desc distinct drop else end except exists explain false foreign from full function grant group having if in index inner insert intersect into is join key left like limit natural not null offset on or order outer primary recursive references returning returns revoke right rollback select set table then transaction trigger true truncate union unique update using values view when where with"
            )
            s.lineComment = Array("--".utf8)
            s.single = .string
            s.singleSpans = true
            s.doubleNames = true
            s.escapes = false
            s.backtick = .name
            s.foldsCase = true
            return s
        }()
    }

    // MARK: The reader they share

    private static let quote = UInt8(ascii: "\"")
    private static let apostrophe = UInt8(ascii: "'")
    private static let tick = UInt8(ascii: "`")
    private static let backslash = UInt8(ascii: "\\")
    private static let hash = UInt8(ascii: "#")
    private static let slash = UInt8(ascii: "/")
    private static let star = UInt8(ascii: "*")
    private static let dollar = UInt8(ascii: "$")
    private static let dot = UInt8(ascii: ".")

    /// The marks a value never ends with: a `/` after one starts a regular
    /// expression and divides nothing. Not `<` or `>`: in JSX a tag closes
    /// with `</`, which would read as one.
    private static let beforeRegex = Set("(,=:[!&|?{;~+-*%".utf8)

    static func words(_ s: inout CodeScan, _ state: inout LexState, _ syntax: Syntax) {
        // What the lines before left open.
        switch state.mode {
        case .comment:
            guard let end = commentEnd(s, from: 0, depth: &state.depth, syntax) else {
                s.paint(0, s.end, .comment)
                return
            }
            s.paint(0, end, .comment)
            s.at = end
            state = LexState()
        case let .string(mark):
            // A raw string in backticks has no escapes: its first backtick
            // closes it.
            let close = mark == tick ? s.find([tick], from: 0).map { $0 + 1 } : stringEnd(s, from: 0, quote: mark, syntax, opened: true)
            guard let end = close else {
                s.paint(0, s.end, .string)
                return
            }
            s.paint(0, end, .string)
            s.at = end
            state = LexState()
        case let .triple(mark):
            guard let end = tripleEnd(s, from: 0, quote: mark, hashes: state.depth) else {
                s.paint(0, s.end, .string)
                return
            }
            s.paint(0, end, .string)
            s.at = end
            state = LexState()
        case .raw:
            guard let end = rawEnd(s, from: 0, hashes: state.depth) else {
                s.paint(0, s.end, .string)
                return
            }
            s.paint(0, end, .string)
            s.at = end
            state = LexState()
        case .template:
            guard let end = templateEnd(s, from: 0, depth: &state.depth) else {
                s.paint(0, s.end, .string)
                return
            }
            s.paint(0, end, .string)
            s.at = end
            state = LexState()
        default:
            state = LexState()
        }

        /// A `/` here would start a regular expression. Unknown where a
        /// line starts: there it is left plain.
        var regexMay = false
        /// The last byte that was not a space, before the reader's place.
        var before: UInt8?

        while s.at < s.end {
            let c = s.line[s.at]
            let from = s.at
            if c == 0x20 {
                s.at += 1
                continue
            }
            defer {
                if s.at > from { before = s.line[s.at - 1] }
            }

            if !syntax.lineComment.isEmpty, s.has(syntax.lineComment, at: from) {
                s.paint(from, s.end, .comment)
                s.at = s.end
                return
            }
            if syntax.hashComment, c == hash {
                s.paint(from, s.end, .comment)
                s.at = s.end
                return
            }
            if syntax.blockComments, c == slash, s.byte(from + 1) == star {
                state.depth = 1
                guard let end = commentEnd(s, from: from + 2, depth: &state.depth, syntax) else {
                    s.paint(from, s.end, .comment)
                    state.mode = .comment
                    s.at = s.end
                    return
                }
                s.paint(from, end, .comment)
                s.at = end
                continue
            }

            if c == quote || (c == apostrophe && syntax.single == .string) {
                if c == quote, syntax.doubleNames {
                    // A quoted name: read past it, so nothing in it reads
                    // as a comment or a string, and left plain.
                    s.at = s.find([quote], from: from + 1).map { $0 + 1 } ?? s.end
                    regexMay = false
                    continue
                }
                guard string(&s, &state, from: from, quote: c, syntax) else { return }
                regexMay = false
                continue
            }
            if c == apostrophe, syntax.single == .character {
                if let end = characterEnd(s, from: from) {
                    s.paint(from, end, .string)
                    s.at = end
                } else {
                    // A lifetime, or a mark this reader does not know: its
                    // name is no keyword.
                    s.at = from + 1
                    while s.at < s.end, s.line[s.at].inName { s.at += 1 }
                }
                regexMay = false
                continue
            }
            if c == tick, syntax.backtick != .none {
                switch syntax.backtick {
                case .raw:
                    guard let close = s.find([tick], from: from + 1) else {
                        s.paint(from, s.end, .string)
                        state.mode = .string(tick)
                        s.at = s.end
                        return
                    }
                    s.paint(from, close + 1, .string)
                    s.at = close + 1
                case .template:
                    state.depth = 0
                    guard let end = templateEnd(s, from: from + 1, depth: &state.depth) else {
                        s.paint(from, s.end, .string)
                        state.mode = .template
                        s.at = s.end
                        return
                    }
                    s.paint(from, end, .string)
                    s.at = end
                case .name, .none:
                    s.at = s.find([tick], from: from + 1).map { $0 + 1 } ?? s.end
                }
                regexMay = false
                continue
            }

            if c == UInt8(ascii: "@"), syntax.annotations, s.byte(from + 1)?.startsName == true,
                !syntax.annotationsLead || s.pastSpaces(0) == from
            {
                var end = from + 1
                while end < s.end, s.line[end].inName || (s.line[end] == dot && s.byte(end + 1)?.startsName == true) { end += 1 }
                s.paint(from, end, .meta)
                s.at = end
                regexMay = false
                continue
            }

            if c == hash {
                if syntax.hashStrings, let opened = hashString(s, from: from) {
                    if opened.triple {
                        guard let end = tripleEnd(s, from: opened.body, quote: quote, hashes: opened.hashes) else {
                            s.paint(from, s.end, .string)
                            state.mode = .triple(quote)
                            state.depth = opened.hashes
                            s.at = s.end
                            return
                        }
                        s.paint(from, end, .string)
                        s.at = end
                    } else if let end = rawEnd(s, from: opened.body, hashes: opened.hashes) {
                        s.paint(from, end, .string)
                        s.at = end
                    } else {
                        return
                    }
                    continue
                }
                if !syntax.directives.isEmpty, s.byte(from + 1)?.startsName == true {
                    var end = from + 1
                    while end < s.end, s.line[end].inName { end += 1 }
                    if syntax.directives.contains(s.text(from + 1, end)) { s.paint(from, end, .keyword) }
                    s.at = end
                    continue
                }
                if syntax.rust, let end = attributeEnd(s, from: from) {
                    s.paint(from, end, .meta)
                    s.at = end
                    continue
                }
                s.at = from + 1
                continue
            }

            if c.isDigit {
                let end = numberEnd(s, from: from)
                s.paint(from, end, .number)
                s.at = end
                regexMay = false
                continue
            }

            if c.startsName || (syntax.dollar && c == dollar) {
                var end = from + 1
                while end < s.end, s.line[end].inName || (syntax.dollar && s.line[end] == dollar) { end += 1 }
                let next = s.byte(end)
                if syntax.prefixes, end - from <= 2, next == quote || next == apostrophe,
                    ["r", "b", "f", "u", "rb", "br", "fr", "rf"].contains(s.text(from, end).lowercased())
                {
                    // The letters are the string's own.
                    guard string(&s, &state, from: end, quote: next ?? quote, syntax, paintFrom: from) else { return }
                    regexMay = false
                    continue
                }
                if syntax.rust, next == quote || next == hash || next == apostrophe {
                    let word = s.text(from, end)
                    if word == "r" || word == "br", let opened = rawString(s, from: end) {
                        guard let close = rawEnd(s, from: opened.body, hashes: opened.hashes) else {
                            s.paint(from, s.end, .string)
                            state.mode = .raw
                            state.depth = opened.hashes
                            s.at = s.end
                            return
                        }
                        s.paint(from, close, .string)
                        s.at = close
                        continue
                    }
                    if word == "b", next == quote {
                        guard string(&s, &state, from: end, quote: quote, syntax, paintFrom: from) else { return }
                        continue
                    }
                    if word == "b", next == apostrophe, let close = characterEnd(s, from: end) {
                        s.paint(from, close, .string)
                        s.at = close
                        continue
                    }
                    if word == "r", next == hash {
                        // A raw identifier (`r#type`): a name, whatever
                        // word it is.
                        s.at = end + 1
                        while s.at < s.end, s.line[s.at].inName { s.at += 1 }
                        continue
                    }
                }
                s.at = end
                // A member's name is no keyword, whatever it is called.
                let member = before == dot && !(from >= 2 && s.byte(from - 2) == dot)
                guard end - from <= syntax.longest, !member else {
                    regexMay = false
                    continue
                }
                var word = s.text(from, end)
                if syntax.foldsCase { word = word.lowercased() }
                if syntax.keywords.contains(word), !(syntax.contextual.contains(word) && named(s, after: end)) {
                    s.paint(from, end, .keyword)
                    regexMay = syntax.regexAfter.contains(word)
                } else {
                    regexMay = false
                }
                continue
            }

            if c == slash, syntax.regex, regexMay, let end = regexEnd(s, from: from) {
                s.paint(from, end, .string)
                s.at = end
                regexMay = false
                continue
            }

            s.at = from + 1
            regexMay = beforeRegex.contains(c)
        }
    }

    /// A word that ends at `after` is used as a name: what follows it is
    /// a mark that follows a name and never one of the language's words.
    private static func named(_ s: CodeScan, after: Int) -> Bool {
        guard let next = s.byte(s.pastSpaces(after)) else { return false }
        return ":=,);.?(]".utf8.contains(next)
    }

    /// Reads the string whose opening quote stands at `from`, painting it
    /// from `paintFrom` where letters before the quote are its own. False
    /// where the line ends inside it: either the string runs on, which the
    /// state then says, or it should have closed, and neither it nor the
    /// rest of the line is coloured.
    private static func string(
        _ s: inout CodeScan, _ state: inout LexState, from: Int, quote mark: UInt8, _ syntax: Syntax, paintFrom: Int? = nil
    ) -> Bool {
        let start = paintFrom ?? from
        if syntax.triple, s.byte(from + 1) == mark, s.byte(from + 2) == mark {
            guard let end = tripleEnd(s, from: from + 3, quote: mark, hashes: 0) else {
                s.paint(start, s.end, .string)
                state.mode = .triple(mark)
                state.depth = 0
                s.at = s.end
                return false
            }
            s.paint(start, end, .string)
            s.at = end
            return true
        }
        if let end = stringEnd(s, from: from, quote: mark, syntax, opened: false) {
            s.paint(start, end, .string)
            s.at = end
            return true
        }
        if (mark == quote && syntax.doubleSpans) || (mark == apostrophe && syntax.singleSpans) {
            s.paint(start, s.end, .string)
            state.mode = .string(mark)
        }
        s.at = s.end
        return false
    }

    /// The byte after the closing quote of a string: from its opening
    /// quote, or from inside it where a line before opened it.
    private static func stringEnd(_ s: CodeScan, from: Int, quote mark: UInt8, _ syntax: Syntax, opened: Bool) -> Int? {
        var i = opened ? from : from + 1
        while i < s.end {
            let c = s.line[i]
            if syntax.escapes, c == backslash {
                if syntax.interpolation == .parenthesis, s.byte(i + 1) == UInt8(ascii: "(") {
                    guard let end = groupEnd(s, from: i + 2, open: UInt8(ascii: "("), close: UInt8(ascii: ")")) else { return nil }
                    i = end
                    continue
                }
                i += 2
                continue
            }
            if syntax.interpolation == .brace, c == dollar, s.byte(i + 1) == UInt8(ascii: "{") {
                guard let end = groupEnd(s, from: i + 2, open: UInt8(ascii: "{"), close: UInt8(ascii: "}")) else { return nil }
                i = end
                continue
            }
            if c == mark { return i + 1 }
            i += 1
        }
        return nil
    }

    /// Past an interpolation opened before `from`, to the byte after the
    /// mark that closes it: a string inside it is read past whole.
    private static func groupEnd(_ s: CodeScan, from: Int, open: UInt8, close: UInt8) -> Int? {
        var depth = 1
        var i = from
        while i < s.end {
            let c = s.line[i]
            if c == open {
                depth += 1
            } else if c == close {
                depth -= 1
                if depth == 0 { return i + 1 }
            } else if c == quote {
                var j = i + 1
                while j < s.end, s.line[j] != quote { j += s.line[j] == backslash ? 2 : 1 }
                guard j < s.end else { return nil }
                i = j
            }
            i += 1
        }
        return nil
    }

    /// The byte after three of `mark` and `hashes` `#`s.
    private static func tripleEnd(_ s: CodeScan, from: Int, quote mark: UInt8, hashes: Int) -> Int? {
        var i = from
        while i + 2 < s.end {
            if hashes == 0, s.line[i] == backslash {
                i += 2
                continue
            }
            if s.line[i] == mark, s.line[i + 1] == mark, s.line[i + 2] == mark, (0 ..< hashes).allSatisfy({ s.byte(i + 3 + $0) == hash }) {
                return i + 3 + hashes
            }
            i += 1
        }
        return nil
    }

    /// The byte after a `"` followed by `hashes` `#`s.
    private static func rawEnd(_ s: CodeScan, from: Int, hashes: Int) -> Int? {
        var i = from
        while i < s.end {
            if s.line[i] == quote, (0 ..< hashes).allSatisfy({ s.byte(i + 1 + $0) == hash }) { return i + 1 + hashes }
            i += 1
        }
        return nil
    }

    /// A `#"…"#` string opening at `from`: how many `#`s, whether three
    /// quotes open it, and where its body starts. Nil where the `#`s lead
    /// to no quote.
    private static func hashString(_ s: CodeScan, from: Int) -> (hashes: Int, triple: Bool, body: Int)? {
        var i = from
        while s.byte(i) == hash { i += 1 }
        guard s.byte(i) == quote else { return nil }
        let triple = s.byte(i + 1) == quote && s.byte(i + 2) == quote
        return (i - from, triple, i + (triple ? 3 : 1))
    }

    /// A Rust raw string's `#`s and quote, from the byte after its `r`.
    private static func rawString(_ s: CodeScan, from: Int) -> (hashes: Int, body: Int)? {
        var i = from
        while s.byte(i) == hash { i += 1 }
        guard s.byte(i) == quote else { return nil }
        return (i - from, i + 1)
    }

    /// The byte after a block comment's end, from inside it. Where comments
    /// nest, `depth` says how deep the reader is and is left so.
    private static func commentEnd(_ s: CodeScan, from: Int, depth: inout Int, _ syntax: Syntax) -> Int? {
        var i = from
        if depth < 1 { depth = 1 }
        while i + 1 < s.end {
            if s.line[i] == star, s.line[i + 1] == slash {
                depth -= 1
                i += 2
                if depth == 0 { return i }
            } else if syntax.nestedComments, s.line[i] == slash, s.line[i + 1] == star {
                depth += 1
                i += 2
            } else {
                i += 1
            }
        }
        return nil
    }

    /// The byte after a template literal's closing backtick, from inside
    /// it. `depth` is how many braces of an interpolation are open, where a
    /// backtick belongs to a template inside it and closes nothing here.
    static func templateEnd(_ s: CodeScan, from: Int, depth: inout Int) -> Int? {
        var i = from
        while i < s.end {
            let c = s.line[i]
            if c == backslash {
                i += 2
                continue
            }
            if depth == 0 {
                if c == tick { return i + 1 }
                if c == dollar, s.byte(i + 1) == UInt8(ascii: "{") {
                    depth = 1
                    i += 2
                    continue
                }
            } else if c == UInt8(ascii: "{") {
                depth += 1
            } else if c == UInt8(ascii: "}") {
                depth -= 1
            }
            i += 1
        }
        return nil
    }

    /// The byte after a character literal that opens at `from`: `'a'`,
    /// `'\n'`, `'\u{1F600}'`. Nil where it is none (a Rust lifetime).
    private static func characterEnd(_ s: CodeScan, from: Int) -> Int? {
        guard let first = s.byte(from + 1), first != apostrophe else { return nil }
        if first == backslash {
            // An escape closes within a few bytes or is no character.
            var i = from + 3
            while i < s.end, i < from + 14 {
                if s.line[i] == apostrophe { return i + 1 }
                i += 1
            }
            return nil
        }
        // One character, of however many bytes.
        var i = from + 2
        while i < s.end, s.line[i] & 0xC0 == 0x80 { i += 1 }
        return s.byte(i) == apostrophe ? i + 1 : nil
    }

    /// The byte after a Rust attribute, `#[…]` or `#![…]`, closed on its
    /// line.
    private static func attributeEnd(_ s: CodeScan, from: Int) -> Int? {
        var i = from + 1
        if s.byte(i) == UInt8(ascii: "!") { i += 1 }
        guard s.byte(i) == UInt8(ascii: "[") else { return nil }
        var depth = 0
        while i < s.end {
            if s.line[i] == UInt8(ascii: "[") {
                depth += 1
            } else if s.line[i] == UInt8(ascii: "]") {
                depth -= 1
                if depth == 0 { return i + 1 }
            } else if s.line[i] == quote {
                var j = i + 1
                while j < s.end, s.line[j] != quote { j += s.line[j] == backslash ? 2 : 1 }
                i = j
            }
            i += 1
        }
        return nil
    }

    /// The byte after a number that starts at `from`: its digits, a
    /// fraction, an exponent with its sign, the letters of a base or a
    /// suffix. A range's dots (`1..<5`) and a member's (`1.description`)
    /// are not its own.
    static func numberEnd(_ s: CodeScan, from: Int) -> Int {
        let based = s.line[from] == 0x30 && [UInt8(ascii: "x"), UInt8(ascii: "X")].contains(s.byte(from + 1) ?? 0)
        var i = from + 1
        var fraction = false
        while i < s.end {
            let c = s.line[i]
            if c.isDigit || c.isLetter || c == 0x5F {
                i += 1
            } else if c == dot, !fraction, s.byte(i + 1)?.isDigit == true {
                fraction = true
                i += 1
            } else if c == UInt8(ascii: "+") || c == UInt8(ascii: "-"), !based, [UInt8(ascii: "e"), UInt8(ascii: "E")].contains(s.line[i - 1]),
                s.byte(i + 1)?.isDigit == true
            {
                i += 1
            } else {
                break
            }
        }
        return i
    }

    /// The byte after a regular expression that opens at `from`, closed on
    /// its line, with its flags. A `/` inside a class (`[/]`) closes none.
    private static func regexEnd(_ s: CodeScan, from: Int) -> Int? {
        guard let first = s.byte(from + 1), first != slash, first != star, first != 0x20, first != UInt8(ascii: "=") else { return nil }
        var i = from + 1
        var inClass = false
        while i < s.end {
            let c = s.line[i]
            if c == backslash {
                i += 2
                continue
            }
            if c == UInt8(ascii: "[") {
                inClass = true
            } else if c == UInt8(ascii: "]") {
                inClass = false
            } else if c == slash, !inClass {
                i += 1
                while i < s.end, s.line[i].isLetter { i += 1 }
                return i
            }
            i += 1
        }
        return nil
    }
}
