import SubstrateCore

/// A command addressed to a controller in a root's tree, as the Android
/// bridge sends one: the keys from the root down to it (none for the root
/// itself), and the command as that controller's `Command` encodes.
public struct RoutedCommand: Codable, Equatable, Sendable {
    public var path: [ChildKey]
    public var command: JSONValue

    public init(path: [ChildKey] = [], command: JSONValue) {
        self.path = path
        self.command = command
    }
}

/// A routed command named a child its parent does not keep, or did not
/// decode as that controller's command.
public struct UnroutableCommand: Error, Equatable, Sendable {
    public var path: [ChildKey]
    public var reason: String
}
