/// Where the pieces of a text being streamed stand, by the position each
/// carries: how many Unicode code points of the text came before it
/// (`AgentEvent.at`). The text itself is the runs laid end to end, and is
/// kept beside this by whoever holds it.
///
/// A stream that is joined twice sends the text again from its start, and
/// each piece is written where it belongs: over what is held, which is the
/// same text, so nothing is emptied and nothing is doubled. A replay that
/// lost its middle (`truncated`) leaves two runs with a gap between them,
/// which a later replay fills.
struct TextRuns: Codable, Hashable, Sendable {
    struct Run: Codable, Hashable, Sendable {
        /// The code points of the whole text before this run.
        var at: Int
        /// The run's own length in code points.
        var count: Int
    }

    private(set) var runs: [Run] = []

    /// Where the text held ends, in the whole text.
    var end: Int { runs.last.map { $0.at + $0.count } ?? 0 }
    /// Everything from the text's start is held, with no gap.
    var whole: Bool { runs.count <= 1 && (runs.first?.at ?? 0) == 0 }

    init() {}

    /// For a text held with no positions known: one run from its start.
    init(holding text: String) {
        let count = text.unicodeScalars.count
        if count > 0 { runs = [Run(at: 0, count: count)] }
    }

    /// Writes `piece` at `at` into `text`. Answers whether the text grew:
    /// whether it now holds something it did not. A piece written over what
    /// is held replaces it, and what is held past the piece's end stays.
    @discardableResult
    mutating func write(_ piece: String, at: Int, into text: inout String) -> Bool {
        let count = piece.unicodeScalars.count
        guard count > 0, at >= 0 else { return false }
        if runs.isEmpty {
            text = piece
            runs = [Run(at: at, count: count)]
            return true
        }
        if runs.count == 1 {
            // The usual: the piece goes on from where the one run ends.
            if at == runs[0].at + runs[0].count {
                text += piece
                runs[0].count += count
                return true
            }
            // A replay from the run's start that reaches at least as far.
            if at == runs[0].at, count >= runs[0].count {
                let grew = count > runs[0].count
                text = piece
                runs[0].count = count
                return grew
            }
            // A replay of text already held, piece by piece: the same text
            // where it stands changes nothing.
            let offset = at - runs[0].at
            if offset >= 0, offset + count <= runs[0].count {
                let view = text.unicodeScalars
                let start = view.index(view.startIndex, offsetBy: offset)
                if view[start...].starts(with: piece.unicodeScalars) { return false }
            }
        }
        // Anything else: lay the runs out, write the piece over them, and
        // join what touches.
        let before = runs.reduce(0) { $0 + $1.count }
        var scalars = Array(text.unicodeScalars)[...]
        var laid: [(at: Int, scalars: [Unicode.Scalar])] = runs.map { run in
            defer { scalars = scalars.dropFirst(run.count) }
            return (run.at, Array(scalars.prefix(run.count)))
        }
        var written = (at: at, scalars: Array(piece.unicodeScalars))
        var kept: [(at: Int, scalars: [Unicode.Scalar])] = []
        for run in laid {
            let runEnd = run.at + run.scalars.count
            let writtenEnd = written.at + written.scalars.count
            if runEnd < written.at || run.at > writtenEnd {
                kept.append(run)
                continue
            }
            // Touching or overlapping: the piece wins where both hold text.
            let start = min(run.at, written.at)
            var joined = [Unicode.Scalar](repeating: " ", count: max(runEnd, writtenEnd) - start)
            joined.replaceSubrange(run.at - start ..< runEnd - start, with: run.scalars)
            joined.replaceSubrange(written.at - start ..< writtenEnd - start, with: written.scalars)
            written = (start, joined)
        }
        kept.append(written)
        laid = kept.sorted { $0.at < $1.at }
        runs = laid.map { Run(at: $0.at, count: $0.scalars.count) }
        var out = String.UnicodeScalarView()
        for run in laid { out.append(contentsOf: run.scalars) }
        text = String(out)
        return runs.reduce(0) { $0 + $1.count } > before
    }
}
