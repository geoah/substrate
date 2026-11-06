import Foundation
import SubstrateCore

// The editor open in place on the Rows layout: the set to do now (`log`), or
// a done set being fixed (`fix`). Every control carries the `SetValuesPatch`
// it applies, so the view never works a value out:
//
// - `log`: send `.edit(row, patch)`; the presentation comes back with the change.
// - `fix`: apply it to the draft, `draft = draft.applying(patch)`, and present
//   again with `RowsFix.editor(draft:)`; Save sends `RowsFix.save(draft)`.

/// The open editor of one row.
public struct RowsEditor: Codable, Equatable, Sendable {
    public enum Mode: String, Codable, Equatable, Sendable {
        /// The set to do now: patches go to the engine at once, Log ticks.
        case log
        /// A done set: patches go to a draft the view holds, Save sends them.
        case fix
    }

    /// The panel's colour.
    public enum Tint: String, Codable, Equatable, Sendable {
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
    /// The set as said aloud: `Set 2`, `Warm-up 1`, `Drop`; in a group `Bird Dog, set 1`.
    public var title: String
    /// The set's small label, as its line shows it: `W`, `2`, `2a`.
    public var setLabel: String
    /// Beside the label: the exercise in a group, `Drop`, `Mini-set 3`; nil for a plain set.
    public var caption: String?
    /// The header line after the title: the suggestion's reason, `after the rest`,
    /// `of 2` (in a group `warm-up 1 of 2`), `to failure`, `at 6 kg`, `holding`,
    /// `logged at 09:28`.
    public var note: RowsNote?
    /// On the header's right: `no rest` on a drop.
    public var trailingNote: String?
    /// The suggestion's text, whatever `note` shows.
    public var reason: String?
    /// Load and reps, or the time, or distance and load, in the grid's order.
    public var fields: [RowsField]
    /// Reps left and why the set ended; nil for a row that takes none.
    public var effort: RowsEffort?
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
    /// The routine slot's one-line cue, beside the set being done:
    /// `Controlled return, pause without bouncing`.
    public var cue: String?

    public init(
        row: RowID, mode: Mode, tint: Tint, title: String, setLabel: String, caption: String?, note: RowsNote?, trailingNote: String?,
        reason: String?, fields: [RowsField], effort: RowsEffort?, timer: RowsTimer?, primary: RowsButton,
        secondary: RowsButton?, delete: RowsButton?, cancelTitle: String?, cue: String? = nil
    ) {
        self.row = row
        self.mode = mode
        self.tint = tint
        self.title = title
        self.setLabel = setLabel
        self.caption = caption
        self.note = note
        self.trailingNote = trailingNote
        self.reason = reason
        self.fields = fields
        self.effort = effort
        self.timer = timer
        self.primary = primary
        self.secondary = secondary
        self.delete = delete
        self.cancelTitle = cancelTitle
        self.cue = cue
    }

    /// The undo offer's words after a Log: `Bird Dog, set 1 logged`, `Set 2 logged`.
    public var loggedText: String {
        "\(title) logged"
    }

    public func field(_ field: ValueField) -> RowsField? {
        fields.first { $0.field == field }
    }
}

/// The editor's header note.
public struct RowsNote: Codable, Equatable, Sendable {
    public enum Kind: String, Codable, Equatable, Sendable {
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
public struct RowsField: Codable, Equatable, Sendable, Identifiable {
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

