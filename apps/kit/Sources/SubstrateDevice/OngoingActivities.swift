import Foundation

/// Something the device shows while it goes on, outside the app (a Live
/// Activity on the Lock Screen and in the Dynamic Island, an ongoing
/// notification on Android): requested or updated, then ended (§5.4). The
/// content is the app's own value as JSON, which the platform's side reads
/// into the shape its widget draws.
public protocol OngoingActivities: Sendable {
    /// Updates the activity of this id, or requests one where there is
    /// none and the owner neither turned them off nor dismissed this one.
    /// An update older than one sent before (`timestamp`) is dropped.
    func show(_ activity: OngoingActivity) async
    /// Ends the activity of this id: with `final` content shown until
    /// `dismissAt`, or gone at once where either is nil.
    func end(_ id: String, final content: Data?, dismissAt: Date?) async
    /// Ends every activity but the one of `id`: left over from a crash, or
    /// from another install.
    func endAll(except id: String?) async
}

/// One activity as the app shows it now.
public struct OngoingActivity: Codable, Equatable, Sendable {
    public var id: String
    /// Set once, at the request.
    public var title: String
    public var content: Data
    /// When the content is out of date if no update came.
    public var staleAt: Date?
    public var timestamp: Date

    public init(id: String, title: String, content: Data, staleAt: Date? = nil, timestamp: Date) {
        self.id = id
        self.title = title
        self.content = content
        self.staleAt = staleAt
        self.timestamp = timestamp
    }
}
