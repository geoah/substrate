import Foundation
import SubstrateCore

/// What Kotlin hands `start`, once per process: what Swift cannot read from
/// the process on Android (docs/plans/app-kit.md §3.2) and the fingerprint
/// of the protocol Kotlin was generated from.
public struct StartConfig: Bridged, Equatable {
    /// `ProtocolFingerprint.value` as Kotlin was built with it.
    public var fingerprint: String
    /// The device's locale as a BCP 47 tag (`en-GB`); `Locale.current` on
    /// Android is `en_001` whatever the device says.
    public var locale: String
    /// The device's time zone id (`Europe/London`); `TimeZone.current` is GMT.
    public var timeZone: String
    /// The folder the APK's Swift resource bundles were copied out to.
    public var resources: String
    /// The app's folders: state that never leaves the device
    /// (`noBackupFilesDir`), and what may be dropped (`cacheDir`).
    public var data: String
    public var cache: String
    /// `Build.VERSION.SDK_INT`.
    public var sdk: Int
    /// A debuggable build: recorded messages go to the log, redacted.
    public var debug: Bool

    public init(fingerprint: String, locale: String, timeZone: String, resources: String, data: String, cache: String,
                sdk: Int, debug: Bool)
    {
        self.fingerprint = fingerprint
        self.locale = locale
        self.timeZone = timeZone
        self.resources = resources
        self.data = data
        self.cache = cache
        self.sdk = sdk
        self.debug = debug
    }
}

/// What `start` came to. A failure is terminal for the process: the app
/// shows it as a problem, and every later message is refused `notStarted`.
public enum StartAnswer: Bridged, Equatable {
    case started(fingerprint: String)
    case failed(failure: BridgeFailure)
}

/// The device's settings a root's environment carries, from `StartConfig`.
public struct BridgeEnvironment: Sendable {
    public let locale: Locale
    public let timeZone: TimeZone
    /// Where the Swift packages' resource bundles are, one
    /// `<Package>_<Target>.resources` (or `.bundle`) folder each.
    public let resources: URL
    public let data: URL
    public let cache: URL
    public let sdk: Int

    public init(locale: Locale, timeZone: TimeZone, resources: URL, data: URL, cache: URL, sdk: Int) {
        self.locale = locale
        self.timeZone = timeZone
        self.resources = resources
        self.data = data
        self.cache = cache
        self.sdk = sdk
    }

    init(_ config: StartConfig) {
        locale = Locale(identifier: config.locale)
        timeZone = TimeZone(identifier: config.timeZone) ?? TimeZone(secondsFromGMT: 0) ?? .current
        resources = URL(fileURLWithPath: config.resources, isDirectory: true)
        data = URL(fileURLWithPath: config.data, isDirectory: true)
        cache = URL(fileURLWithPath: config.cache, isDirectory: true)
        sdk = config.sdk
    }

    /// A package's resources: what `Bundle.module` would be anywhere but
    /// Android. `bundle(package: "SubstrateKit", target: "SubstrateBridgeCheck")`.
    public func bundle(package: String, target: String) -> Bundle? {
        for suffix in ["resources", "bundle"] {
            let path = resources.appendingPathComponent("\(package)_\(target).\(suffix)").path
            if FileManager.default.fileExists(atPath: path) { return Bundle(path: path) }
        }
        return nil
    }
}
