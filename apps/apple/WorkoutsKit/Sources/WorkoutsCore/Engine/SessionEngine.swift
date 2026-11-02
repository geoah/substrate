import Foundation

/// The active session's journal: the engine's events and the state they fold to.
public typealias ActiveSessionJournal = SessionJournal<EventEnvelope, SessionState>

/// The session engine (§4): a pure reducer. It reads no clock, no files and
/// no history; everything it needs is in the state or the event, so folding
/// the journal's events always gives the same state.
public enum SessionEngine {
    /// Applies one event. `state` is nil before `start`; `discard` returns the
    /// state unchanged and the controller then deletes the journal.
    public static func apply(_ envelope: EventEnvelope, to state: SessionState?) throws(EngineError) -> SessionState {
        let next: SessionState
        if case let .start(start) = envelope.event {
            guard state == nil else { throw .sessionActive }
            next = try begin(start, at: envelope.at, eventID: envelope.id)
        } else {
            guard let state else { throw .noSession }
            guard state.phase == .active else { throw .finished }
            var reduction = Reduction(state: state, at: envelope.at, eventID: envelope.id)
            try reduction.apply(envelope.event)
            var reduced = reduction.state
            if envelope.event.touchesSessionRecord { reduced.revision += 1 }
            next = reduced
        }
        assert(invariantViolations(next).isEmpty, "session invariants broken: \(invariantViolations(next))")
        return next
    }

    /// Folds a journal's events into the state they describe.
    public static func fold(_ events: [EventEnvelope]) throws(EngineError) -> SessionState {
        var state: SessionState?
        for envelope in events {
            state = try apply(envelope, to: state)
        }
        guard let state else { throw .noSession }
        return state
    }

