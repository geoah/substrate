import Foundation
@testable import WorkoutsCore
import Testing

// The data-model review's decisions (2026-10-31) as the app holds them:
// the codec reads substrate's canonical reference objects and keeps unknown
// fields at every level (A1, A2); the engine writes how each RPE got there,
// how each set was completed, what its numbers meant, the full target, the
// plan snapshot, the sport and the zone (D1 to D13, A3); the suggestion
// reads a pre-filled RPE as no rating; Apple Health never gets an effort
// score of 0; a history file from before the review still loads.

private func d(_ text: String) -> Decimal { Decimal(string: text, locale: Locale(identifier: "en_US_POSIX"))! }

private let kinds = KindNames.v1

/// Every reference of a record, at every level, as substrate serves it:
/// `{"ref": "<kind>/<id>"}` in place of the bare path.
private func canonical(_ object: JSONObject, _ schema: [PropertySchema]) -> JSONObject {
    var out = JSONObject()
    for (key, value) in object {
        guard let property = schema.first(where: { $0.name == key }) else {
            out[key] = value
            continue
        }
        func convert(_ value: JSONValue) -> JSONValue {
            switch (property.type, value) {
            case let (.reference, .string(path)): .object(["ref": .string(path)])
            case let (.object, .object(fields)): .object(canonical(fields, property.fields))
            default: value
            }
        }
        if property.repeated, case let .array(items) = value {
            out[key] = .array(items.map(convert))
        } else {
            out[key] = convert(value)
        }
    }
    return out
}

/// The inverse: every `{"ref": path}` back to the bare path.
private func shorthand(_ object: JSONObject, _ schema: [PropertySchema]) -> JSONObject {
    var out = JSONObject()
    for (key, value) in object {
        guard let property = schema.first(where: { $0.name == key }) else {
            out[key] = value
            continue
        }
        func convert(_ value: JSONValue) -> JSONValue {
            switch (property.type, value) {
            case let (.reference, .object(fields)): fields["ref"] ?? value
            case let (.object, .object(fields)): .object(shorthand(fields, property.fields))
            default: value
            }
        }
        if property.repeated, case let .array(items) = value {
            out[key] = .array(items.map(convert))
        } else {
            out[key] = convert(value)
        }
    }
    return out
}

private func encodeAny(_ codec: RecordCodec, _ record: FlatRecord) throws -> FlatRecord {
    try codec.encode(codec.decode(record))
}

// MARK: - Codec

@Suite("Data-model review: the codec reads what substrate serves (A1, A2)")
struct ReviewCodecTests {
    let codec = RecordCodec()

