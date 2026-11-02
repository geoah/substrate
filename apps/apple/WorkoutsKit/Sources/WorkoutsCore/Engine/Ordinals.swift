import Foundation

/// Ordinals and orders (§4.10).
///
/// - **Ordinal**: a row's number among its slot's rows of the same role, in
///   display order, a chain counted once (its segments share the head's).
///   During the session it counts every row, so the grid shows 1, 2, 3 as
///   planned; at Finish it counts performed rows only.
/// - **Order**: a row's place among its slot's performed rows in display
///   order, from 1, each record of a chain counting.
public enum Ordinals {
    /// Every row's ordinal in a slot. With `performedOnly`, only performed
    /// rows are numbered and counted.
    public static func ordinals(inSlot key: String, of state: SessionState, performedOnly: Bool = false) -> [RowID: Int] {
        var result: [RowID: Int] = [:]
        var counters: [Role: Int] = [:]
        for unit in state.units(inSlot: key) {
            guard let head = state.rows[unit[0]] else { continue }
            if performedOnly, !head.isCompleted { continue }
            let ordinal = (counters[head.role] ?? 0) + 1
            counters[head.role] = ordinal
            for id in unit where !performedOnly || state.rows[id]?.isCompleted == true {
                result[id] = ordinal
            }
        }
        return result
    }

    /// One row's session ordinal.
    public static func ordinal(of id: RowID, in state: SessionState) -> Int? {
        guard let row = state.rows[id] else { return nil }
        return ordinals(inSlot: row.slot, of: state)[id]
    }

    /// Each performed row's order in its slot, from 1.
    public static func orders(inSlot key: String, of state: SessionState) -> [RowID: Int] {
        let performed = state.units(inSlot: key).joined().filter { state.rows[$0]?.isCompleted == true }
        return Dictionary(uniqueKeysWithValues: performed.enumerated().map { ($1, $0 + 1) })
    }

    /// A segment's place after its head: 1 for the first drop, mini-set,
    /// cluster or second side. Nil on a head.
    public static func segmentIndex(of id: RowID, in state: SessionState) -> Int? {
        guard let row = state.rows[id], !row.isHead else { return nil }
        return state.chain(of: id).firstIndex { $0.id == id }
    }

    /// The number of warm-up units in a slot (a lone warm-up is spoken
    /// "warm-up", not "warm-up 1").
    public static func warmupCount(inSlot key: String, of state: SessionState) -> Int {
        state.units(inSlot: key).count { state.rows[$0[0]]?.role == .warmup }
    }
}

public extension SessionState {
    /// A row's session ordinal (§4.10).
    func ordinal(of id: RowID) -> Int? {
        Ordinals.ordinal(of: id, in: self)
    }
}

/// A row's label (req 37): the short form the set grid shows and the spoken
/// form the Live Activity, notifications and VoiceOver use (§4.10).
public struct SetLabel: Equatable, Sendable {
    /// `W`, `2`, `2a`, `M`, `1L`.
    public var short: String
    /// `warm-up 2`, `set 2`, `set 2, drop 1`, `set 1, left side`.
    public var spoken: String
    /// A failure or AMRAP set shows a red F badge beside its number.
    public var failureBadge: Bool
    /// "Top" or "Back-off", in small text under the number.
    public var roleText: String?

    public init(short: String, spoken: String, failureBadge: Bool = false, roleText: String? = nil) {
        self.short = short
        self.spoken = spoken
        self.failureBadge = failureBadge
        self.roleText = roleText
    }

    /// The label of a row as the session stands.
    public static func make(_ id: RowID, in state: SessionState) -> SetLabel? {
        guard let row = state.rows[id], let head = state.rows[row.headID], let ordinal = state.ordinal(of: id) else { return nil }
        let warmup = head.role == .warmup
        let number = warmup ? "W" : "\(ordinal)"
        let spokenSet = warmup
            ? (Ordinals.warmupCount(inSlot: row.slot, of: state) > 1 ? "warm-up \(ordinal)" : "warm-up")
            : "set \(ordinal)"
        let badge = row.isHead && (head.termination == .failure || head.termination == .amrap)
        let roleText = row.isHead ? head.roleLabel.map { $0 == .top ? "Top" : "Back-off" } : nil
        if let side = row.side {
            let letter = side == .left ? "L" : "R"
            return SetLabel(short: number + letter, spoken: "\(spokenSet), \(side.rawValue) side", failureBadge: badge, roleText: roleText)
        }
        guard let index = Ordinals.segmentIndex(of: id, in: state) else {
            return SetLabel(short: number, spoken: spokenSet, failureBadge: badge, roleText: roleText)
        }
        // Segments: 2a, 2b for drops, rest-pause mini-sets and clusters; M for myo mini-sets.
        let letter = String(UnicodeScalar(UInt8(ascii: "a") + UInt8(min(index - 1, 25))))
        switch head.structure {
        case .drop: return SetLabel(short: number + letter, spoken: "\(spokenSet), drop \(index)")
        case .restpause: return SetLabel(short: number + letter, spoken: "\(spokenSet), mini-set \(index)")
        // The head is the first cluster.
        case .cluster: return SetLabel(short: number + letter, spoken: "\(spokenSet), cluster \(index + 1)")
        case .myo, .myomatch: return SetLabel(short: "M", spoken: "\(spokenSet), mini-set \(index)")
        case .straight: return SetLabel(short: number + letter, spoken: spokenSet)
        }
    }
}
