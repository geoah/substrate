import Foundation
import SubstrateCore

/// The device calls Swift is waiting on, by id. Each is answered exactly
/// once: a reply, an error, a cancel, or the close of its handle, whichever
/// comes first; anything after that for the id is refused as unknown. A
/// chunked answer takes its chunks in `seq` order into a bounded queue, and
/// ends exactly once. A call's request leaves only through `admit`, which
/// orders it against the call's cancel.
final class CallTable: @unchecked Sendable {
    typealias Chunks = AsyncThrowingStream<JSONValue, Error>

    typealias Answer = @Sendable (Result<JSONValue, Error>) -> Void

    private enum Entry {
        case call(CheckedContinuation<JSONValue, Error>)
        /// A caller blocked on its thread (DeviceChannel.callBlocking):
        /// answered on whichever thread answers the call, so no task has to
        /// run for it to wake.
        case blocking(Answer)
        case chunks(Chunks.Continuation, next: Int)
    }

    private struct Waiting {
        var entry: Entry
        var handle: Handle?
        var service: DeviceService
        /// The request was handed to the outbox: a cancel from here on is
        /// Kotlin's to hear, after it.
        var sent = false
    }

    private let lock = NSLock()
    private var waiting: [Int: Waiting] = [:]
    private var lastID = 0
    /// Runs before each request is admitted, outside the lock: a test
    /// pauses there, between a caller's cancellation check and its send.
    private let beforeAdmitting: @Sendable (Int) -> Void

    init(beforeAdmitting: @escaping @Sendable (Int) -> Void = { _ in }) {
        self.beforeAdmitting = beforeAdmitting
    }

    func nextID() -> Int {
        lock.withLock {
            lastID += 1
            return lastID
        }
    }

    func service(of id: Int) -> DeviceService? {
        lock.withLock { waiting[id]?.service }
    }

    func waitCall(_ id: Int, handle: Handle?, service: DeviceService, _ continuation: CheckedContinuation<JSONValue, Error>) {
        lock.withLock { waiting[id] = Waiting(entry: .call(continuation), handle: handle, service: service) }
    }

    func waitChunks(_ id: Int, handle: Handle?, service: DeviceService, _ continuation: Chunks.Continuation) {
        lock.withLock { waiting[id] = Waiting(entry: .chunks(continuation, next: 0), handle: handle, service: service) }
    }

    func waitBlocking(_ id: Int, handle: Handle?, service: DeviceService, _ answer: @escaping Answer) {
        lock.withLock { waiting[id] = Waiting(entry: .blocking(answer), handle: handle, service: service) }
    }

    /// What admitting a call's request came to.
    enum Admission: Equatable {
        /// It left, and a cancel from here on follows it.
        case sent
        /// The call was answered first (cancelled, or its handle closed), so
        /// nothing left.
        case answered
        /// The outbox refused it: the handle is closing.
        case refused
    }

    /// Hands the call's request to `send` unless the call was answered
    /// first. The check, the send and the mark are one step under the
    /// table's lock, so a cancel either finds the request unsent and keeps
    /// it from leaving, or finds it sent and goes after it. Kotlin drops a
    /// cancel for a call it has not heard of, so one that overtook its
    /// request would leave the request to start for a caller that is gone.
    func admit(_ id: Int, _ send: () -> Bool) -> Admission {
        beforeAdmitting(id)
        return lock.withLock {
            guard waiting[id] != nil else { return .answered }
            guard send() else { return .refused }
            waiting[id]?.sent = true
            return .sent
        }
    }

    /// A reply: the call's answer. A reply to a chunked answer is refused.
    func reply(_ id: Int, _ body: JSONValue?) -> BridgeFailure? {
        let entry: Entry? = lock.withLock {
            switch waiting[id]?.entry {
            case .call?, .blocking?:
                return waiting.removeValue(forKey: id)?.entry
            case .chunks?, nil:
                return nil
            }
        }
        switch entry {
        case let .call(continuation)?: continuation.resume(returning: body ?? .null)
        case let .blocking(answer)?: answer(.success(body ?? .null))
        case .chunks?, nil: return BridgeFailure(.unknownCall, "a reply for call \(id), which nothing waits on")
        }
        return nil
    }

