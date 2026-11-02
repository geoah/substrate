import Observation
import WorkoutsCore
import SwiftUI

/// The Rows layout's own view state (DESIGN.md §7.6a): which done set is
/// being fixed and its draft, which field the keypad types into, which
/// finished blocks and warm-up folds are open, the RPE meaning shown for a
/// moment, and the undo offer after a Log. The session lives in
/// `SessionController`; this never holds a rule. Every value it shows and
/// every change it sends comes from `SessionPresentation.rowsLayout`.
@MainActor
@Observable
final class RowsScreenModel {
    /// A done set open in place: the draft starts at what was logged, takes
    /// every patch, and Save sends one edit (`RowsFix.save`).
    struct Fixing: Equatable {
        var row: RowID
        var draft: SetValues
    }

    /// The keypad, open on one field of the open editor or of the fix.
    struct Typing: Equatable {
        var row: RowID
        var mode: RowsEditor.Mode
        var keypad: RowsKeypad
        /// What was typed; empty shows the field's value until the first key.
        var text = ""

        var field: ValueField { keypad.field }
    }

    /// A line shown for a moment in an editor's header: an RPE's meaning.
    struct Flash: Equatable {
        var row: RowID
        var text: String
        var token: Int
    }

    /// The offer to take back the set just logged.
    struct Undo: Equatable {
        var row: RowID
        var text: String
        var token: Int
    }

    var fixing: Fixing?
    var typing: Typing?
    /// Finished blocks the owner opened again.
    var expanded: Set<String> = []
    /// Warm-up folds the owner opened.
    var unfolded: Set<String> = []
    /// Rows whose `+ RPE` the owner tapped.
    var openStrips: Set<RowID> = []
    var flash: Flash?
    var undo: Undo?
    /// The set just logged: tinted green for a moment.
    var fresh: RowID?
    /// A Log the entry check holds: once the answer logs the row, it gets the
    /// undo offer and the green wash as any other Log.
    @ObservationIgnored private var heldLog: (row: RowID, text: String?)?
    @ObservationIgnored private var token = 0

    init(fixing: Fixing? = nil, typing: Typing? = nil) {
        self.fixing = fixing
        self.typing = typing
    }

    // MARK: - Changing values

    /// A stepper, a reset chip, a keypad chip or an RPE cell: on the open
    /// row an edit at once, on a fix a change to the draft.
    func apply(_ patch: SetValuesPatch, row: RowID, mode: RowsEditor.Mode, session: SessionController) {
        Haptics.shared.play(.step)
        switch mode {
        case .log:
            session.run(.edit(row, patch))
        case .fix:
            guard var fixing, fixing.row == row else { return }
            fixing.draft = fixing.draft.applying(patch)
            self.fixing = fixing
        }
    }

    /// An RPE cell: its patch (rate, or clear the rating), and for a moment
    /// the header says what the value means.
    func pick(_ cell: RowsRPECell, in editor: RowsEditor, session: SessionController) {
        apply(cell.patch, row: editor.row, mode: editor.mode, session: session)
        if cell.style == .filled {
            flash = nil
        } else {
            show(Flash(row: editor.row, text: cell.meaning, token: next()))
        }
    }

    // MARK: - The keypad

    func openKeypad(_ field: RowsField, in editor: RowsEditor) {
        typing = Typing(row: editor.row, mode: editor.mode, keypad: field.keypad)
    }

    func type(_ key: String) {
        guard var typing else { return }
        switch key {
        case "delete":
            if !typing.text.isEmpty { typing.text.removeLast() }
        case ".":
            guard typing.keypad.allowsDecimal, !typing.text.contains(".") else { return }
            typing.text = typing.text.isEmpty ? "0." : typing.text + "."
        default:
            guard typing.text.count < 6 else { return }
            typing.text += key
        }
        self.typing = typing
    }

    /// A chip or the confirm button: the patch, and the keypad closes.
    func enter(_ patch: SetValuesPatch, session: SessionController) {
        guard let typing else { return }
        apply(patch, row: typing.row, mode: typing.mode, session: session)
        self.typing = nil
    }

    // MARK: - Actions

    /// Runs a Rows action: Log and Stop and log, a fix's Save, Delete set, the rest.
    func perform(_ action: RowsAction, session: SessionController, cover: CoverModel, loggedText: String? = nil) {
        Task { await performNow(action, session: session, cover: cover, loggedText: loggedText) }
    }

    func performNow(_ action: RowsAction, session: SessionController, cover: CoverModel, loggedText: String?) async {
        switch action {
        case let .tick(row), let .stopTimedSet(row):
            typing = nil
            heldLog = nil
            await session.send(action.command)
            if session.state?.rows[row]?.isPending == false {
                logged(row, text: loggedText)
            } else if session.entryCheck?.row == row {
                // The entry check asks first: "Keep" or "Use corrected" logs it later.
                heldLog = (row, loggedText)
            }
        case let .edit(row, _) where fixing?.row == row:
            await session.send(action.command)
            fixing = nil
            typing = nil
        case let .removeSet(row) where session.state?.rows[row]?.isPending == false:
            // A done set asks first, as the Table's does.
            fixing = nil
            typing = nil
            cover.removeDoneRow = row
        case .touch:
            fixing = nil
            typing = nil
            await session.send(action.command)
        default:
            await session.send(action.command)
        }
    }

    /// A done row tapped: open its fix on what was logged.
    func startFix(_ item: RowsSetItem) {
        guard let fix = item.fix else { return }
        typing = nil
        fixing = Fixing(row: item.id, draft: fix.logged)
    }

    func cancelFix() {
        fixing = nil
        if typing?.mode == .fix { typing = nil }
    }

    func undoLast(session: SessionController) {
        guard let undo else { return }
        self.undo = nil
        if fresh == undo.row { fresh = nil }
        session.run(.untick(undo.row))
    }

    /// Drops what no longer matches the session: a fix of a row that is no
    /// longer done, a keypad on a row no longer open. A Log the entry check
    /// held is offered for undo once its answer logged the row, and dropped
    /// when the answer was Edit.
    func reconcile(with layout: RowsLayout, entryCheckRow: RowID?) {
        if let held = heldLog {
            if layout.item(held.row)?.state == .done {
                heldLog = nil
                logged(held.row, text: held.text)
            } else if entryCheckRow != held.row {
                heldLog = nil
            }
        }
        if let fixing, layout.item(fixing.row)?.state != .done {
            self.fixing = nil
        }
        if let typing {
            switch typing.mode {
            case .log where layout.item(typing.row)?.editor == nil: self.typing = nil
            case .fix where fixing?.row != typing.row: self.typing = nil
            default: break
            }
        }
        if let undo, layout.item(undo.row)?.state != .done {
            self.undo = nil
        }
    }

    // MARK: - Moments

    private func logged(_ row: RowID, text: String?) {
        let token = next()
        fresh = row
        undo = text.map { Undo(row: row, text: $0, token: token) }
        Task { [weak self] in
            try? await Task.sleep(for: .seconds(5))
            guard let self else { return }
            if self.undo?.token == token { self.undo = nil }
            if self.fresh == row { self.fresh = nil }
        }
    }

    private func show(_ flash: Flash) {
        self.flash = flash
        Task { [weak self] in
            try? await Task.sleep(for: .seconds(2.5))
            if self?.flash?.token == flash.token { self?.flash = nil }
        }
    }

    private func next() -> Int {
        token += 1
        return token
    }
}
