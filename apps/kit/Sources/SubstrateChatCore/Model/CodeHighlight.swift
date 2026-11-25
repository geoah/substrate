import Foundation
import SubstrateRuntime

/// What a piece of code is coloured as. Few on purpose: each is a thing a
/// reader can name, and what the highlighter cannot name it leaves plain.
public enum CodeToken: String, Codable, Hashable, Sendable, CaseIterable {
    /// A word of the language: `func`, `SELECT`, a tag's name, an at-rule.
    case keyword
    case string
    case comment
    case number
    /// A name the code declares for a value: a JSON or YAML key, an HTML
    /// attribute, a CSS property, a shell variable.
    case name
    /// What is about the code and not of it: an annotation, a diff's file
    /// and hunk headers, a document's doctype.
    case meta
    /// A diff's added and removed lines. The line's own `+` or `-` says so
    /// too: the colour never carries it alone.
    case added
    case removed
}

/// One coloured run of a code block: where it starts in the block's text
/// and how long it is, in UTF-8 bytes. What lies between two runs is plain.
public struct CodeRun: Codable, Hashable, Sendable {
    public var start: Int
    public var length: Int
    public var token: CodeToken

    public init(start: Int, length: Int, token: CodeToken) {
        self.start = start
        self.length = length
        self.token = token
    }
}

/// A code block's colours, as values: made where the reply is parsed, so a
/// finished block is an equal value at every flush after it and a view
/// draws it without reading its text again.
public struct CodeHighlight: Codable, Hashable, Sendable {
    /// In order, never overlapping, each inside one line.
    public var runs: [CodeRun]
    /// The longest line, in columns of the fixed-width font: what says
    /// whether a line overflows the room it is drawn in (`CodeWrap`).
    public var columns: Int

    public init(runs: [CodeRun] = [], columns: Int = 0) {
        self.runs = runs
        self.columns = columns
    }
}

/// The languages the highlighter reads: the ones people paste. A fence that
/// names another, or none, is plain text.
public enum CodeLanguage: String, CaseIterable, Sendable {
    case swift, go, python, javascript, typescript, json, yaml, shell, sql, html, css, rust, kotlin, diff

    /// The language a fence's name says, in whatever case, with what some
    /// writers put after it (`swift:Main.swift`, `python,linenos`) left off.
    public init?(fence name: String?) {
        guard let name else { return nil }
        let word = name.lowercased().drop { $0 == "." || $0 == "{" }.prefix { $0.isLetter || $0.isNumber || $0 == "+" || $0 == "#" || $0 == "-" }
        guard let language = Self.names[String(word)] else { return nil }
        self = language
    }

    private static let names: [String: CodeLanguage] = [
        "swift": .swift,
        "go": .go, "golang": .go,
        "python": .python, "py": .python, "python3": .python,
        "javascript": .javascript, "js": .javascript, "jsx": .javascript, "mjs": .javascript, "cjs": .javascript, "node": .javascript,
        "typescript": .typescript, "ts": .typescript, "tsx": .typescript,
        "json": .json, "jsonc": .json, "json5": .json, "ndjson": .json, "jsonl": .json,
        "yaml": .yaml, "yml": .yaml,
        "bash": .shell, "sh": .shell, "shell": .shell, "zsh": .shell,
        "sql": .sql, "postgres": .sql, "postgresql": .sql, "psql": .sql, "plpgsql": .sql, "mysql": .sql, "sqlite": .sql,
        "html": .html, "xml": .html, "svg": .html, "xhtml": .html,
        "css": .css, "scss": .css, "less": .css,
        "rust": .rust, "rs": .rust,
        "kotlin": .kotlin, "kt": .kotlin, "kts": .kotlin,
        "diff": .diff, "patch": .diff,
    ]
}

public extension MeterPoint {
    /// One code block read for its colours.
    static let highlight = MeterPoint("highlight")
}