    /// Start: the header, the plan snapshot, slots and groups from the
    /// routine, and rows from its planned sets (§4.3, §4.5).
    static func begin(_ start: SessionEvent.Start, at: Date, eventID: EventID) throws(EngineError) -> SessionState {
        let exercises = Dictionary(start.exercises.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        let routine = start.routine
        var state = SessionState(
            session: SessionHeader(
                id: start.session,
                at: at,
                routine: routine?.id,
                routineVersion: routine?.resolvedVersion,
                deload: routine?.deload,
                activityType: routine?.activityType,
                utcOffsetStart: start.utcOffset,
                timezone: start.timeZone,
                bodyMass: start.bodyMass
            ),
            plan: routine,
            exercises: exercises,
            settings: start.settings,
            revision: 1
        )
        var ids = RowIDPool(start.rowIDs, prefix: eventID.rawValue)
        let groupKeys = Set(routine?.groups.map(\.key) ?? [])
        for routineSlot in routine?.slots ?? [] {
            // A slot whose exercise did not decode is left out rather than
            // blocking the whole routine; the preview lists it as unreadable.
            guard let exercise = exercises[routineSlot.exercise] else { continue }
            guard state.slotIndex(routineSlot.key) == nil else { throw .duplicateID }
            state.slots.append(SlotState(
                key: routineSlot.key,
                exercise: routineSlot.exercise,
                group: routineSlot.group.flatMap { groupKeys.contains($0) ? $0 : nil },
                routineSlot: routineSlot.key,
                sideOrder: routineSlot.sideOrder,
                sideMatch: routineSlot.sideMatch
            ))
            let index = state.slots.count - 1
            let side = firstSide(routineSlot.sideOrder, resolved: start.firstSides[routineSlot.key])
            for planned in routineSlot.sets {
                let new = rows(for: planned, slot: routineSlot.key, exercise: exercise, firstSide: side, ids: &ids,
                               bodyMass: start.bodyMass)
                try state.insert(new, inSlot: index, at: state.slots[index].rows.count)
            }
            state.sortWarmupsFirst(slot: routineSlot.key)
        }
        for group in routine?.groups ?? [] where state.group(group.key) == nil {
            state.groups.append(GroupState(key: group.key, groupKind: group.groupKind, pairing: group.pairing))
        }
        state.gatherGroups()
        state.pruneGroups()
        return state
    }

    /// Everything §4.4 says must hold after every event, as messages; empty
    /// when the state is sound. Invariant 1 is the controller's (one journal)
    /// and 9 its clock clamp; 10 is checked by folding.
    public static func invariantViolations(_ state: SessionState) -> [String] {
        var problems: [String] = []
        // 2. Unique slot keys; each row in exactly one slot, once.
        let keys = state.slots.map(\.key)
        if Set(keys).count != keys.count { problems.append("slot keys repeat: \(keys)") }
        var placed: [RowID: String] = [:]
        for slot in state.slots {
            for id in slot.rows {
                if placed[id] != nil { problems.append("row \(id) appears twice") }
                placed[id] = slot.key
                guard let row = state.rows[id] else {
                    problems.append("row \(id) of slot \(slot.key) does not exist")
                    continue
                }
                if row.slot != slot.key { problems.append("row \(id) names slot \(row.slot) but sits in \(slot.key)") }
            }
        }
        if Set(placed.keys) != Set(state.rows.keys) { problems.append("some rows sit in no slot") }
        // 3. Warm-ups first; each head followed directly by its segments in order.
        for slot in state.slots {
            var working = false
            var head: Row?
            var position = 1
            for id in slot.rows {
                guard let row = state.rows[id] else { continue }
                if row.isHead {
                    if row.role == .warmup, working { problems.append("warm-up \(id) after a working set in \(slot.key)") }
                    if row.role == .working { working = true }
                    if row.chainPosition != 1 { problems.append("head \(id) at chain position \(row.chainPosition)") }
                    head = row
                    position = 2
                } else {
                    if row.chainHead != head?.id || row.chainPosition != position {
                        problems.append("segment \(id) is not where its chain puts it in \(slot.key)")
                    }
                    if row.role != head?.role { problems.append("segment \(id) has another role than its head") }
                    position += 1
                }
            }
        }
        // 4. Groups contiguous, of two or more, keys unique, every slot's group known.
        let groupKeys = state.groups.map(\.key)
        if Set(groupKeys).count != groupKeys.count { problems.append("group keys repeat: \(groupKeys)") }
        for group in state.groups {
            let indices = state.slots.indices.filter { state.slots[$0].group == group.key }
            if indices.count < 2 { problems.append("group \(group.key) has \(indices.count) member") }
            if let first = indices.first, let last = indices.last, last - first + 1 != indices.count {
                problems.append("group \(group.key) is not contiguous")
            }
        }
        for slot in state.slots {
            if let key = slot.group, state.group(key) == nil { problems.append("slot \(slot.key) names unknown group \(key)") }
        }
        // 5. A segment completed only after every earlier link.
        for row in state.rows.values where row.isCompleted && !row.isHead && !state.earlierLinksCompleted(row.id) {
            problems.append("segment \(row.id) completed before an earlier link")
        }
        // 6. At most one pending grown mini-set per chain, and it is the last.
        for head in state.rows.values where head.isHead && head.structure.growsOneAtATime {
            let chain = state.chain(of: head.id)
            let pending = chain.dropFirst().filter(\.isPending)
            if pending.count > 1 { problems.append("chain \(head.id) has \(pending.count) pending mini-sets") }
            if let one = pending.first, one.id != chain.last?.id { problems.append("chain \(head.id)'s pending mini-set is not last") }
        }
        // 7. A timed countdown names a pending row whose timer started.
        if let timed = state.countdown?.timedRow {
            let row = state.rows[timed]
            if row?.isPending != true || row?.timerStartedAt == nil {
                problems.append("timed countdown on \(timed), which is not a running pending row")
            }
        }
        // 8. Completed rows: a record id, ticked no earlier than the start, values complete.
        var recordIDs: Set<SetID> = []
        for row in state.rows.values {
            guard let performed = row.performed else { continue }
            if performed.recordID.rawValue.isEmpty || !recordIDs.insert(performed.recordID).inserted {
                problems.append("row \(row.id) has an empty or repeated record id")
            }
            if performed.completedAt < state.session.at { problems.append("row \(row.id) ticked before the start") }
            if let measurement = state.exercise(ofRow: row.id)?.measurement, !measurement.missing(in: performed.values).isEmpty {
                problems.append("row \(row.id) is missing \(measurement.missing(in: performed.values))")
            }
        }
        return problems
    }
}

/// One event applied to a working copy of the state. A thrown error discards
/// the copy, so a refused event changes nothing.
struct Reduction {
    var state: SessionState
    let at: Date
    let eventID: EventID

