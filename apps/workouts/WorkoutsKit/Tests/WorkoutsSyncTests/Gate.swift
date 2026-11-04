import Foundation

/// One request held on the wire: `arrived()` returns once it is, and
/// `release()` lets it through.
final class Gate: @unchecked Sendable {
    private let lock = NSLock()
    private var arrival: CheckedContinuation<Void, Never>?
    private var hasArrived = false
    private var release_: CheckedContinuation<Void, Never>?
    private var released = false

    func arriveAndWait() async {
        lock.withLock {
            hasArrived = true
            arrival?.resume()
            arrival = nil
        }
        await withCheckedContinuation { continuation in
            let go = lock.withLock { () -> Bool in
                if released { return true }
                release_ = continuation
                return false
            }
            if go { continuation.resume() }
        }
    }

    func arrived() async {
        await withCheckedContinuation { continuation in
            let go = lock.withLock { () -> Bool in
                if hasArrived { return true }
                arrival = continuation
                return false
            }
            if go { continuation.resume() }
        }
    }

    func release() {
        let continuation = lock.withLock { () -> CheckedContinuation<Void, Never>? in
            released = true
            defer { release_ = nil }
            return release_
        }
        continuation?.resume()
    }
}
