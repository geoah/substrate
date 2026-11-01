import Foundation
import WorkoutsCore

/// What one sync does between the phone's history and one repository's
/// sessions, decided from three records the phone keeps:
///
/// - the outbox's versions, per repository: a session whose record version
///   is there was uploaded to that repository or read from it, so its
///   absence on the server later is the owner's delete, never a reason to
///   send it again;
/// - the phone's tombstones (`SessionTombstones`): a session deleted on the
///   phone is never imported again, and a server copy is purged;
/// - the writes still queued: a session with a write waiting is the phone's
///   until the write lands, so the server's copy is neither merged nor read
///   as deleted.
public enum HistoryExchange {
    public struct Plan: Sendable, Equatable {
        /// Finished sessions on the server the phone lacks.
        public var imports: [SessionRecords] = []
        /// Sessions on the phone changed on the server since the phone last
        /// wrote or read them: the server's records replace the phone's,
        /// which keeps its own Health state.
        public var merges: [SessionRecords] = []
        /// Sessions on the phone that this repository held and no longer
        /// does: deleted on the server, so removed from the phone.
        public var removals: [SessionID] = []
        /// Sessions on the phone this repository never held: sent.
        public var uploads: [SessionRecords] = []
        /// Sessions deleted on the phone that the server still holds.
        public var purges: [SessionID] = []
        /// Sessions finished on the phone that the server holds unfinished
        /// (a Finish refused as stale behind another writer's edit): Finish
        /// again, above the server's revision.
        public var finishes: [OutboxEntry] = []
        /// Record versions read for what was imported or merged, for the
        /// outbox to keep.
        public var seen: [String: Int] = [:]
        /// Server sessions the phone could not read.
        public var unreadable: [UnreadableRecord] = []

        public init() {}

        public var isEmpty: Bool {
            imports.isEmpty && merges.isEmpty && removals.isEmpty && uploads.isEmpty && purges.isEmpty && finishes.isEmpty && seen.isEmpty
        }
    }

    public static func plan(server: ServerSnapshot, local: [SessionRecords], queued: Set<String>, versions: [String: Int],
                            tombstones: Set<String>, codec: RecordCodec) -> Plan
    {
        var plan = Plan()
        let kinds = codec.kinds
        let localByID = Dictionary(local.map { ($0.id.rawValue, $0) }, uniquingKeysWith: { first, _ in first })

        for id in server.sessions.keys.sorted() {
            guard let record = server.sessions[id] else { continue }
            if tombstones.contains(id) {
                if !queued.contains(id) { plan.purges.append(SessionID(rawValue: id)) }
                continue
            }
            if queued.contains(id) { continue }
            let sets = server.sets[id] ?? []
            let records: SessionRecords
            do {
                records = try decode(record, sets: sets, codec: codec)
            } catch {
                plan.unreadable.append(UnreadableRecord(source: "your substrate", kind: record.kind, id: id, reason: error.description))
                continue
            }
            let finished = records.session.status == .finished
            guard let mine = localByID[id] else {
                if finished {
                    plan.imports.append(records)
                    plan.seen.merge(stamps(record, sets, kinds: kinds)) { _, new in new }
                }
                continue
            }
            guard finished else {
                // Finished here, not there: Finish again on top of whatever
                // revision the server holds.
                if mine.session.status == .finished {
                    let revision = max(records.session.revision ?? 1, mine.session.revision ?? 1) + 1
                    plan.finishes.append(OutboxPlan.finish(mine, revision: revision, codec: codec))
                }
                continue
            }
            if changed(record, sets, mine: mine, versions: versions, kinds: kinds) {
                plan.merges.append(records)
                plan.seen.merge(stamps(record, sets, kinds: kinds)) { _, new in new }
            }
        }

        for mine in local {
            let id = mine.id.rawValue
            guard server.sessions[id] == nil, !queued.contains(id), !tombstones.contains(id) else { continue }
            if versions["\(kinds.session)/\(id)"] != nil {
                plan.removals.append(mine.id)
            } else {
                plan.uploads.append(mine)
            }
        }
        return plan
    }

    private static func decode(_ session: FlatRecord, sets: [FlatRecord], codec: RecordCodec) throws(CodecError) -> SessionRecords {
        guard let records = try codec.sessionRecords(from: [session] + sets).first else {
            throw CodecError.invalid(kind: session.kind, id: session.id, property: "record", value: "not a session")
        }
        return records
    }

    /// The versions of a session's records, keyed as the outbox keys them.
    static func stamps(_ session: FlatRecord, _ sets: [FlatRecord], kinds: KindNames) -> [String: Int] {
        var out: [String: Int] = [:]
        for record in [session] + sets {
            if let version = record.version { out["\(record.kind)/\(record.id)"] = version }
        }
        return out
    }

    /// Whether another writer changed the session since the phone last wrote
    /// or read it: a record at a version the phone did not see, or a set
    /// added or removed.
    static func changed(_ session: FlatRecord, _ sets: [FlatRecord], mine: SessionRecords, versions: [String: Int], kinds: KindNames) -> Bool {
        if Set(sets.map(\.id)) != Set(mine.sets.map(\.id.rawValue)) { return true }
        for (key, version) in stamps(session, sets, kinds: kinds) where versions[key] != version {
            return true
        }
        return false
    }
}
