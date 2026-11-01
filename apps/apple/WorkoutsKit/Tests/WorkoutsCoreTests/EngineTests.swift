import Foundation
@testable import WorkoutsCore
import Testing

// The session engine (DESIGN §4, §10.1, §10.5): every event's success and
// each error, the invariants and the journal's fold after every event, Finish
// to records against the example sessions. `EngineScript` drives the engine
// as the controller does and is shared by the rest, Up next and chain tests.

// MARK: - The script

/// Sends events the way the controller does: minted ids, an instant per
/// event, applied one at a time. After every event it checks the §4.4
/// invariants, that folding the journal's events gives the same state, and
/// that the state survives the journal's JSON round trip.
struct EngineScript {
    private(set) var events: [EventEnvelope] = []
    private(set) var state: SessionState?
    private(set) var now: Date
    private var generator: SeededGenerator

    init(at start: Date, seed: UInt64 = 7) {
        now = start
        generator = SeededGenerator(seed: seed)
    }

    /// The state; fails the test when no session exists.
    var current: SessionState {
        get throws { try #require(state) }
    }

    mutating func rowIDs(_ count: Int) -> [RowID] {
        (0 ..< count).map { _ in RowID(UUIDv7.make(at: now, using: &generator)) }
    }

    mutating func setID() -> SetID {
        SetID(UUIDv7.make(at: now, using: &generator))
    }

    mutating func wait(_ seconds: Int) {
        now = now.addingTimeInterval(TimeInterval(seconds))
    }

    mutating func envelope(_ event: SessionEvent, at date: Date?) -> EventEnvelope {
        if let date { now = date }
        return EventEnvelope(id: EventID(UUIDv7.make(at: now, using: &generator)), at: now, event: event)
    }

    @discardableResult
    mutating func send(_ event: SessionEvent, at date: Date? = nil, sourceLocation: SourceLocation = #_sourceLocation) throws -> SessionState {
        let envelope = envelope(event, at: date)
        let next = try SessionEngine.apply(envelope, to: state)
        events.append(envelope)
        state = next
        let problems = SessionEngine.invariantViolations(next)
        #expect(problems.isEmpty, "invariants after \(event): \(problems)", sourceLocation: sourceLocation)
        #expect(try SessionEngine.fold(events) == next, "fold(events) differs after \(event)", sourceLocation: sourceLocation)
        let decoded = try JSONDecoder().decode(SessionState.self, from: JSONEncoder().encode(next))
        #expect(decoded == next, "the state does not survive JSON after \(event)", sourceLocation: sourceLocation)
        return next
    }

    /// Sends an event the engine must refuse; the state stays as it was.
    mutating func refuse(_ event: SessionEvent, _ expected: EngineError, at date: Date? = nil,
                         sourceLocation: SourceLocation = #_sourceLocation)
    {
        let envelope = envelope(event, at: date)
        let before = state
        #expect(throws: expected, sourceLocation: sourceLocation) { try SessionEngine.apply(envelope, to: before) }
        #expect(state == before, sourceLocation: sourceLocation)
    }

    // MARK: Shortcuts

    @discardableResult
    mutating func start(_ routine: Routine?, exercises: [Exercise], bodyMass: Mass? = nil, settings: SessionSettings = SessionSettings(),
                        firstSides: [String: Side] = [:], timeZone: String = "Europe/London", session: SessionID? = nil) throws -> SessionState
    {
        let byID = Dictionary(exercises.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let start = SessionEvent.Start(
            session: session ?? SessionID(UUIDv7.make(at: now, using: &generator)),
            routine: routine,
            exercises: exercises,
            settings: settings,
            bodyMass: bodyMass,
            timeZone: timeZone,
            utcOffset: TrainingSession.utcOffset(in: TimeZone(identifier: timeZone)!, at: now),
            rowIDs: rowIDs(SessionEngine.rowIDCount(for: routine, exercises: byID)),
            firstSides: firstSides
        )
        return try send(.start(start))
    }

    /// The id of a slot's `index`-th row in display order.
    func row(_ slot: String, _ index: Int) throws -> RowID {
        let rows = try current.orderedRows(inSlot: slot)
        try #require(index < rows.count, "slot \(slot) has \(rows.count) rows")
        return rows[index].id
    }

    @discardableResult
    mutating func tick(_ row: RowID, _ values: SetValues, at date: Date? = nil, noLighterLoad: Bool = false,
                       sourceLocation: SourceLocation = #_sourceLocation) throws -> SessionState
    {
        let ids = rowIDs(1)
        return try send(.completeSet(.init(row: row, recordID: setID(), values: values, segmentRowIDs: ids,
                                           noLighterLoad: noLighterLoad)), at: date, sourceLocation: sourceLocation)
    }
}

/// Values in kilograms; an RPE is one the owner tapped (`entered`).
func kgReps(_ load: Decimal, _ reps: Int, rpe: Decimal? = nil) -> SetValues {
    SetValues(load: .kg(load), reps: reps, rpe: rpe, rpeMethod: rpe == nil ? nil : .entered)
}

let engineEpoch = Date(timeIntervalSince1970: 1_793_700_000) // 2026-11-02T08:40:00Z

// MARK: - A small catalogue for hand-made sessions

enum EngineCatalog {
    static func exercise(_ id: String, _ measurement: MeasurementType = .weightreps, laterality: Laterality? = nil,
                         restClass: RestClass? = .compound, equipment: Equipment? = .machine, myoStopRule: MyoStopRule? = nil,
                         myoMaxMiniSets: Int? = nil, loadBasis: LoadBasis? = nil, implements: Int? = nil,
                         primary: [Muscle] = [], secondary: [Muscle] = []) -> Exercise
    {
        Exercise(id: ExerciseID(id), name: id.capitalized, measurement: measurement, equipment: equipment, laterality: laterality,
                 loadBasis: loadBasis, implements: implements, primaryMuscles: primary, secondaryMuscles: secondary,
                 restClass: restClass, myoStopRule: myoStopRule, myoMaxMiniSets: myoMaxMiniSets)
    }

    static let press = exercise("press", primary: [.chest], secondary: [.triceps, .shoulders])
    static let row = exercise("row", primary: [.upperback], secondary: [.biceps])
    static let curl = exercise("curl", restClass: .isolation)
    static let raise = exercise("raise", restClass: .isolation, equipment: .dumbbell, loadBasis: .perimplement, implements: 2)
    static let lunge = exercise("lunge", laterality: .unilateral, equipment: .dumbbell)
    static let plank = exercise("plank", .duration, restClass: .timed, equipment: Equipment.none)
    static let pushUp = exercise("push-up", .bodyweightreps, equipment: Equipment.none)
    static let carry = exercise("carry", .shortdistanceweight, restClass: .carry, equipment: .dumbbell)
    static let all = [press, row, curl, raise, lunge, plank, pushUp, carry]

    static func sets(_ count: Int, prefix: String = "s", load: Decimal? = 50, reps: (Int, Int) = (8, 12), rpe: Decimal? = 8) -> [PlannedSet] {
        (1 ... max(1, count)).prefix(count).map { index in
            PlannedSet(key: "\(prefix)\(index)", repsMin: reps.0, repsMax: reps.1, load: load.map(Mass.kg), rpeMin: rpe, rpeMax: rpe)
        }
    }

    static func slot(_ key: String, _ exercise: Exercise, _ sets: [PlannedSet], group: String? = nil, setRest: Seconds? = nil,
                     warmupRest: Seconds? = nil, transition: Seconds? = nil, sideSwitch: Seconds? = nil,
                     sideOrder: SideOrder? = nil, sideMatch: Bool? = nil) -> RoutineSlot
    {
        RoutineSlot(key: key, exercise: exercise.id, group: group, setRest: setRest, warmupRest: warmupRest, transition: transition,
                    sideSwitch: sideSwitch, sideOrder: sideOrder, sideMatch: sideMatch, sets: sets)
    }

    static func routine(_ slots: [RoutineSlot], groups: [RoutineGroup] = [], id: String = "test-routine") -> Routine {
        Routine(id: RoutineID(id), name: "Test routine", groups: groups, slots: slots)
    }
}

// MARK: - The example records

enum EngineFixtures {
    static func exercises() throws -> [ExerciseID: Exercise] {
        let codec = RecordCodec()
        var result: [ExerciseID: Exercise] = [:]
        for record in try ExampleFixtures.records("exercises") + ExampleFixtures.records("coverage") {
            if case let .exercise(exercise) = try? codec.decode(record) { result[exercise.id] = exercise }
        }
        return result
    }

    static func routines() throws -> [RoutineID: Routine] {
        let codec = RecordCodec()
        var result: [RoutineID: Routine] = [:]
        for record in try ExampleFixtures.records("routines") + ExampleFixtures.records("coverage") {
            if case let .routine(routine) = try? codec.decode(record) { result[routine.id] = routine }
        }
        return result
    }

    static func session(_ file: String, _ id: String) throws -> SessionRecords {
        try #require(try ExampleFixtures.sessions(file).first { $0.session.id.rawValue == id }, "no session \(id) in \(file)")
    }

    static let day1First = "01a16363-cf80-73ed-85fd-704f6ce38d73"
    static let day1Second = "01a17831-8a38-7502-a232-81e9c175d65e"
    static let fullyBody = "01a17d8d-afc0-7a2b-bc4d-5e6f70819203"
    static let coverage = "01a181f3-c900-74e4-b0e2-345e3defd3cf"
    static let hotelGym = "01a188d6-2ce0-7bbd-84a8-a52f602ab29c"
}

/// Replays an example session through the engine from its records: Start
/// from its routine, then, set by set in the order done, the rest's end (a
/// touch, a Skip, or a timed set's start), the tick or the timer, then
/// Finish. The fixture's ids and instants go into the events, so the records
/// the engine writes can be compared with the fixture's.
struct ExampleReplay {
    var script: EngineScript
    let fixture: SessionRecords
    private(set) var rows: [SetID: RowID] = [:]
    /// The rest slot in force before each set's tick: the decision of the tick before.
    private(set) var restSlots: [SetID: RestSlot] = [:]

    /// Replays `fixture`. `prepare` runs right after Start; by default it
    /// makes the slot changes the record shows (a replaced exercise, an added
    /// set, a slot note).
    static func run(_ fixture: SessionRecords, prepare: ((inout EngineScript, SessionRecords) throws -> Void)? = nil,
                    finish: Bool = true) throws -> ExampleReplay
    {
        let exercises = try EngineFixtures.exercises()
        let routine = try fixture.session.routine.map { id in try #require(try EngineFixtures.routines()[id]) }
        var script = EngineScript(at: fixture.session.at, seed: 11)
        let named = Set((routine?.slots.map(\.exercise) ?? []) + fixture.session.slots.map(\.exercise))
        try script.start(routine, exercises: named.sorted().compactMap { exercises[$0] }, bodyMass: fixture.session.bodyMass,
                         timeZone: fixture.session.timezone ?? "UTC", session: fixture.session.id)
        if let prepare {
            try prepare(&script, fixture)
        } else {
            try Self.prepareFromRecords(&script, fixture, exercises: exercises)
        }
        var replay = ExampleReplay(script: script, fixture: fixture)
        try replay.playSets()
        if finish {
            try replay.script.send(.finish(.init(start: fixture.session.at, end: fixture.session.endsAt ?? replay.script.now,
                                                 sessionRPE: fixture.session.sessionRPE, notes: fixture.session.notes,
                                                 utcOffset: fixture.session.utcOffsetEnd ?? 0)),
                                   at: fixture.session.endsAt)
        }
        return replay
    }

    static func prepareFromRecords(_ script: inout EngineScript, _ fixture: SessionRecords,
                                   exercises: [ExerciseID: Exercise]) throws
    {
        for slot in fixture.session.slots {
            if let routineSlot = slot.routineSlot, routineSlot != slot.key {
                try script.send(.replaceExercise(.init(slot: routineSlot, newSlot: slot.key,
                                                       exercise: try #require(exercises[slot.exercise]))))
            }
            if let notes = slot.notes {
                try script.send(.setNote(target: .slot(slot.key), text: notes))
            }
            // Sets the plan did not have.
            for set in fixture.sets where set.slot == slot.key && set.target == nil && set.continues == nil {
                try script.send(.addSet(.init(slot: slot.key, row: script.rowIDs(1)[0], role: set.role)))
            }
        }
    }

    /// The fixture's sets, in the order they were done.
    var setsInOrder: [PerformedSet] {
        fixture.sets.sorted { $0.at != $1.at ? $0.at < $1.at : $0.order < $1.order }
    }

    mutating func playSets() throws {
        var pauses = fixture.session.pauses.flatMap { pause -> [(Date, SessionEvent)] in
            [(pause.start, .pauseClock)] + (pause.end.map { [($0, .resumeClock)] } ?? [])
        }
        for set in setsInOrder {
            let firstInstant = set.restBefore.flatMap { rest in try? script.current.restClock.map { $0.since.addingTimeInterval(TimeInterval(rest)) } }
                ?? set.startedAt ?? set.at
            while let next = pauses.first, next.0 <= firstInstant {
                try script.send(next.1, at: next.0)
                pauses.removeFirst()
            }
            try play(set)
        }
        for (date, event) in pauses {
            try script.send(event, at: date)
        }
    }

    mutating func play(_ set: PerformedSet) throws {
        let state = try script.current
        let row = try findRow(for: set, in: state)
        if let slot = state.restClock?.slot { restSlots[set.id] = slot }
        // The rest's end.
        if let rest = set.restBefore {
            let since = try #require(state.restClock?.since, "no rest clock before \(set.id)")
            let end = since.addingTimeInterval(TimeInterval(rest))
            if let started = set.startedAt {
                #expect(end == started, "the fixture's rest before \(set.id) does not end at its timer's start")
            } else if set.restSkipped == true {
                try script.send(.skipRest, at: end)
            } else if end < set.at {
                if let countdown = state.countdown, let countdownEnd = countdown.endsAt, countdownEnd > end {
                    // The examples end a rest at the first touch even inside
                    // the countdown; §4.8 waits for its end. The same record
                    // comes from shortening the countdown to the touch and
                    // restoring it at once.
                    let gap = Int(countdownEnd.timeIntervalSince(end))
                    try script.send(.adjustRest(seconds: -gap), at: end)
                    try script.send(.touchRow(row: row), at: end)
                    try script.send(.adjustRest(seconds: gap), at: end)
                } else {
                    try script.send(.touchRow(row: row), at: end)
                }
            }
        }
        if let notes = set.notes {
            try script.send(.editSet(.init(row: row, note: .set(notes))), at: max(script.now, set.startedAt ?? set.at))
        }
        var values = SetValues(load: set.load, assistance: set.assistance, reps: set.reps, duration: set.duration,
                               distance: set.distance, rpe: set.rpe, rpeMethod: set.rpeMethod,
                               reachedFailure: set.reachedFailure, failedRepAttempted: set.failedRepAttempted)
        // A pre-filled RPE is the plan's, which the tick accepts (§4.14).
        if set.rpeMethod == .prefilled {
            values.rpe = nil
            values.rpeMethod = nil
            values = state.acceptingRPEPrefill(values, row: row)
        }
        let segmentIDs = script.rowIDs(1)
        if let started = set.startedAt {
            try script.send(.startTimedSet(row: row, target: set.target?.durationMin), at: started)
            var timed = values
            timed.duration = nil
            try script.send(.stopTimedSet(.init(row: row, recordID: set.id, values: timed, segmentRowIDs: segmentIDs)), at: set.at)
            #expect(try script.current.rows[row]?.performed?.values.duration == set.duration, "timer of \(set.id)")
        } else if (set.revision ?? 1) > 1, let reps = set.reps {
            // Ticked with other reps, then corrected.
            var first = values
            first.reps = reps - 2
            try script.send(.completeSet(.init(row: row, recordID: set.id, values: first, segmentRowIDs: segmentIDs)), at: set.at)
            try script.send(.editSet(.init(row: row, values: SetValuesPatch(reps: .set(reps)))), at: set.at)
        } else {
            try script.send(.completeSet(.init(row: row, recordID: set.id, values: values, segmentRowIDs: segmentIDs)), at: set.at)
        }
        rows[set.id] = row
    }

    func findRow(for set: PerformedSet, in state: SessionState) throws -> RowID {
        if let head = set.continues {
            let headRow = try #require(rows[head], "\(set.id) continues a set not replayed")
            let link = try #require(state.chain(of: headRow).first { $0.chainPosition == set.chainPosition }, "no link \(set.chainPosition ?? 0) for \(set.id)")
            return link.id
        }
        let candidates = state.orderedRows(inSlot: set.slot).filter { $0.isHead && $0.isPending }
        let planned = set.target?.plannedSet
        let row = candidates.first { $0.plannedSet == planned && (planned != nil || $0.role == set.role) }
        return try #require(row, "no pending row for \(set.id) in \(set.slot)").id
    }

    /// The engine's records for the replayed session.
    func records() throws -> SessionRecords {
        try SessionEngine.records(script.current, adapter: "substrate-workouts@0.1.0")
    }
}

/// Each property that differs between two records, as `name: ours vs theirs`.
func recordDifferences(_ ours: FlatRecord, _ theirs: FlatRecord, ignoring: Set<String> = []) -> [String] {
    let a = Dictionary(ours.properties.map { ($0.key, $0.value) }, uniquingKeysWith: { first, _ in first })
    let b = Dictionary(theirs.properties.map { ($0.key, $0.value) }, uniquingKeysWith: { first, _ in first })
    return Set(a.keys).union(b.keys).subtracting(ignoring).sorted().compactMap { key in
        a[key] == b[key] ? nil : "\(ours.id).\(key): \(a[key].map { "\($0)" } ?? "absent") vs \(b[key].map { "\($0)" } ?? "absent")"
    }
}

// MARK: - Start and the plan

@Suite("Engine: start")
struct EngineStartTests {
    @Test("start builds the header, slots, groups and rows from the routine, at revision 1")
    func startFromRoutine() throws {
        let exercises = try EngineFixtures.exercises()
        let routine = try #require(try EngineFixtures.routines()["fully-body-collection-1"])
        var script = EngineScript(at: engineEpoch)
        let state = try script.start(routine, exercises: Array(exercises.values), bodyMass: .kg(Decimal(string: "82.4")!))
        #expect(state.revision == 1)
        #expect(state.phase == .active)
        #expect(state.session.routine == routine.id)
        #expect(state.session.routineVersion == 1)
        #expect(state.session.timezone == "Europe/London")
        #expect(state.slots.map(\.key) == routine.slots.map(\.key))
        #expect(state.slots.allSatisfy { $0.routineSlot == $0.key })
        #expect(state.groups == [GroupState(key: "a", groupKind: .superset)])
        // The chest press: W, 1, 2 and the drop 2a, created with its head.
        let chest = state.orderedRows(inSlot: "chest-press")
        #expect(chest.map(\.plannedSet) == ["w1", "s1", "s2", "s2"])
        #expect(chest.map(\.structure) == [.straight, .straight, .drop, .drop])
        #expect(chest[3].chainHead == chest[2].id)
        #expect(chest[3].target == SetTarget(plannedSet: "s2"))
        #expect(chest[2].target?.drops == 1)
        #expect(chest[1].target == SetTarget(plannedSet: "s1", termination: .reprange, repsMin: 10, repsMax: 15,
                                             load: .kg(35), rpeMin: Decimal(string: "9.5"), rpeMax: Decimal(string: "9.5")))
        // One side at a time: left, then right, the second side with the key only.
        let split = state.orderedRows(inSlot: "bulgarian-split-squat")
        #expect(split.map(\.side) == [.left, .right, .left, .right])
        #expect(split[1].target == SetTarget(plannedSet: "s1"))
        // Timed holds and an effort target imply their terminations.
        #expect(state.orderedRows(inSlot: "plank")[0].target?.termination == .time)
        #expect(state.orderedRows(inSlot: "push-up")[0].termination == .rirstop)
        #expect(state.orderedRows(inSlot: "lumbar-extension-stretch")[0].target?.termination == .reps)
        #expect(state.restClock == nil && state.countdown == nil && state.lastCompleted == nil)
        #expect(state.upNext == state.orderedRows(inSlot: "leg-press")[0].id)
    }

    @Test("an empty session has no slots; a second start and an event before start are refused")
    func emptySessionAndErrors() throws {
        var script = EngineScript(at: engineEpoch)
        script.refuse(.touchRow(row: "x"), .noSession)
        let state = try script.start(nil, exercises: [])
        #expect(state.slots.isEmpty && state.rows.isEmpty && state.plan == nil)
        #expect(state.session.routine == nil)
        script.refuse(.start(.init(session: "again", routine: nil, exercises: [], timeZone: "UTC", utcOffset: 0)), .sessionActive)
    }

    @Test("a routine slot whose exercise did not decode is left out")
    func missingExercise() throws {
        let routine = EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(2)),
            EngineCatalog.slot("ghost", EngineCatalog.exercise("ghost"), EngineCatalog.sets(2)),
        ])
        var script = EngineScript(at: engineEpoch)
        let state = try script.start(routine, exercises: [EngineCatalog.press])
        #expect(state.slots.map(\.key) == ["press"])
    }

    @Test("rowIDCount counts heads and the segments created with them")
    func rowCount() throws {
        let exercises = try EngineFixtures.exercises()
        let routines = try EngineFixtures.routines()
        // Fully Body: 44 planned sets, one drop, and five one-side slots of two sets each.
        #expect(SessionEngine.rowIDCount(for: routines["fully-body-collection-1"], exercises: exercises) == 44 + 1 + 10)
        // Coverage: 23 planned sets, three clusters, the side plank's two second sides.
        #expect(SessionEngine.rowIDCount(for: routines["coverage-every-method"], exercises: exercises) == 23 + 3 + 2)
        #expect(SessionEngine.rowIDCount(for: nil, exercises: exercises) == 0)
    }
}

// MARK: - Sets

@Suite("Engine: sets")
struct EngineSetTests {
    private func pressSession(sets: Int = 3) throws -> EngineScript {
        var script = EngineScript(at: engineEpoch)
        let routine = EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, [PlannedSet(key: "w1", role: .warmup, repsMin: 10, repsMax: 10, load: .kg(30))]
                + EngineCatalog.sets(sets), setRest: 120),
            EngineCatalog.slot("plank", EngineCatalog.plank, [PlannedSet(key: "s1", durationMin: 45, durationMax: 60)], setRest: 60),
        ])
        try script.start(routine, exercises: EngineCatalog.all)
        return script
    }