/// A small highlighter for the code in a reply: by the language its fence
/// names, a line at a time, each line from what the lines before it left
/// open (a comment, a string that spans lines). So a line that is finished
/// is coloured once and stays as it is, whatever is written after it, and a
/// block still being written costs its last line.
///
/// It would rather leave a thing plain than colour it wrongly: a string
/// that does not close on its line, where its language says it must, is not
/// coloured, nor is the rest of that line; a word is a keyword only where
/// the language has no other use for it.
public enum CodeHighlighter {
    /// A block's colours, read whole.
    public static func highlight(_ text: String, language: String?) -> CodeHighlight {
        var lines: [CodeMemo.Line] = []
        return highlight(text, language: language, kept: &lines)
    }

    /// A block's colours, with the lines read for it before in `kept`: a
    /// line that is as it was, after the same lines, is not read again.
    static func highlight(_ text: String, language name: String?, kept: inout [CodeMemo.Line]) -> CodeHighlight {
        Meter.shared.time(.highlight) {
            let language = CodeLanguage(fence: name)
            // SCSS and Less have `//` comments, which CSS reads as part of
            // an address.
            let loose = ["scss", "less"].contains(name?.lowercased() ?? "")
            let bytes = Array(text.utf8)
            var out = CodeHighlight()
            var state = LexState()
            var start = 0
            var index = 0
            while start <= bytes.count {
                let end = bytes[start...].firstIndex(of: 0x0A) ?? bytes.count
                let line = bytes[start ..< end]
                if index < kept.count, kept[index].before == state, kept[index].bytes.elementsEqual(line) {
                    state = kept[index].after
                } else {
                    var scan = CodeScan(line: Array(line))
                    let before = state
                    if let language { CodeLexers.lex(&scan, &state, language, loose: loose) }
                    let made = CodeMemo.Line(bytes: scan.line, before: before, after: state, runs: scan.runs, columns: columns(scan.line))
                    if index < kept.count { kept[index] = made } else { kept.append(made) }
                }
                for run in kept[index].runs { out.runs.append(CodeRun(start: start + run.start, length: run.length, token: run.token)) }
                out.columns = max(out.columns, kept[index].columns)
                index += 1
                start = end + 1
            }
            if kept.count > index { kept.removeSubrange(index...) }
            return out
        }
    }

    /// A line's width in columns of a fixed-width font: one for a
    /// character, two for the wide ones (CJK, emoji), none for a mark that
    /// joins the character before it.
    static func columns(_ line: [UInt8]) -> Int {
        var count = 0
        for scalar in String(decoding: line, as: UTF8.self).unicodeScalars {
            let value = scalar.value
            if value < 0x300 {
                count += 1
            } else if (0x300 ... 0x36F).contains(value) || value == 0x200D || (0xFE00 ... 0xFE0F).contains(value) {
                continue
            } else if (0x1100 ... 0x115F).contains(value) || (0x2E80 ... 0xA4CF).contains(value) || (0xAC00 ... 0xD7A3).contains(value)
                || (0xF900 ... 0xFAFF).contains(value) || (0xFE30 ... 0xFE4F).contains(value) || (0xFF00 ... 0xFF60).contains(value)
                || (0xFFE0 ... 0xFFE6).contains(value) || (0x1F300 ... 0x1FAFF).contains(value) || (0x20000 ... 0x3FFFD).contains(value)
            {
                count += 2
            } else {
                count += 1
            }
        }
        return count
    }
}

/// The lines read for the code blocks of one reply, kept between two
/// parses of it while it is written: a reply is parsed whole at each flush,
/// and its code is coloured from here, so a block's finished lines are read
/// once. A reference the presenter keeps beside the reply's rows; what it
/// answers is what reading the block whole would.
final class CodeMemo {
    struct Line {
        var bytes: [UInt8]
        var before: LexState
        var after: LexState
        /// From the line's own start.
        var runs: [CodeRun]
        var columns: Int
    }

    /// By a block's row in the reply and its place among that row's code
    /// blocks.
    struct Slot: Hashable {
        var row: Int
        var block: Int
    }

    var lines: [Slot: [Line]] = [:]
}

