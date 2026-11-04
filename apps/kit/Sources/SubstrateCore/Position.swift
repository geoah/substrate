import Foundation

/// Where a record sits among its neighbours (a task in its list, a list among
/// the owner's): a decimal, so a move writes the one record moved, with the
/// shortest number strictly between its new neighbours. A gap that has run
/// out of room (a number past `digitLimit` significant digits, or two
/// neighbours two devices gave the same number) has its neighbours spread out
/// again in the same writes, so the order drawn is always the order kept.
public enum Position {
    /// The most significant digits a position is written with: well inside a
    /// `Decimal`'s 38, so a midpoint never rounds onto a neighbour, and
    /// small enough that every system's decimal reads it back exactly.
    public static let digitLimit = 20

    /// A number after `before` and before `after`, either of which may be
    /// missing: after the last, before the first, or the first of all. Where
    /// the two leave no room within `digitLimit`, their exact midpoint, which
    /// `moves` spreads out instead of writing.
    public static func between(_ before: Decimal?, _ after: Decimal?) -> Decimal {
        if let fit = fit(before, after) { return fit }
        guard let before, let after else { return 1 }
        return (before + after) / 2
    }

    /// The shortest number strictly between the two within `digitLimit`
    /// digits, nil where there is none (equal neighbours, or a gap used up).
    public static func fit(_ before: Decimal?, _ after: Decimal?) -> Decimal? {
        switch (before, after) {
        case let (before?, nil): return before + 1
        case let (nil, after?): return after - 1
        case (nil, nil): return 1
        case let (before?, after?):
            guard before < after else { return nil }
            let middle = (before + after) / 2
            for scale in 0 ... digitLimit {
                let candidate = rounded(middle, scale)
                if candidate > before, candidate < after, digits(candidate) <= digitLimit { return candidate }
            }
            return nil
        }
    }

    /// The positions to write so `moved` sits at `index` of `order` (the ids
    /// as drawn, `moved` among them), given each one's position now. One
    /// write where the neighbours leave room. A record before the moved one
    /// with no position (written elsewhere without one) gets one first, in
    /// its order; one after it whose number no longer sits above it (two
    /// devices that appended at once) gets a number above it too; and a gap
    /// with no room left has the records around the moved one spread out.
    public static func moves(_ moved: String, to index: Int, in order: [String], positions: [String: Decimal]) -> [String: Decimal] {
        var ids = order.filter { $0 != moved }
        let at = max(0, min(index, ids.count))
        ids.insert(moved, at: at)
        var known = positions
        known[moved] = nil
        var writes: [String: Decimal] = [:]
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
        // The moved record, then each after it that does not sit above the
        // one before it, up to the first that does: the rest keep theirs.
        for offset in ids.indices.dropFirst(at) {
            let id = ids[offset]
            if offset > at {
                guard let position = known[id] else { break }
                if last.map({ position > $0 }) ?? true { break }
            }
            let ceiling = ids[(offset + 1)...].lazy.compactMap { known[$0] }.first { position in last.map { position > $0 } ?? true }
            guard let next = fit(last, ceiling) else { return spread(ids, around: at, known: positions) }
            known[id] = next
            writes[id] = next
            last = next
        }
        return writes
    }

    /// Numbers for the records around `index`, evenly between the nearest
    /// neighbours that leave room for them, widening one record each side
    /// until they do; at the widest, every record of the order, from 1.
    static func spread(_ ids: [String], around index: Int, known positions: [String: Decimal]) -> [String: Decimal] {
        var known = positions
        known[ids[index]] = nil
        var low = index
        var high = index + 1
        while true {
            let floor = low > 0 ? known[ids[low - 1]] : nil
            let ceiling = high < ids.count ? known[ids[high]] : nil
            if let values = evenly(count: high - low, after: floor, before: ceiling) {
                var writes: [String: Decimal] = [:]
                for (id, value) in zip(ids[low ..< high], values) where positions[id] != value || id == ids[index] {
                    writes[id] = value
                }
                return writes
            }
            if low == 0, high == ids.count { return Dictionary(uniqueKeysWithValues: ids.enumerated().map { ($1, Decimal($0 + 1)) }) }
            low = max(0, low - 1)
            high = min(ids.count, high + 1)
        }
    }

    /// `count` numbers strictly increasing, strictly between the two bounds,
    /// each within `digitLimit` digits; nil where they do not fit.
    static func evenly(count: Int, after floor: Decimal?, before ceiling: Decimal?) -> [Decimal]? {
        var values: [Decimal] = []
        switch (floor, ceiling) {
        case (nil, nil): values = (1 ... count).map { Decimal($0) }
        case let (floor?, nil): values = (1 ... count).map { floor + Decimal($0) }
        case let (nil, ceiling?): values = (1 ... count).reversed().map { ceiling - Decimal($0) }
        case let (floor?, ceiling?):
            guard floor < ceiling else { return nil }
            let step = (ceiling - floor) / Decimal(count + 1)
            var previous = floor
            // Each the shortest number within half a step of its even place.
            for n in 1 ... count {
                let target = floor + step * Decimal(n)
                guard let value = fit(max(previous, target - step / 2), min(ceiling, target + step / 2)) else { return nil }
                values.append(value)
                previous = value
            }
        }
        var previous = floor
        for value in values {
            if let previous, value <= previous { return nil }
            if digits(value) > digitLimit { return nil }
            previous = value
        }
        if let ceiling, let top = values.last, top >= ceiling { return nil }
        return values
    }

    /// `value` rounded to `scale` digits after the point.
    static func rounded(_ value: Decimal, _ scale: Int) -> Decimal {
        var input = value
        var output = Decimal()
        NSDecimalRound(&output, &input, scale, .plain)
        return output
    }

    /// The significant digits `value` is written with: 1.05 has three,
    /// 0.005 one.
    static func digits(_ value: Decimal) -> Int {
        let text = DecimalText.format(value).filter(\.isNumber)
        return max(1, text.drop { $0 == "0" }.count)
    }
}
