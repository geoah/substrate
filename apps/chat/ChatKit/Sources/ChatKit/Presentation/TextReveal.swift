import Foundation

/// How much of a reply being written is drawn. Text arrives in pieces the
/// size the network delivered; it is revealed at a steady rate instead,
/// one that catches up: each step draws a share of what waits, so a burst
/// pours in and a trickle is drawn as it comes. What arrived `lag` ago is
/// drawn by now whatever the rate, so the screen is never further behind
/// the model than that, and what is drawn never moves back.
///
/// Counted in Unicode code points, as the stream counts. Kept by the
/// presenter between presentations, since it is no function of the state
/// alone: it reads the instant.
struct TextReveal: Equatable, Sendable {
    /// The furthest the screen is behind the model.
    static let lag: TimeInterval = 0.4
    /// A step draws the share of the waiting text that draws all of it in
    /// about this long at a steady inflow.
    static let ease: TimeInterval = 0.2
    /// Steps a second while something waits: the parses a second a reply
    /// being written is allowed (`Markdown.parsesPerSecond`).
    static let steps: Double = 30

    private struct Mark: Equatable, Sendable {
        var at: Date
        var total: Int
    }

    /// The code points drawn.
    private(set) var shown = 0
    /// The code points held when it last stepped.
    private(set) var total = 0
    private var stepped: Date?
    /// What had arrived by when, newest last, for the bound.
    private var marks: [Mark] = []

    /// Something held is not drawn yet.
    var waits: Bool { shown < total }

    /// Steps to `now` with `total` code points held. Answers when to step
    /// again, while something still waits. A second step at the same
    /// instant draws nothing more.
    mutating func advance(to total: Int, at now: Date) -> Date? {
        self.total = total
        guard total > shown else {
            marks.removeAll(keepingCapacity: true)
            stepped = now
            return nil
        }
        if marks.last?.total != total { marks.append(Mark(at: now, total: total)) }
        // What arrived `lag` ago is drawn by now.
        var bound = 0
        while let first = marks.first, now.timeIntervalSince(first.at) >= Self.lag {
            bound = first.total
            marks.removeFirst()
        }
        // A clock that went back, or a long gap, steps as one step does.
        let elapsed = min(max(stepped.map { now.timeIntervalSince($0) } ?? 1 / Self.steps, 0), 0.25)
        let step = elapsed > 0 ? max(1, Int((Double(total - shown) * elapsed / Self.ease).rounded(.up))) : 0
        shown = min(total, max(shown + step, bound))
        stepped = now
        return shown < total ? now.addingTimeInterval(1 / Self.steps) : nil
    }

    /// Everything held is drawn: where nothing is paced.
    mutating func finish(_ total: Int) {
        self.total = total
        shown = max(shown, total)
        marks.removeAll()
    }
}
