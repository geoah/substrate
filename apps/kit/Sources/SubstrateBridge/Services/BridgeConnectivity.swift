import Foundation
import SubstrateCore
import SubstrateDevice

/// The network on Android: `ConnectivityManager`'s default network, through
/// the bridge.
public struct BridgeConnectivity: ConnectivityMonitor {
    public let channel: DeviceChannel

    public init(channel: DeviceChannel) {
        self.channel = channel
    }

    public func current() async throws -> NetworkPath {
        try await channel.call(.connectivityCurrent, as: NetworkPath.self)
    }

    public func paths() -> AsyncThrowingStream<NetworkPath, Error> {
        let values = channel.stream(.connectivityWatch, capacity: 64)
        let (paths, continuation) = AsyncThrowingStream<NetworkPath, Error>.makeStream()
        let pump = Task {
            do {
                for try await value in values {
                    continuation.yield(try BridgeCodec.decode(NetworkPath.self, from: value))
                }
                continuation.finish()
            } catch {
                continuation.finish(throwing: error)
            }
        }
        continuation.onTermination = { _ in pump.cancel() }
        return paths
    }
}

public extension BridgeEnvironment {
    /// The app's folders, as Kotlin named them at start: `noBackupFilesDir`
    /// and `cacheDir`.
    var folders: AppFolders { AppFolders(data: data, cache: cache) }
}
