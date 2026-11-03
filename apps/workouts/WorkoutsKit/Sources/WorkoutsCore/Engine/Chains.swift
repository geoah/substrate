import Foundation

// Chains (§4.9): a structured set is a head and its segments, one record
// each. Drops, clusters and the second side are created with the head;
// myo, myo-match and rest-pause mini-sets appear one at a time, after each
// tick, until the chain's stop rule holds.

public extension Structure {
    /// Mini-sets appear one at a time after each tick.
    var growsOneAtATime: Bool {
        self == .myo || self == .myomatch || self == .restpause
    }
}

public extension SessionState {
    /// Every row of the chain is completed.
    func isChainComplete(_ id: RowID) -> Bool {
        chain(of: id).allSatisfy(\.isCompleted)
    }

    /// Whether a myo, myo-match or rest-pause chain has ended with the links
    /// completed so far (§4.9, 03 §11.6, §5.3). False for other structures
    /// and while the head is pending.
    func chainStops(_ id: RowID) -> Bool {
        guard let row = rows[id], let head = rows[row.headID], let headValues = head.performed?.values,
              head.structure.growsOneAtATime
        else { return false }
        let minis = chain(of: head.id).dropFirst().filter(\.isCompleted).map { $0.performed?.values.reps ?? 0 }
        let headReps = headValues.reps ?? 0
        let sum = minis.reduce(0, +)
        let exercise = exercise(ofRow: head.id)
        switch head.structure {
        case .myo:
            let cap = head.target?.miniSets ?? exercise?.resolvedMyoMaxMiniSets ?? 5
            if minis.count >= cap { return true }
            // 03 §11.6 counts the mini-sets alone towards a myo-rep total.
            if let total = head.target?.totalReps, sum >= total { return true }
            switch plannedSet(ofRow: head.id)?.myoStopRule ?? exercise?.resolvedMyoStopRule ?? .repdrop {
            case .repdrop:
                if let first = minis.first, minis.dropFirst().contains(where: { $0 < first }) { return true }
            case .belowtarget:
                // A quarter of the activation's reps, 3 to 5, halves rounded up.
                let floor = head.target?.miniSetReps ?? min(max((headReps + 2) / 4, 3), 5)
                if minis.contains(where: { $0 < floor }) { return true }
            }
            return false
        case .myomatch:
            // The table's rule: the total or the cap, never a rep drop (a
            // last mini-set cut to the remainder is short by design).
            let cap = head.target?.miniSets ?? exercise?.resolvedMyoMaxMiniSets ?? 5
            if minis.count >= cap { return true }
            if let total = matchTotal(of: head.id), headReps + sum >= total { return true }
            return false
        case .restpause:
            let cap = head.target?.miniSets ?? 2
            if minis.count >= cap { return true }
            if let total = head.target?.totalReps, headReps + sum >= total { return true }
            return false
        case .straight, .drop, .cluster:
            return false
        }
    }

    /// The total a myo-rep match aims for: the planned or resolved
    /// `totalReps`, else the reps of the slot's previous working set.
    func matchTotal(of head: RowID) -> Int? {
        rows[head]?.target?.totalReps ?? previousWorkingReps(before: head)
    }

    /// The reps of the slot's last completed working set before `head`.
    func previousWorkingReps(before head: RowID) -> Int? {
        guard let row = rows[head] else { return nil }
        let units = units(inSlot: row.slot)
        guard let mine = units.firstIndex(where: { $0.first == head }) else { return nil }
        return units[..<mine].reversed().lazy.compactMap { unit -> Int? in
            guard let earlier = rows[unit[0]], earlier.role == .working else { return nil }
            return earlier.performed?.values.reps
        }.first
    }
}

extension SessionState {
    /// What a tick does to its chain: a head resolves its myo-rep match total
    /// and its matched second side; "no lighter load" removes the drops left;
    /// a growing chain gets its next mini-set unless it stops.
    mutating func afterTick(_ id: RowID, noLighterLoad: Bool, ids: inout RowIDPool) throws(EngineError) {
        guard let row = rows[id] else { return }
        if row.isHead {
            resolveMatchTotal(id)
            matchSecondSide(id)
        }
        if noLighterLoad {
            let later = chain(of: id).filter { $0.chainPosition > row.chainPosition && $0.isPending && $0.structure == .drop && $0.side == nil }
            removeRows(Set(later.map(\.id)))
        }
        try growChain(row.headID, ids: &ids)
    }

