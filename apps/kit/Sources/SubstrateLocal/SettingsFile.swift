import Foundation

/// An app's own settings for one repository, kept as one file at the
/// repository folder's root, so a sign-in to another repository reads that
/// one's. A file that does not decode reads as none: settings are the
/// owner's choices, never their data, and the defaults stand in.
public struct SettingsFile<Value: Codable & Sendable>: Sendable {
    public let name: String

    public init(name: String = "settings.json") {
        self.name = name
    }

    public func load(from folder: RepositoryFolder) -> Value? {
        try? folder.load(Value.self, from: name, in: .root).value
    }

    public func save(_ value: Value, to folder: RepositoryFolder) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        try folder.write(encoder.encode(value), to: name, in: .root)
    }
}
