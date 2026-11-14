/// Where a measurement's intervals and events go for the system's profiler
/// to read (Instruments on an Apple device, through `SystemSignposts`). The
/// portable code that times itself (`Meter`, in SubstrateRuntime) writes
/// through this, so it names no system framework.
public protocol Signposts: Sendable {
    /// Begins an interval under `name`, with `detail` as its message (a
    /// controller's type, a view's); the answer ends it.
    func begin(_ name: StaticString, _ detail: String) -> @Sendable () -> Void
    /// One event: something counted, with no length.
    func event(_ name: StaticString, _ detail: String)
}
