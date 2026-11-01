import Foundation

// The editor open in place on the Rows layout: the set to do now (`log`), or
// a done set being fixed (`fix`). Every control carries the `SetValuesPatch`
// it applies, so the view never works a value out:
//
// - `log`: send `.edit(row, patch)`; the presentation comes back with the change.
// - `fix`: apply it to the draft, `draft = draft.applying(patch)`, and present
//   again with `RowsFix.editor(draft:)`; Save sends `RowsFix.save(draft)`.

/// The open editor of one row.
public struct RowsEditor: Equatable, Sendable {
    public enum Mode: String, Equatable, Sendable {
        /// The set to do now: patches go to the engine at once, Log ticks.
        case log
        /// A done set: patches go to a draft the view holds, Save sends them.
        case fix
    }

    /// The panel's colour.
    public enum Tint: String, Equatable, Sendable {
        /// The app's tint: a working set to do now.
        case accent
        /// Orange: a warm-up or a drop.
        case orange
        /// No tint: fixing a done set, while the rest keeps running.
        case neutral
    }

    public var row: RowID
    public var mode: Mode
    public var tint: Tint
    /// `Set 2`, `Warm-up 1`, `Drop`, `Mini-set 3`; in a group the exercise's name.
    public var title: String
    /// `A2`, in a group.
    public var badge: String?
    /// The header line after the title: the suggestion's reason, `after the rest`,
    /// `of 2`, `to failure`, `at 6 kg`, `holding`, `logged at 09:28`.
    public var note: RowsNote?
    /// On the header's right: `no rest` on a drop.
    public var trailingNote: String?
    /// The suggestion's text, whatever `note` shows.
    public var reason: String?
    /// Load and reps, or the time, or distance and load, in the grid's order.
    public var fields: [RowsField]
    /// The RPE strip; nil for a row that takes none.
    public var rpe: RowsRPEStrip?
    /// A timed row's clock and track; nil otherwise.
    public var timer: RowsTimer?
    /// Log (`Log 110 kg × 8 @ 8`), Start, Stop and log, or Save.
    public var primary: RowsButton
    /// `Log 1:07` beside Start, `Cancel` beside Stop, `End myo-reps` under a mini-set.
    public var secondary: RowsButton?
    /// `Delete set`, when fixing.
    public var delete: RowsButton?
    /// `Cancel`, when fixing: drop the draft, send nothing.
    public var cancelTitle: String?

    public init(row: RowID, mode: Mode, tint: Tint, title: String, badge: String?, note: RowsNote?, trailingNote: String?,
                reason: String?, fields: [RowsField], rpe: RowsRPEStrip?, timer: RowsTimer?, primary: RowsButton,
                secondary: RowsButton?, delete: RowsButton?, cancelTitle: String?)
    {
        self.row = row
        self.mode = mode
        self.tint = tint
        self.title = title
        self.badge = badge
        self.note = note
        self.trailingNote = trailingNote
        self.reason = reason
        self.fields = fields
        self.rpe = rpe
        self.timer = timer
        self.primary = primary
        self.secondary = secondary
        self.delete = delete
        self.cancelTitle = cancelTitle
    }

    public func field(_ field: ValueField) -> RowsField? {
        fields.first { $0.field == field }
    }
}

/// The editor's header note.
public struct RowsNote: Equatable, Sendable {
    public enum Kind: String, Equatable, Sendable {
        /// Why today's numbers: a sparkle before it.
        case reason
        /// Plain grey: `after the rest`, `of 2`, `to failure`, `at 6 kg`, `logged at 09:28`.
        case status
        /// A red dot: `holding`, `timing`.
        case running
    }

    public var text: String
    public var kind: Kind

    public init(text: String, kind: Kind) {
        self.text = text
        self.kind = kind
    }
}

