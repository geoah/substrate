#if canImport(SwiftUI)
import Foundation
import SubstrateDevice
import SubstrateDeviceApple
import SubstrateRuntime

public extension AppEnvironment {
    /// The environment on an Apple device: the wall clock, the device's
    /// locale and time zone as they change, the app bundle's resources, DNS
    /// through DNS-SD, connectivity through `NWPathMonitor`, and `transport`.
    static func apple(
        transport: any HTTPTransport = URLSessionTransport.shared, secureStore: (any SessionStorage)? = nil,
        folders: (any AppFolders)? = nil
    ) -> AppEnvironment {
        AppEnvironment(
            locale: .autoupdatingCurrent, timeZone: .autoupdatingCurrent, resources: Bundle.main.resourceURL,
            devices: DeviceServices(
                transport: transport, secureStore: secureStore, resolver: DNSServiceTXTResolver(),
                connectivity: PathMonitor(), folders: folders))
    }
}
#endif
