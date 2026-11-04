import Foundation
import SubstrateRuntime

/// A journal kept in memory, which outlives the runtime that wrote it, so a
/// test closes one runtime as a crash would and opens another over what it
/// left. Every write is kept for the test to read.
public final class MemoryJournal<Event: Codable & Sendable, State: Codable & Sendable>: JournalStore, @unchecked Sendable {
    private let lock = NSLock()
    private var stored: StoredJournal<Event, State>?
    private var written: [[Event]] = []
    private var asideCount = 0
    private var readCount = 0
    private var failing: (any Error)?
    private var failingReads: (any Error)?

    public init(events: [Event]? = nil, state: State? = nil) {
        stored = events.map { StoredJournal(events: $0, cachedState: state) }
    }

    /// The events as stored now; nil when there is no journal.
    public var events: [Event]? {
        lock.lock()
        defer { lock.unlock() }
        return stored?.events
    }

    public var cachedState: State? {
        lock.lock()
        defer { lock.unlock() }
        return stored?.cachedState
    }

    /// Every write, in order: the whole event list each time.
    public var writes: [[Event]] {
        lock.lock()
        defer { lock.unlock() }
        return written
    }

    /// How many times a journal was set aside.
    public var setAsideCount: Int {
        lock.lock()
        defer { lock.unlock() }
        return asideCount
    }

    /// Every write and delete throws `error` until it is set to nil: a full
    /// disk.
    public func failWrites(with error: (any Error)?) {
        lock.lock()
        failing = error
        lock.unlock()
    }

    /// Every read throws `error` until it is set to nil: a journal the
    /// device cannot read, such as `JournalUnavailable` for a lock.
    public func failReads(with error: (any Error)?) {
        lock.lock()
        failingReads = error
        lock.unlock()
    }

    public func write(events: [Event], state: State) throws {
        lock.lock()
        defer { lock.unlock() }
        if let failing { throw failing }
        stored = StoredJournal(events: events, cachedState: state)
        written.append(events)
    }

    public func read() throws -> StoredJournal<Event, State>? {
        lock.lock()
        defer { lock.unlock() }
        readCount += 1
        if let failingReads { throw failingReads }
        return stored
    }

    /// How many times a runtime read the journal: once, when it opens.
    public var reads: Int {
        lock.lock()
        defer { lock.unlock() }
        return readCount
    }

    public func delete() throws {
        lock.lock()
        defer { lock.unlock() }
        if let failing { throw failing }
        stored = nil
    }

    public func setAside(at instant: Date) throws -> String {
        lock.lock()
        defer { lock.unlock() }
        stored = nil
        asideCount += 1
        return "journal.unreadable-\(Int(instant.timeIntervalSince1970))"
    }
}
