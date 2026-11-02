import Foundation

// Session state to records (§3.5): at Finish the engine's state becomes one
// session record and one set record per performed row. Pure, so the example
// sessions are its tests.

public extension SessionEngine {
    /// The session and its sets as records. `adapter` is `substrate-workouts@<version>`.
    /// Meant for a finished state; on an active one it describes what is done so far.
    static func records(_ state: SessionState, adapter: String) -> SessionRecords {
        SessionRecords(session: sessionRecord(state, adapter: adapter), sets: setRecords(state))
    }

    /// The session record.
    static func sessionRecord(_ state: SessionState, adapter: String) -> TrainingSession {
        let header = state.session
        let zone = header.timezone.flatMap(TimeZone.init(identifier:))
        let performedSlots = state.slots.filter { slot in slot.rows.contains { state.rows[$0]?.isCompleted == true } }
        let memberCounts = Dictionary(grouping: performedSlots.compactMap(\.group), by: { $0 }).mapValues(\.count)
        let liveGroups = Set(memberCounts.filter { $0.value >= 2 }.keys)

        var slots: [SessionSlot] = []
        var snapshotted: Set<String> = []
        for slot in performedSlots {
            let group = slot.group.flatMap { liveGroups.contains($0) ? $0 : nil }
            var record = SessionSlot(
                key: slot.key,
                exercise: slot.exercise,
                group: group,
                routineSlot: slot.routineSlot,
                notes: slot.notes,
                setRest: slot.rests.setRest,
                warmupRest: slot.rests.warmupRest,
                transition: slot.rests.transition,
                sideSwitch: slot.rests.sideSwitch
            )
            // A slot no routine slot speaks for also stores the rests the
            // next session would need: a plain slot's set rest, a member's
            // transition (the last member's is never run).
            if state.routineSlot(of: slot.key) == nil {
                if let group {
                    let members = performedSlots.filter { $0.group == group }
                    if members.last?.key != slot.key {
                        record.transition = record.transition ?? RestRules.transition(slot: slot.key, in: state).seconds
                    }
                } else {
                    record.setRest = record.setRest ?? RestRules.setRest(slot: slot.key, in: state).seconds
                }
            }
            // The routine slot's planned sets as the session started with
            // them, done or not (§3.5), once per routine slot: a slot that
            // took over another's pending sets does not repeat them.
            if let name = slot.routineSlot, let planned = state.plan?.slots.first(where: { $0.key == name }),
               snapshotted.insert(name).inserted
            {
                record.sets = planned.sets.map { snapshot($0, bodyMass: header.bodyMass) }
            }
            slots.append(record)
        }

        let groups = state.groups.filter { liveGroups.contains($0.key) }.map { group in
            var record = SessionGroup(key: group.key, groupKind: group.groupKind)
            // The routine's group speaks for itself unless the gym changed it.
            if group.madeInGym || group.changedInGym {
                record.pairing = group.pairing ?? state.routineGroup(of: group.key)?.pairing
                record.rounds = rounds(of: group.key, in: state)
                record.roundRest = RestRules.roundRest(group: group.key, in: state).seconds
            }
            return record
        }

        let start = header.at
        let end = header.endsAt
        return TrainingSession(
            id: header.id,
            at: start,
            endsAt: state.phase == .finished ? end : nil,
            name: TrainingSession.name(routine: state.plan?.name, start: start,
                                       timeZone: zone ?? TimeZone(secondsFromGMT: (header.utcOffsetStart ?? 0) * 60) ?? utc),
            status: state.phase == .finished ? .finished : .active,
            routine: header.routine,
            routineVersion: header.routineVersion,
            deload: header.deload,
            sport: .strength,
            activityType: header.activityType,
            slots: slots,
            groups: groups,
            pauses: header.pauses,
            utcOffsetStart: zone.map { TrainingSession.utcOffset(in: $0, at: start) } ?? header.utcOffsetStart,
            utcOffsetEnd: state.phase == .finished ? (zone.flatMap { z in end.map { TrainingSession.utcOffset(in: z, at: $0) } } ?? header.utcOffsetEnd) : nil,
            timezone: header.timezone,
            bodyMass: header.bodyMass,
            sessionRPE: header.sessionRPE,
            notes: header.notes,
            revision: state.revision,
            adapter: adapter
        )
    }

    /// One record per performed row, in the order they were ticked (rows
    /// marked done at Finish share its end and follow display order).
    static func setRecords(_ state: SessionState) -> [PerformedSet] {
        var sets: [(slot: Int, set: PerformedSet)] = []
        for (slotIndex, slot) in state.slots.enumerated() {
            guard let exercise = state.exercises[slot.exercise] else { continue }
            let ordinals = Ordinals.ordinals(inSlot: slot.key, of: state, performedOnly: true)
            let orders = Ordinals.orders(inSlot: slot.key, of: state)
            for unit in state.units(inSlot: slot.key) {
                let performed = unit.compactMap { state.rows[$0] }.filter(\.isCompleted)
                guard let head = performed.first, head.isHead, let headRecord = head.performed?.recordID else { continue }
                let isChain = performed.count > 1 || head.structure != .straight || head.side != nil
                for (offset, row) in performed.enumerated() {
                    guard let order = orders[row.id] else { continue }
                    let set = setRecord(row, exercise: exercise, state: state, order: order, ordinal: ordinals[row.id],
                                        continues: offset > 0 ? headRecord : nil, chainPosition: isChain ? offset + 1 : nil)
                    sets.append((slotIndex, set))
                }
            }
        }
        return sets.sorted { a, b in
            if a.set.at != b.set.at { return a.set.at < b.set.at }
            if a.slot != b.slot { return a.slot < b.slot }
            return a.set.order < b.set.order
        }.map(\.set)
    }

