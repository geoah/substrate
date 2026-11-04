import Foundation
import SubstrateCore
import SubstrateRuntime

/// A keyed list as it crosses (docs/plans/app-kit.md §5.5): whole at a
/// revision the first time and after a `resync`, then each change as a delta
/// from the revision before it, so a reply arriving in parts re-encodes one row, not
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

/// One handle's keyed lists: the `KeyedList` each last sent, so the next is
/// sent as its `KeyedListDelta` from it, encoding only the rows that changed.
/// The revisions are the lists' own (`SubstrateRuntime`'s).
final class KeyedLists: @unchecked Sendable {
    private struct Sent {
        var revision: Int
        var list: any Sendable
        var whole: () throws -> String
    }

    private let handle: Handle
    private let outbox: HandleOutbox
    private let lock = NSLock()
    private var sent: [String: Sent] = [:]

    init(handle: Handle, outbox: HandleOutbox) {
        self.handle = handle
        self.outbox = outbox
    }

    func publish<Row>(_ name: String, _ list: KeyedList<Row>) {
        let handle = handle
        let whole: () throws -> String = {
            try BridgeCodec.encode(op: .list, handle: handle, body: Cased(whole: WholeOut(list: name, revision: list.revision,
                                                                                            rows: list.rows.map { OutRow(id: $0.id, value: $0) })))
        }
        // Encoded and queued under the lock, so two publishes of one list
        // reach the queue in revision order.
        lock.withLock {
            let before = sent[name]
            if let before, before.revision == list.revision { return }
            sent[name] = Sent(revision: list.revision, list: list, whole: whole)
            do {
                if let previous = before?.list as? KeyedList<Row>, previous.revision < list.revision {
                    let delta = list.delta(from: previous)
                    outbox.enqueue(try BridgeCodec.encode(op: .list, handle: handle,
                                                          body: Cased(delta: DeltaOut(list: name, delta: delta))))
                } else {
                    outbox.enqueue(try whole())
                }
            } catch {
                outbox.enqueue(BridgeCodec.failure(BridgeFailure(.swiftFailure, "keyed list \(name) did not encode: \(BridgeCodec.describe(error))"),
                                                   handle: handle))
            }
        }
    }

    /// Kotlin lost its place: the list again, whole, at its revision now.
    func resync(_ name: String) {
        lock.withLock {
            guard let current = sent[name] else { return }
            do {
                outbox.enqueue(try current.whole())
            } catch {
                outbox.enqueue(BridgeCodec.failure(BridgeFailure(.swiftFailure, "keyed list \(name) did not encode"), handle: handle))
            }
        }
    }

    // `ListMessage`'s shape, each row encoded by its own type rather than
    // through a `JSONValue`, the delta's fields `KeyedListDelta`'s own;
    // `RoundTripTests` holds the two equal.
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

    /// `list` beside the delta's own keys, in one object.
    private struct DeltaOut<Row: Codable & Equatable & Sendable & Identifiable>: Encodable where Row.ID == String {
        var list: String
        var delta: KeyedListDelta<Row>

        enum CodingKeys: String, CodingKey { case list, base, revision, inserted, changed, removed, order }

        func encode(to encoder: any Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encode(list, forKey: .list)
            try container.encode(delta.base, forKey: .base)
            try container.encode(delta.revision, forKey: .revision)
            try container.encode(delta.inserted.map(Self.row), forKey: .inserted)
            try container.encode(delta.changed.map(Self.row), forKey: .changed)
            try container.encode(delta.removed, forKey: .removed)
            try container.encode(delta.order, forKey: .order)
        }

        private static func row(_ row: Row) -> OutRow {
            OutRow(id: row.id, value: row)
        }
    }

    private struct Cased: Encodable {
        var whole: WholeOut?
        var delta: AnyDelta?

        init(whole: WholeOut) {
            self.whole = whole
        }

        init(delta: some Encodable & Sendable) {
            self.delta = AnyDelta(delta)
        }
    }

    private struct AnyDelta: Encodable {
        let value: any Encodable & Sendable

        init(_ value: any Encodable & Sendable) {
            self.value = value
        }

        func encode(to encoder: any Encoder) throws {
            try value.encode(to: encoder)
        }
    }
}
