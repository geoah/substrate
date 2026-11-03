import Foundation

/// Where a task or a list sits among its neighbours: a decimal, so a move
/// writes the one record moved, with a number between its new neighbours
/// (apps/tasks/package/README.md). Halving a decimal is exact.
public enum Position {
    /// A number between `before` and `after`, either of which may be
    /// missing: after the last, before the first, or the first of all.
    public static func between(_ before: Decimal?, _ after: Decimal?) -> Decimal {
        switch (before, after) {
        case let (before?, after?): (before + after) / 2
        case let (before?, nil): before + 1
        case let (nil, after?): after - 1
        case (nil, nil): 1
        }
    }

    /// The positions to write so `moved` sits at `index` of `order` (the ids
    /// as drawn, `moved` among them), given each one's position now. One
    /// write where both neighbours have positions; where one has none (a
    /// record written elsewhere without one), the ones before the moved
    /// record that lack a position get one first, in their order, so the
    /// order drawn is the order kept.
    public static func moves(_ moved: String, to index: Int, in order: [String], positions: [String: Decimal]) -> [String: Decimal] {
        var ids = order.filter { $0 != moved }
        let at = max(0, min(index, ids.count))
        ids.insert(moved, at: at)
        var known = positions
        var writes: [String: Decimal] = [:]
        // Every record up to the moved one keeps or gets a position in order.
        var last: Decimal?
        for id in ids[..<at] {
            if let position = known[id], last.map({ position > $0 }) ?? true {
                last = position
                continue
            }
            let next = (last ?? 0) + 1
            known[id] = next
            writes[id] = next
            last = next
        }
        let floor = last
        let after = ids[(at + 1)...].lazy.compactMap { known[$0] }.first { position in floor.map { position > $0 } ?? true }
        writes[moved] = between(last, after)
        return writes
    }
}
