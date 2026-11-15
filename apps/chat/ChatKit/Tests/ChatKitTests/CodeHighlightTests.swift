import Foundation
import SubstrateRuntime
import Testing

@testable import ChatKit

/// The highlighter: what it colours in a sample of each language it reads,
/// what it leaves plain, and that a block being written is coloured a line
/// at a time, each finished line once and for good.
@Suite("Code is coloured by its fence's language", .timeLimit(.minutes(10)))
struct CodeHighlightTests {
    /// What a block's runs colour, by token, in order.
    static func painted(_ code: String, _ language: String?) -> [CodeToken: [String]] {
        let bytes = Array(code.utf8)
        var out: [CodeToken: [String]] = [:]
        for run in CodeHighlighter.highlight(code, language: language).runs {
            out[run.token, default: []].append(String(decoding: bytes[run.start ..< run.start + run.length], as: UTF8.self))
        }
        return out
    }

    /// Every sample's colours, read by eye once and held: a word that
    /// becomes a keyword, or a string that stops being one, fails here.
    static let expected: [String: [CodeToken: [String]]] = [
        "swift": [
            .keyword: [
                ##"import"##, ##"struct"##, ##"private"##, ##"var"##, ##"let"##, ##"let"##, ##"func"##, ##"async"##, ##"throws"##, ##"#if"##, ##"#endif"##,
                ##"for"##, ##"in"##, ##"where"##, ##"return"##, ##"var"##,
            ],
            .string: [
                ##""Count: \(count + ["a": 1]["a"]!)""##, ##"#"A "quoted" word"#"##, ##"""""##, ##"            several "lines""##, ##"            """"##,
            ],
            .comment: [##"/// A counter, kept on the main actor."##, ##"/* nested /* twice */ still */"##, ##"// as it stands"##],
            .number: [##"0"##, ##"0x1F"##, ##"1"##, ##"5"##, ##"2"##],
            .meta: [##"@MainActor"##],
        ],
        "go": [
            .keyword: [##"package"##, ##"import"##, ##"func"##, ##"for"##, ##"range"##, ##"return"##, ##"nil"##],
            .string: [##""fmt""##, ##"`SELECT *"##, ##"        FROM people WHERE name = 'x'`"##, ##""%d: %s\n""##, ##"'a'"##],
            .comment: [##"// Greet says hello."##, ##"/* a rune */"##],
            .number: [##"0x10"##],
        ],
        "python": [
            .keyword: [##"from"##, ##"import"##, ##"from"##, ##"import"##, ##"class"##, ##"def"##, ##"return"##, ##"if"##, ##"is"##, ##"None"##, ##"else"##],
            .string: [##""""A point"##, ##"    on a plane.""""##, ##"r"\d+'s""##, ##"f'{self.x}'"##],
            .comment: [##"# its length"##],
            .number: [##"1.5e-3"##, ##"0"##, ##"2"##, ##"2"##, ##"0.5"##, ##"0"##, ##"1"##, ##"2"##, ##"2"##],
            .meta: [##"@dataclass"##],
        ],
        "javascript": [
            .keyword: [
                ##"import"##, ##"from"##, ##"export"##, ##"async"##, ##"function"##, ##"true"##, ##"const"##, ##"await"##, ##"const"##, ##"const"##,
                ##"return"##, ##"const"##,
            ],
            .string: [
                ##""node:fs/promises""##, ##"'utf8'"##, ##"/\s+["']?/g"##, ##"`found ${words.length} words in ${path.replace(/`/g, "")}"##,
                ##"    over ${text.split("\n").length} lines`"##, ##""x""##,
            ],
            .comment: [##"/* Reads a file"##, ##"   and counts its words. */"##, ##"// done"##],
            .number: [##"2"##, ##"1"##],
        ],
        "typescript": [
            .keyword: [
                ##"interface"##, ##"type"##, ##"readonly"##, ##"export"##, ##"class"##, ##"implements"##, ##"private"##, ##"private"##, ##"readonly"##,
                ##"const"##, ##"return"##, ##"this"##, ##"as"##,
            ],
            .string: [##""the start""##],
            .meta: [##"@Injectable"##],
        ],
        "json": [
            .keyword: [##"true"##, ##"null"##],
            .string: [##""substrate""##, ##""a""##, ##""b: c""##, ##""https://example.com/a?b=1""##],
            .comment: [##"// where it lives"##],
            .number: [##"3"##, ##"-1.5e3"##],
            .name: [##""name""##, ##""version""##, ##""ratio""##, ##""tags""##, ##""nested""##, ##""ok""##, ##""none""##, ##""url""##],
        ],
        "yaml": [
            .keyword: [##"true"##],
            .string: [##""release/*""##, ##"'it''s'"##],
            .comment: [##"# A workflow"##, ##"# not a key"##],
            .number: [##"3"##],
            .name: [
                ##"name"##, ##"on"##, ##"push"##, ##"branches"##, ##"env"##, ##"RETRIES"##, ##"VERBOSE"##, ##"VERSION"##, ##"URL"##, ##"jobs"##, ##"name"##,
                ##"run"##, ##"at"##, ##"anchors"##, ##"image"##, ##"use"##,
            ],
            .meta: [##"---"##, ##"&base"##, ##"*base"##],
        ],
        "bash": [
            .keyword: [##"for"##, ##"in"##, ##"do"##, ##"if"##, ##"then"##, ##"fi"##, ##"done"##, ##"export"##],
            .string: [
                ##""${1:-world}""##, ##""$file""##, ##"'done'"##, ##""$file""##, ##""Hello, $NAME: $file is done""##, ##"'several"##, ##"lines'"##,
                ##""$HOME/bin:$PATH""##,
            ],
            .comment: [##"#!/usr/bin/env bash"##, ##"# who to greet"##],
            .name: [##"$HOME"##, ##"${USER}"##],
        ],
        "sql": [
            .keyword: [
                ##"SELECT"##, ##"AS"##, ##"FROM"##, ##"LEFT"##, ##"JOIN"##, ##"ON"##, ##"WHERE"##, ##"AND"##, ##"LIKE"##, ##"AND"##, ##"IS"##, ##"NOT"##,
                ##"NULL"##, ##"GROUP"##, ##"BY"##, ##"ORDER"##, ##"BY"##, ##"DESC"##, ##"LIMIT"##,
            ],
            .string: [##"'2026-11-20'"##, ##"'it''s %'"##],
            .comment: [##"-- Tasks due this week"##, ##"/* open"##, ##"     ones */"##],
            .number: [##"10"##],
        ],
        "html": [
            .keyword: [
                ##"html"##, ##"head"##, ##"style"##, ##"style"##, ##"script"##, ##"script"##, ##"head"##, ##"body"##, ##"p"##, ##"p"##, ##"br"##, ##"body"##,
                ##"html"##,
            ],
            .string: [##""en""##, ##""page""##, ##"'7'"##],
            .comment: [##"<!-- the page's"##, ##"         head -->"##],
            .name: [##"lang"##, ##"class"##, ##"data-id"##],
            .meta: [##"<!DOCTYPE html>"##],
        ],
        "css": [
            .keyword: [##"@import"##, ##"@media"##],
            .string: [##""base.css""##, ##""img/a.png""##],
            .comment: [##"/* The page */"##],
            .number: [##"#6A3FE0"##, ##"-4px"##, ##"0"##, ##"1.5rem"##, ##"200%"##],
            .name: [##"--accent"##, ##"color"##, ##"margin"##, ##"background"##, ##"font-size"##],
        ],
        "rust": [
            .keyword: [
                ##"use"##, ##"pub"##, ##"struct"##, ##"impl"##, ##"pub"##, ##"fn"##, ##"mut"##, ##"self"##, ##"let"##, ##"for"##, ##"in"##, ##"self"##,
                ##"let"##, ##"let"##, ##"self"##,
            ],
            .string: [##"r#"a "raw""##, ##"            string"#"##, ##"' '"##, ##""several"##, ##"            lines""##, ##"b'x'"##],
            .comment: [##"/// Counts the words."##, ##"/* one /* more */ */"##],
            .number: [##"0"##, ##"1"##, ##"0xFF_usize"##],
            .meta: [##"#[derive(Debug, Clone)]"##],
        ],
        "kotlin": [
            .keyword: [
                ##"package"##, ##"data"##, ##"class"##, ##"val"##, ##"private"##, ##"val"##, ##"suspend"##, ##"fun"##, ##"val"##, ##"return"##,
                ##"val"##,
            ],
            .string: [
                ##""unused""##, ##"""""##, ##"            Hello, "$name""##, ##"        """"##, ##""${text.map { "$it" }.joinToString("")}: ${times}x""##,
                ##"'G'"##,
            ],
            .comment: [##"/** A greeter. */"##, ##"// done"##],
            .number: [##"3"##, ##"1_000L"##],
            .meta: [##"@Suppress"##],
        ],
        "diff": [
            .meta: [
                ##"diff --git a/notes.md b/notes.md"##, ##"index 83b5ef3..f14b06e 100644"##, ##"--- a/notes.md"##, ##"+++ b/notes.md"##, ##"@@ -1,5 +1,5 @@"##,
                ##"\ No newline at end of file"##,
            ],
            .added: [##"+++ an added line that starts with pluses"##, ##"+new"##],
            .removed: [##"--- a removed line that starts with dashes"##, ##"-old"##],
        ],
    ]

    @Test(arguments: CodeFixtures.all.map(\.language))
    func eachSampleIsColouredAsItWasRead(_ language: String) throws {
        let code = try #require(CodeFixtures.all.first { $0.language == language }?.code)
        let painted = Self.painted(code, language)
        let expected = try #require(Self.expected[language])
        for token in CodeToken.allCases {
            #expect(painted[token] ?? [] == expected[token] ?? [], "\(language), \(token.rawValue)")
        }
    }

    @Test func whatALanguageHasAnotherUseForIsLeftPlain() {
        // A member, a label and an argument called by a keyword's name.
        #expect(Self.painted("let x = value.default + other.self.type", "swift")[.keyword] == ["let"])
        #expect(Self.painted("const o = { type: 1, from: 2, of: [3] }; o.default.new", "ts")[.keyword] == ["const"])
        #expect(Self.painted("store.get(key); cache.set(key, value); file.open()", "swift")[.keyword] == nil)
        // A soft keyword Python reads as a name.
        #expect(Self.painted("match = re.match(pattern, text)", "python")[.keyword] == nil)
        // A Rust lifetime is no character, and its name no keyword.
        #expect(Self.painted("fn f<'static>(x: &'static str) {}", "rust") == [.keyword: ["fn"]])
        // A shell's words are its own only where a command stands.
        #expect(Self.painted("echo if then done", "sh")[.keyword] == nil)
        #expect(Self.painted("rm -rf build # for good", "sh") == [.comment: ["# for good"]])
        #expect(Self.painted("echo a#b $# ${#arr[@]}", "sh")[.comment] == nil)
        // A division is no regular expression, and a closing tag is none.
        #expect(Self.painted("const x = a / b / c;", "js")[.string] == nil)
        #expect(Self.painted("const el = <p>a</p>, <p>b</p>;", "jsx")[.string] == nil)
        #expect(Self.painted("if (/^a\\/b$/.test(s)) {}", "js")[.string] == ["/^a\\/b$/"])
        // YAML: a time, an address and a version are not keys or numbers.
        #expect(Self.painted("at: 12:30\nurl: http://example.com\nv: 1.2.3\nn: 3.12", "yaml") == [.name: ["at", "url", "v", "n"], .number: ["3.12"]])
        // CSS: a selector that looks like a declaration is none.
        #expect(Self.painted("a:hover { color: red }\n.b { &:focus { top: 0 } }", "scss") == [.name: ["color", "top"], .number: ["0"]])
        #expect(Self.painted("a { background: url(http://example.com/a.png) }", "css")[.comment] == nil)
        #expect(Self.painted("// a note\na { top: 0 }", "scss")[.comment] == ["// a note"])
    }

    @Test func aStringThatShouldHaveClosedOnItsLineIsNotColouredNorTheRestOfIt() {
        // Half typed, or not what this reader takes it for: plain, and so
        // is what follows, where a keyword may be the string's own word.
        #expect(Self.painted("let a = \"for the win", "swift") == [.keyword: ["let"]])
        #expect(Self.painted("const s = 'it is for now", "js") == [.keyword: ["const"]])
        #expect(Self.painted("{\"name\": \"half", "json") == [.name: ["\"name\""]])
        // The next line starts clean.
        #expect(Self.painted("let a = \"half\nlet b = 1", "swift") == [.keyword: ["let", "let"], .number: ["1"]])
        // One that may run over lines is coloured from where it opens.
        #expect(Self.painted("x = \"\"\"half", "python")[.string] == ["\"\"\"half"])
        #expect(Self.painted("echo 'half", "sh")[.string] == ["'half"])
        #expect(Self.painted("/* half", "go")[.comment] == ["/* half"])
        #expect(Self.painted("<a href=\"half", "html") == [.keyword: ["a"], .name: ["href"], .string: ["\"half"]])
    }

    @Test func whatRunsOverLinesClosesWhereItShouldAndTheLinesAfterItAreCode() {
        // A brace in a string inside a template's interpolation opens
        // nothing: the template ends at its backtick.
        let template = Self.painted("const s = `${\"{\"}`;\nconst answer = 42;", "js")
        #expect(template[.string] == ["`${\"{\"}`"])
        #expect(template[.keyword] == ["const", "const"] && template[.number] == ["42"])
        let nested = Self.painted("const s = `a ${ok ? `}` : '{'} b`;\nlet n = 1;", "ts")
        #expect(nested[.string] == ["`a ${ok ? `}` : '{'} b`"])
        #expect(nested[.keyword] == ["const", "let"])
        // A character that is a brace, in Kotlin's.
        #expect(Self.painted("val s = \"${if (c == '}') 1 else 2}\" + x", "kotlin")[.string] == ["\"${if (c == '}') 1 else 2}\""])
        // A here-document whose word is quoted: its body is text, and the
        // commands after it are commands.
        for word in ["'EOF'", "\"EOF\"", "EOF", "-EOF"] {
            let painted = Self.painted("cat <<\(word)\nif it's \"text\"\nEOF\nexport PATH=/tmp # set", "bash")
            #expect(painted == [.keyword: ["export"], .comment: ["# set"]], "\(word)")
        }
        // An attribute's value over two lines is one string, and the tags
        // after it are tags.
        let html = Self.painted("<div title=\"hello\nworld\" id=x>\n<p>Text</p>\n</div>", "html")
        #expect(html[.string] == ["\"hello", "world\""])
        #expect(html[.keyword] == ["div", "p", "p", "div"] && html[.name] == ["title", "id"])
    }

    @Test func aDiffIsColouredByItsLinesFirstCharacterAndItsHunksCounts() {
        // With no hunk header at all, as people write a change by hand.
        #expect(Self.painted("- old\n+ new\n same", "diff") == [.removed: ["- old"], .added: ["+ new"]])
        #expect(CodeLexers.diffCounts("@@ -3,7 +3,9 @@") == (7, 9))
        #expect(CodeLexers.diffCounts("@@ -1 +1 @@") == (1, 1))
        // Past its hunk a line of dashes is a file's header again.
        let painted = Self.painted("@@ -1 +1 @@\n-a\n+b\n--- a/next\n+++ b/next", "patch")
        #expect(painted[.meta] == ["@@ -1 +1 @@", "--- a/next", "+++ b/next"])
        #expect(painted[.removed] == ["-a"] && painted[.added] == ["+b"])
    }

    @Test func aFenceNamesItsLanguageInAnyCaseAndAnyOtherIsPlain() {
        #expect(CodeLanguage(fence: "Swift") == .swift && CodeLanguage(fence: "TSX") == .typescript && CodeLanguage(fence: "yml") == .yaml)
        #expect(CodeLanguage(fence: "{.python}") == .python && CodeLanguage(fence: "swift:Main.swift") == .swift)
        #expect(CodeLanguage(fence: "golang") == .go && CodeLanguage(fence: "zsh") == .shell && CodeLanguage(fence: "postgresql") == .sql)
        #expect(CodeLanguage(fence: nil) == nil && CodeLanguage(fence: "") == nil && CodeLanguage(fence: "text") == nil)
        #expect(CodeLanguage(fence: "c++") == nil && CodeLanguage(fence: "java") == nil)
        // Every language answers to its own name.
        for language in CodeLanguage.allCases { #expect(CodeLanguage(fence: language.rawValue) == language) }
        // No name and an unknown one: no colour, whatever the text.
        for (_, code) in CodeFixtures.all {
            #expect(CodeHighlighter.highlight(code, language: nil).runs.isEmpty)
            #expect(CodeHighlighter.highlight(code, language: "plaintext").runs.isEmpty)
        }
    }

    @Test func theLongestLineIsCountedInColumns() {
        #expect(CodeHighlighter.highlight("a\nabcd\nab", language: nil).columns == 4)
        #expect(CodeHighlighter.highlight("", language: "swift").columns == 0)
        // A wide character is two columns, a joining mark none.
        #expect(CodeHighlighter.highlight("let 名前 = 1", language: "swift").columns == 12)
        #expect(CodeHighlighter.highlight("e\u{301}", language: nil).columns == 1)
        #expect(CodeHighlighter.highlight("// 👍 ok", language: "go").columns == 8)
    }

    /// A block's runs are well made: in order, never overlapping, each
    /// inside one line, and none cutting a character in two.
    static func wellMade(_ code: String, _ made: CodeHighlight) -> String? {
        let bytes = Array(code.utf8)
        var end = 0
        for run in made.runs {
            guard run.length > 0, run.start >= end, run.start + run.length <= bytes.count else { return "the run \(run) is out of place" }
            if bytes[run.start ..< run.start + run.length].contains(0x0A) { return "the run \(run) crosses a line" }
            if bytes[run.start] & 0xC0 == 0x80 { return "the run \(run) starts inside a character" }
            if run.start + run.length < bytes.count, bytes[run.start + run.length] & 0xC0 == 0x80 { return "the run \(run) ends inside a character" }
            end = run.start + run.length
        }
        return nil
    }

    /// The runs that lie in the first `lines` lines of a block.
    static func runs(_ made: CodeHighlight, inFirst lines: Int, of code: String) -> [CodeRun] {
        var end = 0
        var seen = 0
        for byte in code.utf8 {
            if seen == lines { break }
            end += 1
            if byte == 0x0A { seen += 1 }
        }
        return made.runs.filter { $0.start + $0.length <= end }
    }

    @Test(arguments: CodeFixtures.all.map(\.language))
    func atEveryPrefixABlockBeingWrittenIsColouredAsItsFinishedLinesWillBe(_ language: String) throws {
        let code = try #require(CodeFixtures.all.first { $0.language == language }?.code)
        let whole = CodeHighlighter.highlight(code, language: language)
        #expect(Self.wellMade(code, whole) == nil)
        var kept: [CodeMemo.Line] = []
        var prefix = ""
        var finished = 0
        for character in code {
            prefix.append(character)
            if character == "\n" { finished += 1 }
            // Coloured from the lines kept of the prefix before, it is what
            // reading the prefix whole makes.
            let made = CodeHighlighter.highlight(prefix, language: language, kept: &kept)
            let fresh = CodeHighlighter.highlight(prefix, language: language)
            #expect(made == fresh, "\(language), after \(prefix.count) characters")
            #expect(Self.wellMade(prefix, made) == nil, "\(language), after \(prefix.count) characters")
            // And every line that is finished is coloured as it stays: what
            // is written after it changes nothing of it.
            let now = Self.runs(made, inFirst: finished, of: prefix)
            let later = Self.runs(whole, inFirst: finished, of: code)
            #expect(now == later, "\(language), after \(prefix.count) characters a finished line is coloured as it will not stay")
            if made != fresh || now != later { return }
        }
        #expect(CodeHighlighter.highlight(prefix, language: language, kept: &kept) == whole)
    }

    @Test func aBlockBeingWrittenIsReadALineAtATimeAndEachFinishedLineOnce() {
        // Each line as it is read, by the meter: a block of 400 lines,
        // written 7 characters at a time, is not read 400 lines at a flush.
        let code = (0 ..< 400).map { "let value\($0) = compute(\($0), \"text\") // line \($0)" }.joined(separator: "\n")
        var kept: [CodeMemo.Line] = []
        var read = 0
        var flushes = 0
        var end = code.startIndex
        while end < code.endIndex {
            end = code.index(end, offsetBy: 7, limitedBy: code.endIndex) ?? code.endIndex
            let before = kept
            _ = CodeHighlighter.highlight(String(code[..<end]), language: "swift", kept: &kept)
            // The lines made anew at this flush.
            read += zip(kept, before + Array(repeating: nil, count: max(0, kept.count - before.count))).count { now, was in
                was.map { $0.bytes != now.bytes || $0.before != now.before } ?? true
            }
            flushes += 1
        }
        // The open line at each flush, and each line once more when it
        // ends: never the block again.
        #expect(read <= flushes + 400, "\(read) lines read in \(flushes) flushes")
        #expect(kept.count == 400)
    }

    @Test func aReplysCodeIsColouredWhereItsRowsAreMadeNestedBlocksToo() {
        let reply = """
            Two ways:

            ```swift
            let a = 1
            ```

            - In a list:

              ```python
              def f(): return None
              ```

            > ```
            > plain text
            > ```

            ```nolang
            for x in y
            """
        let rows = Markdown.rows(reply, open: false)
        #expect(rows.map(\.code.count) == [0, 1, 1, 1, 1])
        #expect(rows.map(\.block.codeCount) == rows.map(\.code.count))
        #expect(rows[1].code[0].runs.map(\.token) == [.keyword, .number])
        #expect(rows[2].code[0].runs.map(\.token) == [.keyword, .keyword, .keyword])
        #expect(rows[3].code[0] == CodeHighlight(runs: [], columns: 10))
        #expect(rows[4].code[0].runs.isEmpty)
        // A row with no code holds none, and a row's id is its block's
        // alone: the colours follow from it.
        #expect(rows[0].code.isEmpty && rows[1].id == MarkdownRow(position: 1, block: rows[1].block).id)
    }

    @Test(arguments: [1, 9, 40])
    func aReplyBeingWrittenIsColouredFromItsLastParseAsAOneShotParseColoursIt(_ size: Int) {
        // The reply's own fence, another after it, and code inside a list,
        // so a block's row moves as the reply grows.
        let reply =
            ChatFixtures.reply + "\n\n```go\n" + CodeFixtures.go + "\n```\n\n1. A step:\n\n   ```sh\n   echo 'two\n   lines' # said\n   ```\n\nDone.\n"
        let memo = CodeMemo()
        for prefix in MarkdownRowsTests.prefixes(reply, size: size) {
            let made = Markdown.rows(prefix, open: true, memo: memo)
            #expect(made == Markdown.rows(prefix, open: true), "after \(prefix.count) characters")
            if made != Markdown.rows(prefix, open: true) { return }
        }
        #expect(Markdown.rows(reply, open: false, memo: memo) == Markdown.rows(reply, open: false))
        // What is kept is of the rows that hold code, and no more.
        #expect(Set(memo.lines.keys.map(\.row)).count == 3)
    }

    @Test func theTurnBeingWrittenKeepsItsCodesLinesBesideItsRowsAndForgetsThemWithThem() {
        let parts = PresenterParts(verifies: true)
        var turn = Turn(id: "live-a1", role: .assistant, content: "```swift\nlet a = 1\nlet b")
        func present(live: Bool) -> AssistantTurn? {
            guard case let .assistant(reply) = TurnPresenter.present(turn, agent: nil, live: live, decided: [], titles: [:], kept: parts, scope: "chat/k").body
            else { return nil }
            return reply
        }
        #expect(present(live: true)?.blocks.rows.first?.code.first?.runs.map(\.token) == [.keyword, .number, .keyword])
        let memo = parts.state("chat/k/blocks/live-a1", as: CodeMemo.self)
        #expect(memo?.lines.values.first?.count == 2)
        turn.content += " = 2\n```\n"
        let ended = present(live: false)
        #expect(ended?.blocks.rows.first?.code.first?.runs.map(\.token) == [.keyword, .number, .keyword, .number])
        // The same reference, kept under the rows' name: forgotten with them.
        #expect(parts.state("chat/k/blocks/live-a1", as: CodeMemo.self) === memo)
        parts.keep(under: "chat/k/blocks/") { _ in false }
        #expect(parts.state("chat/k/blocks/live-a1", as: CodeMemo.self) == nil)
    }
}