/// One value of the editor: the big number, its unit, last time beside it,
/// the reset chip, and the steppers under the thumb.
public struct RowsField: Equatable, Sendable, Identifiable {
    public var field: ValueField
    public var id: ValueField { field }
    /// `110`, `8`, `1:07`; nil when empty (then `placeholder`).
    public var value: String?
    /// What an empty field shows instead: `to failure`, `≈15` (a hint, never logged).
    public var placeholder: String?
    /// `kg`, `reps`, `per side`, `kg added`, `kg assist`, `m`; nil for a time.
    public var unit: String?
    /// `last 105`, at a fixed position after the unit; nil with no last time.
    public var last: String?
    /// The value differs from the suggestion (`log`) or from what was logged (`fix`): tint it.
    public var isChanged: Bool
    /// The chip after `last`: back to the suggestion or to what was logged.
    public var reset: RowsReset?
    /// Required with nothing to log: outline it.
    public var isMissing: Bool
    /// `−5` and `+5`, `−1` and `+1`; nil when the step would go below 0 (or 1 rep).
    public var minus: RowsStep?
    public var plus: RowsStep?
    /// The keypad a tap on the number opens (the dotted underline).
    public var keypad: RowsKeypad
    /// `Leg Press (Machine), set 2, load in kilograms`.
    public var accessibilityLabel: String
    /// `110, suggested`, `105, changed from 110`, `to failure, empty`.
    public var accessibilityValue: String

    public init(field: ValueField, value: String?, placeholder: String?, unit: String?, last: String?, isChanged: Bool,
                reset: RowsReset?, isMissing: Bool, minus: RowsStep?, plus: RowsStep?, keypad: RowsKeypad,
                accessibilityLabel: String, accessibilityValue: String)
    {
        self.field = field
        self.value = value
        self.placeholder = placeholder
        self.unit = unit
        self.last = last
        self.isChanged = isChanged
        self.reset = reset
        self.isMissing = isMissing
        self.minus = minus
        self.plus = plus
        self.keypad = keypad
        self.accessibilityLabel = accessibilityLabel
        self.accessibilityValue = accessibilityValue
    }
}

/// A stepper half.
public struct RowsStep: Equatable, Sendable {
    /// `−5`, `+1`, `+0:05`.
    public var title: String
    /// `5 kilograms less`, `1 rep more`.
    public var accessibilityLabel: String
    public var patch: SetValuesPatch

    public init(title: String, accessibilityLabel: String, patch: SetValuesPatch) {
        self.title = title
        self.accessibilityLabel = accessibilityLabel
        self.patch = patch
    }
}

/// The reset chip: `↺ 8` back to the suggestion, `↺ was 8` back to what was logged.
public struct RowsReset: Equatable, Sendable {
    /// `8`, `was 8`; the view draws the ↺ before it.
    public var text: String
    /// `Back to 8`, `Back to 8, as logged`.
    public var accessibilityLabel: String
    public var patch: SetValuesPatch

    public init(text: String, accessibilityLabel: String, patch: SetValuesPatch) {
        self.text = text
        self.accessibilityLabel = accessibilityLabel
        self.patch = patch
    }
}

/// The keypad for one field (glass, floating): digits, and for a load the
/// nearby steps of its grid as chips that apply at once.
public struct RowsKeypad: Equatable, Sendable {
    public var field: ValueField
    /// `Load in kg`, `Reps`, `Time, minutes and seconds`, `Distance in m`.
    public var title: String
    /// `5 kg steps, tap one to set it`; nil with no chips.
    public var chipsTitle: String?
    /// Six consecutive steps of the exercise's grid from one below the value (`105` to `130`).
    public var chips: [RowsKeypadChip]
    /// The `.` key: loads and distances.
    public var allowsDecimal: Bool
    let unit: MassUnit

    public init(field: ValueField, title: String, chipsTitle: String?, chips: [RowsKeypadChip], allowsDecimal: Bool,
                unit: MassUnit)
    {
        self.field = field
        self.title = title
        self.chipsTitle = chipsTitle
        self.chips = chips
        self.allowsDecimal = allowsDecimal
        self.unit = unit
    }

    /// The edit for the typed text; nil while it is not a value. A time is
    /// typed as digits, the last two the seconds: `107` is 1:07, `45` is 0:45.
    public func patch(_ typed: String) -> SetValuesPatch? {
        guard let value = RowsKeypad.value(typed, field: field) else { return nil }
        return FieldAccess.setting(field, to: value, unit: unit)
    }

    /// The confirm button: `Set 120 kg`, `Set 8 reps`, `Set 1:07`; `Set` while the text is no value.
    public func confirmTitle(_ typed: String) -> String {
        guard let value = RowsKeypad.value(typed, field: field) else { return "Set" }
        return "Set \(RowsText.fieldText(field, value, unit: unit, withUnit: true))"
    }