    /// The round count a group ran: its members' most performed working sets.
    internal static func rounds(of group: String, in state: SessionState) -> Int {
        state.members(ofGroup: group).map { member in
            state.units(inSlot: member.key).count { unit in
                state.rows[unit[0]].map { $0.role == .working && $0.isCompleted } ?? false
            }
        }.max() ?? 0
    }

    /// A planned set as the session's snapshot stores it: as the routine has
    /// it, with a percentage load resolved into `load` (D5).
    static func snapshot(_ planned: PlannedSet, bodyMass: Mass?) -> PlannedSet {
        var snapshot = planned
        if snapshot.load == nil { snapshot.load = resolvedLoad(of: planned, bodyMass: bodyMass).load }
        return snapshot
    }

    /// The target a set record stores (§3.5): the row's copy of the plan,
    /// with what the engine ran where the plan is silent. On a head: the
    /// timer mode of a timed target (countdown), a myo-rep set's stop rule
    /// (the exercise's, else `repdrop`) and a structured set's intra-set
    /// pause (15 s for myo-reps and myo-rep match, 20 s for rest-pause and
    /// clusters, none for drops). A timed second side carries its head's
    /// timer mode. Absent for a row the plan did not have.
    static func recordTarget(_ row: Row, exercise: Exercise, state: SessionState) -> SetTarget? {
        guard var target = row.target else { return nil }
        let head = state.rows[row.headID] ?? row
        let timerMode = head.target?.timerMode ?? head.timerMode ?? .countdown
        if row.isHead {
            if target.timerMode == nil, target.durationMin != nil || target.durationMax != nil {
                target.timerMode = timerMode
            }
            if row.structure == .myo, target.myoStopRule == nil {
                target.myoStopRule = exercise.resolvedMyoStopRule
            }
            if row.structure != .straight, target.intraSetPause == nil {
                target.intraSetPause = RestRules.intraSetPause(head: row).seconds
            }
        } else if row.side != nil, exercise.measurement.isTimedHold, target.timerMode == nil {
            target.timerMode = timerMode
        }
        return target
    }

    private static func setRecord(_ row: Row, exercise: Exercise, state: SessionState, order: Int, ordinal: Int?,
                                  continues: SetID?, chainPosition: Int?) -> PerformedSet
    {
        let performed = row.performed!
        let values = performed.values
        let measurement = exercise.measurement
        let mode = measurement.loadMode
        let reps = measurement.recordsReps ? values.reps : nil
        let laterality = exercise.resolvedLaterality
        let bulk = performed.completion == .bulk
        return PerformedSet(
            id: performed.recordID,
            at: performed.completedAt,
            session: state.session.id,
            exercise: exercise.id,
            slot: row.slot,
            order: order,
            ordinal: ordinal,
            role: row.role,
            roleLabel: row.roleLabel,
            structure: row.structure,
            continues: continues,
            chainPosition: chainPosition,
            side: row.side,
            loadMode: mode,
            load: mode == .external || mode == .bodyweightplus ? values.load : nil,
            assistance: mode == .assisted ? values.assistance : nil,
            reps: reps,
            duration: measurement.recordsDuration ? values.duration : nil,
            distance: measurement.recordsDistance ? values.distance : nil,
            // What the numbers meant, copied from the exercise snapshot (D6).
            loadBasis: mode == nil ? nil : exercise.resolvedLoadBasis,
            implements: mode == nil ? nil : exercise.resolvedImplements,
            bodyShare: mode == nil || mode == .external ? nil : exercise.resolvedBodyShare,
            laterality: laterality,
            repsBasis: reps == nil ? nil : laterality == .bilateral ? .total : .perside,
            rpe: values.rpe,
            rpeMethod: values.rpe == nil ? nil : values.rpeMethod ?? .entered,
            rpeScale: .rirbased,
            reachedFailure: values.reachedFailure == true ? true : nil,
            failedRepAttempted: values.failedRepAttempted == true ? true : nil,
            completionMethod: performed.completion,
            notes: row.notes,
            timeBasis: bulk ? .session : .completion,
            startedAt: performed.startedAt,
            restBefore: performed.restBefore,
            restMethod: restMethod(performed),
            restPlanned: performed.restPlanned,
            restSkipped: performed.restSkipped ? true : nil,
            target: recordTarget(row, exercise: exercise, state: state),
            revision: 1 + performed.edits
        )
    }

    /// How the rest before a set was measured (D10). the app's rest ends at
    /// a timer's start, a Skip, a touch once the countdown is over, or the
    /// tick (§4.8): `start` when it ran to the set's recorded start (a timed
    /// set's play), else `interaction`. Nil with no rest.
    static func restMethod(_ performed: Performed) -> RestMethod? {
        guard let rest = performed.restBefore else { return nil }
        if let start = performed.startedAt, let since = performed.priorRestClock?.since,
           since.addingTimeInterval(TimeInterval(rest)) == start
        {
            return .start
        }
        return .interaction
    }

    private static let utc = TimeZone(identifier: "UTC")!
}

public extension MeasurementType {
    /// The set record carries reps.
    var recordsReps: Bool {
        switch self {
        case .weightreps, .repsonly, .bodyweightreps, .weightedbodyweightreps, .bodyweightassistedreps: true
        case .duration, .weightduration, .distanceduration, .shortdistanceweight: false
        }
    }

    /// The set record carries a duration: timed sets, and a carry timed with its stopwatch.
    var recordsDuration: Bool {
        hasTimer
    }

    /// The set record carries a distance.
    var recordsDistance: Bool {
        self == .distanceduration || self == .shortdistanceweight
    }
}