    @Test("every example record in substrate's canonical form, references as {ref}, decodes to the same values and round-trips as read")
    func canonicalReferences() throws {
        var references = 0
        for original in try ExampleFixtures.allRecords() {
            let schema = try #require(codec.schema(for: original.kind))
            let served = FlatRecord(kind: original.kind, id: original.id,
                                    properties: canonical(original.properties, schema.allProperties))
            let text = String(decoding: JSONValue.object(served.properties).data(pretty: false), as: UTF8.self)
            references += text.components(separatedBy: "\"ref\"").count - 1
            let encoded = try encodeAny(codec, served)
            // Written back in the form it was read, nothing lost.
            #expect(normalizedProperties(encoded.properties, schema.allProperties)
                == normalizedProperties(served.properties, schema.allProperties), "\(original.id)")
            // The same record as the shorthand reads.
            #expect(normalizedProperties(shorthand(encoded.properties, schema.allProperties), schema.allProperties)
                == normalizedProperties(original.properties, schema.allProperties), "\(original.id)")
        }
        #expect(references > 300)
        // The typed values are the targets, at the top level and nested.
        let set = try codec.decodeSet(FlatRecord(kind: kinds.set, id: "x", properties: [
            "at": .string("2026-10-27T13:42:10Z"),
            "session": .object(["ref": .string("\(kinds.session)/s1")]),
            "exercise": .object(["ref": .string("\(kinds.exercise)/chest-press-machine")]),
            "slot": .string("chest-press"), "order": .number(2),
            "setup": .object(["ref": .string("\(kinds.setup)/hotel-gym-chest-press")]),
            "continues": .object(["ref": .string("\(kinds.set)/head")]),
            "origin": .object(["ref": .string("providers.substrate.reamde.dev/hevy/workout/w1")]),
        ]))
        #expect(set.session == "s1" && set.exercise == "chest-press-machine" && set.setup == "hotel-gym-chest-press")
        #expect(set.continues == "head")
        #expect(set.origin == RecordRef(kind: "providers.substrate.reamde.dev/hevy/workout", id: "w1"))
        let routine = try codec.decodeRoutine(FlatRecord(kind: kinds.routine, id: "r", properties: [
            "name": .string("R"),
            "slots": .array([.object(["key": .string("a"), "exercise": .object(["ref": .string("\(kinds.exercise)/plank")])])]),
        ]))
        #expect(routine.slots.first?.exercise == "plank")
    }

    @Test("a decorated reference keeps its link fields while it points at the same record; a changed or cleared one is written anew")
    func decoratedReference() throws {
        let decorated: JSONValue = .object(["ref": .string("\(kinds.exercise)/chest-press-machine"), "since": .string("2026-10-01")])
        let flat = FlatRecord(kind: kinds.set, id: "x", properties: [
            "at": .string("2026-10-27T13:42:10Z"), "session": .string("\(kinds.session)/s1"),
            "exercise": decorated, "slot": .string("a"), "order": .number(1),
            "origin": .object(["ref": .string("x.example.com/import/row/7"), "line": .number(7)]),
        ])
        var set = try codec.decodeSet(flat)
        #expect(codec.encode(set).properties["exercise"] == decorated)
        #expect(codec.encode(set).properties["origin"] == flat.properties["origin"])
        set.exercise = "seated-row-machine"
        set.origin = nil
        let changed = codec.encode(set).properties
        #expect(changed["exercise"] == .string("\(kinds.exercise)/seated-row-machine"))
        #expect(changed["origin"] == nil)
        // A reference object with no path, or to another kind, is refused.
        for bad: JSONValue in [.object(["id": .string("x")]), .object(["ref": .string("\(kinds.routine)/x")]), .array([])] {
            var properties = flat.properties
            properties["exercise"] = bad
            #expect(throws: CodecError.self) { try codec.decodeSet(FlatRecord(kind: kinds.set, id: "x", properties: properties)) }
        }
    }

    @Test("unknown fields at every nesting level, and an unknown descriptive enum, survive decode and encode")
    func unknownNestedFields() throws {
        let routine = FlatRecord(kind: kinds.routine, id: "r", properties: [
            "name": .string("R"),
            "groups": .array([.object(["key": .string("a"), "groupKind": .string("superset"), "cap": .string("PT10M")])]),
            "slots": .array([.object([
                "key": .string("press"), "exercise": .string("\(kinds.exercise)/chest-press-machine"),
                "station": .string("B"),
                "sets": .array([.object(["key": .string("s1"), "roleLabel": .string("finisher"), "band": .string("red"),
                                         "rpeMin": .string("7"), "rpeMax": .string("8")])]),
            ])]),
        ])
        let decodedRoutine = try codec.decodeRoutine(routine)
        #expect(decodedRoutine.groups[0].extra == ["cap": .string("PT10M")])
        #expect(decodedRoutine.slots[0].extra == ["station": .string("B")])
        #expect(decodedRoutine.slots[0].sets[0].roleLabel == nil)
        #expect(decodedRoutine.slots[0].sets[0].extra == ["roleLabel": .string("finisher"), "band": .string("red")])
        #expect(codec.encode(decodedRoutine).properties == routine.properties)

        let session = FlatRecord(kind: kinds.session, id: "s", properties: [
            "at": .string("2026-10-27T13:30:00Z"),
            "slots": .array([.object([
                "key": .string("press"), "exercise": .string("\(kinds.exercise)/chest-press-machine"),
                "routineSlot": .string("press"), "machine": .string("2"),
                "sets": .array([.object(["key": .string("s1"), "work": .string("PT40S")])]),
            ])]),
            "groups": .array([.object(["key": .string("a"), "groupKind": .string("circuit"), "execution": .string("emom")])]),
            "pauses": .array([.object(["start": .string("2026-10-27T13:40:00Z"), "reason": .string("phone")])]),
        ])
        let decodedSession = try codec.decodeSession(session)
        #expect(decodedSession.slots[0].extra == ["machine": .string("2")])
        #expect(decodedSession.slots[0].sets[0].extra == ["work": .string("PT40S")])
        #expect(decodedSession.groups[0].extra == ["execution": .string("emom")])
        #expect(decodedSession.pauses[0].extra == ["reason": .string("phone")])
        #expect(codec.encode(decodedSession).properties == session.properties)

        let set = FlatRecord(kind: kinds.set, id: "x", properties: [
            "at": .string("2026-10-27T13:42:10Z"), "session": .string("\(kinds.session)/s"),
            "exercise": .string("\(kinds.exercise)/chest-press-machine"), "slot": .string("press"), "order": .number(1),
            "target": .object(["plannedSet": .string("s1"), "timerMode": .string("interval"), "cadence": .string("fast")]),
        ])
        let decodedSet = try codec.decodeSet(set)
        #expect(decodedSet.target?.timerMode == nil)
        #expect(decodedSet.target?.extra == ["timerMode": .string("interval"), "cadence": .string("fast")])
        #expect(codec.encode(decodedSet).properties == set.properties)

        let exercise = FlatRecord(kind: kinds.exercise, id: "e", properties: [
            "name": .string("E"), "measurement": .string("weightreps"),
            "codes": .array([.object(["system": .string("fit"), "code": .string("1"), "url": .string("https://x")])]),
        ])
        let decodedExercise = try codec.decodeExercise(exercise)
        #expect(decodedExercise.codes[0].extra == ["url": .string("https://x")])
        #expect(codec.encode(decodedExercise).properties == exercise.properties)
    }

    @Test("an unknown value of an execution enum makes a routine, set or exercise unsupported; a plan snapshot keeps it raw")
    func executionEnums() throws {
        func routine(_ set: JSONObject = [:], slot: JSONObject = [:]) -> FlatRecord {
            var planned: JSONObject = ["key": .string("s1")]
            for (key, value) in set {
                planned[key] = value
            }
            var slotObject: JSONObject = ["key": .string("a"), "exercise": .string("\(kinds.exercise)/e"), "sets": .array([.object(planned)])]
            for (key, value) in slot {
                slotObject[key] = value
            }
            return FlatRecord(kind: kinds.routine, id: "r", properties: ["name": .string("R"), "slots": .array([.object(slotObject)])])
        }
        for (field, value) in [("structure", "emom"), ("role", "cooldown"), ("termination", "amraptime"), ("timerMode", "interval"),
                               ("myoStopRule", "velocity"), ("loadReference", "vmax")]
        {
            #expect(throws: CodecError.unsupported(kind: kinds.routine, id: "r", property: "slots[0].sets[0].\(field)", value: value)) {
                try codec.decodeRoutine(routine([field: .string(value)]))
            }
        }
        #expect(throws: CodecError.unsupported(kind: kinds.routine, id: "r", property: "slots[0].sideOrder", value: "alternate")) {
            try codec.decodeRoutine(routine(slot: ["sideOrder": .string("alternate")]))
        }
        let set: JSONObject = ["at": .string("2026-10-27T13:42:10Z"), "session": .string("\(kinds.session)/s"),
                               "exercise": .string("\(kinds.exercise)/e"), "slot": .string("a"), "order": .number(1),
                               "role": .string("cooldown")]
        #expect(throws: CodecError.unsupported(kind: kinds.set, id: "x", property: "role", value: "cooldown")) {
            try codec.decodeSet(FlatRecord(kind: kinds.set, id: "x", properties: set))
        }
        // An exercise's laterality and myo-rep stop rule decide side chains and the stop.
        for (field, value) in [("laterality", "sequential"), ("myoStopRule", "velocity")] {
            let exercise = FlatRecord(kind: kinds.exercise, id: "e", properties: [
                "name": .string("E"), "measurement": .string("weightreps"), field: .string(value),
            ])
            #expect(throws: CodecError.unsupported(kind: kinds.exercise, id: "e", property: field, value: value)) {
                try codec.decodeExercise(exercise)
            }
        }
        // A session's plan snapshot is never run: an unknown value there is
        // kept raw and the session stays readable (History, PREVIOUS).
        let session = FlatRecord(kind: kinds.session, id: "s", properties: [
            "at": .string("2026-10-27T13:30:00Z"),
            "slots": .array([.object(["key": .string("a"), "exercise": .string("\(kinds.exercise)/e"),
                                      "sets": .array([.object(["key": .string("s1"), "structure": .string("emom"),
                                                               "role": .string("cooldown"), "loadReference": .string("vmax")])])])]),
        ])
        let decoded = try codec.decodeSession(session)
        let snapshot = try #require(decoded.slots.first?.sets.first)
        #expect(snapshot.structure == .straight && snapshot.role == .working && snapshot.loadReference == nil)
        #expect(snapshot.extra == ["structure": .string("emom"), "role": .string("cooldown"), "loadReference": .string("vmax")])
        #expect(codec.encode(decoded).properties == session.properties)
    }

    @Test("the setup kind decodes, round-trips and overrides its exercise's load grid field by field")
    func setups() throws {
        let setups = try ExampleFixtures.records("coverage").filter { $0.kind == kinds.setup }.map(codec.decodeSetup)
        #expect(setups.map(\.id.rawValue).sorted() == ["hotel-gym-chest-press", "hotel-gym-leg-extension", "hotel-gym-seated-row",
                                                       "old-gym-chest-press", "old-gym-seated-row"])
        let hotel = try #require(setups.first { $0.id == "hotel-gym-chest-press" })
        #expect(hotel.exercise == "chest-press-machine")
        let exercise = try #require(try EngineFixtures.exercises()["chest-press-machine"])
        let onSetup = exercise.on(hotel)
        #expect(onSetup.loadStep == (hotel.loadStep ?? exercise.loadStep))
        #expect(onSetup.loadBase == (hotel.loadBase ?? exercise.loadBase))
        #expect(onSetup.loadMax == (hotel.loadMax ?? exercise.loadMax))
        #expect(exercise.on(nil) == exercise)
        // A setup of another exercise changes nothing.
        #expect(try #require(try EngineFixtures.exercises()["leg-press-machine"]).on(hotel).loadStep
            == (try EngineFixtures.exercises()["leg-press-machine"])?.loadStep)
        // A set names its setup by full path and keeps it.
        let set = try #require(try ExampleFixtures.sessions("coverage").flatMap(\.sets).first { $0.setup == "hotel-gym-chest-press" })
        #expect(codec.encode(set).properties["setup"] == .string("\(kinds.setup)/hotel-gym-chest-press"))
    }

    @Test("a journal written before the nested types had unknown fields or the patch an RPE method still decodes")
    func journalCompatibility() throws {
        let planned = try JSONDecoder().decode(PlannedSet.self, from: Data(#"{"key": "s1", "role": "working", "structure": "straight"}"#.utf8))
        #expect(planned.key == "s1" && planned.extra.isEmpty)
        let slot = try JSONDecoder().decode(RoutineSlot.self, from: Data(#"{"key": "a", "exercise": "e", "sets": []}"#.utf8))
        #expect(slot.extra.isEmpty)
        let patch = try JSONDecoder().decode(SetValuesPatch.self, from: JSONEncoder().encode(SetValuesPatch(rpe: .set(8))))
        #expect(patch.rpeMethod == nil)
        // An event written before the method existed: a tick with a bare RPE was a tap.
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(1))]),
                         exercises: EngineCatalog.all)
        let row = try script.row("press", 0)
        let state = try script.tick(row, SetValues(load: .kg(50), reps: 10, rpe: 9))
        #expect(state.rows[row]?.performed?.values.rpeMethod == .entered)
    }
}