    @Test("touchRow focuses a pending row and ends the rest only once its countdown is over")
    func touch() throws {
        var script = try pressSession()
        let (w1, s1, s2) = try (script.row("press", 0), script.row("press", 1), script.row("press", 2))
        try script.tick(w1, kgReps(30, 10))
        // 45 s warm-up rest runs: a touch focuses but does not end it.
        var state = try script.send(.touchRow(row: s2), at: script.now.addingTimeInterval(20))
        #expect(state.focus == s2)
        #expect(state.upNext == s2)
        #expect(state.restClock?.endedAt == nil)
        state = try script.send(.touchRow(row: s1), at: script.now.addingTimeInterval(30))
        #expect(state.restClock?.endedAt == script.now)
        #expect(state.focus == s1)
        // A completed row changes nothing.
        let before = try script.current
        state = try script.send(.touchRow(row: w1))
        #expect(state.focus == before.focus && state.restClock == before.restClock)
        script.refuse(.touchRow(row: "nope"), .unknownRow)
    }

    @Test("editSet merges into a pending row's entered values, and into a completed row's values as an edit")
    func edit() throws {
        var script = try pressSession()
        let s1 = try script.row("press", 1)
        var state = try script.send(.editSet(.init(row: s1, values: SetValuesPatch(load: .set(.kg(50)), reps: .set(9)), note: .set("  Seat 4 "))))
        #expect(state.rows[s1]?.entered == kgReps(50, 9))
        #expect(state.rows[s1]?.notes == "Seat 4")
        state = try script.send(.editSet(.init(row: s1, values: SetValuesPatch(reps: .cleared))))
        #expect(state.rows[s1]?.entered == SetValues(load: .kg(50)))
        try script.tick(s1, kgReps(50, 10))
        state = try script.send(.editSet(.init(row: s1, values: SetValuesPatch(rpe: .set(9)))))
        #expect(state.rows[s1]?.performed?.values == kgReps(50, 10, rpe: 9))
        #expect(state.rows[s1]?.performed?.edits == 1)
        // A completed row's values must stay complete.
        script.refuse(.editSet(.init(row: s1, values: SetValuesPatch(load: .cleared))), .incompleteValues(missing: [.load]))
        // An edit that changes nothing is not an edit.
        state = try script.send(.editSet(.init(row: s1, values: SetValuesPatch(rpe: .set(9)))))
        #expect(state.rows[s1]?.performed?.edits == 1)
        let records = SessionEngine.setRecords(state)
        #expect(records.first { $0.reps == 10 }?.revision == 2)
    }

