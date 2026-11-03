import Foundation
import SubstrateAPI
import SubstrateDevice

/// Everything a controller reaches outside itself, passed in when it is made
/// so a test passes fakes: the clock, the id source, the locale, the time
/// zone and the resources (none of which the process can be asked for on
/// Android), the client, the device services, and what a library adds under
/// a key of its own (`AppEnvironmentKey`; SubstrateLocal's repository
/// folder). Nothing here is a shared instance.
public struct AppEnvironment: Sendable {
    public var clock: any RuntimeClock
    public var ids: any IDSource
    public var locale: Locale
    public var timeZone: TimeZone
    /// The folder the app's resources were copied to: the bundle on an
    /// Apple system, the APK's assets copied out on Android.
    public var resources: URL?
    public var client: ClientSource
    public var devices: DeviceServices
    private var values: [ObjectIdentifier: any Sendable] = [:]

    public init(clock: any RuntimeClock = WallClock(), ids: (any IDSource)? = nil, locale: Locale, timeZone: TimeZone,
                resources: URL? = nil, client: ClientSource = .none, devices: DeviceServices)
    {
        self.clock = clock
        self.ids = ids ?? TimeOrderedIDs(clock: clock)
        self.locale = locale
        self.timeZone = timeZone
        self.resources = resources
        self.client = client
        self.devices = devices
    }

    /// The resources as a bundle.
    public var resourceBundle: Bundle? { resources.flatMap { Bundle(url: $0) } }

    public subscript<Key: AppEnvironmentKey>(key: Key.Type) -> Key.Value {
        get { values[ObjectIdentifier(key)] as? Key.Value ?? Key.defaultValue }
        set { values[ObjectIdentifier(key)] = newValue }
    }
}

/// A value a library adds to the environment, with what reads when none was
/// set.
public protocol AppEnvironmentKey {
    associatedtype Value: Sendable
    static var defaultValue: Value { get }
}

/// The client for the session in use, read when an effect runs, never kept:
/// a sign-out between two effects leaves the second without one.
public struct ClientSource: Sendable {
    private let read: @Sendable () async -> SubstrateClient?

    public init(_ read: @escaping @Sendable () async -> SubstrateClient?) {
        self.read = read
    }

    public static var none: ClientSource { ClientSource { nil } }

    public static func fixed(_ client: SubstrateClient) -> ClientSource { ClientSource { client } }

    public func current() async -> SubstrateClient? { await read() }
}

/// The device services a controller's effects use (§5.4): a protocol each,
/// with an Apple implementation, an Android one and a fake. A service the
/// app does not use is nil.
public struct DeviceServices: Sendable {
    public var transport: any HTTPTransport
    public var secureStore: (any SessionStorage)?
    public var browser: (any BrowserSignIn)?
    public var resolver: (any TXTResolver)?

    public init(transport: any HTTPTransport, secureStore: (any SessionStorage)? = nil, browser: (any BrowserSignIn)? = nil,
                resolver: (any TXTResolver)? = nil)
    {
        self.transport = transport
        self.secureStore = secureStore
        self.browser = browser
        self.resolver = resolver
    }
}
