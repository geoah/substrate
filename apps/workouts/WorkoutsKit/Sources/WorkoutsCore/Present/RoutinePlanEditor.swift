import Foundation

/// A planned set open in place on the routine page (§7.3): the panel a
/// session opens on its set, for targets instead of values. Each field's
/// steps are whole changes (`RoutinePlanChange`), which the view applies
/// through `RoutineEditor.updateSet`; the view computes no rule.
public struct RoutinePlanSetEditor: Equatable, Sendable {
    public var ref: RoutinePlanSetRef
    /// `Set 2`, `Bird Dog, set 1`, `Warm-up`.
    public var title: String
    /// `W`, `2`.
    public var setLabel: String
    public var isWarmup: Bool
    /// `Set 2 is a drop set: 1 drop`: a structure, changed under More; nil
    /// for a straight set.
    public var structure: String?
    /// The load, then the reps or the hold, then reps left, as a session's
    /// panel lists them.
    public var fields: [RoutinePlanField]
    /// `No target`, beside reps left: the plan leaves the effort to the
    /// session; nil when it names none.
    public var clearEffort: RoutinePlanChange?
    /// The rest after this set, its own; nil takes `restDefault`.
    public var rest: Seconds?
    /// The slot's rest, or the app's for the exercise.
    public var restDefault: Seconds

    public init(
        ref: RoutinePlanSetRef, title: String, setLabel: String, isWarmup: Bool, structure: String?,
        fields: [RoutinePlanField], clearEffort: RoutinePlanChange?, rest: Seconds?, restDefault: Seconds
    ) {
        self.ref = ref
        self.title = title
        self.setLabel = setLabel
        self.isWarmup = isWarmup
        self.structure = structure
        self.fields = fields
        self.clearEffort = clearEffort
        self.rest = rest
        self.restDefault = restDefault
    }

    /// `Rest after this set, 2:00 (default)`.
    public var restText: String {
        rest.map(Format.rest) ?? "\(Format.rest(restDefault)) (default)"
    }
}

/// One target of the open set: its label, value and steps.
public struct RoutinePlanField: Equatable, Sendable, Identifiable {
    public enum Kind: String, Sendable {
        case load, assistance, repsMin, repsMax, durationMin, durationMax, repsLeft
    }

    public var kind: Kind
    /// `Load`, `Reps from`, `Hold to`.
    public var label: String
    /// `90`, `8`, `0:45`; `Auto` when the plan leaves it to the session.
    public var value: String
    /// `kg`, `reps`; nil for a time.
    public var unit: String?
    /// The plan names a value: drawn as a number, not a hint.
    public var isSet: Bool
    public var minus: RoutinePlanStep?
    public var plus: RoutinePlanStep?
    /// `Load`, `Fewest reps`.
    public var accessibilityLabel: String
    /// `90 kilograms`, `not set, the session suggests it`.
    public var accessibilityValue: String

    public init(
        kind: Kind, label: String, value: String, unit: String?, isSet: Bool, minus: RoutinePlanStep?,
        plus: RoutinePlanStep?, accessibilityLabel: String, accessibilityValue: String
    ) {
        self.kind = kind
        self.label = label
        self.value = value
        self.unit = unit
        self.isSet = isSet
        self.minus = minus
        self.plus = plus
        self.accessibilityLabel = accessibilityLabel
        self.accessibilityValue = accessibilityValue
    }

    public var id: String { kind.rawValue }
}

/// One side of a stepper: `−2.5`, `+1`.
public struct RoutinePlanStep: Equatable, Sendable {
    public var title: String
    /// `2.5 kilograms less`, `1 rep more`.
    public var accessibilityLabel: String
    public var change: RoutinePlanChange

    public init(title: String, accessibilityLabel: String, change: RoutinePlanChange) {
        self.title = title
        self.accessibilityLabel = accessibilityLabel
        self.change = change
    }
}

/// A change to one planned set's targets, whole: a range is written with
/// both its ends, so it never runs backwards.
public enum RoutinePlanChange: Equatable, Sendable {
    case load(Mass?)
    case assistance(Mass?)
    case reps(min: Int?, max: Int?)
    case duration(min: Seconds?, max: Seconds?)
    /// One RPE, both ends; a reps-in-reserve target gives way to it.
    case rpe(Decimal?)
    case rest(Seconds?)

