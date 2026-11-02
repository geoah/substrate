import Observation
import WorkoutsCore
import SwiftUI
import UIKit

/// The session cover's own state: what is typed but not yet sent, which
/// sheet or question is up. The session itself lives in `SessionController`;
/// this never holds a rule.
@MainActor
@Observable
final class CoverModel {
    /// One value field of the grid.
    struct Field: Hashable {
        var row: RowID
        var column: GridColumn
    }

    /// The sheets the cover presents, one at a time.
    enum Sheet: Identifiable {
        case rpe(RowID)
        case addExercise(after: String?)
        case replaceExercise(slot: String)
        case reorder
        case rest(RestTarget)
        case note(NoteEdit)
        /// The Finish sheet, opened with the dialog's choice for unticked rows.
        case finish(PendingSetPolicy)
        /// "About set types…" from a row's set-kind menu.
        case setTypes

        var id: String {
            switch self {
            case let .rpe(row): "rpe-\(row.rawValue)"
            case let .addExercise(after): "add-\(after ?? "")"
            case let .replaceExercise(slot): "replace-\(slot)"
            case .reorder: "reorder"
            case let .rest(target): "rest-\(target.id)"
            case let .note(edit): "note-\(edit.id)"
            case let .finish(policy): "finish-\(policy.rawValue)"
            case .setTypes: "set-types"
            }
        }
    }

    /// What a note sheet writes to.
    enum NoteEdit {
        case session
        case slot(String)
        case row(RowID)

        var id: String {
            switch self {
            case .session: "session"
            case let .slot(key): "slot-\(key)"
            case let .row(row): "row-\(row.rawValue)"
            }
        }
    }

    var sheet: Sheet?
    /// Remove exercise, when sets are done: the slot and its question.
    var removeExercise: (slot: String, question: String)?
    /// A done row to remove after the owner says so.
    var removeDoneRow: RowID?
    var askDiscard = false
    /// Finish's first step when rows are unticked (§7.7).
    var askUnticked = false
    /// The dialog's words, worked out once when Finish is tapped: the summary
    /// for "Mark them done as shown", which says how many cannot be.
    var untickedPrompt: FinishSummary?
    /// Typed text not yet sent as `editSet`.
    private(set) var drafts: [Field: String] = [:]
    /// Text sent and waiting for the presentation to show it, so the field
    /// never flickers back to the old value.
    private var inFlight: [Field: String] = [:]
    /// The edits on their way, by field.
    @ObservationIgnored private var sending: [Field: Task<Void, Never>] = [:]

    /// The text a field shows: the draft, else what was just sent, else the
    /// row's value.
    func text(_ field: Field, value: String?) -> String {
        drafts[field] ?? inFlight[field] ?? value ?? ""
    }

    /// What the field shows in place of the row's value, when anything: the
    /// draft, else the text just sent. Empty when a value was cleared.
    func draftText(_ field: Field) -> String? {
        drafts[field] ?? inFlight[field]
    }

    func setDraft(_ text: String, for field: Field) {
        drafts[field] = text
    }

    func hasDraft(_ field: Field) -> Bool {
        drafts[field] != nil
    }

    func hasDraft(forRow row: RowID) -> Bool {
        drafts.keys.contains { $0.row == row && !(drafts[$0] ?? "").isEmpty }
    }

    /// Sends a field's draft as an edit and waits for it, or waits for the
    /// edit already on its way, so a tick never overtakes a typed value.
    func commit(_ field: Field, session: SessionController) async {
        guard let text = drafts.removeValue(forKey: field) else {
            await sending[field]?.value
            return
        }
        guard let presentation = session.presentation, let row = presentation.row(field.row),
              let card = presentation.card(row.slot), let valueField = field.column.field
        else { return }
        let current = row.cell(field.column)
        let shown = current?.isGhost == false ? current?.entered ?? current?.text : nil
        let trimmed = text.trimmingCharacters(in: .whitespaces)
        // Nothing changed: no event.
        if trimmed == (shown ?? "") { return }
        // Text that is not a value is dropped; the field shows the row again.
        guard let patch = GridValue.patch(trimmed, field: valueField, unit: card.unit) else { return }
        inFlight[field] = text
        let task = Task { await session.send(.edit(field.row, patch)) }
        sending[field] = task
        await task.value
        if sending[field] == task { sending[field] = nil }
        if inFlight[field] == text { inFlight[field] = nil }
    }

