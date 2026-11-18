import Foundation

/// The device's side of pushes from the publisher's relay: Apple's token
/// for this install, which Apple environment the running build's signature
/// names, and what the install calls itself. The registration with the
/// relay and the device record are the kit's (`PushRegistrar`); this is
/// only what the platform answers.
public protocol PushRegistration: Sendable {
    /// Apple's token for this install, as lowercase hex: the system is asked
    /// to register for remote notifications where it has to be. Throws where
    /// it refuses (a build without the entitlement, no network to Apple).
    func deviceToken() async throws -> String
    /// The Apple environment the running build's signature names; nil for a
    /// build that is not entitled to pushes at all.
    func environment() -> PushEnvironment?
    /// What the device calls itself: "iPhone", "iPad".
    var deviceName: String { get }
    /// The `core/device` platform the install is: `ios`.
    var platform: String { get }
    /// The app's bundle id, the relay's topic.
    var topic: String { get }
}

/// Which of Apple's two push services issued an install's token.
public enum PushEnvironment: String, Codable, Sendable, Equatable {
    case production
    case development

    /// The environment of a build, from its signature: a simulator's is
    /// always `development`, since a simulator build embeds no profile and
    /// reading that as `production` would register a sandbox token as a
    /// production one; a build with no embedded profile (TestFlight, the App
    /// Store) is `production`; any other is what its profile's
    /// `aps-environment` entitlement says, and nil where it says nothing.
    public static func of(profile: Data?, simulator: Bool) -> PushEnvironment? {
        if simulator { return .development }
        guard let profile else { return .production }
        return ProvisioningProfile.apsEnvironment(profile)
    }
}

/// An embedded provisioning profile (`embedded.mobileprovision`): a signed
/// CMS envelope around a property list, read here without checking the
/// signature, since the system checked it to launch the build.
public enum ProvisioningProfile {
    /// The property list the envelope carries in clear.
    public static func plist(_ data: Data) -> [String: Any]? {
        guard let start = data.range(of: Data("<?xml".utf8)),
            let end = data.range(of: Data("</plist>".utf8), in: start.lowerBound ..< data.endIndex)
        else { return nil }
        let xml = data.subdata(in: start.lowerBound ..< end.upperBound)
        return (try? PropertyListSerialization.propertyList(from: xml, options: [], format: nil)) as? [String: Any]
    }

    /// The `aps-environment` entitlement the profile grants, where it grants
    /// one Apple names.
    public static func apsEnvironment(_ data: Data) -> PushEnvironment? {
        guard let entitlements = plist(data)?["Entitlements"] as? [String: Any], let value = entitlements["aps-environment"] as? String
        else { return nil }
        return PushEnvironment(rawValue: value)
    }
}
