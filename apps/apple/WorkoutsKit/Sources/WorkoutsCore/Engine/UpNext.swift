import Foundation

/// Up next (§4.6): the one row the session points at. The Live Activity, the
/// notification, the rest bar and the reason line all describe it.
public enum UpNext {
    /// The plan sequence: slots in display order; a plain slot's rows in
    /// display order; a group as one block, every member's warm-ups first,
    /// then round 1, round 2 and so on, a member dropping out of the rounds
    /// it has no working set for (02 §3.3).
    public static func sequence(_ state: SessionState) -> [RowID] {
        var sequence: [RowID] = []
        var placed: Set<String> = []
        for slot in state.slots {
            guard let key = slot.group else {
                sequence.append(contentsOf: slot.rows)
                continue
            }
            guard !placed.contains(key) else { continue }
            placed.insert(key)
            let units = lanes(ofGroup: key, in: state)
            for memberUnits in units {
                for unit in memberUnits where state.rows[unit[0]]?.role == .warmup {
                    sequence.append(contentsOf: unit)
                }
            }
            let working = units.map { $0.filter { state.rows[$0[0]]?.role != .warmup } }
            let roundCount = working.map(\.count).max() ?? 0
            for round in 0 ..< roundCount {
                for memberUnits in working where round < memberUnits.count {
                    sequence.append(contentsOf: memberUnits[round])
                }
            }
        }
        return sequence
    }

    /// Up next: the focused pending row, else the first pending row after the
    /// last completed one, wrapping around; nil when every row is done.
    public static func row(in state: SessionState) -> RowID? {
        if let focus = state.focus, let row = state.rows[focus], row.isPending, !state.isLocked(focus) {
            return focus
        }
        return expectedNext(after: state.lastCompleted, in: state)
    }

    /// Rule 2 alone: the first pending, unlocked row after `anchor` in the plan
    /// sequence, wrapping around; with no anchor, the first. The rest rule
    /// reads it with the row just ticked as the anchor (§4.7).
    public static func expectedNext(after anchor: RowID?, in state: SessionState) -> RowID? {
        let sequence = sequence(state)
        let start = anchor.flatMap { sequence.firstIndex(of: $0) }.map { $0 + 1 } ?? 0
        let ordered = sequence[start...] + sequence[..<start]
        return ordered.first { id in
            guard let row = state.rows[id], row.isPending, id != anchor else { return false }
            return !state.isLocked(id)
        }
    }

    /// The round a row belongs to inside its group block: its working head's
    /// place among its lane's working units; 0 for warm-ups. Outside a group,
    /// the head's ordinal.
    public static func round(of id: RowID, in state: SessionState) -> Int {
        guard let row = state.rows[id], let head = state.rows[row.headID], head.role == .working else { return 0 }
        if let group = state.slot(row.slot)?.group {
            for lane in lanes(ofGroup: group, in: state) {
                let working = lane.filter { state.rows[$0[0]]?.role != .warmup }
                if let index = working.firstIndex(where: { $0.first == head.id }) { return index + 1 }
            }
        }
        return Ordinals.ordinal(of: head.id, in: state) ?? 0
    }

    /// A group's lanes in display order, each its units in display order. A
    /// slot that took over another's pending sets (a replace after a done
    /// set) shares that slot's lane, after it, so its sets keep their rounds.
    static func lanes(ofGroup key: String, in state: SessionState) -> [[[RowID]]] {
        var order: [String] = []
        var lanes: [String: [[RowID]]] = [:]
        for member in state.members(ofGroup: key) {
            if lanes[member.lane] == nil { order.append(member.lane) }
            lanes[member.lane, default: []] += state.units(inSlot: member.key)
        }
        return order.map { lanes[$0] ?? [] }
    }
}

public extension SessionState {
    /// The row the session points at (§4.6).
    var upNext: RowID? { UpNext.row(in: self) }
}