    func pool(_ ids: [RowID]) -> RowIDPool {
        RowIDPool(ids, prefix: eventID.rawValue)
    }

    mutating func apply(_ event: SessionEvent) throws(EngineError) {
        switch event {
        case .start: throw .sessionActive
        case let .touchRow(row): try touch(row)
        case let .editSet(edit): try self.edit(edit)
        case let .completeSet(tick): try complete(tick, stopsTimer: false)
        case let .uncompleteSet(row): try uncomplete(row)
        case let .addSet(add): try addSet(add)
        case let .removeSet(row): try removeSet(row)
        case let .changeSetKind(change): try changeKind(change)
        case let .setRowRest(row, seconds): try setRowRest(row, seconds)
        case let .addExercise(add): try addExercise(add)
        case let .removeExercise(slot): try removeExercise(slot)
        case let .reorderExercises(keys): try reorder(keys)
        case let .replaceExercise(replace): try self.replace(replace)
        case let .groupExercises(slot, partner, group): try self.group(slot, with: partner, key: group)
        case let .ungroupExercise(slot): try ungroup(slot)
        case let .setGroup(change): try setGroup(change)
        case let .setSlotRest(slot, rest, seconds): try setSlotRest(slot, rest, seconds)
        case let .setNote(target, text): try setNote(target, text)
        case .pauseClock: try pause()
        case .resumeClock: try resume()
        case let .startRest(seconds): try startRest(seconds)
        case let .adjustRest(seconds): try adjustRest(seconds)
        case .skipRest: try skipRest()
        case let .startTimedSet(row, target): try startTimed(row, target: target)
        case let .stopTimedSet(tick): try complete(tick, stopsTimer: true)
        case let .cancelTimedSet(row): try cancelTimed(row)
        case let .finish(finish): try self.finish(finish)
        case .discard: break
        }
    }

    // MARK: - Sets

    mutating func touch(_ id: RowID) throws(EngineError) {
        let row = try state.requireRow(id)
        guard row.isPending, state.earlierLinksCompleted(id) else { return }
        state.focus = id
        // Owners type the next load during rest: a touch ends the rest only
        // once the countdown is over, or when none runs (§4.8).
        // The standalone timer is no rest countdown: it never holds the end back.
        let restRunning = state.countdown.map { countdown in
            if case .rest = countdown.kind { return !countdown.hasEnded(by: at) }
            return false
        } ?? false
        if var clock = state.restClock, clock.endedAt == nil, !restRunning {
            clock.endedAt = at
            state.restClock = clock
        }
    }

    mutating func edit(_ edit: SessionEvent.EditSet) throws(EngineError) {
        let row = try state.requireRow(edit.row)
        // An edit rates (`entered`) or copies (`copied`); a pre-fill is the tick's to accept.
        if case .set = edit.values.rpe, let method = edit.values.rpeMethod, method != .entered, method != .copied {
            throw .invalidEffort
        }
        let notes = edit.note.applied(to: row.notes).flatMap(trimmedNote)
        switch edit.values.rpe {
        case .set: state.rows[edit.row]?.rpePrefillDeclined = false
        case .cleared: state.rows[edit.row]?.rpePrefillDeclined = true
        case .unchanged: break
        }
        guard var performed = row.performed else {
            state.rows[edit.row]?.entered = row.entered.applying(edit.values)
            state.rows[edit.row]?.notes = notes
            return
        }
        let values = performed.values.applying(edit.values)
        guard values != performed.values || notes != row.notes else { return }
        guard let measurement = state.exercise(ofRow: edit.row)?.measurement else { throw .unknownSlot }
        let missing = measurement.missing(in: values)
        guard missing.isEmpty else { throw .incompleteValues(missing: missing) }
        performed.values = values
        performed.edits += 1
        state.rows[edit.row]?.performed = performed
        state.rows[edit.row]?.notes = notes
        var ids = pool(edit.segmentRowIDs)
        try state.recheckChain(row.headID, ids: &ids)
        if row.isHead { state.matchSecondSide(edit.row) }
    }

