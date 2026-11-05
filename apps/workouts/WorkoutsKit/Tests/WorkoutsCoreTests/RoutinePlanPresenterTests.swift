import Foundation
import Testing

@testable import WorkoutsCore

// The routine page's plan, drawn as a started session's Rows layout, over
// the owner's example routines and sessions, and its in-place set editor.

private func d(_ text: String) -> Decimal { Decimal(string: text, locale: Locale(identifier: "en_US_POSIX"))! }

private enum Plan {
    static let dayOne = RoutineID("day-1-strength-foundation")
    static let dayTwo = RoutineID("day-2-volume-stability")

    static func history() throws -> HistoryIndex { HistoryIndex(try ExampleFixtures.sessions("sessions")) }

    static func routine(_ id: RoutineID) throws -> (Routine, Catalog) {
        let catalog = try ExampleFixtures.catalog()
        return (try #require(catalog.routines[id]), catalog)
    }

    static func present(_ routine: Routine, _ catalog: Catalog) throws -> RoutinePlanPresentation {
        RoutinePlanPresenter.present(routine, exercises: catalog.exercises, history: try history())
    }

    static func editor(_ routine: Routine, _ catalog: Catalog, _ slot: String, _ set: String) throws -> RoutinePlanSetEditor {
        try #require(
            RoutinePlanPresenter.editor(
                RoutinePlanSetRef(slot: slot, set: set), in: routine, exercises: catalog.exercises,
                history: try history()))
    }
}

