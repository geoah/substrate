/// When the presentation follows an event.
public enum Coalescing: Equatable, Sendable {
    /// At once: a change of structure (a turn started, a card arrived).
    case now
    /// Within the window, shared with every event in it, so a streamed
    /// reply's deltas draw once per window rather than once each. An event
    /// that presents now ends the window early.
    case within(Duration)
}