    public func apply(to set: inout PlannedSet) {
        switch self {
        case let .load(mass):
            set.load = mass
        case let .assistance(mass):
            set.assistance = mass
        case let .reps(min, max):
            set.repsMin = min
            set.repsMax = max
        case let .duration(min, max):
            set.durationMin = min
            set.durationMax = max
        case let .rpe(value):
            set.rpeMin = value
            set.rpeMax = value
            if value != nil {
                set.rirMin = nil
                set.rirMax = nil
            }
        case let .rest(seconds):
            set.rest = seconds
        }
    }
}

public extension RoutinePlanPresenter {
    /// The editor of one planned set; nil when the routine no longer holds it.
    static func editor(
        _ ref: RoutinePlanSetRef, in routine: Routine, exercises: [ExerciseID: Exercise], history: HistoryIndex,
        rests: SessionSettings = SessionSettings(), defaultUnit: MassUnit = .kg
    ) -> RoutinePlanSetEditor? {
        guard let slot = routine.slots.first(where: { $0.key == ref.slot }),
            let planned = slot.sets.first(where: { $0.key == ref.set })
        else { return nil }
        let builder = PlanBuilder(routine: routine, exercises: exercises, history: history, rests: rests)
        let exercise = exercises[slot.exercise]
        let measurement = exercise?.measurement ?? .weightreps
        let last = builder.lastSets(slot)[planned.key]
        var fields: [RoutinePlanField] = []
        if let exercise {
            let config = SuggestionInputs.config(for: exercise, defaultUnit: defaultUnit)
            switch measurement.loadMode {
            case .external:
                fields.append(massField(.load, label: "Load", planned.load, last: last?.load, config: config))
            case .bodyweightplus:
                fields.append(massField(.load, label: "Added load", planned.load, last: last?.load, config: config))
            case .assisted:
                fields.append(massField(.assistance, label: "Assistance", planned.assistance, last: last?.assistance, config: config))
            case .bodyweight, nil:
                break
            }
            if measurement.recordsReps {
                fields += repsFields(planned, config: config)
            } else if measurement.isTimedHold {
                fields += durationFields(planned, step: config.timeStep)
            }
        } else {
            fields += repsFields(planned, config: nil)
        }
        let isGroup = slot.group.map { key in routine.groups.contains { $0.key == key } } ?? false
        let setTitle = builder.setTitle(planned, in: slot)
        let name = exercise?.name ?? slot.exercise.rawValue
        let title = isGroup ? "\(name), \(setTitle.lowercased())" : setTitle
        let working = slot.sets.filter { $0.role == .working }
        let ordinal = (working.firstIndex { $0.key == planned.key } ?? 0) + 1
        fields.append(effortField(planned, measurement: measurement))
        let restDefault =
            planned.role == .warmup
            ? slot.warmupRest ?? rests.warmupRest
            : slot.setRest ?? rests.setRest[exercise?.restClass]
        return RoutinePlanSetEditor(
            ref: ref,
            title: title,
            setLabel: planned.role == .warmup ? "W" : "\(ordinal)",
            isWarmup: planned.role == .warmup,
            structure: planned.role == .working && planned.structure != .straight
                ? RoutinePresenter.specialText(planned, number: ordinal) : nil,
            fields: fields,
            clearEffort: effortLeft(planned) == nil ? nil : .rpe(nil),
            rest: planned.rest,
            restDefault: restDefault
        )
    }

    // MARK: - Fields

