import Foundation
import SubstrateAPI
import SubstrateCore
import SubstrateLocal
import WorkoutsCore

/// Workouts' outbox is the kit's (`SubstrateLocal.Outbox`, a session and
/// its sets one batch), in the file every build has kept, `outbox.json` in
/// the account's folder: an entry's batch is written under `session` as
/// well, so a build from before the kit reads it back. A format 1 file, from
/// a build that spelled the package `~/training` on the phone, is read
/// through `LegacyKinds` and written back once at format 2. What follows are
/// the routine and session helpers over it.
public extension Outbox {
    static let fileName = "outbox.json"

    /// Respells a format 1 file's kinds, paths and version keys.
    static let workoutsUpgrade = OutboxUpgrade(respell: LegacyKinds.respell, respellProperties: LegacyKinds.respell)

    /// The account folder's outbox.
    static func workouts(directory: URL, writingOptions: Data.WritingOptions = WorkoutsFiles.protectedWriting,
                         now: Date = Date()) throws -> Outbox
    {
        Outbox(folder: try RepositoryFolder(adopting: directory, writingOptions: writingOptions), name: fileName, area: .root, now: now,
               upgrade: workoutsUpgrade)
    }

    /// The file that was there at open and could not be read, set aside, as
    /// Settings lists it.
    var unreadableRecord: UnreadableRecord? {
        unreadable.map {
            UnreadableRecord(source: $0.name, reason: "The changes waiting for your substrate could not be read and were kept aside.")
        }
    }

    /// A session discarded before it finished: its waiting writes dropped
    /// and, when the server holds it or may once a write in flight lands, its
    /// purge queued, in one save. Answers whether a purge was queued.
    @discardableResult
    func discard(session: SessionID, codec: RecordCodec) throws -> Bool {
        try discard(batch: session.rawValue, purge: OutboxPlan.deleted(session, codec: codec))
    }

    /// Drops a session's waiting writes and queues its purge in one save:
    /// a session deleted on the phone.
    func purge(session: SessionID, codec: RecordCodec) throws {
        try purge(batch: session.rawValue, with: OutboxPlan.deleted(session, codec: codec))
    }

    /// A routine deleted on the phone: any write of it still waiting dropped
    /// and its purge queued, in one save.
    func delete(routine: RoutineID, codec: RecordCodec) throws {
        try delete(OutboxPlan.routineDeleted(routine, codec: codec))
    }

    /// Takes back a routine's delete that has not gone yet: the sample added
    /// back before its delete reached the server.
    func withdrawDelete(routine: RoutineID, codec: RecordCodec) throws {
        try withdrawDelete(codec.kinds.routine, routine.rawValue)
    }

    /// Returns once no delete of the routine is on the wire, or after `limit`.
    func settle(routine: RoutineID, codec: RecordCodec, limit: Duration = .seconds(30)) async {
        await settle(codec.kinds.routine, routine.rawValue, limit: limit)
    }

    /// A routine edit, kept on disk before it returns, taken on top of a
    /// guarded edit or create of it still waiting (`Outbox.edit`).
    func edit(routine: RoutineID, patch: JSONObject, baseVersion: Int?, codec: RecordCodec) throws {
        try edit(OutboxPlan.routineEdited(routine, patch: patch, baseVersion: baseVersion, codec: codec))
    }

    /// The routines whose writes wait, as the properties they will set and
    /// whether the write creates the routine: what a catalogue read lays
    /// over the server's answer until they land.
    func waitingRoutines(codec: RecordCodec) -> [String: (creates: Bool, properties: JSONObject)] {
        waiting(kind: codec.kinds.routine).mapValues { (creates: $0.creates, properties: $0.properties) }
    }

    /// Sends what waits, in order, through the app's client.
    @discardableResult
    func flush(_ api: WorkoutsAPI) async -> OutboxState {
        await flush(api.client)
    }
}

public extension OutboxEntry {
    /// An entry with Workouts' word for its batch, the session.
    init(method: Method, kind: String, recordID: String, properties: JSONObject? = nil, guarded: Bool = false, session: String?,
         baseVersion: Int? = nil)
    {
        self.init(method: method, kind: kind, recordID: recordID, properties: properties, guarded: guarded, batch: session,
                  baseVersion: baseVersion)
    }

    /// The session the entry belongs to: its batch.
    var session: String? { batch }
}
