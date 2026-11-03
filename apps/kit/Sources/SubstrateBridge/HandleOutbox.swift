import Foundation

/// One handle's messages to Kotlin, delivered in order on a serial queue of
/// their own. Only presentations are coalesced: one still waiting for its
/// turn at the end of the queue is replaced by a newer one, and encoded only
/// when its turn comes. Lists, calls and the rest are never coalesced.
final class HandleOutbox: @unchecked Sendable {
    private final class Pending: @unchecked Sendable {
        var value: any Encodable & Sendable

        init(_ value: any Encodable & Sendable) {
            self.value = value
        }
    }

    private enum Item {
        case message(String)
        case presentation(Pending)
    }

    let handle: Handle?
    private let queue: DispatchQueue
    private let send: @Sendable (String) -> Void
    private let record: BridgeRecord
    private let lock = NSLock()
    private var items: [Item] = []
    private var head = 0
    private var draining = false
    /// Closing: only the close's own messages go out.
    private var closing = false
    private var closed = false

    init(handle: Handle?, record: BridgeRecord, send: @escaping @Sendable (String) -> Void) {
        self.handle = handle
        self.record = record
        self.send = send
        queue = DispatchQueue(label: "dev.reamde.substrate.bridge.handle-\(handle?.id ?? 0)")
    }

    var isClosed: Bool { lock.withLock { closing || closed } }

    /// Queues a message; false when the handle is closing or closed. The
    /// close's own messages (`cancel`s, then `closed`) are `forced` through.
    @discardableResult
    func enqueue(_ message: String, forced: Bool = false) -> Bool {
        lock.withLock {
            guard !closed, forced || !closing else { return false }
            items.append(.message(message))
            scheduleLocked()
            return true
        }
    }

    func present(_ value: any Encodable & Sendable) {
        lock.withLock {
            guard !closed, !closing else { return }
            if items.count > head, case let .presentation(pending) = items[items.count - 1] {
                pending.value = value
                record.count(\.coalesced)
                return
            }
            items.append(.presentation(Pending(value)))
            scheduleLocked()
        }
    }

    /// From here on only the close's own messages go out.
    func beginClose() {
        lock.withLock { closing = true }
    }

    /// Queues the last message and refuses everything after it.
    func close(with last: String) {
        lock.withLock {
            guard !closed else { return }
            items.append(.message(last))
            closed = true
            scheduleLocked()
        }
    }

    /// Runs `body` on this handle's queue after everything queued before it.
    func after(_ body: @escaping @Sendable () -> Void) {
        queue.async(execute: body)
    }

    private func scheduleLocked() {
        guard !draining else { return }
        draining = true
        queue.async { [self] in drain() }
    }

    private func drain() {
        while true {
            let item: Item? = lock.withLock {
                guard head < items.count else {
                    items.removeAll(keepingCapacity: true)
                    head = 0
                    draining = false
                    return nil
                }
                let item = items[head]
                head += 1
                return item
            }
            guard let item else { return }
            switch item {
            case let .message(text):
                send(text)
            case let .presentation(pending):
                let value = lock.withLock { pending.value }
                do {
                    send(try BridgeCodec.encode(op: .presentation, handle: handle, body: value))
                } catch {
                    send(BridgeCodec.failure(BridgeFailure(.swiftFailure, "a presentation did not encode: \(BridgeCodec.describe(error))"),
                                             handle: handle))
                }
            }
        }
    }
}
