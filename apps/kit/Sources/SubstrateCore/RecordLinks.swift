import Foundation

public extension RecordReference {
    /// The record a link names: `substrate://<kind>/<id>` (decision 0137),
    /// the legacy `ref:<kind>/<id>`, or a record path with no scheme at all
    /// (`~/trips/trip/<id>`, `substrate.reamde.dev/core/contact/<id>`), which
    /// a model writes as often as the scheme it was asked for. Nil for any
    /// other link, a root-relative console path (`/applets/books`) among them.
    init?(link url: URL) {
        let raw = url.absoluteString
        for scheme in ["substrate://", "ref:"] where raw.lowercased().hasPrefix(scheme) {
            let path = String(raw.dropFirst(scheme.count))
            self.init(path: path.removingPercentEncoding ?? path)
            return
        }
        guard url.scheme == nil, url.host == nil else { return nil }
        let path = (url.relativeString.removingPercentEncoding ?? url.relativeString)
        guard RecordPaths.isWhole(path) else { return nil }
        self.init(path: path)
    }
}

/// Record paths written into prose: `~/trips/trip/01m7…` where a link to the
/// record was meant. The grammar is the console's (`chat-text.ts`): an
/// authority that is `~` or carries a dot, a package and a name that are
/// lowercase words, and an id.
public enum RecordPaths {
    private static let grammar =
        #"(~|[a-z0-9-]+(?:\.[a-z0-9-]+)+)/([a-z][a-z0-9]*)/([a-z][a-z0-9]*)/([A-Za-z0-9][A-Za-z0-9_~.-]*)"#
    /// One in running text: not inside a word, an address, a path or a
    /// Markdown link's target.
    nonisolated(unsafe) private static let inText = try! NSRegularExpression(pattern: #"(?<![\w/.:@\-])(?<!\]\()"# + grammar)
    nonisolated(unsafe) private static let whole = try! NSRegularExpression(pattern: "^" + grammar + "$")

    /// Whether the whole string is one record path.
    public static func isWhole(_ text: String) -> Bool {
        whole.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)) != nil
    }

    /// The records named by a bare path in the text, outside code, in order.
    public static func bare(in text: String) -> [RecordReference] {
        var found: [RecordReference] = []
        _ = rewrite(text) { reference, path in
            found.append(reference)
            return path
        }
        return found
    }

    /// Inline Markdown with each bare record path made a link to its record,
    /// `[name](substrate://<path>)`, so it draws as the record's pill. Code
    /// spans and existing links are left as written.
    public static func linking(_ markdown: String, name: (RecordReference) -> String) -> String {
        rewrite(markdown) { reference, _ in
            "[\(escaped(name(reference)))](substrate://\(reference.kind)/\(segment(reference.recordID)))"
        }
    }

    private static func rewrite(_ text: String, _ replace: (RecordReference, String) -> String) -> String {
        guard text.contains("/") else { return text }
        var out = ""
        // Odd pieces between backticks are code spans, kept as written.
        for (index, piece) in text.components(separatedBy: "`").enumerated() {
            if index > 0 { out += "`" }
            guard index % 2 == 0 else {
                out += piece
                continue
            }
            out += rewritePlain(piece, replace)
        }
        return out
    }

    private static func rewritePlain(_ text: String, _ replace: (RecordReference, String) -> String) -> String {
        let ns = text as NSString
        var out = ""
        var at = 0
        for match in inText.matches(in: text, range: NSRange(location: 0, length: ns.length)) {
            var path = ns.substring(with: match.range)
            // A sentence's full stop is not part of the id.
            while path.hasSuffix(".") { path.removeLast() }
            guard let reference = RecordReference(path: path) else { continue }
            out += ns.substring(with: NSRange(location: at, length: match.range.location - at))
            out += replace(reference, path)
            at = match.range.location + (path as NSString).length
        }
        out += ns.substring(from: at)
        return out
    }

    private static func escaped(_ name: String) -> String {
        var out = ""
        for c in name {
            if "\\[]*_`<>".contains(c) { out.append("\\") }
            out.append(c)
        }
        return out
    }

    /// An id as one path segment of a link.
    private static func segment(_ id: String) -> String {
        id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(CharacterSet(charactersIn: "()"))) ?? id
    }
}

/// Inline Markdown as the words it reads as, for a line too short for its
/// marks: a preview, a notification. A link keeps its words and loses its
/// address, emphasis and code lose their marks, and a block's leading mark
/// (a heading's, a quote's, a list item's) goes.
public func plainMarkdown(_ markdown: String) -> String {
    var text = markdown
    let rules: [(String, String)] = [
        (#"!?\[([^\]]*)\]\([^)]*\)"#, "$1"),
        (#"<((?:https?|mailto):[^>]+)>"#, "$1"),
        (#"(?m)^\s{0,3}(?:#{1,6}\s+|>\s?|[-*+]\s+(?:\[[ xX]\]\s+)?|\d+[.)]\s+)"#, ""),
        (#"(\*\*|__)(.+?)\1"#, "$2"),
        (#"(?<![\w*])\*(?!\s)(.+?)(?<!\s)\*(?!\w)"#, "$1"),
        (#"(?<![\w_])_(?!\s)(.+?)(?<!\s)_(?![\w])"#, "$1"),
        (#"~~(.+?)~~"#, "$1"),
        (#"`+([^`]+)`+"#, "$1"),
    ]
    for (pattern, template) in rules {
        guard let regex = try? NSRegularExpression(pattern: pattern) else { continue }
        text = regex.stringByReplacingMatches(in: text, range: NSRange(text.startIndex..., in: text), withTemplate: template)
    }
    return text
}
