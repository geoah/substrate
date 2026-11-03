import Foundation
import SubstrateDevice

/// Background work a test drives: the jobs scheduled, runs it grants and
/// takes back, and continued processing it answers, each kept in order.
public final class ScriptedBackground: BackgroundWork, @unchecked Sendable {
    private let lock = NSLock()
    private var jobs: [BackgroundJob] = []
    private var finishedRuns: [String: Bool] = [:]
    private var listeners: [AsyncStream<BackgroundRun>.Continuation] = []
    private var expiring: [AsyncStream<String>.Continuation] = []
    private var expiredRuns: [String] = []
    private var continuing: [String: AsyncStream<ContinuedProcessing.Update>.Continuation] = [:]
    private var requests: [ContinuedProcessing] = []
    private var advances: [String: Int] = [:]
    private var ends: [String] = []

    public init() {}

    public var scheduled: [BackgroundJob] { lock.withLock { jobs } }
    /// Each run answered, by id, with whether it succeeded.
    public var finished: [String: Bool] { lock.withLock { finishedRuns } }
    public var continued: [ContinuedProcessing] { lock.withLock { requests } }
    public var ended: [String] { lock.withLock { ends } }
    /// Each run taken back, by id, in order.
    public var expired: [String] { lock.withLock { expiredRuns } }
    public func advanced(_ id: String) -> Int { lock.withLock { advances[id] ?? 0 } }

    public func schedule(_ job: BackgroundJob) async { lock.withLock { jobs.append(job) } }

    public func runs() -> AsyncStream<BackgroundRun> {
        let (stream, continuation) = AsyncStream<BackgroundRun>.makeStream()
        lock.withLock { listeners.append(continuation) }
        return stream
    }

    /// The system grants a run.
    public func grant(_ run: BackgroundRun) {
        for continuation in lock.withLock({ listeners }) { continuation.yield(run) }
    }

    /// The system grants a run of the job last scheduled under `name`, with
    /// its payload, as a device does; nil when none was scheduled.
    @discardableResult
    public func grant(job name: String, id: String, budget: TimeInterval? = nil) -> BackgroundRun? {
        guard let job = lock.withLock({ jobs.last { $0.name == name } }) else { return nil }
        let run = BackgroundRun(id: id, job: name, budget: budget, payload: job.payload)
        grant(run)
        return run
    }

    public func expirations() -> AsyncStream<String> {
        let (stream, continuation) = AsyncStream<String>.makeStream()
        lock.withLock { expiring.append(continuation) }
        return stream
    }

    /// The system takes a run back before the app answered it.
    public func expire(_ id: String) {
        let listening = lock.withLock { () -> [AsyncStream<String>.Continuation] in
            expiredRuns.append(id)
            return expiring
        }
        for continuation in listening { continuation.yield(id) }
    }

    public func finish(_ run: BackgroundRun, success: Bool) async { lock.withLock { finishedRuns[run.id] = success } }

    public func continueProcessing(_ request: ContinuedProcessing) -> AsyncStream<ContinuedProcessing.Update> {
        let (stream, continuation) = AsyncStream<ContinuedProcessing.Update>.makeStream()
        lock.withLock {
            requests.append(request)
            continuing[request.id] = continuation
        }
        continuation.yield(.began)
        return stream
    }

    public func advance(_ id: String) async { lock.withLock { advances[id, default: 0] += 1 } }

    public func endProcessing(_ id: String) async {
        let continuation = lock.withLock { () -> AsyncStream<ContinuedProcessing.Update>.Continuation? in
            ends.append(id)
            return continuing.removeValue(forKey: id)
        }
        continuation?.yield(.ended)
        continuation?.finish()
    }
}
