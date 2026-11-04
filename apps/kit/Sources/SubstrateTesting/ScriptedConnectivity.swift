import Foundation
import SubstrateDevice

/// Connectivity a test sets: every reader gets the path now, then each one
/// `set` makes.
public final class ScriptedConnectivity: Connectivity, @unchecked Sendable {
    private let lock = NSLock()
    private var path: NetworkPath
    private var readers: [Int: AsyncStream<NetworkPath>.Continuation] = [:]
    private var next = 0

    public init(_ path: NetworkPath = NetworkPath(online: true)) {
        self.path = path
    }

    public func set(_ path: NetworkPath) {
        let readers = lock.withLock { () -> [AsyncStream<NetworkPath>.Continuation] in
            self.path = path
            return Array(self.readers.values)
        }
        for reader in readers { reader.yield(path) }
    }

    public func current() async -> NetworkPath { lock.withLock { path } }

    public func paths() -> AsyncStream<NetworkPath> {
        let (stream, continuation) = AsyncStream<NetworkPath>.makeStream(bufferingPolicy: .bufferingNewest(1))
        let now = lock.withLock { () -> NetworkPath in
            next += 1
            readers[next] = continuation
            return path
        }
        let id = lock.withLock { next }
        continuation.yield(now)
        continuation.onTermination = { [weak self] _ in self?.lock.withLock { _ = self?.readers.removeValue(forKey: id) } }
        return stream
    }
}
