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

/// The queue as it stood at one look (`Outbox.report()`): its entries and
/// the writes that landed, read together, and the order of the look among
/// every look at this outbox since it opened. A later look has a greater
/// order. Looks reach a reader along more than one path (the queue's own
/// changes, the answer to a write it queued), so they can arrive out of
/// order; a reader drops one older than a look it already has, which would
/// otherwise show a write as gone that is only not in the older look.
public struct OutboxReport: Equatable, Sendable {
    public var entries: [OutboxEntry]
    public var landed: Set<UUID>
    public var order: Int

    public init(entries: [OutboxEntry], landed: Set<UUID>, order: Int) {
        self.entries = entries
        self.landed = landed
        self.order = order
    }
}

/// The count of the looks taken at every outbox of the app, behind a lock:
/// each outbox is an actor of its own, and they share the one count.
final class LookCount: @unchecked Sendable {
    private let lock = NSLock()
    private var count = 0

    /// The next look's order: one more than the last, never the same twice.
    func next() -> Int {
        lock.lock()
        defer { lock.unlock() }
        count += 1
        return count
    }
}
