import Foundation
import SubstrateCore

/// This build, as the About section says it.
public struct BuildFacts: Bridged, Hashable, Sendable {
    public var version: String
    /// The feed it follows, nil for a local build.
    public var channel: String?
    /// The publisher's privacy policy, as a release stamped it; nil for a
    /// local build. App Review wants it reachable inside the app (5.1.1(i)).
    public var privacyPolicy: String?

    public init(version: String, channel: String? = nil, privacyPolicy: String? = nil) {
        self.version = version
        self.channel = channel
        self.privacyPolicy = privacyPolicy
    }

    /// This build as a release stamped it into its Info.plist (`info`).
    ///
    /// Only a build with a feed address follows a feed. A TestFlight or
    /// App Store build has none, because Apple updates it, so it names no
    /// channel and says its short version and build number, as a local
    /// build does. App Review reads a pre-release's version, a channel or a
    /// Check for Updates that finds nothing as a beta or an updater (2.2,
    /// 2.5.2), and About offers the check wherever there is a channel.
    public static func stamped(_ info: [String: Any]) -> BuildFacts {
        func value(_ key: String) -> String { (info[key] as? String)?.trimmingCharacters(in: .whitespaces) ?? "" }
        let stamped = value("SubstrateVersion")
        let channel = value("SubstrateChannel")
        let followsFeed = !value("SubstrateUpdates").isEmpty
        // Only an https address is a policy: anything else would be a row
        // that opens nothing.
        let privacy = value("SubstratePrivacyPolicy")
        let policy = URL(string: privacy).flatMap { $0.scheme == "https" && $0.host != nil ? privacy : nil }
        return BuildFacts(
            version: stamped.isEmpty || !followsFeed ? "\(value("CFBundleShortVersionString")) (\(value("CFBundleVersion")))" : stamped,
            channel: channel.isEmpty || !followsFeed ? nil : channel,
            privacyPolicy: policy)
    }
}