    /// A load in the exercise's grid: a step either way; from nothing, last
    /// time's load or the first point of the grid; below the grid's start,
    /// nothing, which leaves the load to the session.
    internal static func massField(
        _ kind: RoutinePlanField.Kind, label: String, _ current: Mass?, last: Mass?,
        config: ExerciseConfig
    ) -> RoutinePlanField {
        let unit = current?.unit ?? config.unit
        let step = Format.decimal(config.step.flatMap { $0 >= 0.01 ? $0 : nil } ?? (unit == .kg ? 2.5 : 5))
        let base = Format.decimal(config.base ?? 0)
        let stepText = Format.number(step)
        let unitName = unit.spokenName
        func change(_ mass: Mass?) -> RoutinePlanChange { kind == .assistance ? .assistance(mass) : .load(mass) }
        guard let current else {
            let start = last.map { $0.converted(to: unit) } ?? Mass(value: base > 0 ? base : step, unit: unit)
            return RoutinePlanField(
                kind: kind, label: label, value: "Auto", unit: nil, isSet: false, minus: nil,
                plus: RoutinePlanStep(
                    title: "+", accessibilityLabel: "Set to \(Format.number(start.value)) \(unitName)",
                    change: change(Mass(value: start.value.roundedTo(2), unit: unit))),
                accessibilityLabel: label, accessibilityValue: "Not set, the session suggests it"
            )
        }
        let down = current.value - step
        let minus = RoutinePlanStep(
            title: "\u{2212}\(stepText)",
            accessibilityLabel: down < max(base, 0) || down <= 0 ? "Clear, the session suggests it" : "\(stepText) \(unitName) less",
            change: change(down < max(base, 0) || down <= 0 ? nil : Mass(value: down, unit: unit))
        )
        let plus = RoutinePlanStep(
            title: "+\(stepText)", accessibilityLabel: "\(stepText) \(unitName) more",
            change: change(Mass(value: current.value + step, unit: unit)))
        return RoutinePlanField(
            kind: kind, label: label, value: Format.number(current.value), unit: unit.rawValue, isSet: true,
            minus: minus, plus: plus, accessibilityLabel: label,
            accessibilityValue: "\(Format.number(current.value)) \(unitName)")
    }

    /// The rep target: one field for one number, `from` and `to` for a
    /// range (More options makes one of a number), one for `at least` or
    /// `up to`, and one to add a target when the plan names none.
    internal static func repsFields(_ planned: PlannedSet, config: ExerciseConfig?) -> [RoutinePlanField] {
        func reps(_ n: Int) -> String { n == 1 ? "1 rep" : "\(n) reps" }
        func field(
            _ kind: RoutinePlanField.Kind, _ label: String, spoken: String, value: Int,
            minus: RoutinePlanChange?, plus: RoutinePlanChange
        ) -> RoutinePlanField {
            RoutinePlanField(
                kind: kind, label: label, value: "\(value)", unit: value == 1 ? "rep" : "reps", isSet: true,
                minus: minus.map { RoutinePlanStep(title: "\u{2212}1", accessibilityLabel: "1 rep fewer", change: $0) },
                plus: RoutinePlanStep(title: "+1", accessibilityLabel: "1 rep more", change: plus),
                accessibilityLabel: spoken, accessibilityValue: reps(value)
            )
        }
        switch (planned.repsMin, planned.repsMax) {
        case let (low?, high?) where low == high:
            return [
                field(
                    .repsMin, "Reps", spoken: "Reps", value: low,
                    minus: low > 1 ? .reps(min: low - 1, max: low - 1) : nil, plus: .reps(min: low + 1, max: low + 1))
            ]
        case let (low?, high?):
            return [
                field(
                    .repsMin, "Reps from", spoken: "Fewest reps", value: low,
                    minus: low > 1 ? .reps(min: low - 1, max: high) : nil, plus: .reps(min: low + 1, max: max(high, low + 1))),
                field(
                    .repsMax, "to", spoken: "Most reps", value: high,
                    minus: high > 1 ? .reps(min: min(low, high - 1), max: high - 1) : nil, plus: .reps(min: low, max: high + 1)),
            ]
        case let (low?, nil):
            return [
                field(
                    .repsMin, "At least", spoken: "At least", value: low,
                    minus: low > 1 ? .reps(min: low - 1, max: nil) : nil, plus: .reps(min: low + 1, max: nil))
            ]
        case let (nil, high?):
            return [
                field(
                    .repsMax, "Up to", spoken: "Up to", value: high,
                    minus: high > 1 ? .reps(min: nil, max: high - 1) : nil, plus: .reps(min: nil, max: high + 1))
            ]
        case (nil, nil):
            let low = config?.defaultRepsMin ?? 8
            let high = max(low, config?.defaultRepsMax ?? 12)
            return [
                RoutinePlanField(
                    kind: .repsMin, label: "Reps", value: "Any", unit: nil, isSet: false, minus: nil,
                    plus: RoutinePlanStep(title: "+", accessibilityLabel: "Aim at \(low) to \(high) reps", change: .reps(min: low, max: high)),
                    accessibilityLabel: "Reps", accessibilityValue: "No target"
                )
            ]
        }
    }

