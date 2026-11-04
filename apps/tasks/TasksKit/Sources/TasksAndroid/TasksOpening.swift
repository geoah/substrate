import Foundation
import SubstrateAuth
import SubstrateBridge
import SubstrateCore
import TasksCore

/// What Kotlin opens Tasks' roots with: what Swift cannot read from the
/// process on Android, which Apple's build reads from its bundle.
public struct TasksOpening: Bridged, Equatable {
    /// `ProtocolFingerprint.VALUE` of the Kotlin Tasks' values were
    /// generated as, which must be this library's.
    public var fingerprint: String
    /// Whether this is a debug build (`BuildConfig.DEBUG`), the one build
    /// that signs in through loopback with the local client id. A release
    /// never does, whatever it was stamped with.
    public var debug: Bool
    /// A release's client id and the https callback it claims, from its
    /// Gradle stamp; nil in a debug build.
    public var clientID: String?
    public var redirect: String?
    /// "0.1.0 (1)": the versionName and versionCode, as About says them.
    public var version: String
    /// The feed the build follows, nil for a local build.
    public var channel: String?
    /// "this phone", "this tablet".
    public var device: String
    /// The address the connect screen opens with: the one last signed in to.
    public var address: String?

    public init(fingerprint: String, debug: Bool = false, clientID: String? = nil, redirect: String? = nil, version: String,
                channel: String? = nil, device: String, address: String? = nil)
    {
        self.fingerprint = fingerprint
        self.debug = debug
        self.clientID = clientID
        self.redirect = redirect
        self.version = version
        self.channel = channel
        self.device = device
        self.address = address
    }

    /// The settings `open` carried, refused when the Kotlin is another
    /// tree's, and refused for a release without an https client id and
    /// the https callback it claims: Gradle refuses to build one, and were
    /// one built, it would otherwise have no way to sign in but loopback,
    /// where any program on the device could take the code.
    static func read(_ settings: JSONValue?) throws -> TasksOpening {
        guard let settings else { throw BridgeOpenRefused("Tasks' roots open with their settings") }
        let opening = try ValueCoding.decode(TasksOpening.self, from: settings)
        guard opening.fingerprint == ProtocolFingerprint.value else {
            throw BridgeOpenRefused("the app's Kotlin was generated from another tree (\(opening.fingerprint), not \(ProtocolFingerprint.value))")
        }
        guard opening.debug || (opening.releaseClientID != nil && opening.claimedRedirect != nil) else {
            throw BridgeOpenRefused("this release was built without its https client id and claimed callback, so it cannot sign in")
        }
        return opening
    }

    /// This build's manifest: a debug build's local client id and the
    /// loopback route, else the release's stamped client id and claimed
    /// redirect.
    func manifest(in environment: BridgeEnvironment) -> AppManifest {
        TasksManifest.manifest(clientID: debug ? TasksManifest.localClientID : releaseClientID ?? "",
                               claimedRedirect: debug ? nil : claimedRedirect, softwareVersion: version,
                               version: Self.packageVersion(in: environment))
    }

    /// The stamped client id where it is an https URL with a host.
    var releaseClientID: String? {
        let stamped = clientID?.trimmingCharacters(in: .whitespaces) ?? ""
        guard let url = URL(string: stamped), url.scheme?.lowercased() == "https", url.host?.isEmpty == false else { return nil }
        return stamped
    }

    /// The stamped callback where it is one a release may claim.
    var claimedRedirect: URL? { AppManifest.claimedRedirect(redirect?.trimmingCharacters(in: .whitespaces) ?? "") }

    /// The version of the tasks package this build carries, read from the
    /// resources the bridge copied out (`Bundle.module` cannot be read on
    /// Android); 1 where they cannot be read, as on Apple systems.
    static func packageVersion(in environment: BridgeEnvironment) -> Int {
        environment.bundle(package: "TasksKit", target: "TasksCore").flatMap { try? TasksPackage.bundled(from: $0).version } ?? 1
    }
}

/// An `open` Tasks' roots refuse.
struct BridgeOpenRefused: LocalizedError {
    let reason: String

    init(_ reason: String) {
        self.reason = reason
    }

    var errorDescription: String? { reason }
}
