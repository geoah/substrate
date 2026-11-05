import Foundation

/// A note's Markdown as inline Markdown alone. A note is drawn as one `Text`,
/// so it can collapse to a few lines with `lineLimit`, and `AttributedString`
/// reads only inline spans (emphasis, code, links) into a `Text`: so the
/// blocks are spelt here as inline text first. A heading is bold, a bullet is
/// a "•", a quote and a fence lose their markers, a rule goes; every line
/// break is kept, and runs of blank lines are one.
public enum NotesMarkdown {
    public static func inline(_ text: String) -> String {
        var out: [String] = []
        var inFence = false
        for raw in text.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n") {
            let trimmed = raw.trimmingCharacters(in: .whitespaces)
            if trimmed.hasPrefix("```") || trimmed.hasPrefix("~~~") {
                inFence.toggle()
                continue
            }
            if inFence {
                out.append(trimmed.isEmpty ? "" : "`\(trimmed.replacingOccurrences(of: "`", with: "'"))`")
                continue
            }
            if trimmed.isEmpty {
                if let last = out.last, !last.isEmpty { out.append("") }
                continue
            }
            if isRule(trimmed) { continue }
            out.append(line(raw, trimmed: trimmed))
        }
        while out.last == "" { out.removeLast() }
        return out.joined(separator: "\n")
    }

    private static func line(_ raw: String, trimmed: String) -> String {
        if let heading = heading(trimmed) {
            return heading.isEmpty ? "" : "**\(heading)**"
        }
        if trimmed.hasPrefix(">") {
            return line(raw, trimmed: String(trimmed.dropFirst()).trimmingCharacters(in: .whitespaces))
        }
        let indent = String(repeating: "  ", count: min(raw.prefix { $0 == " " }.count / 2, 3))
        for marker in ["- [ ] ", "* [ ] ", "- [x] ", "* [x] ", "- [X] ", "* [X] "] where trimmed.hasPrefix(marker) {
            let box = marker.contains("[ ]") ? "☐" : "☑"
            return indent + box + " " + trimmed.dropFirst(marker.count)
        }
        for marker in ["- ", "* ", "+ "] where trimmed.hasPrefix(marker) {
            return indent + "• " + trimmed.dropFirst(marker.count)
        }
        // An ordered item keeps its number, which inline Markdown reads as text.
        if let dot = trimmed.firstIndex(where: { $0 == "." || $0 == ")" }),
            !trimmed[..<dot].isEmpty, trimmed[..<dot].allSatisfy(\.isNumber),
            trimmed[trimmed.index(after: dot)...].hasPrefix(" ")
        {
            return indent + trimmed[..<dot] + "." + trimmed[trimmed.index(after: dot)...]
        }
        return trimmed
    }

    private static func heading(_ trimmed: String) -> String? {
        let hashes = trimmed.prefix { $0 == "#" }.count
        guard (1 ... 6).contains(hashes) else { return nil }
        let rest = trimmed.dropFirst(hashes)
        guard rest.isEmpty || rest.hasPrefix(" ") else { return nil }
        var title = rest.trimmingCharacters(in: .whitespaces)
        while title.hasSuffix("#") { title.removeLast() }
        return title.trimmingCharacters(in: .whitespaces)
    }

    private static func isRule(_ trimmed: String) -> Bool {
        let marks = trimmed.filter { $0 != " " }
        guard marks.count >= 3, let first = marks.first, "-*_".contains(first) else { return false }
        return marks.allSatisfy { $0 == first }
    }
}
