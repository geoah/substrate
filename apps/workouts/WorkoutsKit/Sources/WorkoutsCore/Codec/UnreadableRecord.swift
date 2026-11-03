import Foundation

/// A record a source skipped, for Settings' "Records that could not be read".
public struct UnreadableRecord: Codable, Hashable, Sendable {
    /// Where it came from: a bundled file, `local-exercises.json`, a history file.
    public var source: String
    public var kind: String?
    public var id: String?
    public var reason: String

    public init(source: String, kind: String? = nil, id: String? = nil, reason: String) {
        self.source = source
        self.kind = kind
        self.id = id
        self.reason = reason
    }
}
