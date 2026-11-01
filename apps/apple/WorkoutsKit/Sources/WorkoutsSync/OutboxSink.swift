import Foundation
import WorkoutsCore

/// The session controller's sync seam (`SessionSyncSink`) over the outbox: a
/// finished session, a Health link, a new exercise and a discard each become
/// queued writes, kept before the call returns, and `onQueued` asks for a
/// flush. A session in progress is not written: the phone resumes it from
/// its journal, and its records go at Finish with the plan snapshot.
public struct OutboxSink<Event: Sendable, State: Sendable>: SessionSyncSink {
    public let outbox: Outbox
    public let codec: RecordCodec
    public let onQueued: @Sendable () async -> Void

    public init(outbox: Outbox, codec: RecordCodec = RecordCodec(), onQueued: @escaping @Sendable () async -> Void) {
        self.outbox = outbox
        self.codec = codec
        self.onQueued = onQueued
    }

    public func applied(_: Event, state _: State) async {}

    public func finished(_ records: SessionRecords) async {
        await outbox.enqueue(OutboxPlan.finished(records, codec: codec))
        await onQueued()
    }

    public func discarded(_ sessionID: SessionID) async {
        if await outbox.drop(session: sessionID.rawValue, kind: codec.kinds.session) {
            await outbox.enqueue([OutboxPlan.deleted(sessionID, codec: codec)])
            await onQueued()
        }
    }

    public func healthLinked(_ sessionID: SessionID, healthKitWorkout: String, revision: Int) async {
        await outbox.enqueue([OutboxPlan.healthLinked(sessionID, healthKitWorkout: healthKitWorkout, revision: revision, codec: codec)])
        await onQueued()
    }

    public func exerciseAdded(_ exercise: Exercise) async {
        await outbox.enqueue([OutboxPlan.exerciseAdded(exercise, codec: codec)])
        await onQueued()
    }

    /// A finished session deleted from History: what still waits is dropped
    /// and the server's copy purged, so a replayed create cannot bring it back.
    public func deleted(_ sessionID: SessionID) async {
        await outbox.drop(session: sessionID.rawValue, kind: codec.kinds.session)
        await outbox.enqueue([OutboxPlan.deleted(sessionID, codec: codec)])
        await onQueued()
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

    /// Every finished session and its sets. Sessions still running
    /// elsewhere are left out.
    public func sessions() async throws -> [SessionRecords] {
        let sessions = try await api.list([codec.kinds.session], properties: ["status": ["eq": "finished"]])
        guard !sessions.isEmpty else { return [] }
        let sets = try await api.list([codec.kinds.set], limit: 20000)
        return try codec.sessionRecords(from: sessions + sets)
    }
}