    /// A hold's range, in the exercise's time step.
    internal static func durationFields(_ planned: PlannedSet, step: Seconds) -> [RoutinePlanField] {
        let step = max(1, step)
        let spokenStep = RowsText.spokenDuration(step)
        func field(
            _ kind: RoutinePlanField.Kind, _ label: String, spoken: String, value: Seconds,
            minus: RoutinePlanChange?, plus: RoutinePlanChange
        ) -> RoutinePlanField {
            RoutinePlanField(
                kind: kind, label: label, value: Format.duration(value), unit: nil, isSet: true,
                minus: minus.map {
                    RoutinePlanStep(
                        title: "\u{2212}\(Format.duration(step))", accessibilityLabel: "\(spokenStep) less",
                        change: $0)
                },
                plus: RoutinePlanStep(title: "+\(Format.duration(step))", accessibilityLabel: "\(spokenStep) more", change: plus),
                accessibilityLabel: spoken, accessibilityValue: RowsText.spokenDuration(value)
            )
        }
        guard let low = planned.durationMin ?? planned.durationMax else {
            return [
                RoutinePlanField(
                    kind: .durationMin, label: "Hold", value: "Any", unit: nil, isSet: false, minus: nil,
                    plus: RoutinePlanStep(title: "+", accessibilityLabel: "Aim at 30 seconds", change: .duration(min: 30, max: 30)),
                    accessibilityLabel: "Hold", accessibilityValue: "No target"
                )
            ]
        }
        let high = max(low, planned.durationMax ?? low)
        if low == high {
            return [
                field(
                    .durationMin, "Hold", spoken: "Hold", value: low,
                    minus: low > step ? .duration(min: low - step, max: low - step) : nil,
                    plus: .duration(min: low + step, max: low + step))
            ]
        }
        return [
            field(
                .durationMin, "Hold from", spoken: "Shortest hold", value: low,
                minus: low > step ? .duration(min: low - step, max: high) : nil,
                plus: .duration(min: low + step, max: max(high, low + step))),
            field(
                .durationMax, "to", spoken: "Longest hold", value: high,
                minus: high > step ? .duration(min: min(low, high - step), max: high - step) : nil,
                plus: .duration(min: low, max: high + step)),
        ]
    }

    /// The reps left the plan aims at, fewest first: the top of an RPE
    /// range (RPE 7–8 aims at 2), else the fewest reps in reserve.
    internal static func effortLeft(_ planned: PlannedSet) -> Decimal? {
        if let top = planned.rpeMax ?? planned.rpeMin { return max(0, 10 - top) }
        return planned.rirMin ?? planned.rirMax
    }

    /// Reps left, a target like the others: − and + a rep at a time, 0 to
    /// 4+, written as one RPE (RPE = 10 − reps left), which takes a reps in
    /// reserve target's place; from none, `+` sets 2 left.
    internal static func effortField(_ planned: PlannedSet, measurement: MeasurementType) -> RoutinePlanField {
        let label = "Reps left"
        func change(_ left: Decimal) -> RoutinePlanChange { .rpe(10 - left) }
        guard let left = effortLeft(planned) else {
            return RoutinePlanField(
                kind: .repsLeft, label: label, value: "Auto", unit: nil, isSet: false, minus: nil,
                plus: RoutinePlanStep(title: "+", accessibilityLabel: "Set to 2 reps left", change: change(2)),
                accessibilityLabel: label, accessibilityValue: "Not set, no effort target")
        }
        let whole = left.roundedTo(0) == left
        let fewer = whole ? left - 1 : (left - Decimal(5) / 10).roundedTo(0)
        let more = whole ? left + 1 : (left + Decimal(5) / 10).roundedTo(0)
        let shown = Format.effortRange(
            rpeMin: planned.rpeMin, rpeMax: planned.rpeMax, rirMin: planned.rirMin, rirMax: planned.rirMax)
            ?? Format.number(left)
        let value = left == RowsEffort.most && shown == "4" ? "4+" : shown
        return RoutinePlanField(
            kind: .repsLeft, label: label, value: value, unit: measurement.recordsReps ? "reps left" : "left", isSet: true,
            minus: fewer >= 0
                ? RoutinePlanStep(title: "\u{2212}1", accessibilityLabel: "1 rep left fewer", change: change(fewer)) : nil,
            plus: more <= RowsEffort.most
                ? RoutinePlanStep(title: "+1", accessibilityLabel: "1 rep left more", change: change(more)) : nil,
            accessibilityLabel: label, accessibilityValue: "\(value) reps left, planned")
    }
}