    /// The typed text as the field's number: a load or distance in its unit,
    /// reps, or seconds.
    static func value(_ typed: String, field: ValueField) -> Decimal? {
        let text = typed.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: ",", with: ".")
        guard !text.isEmpty else { return nil }
        switch field {
        case .load, .assistance, .distance:
            guard let value = DecimalText.parse(text), value >= 0 else { return nil }
            return value
        case .reps:
            guard text.allSatisfy(\.isASCIIDigit), let reps = Int(text) else { return nil }
            return Decimal(reps)
        case .duration:
            guard text.allSatisfy(\.isASCIIDigit), let number = Int(text) else { return nil }
            // Two digits or fewer are seconds; more put minutes before the last two.
            if text.count <= 2 { return number >= 1 ? Decimal(number) : nil }
            let (minutes, seconds) = (number / 100, number % 100)
            guard seconds < 60 else { return nil }
            return Decimal(minutes * 60 + seconds)
        }
    }
}

/// A keypad chip: one step of the grid.
public struct RowsKeypadChip: Equatable, Sendable, Identifiable {
    /// `120`.
    public var text: String
    /// It is the value the field holds now: filled.
    public var isCurrent: Bool
    public var patch: SetValuesPatch
    public var id: String { text }

    public init(text: String, isCurrent: Bool, patch: SetValuesPatch) {
        self.text = text
        self.isCurrent = isCurrent
        self.patch = patch
    }
}

/// The full-width RPE strip: 6 to 10 in half steps. A value that is no
/// rating yet (the plan's pre-fill, or a logged pre-filled or copied value)
/// is one outlined, hollow cell; a tap on it rates it. A rating is filled
/// solid; a tap on it clears it.
public struct RowsRPEStrip: Equatable, Sendable {
    public var cells: [RowsRPECell]
    /// The value the strip shows, rated or not.
    public var shown: Decimal?
    /// `shown` is the owner's rating (a tap, or an imported value).
    public var isRated: Bool
    /// Hidden behind `addTitle` until the owner opens it (view state): no value, no
    /// pre-fill and no rating last time, or a warm-up or timed set.
    public var isCollapsed: Bool
    /// `+ RPE`.
    public var addTitle: String
    /// `Leg Press (Machine), set 2, RPE`.
    public var accessibilityLabel: String
    /// `8, planned, not rated yet`, `8.5`, `9, copied from last time`, `not rated`.
    public var accessibilityValue: String

    public init(cells: [RowsRPECell], shown: Decimal?, isRated: Bool, isCollapsed: Bool, addTitle: String,
                accessibilityLabel: String, accessibilityValue: String)
    {
        self.cells = cells
        self.shown = shown
        self.isRated = isRated
        self.isCollapsed = isCollapsed
        self.addTitle = addTitle
        self.accessibilityLabel = accessibilityLabel
        self.accessibilityValue = accessibilityValue
    }

    public static let addTitle = "+ RPE"
}

/// One cell of the RPE strip (44 pt wide).
public struct RowsRPECell: Equatable, Sendable, Identifiable {
    public enum Style: String, Equatable, Sendable {
        case plain
        /// Shown but no rating: the plan's pre-fill, or a pre-filled or copied
        /// value. An outline, hollow, at the cell's full size.
        case outlined
        /// The owner's rating: filled solid, the fill inset in the cell (no halo).
        case filled
    }

    public var value: Decimal
    /// `8`, `8.5`.
    public var text: String
    /// A half step: smaller, regular weight.
    public var isHalf: Bool
    public var style: Style
    /// A tap: `enteringRPE(value)`, on an outlined cell too (it then counts as
    /// rated), or `clearingRPE` on the filled cell.
    public var patch: SetValuesPatch
    /// What the header says for a moment after the tap: `RPE 8.5: could do 1 more rep, maybe 2`.
    public var meaning: String
    public var id: String { text }

    public init(value: Decimal, text: String, isHalf: Bool, style: Style, patch: SetValuesPatch, meaning: String) {
        self.value = value
        self.text = text
        self.isHalf = isHalf
        self.style = style
        self.patch = patch
        self.meaning = meaning
    }
}

/// A timed row's clock and its track: 0:00 on the left, last time below the
/// track and the goal above it, so the labels never collide.
public struct RowsTimer: Equatable, Sendable {
    public enum Phase: Equatable, Sendable {
        /// Start counts down to `goal` (a hold), or runs a stopwatch.
        case ready
        /// The big clock counts up from `startedAt` (`Text(timerInterval:)`).
        case running(startedAt: Date, endsAt: Date?)
    }

