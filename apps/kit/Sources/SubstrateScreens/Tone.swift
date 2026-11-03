/// What a mark or a card says by its colour, which each system's design
/// tokens draw in its own (§7.4).
public enum Tone: String, Codable, Hashable, Sendable {
    /// No colour of its own: secondary text.
    case quiet
    /// Allowed, done, up to date.
    case positive
    /// Something to know or approve.
    case informative
    /// An account or a thing made.
    case supporting
    /// Something to act on soon.
    case caution
    /// Something that stopped.
    case critical
}