    @Test("completeSet commits the values, the rest fields and the next rest; each refusal")
    func complete() throws {
        var script = try pressSession()
        let (w1, s1, s2) = try (script.row("press", 0), script.row("press", 1), script.row("press", 2))
        let plank = try script.row("plank", 0)
        script.refuse(.completeSet(.init(row: s1, recordID: "r", values: SetValues(reps: 10))), .incompleteValues(missing: [.load]))
        script.refuse(.completeSet(.init(row: s1, recordID: "r", values: SetValues())), .incompleteValues(missing: [.load, .reps]))
        script.refuse(.completeSet(.init(row: plank, recordID: "r", values: SetValues(duration: 0))), .incompleteValues(missing: [.duration]))
        script.refuse(.completeSet(.init(row: "nope", recordID: "r", values: SetValues())), .unknownRow)

        let first = try script.tick(w1, kgReps(30, 10))
        let performed = try #require(first.rows[w1]?.performed)
        #expect(performed.restBefore == nil && performed.restPlanned == nil && performed.restSkipped == false)
        #expect(first.lastCompleted == w1 && first.focus == nil)
        #expect(first.countdown == Countdown(kind: .rest(.warmupRest), startedAt: script.now, endsAt: script.now.addingTimeInterval(45)))
        #expect(first.restClock == RestClock(since: script.now, planned: 45, slot: .warmupRest))
        script.refuse(.completeSet(.init(row: w1, recordID: "r", values: kgReps(30, 10))), .rowNotPending)

        let tickAt = script.now.addingTimeInterval(70)
        let second = try script.tick(s1, kgReps(50, 10), at: tickAt)
        let rest = try #require(second.rows[s1]?.performed)
        #expect(rest.restBefore == 70 && rest.restPlanned == 45 && rest.restSkipped == false)
        #expect(rest.priorRestClock == first.restClock)
        #expect(second.restClock?.planned == 120 && second.countdown?.kind == .rest(.setRest))

        // Another row's running timer blocks a tick.
        try script.send(.startTimedSet(row: plank, target: 45))
        script.refuse(.completeSet(.init(row: s2, recordID: "r", values: kgReps(50, 10))), .timerRunning)
    }