    public var phase: Phase
    /// What Start counts down to: the shown duration of a hold; nil for a stopwatch.
    public var goal: Seconds?
    /// Last time's duration.
    public var last: Seconds?
    /// The track's length, a little past the goal and last time.
    public var trackEnd: Seconds
    /// `goal 1:07`, above the track.
    public var goalText: String?
    /// `last 1:02`, below the track.
    public var lastText: String?

    public init(phase: Phase, goal: Seconds?, last: Seconds?, trackEnd: Seconds, goalText: String?, lastText: String?) {
        self.phase = phase
        self.goal = goal
        self.last = last
        self.trackEnd = trackEnd
        self.goalText = goalText
        self.lastText = lastText
    }

    /// Where `seconds` sits on the track, 0 to 1.
    public func fraction(_ seconds: Seconds) -> Double {
        guard trackEnd > 0 else { return 0 }
        return min(1, max(0, Double(seconds) / Double(trackEnd)))
    }

    /// The line beside the running clock: `0:21 to beat last time`, `0:04 to the goal`,
    /// `Goal reached`; nil for a stopwatch with no last time. Its width does not
    /// change as the seconds tick (monospaced digits).
    public func caption(elapsed: Seconds) -> String? {
        if let last, elapsed < last { return "\(Format.duration(last - elapsed)) to beat last time" }
        if let goal {
            return elapsed < goal ? "\(Format.duration(goal - elapsed)) to the goal" : "Goal reached"
        }
        if last != nil { return "Past last time" }
        return nil
    }

    /// A track a little longer than the goal and last time, on a 5 s step.
    static func trackEnd(goal: Seconds?, last: Seconds?) -> Seconds {
        let longest = max(goal ?? 0, last ?? 0, 30)
        let padded = Int((Double(longest) * 1.15).rounded(.up))
        return (padded + 4) / 5 * 5
    }
}

/// A done row's fix: the editor opens on its logged values; each change goes
/// to a draft the view holds; Save sends one edit; the rest keeps running.
public struct RowsFix: Equatable, Sendable {
    public var row: RowID
    /// What was logged: the draft starts here.
    public var logged: SetValues
    let source: RowsEditorSource

    init(row: RowID, logged: SetValues, source: RowsEditorSource) {
        self.row = row
        self.logged = logged
        self.source = source
    }

    /// The fix editor for a draft (`logged`, then every patch applied with
    /// `SetValues.applying`). Save is enabled once the draft differs and is complete.
    public func editor(draft: SetValues) -> RowsEditor {
        let missing = source.measurement.missing(in: draft)
        let save = save(draft)
        var editor = source.editor(values: draft, rpe: draft.rpe.map { .value($0, draft.rpeMethod ?? .entered) } ?? .none,
                                   commit: missing.isEmpty ? draft : nil, canCommit: missing.isEmpty && save != nil,
                                   isMissing: missing)
        if let save { editor.primary.action = save }
        return editor
    }

    /// Save: `.edit(row, patch)` with what changed; nil when nothing did.
    public func save(_ draft: SetValues) -> RowsAction? {
        savePatch(draft).map { .edit(row, $0) }
    }

    /// The fields the draft changed: each set or cleared; the RPE with its method
    /// (a tap made it `entered`).
    func savePatch(_ draft: SetValues) -> SetValuesPatch? {
        var patch = SetValuesPatch()
        var changed = false
        func field<V: Equatable>(_ new: V?, _ old: V?) -> FieldPatch<V> {
            guard new != old else { return .unchanged }
            changed = true
            return new.map(FieldPatch.set) ?? .cleared
        }
        patch.load = field(draft.load, logged.load)
        patch.assistance = field(draft.assistance, logged.assistance)
        patch.reps = field(draft.reps, logged.reps)
        patch.duration = field(draft.duration, logged.duration)
        patch.distance = field(draft.distance, logged.distance)
        if draft.rpe != logged.rpe || (draft.rpe != nil && draft.rpeMethod != logged.rpeMethod) {
            changed = true
            patch.rpe = draft.rpe.map(FieldPatch.set) ?? .cleared
            patch.rpeMethod = draft.rpe == nil ? nil : draft.rpeMethod ?? .entered
        }
        return changed ? patch : nil
    }
}

