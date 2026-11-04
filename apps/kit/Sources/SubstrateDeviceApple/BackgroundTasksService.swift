#if os(iOS)
import BackgroundTasks
import Foundation
import SubstrateDevice
import UIKit

/// Background work through BackgroundTasks: each job a refresh task the
/// app declares in its Info.plist, registered when this is made, which must
/// be before the app finishes launching; and continued processing as
/// iOS 26's `BGContinuedProcessingTask`, a fresh identifier under the
/// declared prefix each time (registering one twice ends the app), or the
/// short time any app leaving the screen gets where iOS will not run one.
/// A refresh request carries no payload of its own, so each job's is kept
/// in the defaults under its name until the run it becomes.
public final class BackgroundTasksService: BackgroundWork, @unchecked Sendable {
    /// The continued tasks' identifiers are this followed by a UUID; the
    /// Info.plist permits it followed by `*`.
    public let continuedPrefix: String
    private let lock = NSLock()
    private var tasks: [String: BGTask] = [:]
    private var held: [BackgroundRun] = []
    private var listeners: [UUID: AsyncStream<BackgroundRun>.Continuation] = [:]
    private var expiring: [UUID: AsyncStream<String>.Continuation] = [:]
    private let defaults: UserDefaults
    private var continued: [String: Continued] = [:]

    /// The progress units of a continued task: it never knows how long the
    /// work takes, so it creeps toward an end it never reaches.
    private static let units: Int64 = 1_000_000

    private final class Continued {
        var task: BGContinuedProcessingTask?
        var fallback: UIBackgroundTaskIdentifier = .invalid
        var ticks = 0.0
        var ended = false
        var updates: AsyncStream<ContinuedProcessing.Update>.Continuation?
    }

    @MainActor
    public init(jobs: [String], continuedPrefix: String, defaults: UserDefaults = .standard) {
        self.continuedPrefix = continuedPrefix
        self.defaults = defaults
        for job in jobs {
            BGTaskScheduler.shared.register(forTaskWithIdentifier: job, using: .main) { [weak self] task in
                self?.began(task, job: job)
            }
        }
    }

    private func began(_ task: BGTask, job: String) {
        let run = BackgroundRun(id: UUID().uuidString, job: job, budget: 30, payload: payload(of: job))
        task.expirationHandler = { [weak self] in
            // A run taken back before anyone listened is never handed over.
            guard let self,
                let task = lock.withLock({ () -> BGTask? in
                    held.removeAll { $0.id == run.id }
                    return tasks.removeValue(forKey: run.id)
                })
            else { return }
            task.setTaskCompleted(success: false)
            for continuation in lock.withLock({ Array(expiring.values) }) { continuation.yield(run.id) }
        }
        let listening = lock.withLock { () -> [AsyncStream<BackgroundRun>.Continuation] in
            tasks[run.id] = task
            if listeners.isEmpty { held.append(run) }
            return Array(listeners.values)
        }
        for continuation in listening { continuation.yield(run) }
    }

    public func schedule(_ job: BackgroundJob) async {
        // Kept first: a run the request becomes reads it.
        defaults.set(try? JSONEncoder().encode(job.payload), forKey: Self.payloadKey(job.name))
        let request = BGAppRefreshTaskRequest(identifier: job.name)
        request.earliestBeginDate = job.earliest
        try? BGTaskScheduler.shared.submit(request)
    }

    private static func payloadKey(_ job: String) -> String { "substrate.background.payload." + job }

    private func payload(of job: String) -> [String: String] {
        defaults.data(forKey: Self.payloadKey(job)).flatMap { try? JSONDecoder().decode([String: String].self, from: $0) } ?? [:]
    }

    public func runs() -> AsyncStream<BackgroundRun> {
        let (stream, continuation) = AsyncStream<BackgroundRun>.makeStream()
        let id = UUID()
        let waiting = lock.withLock { () -> [BackgroundRun] in
            listeners[id] = continuation
            defer { held = [] }
            return held
        }
        for run in waiting { continuation.yield(run) }
        continuation.onTermination = { [weak self] _ in self?.lock.withLock { _ = self?.listeners.removeValue(forKey: id) } }
        return stream
    }

    public func expirations() -> AsyncStream<String> {
        let (stream, continuation) = AsyncStream<String>.makeStream()
        let id = UUID()
        lock.withLock { expiring[id] = continuation }
        continuation.onTermination = { [weak self] _ in self?.lock.withLock { _ = self?.expiring.removeValue(forKey: id) } }
        return stream
    }

    public func finish(_ run: BackgroundRun, success: Bool) async {
        lock.withLock { tasks.removeValue(forKey: run.id) }?.setTaskCompleted(success: success)
    }

    public func continueProcessing(_ request: ContinuedProcessing) -> AsyncStream<ContinuedProcessing.Update> {
        let (stream, updates) = AsyncStream<ContinuedProcessing.Update>.makeStream()
        let run = Continued()
        run.updates = updates
        lock.withLock { continued[request.id] = run }
        Task { @MainActor in self.start(request, run) }
        return stream
    }

    @MainActor
    private func start(_ request: ContinuedProcessing, _ run: Continued) {
        let identifier = continuedPrefix + UUID().uuidString
        let registered = BGTaskScheduler.shared.register(forTaskWithIdentifier: identifier, using: .main) { [weak self] task in
            guard let self else { return }
            guard let task = task as? BGContinuedProcessingTask, !run.ended else {
                task.setTaskCompleted(success: run.ended)
                return
            }
            run.task = task
            task.progress.totalUnitCount = Self.units
            task.expirationHandler = { [weak self] in
                Task { @MainActor in await self?.endProcessing(request.id) }
            }
            run.updates?.yield(.began)
            Task { await self.advance(request.id) }
        }
        let submission = BGContinuedProcessingTaskRequest(identifier: identifier, title: request.title, subtitle: request.subtitle)
        submission.strategy = .fail
        if registered, (try? BGTaskScheduler.shared.submit(submission)) != nil { return }
        run.updates?.yield(.refused)
        run.fallback = UIApplication.shared.beginBackgroundTask(withName: request.title) { [weak self] in
            MainActor.assumeIsolated { Task { await self?.endProcessing(request.id) } }
        }
    }

    public func advance(_ id: String) async {
        guard let run = lock.withLock({ continued[id] }) else { return }
        await MainActor.run {
            run.ticks += 1
            guard let task = run.task else { return }
            let fraction = 1 - exp(-run.ticks / 60)
            task.progress.completedUnitCount = Int64(fraction * 0.95 * Double(Self.units))
        }
    }

    public func endProcessing(_ id: String) async {
        guard let run = lock.withLock({ continued.removeValue(forKey: id) }) else { return }
        await MainActor.run {
            guard !run.ended else { return }
            run.ended = true
            run.task?.progress.completedUnitCount = Self.units
            run.task?.setTaskCompleted(success: true)
            run.task = nil
            if run.fallback != .invalid {
                UIApplication.shared.endBackgroundTask(run.fallback)
                run.fallback = .invalid
            }
            run.updates?.yield(.ended)
            run.updates?.finish()
        }
    }
}
#endif