// MARK: - Effort provenance

/// One press slot whose planned sets aim at RPE 8, RPE 7 to 8, and 2 to 3
/// reps in reserve, then an added set with no plan.
private func provenanceSession() throws -> EngineScript {
    var script = EngineScript(at: engineEpoch)
    let routine = EngineCatalog.routine([
        EngineCatalog.slot("press", EngineCatalog.press, [
            PlannedSet(key: "s1", repsMin: 8, repsMax: 12, load: .kg(50), rpeMin: 8, rpeMax: 8),
            PlannedSet(key: "s2", repsMin: 8, repsMax: 12, load: .kg(50), rpeMin: 7, rpeMax: 8),
            PlannedSet(key: "s3", repsMin: 8, repsMax: 12, load: .kg(50), rirMin: 2, rirMax: 3),
            PlannedSet(key: "s4", structure: .drop, repsMin: 8, repsMax: 12, load: .kg(50), rpeMin: 9, rpeMax: 9),
        ], setRest: 90),
    ])
    try script.start(routine, exercises: EngineCatalog.all)
    return script
}

@Suite("Data-model review: how each RPE got there (D1)")
struct ReviewEffortTests {
    @Test("a pending row pre-fills the top of its plan's effort range; a drop, an added set and a done row do not")
    func prefill() throws {
        var script = try provenanceSession()
        let state = try script.current
        let rows = state.orderedRows(inSlot: "press")
        #expect(rows.map { state.rpePrefill(ofRow: $0.id) } == [8, 8, 8, 9, nil])
        #expect(state.rpe(ofRow: rows[1].id) == .prefill(8))
        #expect(state.rpe(ofRow: rows[4].id) == .none)
        try script.send(.addSet(.init(slot: "press", row: "added", role: .working)))
        #expect(try script.current.rpePrefill(ofRow: "added") == nil)
    }

    @Test("untouched: a tick that accepts the pre-fill logs the plan's value as prefilled; an untick gives the pre-fill back")
    func untouched() throws {
        var script = try provenanceSession()
        let s1 = try script.row("press", 0)
        let values = try script.current.acceptingRPEPrefill(SetValues(load: .kg(50), reps: 10), row: s1)
        #expect(values.rpe == 8 && values.rpeMethod == .prefilled)
        var state = try script.tick(s1, values)
        #expect(state.rpe(ofRow: s1) == .value(8, .prefilled))
        let record = try #require(SessionEngine.setRecords(state).first)
        #expect(record.rpe == 8 && record.rpeMethod == .prefilled)
        #expect(record.ratedRPE == nil)
        state = try script.send(.uncompleteSet(row: s1))
        #expect(state.rows[s1]?.entered.rpe == nil)
        #expect(state.rpe(ofRow: s1) == .prefill(8))
    }

