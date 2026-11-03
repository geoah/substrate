import SubstrateCore
import SubstrateLocal

/// The records as the owner sees them: the server's as last read, with the
/// writes still waiting in the outbox laid over in their order, so a change
/// made offline shows at once and stays shown when the server's copy of the
/// record is read again before the write lands. A write the server refused
/// is not laid over: the record shows as the server holds it while the
/// Status screen offers Retry and Discard.
public enum RecordFold {
    /// `kind/id`, how the fold and the outbox key a record.
    public static func key(_ kind: String, _ id: String) -> String { "\(kind)/\(id)" }

    public static func fold(_ server: [String: Record], _ queue: [OutboxEntry]) -> [String: Record] {
        var out = server
        for entry in queue where !entry.parked && TasksKinds.all.contains(entry.kind) {
            out[entry.key] = apply(entry, to: out[entry.key])
        }
        return out
    }

    /// One write over a record: a put or a patch merges (a patch's null
    /// clears), a delete removes. A put over nothing is a record not sent
    /// yet, at version 0; a patch over nothing is of a record already gone.
    public static func apply(_ entry: OutboxEntry, to record: Record?) -> Record? {
        switch entry.method {
        case .purge, .delete:
            return nil
        case .patch where record == nil:
            return nil
        case .put, .patch:
            var next = record ?? Record(id: entry.recordID, kind: entry.kind, properties: [:], version: 0)
            for (name, value) in entry.properties ?? JSONObject() {
                next.properties[name] = value == .null ? nil : value
            }
            return next
        }
    }
}
