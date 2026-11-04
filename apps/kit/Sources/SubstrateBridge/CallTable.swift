import Foundation
import SubstrateCore

/// The device calls Swift is waiting on, by id. Each is answered exactly
/// once: a reply, an error, a cancel, or the close of its handle, whichever
/// comes first; anything after that for the id is refused as unknown. A
/// chunked answer takes its chunks in `seq` order into a bounded queue, and
/// ends exactly once.
final class CallTable: @unchecked Sendable {
    typealias Chunks = AsyncThrowingStream<JSONValue, Error>

    private enum Entry {
        case call(CheckedContinuation<JSONValue, Error>)
        case chunks(Chunks.Continuation, next: Int)
    }

    private struct Waiting {
        var entry: Entry
        var handle: Handle?
        var service: DeviceService
    }

    private let lock = NSLock()
    private var waiting: [Int: Waiting] = [:]
    private var lastID = 0

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

    /// A reply: the call's answer. A reply to a chunked answer is refused.
    func reply(_ id: Int, _ body: JSONValue?) -> BridgeFailure? {
        let continuation: CheckedContinuation<JSONValue, Error>? = lock.withLock {
            guard case let .call(continuation)? = waiting[id]?.entry else { return nil }
            waiting[id] = nil
            return continuation
        }
        guard let continuation else { return BridgeFailure(.unknownCall, "a reply for call \(id), which nothing waits on") }
        continuation.resume(returning: body ?? .null)
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
        case let .chunks(continuation, _)?:
            if let failure { continuation.finish(throwing: failure) } else { continuation.finish() }
        }
        return nil
    }

    /// Answers the call `cancelled` if it is still waiting; true when it was,
    /// so the caller tells Kotlin to stop.
    @discardableResult
    func cancel(_ id: Int, _ reason: String) -> Bool {
        let entry: Entry? = lock.withLock { waiting.removeValue(forKey: id)?.entry }
        let failure = BridgeFailure(.cancelled, reason)
        switch entry {
        case nil: return false
        case let .call(continuation)?: continuation.resume(throwing: failure)
        case let .chunks(continuation, _)?: continuation.finish(throwing: failure)
        }
        return true
    }

    /// The calls a handle is waiting on, each answered `cancelled`: its ids,
    /// for the caller to cancel on Kotlin's side.
    func cancelAll(handle: Handle, _ reason: String) -> [Int] {
        let ids = lock.withLock { waiting.filter { $0.value.handle == handle }.keys.sorted() }
        return ids.filter { cancel($0, reason) }
    }

    var outstanding: Int { lock.withLock { waiting.count } }
}