/// What a line leaves open for the next: read with the line's bytes, it is
/// all a line's colours depend on.
struct LexState: Hashable, Sendable {
    enum Mode: Hashable, Sendable {
        case code
        /// A block comment, `depth` deep where they nest.
        case comment
        /// A string that spans lines, closed by this byte.
        case string(UInt8)
        /// One closed by three of this byte, then `depth` `#`s.
        case triple(UInt8)
        /// A raw string closed by `"` and `depth` `#`s.
        case raw
        /// A template literal, `depth` braces into an interpolation.
        case template
        /// A shell's here-document, up to the line that is `word`.
        case heredoc
        /// A YAML block scalar: the lines indented past `depth`.
        case scalar
        /// Inside an HTML tag named `word`.
        case tag
        /// An HTML comment.
        case markupComment
        /// The body of an HTML `script` or `style`, up to its closing tag.
        case rawText
    }

    var mode = Mode.code
    var depth = 0
    /// A second count: a CSS declaration's value is open; a diff's lines
    /// still to come of the new file.
    var extra = 0
    var word = ""
}

/// One line being read: its bytes, where the reader is, and the runs made.
struct CodeScan {
    let line: [UInt8]
    var at = 0
    var runs: [CodeRun] = []

    init(line: [UInt8]) {
        self.line = line
    }

    var end: Int { line.count }

    func byte(_ index: Int) -> UInt8? { index >= 0 && index < line.count ? line[index] : nil }

    func has(_ word: [UInt8], at index: Int) -> Bool {
        guard index >= 0, index + word.count <= line.count else { return false }
        for offset in word.indices where line[index + offset] != word[offset] { return false }
        return true
    }

    /// The first place `word` stands at or after `index`.
    func find(_ word: [UInt8], from index: Int) -> Int? {
        guard !word.isEmpty, index <= line.count - word.count else { return nil }
        var i = max(index, 0)
        while i <= line.count - word.count {
            if has(word, at: i) { return i }
            i += 1
        }
        return nil
    }

    func text(_ from: Int, _ to: Int) -> String { String(decoding: line[from ..< to], as: UTF8.self) }

    mutating func paint(_ from: Int, _ to: Int, _ token: CodeToken) {
        guard to > from else { return }
        // Two runs of one token that touch are one run.
        if let last = runs.indices.last, runs[last].token == token, runs[last].start + runs[last].length == from {
            runs[last].length += to - from
        } else {
            runs.append(CodeRun(start: from, length: to - from, token: token))
        }
    }

    /// Where the spaces from `index` end.
    func pastSpaces(_ index: Int) -> Int {
        var i = index
        while i < line.count, line[i] == 0x20 { i += 1 }
        return i
    }
}

extension UInt8 {
    var isDigit: Bool { self >= 0x30 && self <= 0x39 }
    var isLetter: Bool { (self >= 0x41 && self <= 0x5A) || (self >= 0x61 && self <= 0x7A) }
    /// A byte a name starts with: a letter, `_`, or part of a character
    /// past ASCII.
    var startsName: Bool { isLetter || self == 0x5F || self >= 0x80 }
    var inName: Bool { startsName || isDigit }
}

public extension MarkdownBlock {
    /// The code blocks in this block, a list's and a quote's included, in
    /// the order they stand: the order their colours are kept in
    /// (`MarkdownRow.code`).
    var codeBlocks: [(language: String?, text: String)] {
        switch self {
        case let .code(language, text, _): [(language, text)]
        case let .quote(blocks): blocks.flatMap(\.codeBlocks)
        case let .list(_, _, items): items.flatMap { $0.blocks.flatMap(\.codeBlocks) }
        case .paragraph, .heading, .table, .rule: []
        }
    }

    /// How many code blocks stand in this block.
    var codeCount: Int {
        switch self {
        case .code: 1
        case let .quote(blocks): blocks.reduce(0) { $0 + $1.codeCount }
        case let .list(_, _, items): items.reduce(0) { $0 + $1.blocks.reduce(0) { $0 + $1.codeCount } }
        case .paragraph, .heading, .table, .rule: 0
        }
    }
}