    public init(
        field: ValueField, value: String?, placeholder: String?, unit: String?, last: String?, isChanged: Bool,
        reset: RowsReset?, isMissing: Bool, minus: RowsStep?, plus: RowsStep?, keypad: RowsKeypad,
        accessibilityLabel: String, accessibilityValue: String
    ) {
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
public struct RowsStep: Codable, Equatable, Sendable {
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

    /// The compact stepper's glyph: the sign alone, `−` or `+`.
    public var symbol: String {
        String(title.prefix(1))
    }
}

/// The reset chip: `↺ 8` back to the suggestion, `↺ was 8` back to what was logged.
public struct RowsReset: Codable, Equatable, Sendable {
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
public struct RowsKeypad: Codable, Equatable, Sendable {
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

    public init(
        field: ValueField, title: String, chipsTitle: String?, chips: [RowsKeypadChip], allowsDecimal: Bool,
        unit: MassUnit
    ) {
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

    /// The field back as `values` hold it, set or cleared: what Cancel sends
    /// after the typed text was applied, and what a text that is no value
    /// (all deleted, `1.2.`) stands for while typing.
    public func restoring(_ values: SetValues) -> SetValuesPatch {
        FieldAccess.restoring(field, from: values)
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
public struct RowsKeypadChip: Codable, Equatable, Sendable, Identifiable {
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

/// Reps left, built like the load and reps fields: the number, `reps left`,
/// last time, a reset chip, and `−1` / `+1` under the thumb. 0 to 4, where 4
/// reads `4+` (RPE 6, "could do 4 or more"); on the RIR-based scale every set
/// is logged on, the value is `rpe` = 10 − reps left, never converted. The
/// plan's value is pre-filled hollow, not yet a rating: a step or a tap on the
/// number rates it, and the reset chip goes back to it. Beside it, why the
/// set ended (`stop`).
///
/// It holds the values its patches are made of, not the patches: an editor
/// and its line are built and copied on the presenter's stack many times
/// over, and a patch per control made the frames outgrow a thread's stack.
public struct RowsEffort: Codable, Equatable, Sendable {
    /// `2`, `1–2`, `4+`; nil with no value and nothing pre-filled.
    public var value: String?
    /// The RPE the number stands for, rated or not.
    public var shown: Decimal?
    /// `left`, after the number as `kg` and `reps` are; `effort` on a timed or
    /// distance set, where the value is the scale's word.
    public var unit: String
    /// `shown` is the owner's rating (a step, a tap, or an imported value):
    /// drawn solid. Otherwise hollow, as the plan's pre-fill or a copy.
    public var isRated: Bool
    /// `last 2`, after the unit; nil with no rating last time.
    public var last: String?
    /// The reset chip: back to the plan's value, or to what was logged when fixing.
    public var back: Back?
    /// The RPE `−1` and `+1` rate; nil past the ends (0 and 4+).
    public var fewer: Decimal?
    public var more: Decimal?
    /// Hidden behind `addTitle` until the owner opens it (view state): no
    /// value, no pre-fill and no rating last time, or a warm-up or timed set.
    public var isCollapsed: Bool
    /// `Reps left`, after a plus symbol.
    public var addTitle: String
    /// Why the set ended; nil on a warm-up.
    public var stop: RowsStopMenu?
    /// What the header says for a moment after a step: `2 left: hard`.
    public var meaning: String?
    /// `Leg Press (Machine), set 2, reps left`.
    public var accessibilityLabel: String
    /// `2, planned, not rated yet`, `1`, `not rated`.
    public var accessibilityValue: String
    /// A hold or a distance, where reps mean nothing: the value is the
    /// scale's word (`Hard`) and the steps say Easier and Harder.
    public var saysWords: Bool = false

    /// Where the reset chip goes.
    public enum Back: Codable, Equatable, Sendable {
        /// The plan's value, unrated, shown as `↺ 2`.
        case plan(Decimal)
        /// No value: a rating where the plan names none.
        case clear
        /// What a fixed set was logged with, `↺ was 2`; nil when it had none.
        case logged(Decimal?, EffortMethod?)
    }

    public init(
        value: String?, shown: Decimal?, unit: String, isRated: Bool, last: String?, back: Back?, fewer: Decimal?,
        more: Decimal?, isCollapsed: Bool, addTitle: String, stop: RowsStopMenu?, meaning: String?,
        accessibilityLabel: String, accessibilityValue: String
    ) {
        self.value = value
        self.shown = shown
        self.unit = unit
        self.isRated = isRated
        self.last = last
        self.back = back
        self.fewer = fewer
        self.more = more
        self.isCollapsed = isCollapsed
        self.addTitle = addTitle
        self.stop = stop
        self.meaning = meaning
        self.accessibilityLabel = accessibilityLabel
        self.accessibilityValue = accessibilityValue
    }

    public static let addTitle = "Reps left"
    /// The most reps left the control says: 4 reads `4+`.
    public static let most: Decimal = 4

    /// `−1`: one rep left fewer, rated; in words, `Easier`, a step down the scale.
    public var minus: RowsStep? {
        guard !saysWords else { return more.map { RowsStep(title: "Easier", accessibilityLabel: "Easier", patch: .enteringRPE($0)) } }
        return fewer.map { RowsStep(title: "\u{2212}1", accessibilityLabel: "1 rep left fewer", patch: .enteringRPE($0)) }
    }

    /// `+1`: one rep left more, rated; in words, `Harder`, a step up the scale.
    public var plus: RowsStep? {
        guard !saysWords else { return fewer.map { RowsStep(title: "Harder", accessibilityLabel: "Harder", patch: .enteringRPE($0)) } }
        return more.map { RowsStep(title: "+1", accessibilityLabel: "1 rep left more", patch: .enteringRPE($0)) }
    }

    /// Clear: no value, rated or pre-filled, until a step (a long press on
    /// the number, the strip's tap on the filled stop before it); nil with
    /// nothing shown.
    public var clear: RowsStep? {
        shown.map { _ in
            RowsStep(title: saysWords ? "Clear the effort" : "Clear reps left", accessibilityLabel: "Clear", patch: .clearingRPE)
        }
    }

    /// The value as the row says it: `2`, `4+`, or in words `Hard`.
    public func text(_ rpe: Decimal) -> String {
        saysWords ? RPEScaleText.headline(rpe) ?? Self.number(rpe) : Self.number(rpe)
    }

    /// A tap on the hollow number rates it as shown; nil once rated.
    public var rate: SetValuesPatch? {
        isRated ? nil : shown.map(SetValuesPatch.enteringRPE)
    }

    /// The reset chip and its patch.
    public var reset: RowsReset? {
        switch back {
        case nil:
            return nil
        case let .plan(rpe):
            let text = text(rpe)
            let said = saysWords ? text : "\(text) left"
            return RowsReset(text: text, accessibilityLabel: "Back to \(said), as planned", patch: .restoringRPEPrefill)
        case .clear:
            return RowsReset(
                text: "clear", accessibilityLabel: saysWords ? "Clear the effort" : "Clear reps left", patch: .clearingRPE)
        case let .logged(rpe, method):
            let was = rpe.map(text) ?? "empty"
            let patch = rpe.map { SetValuesPatch(rpe: .set($0), rpeMethod: method) } ?? .clearingRPE
            return RowsReset(text: "was \(was)", accessibilityLabel: "Back to \(was), as logged", patch: patch)
        }
    }
}

/// Why the set ended, a menu beside reps left: four reasons and none.
public struct RowsStopMenu: Codable, Equatable, Sendable {
    /// `Why stop?` with none chosen, else the reason's word: `Form`, `Pain`.
    public var title: String
    public var selected: StopReason?
    /// `Leg Press (Machine), set 2, why the set ended`.
    public var accessibilityLabel: String
    /// `Form broke down`, `not said`.
    public var accessibilityValue: String

    public init(title: String, selected: StopReason?, accessibilityLabel: String, accessibilityValue: String) {
        self.title = title
        self.selected = selected
        self.accessibilityLabel = accessibilityLabel
        self.accessibilityValue = accessibilityValue
    }

    public static let title = "Why stop?"

    /// The three reasons, the chosen one marked; a tap on it takes it back.
    public var options: [RowsStopOption] {
        StopReason.allCases.map { reason in
            RowsStopOption(
                reason: reason, title: StopReasonText.title(reason), isSelected: reason == selected,
                patch: .stopping(reason == selected ? nil : reason))
        }
    }

    /// `Clear reason`, which clears a chosen one; nil with none chosen.
    public var clear: SetValuesPatch? {
        selected == nil ? nil : .stopping(nil)
    }
}

/// One reason in the stop menu.
public struct RowsStopOption: Codable, Equatable, Sendable, Identifiable {
    public var reason: StopReason
    /// `Muscular fatigue`, `Form broke down`, `Pain or discomfort`, `Other reason`.
    public var title: String
    public var isSelected: Bool
    /// The reason chosen; on the chosen one, no reason.
    public var patch: SetValuesPatch
    public var id: StopReason { reason }

    public init(reason: StopReason, title: String, isSelected: Bool, patch: SetValuesPatch) {
        self.reason = reason
        self.title = title
        self.isSelected = isSelected
        self.patch = patch
    }
}

/// A timed row's clock and its track: 0:00 on the left, last time below the
/// track and the goal above it, so the labels never collide.
public struct RowsTimer: Codable, Equatable, Sendable {
    public enum Phase: Codable, Equatable, Sendable {
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
public struct RowsFix: Codable, Equatable, Sendable {
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
        var editor = source.editor(
            values: draft, rpe: draft.rpe.map { .value($0, draft.rpeMethod ?? .entered) } ?? .none,
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
        patch.stopReason = field(draft.stopReason, logged.stopReason)
        return changed ? patch : nil
    }
}

// MARK: - Building

/// Everything an editor needs that does not change while it is open: the row,
/// its words, the suggestion or what was logged, last time, the steps and the grid.
struct RowsEditorSource: Codable, Equatable, Sendable {
    var row: RowID
    var mode: RowsEditor.Mode
    var tint: RowsEditor.Tint
    var title: String
    var setLabel: String
    var caption: String?
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
    var previous: SetValues?
    var loadStep: Mass
    var grid: RowsGrid?
    var timeStep: Seconds
    /// Reps left exists; with `rpeOpen` it shows with no value.
    var rpeOffered: Bool
    var rpeOpen: Bool
    /// The plan's effort for the row (`SetTarget.effortTop`), which the
    /// reset chip goes back to.
    var plannedRPE: Decimal?
    /// The row says why it ended: a working set, not a warm-up.
    var offersStop: Bool
    /// The routine slot's cue.
    var cue: String?
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
        let effort = rpeOffered ? effort(rpe, values: values) : nil
        let primary: RowsButton
        var secondary = secondary
        var delete: RowsButton?
        let shown = shownValues(values)
        let shownEffort = commit?.rpe ?? rpe.shown
        // A mini-set's editor holds its reps alone; its load is the activation's.
        let repsAlone = fields == [.reps] && measurement.loadMode != nil && measurement.loadMode != .bodyweight
        func text(_ values: SetValues) -> String {
            repsAlone
                ? RowsText.values(SetValues(reps: values.reps), measurement: .repsonly, perSide: perSide)
                : RowsText.values(values, measurement: measurement, perSide: perSide)
        }
        switch mode {
        case .fix:
            let text = text(commit ?? shown)
            primary = RowsButton(
                title: "Save" + (text.isEmpty ? "" : " \(text)") + RowsText.effortSuffix(shownEffort, measurement: measurement),
                action: .edit(row, SetValuesPatch()), isEnabled: canCommit)
            delete = RowsButton(title: "Delete set", accessibilityLabel: "Delete \(spoken)", action: .removeSet(row))
        case .log:
            let text = text(commit ?? shown)
            let logTitle = RowsText.logTitle(prefix: logPrefix, values: text, rpe: shownEffort, measurement: measurement)
            if timer != nil, measurement.hasTimer {
                if isRunning {
                    primary = RowsButton(
                        title: "Stop and log", accessibilityLabel: "Stop and log \(spoken)",
                        action: .stopTimedSet(row), isEnabled: canCommit)
                    secondary = RowsButton(title: "Cancel", accessibilityLabel: "Cancel the timer", action: .cancelTimedSet(row))
                } else {
                    let goal = timer?.goal.map { ", goal \(Format.duration($0))" } ?? ""
                    primary = RowsButton(
                        title: "Start", accessibilityLabel: "Start \(spoken)\(goal)",
                        action: .startTimedSet(row), isEnabled: canStart)
                    secondary = RowsButton(title: logTitle, action: .tick(row), isEnabled: canCommit)
                }
            } else {
                primary = RowsButton(title: logTitle, action: .tick(row), isEnabled: canCommit)
            }
        }
        return RowsEditor(
            row: row, mode: mode, tint: tint, title: title, setLabel: setLabel, caption: caption, note: note,
            trailingNote: trailingNote, reason: reason, fields: fieldPresentations, effort: effort, timer: timer,
            primary: primary,
            secondary: secondary, delete: delete, cancelTitle: mode == .fix ? "Cancel" : nil,
            cue: mode == .log ? cue : nil)
    }

    /// The values the editor shows: typed over the suggestion (`log`), the draft (`fix`).
    func shownValues(_ values: SetValues) -> SetValues {
        guard mode == .log else { return values }
        var shown = values
        for field in fields where FieldAccess.number(field, values) == nil {
            if field == .reps, repsPlaceholder != nil { continue }
            shown = shown.applying(FieldAccess.restoring(field, from: baseline))
        }
        return shown
    }

    func field(_ field: ValueField, values: SetValues, missing: Bool) -> RowsField {
        let typed = FieldAccess.number(field, values)
        let base = FieldAccess.number(field, baseline)
        // "To failure" is no suggestion to go back to: it is never logged.
        let baselineIsHint = mode == .log && field == .reps && repsPlaceholder != nil
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
                let text =
                    base.map {
                        RowsText.fieldText(
                            field, $0, unit: FieldAccess.unit(field, baseline) ?? unit,
                            withUnit: false)
                    } ?? "empty"
                reset = RowsReset(
                    text: "was \(text)", accessibilityLabel: "Back to \(text), as logged",
                    patch: FieldAccess.restoring(field, from: baseline))
            }
        }
        // Words (`to failure`) do not show in the empty field: a field holds
        // a number, and the header says it once (owner, 2026-11-02).
        // VoiceOver still hears them with the empty field. A `rirstop` set's
        // approximate reps are no placeholder: they pre-fill as planned.
        var words: String?
        if current == nil, mode == .log, field == .reps, let repsPlaceholder {
            words = repsPlaceholder
        }
        let previousValue = previous.flatMap { FieldAccess.number(field, $0) }
        let previousUnit = previous.flatMap { FieldAccess.unit(field, $0) } ?? unit
        let last = previousValue.map { "last \(RowsText.fieldText(field, $0, unit: previousUnit, withUnit: false))" }
        // Empty: the steppers start from last time, else the hint.
        let seed = current ?? previousValue ?? (hinted ? base : nil)
        let (minus, plus) = steps(field, from: seed, unit: currentUnit)
        let spokenName = RowsText.spokenField(field, measurement: measurement, unit: unit)
        let valueText = current.map { RowsText.fieldText(field, $0, unit: currentUnit, withUnit: false) }
        var accessibilityValue = valueText ?? words.map { "\($0), empty" } ?? (missing ? "required, empty" : "empty")
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
            placeholder: nil,
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
            return (
                nil,
                RowsStep(
                    title: "+" + title, accessibilityLabel: spoken.more,
                    patch: FieldAccess.setting(field, to: first, unit: unit))
            )
        }
        let lower = value - step
        let minus =
            lower >= floor
            ? RowsStep(
                title: "\u{2212}" + title, accessibilityLabel: spoken.less,
                patch: FieldAccess.setting(field, to: lower, unit: unit))
            : nil
        let plus = RowsStep(
            title: "+" + title, accessibilityLabel: spoken.more,
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
                return RowsKeypadChip(
                    text: Format.number(mass.value),
                    isCurrent: current.map { abs($0.double - point) < 1e-6 } ?? false,
                    patch: FieldAccess.setting(field, to: mass.value, unit: unit))
            }
            chipsTitle = "\(Format.mass(Mass(suggested: grid.step, unit: unit))) steps, tap one to set it"
        }
        let title =
            switch field {
            case .load: "Load in \(unit.rawValue)"
            case .assistance: "Assistance in \(unit.rawValue)"
            case .reps: "Reps"
            case .duration: "Time, minutes and seconds"
            case .distance: "Distance in m"
            }
        return RowsKeypad(
            field: field, title: title, chipsTitle: chips.isEmpty ? nil : chipsTitle, chips: chips,
            allowsDecimal: field == .load || field == .assistance || field == .distance, unit: unit)
    }
}

extension RowsEditorSource {
    /// Reps left for the row: its value rated or not, the steps from it (from
    /// last time's, else 2, when empty), the reset chip and why it ended.
    func effort(_ rpe: RowRPE, values: SetValues) -> RowsEffort {
        let shown = rpe.shown
        let isRated = if case let .value(_, method) = rpe { method.isRating } else { false }
        let last = previous.flatMap { $0.rpe }
        let left = shown.map { max(0, 10 - $0) }
        let seed = left ?? last.map { max(0, 10 - $0) } ?? 2
        func rated(_ newLeft: Decimal) -> Decimal? {
            newLeft >= 0 && newLeft <= RowsEffort.most ? 10 - newLeft : nil
        }
        // From a half point, a step lands on the whole count beside it.
        let whole = seed.roundedTo(0) == seed
        let fewer = left == nil || whole ? seed - 1 : (seed - Decimal(5) / 10).roundedTo(0)
        let more = left == nil || whole ? seed + 1 : (seed + Decimal(5) / 10).roundedTo(0)
        var back: RowsEffort.Back?
        switch mode {
        case .log:
            if isRated, let shown, let plannedRPE, shown != plannedRPE {
                back = .plan(plannedRPE)
            } else if isRated, plannedRPE == nil {
                back = .clear
            }
        case .fix:
            if values.rpe != baseline.rpe || (values.rpe != nil && values.rpeMethod != baseline.rpeMethod) {
                back = .logged(baseline.rpe, baseline.rpeMethod)
            }
        }
        let words = !measurement.recordsReps
        func said(_ rpe: Decimal) -> String {
            words ? RPEScaleText.headline(rpe) ?? RowsEffort.number(rpe) : RowsEffort.number(rpe)
        }
        let value: String =
            switch rpe {
            case .none: "not rated"
            case let .prefill(x): "\(said(x)), planned, not rated yet"
            case let .value(x, .copied): "\(said(x)), copied from last time, not rated yet"
            case let .value(x, .prefilled): "\(said(x)), as planned, not rated"
            case let .value(x, _): said(x)
            }
        var effort = RowsEffort(
            value: shown.map(said),
            shown: shown,
            // `left`, as the Log button says it: the row fits its two buttons beside the steps.
            unit: words ? "effort" : "left",
            isRated: isRated,
            last: last.map { "last \(words ? said($0).lowercased() : said($0))" },
            back: back,
            fewer: rated(fewer),
            more: rated(more),
            isCollapsed: shown == nil && !rpeOpen,
            addTitle: words ? "Effort" : RowsEffort.addTitle,
            stop: offersStop ? stopMenu(values.stopReason) : nil,
            meaning: shown.map { words ? said($0) : RowsEffort.meaning($0) },
            accessibilityLabel: "\(spoken), \(words ? "effort" : "reps left")",
            accessibilityValue: value)
        effort.saysWords = words
        return effort
    }

    func stopMenu(_ selected: StopReason?) -> RowsStopMenu {
        RowsStopMenu(
            title: selected.map(StopReasonText.short) ?? RowsStopMenu.title,
            selected: selected,
            accessibilityLabel: "\(spoken), why the set ended",
            accessibilityValue: selected.map(StopReasonText.title) ?? "not said")
    }
}

extension RowsEffort {
    /// The RPE one rep left fewer (`fewer`) or more than `rpe`, inside 0 to
    /// 4+ reps left; from a half point, the whole count beside it; from no
    /// value, one either side of 2 left. Nil past the ends.
    public static func stepped(_ rpe: Decimal?, fewer: Bool) -> Decimal? {
        let left = rpe.map { max(0, 10 - $0) } ?? 2
        let whole = rpe == nil || left.roundedTo(0) == left
        let half = Decimal(5) / 10
        let next = fewer ? (whole ? left - 1 : (left - half).roundedTo(0)) : (whole ? left + 1 : (left + half).roundedTo(0))
        return next >= 0 && next <= most ? 10 - next : nil
    }

    /// `2`, `1–2`, and `4+` for 4: the control's top stands for "4 or more".
    public static func number(_ rpe: Decimal) -> String {
        max(0, 10 - rpe) == most ? "4+" : Format.repsLeftNumber(rpe)
    }

    /// What the header says for a moment after a step: `2 left: hard`,
    /// `0 left: maximum effort`, `4+ left: moderate effort`.
    public static func meaning(_ rpe: Decimal) -> String {
        "\(number(rpe)) left" + (RPEScaleText.headline(rpe).map { ": \($0.lowercasedFirst)" } ?? "")
    }
}

/// A load grid for the keypad's chips, in the exercise's unit.
struct RowsGrid: Codable, Equatable, Sendable {
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