    mutating func complete(_ tick: SessionEvent.CompleteSet, stopsTimer: Bool) throws(EngineError) {
        let row = try state.requireRow(tick.row)
        guard row.isPending else { throw .rowNotPending }
        guard state.earlierLinksCompleted(tick.row) else { throw .segmentBeforeItsLink }
        var values = tick.values
        if stopsTimer {
            guard state.timedRow == tick.row, let started = row.timerStartedAt else { throw .noTimer }
            // Reaching the target does nothing by itself; Stop takes the time run.
            if values.duration == nil { values.duration = seconds(from: started, to: at) }
        } else if let timed = state.timedRow, timed != tick.row {
            throw .timerRunning
        }
        guard let measurement = state.exercise(ofRow: tick.row)?.measurement else { throw .unknownSlot }
        let missing = measurement.missing(in: values)
        guard missing.isEmpty else { throw .incompleteValues(missing: missing) }
        values = try state.committedEffort(values, row: tick.row)
        values = state.markedFailure(values, row: tick.row)

        let clock = state.restClock
        var performed = Performed(recordID: tick.recordID, completedAt: at, startedAt: row.timerStartedAt, values: values,
                                  priorRestClock: clock)
        if let kept = row.kept {
            // Ticked before: the first record id and rest fields stay.
            performed.recordID = kept.recordID
            performed.restBefore = kept.restBefore
            performed.restPlanned = kept.restPlanned
            performed.restSkipped = kept.restSkipped
            performed.edits = kept.edits
        } else if let clock {
            performed.restBefore = max(0, seconds(from: clock.since, to: clock.endedAt ?? at))
            performed.restPlanned = clock.planned
            performed.restSkipped = clock.skipped
        }
        state.rows[tick.row]?.performed = performed
        state.rows[tick.row]?.kept = nil
        if state.timedRow == tick.row { state.countdown = nil }
        var ids = pool(tick.segmentRowIDs)
        try state.afterTick(tick.row, noLighterLoad: tick.noLighterLoad, ids: &ids)
        state.lastCompleted = tick.row
        state.focus = nil
        startRestClock(after: tick.row)
    }

    /// The rest decision for a tick (§4.7) sets the countdown and a new rest clock (§4.8).
    mutating func startRestClock(after id: RowID) {
        let decision = RestRules.after(id, in: state)
        if decision.seconds > 0 {
            state.countdown = Countdown(kind: .rest(decision.slot ?? .setRest), startedAt: at,
                                        endsAt: at.addingTimeInterval(TimeInterval(decision.seconds)))
        } else if state.countdown?.kind != .manual {
            // A rest of 0 runs no timer; an older rest is over. The standalone timer keeps running.
            state.countdown = nil
        }
        state.restClock = RestClock(since: at, planned: decision.seconds, slot: decision.slot)
    }

    mutating func uncomplete(_ id: RowID) throws(EngineError) {
        let row = try state.requireRow(id)
        guard let performed = row.performed else { throw .rowNotCompleted }
        // Only the last completed link of a chain, or a straight set.
        guard !state.chain(of: id).contains(where: { $0.chainPosition > row.chainPosition && $0.isCompleted }) else {
            throw .invalidOrder
        }
        state.rows[id]?.performed = nil
        // A pre-filled RPE was never the owner's: it goes back to being the
        // row's pre-fill, and a tick that accepts it logs it so again.
        var entered = performed.values
        if entered.rpeMethod == .prefilled {
            entered.rpe = nil
            entered.rpeMethod = nil
        }
        state.rows[id]?.entered = entered
        state.rows[id]?.kept = KeptTick(recordID: performed.recordID, restBefore: performed.restBefore,
                                        restPlanned: performed.restPlanned, restSkipped: performed.restSkipped,
                                        edits: performed.edits)
        state.removePendingMiniSets(of: row.headID, after: row.chainPosition)
        if row.isHead { state.unmatchSecondSide(id) }
        // The countdown and the clock go back only when this tick started
        // them; an older set's un-tick leaves the newer rest alone. Instants
        // are floored to the second, so the latest tick is checked as well.
        let startedByThisTick = state.lastCompleted == id
        if startedByThisTick, let countdown = state.countdown, case .rest = countdown.kind,
           countdown.startedAt == performed.completedAt
        {
            state.countdown = nil
        }
        if startedByThisTick, state.restClock?.since == performed.completedAt {
            state.restClock = performed.priorRestClock
        }
        state.recomputeLastCompleted()
    }