    /// Adds the next mini-set of a growing chain whose links are all done,
    /// unless its stop rule holds.
    mutating func growChain(_ head: RowID, ids: inout RowIDPool) throws(EngineError) {
        guard let headRow = rows[head], headRow.structure.growsOneAtATime, headRow.side == nil, headRow.isCompleted,
              isChainComplete(head), !chainStops(head), let index = slotIndex(headRow.slot)
        else { return }
        let next = SessionEngine.segment(of: headRow, position: chain(of: head).count + 1, id: ids.next(),
                                         reps: headRow.target?.miniSetReps)
        try insert([next], inSlot: index, at: positionAfterChain(head) ?? slots[index].rows.count)
    }

    /// After an edit: a chain that now stops loses its pending mini-set; one
    /// that no longer stops gets one back (§4.3 `editSet`).
    mutating func recheckChain(_ head: RowID, ids: inout RowIDPool) throws(EngineError) {
        guard let headRow = rows[head], headRow.structure.growsOneAtATime else { return }
        if chainStops(head) {
            removeRows(Set(chain(of: head).filter { $0.isPending && !$0.isHead }.map(\.id)))
        } else {
            try growChain(head, ids: &ids)
        }
    }

    /// Removes a chain's pending mini-sets after `position` (an un-tick).
    mutating func removePendingMiniSets(of head: RowID, after position: Int) {
        let later = chain(of: head).filter { $0.chainPosition > position && $0.isPending && $0.structure.growsOneAtATime && $0.side == nil }
        removeRows(Set(later.map(\.id)))
    }

    /// Resolves a myo-rep match head's total into its target at its tick: the
    /// reps of the slot's previous working set (§4.5).
    mutating func resolveMatchTotal(_ head: RowID) {
        guard let row = rows[head], row.structure == .myomatch, row.target != nil, row.target?.totalReps == nil,
              let total = previousWorkingReps(before: head)
        else { return }
        rows[head]?.target?.totalReps = total
    }

    /// With `sideMatch`, the second side aims for what the first side did:
    /// its reps, or its time on a timed hold.
    mutating func matchSecondSide(_ head: RowID) {
        guard let row = rows[head], row.side != nil, let target = row.target, slot(row.slot)?.sideMatch == true,
              let values = row.performed?.values
        else { return }
        var matched = SetTarget(plannedSet: target.plannedSet)
        if exercise(ofRow: head)?.measurement.isTimedHold == true {
            guard let duration = values.duration else { return }
            matched.termination = .time
            matched.durationMin = duration
            matched.durationMax = duration
        } else {
            guard let reps = values.reps else { return }
            matched.termination = .reps
            matched.repsMin = reps
            matched.repsMax = reps
        }
        for second in chain(of: head).dropFirst() where second.isPending {
            rows[second.id]?.target = matched
        }
    }

    /// Undoes `matchSecondSide` when the first side is un-ticked.
    mutating func unmatchSecondSide(_ head: RowID) {
        guard let row = rows[head], row.side != nil, let target = row.target, slot(row.slot)?.sideMatch == true else { return }
        for second in chain(of: head).dropFirst() where second.isPending {
            rows[second.id]?.target = SetTarget(plannedSet: target.plannedSet)
        }
    }

    /// Gives a head a new structure: segments of the old one go, a drop set
    /// gets its drops at once, and a completed head grows its first mini-set.
    mutating func restructure(_ head: RowID, to structure: Structure, ids: inout RowIDPool) throws(EngineError) {
        guard let headRow = rows[head], let index = slotIndex(headRow.slot) else { return }
        let old = chain(of: head).filter { !$0.isHead && $0.side == nil }
        removeRows(Set(old.map(\.id)))
        rows[head]?.structure = structure
        switch structure {
        case .drop:
            let count = headRow.target?.drops ?? 1
            let reps = plannedSet(ofRow: head)?.dropReps
            let drops = (0 ..< count).map { offset in
                SessionEngine.segment(of: rows[head]!, position: offset + 2, id: ids.next(), reps: reps)
            }
            try insert(drops, inSlot: index, at: positionAfterChain(head) ?? slots[index].rows.count)
        case .myo, .myomatch, .restpause:
            resolveMatchTotal(head)
            try growChain(head, ids: &ids)
        case .straight, .cluster:
            break
        }
    }
}
