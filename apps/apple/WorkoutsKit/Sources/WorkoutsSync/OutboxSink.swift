import Foundation
import WorkoutsCore

/// The session controller's sync seam (`SessionSyncSink`) over the outbox: a
/// finished session, a Health link, a new exercise, a discard and a delete
/// each become queued writes, kept on disk before the call returns, and
/// `onQueued` asks for a flush. A change the outbox could not keep goes to
/// `onProblem` instead. A session in progress is not written: the phone
/// resumes it from its journal, and its records go at Finish with the plan
/// snapshot.
public struct OutboxSink<Event: Sendable, State: Sendable>: SessionSyncSink {
    public let outbox: Outbox
    public let codec: RecordCodec
    public let onQueued: @Sendable () async -> Void
    public let onProblem: @Sendable (any Error) async -> Void

    public init(outbox: Outbox, codec: RecordCodec = RecordCodec(), onQueued: @escaping @Sendable () async -> Void,
                onProblem: @escaping @Sendable (any Error) async -> Void = { _ in })
    {
        self.outbox = outbox
        self.codec = codec
        self.onQueued = onQueued
        self.onProblem = onProblem
    }

    public func applied(_: Event, state _: State) async {}

    public func finished(_ records: SessionRecords) async {
        await queue { try await outbox.enqueue(OutboxPlan.finished(records, codec: codec)) }
    }

    public func discarded(_ sessionID: SessionID) async {
        await queue { try await outbox.discard(session: sessionID, codec: codec) }
    }

    public func healthLinked(_ sessionID: SessionID, healthKitWorkout: String, revision: Int) async {
        await queue { try await outbox.enqueue([OutboxPlan.healthLinked(sessionID, healthKitWorkout: healthKitWorkout, revision: revision, codec: codec)]) }
    }

    public func exerciseAdded(_ exercise: Exercise) async {
        await queue { try await outbox.enqueue([OutboxPlan.exerciseAdded(exercise, codec: codec)]) }
    }

    /// A finished session deleted from History: what still waits is dropped
    /// and the server's copy purged, so a replayed create cannot bring it
    /// back. The phone's tombstone (`SessionTombstones`) is written before
    /// this, so a purge that was not kept is queued again at the next sync.
    public func deleted(_ sessionID: SessionID) async {
        await queue { try await outbox.purge(session: sessionID, codec: codec) }
    }

    private func queue(_ change: @Sendable () async throws -> some Sendable) async {
        do {
            _ = try await change()
            await onQueued()
        } catch {
            await onProblem(error)
        }
    }
}

/// The sessions deleted on this phone, kept for good beside the history, so
/// no exchange with any repository imports one again or leaves the server's
/// copy: a session id is a UUIDv7 and never comes back on its own.
/// Synchronous, so a delete is on disk before the history file goes.
public final class SessionTombstones: @unchecked Sendable {
    public static let fileName = "deleted-sessions.json"

    public let url: URL
    private let writingOptions: Data.WritingOptions
    // Guards `ids`; `@unchecked Sendable` rests on it.
    private let lock = NSLock()
    private var ids: Set<String> = []
    /// The list that was there at launch and could not be read, set aside.
    public let unreadable: UnreadableRecord?

    public init(directory: URL, writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting, now: Date = Date()) {
        url = directory.appendingPathComponent(Self.fileName)
        self.writingOptions = writingOptions
        guard let data = try? Data(contentsOf: url) else {
            unreadable = nil
            return
        }
        do {
            ids = Set(try JSONDecoder().decode([String].self, from: data))
            unreadable = nil
        } catch {
            let aside = try? WorkoutsFiles.setAside(url, at: now)
            unreadable = UnreadableRecord(source: aside?.lastPathComponent ?? Self.fileName,
                                          reason: "The list of sessions deleted on this iPhone could not be read: \(error)")
        }
    }

    public var all: Set<String> { lock.withLock { ids } }

    public func contains(_ id: SessionID) -> Bool { lock.withLock { ids.contains(id.rawValue) } }

    /// Records the delete, on disk before it returns.
    public func add(_ id: SessionID) throws {
        try lock.withLock {
            guard !ids.contains(id.rawValue) else { return }
            var next = ids
            next.insert(id.rawValue)
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.withoutEscapingSlashes]
            try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
            try encoder.encode(next.sorted()).write(to: url, options: writingOptions)
            ids = next
        }
    }
}

/// Every session and set one repository holds, read for the history
/// exchange: the session records, finished or not, and the sets by session,
/// each with the version the server answered.
public struct ServerSnapshot: Sendable {
    public var sessions: [String: FlatRecord]
    public var sets: [String: [FlatRecord]]

    public init(sessions: [String: FlatRecord] = [:], sets: [String: [FlatRecord]] = [:]) {
        self.sessions = sessions
        self.sets = sets
    }
}

/// Finished sessions from the server: the ones this phone wrote, and any
/// another writer (an importer, another device) put there. The app keeps
/// them in its history store beside its own, so History, PREVIOUS and the
/// suggestions read one history.
public struct ServerHistory: Sendable {
    public var api: WorkoutsAPI
    public var codec: RecordCodec

    public init(api: WorkoutsAPI, codec: RecordCodec = RecordCodec()) {
        self.api = api
        self.codec = codec
    }

    /// Every session, finished or not, and every set, read to the end of the
    /// list: a session missing from a truncated read would look deleted.
    public func snapshot() async throws -> ServerSnapshot {
        let sessions = try await api.list([codec.kinds.session], limit: nil)
        var snapshot = ServerSnapshot()
        for record in sessions { snapshot.sessions[record.id] = record }
        guard !sessions.isEmpty else { return snapshot }
        for record in try await api.list([codec.kinds.set], limit: nil) {
            let set = try codec.decodeSet(record)
            snapshot.sets[set.session.rawValue, default: []].append(record)
        }
        return snapshot
    }
}
