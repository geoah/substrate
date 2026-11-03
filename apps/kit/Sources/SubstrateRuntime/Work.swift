/// What an effect does once the runtime starts it. Its results are events,
/// delivered through the same loop as a command's, and dropped when the
/// effect's key was started again, or its controller closed or reset,
/// since.
public enum Work<Event: Sendable>: Sendable {
    /// Nothing to run: the effect was a value for a test, or a no-op here.
    case none
    /// One result, or none.
    case once(@Sendable () async -> Event?)
    /// Results until the sequence ends or the effect is cancelled: a watch, a
    /// turn's stream, connectivity.
    case sequence(@Sendable () -> AsyncStream<Event>)
    /// Stops what runs under the effect's key; its late results are dropped.
    case cancel
}
