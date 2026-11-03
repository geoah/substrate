import Foundation
import SubstrateCore
import SubstrateDevice
import SubstrateRuntime

/// The network on Android: `ConnectivityManager`'s default network, through
/// the bridge.
/// A network the bridge could not ask about reads as offline: an app does
/// not write on a network it cannot see.
public struct BridgeConnectivity: Connectivity {
    public let channel: DeviceChannel

    public init(channel: DeviceChannel) {
        self.channel = channel
    }

    public func current() async -> NetworkPath {
        (try? await channel.call(.connectivityCurrent, as: NetworkPath.self)) ?? .offline
    }

    public func paths() -> AsyncStream<NetworkPath> {
        let values = channel.stream(.connectivityWatch, capacity: 64)
        let (paths, continuation) = AsyncStream<NetworkPath>.makeStream(bufferingPolicy: .bufferingNewest(1))
        let pump = Task {
            do {
                for try await value in values {
                    continuation.yield(try BridgeCodec.decode(NetworkPath.self, from: value))
                }
            } catch {
                continuation.yield(.offline)
            }
            continuation.finish()
        }
        continuation.onTermination = { _ in pump.cancel() }
        return paths
    }
}

/// The app's folders, as Kotlin named them at start: `noBackupFilesDir`,
/// which no backup or transfer reads, and `cacheDir`. Android encrypts both
/// with the credential-encrypted storage, readable after the first unlock.
public struct BridgeFolders: AppFolders {
    public let dataFolder: URL
    public let cacheFolder: URL

    public init(data: URL, cache: URL) {
        dataFolder = data
        cacheFolder = cache
    }

    public func data() throws -> URL { dataFolder }
    public func cache() throws -> URL { cacheFolder }
    public var writingOptions: Data.WritingOptions { [] }
}

public extension BridgeEnvironment {
    var folders: BridgeFolders { BridgeFolders(data: data, cache: cache) }
}

public extension BridgeContext {
    /// Android's device services for this handle, as a controller's
    /// environment carries them. The secure store is named by the app; a
    /// browser sign-in comes with the step that brings the Auth Tab.
    func devices(secureStore: String? = nil) -> DeviceServices {
        DeviceServices(transport: BridgeTransport(channel: device, folder: environment.cache.appendingPathComponent("bridge")),
                       secureStore: secureStore.map { BridgeSecureStore(channel: device, store: $0) },
                       resolver: BridgeTXTResolver(channel: device), connectivity: BridgeConnectivity(channel: device),
                       folders: environment.folders)
    }

    /// An environment for a controller tree on this handle: the device's
    /// locale, time zone and resources, Android's device services.
    func appEnvironment(client: ClientSource = .none, secureStore: String? = nil) -> AppEnvironment {
        AppEnvironment(locale: environment.locale, timeZone: environment.timeZone, resources: environment.resources, client: client,
                       devices: devices(secureStore: secureStore))
    }
}
