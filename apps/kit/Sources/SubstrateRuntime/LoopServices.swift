/// What a controller's host asks of the runtime it runs on. Every member is
/// called on the runtime's executor, so a step runs from a command or a
/// result to its journal write without a suspension.
protocol LoopServices: AnyObject {
    var environment: AppEnvironment { get }
    var link: LinkSnapshot? { get }
    var performsEffects: Bool { get }
    /// Runs `body` on the runtime's executor, then settles what it asked.
    var hop: @Sendable (@escaping @Sendable () -> Void) async -> Void { get }
    func requestPresent(_ coalescing: Coalescing)
    /// Runs `work` after the next presentation: effects follow the
    /// presentation they were decided from.
    func afterPresent(_ work: @escaping () -> Void)
    func trace(_ entry: RuntimeTrace)
    func count(delivered: Bool, drop: RuntimeCounters.Drop?)
}
