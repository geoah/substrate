import Foundation

/// A message a search found, cut to the words around the first one that
/// matched, with every one of them marked: what a hit shows under its chat.
/// The server matched a word in any form and any case, so a hit may hold
/// none as typed; it then shows the message's start, with nothing marked.
enum SearchSnippet {
    /// How much of the message stands before the first match, and how long
    /// the whole piece is at most.
    static let lead = 32
    static let length = 140

    static func runs(of text: String, words: [String]) -> [SearchRun] {
        let flat = text.split(whereSeparator: \.isNewline).joined(separator: " ")
        let found = matches(in: flat, words: words)
        var start = flat.startIndex
        if let first = found.first, flat.distance(from: flat.startIndex, to: first.lowerBound) > lead {
            // From the start of a word, a little before the match.
            let from = flat.index(first.lowerBound, offsetBy: -lead)
            start = flat[from ..< first.lowerBound].firstIndex(where: \.isWhitespace).map { flat.index(after: $0) } ?? from
        }
        let end = flat.index(start, offsetBy: length, limitedBy: flat.endIndex) ?? flat.endIndex
        var runs: [SearchRun] = []
        func plain(_ piece: Substring) {
            if !piece.isEmpty { runs.append(SearchRun(text: String(piece), match: false)) }
        }
        if start > flat.startIndex { runs.append(SearchRun(text: "…", match: false)) }
        var at = start
        for range in found where range.lowerBound >= at && range.upperBound <= end {
            plain(flat[at ..< range.lowerBound])
            runs.append(SearchRun(text: String(flat[range]), match: true))
            at = range.upperBound
        }
        plain(flat[at ..< end])
        if end < flat.endIndex { runs.append(SearchRun(text: "…", match: false)) }
        return runs
    }

    /// Where a word of the search stands in the text: a word of the text
    /// that starts with the searched word, or with its stem (the server
    /// matches a word in any form, so "tomatoes" finds "tomato"), marked
    /// whole, whatever the case and the accents; in order, none inside
    /// another.
    static func matches(in text: String, words: [String]) -> [Range<String.Index>] {
        var out: [Range<String.Index>] = []
        for word in Set(words.map(stem)) where !word.isEmpty {
            var from = text.startIndex
            while from < text.endIndex, let range = text.range(of: word, options: [.caseInsensitive, .diacriticInsensitive], range: from ..< text.endIndex) {
                let starts = range.lowerBound == text.startIndex || !(text[text.index(before: range.lowerBound)].isLetter)
                // To the end of the word it begins.
                let end = text[range.upperBound...].firstIndex { !$0.isLetter && !$0.isNumber } ?? text.endIndex
                if starts { out.append(range.lowerBound ..< end) }
                from = range.upperBound
            }
        }
        out.sort { $0.lowerBound < $1.lowerBound }
        var kept: [Range<String.Index>] = []
        for range in out where kept.last.map({ $0.upperBound <= range.lowerBound }) ?? true { kept.append(range) }
        return kept
    }

    /// A word without the ending English puts on it, where enough of it is
    /// left to mean something: what the words of a message are matched by.
    static func stem(_ word: String) -> String {
        let lower = word.lowercased()
        for ending in ["ies", "ing", "es", "ed", "s"] where lower.hasSuffix(ending) && lower.count - ending.count >= 4 {
            return String(lower.dropLast(ending.count))
        }
        return lower
    }
}