    @Test("uncompleteSet returns the values, restores the clock, and a re-tick keeps the first record id and rest")
    func uncomplete() throws {
        var script = try pressSession()
        let (w1, s1) = try (script.row("press", 0), script.row("press", 1))
        script.refuse(.uncompleteSet(row: s1), .rowNotCompleted)
        let afterWarmup = try script.tick(w1, kgReps(30, 10))
        let ticked = try script.tick(s1, kgReps(50, 10), at: script.now.addingTimeInterval(60))
        let record = try #require(ticked.rows[s1]?.performed)
        var state = try script.send(.uncompleteSet(row: s1), at: script.now.addingTimeInterval(5))
        #expect(state.rows[s1]?.performed == nil)
        #expect(state.rows[s1]?.entered == kgReps(50, 10))
        #expect(state.restClock == afterWarmup.restClock)
        #expect(state.countdown == nil)
        #expect(state.lastCompleted == w1)
        state = try script.tick(s1, kgReps(50, 11), at: script.now.addingTimeInterval(30))
        let again = try #require(state.rows[s1]?.performed)
        #expect(again.recordID == record.recordID)
        #expect(again.restBefore == record.restBefore && again.restPlanned == record.restPlanned)
        #expect(again.values.reps == 11)
    }

    @Test("un-ticking an older set leaves the newer rest alone; removing a running timed row stops its timer")
    func olderUntickAndTimedRemoval() throws {
        var script = try pressSession()
        let (w1, s1, plank) = try (script.row("press", 0), script.row("press", 1), script.row("plank", 0))
        try script.tick(w1, kgReps(30, 10))
        let latest = try script.tick(s1, kgReps(50, 10), at: script.now.addingTimeInterval(60))
        var state = try script.send(.uncompleteSet(row: w1), at: script.now.addingTimeInterval(10))
        #expect(state.restClock == latest.restClock && state.countdown == latest.countdown)
        #expect(state.lastCompleted == s1)
        try script.send(.startTimedSet(row: plank, target: 45))
        state = try script.send(.removeSet(row: plank))
        #expect(state.countdown == nil && state.rows[plank] == nil)
    }

    @Test("addSet puts a warm-up after the last warm-up and a working set last, with no target")
    func addSet() throws {
        var script = try pressSession(sets: 2)
        let warmup = script.rowIDs(1)[0]
        var state = try script.send(.addSet(.init(slot: "press", row: warmup, role: .warmup)))
        #expect(state.slot("press")?.rows[1] == warmup)
        #expect(state.rows[warmup]?.target == nil && state.rows[warmup]?.plannedSet == nil)
        let working = script.rowIDs(1)[0]
        state = try script.send(.addSet(.init(slot: "press", row: working, role: .working)))
        #expect(state.slot("press")?.rows.last == working)
        #expect(state.ordinal(of: working) == 3)
        #expect(state.revision == 1)
        script.refuse(.addSet(.init(slot: "nope", row: "x", role: .working)), .unknownSlot)
        script.refuse(.addSet(.init(slot: "press", row: working, role: .working)), .duplicateID)
    }

