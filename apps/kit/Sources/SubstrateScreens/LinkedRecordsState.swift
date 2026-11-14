import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateLocal
import SubstrateRuntime

/// What `LinkedRecords` holds inside an app's state. The server's records
/// and the outbox's queue are the truth of what the owner sees; `records`
/// is their fold, kept so a presenter does not fold again.
public struct LinkedRecordsState: Codable, Equatable, Sendable {
    public var link: LinkSnapshot?
    /// The kinds the app named last (`LinkedRecords.Event.rekinded`); nil
    /// while they are the ones it opened with.
    public var kinds: [String]?
    /// The records as the server last answered them, by `kind/id`.
    public var server: [String: Record] = [:]
    /// The outbox's writes of the kinds, as it last reported them, and the
    /// app's own ahead of that report.
    public var queue: [OutboxEntry] = []
    /// The ids of writes queued here and not yet seen in a report.
    public var unconfirmed: Set<UUID> = []
    public var records: [String: Record] = [:]
    /// The cache and the queue were read from the device.
    public var loaded = false
    /// Where the watch resumes, nil before it ever opened.
    public var cursor: WatchCursor?
    /// When the last read again for a `compacted` watch started: another
    /// refusal within a minute of it is a failure shown as one, never
    /// another read (`WatchFollower.recovers`). Nothing but the minute
    /// passing lets the next one start.
    public var recoveryStarted: Date?
    public var phase: ReadPhase = .idle
    /// Records whose read again failed (the network went), as `kind/id`:
    /// read at the next chance, so a write that landed is never shown laid
    /// over for longer than the connection is gone.
    public var stale: Set<String> = []
    /// The records the last read stopped at, with more left unread; nil
    /// while the read took them all.
    public var limitReached: Int?

    public init() {}

    /// The folded records of one kind.
    public func records(of kind: String) -> [Record] {
        records.values.filter { $0.kind == kind }
    }
}
