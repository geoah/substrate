import Foundation
import SubstrateCore

/// A keyed list as it crosses (docs/plans/app-kit.md §5.5): whole at a
/// revision the first time and after a `resync`, then each change as a delta
/// from the revision before it, so a streamed reply re-encodes one row, not
/// the transcript. Kotlin applies deltas in revision order and asks for the
/// list whole when one's base is not its revision.
public enum ListMessage: Bridged, Equatable {
    case whole(list: String, revision: Int, rows: [ListRow])
    case delta(list: String, base: Int, revision: Int, inserted: [ListRow], changed: [ListRow], removed: [String],
               order: [String])
}

/// One row: its id and its value as the row's own type encodes it.
public struct ListRow: Bridged, Equatable {
    public var id: String
    public var value: JSONValue

    public init(id: String, value: JSONValue) {
        self.id = id
        self.value = value
    }
}

/// Kotlin's ask for a list whole again (`resync`).
public struct ResyncRequest: Bridged, Equatable {
    public var list: String

    public init(list: String) {
        self.list = list
    }
}

/// One handle's keyed lists: what each last sent, to tell the next rows'
/// changes from it by value, encoding only the rows that changed.
final class KeyedLists: @unchecked Sendable {
    private struct Sent {
        var revision: Int
        var order: [String]
        var rows: [String: any Encodable & Sendable]
    }

    private let handle: Handle
    private let outbox: HandleOutbox
    private let lock = NSLock()
    private var sent: [String: Sent] = [:]

    init(handle: Handle, outbox: HandleOutbox) {
        self.handle = handle
        self.outbox = outbox
    }

    func publish<Row: Encodable & Equatable & Sendable>(_ list: String, rows: [Row], id: (Row) -> String) {
        let order = rows.map(id)
        guard Set(order).count == order.count else {
            outbox.enqueue(BridgeCodec.failure(BridgeFailure(.swiftFailure, "keyed list \(list) has two rows with one id"),
                                               handle: handle))
            return
        }
        // Encoded and queued under the lock, so two publishes of one list
        // reach the queue in the order their revisions were given.
        lock.withLock {
            let byID = Dictionary(uniqueKeysWithValues: zip(order, rows.map { $0 as any Encodable & Sendable }))
            guard let before = sent[list] else {
                sent[list] = Sent(revision: 1, order: order, rows: byID)
                enqueueWhole(list, revision: 1, order: order, rows: byID)
                return
            }
            var inserted: [OutRow] = []
            var changed: [OutRow] = []
            for (key, row) in zip(order, rows) {
                switch before.rows[key] {
                case nil: inserted.append(OutRow(id: key, value: row))
                case let old? where (old as? Row) != row: changed.append(OutRow(id: key, value: row))
                default: break
                }
            }
            let kept = Set(order)
            let removed = before.order.filter { !kept.contains($0) }
            guard !inserted.isEmpty || !changed.isEmpty || !removed.isEmpty || order != before.order else { return }
            let revision = before.revision + 1
            sent[list] = Sent(revision: revision, order: order, rows: byID)
            let delta = DeltaOut(list: list, base: before.revision, revision: revision, inserted: inserted, changed: changed,
                                 removed: removed, order: order)
            enqueue(list, Cased(delta: delta))
        }
    }

    /// Kotlin lost its place: the list again, whole, at its revision now.
    func resync(_ list: String) {
        lock.withLock {
            guard let current = sent[list] else { return }
            enqueueWhole(list, revision: current.revision, order: current.order, rows: current.rows)
        }
    }

    private func enqueueWhole(_ list: String, revision: Int, order: [String], rows: [String: any Encodable & Sendable]) {
        let out = order.compactMap { key in rows[key].map { OutRow(id: key, value: $0) } }
        enqueue(list, Cased(whole: WholeOut(list: list, revision: revision, rows: out)))
    }

    private func enqueue(_ list: String, _ message: Cased) {
        do {
            outbox.enqueue(try BridgeCodec.encode(op: .list, handle: handle, body: message))
        } catch {
            outbox.enqueue(BridgeCodec.failure(BridgeFailure(.swiftFailure, "keyed list \(list) did not encode: \(BridgeCodec.describe(error))"),
                                               handle: handle))
        }
    }

    // `ListMessage`'s shape, with each row encoded by its own type instead
    // of through a `JSONValue`; `BridgeTests` holds the two equal.
    private struct OutRow: Encodable {
        var id: String
        var value: any Encodable & Sendable

        enum CodingKeys: String, CodingKey { case id, value }

        func encode(to encoder: any Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encode(id, forKey: .id)
            try container.encode(value, forKey: .value)
        }
    }

    private struct WholeOut: Encodable {
        var list: String
        var revision: Int
        var rows: [OutRow]
    }

    private struct DeltaOut: Encodable {
        var list: String
        var base: Int
        var revision: Int
        var inserted: [OutRow]
        var changed: [OutRow]
        var removed: [String]
        var order: [String]
    }

    private struct Cased: Encodable {
        var whole: WholeOut?
        var delta: DeltaOut?
    }
}