    @Test("removeSet removes a pending row, or a completed head with its whole chain, and the rest it started")
    func removeSet() throws {
        var script = EngineScript(at: engineEpoch)
        let routine = EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, [
                PlannedSet(key: "s1", repsMin: 8, repsMax: 12, load: .kg(50)),
                PlannedSet(key: "s2", structure: .drop, repsMin: 8, repsMax: 12, load: .kg(50), drops: 2),
                PlannedSet(key: "s3", repsMin: 8, repsMax: 12, load: .kg(50)),
            ]),
        ])
        try script.start(routine, exercises: EngineCatalog.all)
        let s3 = try script.row("press", 4)
        var state = try script.send(.removeSet(row: s3))
        #expect(state.slot("press")?.rows.count == 4)
        let (s1, head, drop1, drop2) = try (script.row("press", 0), script.row("press", 1), script.row("press", 2), script.row("press", 3))
        try script.tick(s1, kgReps(50, 10))
        try script.tick(head, kgReps(50, 10), at: script.now.addingTimeInterval(120))
        // Removing the second drop of a pending pair keeps the first.
        state = try script.send(.removeSet(row: drop2))
        #expect(state.chain(of: head).map(\.id) == [head, drop1])
        // Removing the completed head takes its chain and the rest its tick started.
        state = try script.send(.removeSet(row: head))
        #expect(state.slot("press")?.rows == [s1])
        #expect(state.countdown == nil)
        #expect(state.lastCompleted == s1)
        script.refuse(.removeSet(row: head), .unknownRow)
    }

    @Test("changeSetKind on pending and completed rows, and what it refuses")
    func changeKind() throws {
        var script = EngineScript(at: engineEpoch)
        let routine = EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, [PlannedSet(key: "w1", role: .warmup, repsMin: 10, repsMax: 10)] + EngineCatalog.sets(3)),
            EngineCatalog.slot("lunge", EngineCatalog.lunge, EngineCatalog.sets(1)),
        ])
        try script.start(routine, exercises: EngineCatalog.all)
        let (w1, s1, s2, s3) = try (script.row("press", 0), script.row("press", 1), script.row("press", 2), script.row("press", 3))
        // A working set made a warm-up moves to the warm-ups.
        var state = try script.send(.changeSetKind(.init(row: s2, kind: .warmup)))
        #expect(state.slot("press")?.rows == [w1, s2, s1, s3])
        #expect(state.ordinal(of: s2) == 2 && state.ordinal(of: s3) == 2)
        // Back to working: after the warm-ups, the planned termination restored.
        state = try script.send(.changeSetKind(.init(row: s2, kind: .working)))
        #expect(state.slot("press")?.rows == [w1, s2, s1, s3])
        #expect(state.rows[s2]?.termination == .reprange)
        state = try script.send(.changeSetKind(.init(row: s3, kind: .failure)))
        #expect(state.rows[s3]?.termination == .failure)
        // Drop: drop rows (planned drops or 1); myo: nothing until the head is ticked.
        state = try script.send(.changeSetKind(.init(row: s1, kind: .drop)))
        #expect(state.chain(of: s1).map(\.structure) == [.drop, .drop])
        state = try script.send(.changeSetKind(.init(row: s1, kind: .myo)))
        #expect(state.chain(of: s1).count == 1 && state.rows[s1]?.structure == .myo)
        let drop = try script.send(.changeSetKind(.init(row: s3, kind: .drop))).chain(of: s3)[1].id
        script.refuse(.changeSetKind(.init(row: drop, kind: .working)), .kindNotAllowed)
        let lunge = try script.row("lunge", 0)
        script.refuse(.changeSetKind(.init(row: lunge, kind: .drop)), .kindNotAllowed)
        // A completed row: failure sets reachedFailure and counts as an edit.
        try script.tick(w1, kgReps(30, 10))
        state = try script.send(.changeSetKind(.init(row: w1, kind: .failure)))
        #expect(state.rows[w1]?.role == .working)
        #expect(state.rows[w1]?.performed?.values.reachedFailure == true)
        #expect(state.rows[w1]?.performed?.edits == 1)
        // A completed head grows its first mini-set at once.
        try script.tick(s1, kgReps(40, 15, rpe: 9))
        state = try script.send(.changeSetKind(.init(row: s3, kind: .myo)))
        #expect(state.chain(of: s3).count == 1)
        try script.tick(s3, kgReps(40, 16))
        let mini = try #require(script.current.chain(of: s3).last)
        #expect(!mini.isHead && mini.isPending)
        try script.tick(mini.id, kgReps(40, 4))
        script.refuse(.changeSetKind(.init(row: s3, kind: .drop)), .kindNotAllowed)
    }

    @Test("setRowRest overrides the rest after the whole set, on its head")
    func rowRest() throws {
        var script = try pressSession()
        let (s1, s2) = try (script.row("press", 1), script.row("press", 2))
        var state = try script.send(.setRowRest(row: s1, seconds: 200))
        #expect(state.rows[s1]?.restOverride == 200)
        state = try script.tick(s1, kgReps(50, 10))
        #expect(state.restClock?.slot == .override && state.restClock?.planned == 200)
        state = try script.send(.setRowRest(row: s2, seconds: nil))
        #expect(state.rows[s2]?.restOverride == nil)
        #expect(state.revision == 1)
    }
}

// MARK: - Exercises and groups

@Suite("Engine: exercises and groups")
struct EngineStructureTests {
    private func threeSlots() throws -> EngineScript {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(2)),
            EngineCatalog.slot("row", EngineCatalog.row, EngineCatalog.sets(2)),
            EngineCatalog.slot("curl", EngineCatalog.curl, EngineCatalog.sets(2)),
        ]), exercises: EngineCatalog.all)
        return script
    }

    @Test("addExercise appends a slot with its rows and rests, or inserts it after a group block")
    func addExercise() throws {
        var script = try threeSlots()
        var state = try script.send(.addExercise(.init(slot: "raise", exercise: EngineCatalog.raise, roles: [.working, .warmup, .working],
                                                       rowIDs: script.rowIDs(3), rests: SlotRests(setRest: 75))))
        #expect(state.slots.map(\.key) == ["press", "row", "curl", "raise"])
        #expect(state.orderedRows(inSlot: "raise").map(\.role) == [.warmup, .working, .working])
        #expect(state.slot("raise")?.rests.setRest == 75)
        #expect(state.revision == 2)
        state = try script.send(.groupExercises(slot: "row", partner: "press", group: nil))
        state = try script.send(.addExercise(.init(slot: "lunge", exercise: EngineCatalog.lunge, roles: [.working], rowIDs: script.rowIDs(2),
                                                   after: "press", firstSide: .right)))
        #expect(state.slots.map(\.key) == ["press", "row", "lunge", "curl", "raise"])
        #expect(state.orderedRows(inSlot: "lunge").map(\.side) == [.right, .left])
        script.refuse(.addExercise(.init(slot: "lunge", exercise: EngineCatalog.lunge)), .duplicateID)
        script.refuse(.addExercise(.init(slot: "x", exercise: EngineCatalog.lunge, after: "nope")), .unknownSlot)
    }

    @Test("removeExercise removes the slot and its rows, and a group left with one member")
    func removeExercise() throws {
        var script = try threeSlots()
        try script.send(.groupExercises(slot: "row", partner: "press", group: nil))
        let rows = try script.current.slot("row")!.rows
        let state = try script.send(.removeExercise(slot: "row"))
        #expect(state.slots.map(\.key) == ["press", "curl"])
        #expect(rows.allSatisfy { state.rows[$0] == nil })
        #expect(state.groups.isEmpty && state.slot("press")?.group == nil)
        script.refuse(.removeExercise(slot: "row"), .unknownSlot)
    }

    @Test("reorderExercises takes a permutation and keeps each group together at its first member")
    func reorder() throws {
        var script = try threeSlots()
        try script.send(.groupExercises(slot: "curl", partner: "press", group: nil))
        #expect(try script.current.slots.map(\.key) == ["press", "curl", "row"])
        let state = try script.send(.reorderExercises(slots: ["row", "curl", "press"]))
        #expect(state.slots.map(\.key) == ["row", "curl", "press"])
        let split = try script.send(.reorderExercises(slots: ["curl", "row", "press"]))
        #expect(split.slots.map(\.key) == ["curl", "press", "row"])
        script.refuse(.reorderExercises(slots: ["row", "curl"]), .invalidOrder)
        script.refuse(.reorderExercises(slots: ["row", "curl", "curl"]), .invalidOrder)
    }

    @Test("replaceExercise in place before any set is done, else a new slot for the pending sets")
    func replace() throws {
        var script = try threeSlots()
        try script.send(.setSlotRest(slot: "press", rest: .setRest, seconds: 100))
        var state = try script.send(.replaceExercise(.init(slot: "press", newSlot: "fly", exercise: EngineCatalog.exercise("fly"))))
        #expect(state.slots.map(\.key) == ["fly", "row", "curl"])
        #expect(state.slot("fly")?.routineSlot == "press" && state.slot("fly")?.rests.setRest == 100)
        #expect(state.orderedRows(inSlot: "fly").map(\.target?.plannedSet) == ["s1", "s2"])
        // With a set done, the old slot keeps it and the pending set moves.
        let r1 = try script.row("row", 0)
        try script.tick(r1, kgReps(50, 10))
        state = try script.send(.replaceExercise(.init(slot: "row", newSlot: "lunge", exercise: EngineCatalog.lunge)))
        #expect(state.slots.map(\.key) == ["fly", "row", "lunge", "curl"])
        #expect(state.slot("row")?.rows == [r1])
        let lunge = state.orderedRows(inSlot: "lunge")
        #expect(lunge.map(\.side) == [.left, .right])
        #expect(lunge[0].target?.plannedSet == "s2" && state.slot("lunge")?.routineSlot == "row")
        script.refuse(.replaceExercise(.init(slot: "row", newSlot: "row", exercise: EngineCatalog.curl)), .duplicateID)
    }

    @Test("groupExercises makes a group in the gym whose kind follows its size; ungroup takes members out")
    func groups() throws {
        var script = try threeSlots()
        try script.send(.addExercise(.init(slot: "raise", exercise: EngineCatalog.raise)))
        var state = try script.send(.groupExercises(slot: "curl", partner: "press", group: nil))
        #expect(state.slots.map(\.key) == ["press", "curl", "row", "raise"])
        #expect(state.groups == [GroupState(key: "a", groupKind: .superset, madeInGym: true)])
        state = try script.send(.groupExercises(slot: "raise", partner: "curl", group: nil))
        #expect(state.slots.map(\.key) == ["press", "curl", "raise", "row"])
        #expect(state.group("a")?.groupKind == .triset)
        state = try script.send(.groupExercises(slot: "row", partner: "press", group: nil))
        #expect(state.group("a")?.groupKind == .giantset)
        state = try script.send(.ungroupExercise(slot: "curl"))
        #expect(state.slots.map(\.key) == ["press", "raise", "row", "curl"])
        #expect(state.group("a")?.groupKind == .triset)
        script.refuse(.ungroupExercise(slot: "curl"), .invalidOrder)
        script.refuse(.groupExercises(slot: "row", partner: "row", group: nil), .groupTooSmall)
        script.refuse(.groupExercises(slot: "nope", partner: "row", group: nil), .unknownSlot)
        script.refuse(.groupExercises(slot: "row", partner: "raise", group: nil), .invalidOrder)
        // A kind set by hand stays.
        state = try script.send(.setGroup(.init(group: "a", kind: .circuit)))
        state = try script.send(.ungroupExercise(slot: "row"))
        #expect(state.group("a")?.groupKind == .circuit)
        state = try script.send(.ungroupExercise(slot: "raise"))
        #expect(state.groups.isEmpty && state.slots.allSatisfy { $0.group == nil })
        // A second group takes the next letter.
        try script.send(.groupExercises(slot: "curl", partner: "row", group: nil))
        state = try script.send(.groupExercises(slot: "raise", partner: "press", group: nil))
        #expect(Set(state.groups.map(\.key)) == ["a", "b"])
    }

    @Test("setGroup, setSlotRest and setNote store their values and raise the revision")
    func setters() throws {
        var script = try threeSlots()
        try script.send(.groupExercises(slot: "row", partner: "press", group: "x"))
        var state = try script.send(.setGroup(.init(group: "x", pairing: .set(.antagonist), roundRest: .set(90))))
        #expect(state.group("x") == GroupState(key: "x", groupKind: .superset, pairing: .antagonist, roundRest: 90, madeInGym: true, changedInGym: true))
        state = try script.send(.setGroup(.init(group: "x", roundRest: .cleared)))
        #expect(state.group("x")?.roundRest == nil && state.group("x")?.pairing == .antagonist)
        script.refuse(.setGroup(.init(group: "nope")), .unknownGroup)
        state = try script.send(.setSlotRest(slot: "curl", rest: .warmupRest, seconds: 30))
        #expect(state.slot("curl")?.rests == SlotRests(warmupRest: 30))
        state = try script.send(.setSlotRest(slot: "curl", rest: .warmupRest, seconds: nil))
        #expect(state.slot("curl")?.rests == SlotRests())
        state = try script.send(.setNote(target: .slot("curl"), text: "Cable at 3"))
        #expect(state.slot("curl")?.notes == "Cable at 3")
        state = try script.send(.setNote(target: .session, text: "Busy gym"))
        #expect(state.session.notes == "Busy gym")
        state = try script.send(.setNote(target: .session, text: "  "))
        #expect(state.session.notes == nil)
        script.refuse(.setSlotRest(slot: "nope", rest: .setRest, seconds: 1), .unknownSlot)
        script.refuse(.setNote(target: .slot("nope"), text: "x"), .unknownSlot)
        // Start, the group, two group changes, two rests and three notes.
        #expect(state.revision == 1 + 1 + 2 + 2 + 3)
    }
}