    @Test("a tap is entered, even on the value already shown; equality never decides")
    func tapEqual() throws {
        var script = try provenanceSession()
        let s1 = try script.row("press", 0)
        var state = try script.send(.editSet(.init(row: s1, values: .enteringRPE(8))))
        #expect(state.rpe(ofRow: s1) == .value(8, .entered))
        #expect(state.rpePrefill(ofRow: s1) == nil)
        // The row's own value wins over the pre-fill; the tick keeps its method.
        let committed = state.acceptingRPEPrefill(try #require(state.rows[s1]).entered.applying(.setting(SetValues(load: .kg(50), reps: 10))),
                                                  row: s1)
        #expect(committed.rpeMethod == .entered)
        state = try script.tick(s1, committed)
        #expect(SessionEngine.setRecords(state).first?.rpeMethod == .entered)
        // A plain patch with no method is a tap too.
        let s2 = try script.row("press", 1)
        state = try script.send(.editSet(.init(row: s2, values: SetValuesPatch(rpe: .set(8)))))
        #expect(state.rows[s2]?.entered.rpeMethod == .entered)
    }

    @Test("PREVIOUS copies a rated RPE as copied; a later tap makes it entered; a pre-filled one is neither shown nor copied")
    func copied() throws {
        var script = try provenanceSession()
        let s1 = try script.row("press", 0)
        let previous = PreviousSet(session: "past", date: engineEpoch, record: "p1",
                                   values: SetValues(load: .kg(45), reps: 12, rpe: 9, rpeMethod: .entered), text: "45 kg × 12 @ 9",
                                   deload: false)
        var state = try script.send(.editSet(.init(row: s1, values: previous.copy)))
        #expect(state.rpe(ofRow: s1) == .value(9, .copied))
        state = try script.tick(s1, try #require(state.rows[s1]).entered)
        let record = try #require(SessionEngine.setRecords(state).first)
        #expect(record.rpe == 9 && record.rpeMethod == .copied && record.ratedRPE == nil)
        let s2 = try script.row("press", 1)
        state = try script.send(.editSet(.init(row: s2, values: previous.copy)))
        state = try script.send(.editSet(.init(row: s2, values: .enteringRPE(9))))
        #expect(state.rpe(ofRow: s2) == .value(9, .entered))

        // The 26 October leg press set 1 was logged with the plan's RPE pre-filled.
        let routine = try #require(try EngineFixtures.routines()["day-1-strength-foundation"])
        var next = EngineScript(at: DateText.parse("2026-10-29T12:00:00Z")!)
        try next.start(routine, exercises: Array(try EngineFixtures.exercises().values))
        let history = try Progression.history(before: next.now)
        let set1 = try #require(Previous.find(for: try next.row("leg-press", 2), in: next.current, history: history))
        #expect(set1.values.rpe == nil)
        #expect(set1.text == "105 kg × 9")
        #expect(set1.copy.rpe == .unchanged)
        let set2 = try #require(Previous.find(for: try next.row("leg-press", 3), in: next.current, history: history))
        #expect(set2.text == "105 kg × 9 @ 8.5")
    }

    @Test("clearing removes the value and its method; on a pending row it also stops the pre-fill")
    func clear() throws {
        var script = try provenanceSession()
        let (s1, s2) = try (script.row("press", 0), script.row("press", 1))
        try script.tick(s1, SetValues(load: .kg(50), reps: 10, rpe: 9, rpeMethod: .entered))
        var state = try script.send(.editSet(.init(row: s1, values: .clearingRPE)))
        #expect(state.rows[s1]?.performed?.values.rpe == nil)
        #expect(state.rows[s1]?.performed?.values.rpeMethod == nil)
        let record = try #require(SessionEngine.setRecords(state).first)
        #expect(record.rpe == nil && record.rpeMethod == nil && record.revision == 2)
        // An untick of a cleared set does not bring the plan's value back.
        state = try script.send(.uncompleteSet(row: s1))
        #expect(state.rpe(ofRow: s1) == .none)
        state = try script.send(.editSet(.init(row: s2, values: .clearingRPE)))
        #expect(state.rpe(ofRow: s2) == .none)
        #expect(state.acceptingRPEPrefill(SetValues(load: .kg(50), reps: 10), row: s2).rpe == nil)
        // A tap brings a value back, entered.
        state = try script.send(.editSet(.init(row: s2, values: .enteringRPE(7))))
        #expect(state.rpe(ofRow: s2) == .value(7, .entered))
    }

    @Test("a method the event cannot claim is refused")
    func refusals() throws {
        var script = try provenanceSession()
        let s1 = try script.row("press", 0)
        script.refuse(.completeSet(.init(row: s1, recordID: "r", values: SetValues(load: .kg(50), reps: 10, rpe: 9, rpeMethod: .prefilled))),
                      .invalidEffort)
        script.refuse(.completeSet(.init(row: s1, recordID: "r", values: SetValues(load: .kg(50), reps: 10, rpe: 8, rpeMethod: .imported))),
                      .invalidEffort)
        script.refuse(.editSet(.init(row: s1, values: SetValuesPatch(rpe: .set(8), rpeMethod: .prefilled))), .invalidEffort)
        try script.send(.addSet(.init(slot: "press", row: "added", role: .working)))
        let added: RowID = "added"
        script.refuse(.completeSet(.init(row: added, recordID: "r", values: SetValues(load: .kg(50), reps: 10, rpe: 8, rpeMethod: .prefilled))),
                      .invalidEffort)
        // A method with no value is dropped.
        let state = try script.tick(s1, SetValues(load: .kg(50), reps: 10, rpeMethod: .entered))
        #expect(state.rows[s1]?.performed?.values.rpeMethod == nil)
    }

    @Test("the second side of a one-side set pre-fills its head's plan")
    func secondSide() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([EngineCatalog.slot("lunge", EngineCatalog.lunge, EngineCatalog.sets(1, rpe: 8))]),
                         exercises: EngineCatalog.all)
        let (left, right) = try (script.row("lunge", 0), script.row("lunge", 1))
        #expect(try script.current.rpePrefill(ofRow: right) == 8)
        try script.tick(left, try script.current.acceptingRPEPrefill(kgReps(10, 10), row: left))
        let state = try script.tick(right, try script.current.acceptingRPEPrefill(kgReps(10, 9), row: right))
        #expect(SessionEngine.setRecords(state).map(\.rpeMethod) == [.prefilled, .prefilled])
    }

    @Test("the presentation shows the pre-fill apart from a value, and commits it only on a screen that pre-fills")
    func presentation() throws {
        var script = try provenanceSession()
        let s1 = try script.row("press", 0)
        let plain = SessionPresenter.present(try script.current, history: HistoryIndex([]), settings: Progression.settings, now: engineEpoch)
        let row = try #require(plain.row(s1))
        #expect(row.rpeEntry == .prefill(8))
        #expect(row.rpePrefillText == "8")
        #expect(row.rpe == nil && row.rpeText == nil)
        #expect(row.commitValues?.rpe == nil)
        var settings = Progression.settings
        settings.prefillsRPE = true
        var rows = SessionPresenter.present(try script.current, history: HistoryIndex([]), settings: settings, now: engineEpoch)
        #expect(rows.row(s1)?.commitValues?.rpe == 8)
        #expect(rows.row(s1)?.commitValues?.rpeMethod == .prefilled)
        #expect(rows.unticked.first { $0.row == s1 }?.values?.rpeMethod == .prefilled)
        // What the presentation commits, the engine takes.
        try script.tick(s1, try #require(rows.row(s1)?.commitValues))
        rows = SessionPresenter.present(try script.current, history: HistoryIndex([]), settings: settings, now: engineEpoch)
        #expect(rows.row(s1)?.rpeEntry == .value(8, .prefilled))
        #expect(rows.row(s1)?.rpePrefillText == nil)
    }
}

