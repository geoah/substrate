/// A long collection in a presentation (a transcript, a task list), kept by
/// id with a revision (§5.5), so a change goes over the Android bridge as a
/// delta of the rows it touched instead of the whole list again. The
/// revision moves only when a row or the order changes.
public struct KeyedList<Row: Codable & Equatable & Sendable & Identifiable>: Codable, Equatable, Sendable where Row.ID == String {
    public private(set) var revision: Int
    public private(set) var rows: [Row]

    /// A row whose id came earlier in `rows` is left out.
    public init(_ rows: [Row] = [], revision: Int = 0) {
        self.revision = revision
        self.rows = Self.unique(rows)
    }

    public var ids: [String] { rows.map(\.id) }

    public subscript(id: String) -> Row? { rows.first { $0.id == id } }

    /// Replaces the rows, moving the revision when they differ.
    public mutating func update(_ rows: [Row]) {
        let unique = Self.unique(rows)
        guard unique != self.rows else { return }
        self.rows = unique
        revision += 1
    }

    /// What turns `base` into this list: the rows inserted and changed, the
    /// ids removed, and the order as ids.
    public func delta(from base: KeyedList) -> KeyedListDelta<Row> {
        var old: [String: Row] = [:]
        for row in base.rows { old[row.id] = row }
        var inserted: [Row] = []
        var changed: [Row] = []
        for row in rows {
            if let before = old.removeValue(forKey: row.id) {
                if before != row { changed.append(row) }
            } else {
                inserted.append(row)
            }
        }
        let removed = base.ids.filter { old[$0] != nil }
        return KeyedListDelta(base: base.revision, revision: revision, inserted: inserted, changed: changed, removed: removed,
                              order: ids)
    }

    /// Applies a delta made from this revision. One made from another is
    /// refused: the reader asks for the whole list again.
    public mutating func apply(_ delta: KeyedListDelta<Row>) throws {
        guard delta.base == revision else { throw KeyedListMismatch(revision: revision, base: delta.base) }
        var byID: [String: Row] = [:]
        for row in rows { byID[row.id] = row }
        for id in delta.removed { byID[id] = nil }
        for row in delta.inserted + delta.changed { byID[row.id] = row }
        var ordered: [Row] = []
        for id in delta.order {
            guard let row = byID[id] else { throw KeyedListMismatch(revision: revision, base: delta.base) }
            ordered.append(row)
        }
        rows = ordered
        revision = delta.revision
    }

    private static func unique(_ rows: [Row]) -> [Row] {
        var seen = Set<String>()
        return rows.filter { seen.insert($0.id).inserted }
    }
}

/// One step of a `KeyedList`, from the revision `base` to `revision`.
public struct KeyedListDelta<Row: Codable & Equatable & Sendable>: Codable, Equatable, Sendable {
    public var base: Int
    public var revision: Int
    public var inserted: [Row]
    public var changed: [Row]
    public var removed: [String]
    public var order: [String]

    public init(base: Int, revision: Int, inserted: [Row], changed: [Row], removed: [String], order: [String]) {
        self.base = base
        self.revision = revision
        self.inserted = inserted
        self.changed = changed
        self.removed = removed
        self.order = order
    }

    public var isEmpty: Bool { base == revision && inserted.isEmpty && changed.isEmpty && removed.isEmpty }
}

/// A delta made from another revision than the list's, or naming a row the
/// list cannot find.
public struct KeyedListMismatch: Error, Equatable, Sendable {
    public var revision: Int
    public var base: Int

    public init(revision: Int, base: Int) {
        self.revision = revision
        self.base = base
    }
}