    mutating func addSet(_ add: SessionEvent.AddSet) throws(EngineError) {
        let index = try state.requireSlotIndex(add.slot)
        let slot = state.slots[index]
        var head = Row(id: add.row, slot: add.slot, role: add.role)
        var new = [head]
        if state.exercises[slot.exercise]?.resolvedLaterality == .unilateral {
            head.side = slotFirstSide(slot)
            var ids = pool(add.segmentRowIDs)
            new = [head, SessionEngine.secondSide(of: head, id: ids.next())]
        }
        try state.insert(new, inSlot: index, at: state.insertionPosition(forRole: add.role, inSlot: index))
    }

    mutating func removeSet(_ id: RowID) throws(EngineError) {
        let row = try state.requireRow(id)
        let chain = state.chain(of: id)
        // A head goes with its whole chain; a segment with the segments after it.
        let doomed = row.isHead ? chain : chain.filter { $0.chainPosition >= row.chainPosition }
        state.removeRows(Set(doomed.map(\.id)))
    }

    mutating func changeKind(_ change: SessionEvent.ChangeSetKind) throws(EngineError) {
        let row = try state.requireRow(change.row)
        guard row.isHead else { throw .kindNotAllowed }
        let unilateral = state.exercise(ofRow: change.row)?.resolvedLaterality == .unilateral
        let segmentDone = state.chain(of: change.row).contains { !$0.isHead && $0.isCompleted && $0.side == nil }
        let structure: Structure
        switch change.kind {
        case .warmup, .working, .failure:
            structure = segmentDone ? row.structure : .straight
        case .drop, .myo, .myomatch:
            // The side chain wins on a one-side exercise (§4.9); a structure
            // changes only while no segment of it is done.
            guard !unilateral, !segmentDone else { throw .kindNotAllowed }
            structure = change.kind == .drop ? .drop : change.kind == .myo ? .myo : .myomatch
        }
        let role: Role = change.kind == .warmup ? .warmup : .working
        let termination: Termination? = switch change.kind {
        case .failure: .failure
        case .myomatch: .totalreps
        default: row.target?.termination
        }
        state.rows[change.row]?.role = role
        state.rows[change.row]?.termination = termination
        if structure != row.structure {
            var ids = pool(change.segmentRowIDs)
            try state.restructure(change.row, to: structure, ids: &ids)
        }
        if row.isCompleted {
            if change.kind == .failure { state.rows[change.row]?.performed?.values.reachedFailure = true }
            let after = state.rows[change.row]
            if after?.role != row.role || after?.structure != row.structure || after?.termination != row.termination
                || after?.performed?.values != row.performed?.values
            {
                state.rows[change.row]?.performed?.edits += 1
            }
        }
        state.syncChain(change.row)
        state.sortWarmupsFirst(slot: row.slot)
    }

    mutating func setRowRest(_ id: RowID, _ seconds: Seconds?) throws(EngineError) {
        let row = try state.requireRow(id)
        // A per-set override is a rest after the whole set: it lives on the head.
        state.rows[row.headID]?.restOverride = seconds.map { max(0, $0) }
    }

    // MARK: - Exercises and groups

    mutating func addExercise(_ add: SessionEvent.AddExercise) throws(EngineError) {
        guard state.slotIndex(add.slot) == nil else { throw .duplicateID }
        state.exercises[add.exercise.id] = add.exercise
        var position = state.slots.count
        if let after = add.after {
            let index = try state.requireSlotIndex(after)
            let group = state.slots[index].group
            position = (group.flatMap { key in state.slots.lastIndex { $0.group == key } } ?? index) + 1
        }
        state.slots.insert(SlotState(key: add.slot, exercise: add.exercise.id, rests: add.rests), at: position)
        let unilateral = add.exercise.resolvedLaterality == .unilateral
        var ids = pool(add.rowIDs)
        let roles = add.roles.filter { $0 == .warmup } + add.roles.filter { $0 != .warmup }
        for role in roles {
            let head = Row(id: ids.next(), slot: add.slot, role: role, side: unilateral ? add.firstSide ?? .left : nil)
            let new = unilateral ? [head, SessionEngine.secondSide(of: head, id: ids.next())] : [head]
            try state.insert(new, inSlot: position, at: state.slots[position].rows.count)
        }
    }