// MARK: - Records

@Suite("Data-model review: what each record says (D2 to D13)")
struct ReviewRecordTests {
    @Test("Finish's \"mark them done as shown\" writes bulk at the session's time, with the pre-fill as prefilled; a tick writes individual")
    func bulk() throws {
        var script = try provenanceSession()
        let s1 = try script.row("press", 0)
        try script.tick(s1, kgReps(50, 10, rpe: 8), at: engineEpoch.addingTimeInterval(60))
        var settings = Progression.settings
        settings.prefillsRPE = true
        let end = engineEpoch.addingTimeInterval(900)
        let presentation = SessionPresenter.present(try script.current, history: HistoryIndex([]), settings: settings, now: end)
        let projection = FinishPresenter.projection(try script.current, presentation: presentation, policy: .markDone, end: end) {
            SetID("bulk-\($0.rawValue)")
        }
        let state = try script.send(.finish(projection.event(start: engineEpoch, end: end, utcOffset: 0)), at: end)
        let sets = SessionEngine.setRecords(state)
        let ticked = try #require(sets.first { $0.completionMethod == .individual })
        #expect(ticked.timeBasis == .completion && ticked.rpeMethod == .entered && ticked.restMethod == nil)
        // s2, s3 and the drop set's head; its drop goes to failure, so has no reps to commit.
        let marked = sets.filter { $0.completionMethod == .bulk }
        #expect(marked.count == 3)
        #expect(marked.allSatisfy { $0.timeBasis == .session && $0.at == end && $0.restBefore == nil && $0.restMethod == nil })
        #expect(marked.first { $0.target?.plannedSet == "s2" }?.rpeMethod == .prefilled)
        #expect(marked.first { $0.target?.plannedSet == "s2" }?.rpe == 8)
    }

    @Test("every set the app writes says its scale, time basis and rest method; rest method goes with rest")
    func provenanceFields() throws {
        let replay = try ExampleReplay.run(EngineFixtures.session("sessions", EngineFixtures.fullyBody))
        let records = try replay.records()
        #expect(records.sets.allSatisfy { $0.rpeScale == .rirbased && $0.timeBasis == .completion && $0.completionMethod == .individual })
        #expect(records.sets.allSatisfy { ($0.restBefore == nil) == ($0.restMethod == nil) })
        #expect(records.sets.allSatisfy { ($0.rpe == nil) == ($0.rpeMethod == nil) })
        #expect(records.sets.contains { $0.restMethod == .interaction })
        #expect(records.session.sport == .strength)
        #expect(records.session.timezone == "Europe/London")
        let encoded = RecordCodec().encode(records.session).properties
        #expect(encoded["timezone"] == .string("Europe/London"))
        #expect(encoded["timeZone"] == nil)
    }

