/// Adding a provider's API key: written here for a password sign-in, which
/// may; for an app, which never writes a key, the provider's record in the
/// console. The key lives in the field until it is sent.
public struct KeySheet: Codable, Equatable, Sendable, Identifiable {
    public var id: String
    public var title: String
    public var header: String
    public var footer: String
    /// Nil where the app may write the key itself.
    public var console: String?
    public var consoleLink: String
    public var consoleNote: String
    public var problem: String?
}