    mutating func removeExercise(_ key: String) throws(EngineError) {
        let index = try state.requireSlotIndex(key)
        state.removeRows(Set(state.slots[index].rows))
        state.slots.remove(at: index)
        state.pruneGroups()
    }

    mutating func reorder(_ keys: [String]) throws(EngineError) {
        guard keys.count == state.slots.count, Set(keys) == Set(state.slots.map(\.key)) else { throw .invalidOrder }
        let byKey = Dictionary(uniqueKeysWithValues: state.slots.map { ($0.key, $0) })
        state.slots = keys.compactMap { byKey[$0] }
        state.gatherGroups()
    }

    mutating func replace(_ replace: SessionEvent.ReplaceExercise) throws(EngineError) {
        let index = try state.requireSlotIndex(replace.slot)
        let old = state.slots[index]
        guard replace.newSlot == replace.slot || state.slotIndex(replace.newSlot) == nil else { throw .duplicateID }
        state.exercises[replace.exercise.id] = replace.exercise
        var ids = pool(replace.segmentRowIDs)
        let side = slotFirstSide(old)
        let units = state.units(inSlot: replace.slot)
        let started = units.filter { state.rows[$0[0]]?.isCompleted == true }
        if started.isEmpty {
            // Nothing done yet: the slot is replaced in place, keeping its
            // group, routine slot, rests, notes and its rows' targets.
            state.slots[index].key = replace.newSlot
            state.slots[index].exercise = replace.exercise.id
            for id in old.rows {
                state.rows[id]?.slot = replace.newSlot
            }
        } else {
            // The old slot keeps what was done; the pending sets move to a new
            // slot right after it, in the same group, for the same routine slot.
            guard replace.newSlot != replace.slot else { throw .duplicateID }
            state.removeRows(Set(started.joined().filter { state.rows[$0]?.isPending == true }))
            let moving = units.filter { state.rows[$0[0]]?.isPending == true }.flatMap(\.self)
            state.slots[index].rows.removeAll(where: moving.contains)
            for id in moving {
                state.rows[id]?.slot = replace.newSlot
            }
            state.slots.insert(SlotState(key: replace.newSlot, exercise: replace.exercise.id, group: old.group,
                                         routineSlot: old.routineSlot, rows: moving, sideOrder: old.sideOrder,
                                         sideMatch: old.sideMatch, replaces: old.lane), at: index + 1)
            state.pruneGroups()
        }
        try state.normalizeSides(slot: replace.newSlot, firstSide: side, ids: &ids)
    }

    mutating func group(_ key: String, with partner: String, key groupKey: String?) throws(EngineError) {
        // A slot grouped with itself would make a group of one.
        guard key != partner else { throw .groupTooSmall }
        _ = try state.requireSlotIndex(key)
        let partnerIndex = try state.requireSlotIndex(partner)
        let target: String
        if let existing = state.slots[partnerIndex].group {
            guard state.slot(key)?.group != existing else { throw .invalidOrder }
            target = existing
        } else {
            let taken = Set(state.groups.map(\.key))
            target = groupKey.flatMap { taken.contains($0) ? nil : $0 } ?? Self.firstUnusedKey(taken)
            state.groups.append(GroupState(key: target, groupKind: .superset, madeInGym: true))
            state.slots[partnerIndex].group = target
        }
        guard let index = state.slotIndex(key) else { throw .unknownSlot }
        var slot = state.slots.remove(at: index)
        slot.group = target
        let last = state.slots.lastIndex { $0.group == target } ?? (state.slots.count - 1)
        state.slots.insert(slot, at: last + 1)
        state.pruneGroups()
        state.gatherGroups()
    }

