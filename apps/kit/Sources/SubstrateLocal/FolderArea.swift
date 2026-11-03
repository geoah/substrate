/// One part of a repository's folder, a subfolder of its own, which Storage
/// sizes and a budget bounds apart from the others.
public struct FolderArea: RawRepresentable, Hashable, Codable, Sendable, Comparable {
    public var rawValue: String

    public init(rawValue: String) {
        self.rawValue = rawValue
    }

    public init(_ rawValue: String) {
        self.rawValue = rawValue
    }

    /// The folder itself: its format, the link generation.
    public static let root = FolderArea("")
    /// Controllers' journals: their events are their truth.
    public static let journals = FolderArea("journals")
    /// The outbox: writes waiting for the server.
    public static let outbox = FolderArea("outbox")
    /// The record cache the watch keeps current: the server's, read again
    /// on demand.
    public static let records = FolderArea("records")
    /// Anything else an app keeps that it can read again (Chat's
    /// transcripts).
    public static let cache = FolderArea("cache")

    public static func < (lhs: FolderArea, rhs: FolderArea) -> Bool { lhs.rawValue < rhs.rawValue }
}
