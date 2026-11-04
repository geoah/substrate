import Foundation
import SubstrateCore

/// Device-service calls to Kotlin, for one handle or for none. A call is
/// answered exactly once; cancelling the task that waits on it, or closing
/// its handle, answers it `cancelled` and tells Kotlin to stop.
public final class DeviceChannel: Sendable {
    let handle: Handle?
    private let calls: CallTable
    private let outbox: HandleOutbox
    private let record: BridgeRecord

    init(handle: Handle?, calls: CallTable, outbox: HandleOutbox, record: BridgeRecord) {
        self.handle = handle
        self.calls = calls
        self.outbox = outbox
        self.record = record
    }

    /// One answer, decoded as the type the request's comment names.
    public func call<Answer: Decodable>(_ request: DeviceRequest, as _: Answer.Type) async throws -> Answer {
        let body = try await call(request)
        return try BridgeCodec.decode(Answer.self, from: body)
    }

    public func call(_ request: DeviceRequest) async throws -> JSONValue {
        let id = calls.nextID()
        let message = try BridgeCodec.encode(op: .request, handle: handle, id: id, body: request)
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                calls.waitCall(id, handle: handle, service: request.service, continuation)
                if Task.isCancelled {
                    cancel(id, "the caller was cancelled")
                } else if !outbox.enqueue(message, onFailure: { [calls] in _ = calls.finish(id, failure: $0) }) {
                    calls.cancel(id, "the handle is closed")
                }
            }
        } onCancel: {
            cancel(id, "the caller was cancelled")
        }
    }

    /// One answer, waited for on the calling thread, for a caller that
    /// cannot suspend (a `SessionStorage` is synchronous). The thread that
    /// answers the call wakes the caller, and the request leaves on the
    /// handle's own queue, so the wait needs no thread of Swift's executors:
    /// a few callers blocked on those would otherwise leave none for the
    /// answers, and every wait would run out its time. Past `timeout` the
    /// call is cancelled on both sides.
    public func callBlocking(_ request: DeviceRequest, timeout: TimeInterval) -> Result<JSONValue, Error> {
        let id = calls.nextID()
        let message: String
        do {
            message = try BridgeCodec.encode(op: .request, handle: handle, id: id, body: request)
        } catch {
            return .failure(error)
        }
        let box = AnswerBox()
        calls.waitBlocking(id, handle: handle, service: request.service) { box.set($0) }
        if !outbox.enqueue(message, onFailure: { [calls] in _ = calls.finish(id, failure: $0) }) {
            calls.cancel(id, "the handle is closed")
        }
        if !box.wait(timeout: timeout) {
            cancel(id, "the caller stopped waiting")
        }
        return box.result ?? .failure(BridgeFailure(.unavailable, "call \(id) was not answered in time"))
    }

    /// A chunked answer: values in order, one `chunk` each, until Kotlin
    /// ends it. At most `capacity` wait to be read: past that the answer
    /// fails with `overflow` and Kotlin is told to stop, never a value
    /// silently dropped. Ending the iteration early cancels it on Kotlin's
    /// side.
    public func chunks(_ request: DeviceRequest, capacity: Int = 256) -> AsyncThrowingStream<JSONValue, Error> {
        let id = calls.nextID()
        let (chunks, continuation) = AsyncThrowingStream<JSONValue, Error>.makeStream(bufferingPolicy: .bufferingOldest(capacity))
        continuation.onTermination = { [weak self] termination in
            if case .cancelled = termination { self?.cancel(id, "the reader stopped") }
        }
        calls.waitChunks(id, handle: handle, service: request.service, continuation)
        do {
            let message = try BridgeCodec.encode(op: .request, handle: handle, id: id, body: request)
            if !outbox.enqueue(message, onFailure: { [calls] in _ = calls.finish(id, failure: $0) }) {
                calls.cancel(id, "the handle is closed")
            }
        } catch {
            _ = calls.finish(id, failure: BridgeFailure(.swiftFailure, "a request did not encode: \(BridgeCodec.describe(error))"))
        }
        return chunks
    }

    /// Answers the call `cancelled` and, when it was still waiting, tells Kotlin.
    func cancel(_ id: Int, _ reason: String) {
        if calls.cancel(id, reason) {
            record.count(\.cancelled)
            // Kotlin may never hear it; the call is answered here either way.
            outbox.enqueue(BridgeCodec.encode(op: .cancel, handle: handle, id: id), forced: true, onFailure: { _ in })
        }
    }
}

/// The one answer a blocking call waits for, set from whichever thread
/// answers it.
private final class AnswerBox: @unchecked Sendable {
    private let semaphore = DispatchSemaphore(value: 0)
    private let lock = NSLock()
    private var value: Result<JSONValue, Error>?

    func set(_ result: Result<JSONValue, Error>) {
        let first = lock.withLock { () -> Bool in
            guard value == nil else { return false }
            value = result
            return true
        }
        if first { semaphore.signal() }
    }

    func wait(timeout: TimeInterval) -> Bool {
        semaphore.wait(timeout: .now() + timeout) == .success
    }

    var result: Result<JSONValue, Error>? { lock.withLock { value } }
}