    /// A group made in the gym takes the first unused lowercase letter.
    static func firstUnusedKey(_ taken: Set<String>) -> String {
        let letters = "abcdefghijklmnopqrstuvwxyz".map(String.init)
        return letters.first { !taken.contains($0) } ?? (2...).lazy.map { "g\($0)" }.first { !taken.contains($0) }!
    }

    mutating func ungroup(_ key: String) throws(EngineError) {
        let index = try state.requireSlotIndex(key)
        guard let group = state.slots[index].group else { throw .invalidOrder }
        var slot = state.slots.remove(at: index)
        slot.group = nil
        let last = state.slots.lastIndex { $0.group == group } ?? (index - 1)
        state.slots.insert(slot, at: last + 1)
        state.pruneGroups()
    }

    mutating func setGroup(_ change: SessionEvent.SetGroup) throws(EngineError) {
        guard let index = state.groups.firstIndex(where: { $0.key == change.group }) else { throw .unknownGroup }
        if let kind = change.kind {
            state.groups[index].groupKind = kind
            state.groups[index].kindSetByHand = true
        }
        state.groups[index].pairing = change.pairing.applied(to: state.groups[index].pairing)
        state.groups[index].roundRest = change.roundRest.applied(to: state.groups[index].roundRest).map { max(0, $0) }
        state.groups[index].changedInGym = true
    }

    mutating func setSlotRest(_ key: String, _ rest: SlotRestKind, _ seconds: Seconds?) throws(EngineError) {
        let index = try state.requireSlotIndex(key)
        state.slots[index].rests[rest] = seconds.map { max(0, $0) }
    }

    mutating func setNote(_ target: NoteTarget, _ text: String?) throws(EngineError) {
        let note = text.flatMap(trimmedNote)
        switch target {
        case .session:
            state.session.notes = note
        case let .slot(key):
            let index = try state.requireSlotIndex(key)
            state.slots[index].notes = note
        }
    }

    // MARK: - Clocks

    mutating func pause() throws(EngineError) {
        guard !state.session.isPaused else { throw .clockPaused }
        state.session.pauses.append(Pause(start: at))
    }

    mutating func resume() throws(EngineError) {
        guard state.session.isPaused else { throw .clockNotPaused }
        state.session.pauses[state.session.pauses.count - 1].end = at
    }

    mutating func startRest(_ seconds: Seconds) throws(EngineError) {
        guard state.timedRow == nil else { throw .timerRunning }
        // A standalone timer replaces any rest countdown; the rest clock is untouched.
        state.countdown = seconds > 0
            ? Countdown(kind: .manual, startedAt: at, endsAt: at.addingTimeInterval(TimeInterval(seconds)))
            : nil
    }

    mutating func adjustRest(_ seconds: Seconds) throws(EngineError) {
        guard var countdown = state.countdown, countdown.isRestLike, let end = countdown.endsAt else { throw .noTimer }
        // −15 never moves the end before now.
        let newEnd = max(at, end.addingTimeInterval(TimeInterval(seconds)))
        countdown.endsAt = newEnd
        state.countdown = countdown
        if case .rest = countdown.kind, var clock = state.restClock, clock.since == countdown.startedAt {
            clock.planned = self.seconds(from: countdown.startedAt, to: newEnd)
            state.restClock = clock
        }
    }

    mutating func skipRest() throws(EngineError) {
        guard let countdown = state.countdown, countdown.isRestLike else { throw .noTimer }
        state.countdown = nil
        if var clock = state.restClock, clock.endedAt == nil {
            clock.endedAt = at
            // Skip after the planned rest has run out is not a skipped rest.
            clock.skipped = at < clock.since.addingTimeInterval(TimeInterval(clock.planned))
            state.restClock = clock
        }
    }

    mutating func startTimed(_ id: RowID, target: Seconds?) throws(EngineError) {
        let row = try state.requireRow(id)
        guard row.isPending else { throw .rowNotPending }
        guard state.earlierLinksCompleted(id) else { throw .segmentBeforeItsLink }
        guard state.isTimed(id) else { throw .noTimer }
        guard state.timedRow == nil else { throw .timerRunning }
        // The set's start ends the rest before it (§4.8) and replaces any rest countdown.
        if var clock = state.restClock, clock.endedAt == nil {
            clock.endedAt = at
            state.restClock = clock
        }
        let now = at
        state.rows[id]?.timerStartedAt = now
        state.countdown = Countdown(kind: .timedSet(id), startedAt: at,
                                    endsAt: target.flatMap { $0 > 0 ? at.addingTimeInterval(TimeInterval($0)) : nil })
        state.focus = id
    }

