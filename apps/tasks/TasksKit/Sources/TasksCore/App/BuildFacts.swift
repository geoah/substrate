/// This build, as the About section says it.
public struct BuildFacts: Codable, Hashable, Sendable {
    public var version: String
    /// The feed it follows, nil for a local build.
    public var channel: String?

    public init(version: String, channel: String? = nil) {
        self.version = version
        self.channel = channel
    }
}