// MARK: - Building

/// Everything an editor needs that does not change while it is open: the row,
/// its words, the suggestion or what was logged, last time, the steps and the grid.
struct RowsEditorSource: Equatable, Sendable {
    var row: RowID
    var mode: RowsEditor.Mode
    var tint: RowsEditor.Tint
    var title: String
    var badge: String?
    var note: RowsNote?
    var trailingNote: String?
    var reason: String?
    /// `Leg Press (Machine), set 2`: VoiceOver's prefix.
    var spoken: String
    var measurement: MeasurementType
    /// The exercise's unit.
    var unit: MassUnit
    /// Reps count per side (an alternating exercise).
    var perSide: Bool
    /// The value fields, in the grid's order.
    var fields: [ValueField]
    /// `log`: the suggestion (the ghost values). `fix`: what was logged.
    var baseline: SetValues
    /// `log`: words an empty reps field shows (`to failure`).
    var repsPlaceholder: String?
    /// `log`: the baseline's reps are a hint (`≈15`), shown, never logged.
    var repsHint: Bool
    var previous: SetValues?
    var loadStep: Mass
    var grid: RowsGrid?
    var timeStep: Seconds
    /// The strip exists; with `rpeOpen` it shows with no value.
    var rpeOffered: Bool
    var rpeOpen: Bool
    /// `Log`, `Log warm-up`, `Log drop`, `Log A2,`, `Log mini-set 3,`.
    var logPrefix: String
    var timer: RowsTimer?
    var secondary: RowsButton?
    var isRunning: Bool
    var canStart: Bool

    /// The editor for `values`: the row's typed values (`log`) or the draft (`fix`).
    func editor(values: SetValues, rpe: RowRPE, commit: SetValues?, canCommit: Bool, isMissing: [ValueField]) -> RowsEditor {
        let shownFields = fields.filter { !(isRunning && $0 == .duration) }
        let fieldPresentations = shownFields.map { field($0, values: values, missing: isMissing.contains($0)) }
        let strip = rpeOffered ? rpeStrip(rpe) : nil
        let primary: RowsButton
        var secondary = secondary
        var delete: RowsButton?
        let shown = shownValues(values)
        let effort = commit?.rpe ?? rpe.shown
        // A mini-set's editor holds its reps alone; its load is the activation's.
        let repsAlone = fields == [.reps] && measurement.loadMode != nil && measurement.loadMode != .bodyweight
        func text(_ values: SetValues) -> String {
            repsAlone ? RowsText.values(SetValues(reps: values.reps), measurement: .repsonly, perSide: perSide)
                : RowsText.values(values, measurement: measurement, perSide: perSide)
        }
        switch mode {
        case .fix:
            let text = text(commit ?? shown)
            primary = RowsButton(title: "Save" + (text.isEmpty ? "" : " \(text)") + RowsText.effortSuffix(effort),
                                 action: .edit(row, SetValuesPatch()), isEnabled: canCommit)
            delete = RowsButton(title: "Delete set", accessibilityLabel: "Delete \(spoken)", action: .removeSet(row))
        case .log:
            let text = text(commit ?? shown)
            let logTitle = RowsText.logTitle(prefix: logPrefix, values: text, rpe: effort)
            if timer != nil, measurement.hasTimer {
                if isRunning {
                    primary = RowsButton(title: "Stop and log", accessibilityLabel: "Stop and log \(spoken)",
                                         action: .stopTimedSet(row), isEnabled: canCommit)
                    secondary = RowsButton(title: "Cancel", accessibilityLabel: "Cancel the timer", action: .cancelTimedSet(row))
                } else {
                    let goal = timer?.goal.map { ", goal \(Format.duration($0))" } ?? ""
                    primary = RowsButton(title: "Start", accessibilityLabel: "Start \(spoken)\(goal)",
                                         action: .startTimedSet(row), isEnabled: canStart)
                    secondary = RowsButton(title: logTitle, action: .tick(row), isEnabled: canCommit)
                }
            } else {
                primary = RowsButton(title: logTitle, action: .tick(row), isEnabled: canCommit)
            }
        }
        return RowsEditor(row: row, mode: mode, tint: tint, title: title, badge: badge, note: note, trailingNote: trailingNote,
                          reason: reason, fields: fieldPresentations, rpe: strip, timer: timer, primary: primary,
                          secondary: secondary, delete: delete, cancelTitle: mode == .fix ? "Cancel" : nil)
    }

