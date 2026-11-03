import Foundation
import SubstrateCore

/// The device's network as an app decides by it: whether it is online, and
/// whether the network is one the owner pays for by the byte.
public struct NetworkPath: Bridged, Hashable {
    public var online: Bool
    public var expensive: Bool

    public init(online: Bool, expensive: Bool) {
        self.online = online
        self.expensive = expensive
    }
}

/// Reports the network, now and as it changes.
public protocol ConnectivityMonitor: Sendable {
    func current() async throws -> NetworkPath
    /// The current path first, then each change, until the reader stops.
    func paths() -> AsyncThrowingStream<NetworkPath, Error>
}
