import Foundation

#if canImport(Glibc)
import Glibc
#elseif canImport(Musl)
import Musl
#endif

/// One thread that runs every job of one seeded run, one at a time, in the
/// order they were enqueued.
///
/// A seed must replay the same way whatever the host is doing, and the
/// shared pool cannot give that: which of two runnable tasks goes first is
/// the scheduler's, and whether a task has had its turn yet is a guess by
/// the wall clock. So a seeded run leaves the pool. Its test's task moves
/// here (`run`), and from then on every job enqueued from this thread
/// comes back to this thread: the tasks the run starts, their children,
/// the actors they hop to, the continuations they resume. The order is the
/// order of the enqueues, which is the run's own doing and nothing else's,
/// and `idle` is exact: the queue is empty, so every task of the run waits
/// on the rig's clock or on the seeded server, which only the next step
/// moves.
///
/// It holds only for work woken from this thread. A real sleep, a callback
/// on a dispatch queue or a hop to the main actor is woken from another
/// thread and runs on the pool again, so nothing in a seeded run may wait
/// on one: `SeededServer` counts a request that reaches it off the thread
/// (`strayed`), and a run that strayed says so.
///
/// The jobs are taken through the runtime's `swift_task_enqueueGlobal_hook`,
/// which the test process sets once and which passes every job enqueued
/// from any other thread on to the pool untouched, so no other test sees
/// it.
final class SeededThread: SerialExecutor, @unchecked Sendable {
    private typealias Original = @convention(thin) (UnownedJob) -> Void
    private typealias Hook = @convention(thin) (UnownedJob, Original) -> Void

    private static let key: pthread_key_t = {
        var key = pthread_key_t()
        pthread_key_create(&key, nil)
        return key
    }()

    /// False where the runtime has no hook to set: a seeded run cannot be
    /// made deterministic there, and refuses to run.
    private static let hooked: Bool = {
        guard let symbol = dlsym(dlopen(nil, RTLD_NOW), "swift_task_enqueueGlobal_hook") else { return false }
        _ = key
        symbol.assumingMemoryBound(to: Hook?.self).pointee = { job, original in
            if let thread = SeededThread.current { thread.push(job) } else { original(job) }
        }
        return true
    }()

    /// The seeded thread this code runs on, nil on any other thread.
    static var current: SeededThread? {
        pthread_getspecific(key).map { Unmanaged<SeededThread>.fromOpaque($0).takeUnretainedValue() }
    }

    private let condition = NSCondition()
    private var jobs: [UnownedJob] = []
    private var next = 0
    private var entering: UnsafeContinuation<Void, Never>?
    private var idling: UnsafeContinuation<Void, Never>?
    private var leaving: UnsafeContinuation<Void, Never>?

    private init() {}

    /// Runs `body` on a thread of its own, in the task that called: what it
    /// starts runs there too, in the order it was started. Returns on the
    /// pool, once everything `body` left runnable has run.
    static func run<Value>(_ body: () async -> Value) async -> Value {
        precondition(hooked, "this Swift runtime has no swift_task_enqueueGlobal_hook: a seeded run cannot be ordered here")
        precondition(current == nil, "a seeded run is not started from inside another")
        let thread = SeededThread()
        thread.start()
        // Resumed from the new thread, so this task's next job is enqueued
        // from it and is the thread's first. A continuation resumed before
        // its task has let go of the thread it is on goes on where it is,
        // with no enqueue at all, so the task asks until it has moved.
        while current !== thread {
            await withUnsafeContinuation { (entered: UnsafeContinuation<Void, Never>) in
                thread.condition.lock()
                thread.entering = entered
                thread.condition.signal()
                thread.condition.unlock()
            }
        }
        let value = await body()
        await thread.idle()
        await withUnsafeContinuation { (left: UnsafeContinuation<Void, Never>) in
            thread.condition.lock()
            thread.leaving = left
            thread.condition.signal()
            thread.condition.unlock()
        }
        return value
    }

    /// Returns once no job of the run is left to run: every task it started
    /// has finished or waits on something only the test moves.
    func idle() async {
        // Asked from this thread only: the loop answers once the job that
        // asked has gone back to it, so the answer is never early.
        precondition(Self.current === self, "idle is asked from the seeded thread's own work")
        await withUnsafeContinuation { (continuation: UnsafeContinuation<Void, Never>) in
            condition.lock()
            idling = continuation
            condition.signal()
            condition.unlock()
        }
    }

    func enqueue(_ job: consuming ExecutorJob) {
        push(UnownedJob(job))
    }

    private func push(_ job: UnownedJob) {
        condition.lock()
        jobs.append(job)
        condition.signal()
        condition.unlock()
    }

    private func start() {
        let thread = Thread { [self] in
            pthread_setspecific(Self.key, Unmanaged.passUnretained(self).toOpaque())
            while true {
                condition.lock()
                while next == jobs.count, entering == nil, idling == nil, leaving == nil { condition.wait() }
                if next < jobs.count {
                    let job = jobs[next]
                    next += 1
                    if next == jobs.count {
                        jobs.removeAll(keepingCapacity: true)
                        next = 0
                    }
                    condition.unlock()
                    job.runSynchronously(on: asUnownedSerialExecutor())
                } else if let waiter = entering {
                    entering = nil
                    condition.unlock()
                    waiter.resume()
                } else if let waiter = idling {
                    idling = nil
                    condition.unlock()
                    waiter.resume()
                } else if let waiter = leaving {
                    leaving = nil
                    condition.unlock()
                    // From here an enqueue from this thread is the pool's
                    // again: the test's task goes back to it.
                    pthread_setspecific(Self.key, nil)
                    waiter.resume()
                    return
                } else {
                    condition.unlock()
                }
            }
        }
        // A thread made by hand gets a small stack by default, and the
        // app's parsers recurse.
        thread.stackSize = 8 << 20
        thread.name = "seeded run"
        thread.start()
    }
}