    @Test("each set copies what its numbers meant, and a later edit of the exercise reinterprets nothing")
    func loadMeaning() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("raise", EngineCatalog.raise, EngineCatalog.sets(1, load: 10)),
            EngineCatalog.slot("lunge", EngineCatalog.lunge, EngineCatalog.sets(1, load: 12)),
            EngineCatalog.slot("push-up", EngineCatalog.pushUp, EngineCatalog.sets(1, load: nil)),
            EngineCatalog.slot("plank", EngineCatalog.plank, [PlannedSet(key: "s1", durationMin: 30)]),
        ]), exercises: EngineCatalog.all, bodyMass: .kg(80))
        try script.tick(try script.row("raise", 0), kgReps(10, 10))
        try script.tick(try script.row("lunge", 0), kgReps(12, 8))
        try script.tick(try script.row("lunge", 1), kgReps(12, 8))
        try script.tick(try script.row("push-up", 0), SetValues(reps: 15))
        try script.send(.startTimedSet(row: try script.row("plank", 0), target: 30))
        let state = try script.send(.stopTimedSet(.init(row: try script.row("plank", 0), recordID: "plank-1", values: SetValues())),
                                    at: script.now.addingTimeInterval(32))
        let sets = SessionEngine.setRecords(state)
        let raise = try #require(sets.first { $0.slot == "raise" })
        #expect(raise.loadBasis == .perimplement && raise.implements == 2 && raise.laterality == .bilateral && raise.repsBasis == .total)
        #expect(raise.bodyShare == nil)
        #expect(sets.filter { $0.slot == "lunge" }.allSatisfy { $0.laterality == .unilateral && $0.repsBasis == .perside })
        let push = try #require(sets.first { $0.slot == "push-up" })
        #expect(push.loadBasis == .total && push.implements == 1 && push.bodyShare == 1)
        let plank = try #require(sets.first { $0.slot == "plank" })
        #expect(plank.loadBasis == nil && plank.implements == nil && plank.repsBasis == nil && plank.laterality == .bilateral)

        // 10 kg × 10 with two dumbbells is 200 kg, whatever the exercise says later.
        var edited = EngineCatalog.raise
        edited.loadBasis = .total
        edited.implements = 1
        let volume = Counting.volume([raise], exercises: [edited.id: edited], bodyMass: nil, unit: .kg, includeWarmups: false)
        #expect(volume == .kg(200))
        var legacy = raise
        legacy.loadBasis = nil
        legacy.implements = nil
        #expect(Counting.volume([legacy], exercises: [edited.id: edited], bodyMass: nil, unit: .kg, includeWarmups: false) == .kg(100))
        // A body share stored with the set holds too.
        var push65 = push
        push65.bodyShare = d("0.65")
        var pushUp = EngineCatalog.pushUp
        pushUp.bodyShare = d("0.7")
        #expect(Counting.effectiveLoad(push65, exercise: pushUp, bodyMass: .kg(80), unit: .kg) == 52)
        // The state's snapshot is the exercise as it was at Start.
        #expect(state.exercises["raise"] == EngineCatalog.raise)
    }

    @Test("the target is the whole prescription, with what the engine ran where the plan was silent")
    func fullTarget() throws {
        var myoExercise = EngineCatalog.curl
        myoExercise.myoStopRule = .belowtarget
        let hold = EngineCatalog.exercise("side-hold", .duration, laterality: .unilateral, restClass: .timed, equipment: Equipment.none)
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, [
                PlannedSet(key: "s1", repsMin: 8, repsMax: 10, load: .kg(50), rpeMin: 7, rpeMax: 8, tempo: "3-1-1-0"),
                PlannedSet(key: "s2", structure: .drop, repsMin: 8, repsMax: 10, load: .kg(50), drops: 1, dropReps: 6),
            ]),
            EngineCatalog.slot("curl", myoExercise, [PlannedSet(key: "s1", structure: .myo, repsMin: 12, repsMax: 15, load: .kg(10))]),
            EngineCatalog.slot("plank", EngineCatalog.plank, [PlannedSet(key: "s1", durationMin: 30, durationMax: 45)]),
            EngineCatalog.slot("hold", hold, [PlannedSet(key: "s1", durationMin: 20, timerMode: .stopwatch)]),
        ]), exercises: EngineCatalog.all + [myoExercise, hold])
        let (s1, s2, drop) = try (script.row("press", 0), script.row("press", 1), script.row("press", 2))
        try script.tick(s1, kgReps(50, 9))
        try script.tick(s2, kgReps(50, 9))
        try script.tick(drop, kgReps(40, 6))
        try script.tick(try script.row("curl", 0), kgReps(10, 14))
        for row in try [script.row("plank", 0), script.row("hold", 0), script.row("hold", 1)] {
            try script.send(.startTimedSet(row: row, target: nil))
            try script.send(.stopTimedSet(.init(row: row, recordID: SetID("t-\(row.rawValue)"), values: SetValues())),
                            at: script.now.addingTimeInterval(25))
        }
        let sets = SessionEngine.setRecords(try script.current)
        let straight = try #require(sets.first { $0.slot == "press" && $0.target?.plannedSet == "s1" }?.target)
        #expect(straight == SetTarget(plannedSet: "s1", termination: .reprange, repsMin: 8, repsMax: 10, load: .kg(50), rpeMin: 7, rpeMax: 8,
                                      tempo: "3-1-1-0"))
        let dropHead = try #require(sets.first { $0.slot == "press" && $0.structure == .drop && $0.continues == nil }?.target)
        #expect(dropHead.dropReps == 6 && dropHead.drops == 1 && dropHead.intraSetPause == 0)
        let dropLink = try #require(sets.first { $0.slot == "press" && $0.continues != nil }?.target)
        #expect(dropLink == SetTarget(plannedSet: "s2", termination: nil, repsMin: 6, repsMax: 6))
        let myo = try #require(sets.first { $0.slot == "curl" && $0.continues == nil }?.target)
        #expect(myo.myoStopRule == .belowtarget && myo.intraSetPause == 15)
        #expect(sets.first { $0.slot == "plank" }?.target?.timerMode == .countdown)
        let holdSides = sets.filter { $0.slot == "hold" }
        #expect(holdSides.map { $0.target?.timerMode } == [.stopwatch, .stopwatch])
    }

    @Test("a percentage of body mass resolves into the target's and the snapshot's load; another reference stays a percentage")
    func percentLoads() throws {
        var script = EngineScript(at: engineEpoch)
        try script.start(EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, [
                PlannedSet(key: "s1", repsMin: 8, repsMax: 8, loadPercent: 50, loadReference: .bodymass),
                PlannedSet(key: "s2", repsMin: 5, repsMax: 5, loadPercent: 80, loadReference: .trainingmax),
                PlannedSet(key: "s3", repsMin: 5, repsMax: 5, load: .kg(60), loadPercent: 80, loadReference: .bodymass),
            ]),
        ]), exercises: EngineCatalog.all, bodyMass: .kg(d("82.4")))
        let rows = try script.current.orderedRows(inSlot: "press")
        #expect(rows[0].target?.load == .kg(d("41.2")) && rows[0].target?.referenceLoad == .kg(d("82.4")))
        #expect(rows[0].target?.loadPercent == 50 && rows[0].target?.loadReference == .bodymass)
        #expect(rows[1].target?.load == nil && rows[1].target?.referenceLoad == nil && rows[1].target?.loadPercent == 80)
        #expect(rows[2].target?.load == .kg(60) && rows[2].target?.referenceLoad == nil)
        try script.tick(rows[0].id, kgReps(d("41.2"), 8))
        let session = SessionEngine.sessionRecord(try script.current, adapter: "substrate-workouts@test")
        let snapshot = try #require(session.slots.first?.sets)
        #expect(snapshot.map(\.load) == [.kg(d("41.2")), nil, .kg(60)])
        #expect(snapshot.map(\.loadPercent) == [50, 80, 80])
    }

    @Test("each routine slot's planned sets go on the session once, done or not, and an added slot has none")
    func planSnapshot() throws {
        var script = EngineScript(at: engineEpoch)
        let routine = EngineCatalog.routine([
            EngineCatalog.slot("press", EngineCatalog.press, EngineCatalog.sets(3)),
            EngineCatalog.slot("row", EngineCatalog.row, EngineCatalog.sets(2)),
        ])
        try script.start(routine, exercises: EngineCatalog.all)
        try script.tick(try script.row("press", 0), kgReps(50, 10))
        // The press is swapped for the curl after its first set: two slots, one routine slot.
        try script.send(.replaceExercise(.init(slot: "press", newSlot: "curl", exercise: EngineCatalog.curl)))
        try script.tick(try script.row("curl", 0), kgReps(20, 10))
        try script.send(.addExercise(.init(slot: "raise", exercise: EngineCatalog.raise, rowIDs: script.rowIDs(1))))
        try script.tick(try script.row("raise", 0), kgReps(8, 12))
        let session = SessionEngine.sessionRecord(try script.current, adapter: "substrate-workouts@test")
        #expect(session.slots.map(\.key) == ["press", "curl", "raise"])
        // Three planned, one done on the press and one on the curl: all three are kept.
        #expect(session.slots[0].sets == routine.slots[0].sets)
        #expect(session.slots[0].sets.map(\.key) == ["s1", "s2", "s3"])
        #expect(session.slots[1].sets.isEmpty)
        #expect(session.slots[2].sets.isEmpty)
        // The row slot, nothing done, goes at Finish with its plan (open question 32).
        #expect(!session.slots.contains { $0.routineSlot == "row" })
        let encoded = RecordCodec().encode(session)
        #expect(try RecordCodec().decodeSession(encoded).slots[0].sets == routine.slots[0].sets)
    }

    @Test("every kept slot of the 27 October session carries its routine slot's planned sets as the routine has them")
    func exampleSnapshots() throws {
        let replay = try ExampleReplay.run(EngineFixtures.session("sessions", EngineFixtures.fullyBody))
        let records = try replay.records()
        let routine = try #require(try EngineFixtures.routines()["fully-body-collection-1"])
        #expect(records.session.slots.count == 9)
        for slot in records.session.slots {
            let planned = try #require(routine.slots.first { $0.key == slot.routineSlot })
            #expect(slot.sets == planned.sets, "\(slot.key)")
        }
    }
}

