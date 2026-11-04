import Foundation

/// Where an outbox stands, as `Outbox.changes()` reports it each time it
/// moves: what the link presents as the Status screen, and the revision it
/// starts a flush on.
public struct OutboxSummary: Equatable, Sendable {
    /// How the last flush ended.
    public var state: OutboxState
    /// Every write still queued, parked ones included.
    public var waiting: Int
    /// The writes the server refused, waiting for the owner.
    public var parked: [OutboxEntry]
    /// The writes a flush would try now: not parked, and not behind a
    /// parked write of their batch.
    public var sendable: Int
    /// A flush is running.
    public var flushing: Bool
    /// Moved by every change the app makes to the queue (a write queued,
    /// retried, discarded), never by a flush, so a reader that flushes when
    /// it moves never flushes on its own flush's answer.
    public var revision: Int
    /// Why the last save failed, until one succeeds.
    public var saveProblem: String?
    /// The file that was there at open and could not be read, set aside.
    public var unreadable: SetAsideFile?
    /// Why the file there could not be read, until a read again succeeds:
    /// every change is refused meanwhile.
    public var unavailable: String?

    public init(
        state: OutboxState = .idle, waiting: Int = 0, parked: [OutboxEntry] = [], sendable: Int = 0, flushing: Bool = false,
        revision: Int = 0, saveProblem: String? = nil, unreadable: SetAsideFile? = nil, unavailable: String? = nil
    ) {
        self.state = state
        self.waiting = waiting
        self.parked = parked
        self.sendable = sendable
        self.flushing = flushing
        self.revision = revision
        self.saveProblem = saveProblem
        self.unreadable = unreadable
        self.unavailable = unavailable
    }
}
