import Foundation

/// A finish on its way out: the history entry Save writes, the session to
/// start after it, the Live Activity's last content, which the activity
/// keeps with the summary for five minutes, and the receipt the Save is
/// answered under.
public struct FinishedSession: Codable, Equatable, Sendable {
    public var entry: HistoryEntry
    public var then: StartRequest?
    public var activity: LiveActivityContent?
    /// The Save's receipt (`FinishRequest.receipt`).
    public var receipt: UUID?

    public init(entry: HistoryEntry, then: StartRequest?, activity: LiveActivityContent?, receipt: UUID? = nil) {
        self.entry = entry
        self.then = then
        self.activity = activity
        self.receipt = receipt
    }

    public var id: SessionID { entry.id }
}
