/// How a code block lays out its lines: side by side with a scroll, or
/// wrapped. A long line scrolls sideways unless its reader chose to wrap
/// (`ChatPreferences.wrapCode`, kept on the device), and the switch between
/// the two is offered only where a line is longer than the room for it.
public enum CodeWrap {
    public struct Layout: Equatable, Sendable {
        /// A line is longer than the room: the switch is shown.
        public var offers: Bool
        /// The lines are wrapped.
        public var wraps: Bool
    }

    /// Whether the block's longest line overflows.
    ///
    /// - Parameters:
    ///   - columns: the longest line, in columns (`CodeHighlight.columns`).
    ///   - advance: one column's width in the font the code is drawn in.
    ///   - room: the width the lines have. Not yet measured (zero), nothing
    ///     is known to overflow.
    public static func overflows(columns: Int, advance: Double, room: Double) -> Bool {
        guard room > 0, advance > 0 else { return false }
        // Half a point of slack: a line that fits to the point is drawn
        // whole either way.
        return Double(columns) * advance > room + 0.5
    }

    public static func layout(columns: Int, advance: Double, room: Double, prefersWrap: Bool) -> Layout {
        Layout(offers: overflows(columns: columns, advance: advance, room: room), wraps: prefersWrap)
    }
}