    /// The values the editor shows: typed over the suggestion (`log`), the draft (`fix`).
    func shownValues(_ values: SetValues) -> SetValues {
        guard mode == .log else { return values }
        var shown = values
        for field in fields where FieldAccess.number(field, values) == nil {
            if field == .reps, repsHint || repsPlaceholder != nil { continue }
            shown = shown.applying(FieldAccess.restoring(field, from: baseline))
        }
        return shown
    }

    func field(_ field: ValueField, values: SetValues, missing: Bool) -> RowsField {
        let typed = FieldAccess.number(field, values)
        let base = FieldAccess.number(field, baseline)
        // A hint or "to failure" is no suggestion to go back to: it is never logged.
        let baselineIsHint = mode == .log && field == .reps && (repsHint || repsPlaceholder != nil)
        let hinted = baselineIsHint && typed == nil
        let current: Decimal? = typed ?? (mode == .log && !hinted ? base : nil)
        let currentUnit = FieldAccess.unit(field, values) ?? FieldAccess.unit(field, baseline) ?? unit
        let isChanged: Bool
        var reset: RowsReset?
        switch mode {
        case .log:
            isChanged = typed != nil && base != nil && !baselineIsHint && FieldAccess.differs(field, values, baseline)
            if isChanged, let base {
                let text = RowsText.fieldText(field, base, unit: FieldAccess.unit(field, baseline) ?? unit, withUnit: false)
                reset = RowsReset(text: text, accessibilityLabel: "Back to \(text)", patch: FieldAccess.clearing(field))
            }
        case .fix:
            isChanged = FieldAccess.differs(field, values, baseline)
            if isChanged {
                let text = base.map { RowsText.fieldText(field, $0, unit: FieldAccess.unit(field, baseline) ?? unit,
                                                         withUnit: false) } ?? "empty"
                reset = RowsReset(text: "was \(text)", accessibilityLabel: "Back to \(text), as logged",
                                  patch: FieldAccess.restoring(field, from: baseline))
            }
        }
        var placeholder: String?
        if current == nil, mode == .log, field == .reps {
            if let words = repsPlaceholder {
                placeholder = words
            } else if repsHint, let base {
                placeholder = "\u{2248}\(RowsText.fieldText(.reps, base, unit: unit, withUnit: false))"
            }
        }
        let previousValue = previous.flatMap { FieldAccess.number(field, $0) }
        let previousUnit = previous.flatMap { FieldAccess.unit(field, $0) } ?? unit
        let last = previousValue.map { "last \(RowsText.fieldText(field, $0, unit: previousUnit, withUnit: false))" }
        // Empty: the steppers start from last time, else the hint.
        let seed = current ?? previousValue ?? (hinted ? base : nil)
        let (minus, plus) = steps(field, from: seed, unit: currentUnit)
        let spokenName = RowsText.spokenField(field, measurement: measurement, unit: unit)
        let valueText = current.map { RowsText.fieldText(field, $0, unit: currentUnit, withUnit: false) }
        var accessibilityValue = valueText ?? placeholder.map { "\($0), empty" } ?? (missing ? "required, empty" : "empty")
        if let valueText {
            if isChanged, let base {
                let was = RowsText.fieldText(field, base, unit: FieldAccess.unit(field, baseline) ?? unit, withUnit: false)
                accessibilityValue = "\(valueText), changed from \(was)"
            } else if mode == .log, typed == nil {
                accessibilityValue = "\(valueText), suggested"
            }
        }
        return RowsField(
            field: field,
            value: valueText,
            placeholder: placeholder,
            unit: RowsText.unitText(field, measurement: measurement, unit: currentUnit, perSide: perSide, value: current),
            last: last,
            isChanged: isChanged,
            reset: reset,
            isMissing: missing,
            minus: minus,
            plus: plus,
            keypad: keypad(field, current: current, unit: currentUnit),
            accessibilityLabel: "\(spoken), \(spokenName)",
            accessibilityValue: accessibilityValue
        )
    }

