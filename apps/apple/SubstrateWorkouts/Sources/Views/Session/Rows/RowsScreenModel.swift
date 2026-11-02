import Observation
import WorkoutsCore
import SwiftUI

/// The Rows layout's own view state (DESIGN.md §7.6a): which done set is
/// being fixed and its draft, which field the keypad types into, which
/// finished blocks and warm-up folds are open, the RPE meaning shown for a
/// moment, the undo offer after a Log, an untick or a delete, the set a
/// "Then" tap marks, the island's RPE picker, and what the list reports for
/// the island's dock (`RowsDock` decides it). The session lives in
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
        /// Opened from the island's docked set: the list stays where the owner scrolled it.
        var fromIsland = false

        var field: ValueField { keypad.field }
    }

    /// A line shown for a moment in an editor's header: an RPE's meaning.
    struct Flash: Equatable {
        var row: RowID
        var text: String
        var token: Int
    }

    /// The offer to take back what was just done to a set.
    struct Undo: Equatable {
        enum Kind: Equatable {
            /// Logged: the Undo takes it back and opens it (`RowsAction.takeBack`).
            case logged
            /// Unticked: the Undo logs it again with its values.
            case unticked
            /// Deleted: the removal is still held (`SessionController.pendingRemoval`); the Undo keeps the set.
            case deleted
        }

        var row: RowID
        var text: String
        var token: Int
        var kind: Kind = .logged
    }

    /// A set a "Then" tap took the list to, marked for a moment.
    struct Mark: Equatable {
        var row: RowID
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
    /// The set a "Then" tap took the list to.
    var mark: Mark?
    /// The island's RPE stops, open on the docked set.
    var islandRPE: RowID?
    /// Whether the island docks the open set, and holds the docked height.
    private(set) var dock = RowsDock()
    /// The dock's changes animate with this: a fade under Reduce Motion (the view sets it).
    @ObservationIgnored var dockAnimation: Animation? = .smooth(duration: 0.3)
    /// The list's frame on screen and its insets (the bars over it): its viewport.
    var listFrame = CGRect.zero
    var listInsets = EdgeInsets()

    /// Where the list shows content, on screen: its frame less the toolbar and the island.
    var listViewport: ClosedRange<Double> {
        let top = Double(listFrame.minY + listInsets.top)
        return top ... max(top, Double(listFrame.maxY - listInsets.bottom))
    }
    @ObservationIgnored private var hideTask: Task<Void, Never>?
    /// The bounded wait for an awaited editor's report (`RowsDock.noReport`).
    @ObservationIgnored private var reportTask: Task<Void, Never>?
    /// The list's own scroll to the open set, while it runs (`RowsDock.seek`).
    @ObservationIgnored private var seekTask: Task<Void, Never>?
    /// Releases a held height when the list never says it is idle.
    @ObservationIgnored private var holdTask: Task<Void, Never>?
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

    /// The `½`: the value with its half point added or taken off, rated, and its meaning for a moment.
    func pick(_ half: RowsRPEHalf, in editor: RowsEditor, session: SessionController) {
        guard let patch = half.patch else { return }
        apply(patch, row: editor.row, mode: editor.mode, session: session)
        if let meaning = half.meaning { show(Flash(row: editor.row, text: meaning, token: next())) }
    }

    // MARK: - The keypad

    func openKeypad(_ field: RowsField, in editor: RowsEditor) {
        openKeypad(field, row: editor.row, mode: editor.mode)
    }

    func openKeypad(_ field: RowsField, row: RowID, mode: RowsEditor.Mode, fromIsland: Bool = false) {
        typing = Typing(row: row, mode: mode, keypad: field.keypad, fromIsland: fromIsland)
    }

    // MARK: - Docking

    /// The open editor's Log button came into view or left it. Back in view
    /// undocks at once; out of view docks after a beat, so a panel that only
    /// blinks out (a Log, a row opening) never makes the island jump. Only
    /// the open editor's reports count, so the panel a Log removed cannot
    /// cancel the new one's.
    func logVisibility(_ row: RowID, visible: Bool) {
        guard row == dock.open else { return }
        hideTask?.cancel()
        if visible {
            withAnimation(dockAnimation) { dock.logButton(of: row, isVisible: true) }
            return
        }
        hideTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(250))
            guard !Task.isCancelled, let self else { return }
            withAnimation(self.dockAnimation) { self.dock.logButton(of: row, isVisible: false) }
        }
    }

    /// The list started or stopped moving. Idle gives back a held height,
    /// animated, so the end of the list settles rather than jumps after the
    /// finger lifts. A list that never says it is idle (its drag taken by a
    /// system gesture) gives it back after a few seconds.
    func scrollPhase(isIdle: Bool) {
        holdTask?.cancel()
        withAnimation(isIdle ? dockAnimation : nil) { dock.scroll(isIdle: isIdle) }
        guard !isIdle else { return }
        holdTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(4))
            guard !Task.isCancelled, let self else { return }
            withAnimation(self.dockAnimation) { self.dock.scroll(isIdle: true) }
        }
    }

    /// The list scrolls itself to the open set: nothing docks until it has
    /// landed (the scroll's spring, and a beat for the editor's report).
    func seek() {
        seekTask?.cancel()
        dock.seek(true)
        seekTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(750))
            guard !Task.isCancelled, let self else { return }
            withAnimation(self.dockAnimation) { self.dock.seek(false) }
        }
    }

    /// The list jumped to a set other than the open one.
    func jumped() {
        withAnimation(dockAnimation) { dock.jumped() }
    }

    /// The screen went away (the cover closed, the Table took over): nothing held.
    func leave() {
        for task in [hideTask, reportTask, seekTask, holdTask] { task?.cancel() }
        dock.seek(false)
        dock.scroll(isIdle: true)
    }

    /// The open row or the fix changed. A new open row, or a fix ending,
    /// waits for the editor's report: a panel laid out reports at once, so
    /// none after a beat (and after the list's own scroll) means it was
    /// never laid out, and it is out of view.
    private func showEditor(open: RowID?) {
        guard open != dock.open || (fixing != nil) != dock.isFixing else { return }
        hideTask?.cancel()
        dock.show(open: open, isFixing: fixing != nil)
        guard dock.awaitsReport else { return }
        reportTask?.cancel()
        reportTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(600))
            while self?.dock.isSeeking == true, !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(100))
            }
            try? await Task.sleep(for: .milliseconds(350))
            guard !Task.isCancelled, let self else { return }
            withAnimation(self.dockAnimation) { self.dock.noReport() }
        }
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
            showEditor(open: dock.open)
        case .touch:
            fixing = nil
            typing = nil
            showEditor(open: dock.open)
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
        showEditor(open: dock.open)
    }

    func cancelFix() {
        fixing = nil
        if typing?.mode == .fix { typing = nil }
        showEditor(open: dock.open)
    }

    /// The undo toast's Undo, for what it offered.
    func undoLast(session: SessionController) {
        guard let undo else { return }
        self.undo = nil
        if fresh == undo.row { fresh = nil }
        switch undo.kind {
        case .logged:
            Task {
                for action in RowsAction.takeBack(undo.row) { await session.send(action.command) }
            }
        case .unticked:
            Task {
                await session.send(.tick(undo.row))
                if session.state?.rows[undo.row]?.isPending == false { logged(undo.row, text: nil) }
            }
        case .deleted:
            session.keepPendingRemoval()
        }
    }

    /// Untick set, or a tap on a done set's check: back to pending with its
    /// values, open in place, as the Undo after a Log does. The toast offers
    /// to log it again. `name` is the set as its line names it (`Set 2`).
    func untick(_ row: RowID, name: String, session: SessionController) {
        Task {
            if fixing?.row == row { cancelFix() }
            await session.send(.untick(row))
            guard session.state?.rows[row]?.isPending == true else { return }
            await session.send(.touchRow(row))
            Haptics.shared.play(.step)
            offer(Undo(row: row, text: "\(name) unticked", token: next(), kind: .unticked))
        }
    }

    /// Delete set from a set's `…`: gone at once, with a moment to take it back.
    func delete(_ row: RowID, name: String, session: SessionController) {
        Task {
            if fixing?.row == row { cancelFix() }
            typing = nil
            await session.removeLater(row)
            guard session.pendingRemoval == row else { return }
            offer(Undo(row: row, text: "\(name) deleted", token: next(), kind: .deleted))
        }
    }

    /// "Then …": the list goes to that set and marks it for a moment; nothing opens.
    func showLine(_ row: RowID, session: SessionController) {
        let mark = Mark(row: row, token: next())
        self.mark = mark
        session.scrollTarget = ScrollTarget(row: row)
        Task { [weak self] in
            try? await Task.sleep(for: .seconds(2))
            if self?.mark == mark { self?.mark = nil }
        }
    }

    /// Drops what no longer matches the session: a fix of a row that is no
    /// longer done, a keypad on a row no longer open. A Log the entry check
    /// held is offered for undo once its answer logged the row, and dropped
    /// when the answer was Edit.
    func reconcile(with layout: RowsLayout, entryCheckRow: RowID?, pendingRemoval: RowID?) {
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
        showEditor(open: layout.openRow)
        if let typing {
            switch typing.mode {
            case .log where layout.item(typing.row)?.editor == nil: self.typing = nil
            case .fix where fixing?.row != typing.row: self.typing = nil
            default: break
            }
        }
        if let undo {
            let state = layout.item(undo.row)?.state
            let stands = switch undo.kind {
            case .logged: state == .done
            case .unticked: state != nil && state != .done
            case .deleted: pendingRemoval == undo.row
            }
            if !stands { self.undo = nil }
        }
        if let islandRPE, islandRPE != layout.openRow { self.islandRPE = nil }
    }

    // MARK: - Moments

    private func logged(_ row: RowID, text: String?) {
        fresh = row
        if let text {
            offer(Undo(row: row, text: text, token: next()))
        } else {
            undo = nil
        }
        Task { [weak self] in
            try? await Task.sleep(for: .seconds(5))
            if self?.fresh == row { self?.fresh = nil }
        }
    }

    /// The toast, for five seconds (a held removal is sent as it goes).
    private func offer(_ undo: Undo) {
        self.undo = undo
        Task { [weak self] in
            try? await Task.sleep(for: .seconds(5))
            if self?.undo?.token == undo.token { self?.undo = nil }
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
