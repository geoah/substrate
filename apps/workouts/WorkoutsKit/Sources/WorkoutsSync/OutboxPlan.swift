import Foundation
import SubstrateCore
import SubstrateLocal
import WorkoutsCore

/// What the outbox writes for each change, in the order the server needs
/// (the package's write rules, docs in SubstrateWorkouts/README.md):
///
/// - an exercise before anything names it;
/// - a session's create before its sets, since `set.session` must exist, and
///   never with `status`, since a create replayed after Finish would move the
///   state back;
/// - a chain's head before its members, since `set.continues` must exist;
/// - Finish and the Health link as patches under `ifVersion`, each raising
///   `revision`, so two writes never share one.
public enum OutboxPlan {
    /// The keys Finish moves, which the create leaves out or the patch sets.
    static let finishKeys = ["status", "endsAt", "utcOffsetEnd", "sessionRPE", "slots"]

    public static func finished(_ records: SessionRecords, codec: RecordCodec) -> [OutboxEntry] {
        let session = codec.encode(records.session)
        let sessionID = records.session.id.rawValue
        let revision = max(records.session.revision ?? 1, 1)
        var create = session.properties
        create["status"] = nil
        create["revision"] = .number(Decimal(revision))
        var out = [OutboxEntry(method: .put, kind: session.kind, recordID: sessionID, properties: create, batch: sessionID)]
        for set in headsFirst(records.sets) {
            let record = codec.encode(set)
            out.append(OutboxEntry(method: .put, kind: record.kind, recordID: record.id, properties: record.properties, batch: sessionID))
        }
        out.append(finish(records, revision: revision + 1, codec: codec))
        return out
    }

    /// Finish alone, at a revision: the patch that moves a session the
    /// server holds unfinished to finished.
    public static func finish(_ records: SessionRecords, revision: Int, codec: RecordCodec) -> OutboxEntry {
        let session = codec.encode(records.session)
        var finish = JSONObject()
        for key in finishKeys {
            if let value = session.properties[key] { finish[key] = value }
        }
        finish["status"] = .string(SessionStatus.finished.rawValue)
        finish["revision"] = .number(Decimal(revision))
        return OutboxEntry(method: .patch, kind: session.kind, recordID: records.session.id.rawValue, properties: finish, guarded: true,
                           batch: records.session.id.rawValue)
    }

    /// The Health save's workout UUID on the session, a revision above
    /// Finish's.
    public static func healthLinked(_ session: SessionID, healthKitWorkout: String, revision: Int, codec: RecordCodec) -> OutboxEntry {
        OutboxEntry(method: .patch, kind: codec.kinds.session, recordID: session.rawValue,
                    properties: ["healthKitWorkout": .string(healthKitWorkout), "revision": .number(Decimal(revision + 2))],
                    guarded: true, batch: session.rawValue)
    }

    public static func exerciseAdded(_ exercise: Exercise, codec: RecordCodec) -> OutboxEntry {
        var local = exercise
        local.isLocal = false
        let record = codec.encode(local)
        return OutboxEntry(method: .put, kind: record.kind, recordID: record.id, properties: record.properties)
    }

    public static func deleted(_ session: SessionID, codec: RecordCodec) -> OutboxEntry {
        OutboxEntry(method: .purge, kind: codec.kinds.session, recordID: session.rawValue, guarded: true, batch: session.rawValue)
    }

    /// A routine deleted on the phone. Purged, so the same id written again
    /// (the sample added back) starts a fresh record. Unguarded: the delete
    /// is the owner's, whatever the agent wrote since.
    public static func routineDeleted(_ routine: RoutineID, codec: RecordCodec) -> OutboxEntry {
        OutboxEntry(method: .purge, kind: codec.kinds.routine, recordID: routine.rawValue)
    }

    /// A routine edited on the phone: the top-level properties the edit
    /// changed (`RoutineEditor.patch`), as a PATCH under the version the
    /// owner edited.
    public static func routineEdited(_ routine: RoutineID, patch: JSONObject, baseVersion: Int?, codec: RecordCodec) -> OutboxEntry {
        OutboxEntry(method: .patch, kind: codec.kinds.routine, recordID: routine.rawValue, properties: patch, guarded: true,
                    baseVersion: baseVersion ?? 0)
    }

    /// A new routine (a duplicate), written whole at an id nothing holds:
    /// `ifVersion: 0`, so a routine another writer made at the same id since
    /// is refused, never merged into.
    public static func routineCreated(_ routine: Routine, codec: RecordCodec) -> OutboxEntry {
        let record = codec.encode(routine)
        return OutboxEntry(method: .put, kind: record.kind, recordID: record.id, properties: record.properties, guarded: true,
                           baseVersion: 0)
    }

    /// The exercises the outbox must write before `uploads`: every one an
    /// upload names (a slot or a set) that the repository lacks, from
    /// `known` (the phone's exercises, then the ones this build carries), in
    /// id order. A session logged signed out on the sample names the
    /// sample's exercises, and a repository that never got the sample would
    /// refuse it: a session's and a set's exercise must exist. An id no
    /// catalogue knows is left to the server's refusal, parked with its words.
    public static func exercisesFor(_ uploads: [SessionRecords], server: Set<String>, known: [ExerciseID: Exercise],
                                    codec: RecordCodec) -> [OutboxEntry]
    {
        var named = Set<ExerciseID>()
        for records in uploads {
            named.formUnion(records.session.slots.map(\.exercise))
            named.formUnion(records.sets.map(\.exercise))
        }
        return named.filter { !server.contains($0.rawValue) }
            .sorted { $0.rawValue < $1.rawValue }
            .compactMap { known[$0].map { exerciseAdded($0, codec: codec) } }
    }

    /// The sets in their order, each chain member after the set it continues.
    static func headsFirst(_ sets: [PerformedSet]) -> [PerformedSet] {
        let ids = Set(sets.map(\.id))
        var placed = Set<SetID>()
        var out: [PerformedSet] = []
        var waiting = sets.sorted { ($0.at, $0.order) < ($1.at, $1.order) }
        while !waiting.isEmpty {
            let before = waiting.count
            waiting.removeAll { set in
                guard let head = set.continues, ids.contains(head), !placed.contains(head) else {
                    out.append(set)
                    placed.insert(set.id)
                    return true
                }
                return false
            }
            if waiting.count == before {
                // A cycle no engine writes: send the rest as they are, and let
                // the server refuse what does not hold.
                out += waiting
                break
            }
        }
        return out
    }
}