    /// Sends every draft of a row, and waits for its edits on their way,
    /// before its tick commits what it shows.
    func commitRow(_ row: RowID, session: SessionController) async {
        let fields = Set(drafts.keys.filter { $0.row == row } + sending.keys.filter { $0.row == row })
        for field in fields {
            await commit(field, session: session)
        }
    }
}

/// What a rest picker sets.
enum RestTarget {
    case slot(String, SlotRestKind)
    case row(RowID)
    case roundRest(group: String)
    /// The toolbar's standalone timer.
    case timer

    var id: String {
        switch self {
        case let .slot(key, kind): "slot-\(key)-\(kind.rawValue)"
        case let .row(row): "row-\(row.rawValue)"
        case let .roundRest(group): "round-\(group)"
        case .timer: "timer"
        }
    }

    /// The command for a value; nil goes back to the default.
    func command(_ seconds: Seconds?) -> SessionCommand {
        switch self {
        case let .slot(key, kind): .setSlotRest(slot: key, kind: kind, seconds: seconds)
        case let .row(row): .setRowRest(row, seconds)
        case let .roundRest(group): .setGroup(SessionEvent.SetGroup(group: group, roundRest: seconds.map { .set($0) } ?? .cleared))
        case .timer: .startTimer(seconds ?? 0)
        }
    }
}

/// Reading what the owner types into a cell. The keypad is the system
/// decimal pad, so a comma may stand for the point.
enum GridValue {
    /// A patch setting `field` to `text`, or clearing it when empty; nil for
    /// text that is not a value.
    static func patch(_ text: String, field: ValueField, unit: MassUnit) -> SetValuesPatch? {
        var patch = SetValuesPatch()
        if text.isEmpty {
            switch field {
            case .load: patch.load = .cleared
            case .assistance: patch.assistance = .cleared
            case .reps: patch.reps = .cleared
            case .duration: patch.duration = .cleared
            case .distance: patch.distance = .cleared
            }
            return patch
        }
        switch field {
        case .load:
            guard let value = decimal(text), value >= 0 else { return nil }
            patch.load = .set(Mass(value: value, unit: unit))
        case .assistance:
            guard let value = decimal(text), value >= 0 else { return nil }
            patch.assistance = .set(Mass(value: value, unit: unit))
        case .reps:
            guard let value = Int(text), value >= 0 else { return nil }
            patch.reps = .set(value)
        case .duration:
            guard let value = seconds(text) else { return nil }
            patch.duration = .set(value)
        case .distance:
            guard let value = decimal(text), value >= 0 else { return nil }
            patch.distance = .set(Length(metres: value))
        }
        return patch
    }

    static func decimal(_ text: String) -> Decimal? {
        // A ghost in another unit reads "83.91 kg": the number is enough.
        let number = text.replacingOccurrences(of: ",", with: ".").split(separator: " ").first.map(String.init) ?? ""
        return DecimalText.parse(number)
    }

    /// `1:07`, or digits read from the right as minutes and seconds the way
    /// a timer's keypad fills (`107` is 1:07, `45` is 0:45, `90` is 1:30).
    static func seconds(_ text: String) -> Seconds? {
        let parts = text.split(separator: ":", omittingEmptySubsequences: false)
        if parts.count == 2 || parts.count == 3 {
            let numbers = parts.map { Int($0.isEmpty ? "0" : $0) }
            guard numbers.allSatisfy({ $0 != nil }) else { return nil }
            return numbers.compactMap { $0 }.reduce(0) { $0 * 60 + $1 }
        }
        let digits = text.filter(\.isNumber)
        guard !digits.isEmpty, digits.count == text.count, let value = Int(digits) else { return nil }
        if digits.count <= 2 { return value }
        return value / 100 * 60 + value % 100
    }

    /// The value a field shows (typed or ghost) moved by `step`, as text.
    static func stepped(_ text: String, field: ValueField, by step: Decimal) -> String? {
        switch field {
        case .duration:
            let current = seconds(text) ?? 0
            let moved = max(0, current + NSDecimalNumber(decimal: step).intValue)
            return Format.duration(moved)
        case .reps:
            let current = Int(text.filter(\.isNumber)) ?? 0
            return String(max(0, current + NSDecimalNumber(decimal: step).intValue))
        case .load, .assistance, .distance:
            let current = decimal(text) ?? 0
            return Format.number(max(0, current + step))
        }
    }

    /// The keypad a field gets.
    static func keyboard(for field: ValueField) -> UIKeyboardType {
        switch field {
        case .reps, .duration: .numberPad
        case .load, .assistance, .distance: .decimalPad
        }
    }
}
