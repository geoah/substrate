/// A finish on its way out: the history entry Save writes, the session to
/// start after it, and the Live Activity's last content, which the
/// activity keeps with the summary for five minutes.
public struct FinishedSession: Codable, Equatable, Sendable {
    public var entry: HistoryEntry
    public var then: StartRequest?
    public var activity: LiveActivityContent?

    public init(entry: HistoryEntry, then: StartRequest?, activity: LiveActivityContent?) {
        self.entry = entry
        self.then = then
        self.activity = activity
    }

    public var id: SessionID { entry.id }
}
