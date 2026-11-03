import Foundation

/// The runtime's scheduling: when a presentation goes out, which effects
/// wait for it, and the window coalesced events share. Read and written only
/// on its runtime's executor, as its hosts are.
final class Loop<Root: Controller>: LoopServices, @unchecked Sendable {
    let environment: AppEnvironment
    let performsEffects: Bool
    let hop: @Sendable (@escaping @Sendable () -> Void) async -> Void
    private let traceSink: (@Sendable (RuntimeTrace) -> Void)?

    var link: LinkSnapshot?
    var root: Host<Root>!
    private(set) var counters = RuntimeCounters()
    private(set) var published: Root.Presentation?

    private var presentNow = false
    private var window: TimeInterval?
    private var flushTask: Task<Void, Never>?
    private var pending: [() -> Void] = []
    private var subscribers: [Int: AsyncStream<Root.Presentation>.Continuation] = [:]
    private var nextSubscriber = 0

    init(environment: AppEnvironment, options: RuntimeOptions,
         hop: @escaping @Sendable (@escaping @Sendable () -> Void) async -> Void)
    {
        self.environment = environment
        performsEffects = options.performsEffects
        traceSink = options.trace
        self.hop = hop
    }

    func requestPresent(_ coalescing: Coalescing) {
        switch coalescing {
        case .now:
            presentNow = true
        case let .within(duration):
            window = min(window ?? duration.seconds, duration.seconds)
        }
    }

    func afterPresent(_ work: @escaping () -> Void) {
        pending.append(work)
    }

    func trace(_ entry: RuntimeTrace) {
        traceSink?(entry)
    }

    func count(delivered: Bool, drop: RuntimeCounters.Drop?) {
        if delivered { counters.delivered += 1 }
        switch drop {
        case .epoch?: counters.droppedByEpoch += 1
        case .generation?: counters.droppedByGeneration += 1
        case nil: break
        }
    }

    /// Ends a step: presents now when an event asked to, and otherwise opens
    /// the window the coalesced events share, once.
    func settle() {
        if presentNow {
            presentNow = false
            window = nil
            flushTask?.cancel()
            flushTask = nil
            flush()
        } else if let window, flushTask == nil {
            let deadline = environment.clock.now.addingTimeInterval(window)
            let clock = environment.clock
            let hop = hop
            let loop = Weak(self)
            flushTask = Task {
                do { try await clock.sleep(until: deadline) } catch { return }
                await hop { loop.target?.windowEnded() }
            }
        }
    }

    private func windowEnded() {
        flushTask = nil
        window = nil
        presentNow = true
    }

    /// The tree presented and published as one value, then the effects its
    /// events asked for, in the order the events were applied.
    private func flush() {
        root.present()
        publish()
        while !pending.isEmpty {
            let batch = pending
            pending = []
            for work in batch { work() }
        }
    }

    /// The first presentation, from the replayed state: no effect runs.
    func presentFirst() {
        root.present()
        pending = []
        published = root.presentation
    }

    private func publish() {
        guard let presentation = root.presentation, presentation != published else { return }
        published = presentation
        trace(.published)
        for continuation in subscribers.values { continuation.yield(presentation) }
    }

    func subscribe() -> AsyncStream<Root.Presentation> {
        let (stream, continuation) = AsyncStream<Root.Presentation>.makeStream(bufferingPolicy: .bufferingNewest(1))
        nextSubscriber += 1
        let id = nextSubscriber
        subscribers[id] = continuation
        if let published { continuation.yield(published) }
        let hop = hop
        let loop = Weak(self)
        continuation.onTermination = { _ in
            Task { await hop { loop.target?.subscribers[id] = nil } }
        }
        return stream
    }

    func close() {
        root.close()
        flushTask?.cancel()
        flushTask = nil
        pending = []
        for continuation in subscribers.values { continuation.finish() }
        subscribers = [:]
    }
}
