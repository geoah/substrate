import Foundation

/// A session whose Apple Health save has not succeeded yet.
public struct HealthQueueEntry: Codable, Hashable, Sendable {
    public var session: SessionID
    public var enqueuedAt: Date
    public var attempts: Int
    public var lastError: String?
    public var lastAttemptAt: Date?

    public init(session: SessionID, enqueuedAt: Date, attempts: Int = 0, lastError: String? = nil, lastAttemptAt: Date? = nil) {
        self.session = session
        self.enqueuedAt = enqueuedAt
        self.attempts = attempts
        self.lastError = lastError
        self.lastAttemptAt = lastAttemptAt
    }
}

/// `health-queue.json`: the sessions to save to Apple Health, oldest first,
/// with the last error (§2.3, §9.5). Written on enqueue, success and failure.
/// Each call reads and rewrites the whole file; the app calls it from one
/// actor, so no two calls interleave.
public struct HealthQueueFile: Sendable {
    public static let fileName = "health-queue.json"

    public let url: URL
    public let writingOptions: Data.WritingOptions

    /// - Parameter directory: the app's directory.
    public init(directory: URL, writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting) {
        url = directory.appendingPathComponent(Self.fileName, isDirectory: false)
        self.writingOptions = writingOptions
    }

    /// The queue, oldest first; empty when there is no file.
    public func load() throws -> [HealthQueueEntry] {
        guard let data = try WorkoutsFiles.read(url) else { return [] }
        return try Self.decoder.decode(File.self, from: data).entries
    }

    public func save(_ entries: [HealthQueueEntry]) throws {
        try WorkoutsFiles.write(Self.encoder.encode(File(format: 1, entries: entries)), to: url, options: writingOptions)
    }

    /// Adds the session at the end, once.
    @discardableResult
    public func enqueue(_ session: SessionID, at date: Date) throws -> [HealthQueueEntry] {
        var entries = try load()
        guard !entries.contains(where: { $0.session == session }) else { return entries }
        entries.append(HealthQueueEntry(session: session, enqueuedAt: date))
        try save(entries)
        return entries
    }

    /// Adds every session not queued yet, in order; returns those added.
    @discardableResult
    public func enqueue(missing sessions: [SessionID], at date: Date) throws -> [SessionID] {
        var entries = try load()
        let queued = Set(entries.map(\.session))
        let added = sessions.reduce(into: [SessionID]()) { added, session in
            if !queued.contains(session), !added.contains(session) { added.append(session) }
        }
        guard !added.isEmpty else { return [] }
        entries += added.map { HealthQueueEntry(session: $0, enqueuedAt: date) }
        try save(entries)
        return added
    }

    /// Finished sessions still waiting for Apple Health that the queue has
    /// lost: Finish died between the history file and the queue write, or the
    /// queue was unreadable. Each is still `pending` with no workout.
    public static func lost(in history: [HistoryEntry]) -> [SessionID] {
        history
            .filter { $0.local.health.status == .pending && $0.records.session.healthKitWorkout == nil }
            .sorted { $0.records.session.at < $1.records.session.at }
            .map(\.id)
    }

    /// Moves a queue file that does not decode aside, so the queue starts
    /// empty and `lost(in:)` refills it from History. Nil when it reads.
    /// A file that cannot be read for another reason (locked, I/O) stays.
    public func setAsideIfUnreadable(at date: Date) -> URL? {
        do {
            _ = try load()
            return nil
        } catch is DecodingError {
            return try? WorkoutsFiles.setAside(url, at: date)
        } catch {
            return nil
        }
    }

    /// Takes the session off the queue: saved, unavailable, or left to "Try again".
    @discardableResult
    public func remove(_ session: SessionID) throws -> [HealthQueueEntry] {
        var entries = try load()
        entries.removeAll { $0.session == session }
        try save(entries)
        return entries
    }

    /// Keeps the session queued with the error, to retry next time.
    @discardableResult
    public func recordFailure(_ session: SessionID, error: String, at date: Date) throws -> [HealthQueueEntry] {
        var entries = try load()
        guard let index = entries.firstIndex(where: { $0.session == session }) else { return entries }
        entries[index].attempts += 1
        entries[index].lastError = error
        entries[index].lastAttemptAt = date
        try save(entries)
        return entries
    }

    private struct File: Codable {
        var format: Int
        var entries: [HealthQueueEntry]
    }

    private static var encoder: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }

    private static var decoder: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }
}
