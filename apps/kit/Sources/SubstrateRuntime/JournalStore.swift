import Foundation

/// Where a controller's journal is kept: its journaled events, and the state
/// they fold to as a cache. Synchronous, because the runtime writes it before
/// anything else happens after an event, with no suspension between the
/// engine and the write. SubstrateLocal's `JournalFile` keeps it in the
/// repository's folder; SubstrateTesting's `MemoryJournal` in memory.
public protocol JournalStore<Event, State>: Sendable {
    associatedtype Event: Codable & Sendable
    associatedtype State: Codable & Sendable

    /// Replaces the journal with these events and their fold.
    func write(events: [Event], state: State) throws
    /// The journal, or nil when there is none. The cached state is nil when
    /// it no longer decodes; the events must.
    func read() throws -> StoredJournal<Event, State>?
    func delete() throws
    /// Moves a journal that cannot be folded out of the way, kept for a
    /// look; answers where it went, in words.
    func setAside(at instant: Date) throws -> String
}

/// A journal as read back.
public struct StoredJournal<Event: Sendable, State: Sendable>: Sendable {
    public var events: [Event]
    public var cachedState: State?

    public init(events: [Event], cachedState: State?) {
        self.events = events
        self.cachedState = cachedState
    }
}

/// The journal is there and the device could not read it (a lock, an I/O
/// error), which a later launch may: it is left where it is, never set
/// aside.
public struct JournalUnavailable: Error, Sendable {
    public var reason: String

    public init(reason: String) {
        self.reason = reason
    }
}
