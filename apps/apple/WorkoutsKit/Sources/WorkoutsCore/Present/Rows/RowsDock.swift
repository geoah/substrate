// When the island docks the open set (DESIGN.md §7.6a). The view reports
// what it sees (the open editor's Log button in or out of view, the list
// scrolling or idle, a jump, a fix, an Undo); this decides what the island
// draws and how tall the bar stays. It is a rule, so it lives here and is
// tested, and the view only feeds it.

/// Whether the island docks the open set, and whether the bar keeps the
/// docked height.
///
/// - The open set docks only while its editor's Log button is known to be
///   out of view and no done set is being fixed. A new open row, a fix
///   starting or ending, and an Undo all start from "in view": the editor is
///   put back in front of the owner, so the island never shows it twice.
/// - An open editor the list never laid out counts as out of view once the
///   list jumps elsewhere (the jump menu, a deep link).
/// - The view reports the Log button from its geometry, on appearing too,
///   so a panel laid out again (a fix ending, an Undo) undocks at once.
/// - Undocking while the list moves keeps the bar's height until it is idle:
///   the bar is the list's bottom inset, and shrinking it near the end of the
///   list would move the content under the finger.
public struct RowsDock: Equatable, Sendable {
    /// The open row as the layout last had it.
    public private(set) var open: RowID?
    /// A done set is being fixed: its panel is the editor on screen.
    public private(set) var isFixing = false
    /// The open editor's Log button as last reported; nil until it reports.
    public private(set) var logVisible: Bool?
    public private(set) var isScrolling = false
    /// The bar keeps the docked height: set on docking, released once the
    /// list is idle and the set is no longer docked.
    public private(set) var holdsHeight = false

    public init(open: RowID? = nil) {
        self.open = open
    }

    /// The island draws the open set, compact.
    public var isDocked: Bool {
        open != nil && !isFixing && logVisible == false
    }

    /// The open row or the fix, as the session and the screen now have them.
    /// A change puts the editor back in view.
    public mutating func show(open: RowID?, isFixing: Bool) {
        guard open != self.open || isFixing != self.isFixing else { return }
        self.open = open
        self.isFixing = isFixing
        logVisible = nil
        settle()
    }

    /// An Undo: the set taken back opens in place, in view.
    public mutating func undo() {
        logVisible = nil
        settle()
    }

    /// An editor's Log button came into view or left it. Only the open
    /// editor's counts, and none while a fix holds the screen.
    public mutating func logButton(of row: RowID, isVisible: Bool) {
        guard row == open, !isFixing else { return }
        logVisible = isVisible
        settle()
    }

    /// The list jumped to another set. An open editor that never reported
    /// was never laid out, so it is out of view; one that did reports again
    /// when the jump moves it.
    public mutating func jumped() {
        guard open != nil, !isFixing, logVisible == nil else { return }
        logVisible = false
        settle()
    }

    /// The Log button is in view when at least half of it lies inside the
    /// list's viewport: the scroll view less the bars over it (the island
    /// at the bottom, at its docked height while it holds it). The panel
    /// itself does not count: a tall panel half on screen with its Log
    /// button gone has nothing to log with, and one whose Log button shows
    /// needs no second one in the island. `button` and `viewport` share one
    /// vertical axis.
    public static func isInView(_ button: ClosedRange<Double>, viewport: ClosedRange<Double>) -> Bool {
        let height = button.upperBound - button.lowerBound
        guard height > 0 else { return false }
        let shown = min(button.upperBound, viewport.upperBound) - max(button.lowerBound, viewport.lowerBound)
        return shown >= height / 2
    }

    public mutating func scroll(isIdle: Bool) {
        isScrolling = !isIdle
        settle()
    }

    private mutating func settle() {
        if isDocked {
            holdsHeight = true
        } else if !isScrolling {
            holdsHeight = false
        }
    }
}
