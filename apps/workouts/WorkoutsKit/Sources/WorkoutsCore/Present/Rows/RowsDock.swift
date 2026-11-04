// When the island docks the open set (DESIGN.md §7.6a). The view reports
// what it sees (the open editor's Log button in or out of view, the list
// scrolling or idle, the list scrolling itself to the open set, a jump, a
// fix); this decides what the island draws and how tall the bar stays. It
// is a rule, so it lives here and is tested, and the view only feeds it.

/// Whether the island docks the open set, and whether the bar keeps the
/// docked height.
///
/// - The open set docks only while its editor's Log button is known to be
///   out of view and no done set is being fixed. A new open row starts
///   unknown, unless its editor reported before the dock heard of the row
///   (a panel laid out in the same pass that opened it): that report is
///   kept and used, since its button reports only when it changes.
/// - A fix takes the open panel off the list for a while; that is not the
///   panel leaving the viewport. The last report stands through the fix,
///   and when it ends the panel is laid out again and reports at once.
///   Until it does the dock waits for a report (`awaitsReport`); if none
///   comes, the editor was never laid out, so it is out of view
///   (`noReport()`, which the view calls after a bounded beat).
/// - An open editor the list never laid out counts as out of view once the
///   list jumps elsewhere (the jump menu, a deep link).
/// - While the list scrolls itself to the open set (`seek`), nothing docks:
///   the editor is on its way into view, and an island that showed it for a
///   beat would blink. Undocking is never held back.
/// - Undocking while the list moves keeps the bar's height until it is idle:
///   the bar is the list's bottom inset, and shrinking it near the end of the
///   list would move the content under the finger.
public struct RowsDock: Codable, Equatable, Sendable {
    /// The open row as the layout last had it.
    public private(set) var open: RowID?
    /// A done set is being fixed: its panel is the editor on screen.
    public private(set) var isFixing = false
    /// The open editor's Log button as last reported; nil until it reports.
    public private(set) var logVisible: Bool?
    /// Reports from editors of rows not open (yet), since the last change
    /// of the open row: the first report of a panel laid out before the dock
    /// heard that its row opened.
    public private(set) var early: [RowID: Bool] = [:]
    /// The open editor is to report: a new open row, or a fix ended. Until
    /// it does, `logVisible` is the best guess, and `noReport()` settles it.
    public private(set) var awaitsReport = false
    public private(set) var isScrolling = false
    /// The list scrolls itself to the open set.
    public private(set) var isSeeking = false
    /// The island draws the open set, compact.
    public private(set) var isDocked = false
    /// The bar keeps the docked height: set on docking, released once the
    /// list is idle and the set is no longer docked.
    public private(set) var holdsHeight = false

    public init(open: RowID? = nil) {
        self.open = open
    }

    /// The open row or the fix, as the session and the screen now have them.
    public mutating func show(open: RowID?, isFixing: Bool) {
        guard open != self.open || isFixing != self.isFixing else { return }
        if open != self.open {
            logVisible = open.flatMap { early[$0] }
            awaitsReport = open != nil && logVisible == nil
            early = [:]
        } else if !isFixing {
            // The fix ended: the open panel is laid out again where it was.
            awaitsReport = true
        }
        self.open = open
        self.isFixing = isFixing
        settle()
    }

    /// An editor's Log button came into view or left it. Only the open
    /// editor's counts, and none while a fix holds the screen (the open
    /// panel leaving for the fix's is not the panel leaving the viewport).
    public mutating func logButton(of row: RowID, isVisible: Bool) {
        guard !isFixing else { return }
        guard row == open else {
            early[row] = isVisible
            return
        }
        logVisible = isVisible
        awaitsReport = false
        settle()
    }

    /// The open editor was awaited and never reported: it was never laid
    /// out, so it is out of view.
    public mutating func noReport() {
        guard awaitsReport, open != nil, !isFixing else { return }
        logVisible = false
        awaitsReport = false
        settle()
    }

    /// The list jumped to another set. An open editor that never reported
    /// was never laid out, so it is out of view; one that did reports again
    /// when the jump moves it.
    public mutating func jumped() {
        guard open != nil, !isFixing, logVisible == nil else { return }
        logVisible = false
        awaitsReport = false
        settle()
    }

    /// The list started or stopped scrolling itself to the open set.
    public mutating func seek(_ isSeeking: Bool) {
        self.isSeeking = isSeeking
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
        let wanted = open != nil && !isFixing && logVisible == false
        // A seek holds a dock back, never an undock.
        isDocked = wanted && (isDocked || !isSeeking)
        if isDocked {
            holdsHeight = true
        } else if !isScrolling {
            holdsHeight = false
        }
    }
}