    /// One chunk of a chunked answer, which must be the next in order. A
    /// full queue ends the answer with `overflow`, which the caller cancels on Kotlin's
    /// side: the answer is then `.overflowed`.
    enum ChunkResult: Equatable {
        case taken
        case overflowed
        case refused(BridgeFailure)
    }

    func chunk(_ id: Int, seq: Int?, _ body: JSONValue?) -> ChunkResult {
        lock.lock()
        guard case let .chunks(continuation, next)? = waiting[id]?.entry else {
            lock.unlock()
            return .refused(BridgeFailure(.unknownCall, "a chunk for call \(id), which nothing waits on"))
        }
        guard seq == next else {
            waiting[id] = nil
            lock.unlock()
            let failure = BridgeFailure(.malformed, "call \(id) got chunk \(seq.map(String.init) ?? "without a seq") where \(next) was next")
            continuation.finish(throwing: failure)
            return .refused(failure)
        }
        // Yielded under the lock, so two chunks posted at once cannot pass
        // each other between the check and the queue.
        let result = continuation.yield(body ?? .null)
        if case .dropped = result {
            waiting[id] = nil
            lock.unlock()
            continuation.finish(throwing: BridgeFailure(.overflow, "call \(id) delivered its chunks faster than they were read"))
            return .overflowed
        }
        waiting[id]?.entry = .chunks(continuation, next: next + 1)
        lock.unlock()
        return .taken
    }

    /// A chunked answer's end, or an `error` for any call.
    func finish(_ id: Int, failure: BridgeFailure?) -> BridgeFailure? {
        let entry: Entry? = lock.withLock { waiting.removeValue(forKey: id)?.entry }
        switch entry {
        case nil:
            return BridgeFailure(.unknownCall, "an end for call \(id), which nothing waits on")
        case let .call(continuation)?:
            continuation.resume(throwing: failure ?? BridgeFailure(.malformed, "call \(id) was ended, not answered"))
        case let .blocking(answer)?:
            answer(.failure(failure ?? BridgeFailure(.malformed, "call \(id) was ended, not answered")))
        case let .chunks(continuation, _)?:
            if let failure { continuation.finish(throwing: failure) } else { continuation.finish() }
        }
        return nil
    }

    /// A call answered `cancelled`, by whether its request had left: Kotlin
    /// is told to stop only what it was asked to start.
    enum Cancelled: Equatable {
        case unsent
        case sent
    }

    /// Answers the call `cancelled` if it is still waiting; nil when it was
    /// not.
    @discardableResult
    func cancel(_ id: Int, _ reason: String) -> Cancelled? {
        guard let removed = lock.withLock({ waiting.removeValue(forKey: id) }) else { return nil }
        let failure = BridgeFailure(.cancelled, reason)
        switch removed.entry {
        case let .call(continuation): continuation.resume(throwing: failure)
        case let .blocking(answer): answer(.failure(failure))
        case let .chunks(continuation, _): continuation.finish(throwing: failure)
        }
        return removed.sent ? .sent : .unsent
    }

    /// The calls a handle is waiting on, each answered `cancelled`, in id
    /// order, for the caller to cancel on Kotlin's side the ones it sent.
    func cancelAll(handle: Handle, _ reason: String) -> [(id: Int, cancelled: Cancelled)] {
        let ids = lock.withLock { waiting.filter { $0.value.handle == handle }.keys.sorted() }
        return ids.compactMap { id in cancel(id, reason).map { (id, $0) } }
    }

    var outstanding: Int { lock.withLock { waiting.count } }
}