    /// The steppers: the load step (the exercise's grid), 1 rep, the time step, 5 m.
    func steps(_ field: ValueField, from value: Decimal?, unit: MassUnit) -> (RowsStep?, RowsStep?) {
        let step: Decimal
        let floor: Decimal
        switch field {
        case .load, .assistance:
            step = loadStep.converted(to: unit).value.roundedTo(2)
            floor = 0
        case .reps:
            step = 1
            floor = 1
        case .duration:
            step = Decimal(timeStep)
            floor = 1
        case .distance:
            step = 5
            floor = 0
        }
        let title = RowsText.stepText(field, step)
        let spoken = RowsText.spokenStep(field, step, unit: unit)
        // With no value and nothing to start from, + sets one step and − is off.
        guard let value else {
            let first = max(step, floor)
            return (nil, RowsStep(title: "+" + title, accessibilityLabel: spoken.more,
                                  patch: FieldAccess.setting(field, to: first, unit: unit)))
        }
        let lower = value - step
        let minus = lower >= floor
            ? RowsStep(title: "\u{2212}" + title, accessibilityLabel: spoken.less,
                       patch: FieldAccess.setting(field, to: lower, unit: unit))
            : nil
        let plus = RowsStep(title: "+" + title, accessibilityLabel: spoken.more,
                            patch: FieldAccess.setting(field, to: value + step, unit: unit))
        return (minus, plus)
    }

    func keypad(_ field: ValueField, current: Decimal?, unit: MassUnit) -> RowsKeypad {
        var chips: [RowsKeypadChip] = []
        var chipsTitle: String?
        if field == .load || field == .assistance, let grid, unit == grid.unit {
            let points = grid.points(around: current.map(\.double))
            chips = points.map { point in
                let mass = Mass(suggested: point, unit: unit)
                return RowsKeypadChip(text: Format.number(mass.value),
                                      isCurrent: current.map { abs($0.double - point) < 1e-6 } ?? false,
                                      patch: FieldAccess.setting(field, to: mass.value, unit: unit))
            }
            chipsTitle = "\(Format.mass(Mass(suggested: grid.step, unit: unit))) steps, tap one to set it"
        }
        let title = switch field {
        case .load: "Load in \(unit.rawValue)"
        case .assistance: "Assistance in \(unit.rawValue)"
        case .reps: "Reps"
        case .duration: "Time, minutes and seconds"
        case .distance: "Distance in m"
        }
        return RowsKeypad(field: field, title: title, chipsTitle: chips.isEmpty ? nil : chipsTitle, chips: chips,
                          allowsDecimal: field == .load || field == .assistance || field == .distance, unit: unit)
    }

    func rpeStrip(_ rpe: RowRPE) -> RowsRPEStrip {
        let shown = rpe.shown
        let isRated = if case let .value(_, method) = rpe { method.isRating } else { false }
        let cells = RPEScaleText.values.map { value in
            let style: RowsRPECell.Style = shown == value ? (isRated ? .filled : .outlined) : .plain
            let words = RPEScaleText.reserve(value, measurement: measurement) ?? RPEScaleText.headline(value)
            let meaning = "RPE \(Format.rpe(value))" + (words.map { ": \($0.lowercasedFirst)" } ?? "")
            return RowsRPECell(value: value, text: Format.rpe(value), isHalf: value != value.roundedTo(0), style: style,
                               patch: style == .filled ? .clearingRPE : .enteringRPE(value), meaning: meaning)
        }
        let value: String = switch rpe {
        case .none: "not rated"
        case let .prefill(x): "\(Format.rpe(x)), planned, not rated yet"
        case let .value(x, .copied): "\(Format.rpe(x)), copied from last time, not rated yet"
        case let .value(x, .prefilled): "\(Format.rpe(x)), as planned, not rated"
        case let .value(x, _): Format.rpe(x)
        }
        return RowsRPEStrip(cells: cells, shown: shown, isRated: isRated, isCollapsed: shown == nil && !rpeOpen,
                            addTitle: RowsRPEStrip.addTitle, accessibilityLabel: "\(spoken), RPE", accessibilityValue: value)
    }
}

/// A load grid for the keypad's chips, in the exercise's unit.
struct RowsGrid: Equatable, Sendable {
    var unit: MassUnit
    var base: Double
    var step: Double
    /// The lowest index: an external load starts one step above a 0 base.
    var lowest: Int
    var highest: Int?

