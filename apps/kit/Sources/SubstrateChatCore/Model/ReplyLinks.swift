import Foundation

/// One web page a reply links: the address, and the text of its first link.
struct ReplyLink: Hashable, Sendable {
    var url: String
    var text: String
}

/// The web pages a reply links, in the order it first links each, once each:
/// the sources a reader may want to open (the console's `replyLinks`,
/// web/console/src/lib/chat-text.ts). Code is never read for links, and a
/// record link is not a page.
func replyLinks(_ text: String) -> [ReplyLink] {
    var seen: Set<String> = []
    var out: [ReplyLink] = []
    func read(_ inline: String) {
        for link in webLinks(inline: inline) where seen.insert(link.url).inserted { out.append(link) }
    }
    func walk(_ blocks: [MarkdownBlock]) {
        for block in blocks {
            switch block {
            case let .paragraph(text), let .heading(_, text): read(text)
            case let .quote(inner): walk(inner)
            case let .list(_, _, items): items.forEach { walk($0.blocks) }
            case let .table(header, _, rows): (header + rows.joined()).forEach(read)
            case .code, .rule: break
            }
        }
    }
    walk(Markdown.parse(text))
    return out
}

/// The web links in one block's inline Markdown, in order: `[text](url)` and
/// `<url>`, never inside a code span and never an image. By rule rather than
/// through a Markdown reader, which Linux's and Android's Foundation do not
/// carry; a target is a link only when it is written `http://` or `https://`.
func webLinks(inline line: String) -> [ReplyLink] {
    let chars = Array(line)
    var out: [ReplyLink] = []
    var i = 0
    while i < chars.count {
        switch chars[i] {
        case "\\":
            i += 2
        case "`":
            i = codeSpanEnd(chars, from: i)
        case "<":
            guard let close = chars[(i + 1)...].firstIndex(where: { $0 == ">" || $0 == "<" || $0.isWhitespace }), chars[close] == ">" else {
                i += 1
                continue
            }
            let target = String(chars[(i + 1) ..< close])
            if let url = pageURL(target) { out.append(ReplyLink(url: url, text: target)) }
            i = close + 1
        case "[":
            guard let close = closingBracket(chars, from: i), close + 1 < chars.count, chars[close + 1] == "(",
                let (target, end) = linkTarget(chars, from: close + 2)
            else {
                i += 1
                continue
            }
            let image = i > 0 && chars[i - 1] == "!"
            if !image, let url = pageURL(target) {
                let label = unescaped(inlinePlain(String(chars[(i + 1) ..< close]))).trimmingCharacters(in: .whitespaces)
                out.append(ReplyLink(url: url, text: label.isEmpty ? target : label))
            }
            i = end
        default:
            i += 1
        }
    }
    return out
}

/// A link's target as a page address: written with an `http` or `https`
/// scheme, then held to `webURL`'s rules.
private func pageURL(_ target: String) -> String? {
    let lower = target.lowercased()
    guard lower.hasPrefix("http://") || lower.hasPrefix("https://") else { return nil }
    return webURL(target)
}

/// Past the code span opening at `start`, which the same run of backticks
/// closes; an unclosed run is only its backticks.
private func codeSpanEnd(_ chars: [Character], from start: Int) -> Int {
    var run = 0
    while start + run < chars.count, chars[start + run] == "`" { run += 1 }
    var i = start + run
    while i < chars.count {
        guard chars[i] == "`" else {
            i += 1
            continue
        }
        var closing = 0
        while i + closing < chars.count, chars[i + closing] == "`" { closing += 1 }
        if closing == run { return i + closing }
        i += closing
    }
    return start + run
}

/// The `]` that closes the `[` at `start`, brackets balanced and escapes
/// skipped.
private func closingBracket(_ chars: [Character], from start: Int) -> Int? {
    var depth = 0
    var i = start
    while i < chars.count {
        switch chars[i] {
        case "\\": i += 1
        case "[": depth += 1
        case "]":
            depth -= 1
            if depth == 0 { return i }
        default: break
        }
        i += 1
    }
    return nil
}

/// The target of a link whose `(` is just before `start`, with an optional
/// title, and the index past its `)`: `<target>`, or a target with its
/// parentheses balanced that ends at a space.
private func linkTarget(_ chars: [Character], from start: Int) -> (String, Int)? {
    var i = start
    func skipSpaces() { while i < chars.count, chars[i] == " " || chars[i] == "\t" { i += 1 } }
    skipSpaces()
    var target = ""
    if i < chars.count, chars[i] == "<" {
        guard let close = chars[(i + 1)...].firstIndex(of: ">") else { return nil }
        target = String(chars[(i + 1) ..< close])
        i = close + 1
    } else {
        var parens = 0
        let begin = i
        scan: while i < chars.count {
            switch chars[i] {
            case "\\": i += 1
            case " ", "\t": break scan
            case "(": parens += 1
            case ")":
                if parens == 0 { break scan }
                parens -= 1
            default: break
            }
            i += 1
        }
        target = String(chars[begin ..< min(i, chars.count)])
    }
    skipSpaces()
    let titles: [Character: Character] = ["\"": "\"", "'": "'", "(": ")"]
    if i < chars.count, let closing = titles[chars[i]] {
        guard let end = chars[(i + 1)...].firstIndex(of: closing) else { return nil }
        i = end + 1
        skipSpaces()
    }
    guard i < chars.count, chars[i] == ")" else { return nil }
    return (unescaped(target), i + 1)
}

/// A backslash before ASCII punctuation is the punctuation itself.
private func unescaped(_ text: String) -> String {
    guard text.contains("\\") else { return text }
    let punctuation = Set("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~")
    var out = ""
    var escaping = false
    for ch in text {
        if escaping {
            if !punctuation.contains(ch) { out.append("\\") }
            out.append(ch)
            escaping = false
        } else if ch == "\\" {
            escaping = true
        } else {
            out.append(ch)
        }
    }
    if escaping { out.append("\\") }
    return out
}
