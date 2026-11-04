#if canImport(Network)
import Network
import SubstrateDevice

/// Connectivity from `NWPathMonitor`: a monitor per reader, cancelled when
/// the reader stops.
public struct PathMonitor: Connectivity {
    public init() {}

    public func current() async -> NetworkPath {
        for await path in paths() { return path }
        return .offline
    }

    public func paths() -> AsyncStream<NetworkPath> {
        AsyncStream(bufferingPolicy: .bufferingNewest(1)) { continuation in
            let monitor = NWPathMonitor()
            monitor.pathUpdateHandler = { path in
                continuation.yield(
                    NetworkPath(
                        online: path.status == .satisfied, expensive: path.isExpensive,
                        constrained: path.isConstrained))
            }
            continuation.onTermination = { _ in monitor.cancel() }
            monitor.start(queue: DispatchQueue(label: "dev.reamde.substrate.kit.path"))
        }
    }
}
#endif