// MARK: - Clocks and timers

@Suite("Engine: clocks and countdowns")
struct EngineClockTests {
    private func session() throws -> EngineScript {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(3), setRest: 90),
            EngineCatalog.slot("plank", EngineCatalog.plank, [PlannedSet(key: "s1", durationMin: 45), PlannedSet(key: "s2", durationMin: 45,
                                                                                                            timerMode: .stopwatch)]),
            EngineCatalog.slot("carry", EngineCatalog.carry, [PlannedSet(key: "s1", termination: .distance, load: .kg(20),
                                                                         distance: Length(metres: 40))]),
        ]), exercises: EngineCatalog.all)
        return script
    }

    @Test("pauseClock and resumeClock record pauses and change no rest")
    func pauses() throws {
        var script = try session()
        try script.tick(script.row("press", 0), kgReps(50, 10))
        let before = try script.current
        var state = try script.send(.pauseClock, at: script.now.addingTimeInterval(10))
        #expect(state.session.isPaused && state.session.pauses == [Pause(start: script.now)])
        #expect(state.countdown == before.countdown && state.restClock == before.restClock)
        script.refuse(.pauseClock, .clockPaused)
        state = try script.send(.resumeClock, at: script.now.addingTimeInterval(240))
        #expect(!state.session.isPaused && state.session.pauses.last?.end == script.now)
        script.refuse(.resumeClock, .clockNotPaused)
        #expect(state.revision == 3)
    }

    @Test("startRest runs the standalone timer; adjustRest never moves the end before now; skipRest marks a skip")
    func restControls() throws {
        var script = try session()
        script.refuse(.adjustRest(seconds: 15), .noTimer)
        script.refuse(.skipRest, .noTimer)
        var state = try script.send(.startRest(seconds: 60))
        #expect(state.countdown == Countdown(kind: .manual, startedAt: script.now, endsAt: script.now.addingTimeInterval(60)))
        #expect(state.restClock == nil)
        try script.tick(script.row("press", 0), kgReps(50, 10))
        let since = script.now
        state = try script.send(.adjustRest(seconds: 15), at: since.addingTimeInterval(10))
        #expect(state.countdown?.endsAt == since.addingTimeInterval(105))
        #expect(state.restClock?.planned == 105)
        state = try script.send(.adjustRest(seconds: -15))
        state = try script.send(.adjustRest(seconds: -15))
        #expect(state.restClock?.planned == 75)
        // −15 s with 5 s left ends the rest now, not before.
        state = try script.send(.adjustRest(seconds: -15), at: since.addingTimeInterval(70))
        #expect(state.countdown?.endsAt == script.now && state.restClock?.planned == 70)
        state = try script.send(.adjustRest(seconds: 15), at: since.addingTimeInterval(72))
        state = try script.send(.skipRest, at: since.addingTimeInterval(80))
        #expect(state.countdown == nil)
        #expect(state.restClock?.endedAt == script.now && state.restClock?.skipped == true)
        let tick = try script.tick(script.row("press", 1), kgReps(50, 10), at: since.addingTimeInterval(100))
        let performed = try #require(tick.rows[script.row("press", 1)]?.performed)
        #expect(performed.restBefore == 80 && performed.restPlanned == 85 && performed.restSkipped)
    }

    @Test("a Skip after the planned rest ran out is not a skipped rest")
    func lateSkip() throws {
        var script = try session()
        try script.tick(script.row("press", 0), kgReps(50, 10))
        let state = try script.send(.skipRest, at: script.now.addingTimeInterval(95))
        #expect(state.restClock?.skipped == false && state.restClock?.endedAt == script.now)
    }

    @Test("a timed set replaces the rest, ends the rest measurement and fills its duration at Stop")
    func timedSets() throws {
        var script = try session()
        let (p1, p2) = try (script.row("plank", 0), script.row("plank", 1))
        let press = try script.row("press", 0)
        script.refuse(.startTimedSet(row: press, target: nil), .noTimer)
        script.refuse(.stopTimedSet(.init(row: p1, recordID: "r", values: SetValues())), .noTimer)
        script.refuse(.cancelTimedSet(row: p1), .noTimer)
        try script.tick(press, kgReps(50, 10))
        let since = script.now
        var state = try script.send(.startTimedSet(row: p1, target: 45), at: since.addingTimeInterval(30))
        #expect(state.countdown == Countdown(kind: .timedSet(p1), startedAt: script.now, endsAt: script.now.addingTimeInterval(45)))
        #expect(state.restClock?.endedAt == script.now)
        #expect(state.rows[p1]?.timerStartedAt == script.now)
        script.refuse(.startTimedSet(row: p2, target: nil), .timerRunning)
        script.refuse(.startRest(seconds: 30), .timerRunning)
        script.refuse(.adjustRest(seconds: 15), .noTimer)
        script.refuse(.finish(.init(start: since, end: script.now, utcOffset: 0)), .timerRunning)
        // Past the target the count runs on; Stop takes the time run.
        state = try script.send(.stopTimedSet(.init(row: p1, recordID: script.setID(), values: SetValues())), at: script.now.addingTimeInterval(62))
        let performed = try #require(state.rows[p1]?.performed)
        #expect(performed.values.duration == 62 && performed.startedAt == script.now.addingTimeInterval(-62))
        #expect(performed.restBefore == 30)
        // A stopwatch: no end. Cancel clears it; a typed duration wins at Stop.
        state = try script.send(.startTimedSet(row: p2, target: nil), at: script.now.addingTimeInterval(20))
        #expect(state.countdown?.endsAt == nil)
        state = try script.send(.cancelTimedSet(row: p2))
        #expect(state.countdown == nil && state.rows[p2]?.timerStartedAt == nil)
        try script.send(.startTimedSet(row: p2, target: nil), at: script.now.addingTimeInterval(5))
        state = try script.send(.stopTimedSet(.init(row: p2, recordID: script.setID(), values: SetValues(duration: 40))),
                                at: script.now.addingTimeInterval(44))
        #expect(state.rows[p2]?.performed?.values.duration == 40)
        // A carry's stopwatch records its time beside the distance.
        let carry = try script.row("carry", 0)
        try script.send(.startTimedSet(row: carry, target: nil), at: script.now.addingTimeInterval(60))
        state = try script.send(.stopTimedSet(.init(row: carry, recordID: script.setID(),
                                                    values: SetValues(load: .kg(20), distance: Length(metres: 40)))),
                                at: script.now.addingTimeInterval(36))
        #expect(state.rows[carry]?.performed?.values.duration == 36)
    }

    @Test("two countdowns never coexist: a rest replaces the standalone timer, a timed set replaces a rest")
    func oneCountdown() throws {
        var script = try session()
        try script.send(.startRest(seconds: 300))
        var state = try script.tick(script.row("press", 0), kgReps(50, 10))
        #expect(state.countdown?.kind == .rest(.setRest))
        state = try script.send(.startRest(seconds: 30))
        #expect(state.countdown?.kind == .manual && state.restClock?.planned == 90)
        state = try script.send(.startTimedSet(row: script.row("plank", 0), target: nil))
        #expect(state.countdown?.timedRow != nil)
    }
}