// MARK: - Suggestions

@Suite("Data-model review: the suggestion reads ratings only, and the top of a range (D1, D3)")
struct ReviewSuggestionTests {
    let config = ExerciseConfig(unit: .kg, base: 0, step: 5)
    let target = Target03(repsMin: 6, repsMax: 10, rpe: 8)
    let now = engineEpoch

    private func done(_ values: SetValues) -> DoneSet {
        SuggestionInputs.doneSet(kind: .normal, values: values, side: nil, config: config, target: target, completedAt: now, ordinal: 1)
    }

    private func next(after set: DoneSet) throws -> Suggestion {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        return try #require(suggest(pending: PendingSet(kind: .normal, ordinal: 2, target: target), exercise: config, today: [set],
                                    history: [], bodyMass: nil, now: now, calendar: calendar))
    }

    @Test("a pre-filled or copied RPE gives what no RPE gives, and says \"assuming\"; a rating says \"at\"")
    func prefilledIsNoRating() throws {
        let none = try next(after: done(SetValues(load: .kg(105), reps: 9)))
        let prefilled = done(SetValues(load: .kg(105), reps: 9, rpe: 8, rpeMethod: .prefilled))
        #expect(prefilled.rpe == nil && prefilled.unratedRPE == 8)
        let fromPrefill = try next(after: prefilled)
        #expect(fromPrefill.load == none.load && fromPrefill.reps == none.reps && fromPrefill.reason == none.reason)
        #expect(none.text == "Set 1: 105 kg × 9; try 105 kg × 9")
        #expect(fromPrefill.text == "Set 1: 105 kg × 9, assuming RPE 8; try 105 kg × 9")
        let fromCopy = try next(after: done(SetValues(load: .kg(105), reps: 9, rpe: 8, rpeMethod: .copied)))
        #expect(fromCopy.text == fromPrefill.text)
        // A pre-filled 6 is not a set that felt easy: the numbers do not move.
        let easyPrefill = try next(after: done(SetValues(load: .kg(105), reps: 9, rpe: 6, rpeMethod: .prefilled)))
        #expect(easyPrefill.load == none.load && easyPrefill.reps == none.reps)
        let rated = try next(after: done(SetValues(load: .kg(105), reps: 9, rpe: 6, rpeMethod: .entered)))
        #expect(rated.text.hasPrefix("Set 1: 105 kg × 9 at RPE 6"))
        #expect(rated.load != none.load || rated.reps != none.reps)
    }

    @Test("history reads only ratings: rpe entered or imported, rir on an RIR-based scale")
    func historyRatings() {
        let base = PerformedSet(id: "x", at: now, session: "s", exercise: "e", slot: "a", order: 1)
        func with(_ change: (inout PerformedSet) -> Void) -> PerformedSet {
            var set = base
            change(&set)
            return set
        }
        #expect(with { $0.rpe = 8 }.ratedRPE == 8)
        #expect(with { $0.rpe = 8; $0.rpeMethod = .imported }.ratedRPE == 8)
        #expect(with { $0.rpe = 8; $0.rpeMethod = .prefilled }.ratedRPE == nil)
        #expect(with { $0.rpe = 8; $0.rpeMethod = .copied }.ratedRPE == nil)
        #expect(with { $0.rir = 2; $0.rirMethod = .imported; $0.rpeScale = .hevy }.ratedRPE == 8)
        #expect(with { $0.rir = 2; $0.rpeScale = .cr10 }.ratedRPE == nil)
        #expect(with { $0.rpe = 8; $0.rpeMethod = .prefilled }.loggedRPE == 8)
        let sets = [with { $0.load = .kg(100); $0.reps = 10; $0.loadMode = .external; $0.rpe = 8; $0.rpeMethod = .prefilled }]
        let doneSets = SuggestionInputs.doneSets(sets, exercise: EngineCatalog.press, config: config)
        #expect(doneSets.first?.rpe == nil && doneSets.first?.unratedRPE == 8)
    }

    @Test("a side pair folds to its highest rating; an unrated side does not count")
    func chainFold() {
        let left = SuggestionInputs.doneSet(kind: .normal, values: SetValues(load: .kg(10), reps: 10, rpe: 9, rpeMethod: .prefilled),
                                            side: .left, config: config, target: target, completedAt: now, ordinal: 1)
        let right = SuggestionInputs.doneSet(kind: .normal, values: SetValues(load: .kg(10), reps: 9, rpe: 8, rpeMethod: .entered),
                                             side: .right, config: config, target: target, completedAt: now, ordinal: 1)
        #expect(SuggestionInputs.merged(left, with: right).rpe == 8)
        let unrated = SuggestionInputs.merged(left, with: left)
        #expect(unrated.rpe == nil && unrated.unratedRPE == 9)
    }

    @Test("a range's top is the load selector: RPE 7 to 8 reads 8, 2 to 3 in reserve reads 8")
    func ranges() throws {
        let config = config
        #expect(SuggestionInputs.target(SetTarget(rpeMin: 7, rpeMax: 8), termination: nil, config: config).rpe == 8)
        #expect(SuggestionInputs.target(SetTarget(rirMin: 2, rirMax: 3), termination: nil, config: config).rpe == 8)
        #expect(SuggestionInputs.target(SetTarget(rpeMin: 9), termination: nil, config: config).rpe == 9)
        #expect(SetTarget(rirMin: 12).effortTop == nil)
        // Day 3's overhead press, planned at RPE 7 to 8.
        let day3 = try #require(try EngineFixtures.routines()["day-3"])
        let press = try #require(day3.slots.first { $0.key == "overhead-press" }?.sets.first)
        #expect(press.rpeMin == 7 && press.rpeMax == 8 && press.effortTop == 8)
        #expect(SessionEngine.target(from: press).effortTop == 8)
        #expect(RoutinePresenter.runText(press, count: 3, measurement: .weightreps) == "3 sets of 8–12 reps at RPE 7–8, from 12 kg")
        #expect(Format.effort(rpeMin: 8, rpeMax: 8, rirMin: nil, rirMax: nil) == "RPE 8")
        #expect(Format.effort(rpeMin: nil, rpeMax: nil, rirMin: 2, rirMax: 3) == "2–3 reps in reserve")
        #expect(Format.effort(rpeMin: nil, rpeMax: nil, rirMin: 1, rirMax: 1) == "1 rep in reserve")
        #expect(RoutinePresenter.runText(PlannedSet(key: "s1", repsMin: 5, repsMax: 5, loadPercent: 80, loadReference: .trainingmax),
                                         count: 3, measurement: .weightreps) == "3 sets of 5 reps, from 80% of training max")
    }
}

