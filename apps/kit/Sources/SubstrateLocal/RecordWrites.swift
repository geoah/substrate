import Foundation
import SubstrateCore

/// The outbox writes an owner's change makes, each in the batch of the
/// record it writes, so a refusal holds back that record's later writes and
/// never another's.
///
/// - A create is a PUT with `ifVersion: 0` at an id the device minted. A
///   retry whose earlier try landed, its answer lost, meets the row there and
///   counts as landed: the row is read, never written again, so a change
///   another device made since stands. It names no state, which moves only
///   by patch.
/// - A change is a patch of what changed, guarded by the version the outbox
///   last saw, read again on a conflict and sent once more on top. A record
///   not sent yet has no version, and its patch follows its create in the
///   batch.
/// - An edit is a patch on the version the owner saw, never merged over: a
///   conflict parks it for the owner. A record not sent yet was seen at 0,
///   which the outbox moves to the version its create made, so an edit
///   queued behind a create parks if another device moved the record first.
/// - A delete is restorable (no purge), guarded the same way, and replaces
///   the record's writes still waiting.
public struct RecordWrites {
    public let mint: () -> UUID

    public init(mint: @escaping () -> UUID) {
        self.mint = mint
    }

    /// A new record's id: a lowercase UUID, time-ordered where the ids are.
    public func newID() -> String { mint().uuidString.lowercased() }

    public func create(_ kind: String, _ id: String, properties: JSONObject) -> LocalWrite {
        LocalWrite(entries: [
            OutboxEntry(
                id: mint(), method: .put, kind: kind, recordID: id, properties: properties,
                batch: LocalWrite.batch(kind, id), baseVersion: 0)
        ])
    }

    /// Nothing, where nothing changed.
    public func change(_ kind: String, _ id: String, properties: JSONObject, version: Int?) -> LocalWrite {
        guard !properties.isEmpty else { return LocalWrite(entries: []) }
        return LocalWrite(entries: [
            OutboxEntry(
                id: mint(), method: .patch, kind: kind, recordID: id, properties: properties,
                guarded: version != nil, batch: LocalWrite.batch(kind, id))
        ])
    }

    /// Nothing, where nothing changed.
    public func edit(_ kind: String, _ id: String, properties: JSONObject, seen version: Int?) -> LocalWrite {
        guard !properties.isEmpty else { return LocalWrite(entries: []) }
        return LocalWrite(entries: [
            OutboxEntry(
                id: mint(), method: .patch, kind: kind, recordID: id, properties: properties,
                batch: LocalWrite.batch(kind, id), baseVersion: version ?? 0)
        ])
    }

    public func delete(_ kind: String, _ id: String, version: Int?) -> LocalWrite {
        let batch = LocalWrite.batch(kind, id)
        return LocalWrite(
            entries: [OutboxEntry(id: mint(), method: .delete, kind: kind, recordID: id, guarded: version != nil, batch: batch)],
            replacing: batch)
    }
}