// MARK: - Finish and discard

@Suite("Engine: finish")
struct EngineFinishTests {
    private func session() throws -> EngineScript {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, [PlannedSet(key: "w1", role: .warmup, repsMin: 10, repsMax: 10)] + EngineCatalog.sets(3)),
            EngineCatalog.slot("row", EngineCatalog.row, EngineCatalog.sets(2), group: "a"),
            EngineCatalog.slot("curl", EngineCatalog.curl, EngineCatalog.sets(2), group: "a"),
            EngineCatalog.slot("raise", EngineCatalog.raise, EngineCatalog.sets(2)),
        ], groups: [RoutineGroup(key: "a", groupKind: .superset)]), exercises: EngineCatalog.all)
        return script
    }

    @Test("Finish drops unticked rows, empty slots and one-member groups, renumbers, and clamps the times")
    func finishDrop() throws {
        var script = try session()
        let start = script.now
        // Done: press set 2 only (so it becomes set 1), and the row's set 1.
        let p2 = try script.row("press", 2)
        try script.tick(p2, kgReps(50, 10), at: start.addingTimeInterval(120))
        try script.send(.pauseClock, at: start.addingTimeInterval(150))
        try script.send(.resumeClock, at: start.addingTimeInterval(180))
        try script.send(.pauseClock, at: start.addingTimeInterval(280))
        try script.tick(script.row("row", 0), kgReps(40, 12), at: start.addingTimeInterval(300))
        let state = try script.send(.finish(.init(start: start.addingTimeInterval(200), end: start.addingTimeInterval(250),
                                                  sessionRPE: 7, notes: "Short", utcOffset: 0)), at: start.addingTimeInterval(500))
        #expect(state.phase == .finished)
        #expect(state.slots.map(\.key) == ["press", "row"])
        #expect(state.groups.isEmpty && state.slot("row")?.group == nil)
        // The start is clamped to the first set, the end to the last.
        #expect(state.session.at == start.addingTimeInterval(120))
        #expect(state.session.endsAt == start.addingTimeInterval(300))
        // The open pause closes at the end.
        #expect(state.session.pauses == [Pause(start: start.addingTimeInterval(150), end: start.addingTimeInterval(180)),
                                         Pause(start: start.addingTimeInterval(280), end: start.addingTimeInterval(300))])
        #expect(state.session.sessionRPE == 7 && state.session.notes == "Short")
        // Start, two pauses' ends and starts, then Finish.
        #expect(state.revision == 1 + 3 + 1)
        let records = SessionEngine.records(state, adapter: "substrate-workouts@0.1.0")
        let press = try #require(records.sets.first { $0.slot == "press" })
        #expect(press.ordinal == 1 && press.order == 1 && press.restBefore == nil)
        #expect(records.session.status == .finished && records.session.revision == 5)
        script.refuse(.touchRow(row: p2), .finished)
        script.refuse(.discard, .finished)
    }

    @Test("Finish marks unticked rows done as shown, at the end, with no rest fields")
    func finishMarkDone() throws {
        var script = try session()
        let start = script.now
        let (w1, s1, s2, s3) = try (script.row("press", 0), script.row("press", 1), script.row("press", 2), script.row("press", 3))
        try script.tick(w1, kgReps(30, 10), at: start.addingTimeInterval(60))
        let end = start.addingTimeInterval(900)
        let marks = try [
            UntickedFate.MarkDone(row: s1, recordID: script.setID(), values: kgReps(50, 10)),
            UntickedFate.MarkDone(row: s2, recordID: script.setID(), values: SetValues(reps: 9)), // nothing to commit: dropped
            UntickedFate.MarkDone(row: s3, recordID: script.setID(), values: kgReps(50, 8)),
            UntickedFate.MarkDone(row: script.row("curl", 0), recordID: script.setID(), values: kgReps(20, 12)),
        ]
        let state = try script.send(.finish(.init(start: start, end: end, unticked: .markDone(marks), utcOffset: 0)), at: end)
        let records = SessionEngine.records(state, adapter: "substrate-workouts@0.1.0")
        #expect(records.sets.map(\.slot) == ["press", "press", "press", "curl"])
        let marked = records.sets.filter { $0.at == end }
        #expect(marked.count == 3)
        #expect(marked.allSatisfy { $0.restBefore == nil && $0.restPlanned == nil })
        #expect(records.sets.filter { $0.slot == "press" }.map(\.ordinal) == [1, 1, 2])
        #expect(records.sets.filter { $0.slot == "press" }.map(\.order) == [1, 2, 3])
        #expect(records.session.slots.map(\.key) == ["press", "curl"])
        #expect(records.session.slots.allSatisfy { $0.group == nil })
    }

    @Test("discard is accepted while active and changes nothing; the controller deletes the journal")
    func discard() throws {
        var script = try session()
        let before = try script.current
        let state = try script.send(.discard)
        #expect(state == before)
    }
}

// MARK: - Counting

