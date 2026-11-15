/// How a reply's table is laid out in the room it has: at its natural
/// width where that fits; with its wide columns wrapped where that makes it
/// fit; scrolled sideways with its first column held in place where it
/// cannot; and at the accessibility text sizes as one card for each row,
/// each header beside its value.
///
/// The widths come from each column's natural width (its widest cell on
/// one line) alone, so a table being written changes a column only when a
/// cell wider than all before it arrives, or when the room runs out.
public enum TableLayout {
    public enum Mode: String, Equatable, Sendable {
        /// The whole table shows, its columns at their natural widths or
        /// wrapped to fit.
        case fits
        /// Wider than its room: it scrolls sideways, the first column held.
        case scrolls
        /// One card for each row.
        case cards
    }

    public struct Plan: Equatable, Sendable {
        public var mode: Mode
        /// Each column's width; none in cards.
        public var widths: [Double]
    }

    /// The sizes the plan is made with, in points at the text's size.
    public struct Metrics: Equatable, Sendable {
        /// The narrowest a column is wrapped to: about ten characters. A
        /// table whose columns cannot all have this much scrolls.
        public var least: Double
        /// The widest a column is drawn at where the table scrolls: about a
        /// line of a phone's width, past which a cell wraps.
        public var widest: Double
        /// How much of the room the held first column may take, so the
        /// columns that scroll keep most of it.
        public var heldShare: Double

        public init(least: Double = 88, widest: Double = 260, heldShare: Double = 0.4) {
            self.least = least
            self.widest = widest
            self.heldShare = heldShare
        }
    }

    /// - Parameters:
    ///   - natural: each column's natural width, its cells' padding in it.
    ///   - room: the width the table has; not yet measured (zero), it has
    ///     all it asks for.
    ///   - accessibilitySize: the text is at one of the accessibility sizes.
    public static func plan(natural: [Double], room: Double, accessibilitySize: Bool, metrics: Metrics = Metrics()) -> Plan {
        if accessibilitySize { return Plan(mode: .cards, widths: []) }
        guard room > 0, natural.reduce(0, +) > room else { return Plan(mode: .fits, widths: natural) }
        if natural.reduce(0, { $0 + min($1, metrics.least) }) <= room {
            return Plan(mode: .fits, widths: shared(natural, room: room))
        }
        var widths = natural.map { min($0, metrics.widest) }
        if let first = widths.first { widths[0] = min(first, max(metrics.least, room * metrics.heldShare)) }
        return Plan(mode: .scrolls, widths: widths)
    }

    /// The room shared among columns that do not all fit at their natural
    /// widths: a column narrower than an even share keeps its width, and
    /// the others share what is left evenly, so only the wide ones wrap.
    public static func shared(_ natural: [Double], room: Double) -> [Double] {
        var widths = natural
        var left = room
        var open = natural.count
        for index in natural.indices.sorted(by: { natural[$0] < natural[$1] }) {
            let share = left / Double(open)
            widths[index] = min(natural[index], share)
            left -= widths[index]
            open -= 1
        }
        return widths
    }
}
