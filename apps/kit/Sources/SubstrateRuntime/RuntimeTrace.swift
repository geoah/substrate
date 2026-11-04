/// One thing the runtime did, in the order it did it, for a test or a log
/// to read: the order (journal, then present, then effects) is the contract,
/// and a trace is how a test holds a runtime to it. A path names the child
/// it happened in, empty for the root.
public enum RuntimeTrace: Sendable {
    /// A journal was folded at open: no effect ran for any of its events.
    case replayed(path: [ChildKey], events: Int)
    /// A journal could not be read or folded and was moved aside.
    case setAside(path: [ChildKey], place: String)
    case journaled(path: [ChildKey], Journaling, event: any Sendable)
    case refused(path: [ChildKey], event: any Sendable, reason: String)
    case presented(path: [ChildKey])
    /// A parent handed the child under `path` a new value; its state stayed.
    case replaced(path: [ChildKey])
    /// The root's presentation tree went out to the UI.
    case published
    case effect(path: [ChildKey], effect: any Sendable)
    /// An effect's result arrived after its key was started again, or after
    /// its controller closed or reset.
    case dropped(path: [ChildKey], RuntimeCounters.Drop)
}

/// What became of the results effects sent back.
public struct RuntimeCounters: Equatable, Sendable {
    public enum Drop: String, Equatable, Sendable {
        /// Another effect started under the same key since.
        case epoch
        /// The controller closed or reset since.
        case generation
    }

    public var delivered = 0
    public var droppedByEpoch = 0
    public var droppedByGeneration = 0

    public init() {}

    public var dropped: Int { droppedByEpoch + droppedByGeneration }
}

/// The effects a runtime has at work: a test that asserts something did not
/// happen waits for the work that might do it to end first.
public struct RuntimeActivity: Equatable, Sendable {
    /// How many effects have started since the runtime opened: each is
    /// numbered by its place in that count, from 1.
    public var started: Int
    /// The effects whose work is running, by number: a request out, an
    /// answer being read, a sleep on the clock.
    public var running: Set<Int>
    /// Effects decided and not yet started, waiting for a presentation.
    public var waiting: Int

    public init(started: Int, running: Set<Int>, waiting: Int) {
        self.started = started
        self.running = running
        self.waiting = waiting
    }
}

/// How a runtime runs.
public struct RuntimeOptions: Sendable {
    /// False holds every effect as a value in the trace, unstarted, for a
    /// test that answers it with `deliver(_:)`.
    public var performsEffects: Bool
    /// Called on the runtime's executor, in order, for every step.
    public var trace: (@Sendable (RuntimeTrace) -> Void)?

    public init(performsEffects: Bool = true, trace: (@Sendable (RuntimeTrace) -> Void)? = nil) {
        self.performsEffects = performsEffects
        self.trace = trace
    }
}