    /// Six consecutive points from one step below `value` (the lowest when none).
    func points(around value: Double?, count: Int = 6) -> [Double] {
        var first = value.map { Int((($0 - base) / step).rounded()) - 1 } ?? lowest
        if let highest { first = min(first, highest - count + 1) }
        first = max(first, lowest)
        let last = highest.map { min($0, first + count - 1) } ?? first + count - 1
        guard last >= first else { return [] }
        return (first ... last).map { (base + Double($0) * step) * 1_000_000 }.map { $0.rounded() / 1_000_000 }
    }
}

// MARK: - Field access

/// Reading and patching one value field of `SetValues` as a number.
enum FieldAccess {
    /// The field's number: a load in its own unit, reps, seconds or metres.
    static func number(_ field: ValueField, _ values: SetValues) -> Decimal? {
        switch field {
        case .load: values.load?.value
        case .assistance: values.assistance?.value
        case .reps: values.reps.map { Decimal($0) }
        case .duration: values.duration.map { Decimal($0) }
        case .distance: values.distance?.metres
        }
    }

    /// A load's unit.
    static func unit(_ field: ValueField, _ values: SetValues) -> MassUnit? {
        switch field {
        case .load: values.load?.unit
        case .assistance: values.assistance?.unit
        case .reps, .duration, .distance: nil
        }
    }

    /// Whether the two values hold different things in the field (a load in
    /// another unit of the same mass is the same).
    static func differs(_ field: ValueField, _ a: SetValues, _ b: SetValues) -> Bool {
        switch field {
        case .load: !same(a.load, b.load)
        case .assistance: !same(a.assistance, b.assistance)
        case .reps: a.reps != b.reps
        case .duration: a.duration != b.duration
        case .distance: a.distance != b.distance
        }
    }

    private static func same(_ a: Mass?, _ b: Mass?) -> Bool {
        guard let a, let b else { return a == nil && b == nil }
        return a.converted(to: .kg).value.roundedTo(4) == b.converted(to: .kg).value.roundedTo(4)
    }

    static func setting(_ field: ValueField, to value: Decimal, unit: MassUnit) -> SetValuesPatch {
        switch field {
        case .load: SetValuesPatch(load: .set(Mass(value: value, unit: unit)))
        case .assistance: SetValuesPatch(assistance: .set(Mass(value: value, unit: unit)))
        case .reps: SetValuesPatch(reps: .set(NSDecimalNumber(decimal: value.roundedTo(0)).intValue))
        case .duration: SetValuesPatch(duration: .set(NSDecimalNumber(decimal: value.roundedTo(0)).intValue))
        case .distance: SetValuesPatch(distance: .set(Length(metres: value)))
        }
    }

    static func clearing(_ field: ValueField) -> SetValuesPatch {
        switch field {
        case .load: SetValuesPatch(load: .cleared)
        case .assistance: SetValuesPatch(assistance: .cleared)
        case .reps: SetValuesPatch(reps: .cleared)
        case .duration: SetValuesPatch(duration: .cleared)
        case .distance: SetValuesPatch(distance: .cleared)
        }
    }

    /// The field as `values` hold it: set to theirs, or cleared.
    static func restoring(_ field: ValueField, from values: SetValues) -> SetValuesPatch {
        switch field {
        case .load: SetValuesPatch(load: values.load.map(FieldPatch.set) ?? .cleared)
        case .assistance: SetValuesPatch(assistance: values.assistance.map(FieldPatch.set) ?? .cleared)
        case .reps: SetValuesPatch(reps: values.reps.map(FieldPatch.set) ?? .cleared)
        case .duration: SetValuesPatch(duration: values.duration.map(FieldPatch.set) ?? .cleared)
        case .distance: SetValuesPatch(distance: values.distance.map(FieldPatch.set) ?? .cleared)
        }
    }
}

extension Decimal {
    /// Rounded half away from zero to `places` decimals.
    func roundedTo(_ places: Int) -> Decimal {
        var input = self
        var result = Decimal()
        NSDecimalRound(&result, &input, places, .plain)
        return result
    }
}

extension Character {
    var isASCIIDigit: Bool { ("0" ... "9").contains(self) }
}

extension String {
    /// The first letter lower-cased: "Could do 2 more reps" becomes "could do 2 more reps".
    var lowercasedFirst: String {
        prefix(1).lowercased() + dropFirst()
    }
}