// MARK: - Apple Health and old history files

@Suite("Data-model review: Apple Health's effort score and history written before the review (A3)")
struct ReviewHealthAndHistoryTests {
    @Test("a session RPE of 0 is never sent as an effort score; 1 to 10 are")
    func effortScore() {
        var session = TrainingSession(id: "s", at: engineEpoch)
        #expect(session.healthEffortScore == nil)
        session.sessionRPE = 0
        #expect(session.healthEffortScore == nil)
        session.sessionRPE = d("0.5")
        #expect(session.healthEffortScore == nil)
        session.sessionRPE = 1
        #expect(session.healthEffortScore == 1)
        session.sessionRPE = 7
        #expect(session.healthEffortScore == 7)
        session.sessionRPE = 10
        #expect(session.healthEffortScore == 10)
    }

    @Test("a format 1 history file loads: timezone, sport, target ranges and tapped RPEs; it is written back as format 2")
    func formatOne() throws {
        let directory = try TemporaryDirectory()
        defer { directory.remove() }
        let store = HistoryStore(directory: directory.url, writingOptions: .atomic)
        let session = SessionID("01a17d8d-afc0-7a2b-bc4d-5e6f70819203")
        let old: JSONValue = .object([
            "format": .number(1),
            "records": .array([
                FlatRecord(kind: kinds.session, id: session.rawValue, properties: [
                    "at": .string("2026-10-27T13:30:00Z"), "status": .string("finished"), "timeZone": .string("Europe/London"),
                    "adapter": .string("substrate-workouts@0.1.0"),
                ]).json,
                FlatRecord(kind: kinds.set, id: "set-1", properties: [
                    "at": .string("2026-10-27T13:42:10Z"), "session": .string("\(kinds.session)/\(session.rawValue)"),
                    "exercise": .string("\(kinds.exercise)/chest-press-machine"), "slot": .string("chest-press"), "order": .number(1),
                    "loadMode": .string("external"), "load": .object(["value": .string("35"), "unit": .string("kg")]), "reps": .number(13),
                    "rpe": .string("9.5"),
                    "target": .object(["plannedSet": .string("s1"), "rpe": .string("9.5"), "rir": .string("1")]),
                ]).json,
            ]),
        ])
        let history = directory.url.appendingPathComponent("history")
        try FileManager.default.createDirectory(at: history, withIntermediateDirectories: true)
        try old.data().write(to: store.url(for: session))
        let load = store.loadAll()
        #expect(load.unreadable.isEmpty)
        let entry = try #require(load.entries.first)
        #expect(entry.records.session.timezone == "Europe/London")
        #expect(entry.records.session.sport == .strength)
        #expect(entry.records.session.extra.isEmpty)
        let set = try #require(entry.records.sets.first)
        #expect(set.rpeMethod == .entered && set.ratedRPE == d("9.5"))
        #expect(set.target == SetTarget(plannedSet: "s1", rpeMin: d("9.5"), rpeMax: d("9.5"), rirMin: 1, rirMax: 1))
        #expect(set.extra.isEmpty && set.target?.extra.isEmpty == true)
        try store.write(entry)
        let written = try #require(try JSONValue(data: Data(contentsOf: store.url(for: session))).objectValue)
        #expect(written["format"] == .number(2))
        #expect(try store.read(session).records == entry.records)
        #expect(RecordCodec().encode(entry.records.session).properties["timeZone"] == nil)
    }
}
