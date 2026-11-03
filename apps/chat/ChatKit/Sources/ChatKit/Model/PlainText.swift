import Foundation

/// Markdown as one line of plain text, for a preview: no markers, no fences,
/// no table rules, whitespace collapsed.
func plainText(markdown: String) -> String {
    var lines: [String] = []
    for raw in markdown.split(separator: "\n", omittingEmptySubsequences: true) {
        var line = raw.trimmingCharacters(in: .whitespaces)
        if line.hasPrefix("```") || line.hasPrefix("~~~") { continue }
        // The server keeps `lastReply` on one line, so block markers arrive
        // mid-line too: fences, table rules and pipes, heading hashes,
        // bullets, quotes. They go before the inline parse, which would read
        // a fence's backticks as code.
        for (pattern, replacement) in [
            (#"```[A-Za-z0-9_+-]*"#, " "),
            (#"\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?"#, " "),
            (#"^\s*\|\s*|\s*\|\s*$"#, ""),
            (#"\s*\|\s*"#, ", "),
            (#"(,\s*){2,}"#, ", "),
            (#"(^|\s)#{1,6}\s"#, "$1"),
            (#"(^|\s)[-*+]\s\[[ xX]\]\s"#, "$1"),
            (#"(^|\s)[*+]\s"#, "$1"),
            (#"^[-]\s"#, ""),
            (#"(^|\s)>\s"#, "$1"),
        ] {
            line = line.replacingOccurrences(of: pattern, with: replacement, options: .regularExpression)
        }
        lines.append(inlinePlain(line))
    }
    return lines.joined(separator: " ").split(whereSeparator: \.isWhitespace).joined(separator: " ")
}

/// A line's inline Markdown as its text: a link as its label, emphasis and
/// code marks gone. By rule rather than through a Markdown reader, which
/// Linux's and Android's Foundation do not carry.
private func inlinePlain(_ line: String) -> String {
    var text = line
    for (pattern, replacement) in [
        (#"!?\[([^\]]*)\]\([^)]*\)"#, "$1"),
        (#"\*\*|__|~~|`"#, ""),
        (#"(?<![\w*])\*(?=\S)|(?<=\S)\*(?![\w*])"#, ""),
        (#"(?<![\w_])_(?=\S)|(?<=\S)_(?![\w_])"#, ""),
    ] {
        text = text.replacingOccurrences(of: pattern, with: replacement, options: .regularExpression)
    }
    return text
}
