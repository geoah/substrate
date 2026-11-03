/// Whether the device can reach a network, and on what terms.
public struct NetworkPath: Codable, Hashable, Sendable {
    public var online: Bool
    /// A cellular or hotspot network the person pays for by the byte.
    public var expensive: Bool
    /// Low Data Mode, or Android's data saver.
    public var constrained: Bool

    public init(online: Bool, expensive: Bool = false, constrained: Bool = false) {
        self.online = online
        self.expensive = expensive
        self.constrained = constrained
    }

    public static let offline = NetworkPath(online: false)
}

/// The device's network, as a device service: `NWPathMonitor` on an Apple
/// system, `ConnectivityManager` on Android, a scripted one in a test.
public protocol Connectivity: Sendable {
    /// The network now.
    func current() async -> NetworkPath
    /// The network now, then each change, until the reader stops reading.
    func paths() -> AsyncStream<NetworkPath>
}
