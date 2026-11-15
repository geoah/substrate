import ChatKit

/// The rows a transcript had on screen when rows last arrived above the
/// ones drawn (`TranscriptScroll`'s `above`): what its reader is kept at.
///
/// Rows that arrive above are laid out before anything can be asked of the
/// scroll view, and by then the rows it says are on screen are the ones
/// that arrived: where they are taller than the screen, the row its reader
/// had is as far below, out of sight. Asked then, the scroll view names a
/// row that arrived, and the reader is left at the oldest of them. So the
/// rows on screen are kept from the last time the transcript was drawn
/// before these, which is while its body is made: before any layout.
///
/// A reference the scroll view holds: it is written as the body is made and
/// draws nothing.
@MainActor
final class RowsBefore {
    private var above: RowsAbove?
    /// The rows on screen when `above` last changed.
    private(set) var seen: [String] = []

    /// Told what the transcript is drawn with, each time its body is made.
    func drawn(above: RowsAbove?, seen: [String]) {
        guard above != self.above else { return }
        self.above = above
        self.seen = seen
    }
}