@Suite("Routine plan presenter", .timeLimit(.minutes(10)))
struct RoutinePlanPresenterTests {
    @Test("A plain exercise reads as a session's card: warm-ups, then each set's targets, last time beside it")
    func plainBlock() throws {
        let (routine, catalog) = try Plan.routine(Plan.dayOne)
        let plan = try Plan.present(routine, catalog)
        // The editor's blocks, so a card and its edits name the same thing.
        #expect(plan.blocks.map(\.id) == RoutineEditor(routine).blocks.map(\.id))
        let leg = try #require(plan.blocks.first)
        #expect(leg.title == "Leg Press (Machine)")
        #expect(leg.subtitle == "6\u{2013}10 reps with 2 left, rest 2:30")
        #expect(leg.notes?.hasPrefix("Feet high") == true)
        #expect(leg.members.map(\.exercise) == ["leg-press-machine"])
        let lines = leg.lines
        #expect(lines.map(\.setLabel) == ["W", "W", "1", "2", "3"])
        #expect(lines.map(\.title) == ["Warm-up 1", "Warm-up 2", "Set 1", "Set 2", "Set 3"])
        #expect(
            lines.map(\.values) == [
                "40 kg \u{00D7} 12", "70 kg \u{00D7} 6", "100 kg \u{00D7} 6\u{2013}10",
                "100 kg \u{00D7} 6\u{2013}10", "100 kg \u{00D7} 6\u{2013}10",
            ])
        #expect(lines.map(\.role) == [.warmup, .warmup, .working, .working, .working])
        #expect(lines.map(\.rpe) == [nil, nil, "2 left", "2 left", "2 left"])
        // The warm-up's own rest is its tag; the others take the slot's.
        #expect(lines[1].tags == [RowsTag(text: "rest 1:30", style: .grey)])
        #expect(lines[2].tags.isEmpty)
        // Pending lines, read only: nothing to tap, no editor, nothing done.
        #expect(lines.allSatisfy { $0.state == .next && $0.tap == .none && $0.editor == nil && $0.fix == nil && $0.changes.isEmpty })
        // Last time, as PREVIOUS reads it: Monday's leg press, the pre-filled RPE no rating.
        #expect(
            lines.map(\.last) == [
                "last 40 \u{00D7} 12", "last 70 \u{00D7} 6", "last 105 \u{00D7} 9",
                "last 105 \u{00D7} 9 · 1–2 left", "last 105 \u{00D7} 8 · 1 left",
            ])
        #expect(lines[2].sentence == "Set 1: 100 kilograms, 6 to 10 reps, 2 reps left. Last time 105 kilograms, 9 reps.")
        // Each line names its planned set.
        #expect(plan.ref(lines[3].id) == RoutinePlanSetRef(slot: "leg-press", set: "s2"))
        #expect(Set(plan.refs.values).count == routine.slots.reduce(0) { $0 + $1.sets.count })
    }

    @Test("A group reads by round, each set after its exercise's name, the round rest in its subtitle")
    func groupBlock() throws {
        let (routine, catalog) = try Plan.routine(Plan.dayTwo)
        let plan = try Plan.present(routine, catalog)
        let group = try #require(plan.blocks.first { $0.group == "a" })
        #expect(group.title == "Superset")
        #expect(group.members.map(\.slot) == ["incline-bench-press", "vertical-traction"])
        #expect(group.subtitle == "Incline Bench Press (Dumbbell) and Vertical Traction (Machine), rest 2:30 after each round")
        #expect(group.spokenTitle == "Superset, Incline Bench Press (Dumbbell) and Vertical Traction (Machine)")
        let slots = routine.slots.filter { $0.group == "a" }
        let rounds = slots.map { $0.sets.count { $0.role == .working } }.max() ?? 0
        let headers = group.items.compactMap { item -> RowsRoundHeader? in if case let .round(header) = item { header } else { nil } }
        #expect(headers.map(\.title) == (1 ... rounds).map { "Round \($0) of \(rounds)" })
        // Round 1: each member's first working set, in member order.
        let firstRound = group.items.drop { if case .round = $0 { false } else { true } }.dropFirst()
            .prefix { if case .set = $0 { true } else { false } }
            .compactMap { item -> RowsSetItem? in if case let .set(set) = item { set } else { nil } }
        #expect(firstRound.map(\.caption) == ["Incline Bench Press (Dumbbell)", "Vertical Traction (Machine)"])
        #expect(firstRound.map(\.title) == ["Incline Bench Press (Dumbbell), set 1", "Vertical Traction (Machine), set 1"])
        #expect(firstRound.map(\.setLabel) == ["1", "1"])
        // A plain slot's line names no exercise: its card does.
        #expect(plan.blocks.first { $0.group == nil }?.lines.allSatisfy { $0.caption == nil } == true)
    }

    @Test("Structures read as a session draws them: a drop as its tag, myo-reps under a chain header")
    func structures() throws {
        let (_, catalog) = try Plan.routine(Plan.dayOne)
        var routine = Routine(id: "test", name: "Test")
        routine.slots = [
            RoutineSlot(
                key: "raise", exercise: "lateral-raise-dumbbell",
                sets: [
                    PlannedSet(key: "s1", roleLabel: .top, repsMin: 8, repsMax: 12, rpeMin: 9, rpeMax: 9),
                    PlannedSet(key: "s2", structure: .drop, repsMin: 10, repsMax: 15, drops: 2),
                    PlannedSet(key: "s3", structure: .myo, termination: .failure, repsMin: 12, repsMax: 15, intraSetPause: 15),
                    PlannedSet(key: "s4", termination: .amrap, repsMin: 10, rirMin: 1, rirMax: 2),
                    PlannedSet(key: "s5", termination: .rirstop, rirMin: 2, rirMax: 3),
                    PlannedSet(key: "s6", termination: .amrap),
                ])
        ]
        // No history: the lines say only what the plan does.
        let block = try #require(RoutinePlanPresenter.present(routine, exercises: catalog.exercises, history: HistoryIndex()).blocks.first)
        let lines = block.lines
        #expect(
            lines.map(\.tags) == [
                [RowsTag(text: "top set", style: .grey)],
                [RowsTag(text: "+ 2 drops", style: .orange)],
                [RowsTag(text: "to failure", style: .red)],
                [RowsTag(text: "max reps", style: .grey)],
                [],
                // The values say it: no tag repeats it.
                [],
            ])
        #expect(
            lines.map(\.values) == [
                "8\u{2013}12 reps", "10\u{2013}15 reps", "12\u{2013}15 reps", "10+ reps",
                "to 2\u{2013}3 left", "max reps",
            ])
        // An effort the values carry stands nowhere else.
        #expect(lines.map(\.rpe) == ["1 left", nil, nil, "1\u{2013}2 left", nil, nil])
        let headers = block.items.compactMap { item -> String? in if case let .chainHeader(header) = item { header.text } else { nil } }
        #expect(headers == ["Set 3 as myo-reps, 0:15 between mini-sets"])
        #expect(lines.allSatisfy { $0.last == nil })
        #expect(lines[1].sentence == "Set 2: 10 to 15 reps, with 2 drops.")
    }

    @Test("A planned set's values: the load the lifter sets, then the reps, the hold or the distance")
    func values() {
        func text(_ set: PlannedSet, _ measurement: MeasurementType, perSide: Bool = false) -> String {
            RoutinePlanPresenter.values(set, measurement: measurement, perSide: perSide)
        }
        #expect(text(PlannedSet(key: "s", repsMin: 6, repsMax: 10, load: .kg(100)), .weightreps) == "100 kg \u{00D7} 6\u{2013}10")
        #expect(text(PlannedSet(key: "s", repsMin: 8, repsMax: 12), .weightreps) == "8\u{2013}12 reps")
        #expect(text(PlannedSet(key: "s", repsMin: 12, repsMax: 12), .repsonly) == "12 reps")
        #expect(text(PlannedSet(key: "s", repsMin: 1, repsMax: 1), .repsonly) == "1 rep")
        #expect(text(PlannedSet(key: "s", repsMin: 10, repsMax: 10), .repsonly, perSide: true) == "10 per side")
        #expect(text(PlannedSet(key: "s", repsMin: 10, repsMax: 10), .bodyweightreps) == "10 reps")
        #expect(text(PlannedSet(key: "s", repsMin: 8, repsMax: 8, load: .kg(10)), .weightedbodyweightreps) == "BW + 10 kg \u{00D7} 8")
        #expect(
            text(PlannedSet(key: "s", repsMin: 8, repsMax: 8, assistance: .kg(30)), .bodyweightassistedreps)
                == "BW \u{2212} 30 kg \u{00D7} 8")
        #expect(
            text(PlannedSet(key: "s", repsMin: 5, repsMax: 5, loadPercent: 70, loadReference: .onerm), .weightreps)
                == "70% 1RM \u{00D7} 5")
        #expect(text(PlannedSet(key: "s", durationMin: 30, durationMax: 45), .duration) == "0:30\u{2013}0:45 hold")
        #expect(text(PlannedSet(key: "s", load: .kg(10), durationMin: 45, durationMax: 45), .weightduration) == "10 kg for 0:45")
        #expect(text(PlannedSet(key: "s", load: .kg(20), distance: Length(metres: 40)), .shortdistanceweight) == "20 kg for 40 m")
        #expect(text(PlannedSet(key: "s", durationMin: 120, distance: Length(metres: 500)), .distanceduration) == "500 m in 2:00")
        #expect(text(PlannedSet(key: "s"), .weightreps) == "any reps")
        #expect(text(PlannedSet(key: "s", rpeMin: 8, rpeMax: 8), .bodyweightreps) == "to 2 left")
        #expect(text(PlannedSet(key: "s", load: .kg(20), rpeMin: 8, rpeMax: 8), .weightreps) == "20 kg")
        #expect(text(PlannedSet(key: "s", repsMax: 12), .repsonly) == "\u{2264}12 reps")
        #expect(RoutinePlanPresenter.effort(PlannedSet(key: "s", rpeMin: 7, rpeMax: d("8.5"))) == "1\u{2013}3 left")
    }

    @Test("The open set's fields step a load by the exercise's grid and a range by its ends")
    func editorFields() throws {
        let (routine, catalog) = try Plan.routine(Plan.dayOne)
        let editor = try Plan.editor(routine, catalog, "leg-press", "s1")
        #expect(editor.title == "Set 1" && editor.setLabel == "1" && !editor.isWarmup && editor.structure == nil)
        #expect(editor.fields.map(\.kind) == [.load, .repsMin, .repsMax, .repsLeft])
        let load = editor.fields[0]
        #expect(load.value == "100" && load.unit == "kg" && load.isSet)
        #expect(load.minus?.title == "\u{2212}5" && load.minus?.change == .load(.kg(95)))
        #expect(load.plus?.change == .load(.kg(105)))
        #expect(load.plus?.accessibilityLabel == "5 kilograms more")
        let from = editor.fields[1]
        let to = editor.fields[2]
        #expect(from.label == "Reps from" && from.value == "6" && to.label == "to" && to.value == "10")
        #expect(from.minus?.change == .reps(min: 5, max: 10) && from.plus?.change == .reps(min: 7, max: 10))
        #expect(to.minus?.change == .reps(min: 6, max: 9) && to.plus?.change == .reps(min: 6, max: 11))
        // The rest: the slot's, until the set names its own.
        #expect(editor.rest == nil && editor.restDefault == 150 && editor.restText == "2:30 (default)")
        // Reps left, a target like the others: RPE 8 is 2 left; −1 is RPE 9, +1 RPE 7; No target clears it.
        let left = editor.fields[3]
        #expect(left.kind == .repsLeft && left.label == "Reps left" && left.value == "2" && left.unit == "reps left")
        #expect(left.minus?.change == .rpe(9) && left.plus?.change == .rpe(7))
        #expect(left.accessibilityValue == "2 reps left, planned")
        #expect(editor.clearEffort == .rpe(nil))

        let warmup = try Plan.editor(routine, catalog, "leg-press", "w2")
        #expect(warmup.title == "Warm-up 2" && warmup.setLabel == "W" && warmup.isWarmup)
        #expect(warmup.rest == 90 && warmup.restText == "1:30" && warmup.restDefault == RestDefaults.warmupRest)
    }

    @Test("A field the plan leaves open says so, and its plus starts from last time")
    func openFields() throws {
        let (_, catalog) = try Plan.routine(Plan.dayOne)
        var routine = Routine(id: "day-1-strength-foundation", name: "Test")
        routine.slots = [
            RoutineSlot(
                key: "leg-press", exercise: "leg-press-machine",
                sets: [
                    PlannedSet(key: "s1"), PlannedSet(key: "s2", repsMin: 1, repsMax: 1, rirMin: 2, rirMax: 2),
                ])
        ]
        let open = try Plan.editor(routine, catalog, "leg-press", "s1")
        #expect(open.fields.map(\.value) == ["Auto", "Any", "Auto"])
        #expect(open.fields.allSatisfy { !$0.isSet && $0.minus == nil })
        // Monday's first working set was 105 kg.
        #expect(open.fields[0].plus?.change == .load(.kg(105)))
        #expect(open.fields[1].plus?.change == .reps(min: 8, max: 12))
        // No effort target: + sets 2 left, and there is nothing to clear.
        #expect(open.fields[2].plus?.change == .rpe(8) && open.clearEffort == nil)

        // One number is one field, stepping both ends; 1 goes no lower.
        let single = try Plan.editor(routine, catalog, "leg-press", "s2")
        #expect(single.fields.map(\.label) == ["Load", "Reps", "Reps left"])
        #expect(single.fields[1].minus == nil && single.fields[1].plus?.change == .reps(min: 2, max: 2))
        // Reps in reserve read as reps left, and a step writes an RPE in their place.
        #expect(single.fields[2].value == "2" && single.fields[2].plus?.change == .rpe(7))
        // A low load steps down to nothing: the session suggests it again.
        let low = RoutinePlanPresenter.massField(
            .load, label: "Load", .kg(5), last: nil,
            config: SuggestionInputs.config(
                for: try #require(catalog.exercises["leg-press-machine"]),
                defaultUnit: .kg))
        #expect(low.minus?.change == .load(nil))
    }

    @Test("A change writes a range with both ends, and an RPE target replaces reps in reserve")
    func changes() {
        var set = PlannedSet(key: "s1", repsMin: 8, repsMax: 12, rirMin: 1, rirMax: 2)
        RoutinePlanChange.rpe(d("8.5")).apply(to: &set)
        #expect(set.rpeMin == d("8.5") && set.rpeMax == d("8.5") && set.rirMin == nil && set.rirMax == nil)
        RoutinePlanChange.reps(min: 6, max: 10).apply(to: &set)
        #expect(set.repsMin == 6 && set.repsMax == 10)
        RoutinePlanChange.rest(120).apply(to: &set)
        #expect(set.rest == 120)
        RoutinePlanChange.load(.kg(50)).apply(to: &set)
        RoutinePlanChange.duration(min: 30, max: 45).apply(to: &set)
        #expect(set.load == .kg(50) && set.durationMin == 30 && set.durationMax == 45)
        RoutinePlanChange.rpe(nil).apply(to: &set)
        #expect(set.rpeMin == nil && set.rpeMax == nil)
    }

    @Test("A hold's fields step by the exercise's time step")
    func holdFields() throws {
        let (_, catalog) = try Plan.routine(Plan.dayOne)
        var routine = Routine(id: "core", name: "Core")
        routine.slots = [RoutineSlot(key: "plank", exercise: "plank", sets: [PlannedSet(key: "s1", durationMin: 30, durationMax: 60)])]
        let editor = try Plan.editor(routine, catalog, "plank", "s1")
        #expect(editor.fields.map(\.label) == ["Hold from", "to", "Reps left"])
        #expect(editor.fields.map(\.value) == ["0:30", "1:00", "Auto"])
        #expect(editor.fields[0].plus?.change == .duration(min: 35, max: 60))
        #expect(editor.fields[1].minus?.change == .duration(min: 30, max: 55))
        let line = try #require(try Plan.present(routine, catalog).blocks.first?.lines.first)
        #expect(line.values == "0:30\u{2013}1:00 hold")
        // One time is one field, stepping both ends.
        routine.slots[0].sets[0].durationMax = 30
        let single = try Plan.editor(routine, catalog, "plank", "s1")
        #expect(single.fields.map(\.label) == ["Hold", "Reps left"])
        #expect(single.fields[0].plus?.change == .duration(min: 35, max: 35))
    }
}
