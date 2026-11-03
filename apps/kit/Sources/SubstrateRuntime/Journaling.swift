/// What the journal does with an applied event, written before the runtime
/// presents or runs an effect.
public enum Journaling: Equatable, Sendable {
    /// Not kept: transient, or a step whose outcome waits on work outside
    /// the engine and arrives as an event of its own.
    case skip
    /// Kept after the events before it.
    case append
    /// Kept as the first event of a new journal: what came before is gone
    /// (a session started).
    case restart
    /// The journal is removed: the state it rebuilt is over (a session
    /// finished, its history file written).
    case clear
}
