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
}
