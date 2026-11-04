/// What a controller's host asks of the runtime it runs on. Every member is
/// called on the runtime's executor, so a step runs from a command or a
/// result to its journal write without a suspension.
protocol LoopServices: AnyObject {
    var environment: AppEnvironment { get }
    /// The link's snapshot as the link last made it: what every controller
    /// reads, and what a controller made later is told.
    var link: LinkSnapshot? { get set }
    var performsEffects: Bool { get }
    /// Runs `body` on the runtime's executor, then settles what it asked.
    var hop: @Sendable (@escaping @Sendable () -> Void) async -> Void { get }
    func requestPresent(_ coalescing: Coalescing)
    /// Runs `work` after the next presentation: effects follow the
    /// presentation they were decided from.
    func afterPresent(_ work: @escaping () -> Void)
    func trace(_ entry: RuntimeTrace)
    func count(delivered: Bool, drop: RuntimeCounters.Drop?)
    /// An effect's work started, numbered in the order effects start in the
    /// whole runtime, or ended (its last result handed back, or none coming).
    func effectStarted() -> Int
    func effectEnded(_ serial: Int)
}
