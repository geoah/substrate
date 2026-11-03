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
                } else if !outbox.enqueue(message) {
                    calls.cancel(id, "the handle is closed")
                }
            }
        } onCancel: {
            cancel(id, "the caller was cancelled")
        }
    }

    /// A stream of values, in order, until Kotlin ends it. At most `capacity`
    /// wait to be read: past that the stream fails with `overflow` and Kotlin
    /// is told to stop, never a value silently dropped. Ending the iteration
    /// early cancels it on Kotlin's side.
    public func stream(_ request: DeviceRequest, capacity: Int = 256) -> AsyncThrowingStream<JSONValue, Error> {
        let id = calls.nextID()
        let (stream, continuation) = AsyncThrowingStream<JSONValue, Error>.makeStream(bufferingPolicy: .bufferingOldest(capacity))
        continuation.onTermination = { [weak self] termination in
            if case .cancelled = termination { self?.cancel(id, "the reader stopped") }
        }
        calls.waitStream(id, handle: handle, service: request.service, continuation)
        do {
            if !outbox.enqueue(try BridgeCodec.encode(op: .request, handle: handle, id: id, body: request)) {
                calls.cancel(id, "the handle is closed")
            }
        } catch {
            _ = calls.finish(id, failure: BridgeFailure(.swiftFailure, "a request did not encode: \(BridgeCodec.describe(error))"))
        }
        return stream
    }

    /// Answers the call `cancelled` and, when it was still waiting, tells Kotlin.
    func cancel(_ id: Int, _ reason: String) {
        if calls.cancel(id, reason) {
            record.count(\.cancelled)
            outbox.enqueue(BridgeCodec.encode(op: .cancel, handle: handle, id: id))
        }
    }
}