@Suite("Engine: counting")
struct EngineCountingTests {
    @Test("sets count working chain heads once; volume follows load basis, body mass and the warm-up switch")
    func counting() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, [PlannedSet(key: "w1", role: .warmup, repsMin: 10, repsMax: 10),
                                                               PlannedSet(key: "s1", structure: .drop, load: .kg(50))]),
            EngineCatalog.slot("raise", EngineCatalog.raise, EngineCatalog.sets(1)),
            EngineCatalog.slot("lunge", EngineCatalog.lunge, EngineCatalog.sets(1)),
            EngineCatalog.slot("push-up", EngineCatalog.pushUp, EngineCatalog.sets(1)),
            EngineCatalog.slot("plank", EngineCatalog.plank, [PlannedSet(key: "s1", durationMin: 30)]),
        ]), exercises: EngineCatalog.all, bodyMass: .kg(80))
        var state = try script.current
        #expect(Counting.workingSetsPlanned(state) == 5)
        try script.tick(script.row("press", 0), kgReps(30, 10))
        try script.tick(script.row("press", 1), kgReps(50, 10))
        try script.tick(script.row("press", 2), kgReps(40, 5))
        try script.tick(script.row("raise", 0), kgReps(6, 12))
        try script.tick(script.row("lunge", 0), kgReps(10, 10))
        try script.tick(script.row("lunge", 1), kgReps(10, 9))
        try script.tick(script.row("push-up", 0), SetValues(reps: 15))
        state = try script.send(.startTimedSet(row: script.row("plank", 0), target: 30))
        state = try script.send(.stopTimedSet(.init(row: script.row("plank", 0), recordID: script.setID(), values: SetValues())),
                                at: script.now.addingTimeInterval(40))
        #expect(Counting.workingSetsDone(state) == 5)
        // 500 + 200 (drop) + 6 × 12 × 2 dumbbells + 100 + 90 (each side) + 80 × 15 bodyweight; the warm-up left out.
        #expect(Counting.volume(state) == .kg(500 + 200 + 144 + 190 + 1200))
        var withWarmups = state
        withWarmups.settings.countWarmupsInVolume = true
        #expect(Counting.volume(withWarmups) == .kg(2234 + 300))
        let muscles = Counting.setsPerMuscle(state)
        #expect(muscles[.chest] == 1 && muscles[.triceps] == Decimal(string: "0.5"))
        let records = SessionEngine.setRecords(state)
        #expect(Counting.workingSets(records) == 5)
        // Without a body mass a bodyweight set adds nothing.
        #expect(Counting.volume(records, exercises: state.exercises, bodyMass: nil, unit: .kg, includeWarmups: false) == .kg(1034))
        #expect(Counting.volume(records, exercises: state.exercises, bodyMass: .kg(80), unit: .lb, includeWarmups: false)
            == .lb(4925))
    }
}

// MARK: - The journal

@Suite("Engine: journal")
struct EngineJournalTests {
    @Test("the engine's events and state go through the session journal and fold back to the same state")
    func journalRoundTrip() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let replay = try ExampleReplay.run(EngineFixtures.session("sessions", EngineFixtures.fullyBody), finish: false)
        let journal = ActiveSessionJournal(directory: directory.url, writingOptions: .atomic)
        let state = try replay.script.current
        try journal.write(events: replay.script.events, state: state)
        let loaded = try #require(try journal.load { try SessionEngine.fold($0) })
        #expect(loaded.cacheMatched)
        #expect(loaded.state == state)
        #expect(loaded.events == replay.script.events)
        // A stale cache loses to the fold and is rewritten.
        var stale = state
        stale.revision += 10
        try journal.write(events: replay.script.events, state: stale)
        let refolded = try #require(try journal.load { try SessionEngine.fold($0) })
        #expect(!refolded.cacheMatched && refolded.state == state)
        #expect(try journal.read()?.cachedState == state)
    }

    @Test("the V1 sync sink takes the engine's events and state")
    func syncSink() async throws {
        let replay = try ExampleReplay.run(EngineFixtures.session("sessions", EngineFixtures.fullyBody))
        let sink: any SessionSyncSink<EventEnvelope, SessionState> = NoSync()
        let state = try replay.script.current
        await sink.applied(try #require(replay.script.events.last), state: state)
        await sink.finished(SessionEngine.records(state, adapter: "substrate-workouts@0.1.0"))
    }
}

// MARK: - Finish to records, against the examples

@Suite("Engine: records from the examples")
struct EngineRecordTests {
    private let codec = RecordCodec()

    private func compare(_ replay: ExampleReplay, ignoringSession: Set<String>, ignoringSet: (PerformedSet) -> Set<String> = { _ in [] })
        throws
    {
        let ours = try replay.records()
        let theirs = replay.fixture
        let sessionDiff = recordDifferences(codec.encode(ours.session), codec.encode(theirs.session), ignoring: ignoringSession)
        #expect(sessionDiff.isEmpty, "session: \(sessionDiff)")
        #expect(ours.sets.count == theirs.sets.count)
        let byID = Dictionary(uniqueKeysWithValues: theirs.sets.map { ($0.id, $0) })
        for set in ours.sets {
            guard let fixture = byID[set.id] else {
                Issue.record("set \(set.id) is not in the fixture")
                continue
            }
            let diff = recordDifferences(codec.encode(set), codec.encode(fixture), ignoring: ignoringSet(fixture))
            #expect(diff.isEmpty, "\(diff)")
        }
    }

    @Test("encode from the engine: the 27 October Fully Body session reproduces its records, ids included")
    func fullyBody() throws {
        let replay = try ExampleReplay.run(EngineFixtures.session("sessions", EngineFixtures.fullyBody))
        // V1 counts its own revisions (§4.12) and links Health later.
        try compare(replay, ignoringSession: ["healthKitWorkout", "workout", "revision"])
        #expect(try replay.records().session.revision == 2)
    }

    @Test("the two Day 1 sessions reproduce their records: a skip, a pause, a corrected set, two myo-rep chains")
    func dayOne() throws {
        for id in [EngineFixtures.day1First, EngineFixtures.day1Second] {
            let replay = try ExampleReplay.run(EngineFixtures.session("sessions", id))
            try compare(replay, ignoringSession: ["healthKitWorkout", "workout", "revision"])
        }
    }

    @Test("the coverage session reproduces its records, save the cluster targets the fixture leaves bare")
    func coverage() throws {
        let replay = try ExampleReplay.run(EngineFixtures.session("coverage", EngineFixtures.coverage))
        // §4.5 gives a cluster's later records `repsMin = repsMax = miniSetReps`;
        // coverage.yaml writes their target as the planned set's key alone.
        try compare(replay, ignoringSession: ["healthKitWorkout", "workout", "revision"]) { fixture in
            fixture.structure == .cluster && fixture.continues != nil ? ["target"] : []
        }
        let cluster = try replay.records().sets.filter { $0.structure == .cluster && $0.continues != nil }
        #expect(cluster.count == 3)
        #expect(cluster.allSatisfy { $0.target == SetTarget(plannedSet: "s1", repsMin: 3, repsMax: 3) })
    }

    @Test("an empty session's added slots store the rests the next session needs (§3.5, the 29 October session)")
    func hotelGym() throws {
        let fixture = try EngineFixtures.session("coverage", EngineFixtures.hotelGym)
        let exercises = try EngineFixtures.exercises()
        let replay = try ExampleReplay.run(fixture, prepare: { script, fixture in
            for slot in fixture.session.slots {
                let roles = fixture.sets.filter { $0.slot == slot.key && $0.continues == nil }.map(\.role)
                try script.send(.addExercise(.init(slot: slot.key, exercise: #require(exercises[slot.exercise]), roles: roles,
                                                   rowIDs: script.rowIDs(roles.count))))
            }
            // The owner's own values: the warm-up rest, the superset, its transition, pairing and round rest.
            try script.send(.setSlotRest(slot: "leg-extension", rest: .warmupRest, seconds: 30))
            try script.send(.groupExercises(slot: "seated-row", partner: "chest-press", group: nil))
            try script.send(.setSlotRest(slot: "chest-press", rest: .transition, seconds: 15))
            try script.send(.setGroup(.init(group: "a", pairing: .set(.antagonist), roundRest: .set(90))))
        })
        let ours = try replay.records()
        #expect(ours.session.slots == fixture.session.slots)
        #expect(ours.session.groups == fixture.session.groups)
        #expect(ours.session.name == fixture.session.name)
        // V1 writes no setup; the rest is the fixture's.
        try compare(replay, ignoringSession: ["status", "endsAt", "utcOffsetEnd", "deload", "revision"]) { _ in ["setup"] }
    }
}