    mutating func cancelTimed(_ id: RowID) throws(EngineError) {
        _ = try state.requireRow(id)
        guard state.timedRow == id else { throw .noTimer }
        state.rows[id]?.timerStartedAt = nil
        state.countdown = nil
    }

    // MARK: - Finish

    mutating func finish(_ finish: SessionEvent.Finish) throws(EngineError) {
        guard state.timedRow == nil else { throw .timerRunning }
        let lastTick = state.rows.values.compactMap { $0.performed?.completedAt }.max()
        let end = max(finish.end, lastTick ?? finish.end)
        // Unticked rows: dropped, or done as shown at the end, in display
        // order, with no rest fields. A row with nothing to commit is dropped.
        var marks: [RowID: UntickedFate.MarkDone] = [:]
        if case let .markDone(list) = finish.unticked {
            marks = Dictionary(list.map { ($0.row, $0) }, uniquingKeysWith: { first, _ in first })
        }
        for slot in state.slots {
            for id in slot.rows {
                guard let row = state.rows[id], row.isPending, let mark = marks[id], state.earlierLinksCompleted(id),
                      let measurement = state.exercise(ofRow: id)?.measurement, measurement.missing(in: mark.values).isEmpty
                else { continue }
                let recordID = row.kept?.recordID ?? mark.recordID
                guard !state.rows.values.contains(where: { $0.performed?.recordID == recordID }) else { continue }
                // A method the row cannot claim loses the RPE, not the set.
                var unrated = mark.values
                unrated.rpe = nil
                unrated.rpeMethod = nil
                let values = state.markedFailure((try? state.committedEffort(mark.values, row: id)) ?? unrated, row: id)
                state.rows[id]?.performed = Performed(recordID: recordID, completedAt: end, startedAt: row.timerStartedAt,
                                                      values: values, edits: row.kept?.edits ?? 0, completion: .bulk)
                state.rows[id]?.kept = nil
            }
        }
        state.removeRows(Set(state.rows.values.filter(\.isPending).map(\.id)))
        // Exercises with no performed set go, and groups left with one member.
        state.slots.removeAll { $0.rows.isEmpty }
        state.pruneGroups()
        let firstStart = state.rows.values.compactMap { $0.performed.map { $0.startedAt ?? $0.completedAt } }.min()
        let start = min(firstStart.map { min(finish.start, $0) } ?? finish.start, end)
        state.session.at = start
        state.session.endsAt = end
        // The open pause closes at the end; pauses are kept inside the
        // confirmed span, and one left with no length goes.
        state.session.pauses = state.session.pauses.compactMap { pause in
            let clamped = Pause(start: max(pause.start, start), end: min(pause.end ?? end, end))
            return clamped.start < clamped.end! ? clamped : nil
        }
        state.session.sessionRPE = finish.sessionRPE
        if let notes = finish.notes { state.session.notes = trimmedNote(notes) }
        state.session.utcOffsetEnd = finish.utcOffset
        state.countdown = nil
        state.restClock = nil
        state.focus = nil
        state.recomputeLastCompleted()
        state.phase = .finished
    }

    // MARK: - Helpers

    func seconds(from start: Date, to end: Date) -> Seconds {
        Seconds(end.timeIntervalSince(start).rounded())
    }

    /// The side a new set of the slot starts with: the slot's existing sets', else its order.
    func slotFirstSide(_ slot: SlotState) -> Side {
        let existing = slot.rows.lazy.compactMap { state.rows[$0] }.first { $0.isHead && $0.side != nil }?.side
        return existing ?? SessionEngine.firstSide(slot.sideOrder, resolved: nil)
    }
}

/// Text with surrounding whitespace trimmed; nil when nothing is left.
func trimmedNote(_ text: String) -> String? {
    let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
    return trimmed.isEmpty ? nil : trimmed
}
