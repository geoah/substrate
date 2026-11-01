import WorkoutsCore
import SwiftUI

/// The session (DESIGN.md §7.6, §7.6a): a full-screen cover with the
/// system's glass toolbar and the session in the content layer, drawn in the
/// owner's layout. Rows (the default) lists each set as a line with the set
/// to do now open in place and a glass capsule at the bottom; Table is the
/// grid of exercise cards with the rest bar. Both read the same presentation;
/// every change is a command.
struct SessionCover: View {
    @Environment(AppModel.self) private var app
    @Environment(SessionController.self) private var session
    @State private var cover = CoverModel()
    /// The Rows layout's view state: a fix in progress, the keypad, what is folded.
    @State private var rowsScreen: RowsScreenModel
    @FocusState private var focus: CoverModel.Field?
    @State private var path = NavigationPath()
    /// The last session shown: Save and Discard clear the session before the
    /// cover has closed, and the closing cover keeps drawing it.
    @State private var lastPresentation: SessionPresentation?

    /// `rowsScreen` lets a preview open with a fix or the keypad up.
    init(rowsScreen: RowsScreenModel = RowsScreenModel()) {
        _rowsScreen = State(initialValue: rowsScreen)
    }

    private var layout: SessionLayout { app.settings.sessionLayout }

    var body: some View {
        @Bindable var cover = cover
        NavigationStack(path: $path) {
            Group {
                if let presentation = session.presentation ?? (app.isSessionPresented ? nil : lastPresentation) {
                    content(presentation)
                } else if session.isStarting {
                    ProgressView()
                } else if app.isSessionPresented {
                    ContentUnavailableView {
                        Label("No session in progress", systemImage: "dumbbell")
                    } actions: {
                        Button("Close") { app.isSessionPresented = false }
                    }
                } else {
                    Color(.systemGroupedBackground)
                }
            }
            .navigationDestination(for: ExerciseID.self) { id in
                ExerciseDetailView(exerciseID: id)
                    .safeAreaBar(edge: .bottom) { pushedRestBar }
            }
            .navigationDestination(for: SessionID.self) { id in
                SessionDetailView(sessionID: id)
                    .safeAreaBar(edge: .bottom) { pushedRestBar }
            }
        }
        .onChange(of: session.presentation) { old, new in
            if new == nil, let old { lastPresentation = old }
        }
        .sessionScreen(keepAwake: app.settings.keepAwake)
        .sheet(item: $cover.sheet, onDismiss: sheetDismissed) { sheet in
            sheetContent(sheet)
        }
        .alert(entryTitle, isPresented: entryCheckShown, presenting: session.entryCheck) { request in
            if let use = request.warning.useTitle {
                Button(use) { Task { await session.answerEntryCheck(.useCorrected) } }
            }
            Button(request.warning.keepTitle) { Task { await session.answerEntryCheck(.keep) } }
            Button(request.warning.editTitle, role: .cancel) {
                let row = request.row
                Task { await session.answerEntryCheck(.edit) }
                // Rows: the set stays open in place with its steppers.
                if layout == .table { focusFirstField(of: row) }
            }
        } message: { request in
            Text(request.warning.message)
        }
        .confirmationDialog(untickedTitle, isPresented: $cover.askUnticked, titleVisibility: .visible) {
            Button(FinishSummary.dropTitle) { cover.sheet = .finish(.drop) }
            Button(FinishSummary.markDoneTitle) { cover.sheet = .finish(.markDone) }
            if (cover.untickedPrompt?.projection.incompleteSets ?? 0) > 0 {
                Button(FinishSheet.fillMissingTitle) {
                    session.startAfterFinish = nil
                    fillMissing()
                }
            }
            Button(FinishSummary.cancelTitle, role: .cancel) { session.startAfterFinish = nil }
        } message: {
            // "2 have a value missing and are left out either way."
            if let message = cover.untickedPrompt?.untickedMessage {
                Text(message)
            }
        }
        .alert(session.presentation?.discardQuestion ?? "Discard this session?", isPresented: $cover.askDiscard) {
            Button("Discard", role: .destructive) { Task { await session.discard() } }
            Button("Cancel", role: .cancel) {}
        }
        .alert(cover.removeExercise?.question ?? "", isPresented: removeExerciseShown) {
            Button("Remove", role: .destructive) {
                if let slot = cover.removeExercise?.slot { session.run(.removeExercise(slot: slot)) }
                cover.removeExercise = nil
            }
            Button("Cancel", role: .cancel) { cover.removeExercise = nil }
        }
        .alert("Remove this done set?", isPresented: removeDoneRowShown) {
            Button("Remove", role: .destructive) {
                if let row = cover.removeDoneRow { session.run(.removeSet(row)) }
                cover.removeDoneRow = nil
            }
            Button("Cancel", role: .cancel) { cover.removeDoneRow = nil }
        } message: {
            Text("Its values are deleted with it.")
        }
        .onChange(of: session.pendingFinishFlow, initial: true) { _, pending in
            guard pending else { return }
            session.pendingFinishFlow = false
            Task {
                // Let the cover finish appearing before a dialog or sheet rises over it.
                try? await Task.sleep(for: .milliseconds(450))
                beginFinish()
            }
        }
        .environment(cover)
        .environment(rowsScreen)
    }

    // MARK: - Content

    @ViewBuilder
    private func content(_ presentation: SessionPresentation) -> some View {
        Group {
            switch layout {
            case .rows:
                RowsSessionView(presentation: presentation, finish: beginFinish) { exercise in
                    path.append(exercise)
                }
            case .table:
                tableContent(presentation)
            }
        }
        .navigationTitle(presentation.title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { toolbar(presentation) }
        .toolbar { keyboardToolbar(presentation) }
        .onChange(of: focus) { old, new in focusChanged(from: old, to: new) }
    }

    /// The Table layout (§7.6): the header, the exercise cards and their
    /// grids, and the rest bar.
    private func tableContent(_ presentation: SessionPresentation) -> some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 14) {
                    SessionHeaderView(header: presentation.header) {
                        session.run(presentation.header.isPaused ? .resumeClock : .pauseClock)
                    }
                    if session.journalWriteFailed {
                        NotSavingBanner()
                    }
                    if session.alertsOff {
                        AlertsOffBanner()
                    }
                    ForEach(presentation.blocks) { block in
                        Group {
                            if let group = block.group {
                                GroupBlockView(group: group, cards: block.cards, allCards: presentation.cards, focus: $focus)
                            } else if let card = block.cards.first {
                                ExerciseCardView(card: card, group: nil, allCards: presentation.cards, focus: $focus)
                            }
                        }
                        .id(block.id)
                    }
                    Button {
                        cover.sheet = .addExercise(after: nil)
                    } label: {
                        Label("Add exercise", systemImage: "plus")
                            .font(.headline)
                            .frame(maxWidth: .infinity, minHeight: 36)
                    }
                    .buttonStyle(.bordered)
                    .padding(.top, 4)
                }
                .padding(.horizontal, 12)
                .padding(.bottom, 16)
                .swipeActionsContainer()
            }
            .background(Color(.systemGroupedBackground))
            .scrollDismissesKeyboard(.interactively)
            .scrollEdgeEffectStyle(.soft, for: .bottom)
            .safeAreaBar(edge: .bottom) {
                // While typing, only a running countdown stays above the keypad.
                if focus == nil || presentation.restBar.isCounting {
                    restBar(presentation.restBar) { scrollToUpNext(proxy) }
                }
            }
            .onChange(of: session.scrollTarget, initial: true) { _, target in
                guard let target else { return }
                session.scrollTarget = nil
                scroll(to: target, presentation: presentation, proxy: proxy)
            }
        }
    }

    // MARK: - The rest bar

    /// The bar steps aside while a sheet is up: its tinted glass would show
    /// through the sheet's (the RPE sheet carries the rest's controls).
    private func restBar(_ bar: RestBarPresentation, showUpNext: @escaping () -> Void) -> some View {
        let hidden = cover.sheet != nil
        return RestBar(
            bar: bar,
            upNext: session.presentation?.upNext,
            adjust: { session.run(.adjustRest($0)) },
            skip: { session.run(.skipRest) },
            stop: { row in
                Task {
                    await cover.commitRow(row, session: session)
                    await session.send(.stopTimedSet(row))
                }
            },
            finish: beginFinish,
            showUpNext: showUpNext,
            addExercise: { cover.sheet = .addExercise(after: nil) }
        )
        .padding(.horizontal, 12)
        .padding(.bottom, 6)
        .opacity(hidden ? 0 : 1)
        .allowsHitTesting(!hidden)
        .accessibilityHidden(hidden)
        .animation(.smooth(duration: 0.2), value: hidden)
    }

    /// On a page pushed inside the cover (an exercise's history), a running
    /// rest or timed set keeps its controls; the Up next chip goes back. In
    /// Rows it is the capsule, whose tap goes back.
    @ViewBuilder
    private var pushedRestBar: some View {
        if layout == .rows, let rows = session.presentation?.rowsLayout, rows.capsule.isCounting {
            RowsCapsuleView(
                capsule: rows.capsule,
                layout: rows,
                perform: { rowsScreen.perform($0, session: session, cover: cover) },
                showOpenRow: { path = NavigationPath() },
                finish: beginFinish,
                addExercise: { cover.sheet = .addExercise(after: nil) }
            )
            .padding(.horizontal, 16)
            .padding(.bottom, 6)
        } else if layout == .table, let bar = session.presentation?.restBar, bar.isCounting {
            restBar(bar) { path = NavigationPath() }
        }
    }

    // MARK: - Toolbar

    @ToolbarContentBuilder
    private func toolbar(_ presentation: SessionPresentation) -> some ToolbarContent {
        if layout == .rows {
            // Rows has no header: the clock sits under the title (§7.6a).
            ToolbarItem(placement: .principal) {
                RowsTitleView(title: presentation.title, header: presentation.header) {
                    session.run(presentation.header.isPaused ? .resumeClock : .pauseClock)
                }
            }
        }
        ToolbarItem(placement: .topBarLeading) {
            Button {
                focus = nil
                app.isSessionPresented = false
            } label: {
                Label("Minimise", systemImage: "chevron.down")
            }
            .accessibilityHint("Keeps the session running")
        }
        ToolbarItem(placement: .topBarTrailing) {
            Menu {
                ForEach(presentation.cards) { card in
                    Button {
                        session.scrollTarget = ScrollTarget(slot: card.slot)
                    } label: {
                        Text(card.groupLabel.map { "\($0) \(card.name)" } ?? card.name)
                        Text(card.progress)
                    }
                }
            } label: {
                Label("Exercises", systemImage: "list.bullet")
            }
        }
        ToolbarItem(placement: .topBarTrailing) {
            Menu {
                Button("Add exercise", systemImage: "plus") { cover.sheet = .addExercise(after: nil) }
                Button("Reorder exercises", systemImage: "arrow.up.arrow.down") { cover.sheet = .reorder }
                Button("Timer…", systemImage: "timer") { cover.sheet = .rest(.timer) }
                if presentation.header.isPaused {
                    Button("Resume clock", systemImage: "play") { session.run(.resumeClock) }
                } else {
                    Button("Pause clock", systemImage: "pause") { session.run(.pauseClock) }
                }
                Button("Session note", systemImage: "note.text") { cover.sheet = .note(.session) }
                Button(layout.switchTitle, systemImage: layout.switchSymbol) { switchLayout() }
                Divider()
                Button("Discard session", systemImage: "trash", role: .destructive) { cover.askDiscard = true }
            } label: {
                Label("More", systemImage: "ellipsis")
            }
        }
        ToolbarSpacer(.fixed, placement: .topBarTrailing)
        ToolbarItem(placement: .topBarPinnedTrailing) {
            Button("Finish", action: beginFinish)
                .buttonStyle(.glassProminent)
        }
        .visibilityPriority(.high)
    }

    /// The keypad's bar (§7.6): the step down and up (the exercise's load
    /// step, 1 rep, 5 s), Next and Done, all on the right.
    @ToolbarContentBuilder
    private func keyboardToolbar(_ presentation: SessionPresentation) -> some ToolbarContent {
        ToolbarItemGroup(placement: .keyboard) {
            // Every keypad action on the right, in the thumb's reach.
            Spacer()
            if let field = focus, let step = step(for: field, in: presentation) {
                Button("\u{2212}\(step.title)") { applyStep(-step.amount, field: field, presentation: presentation) }
                    .monospacedDigit()
                    .accessibilityLabel("Minus \(step.title)")
                Button("+\(step.title)") { applyStep(step.amount, field: field, presentation: presentation) }
                    .monospacedDigit()
                    .accessibilityLabel("Plus \(step.title)")
            }
            Button("Next") { moveToNextField(in: presentation) }
            Button("Done") { focus = nil }
                .fontWeight(.semibold)
        }
    }

    private struct Step {
        var amount: Decimal
        var title: String
    }

    private func step(for field: CoverModel.Field, in presentation: SessionPresentation) -> Step? {
        guard let row = presentation.row(field.row), let card = presentation.card(row.slot), let value = field.column.field else {
            return nil
        }
        switch value {
        case .load, .assistance:
            let step = card.loadStep?.value ?? 1
            return Step(amount: step, title: "\(Format.number(step)) \(card.unit.rawValue)")
        case .reps:
            return Step(amount: 1, title: "1")
        case .duration:
            return Step(amount: Decimal(card.timeStep), title: "\(card.timeStep) s")
        case .distance:
            // The spec names no distance step; 5 m is a gym's floor marking.
            return Step(amount: 5, title: "5 m")
        }
    }

    /// Moves what the field shows, typed or ghost, by a step.
    private func applyStep(_ amount: Decimal, field: CoverModel.Field, presentation: SessionPresentation) {
        guard let row = presentation.row(field.row), let cell = row.cell(field.column), let value = field.column.field else { return }
        let shown = cover.text(field, value: row.isDone ? cell.text : cell.entered)
        let base = shown.isEmpty && !cell.isHint ? cell.text ?? "" : shown
        if let stepped = GridValue.stepped(base, field: value, by: amount) {
            cover.setDraft(stepped, for: field)
        }
    }

    /// Every field the keypad's Next walks, in display order: the open rows'
    /// value fields.
    private func fieldOrder(_ presentation: SessionPresentation) -> [CoverModel.Field] {
        presentation.cards.flatMap { card in
            card.rows.filter { !$0.isDone && $0.state != .locked }.flatMap { row in
                card.columns.filter { $0.field != nil }.map { CoverModel.Field(row: row.id, column: $0) }
            }
        }
    }

    private func moveToNextField(in presentation: SessionPresentation) {
        let order = fieldOrder(presentation)
        guard let current = focus, let index = order.firstIndex(of: current), index + 1 < order.count else {
            focus = nil
            return
        }
        focus = order[index + 1]
    }

    private func focusFirstField(of row: RowID) {
        guard let presentation = session.presentation, let shown = presentation.row(row),
              let card = presentation.card(shown.slot), let column = card.columns.first(where: { $0.field != nil })
        else { return }
        focus = CoverModel.Field(row: row, column: column)
    }

    // MARK: - Focus

    /// Leaving a field sends what was typed; entering a row touches it,
    /// which ends the rest it measures once the countdown is over (§4.8).
    private func focusChanged(from old: CoverModel.Field?, to new: CoverModel.Field?) {
        Task {
            if let old { await cover.commit(old, session: session) }
            if let new, new.row != old?.row { await session.send(.touchRow(new.row)) }
        }
    }

    // MARK: - Scrolling

    /// The only scrolls: a tap on Up next or in the jump menu, a deep link,
    /// the entry check's Edit, and a superset's 0 s transition (§7.6).
    private func scroll(to target: ScrollTarget, presentation: SessionPresentation, proxy: ScrollViewProxy) {
        let slot = target.slot ?? target.row.flatMap { presentation.row($0)?.slot }
        guard let slot, let block = presentation.blocks.first(where: { $0.cards.contains { $0.slot == slot } }) else { return }
        Task {
            // A target set as the cover opens waits for the list to lay out.
            try? await Task.sleep(for: .milliseconds(50))
            withAnimation(.smooth) { proxy.scrollTo(block.id, anchor: .top) }
            // The lazy block is materialised now; its row or member can be brought in.
            try? await Task.sleep(for: .milliseconds(150))
            if let row = target.row {
                withAnimation(.smooth) { proxy.scrollTo(row, anchor: .center) }
            } else if block.group != nil {
                withAnimation(.smooth) { proxy.scrollTo(slot, anchor: .top) }
            }
        }
    }

    private func scrollToUpNext(_ proxy: ScrollViewProxy) {
        guard let row = session.presentation?.upNext?.row ?? session.presentation?.timedRow else { return }
        session.scrollTarget = ScrollTarget(row: row)
    }

    // MARK: - Layout

    /// The More menu's "Show as table" or "Show as rows": the setting
    /// changes, and the session is presented again with its RPE pre-fill on
    /// or off (§4.14).
    private func switchLayout() {
        focus = nil
        rowsScreen.fixing = nil
        rowsScreen.typing = nil
        app.settings.sessionLayout = layout.other
        session.present()
    }

    // MARK: - Finish

    /// Finish (§7.7): a running timed set stops first (the engine will not
    /// finish over one); unticked rows ask once; then the sheet.
    private func beginFinish() {
        focus = nil
        guard let presentation = session.presentation else { return }
        if let timed = presentation.timedRow {
            Task {
                await cover.commitRow(timed, session: session)
                await session.send(.stopTimedSet(timed))
                // Refused, or waiting on the entry check: Finish waits too.
                guard session.presentation?.timedRow == nil, session.entryCheck == nil else { return }
                openFinish()
            }
        } else {
            openFinish()
        }
    }

    private func openFinish() {
        guard let presentation = session.presentation else { return }
        if presentation.unticked.isEmpty {
            cover.sheet = .finish(.drop)
        } else {
            // Worked out once, for "Mark them done as shown": it says how many cannot be.
            cover.untickedPrompt = session.finishSummary(end: .now, policy: .markDone)
            cover.askUnticked = true
        }
    }

    private var untickedTitle: String {
        cover.untickedPrompt?.untickedTitle ?? "Some sets are not ticked"
    }

    /// Finish's way back (§7.7): the first unticked set with a value missing
    /// comes into view, and its first empty field takes the keypad when the
    /// row is open (a locked drop waits for its set).
    private func fillMissing() {
        guard let presentation = session.presentation,
              let id = presentation.unticked.first(where: { $0.values == nil })?.row
        else { return }
        if layout == .rows {
            // Rows: the set opens in place, its empty field outlined, and the
            // list follows the open set. A locked drop waits for its set, and
            // a set already open only needs bringing into view.
            if presentation.row(id)?.state == .locked || presentation.rowsLayout.openRow == id {
                session.scrollTarget = ScrollTarget(row: id)
            } else {
                session.run(.touchRow(id))
            }
            return
        }
        session.scrollTarget = ScrollTarget(row: id)
        Task {
            // After the sheet or dialog has gone and the list has scrolled.
            try? await Task.sleep(for: .milliseconds(650))
            guard let shown = session.presentation, let row = shown.row(id), row.state != .locked, !row.isDone,
                  let card = shown.card(row.slot), let missing = row.missing.first,
                  let column = card.columns.first(where: { $0.field == missing })
            else { return }
            focus = CoverModel.Field(row: id, column: column)
        }
    }

    // MARK: - Sheets

    @ViewBuilder
    private func sheetContent(_ sheet: CoverModel.Sheet) -> some View {
        switch sheet {
        case let .rpe(row):
            RPESheet(rowID: row)
        case let .addExercise(after):
            ExercisePicker(title: "Add exercise") { exercise in
                session.run(.addExercise(exercise, after: after))
            }
        case let .replaceExercise(slot):
            ExercisePicker(title: "Replace exercise") { exercise in
                session.run(.replaceExercise(slot: slot, with: exercise))
            }
        case .reorder:
            let blocks = session.presentation?.blocks ?? []
            ReorderSheet(blocks: blocks) { slots in
                guard slots != blocks.flatMap({ $0.cards.map(\.slot) }) else { return }
                session.run(.reorderExercises(slots))
            }
        case let .rest(target):
            RestPickerSheet(title: restTitle(target), initial: restInitial(target)) { seconds in
                // A timer of nothing starts nothing.
                if case .timer = target, seconds == 0 { return }
                session.run(target.command(seconds))
            }
        case let .note(edit):
            NoteSheet(title: noteTitle(edit), initial: noteText(edit)) { text in
                switch edit {
                case .session: session.run(.setNote(.session, text))
                case let .slot(key): session.run(.setNote(.slot(key), text))
                case let .row(row): session.run(.setRowNote(row, text))
                }
            }
        case let .finish(policy):
            FinishSheet(policy: policy, fillMissing: fillMissing)
        case .setTypes:
            SetTypesSheet()
        }
    }

    private func sheetDismissed() {
        // A Finish cancelled by a swipe keeps the session and drops the start that waited on it.
        if session.isActive, session.startAfterFinish != nil, cover.sheet == nil { session.startAfterFinish = nil }
    }

    private func restTitle(_ target: RestTarget) -> String {
        switch target {
        case let .slot(_, kind): kind.label
        case .row: "Rest after this set"
        case .roundRest: "Rest after each round"
        case .timer: "Timer"
        }
    }

    private func restInitial(_ target: RestTarget) -> Seconds {
        guard let presentation = session.presentation else { return 90 }
        switch target {
        case let .slot(key, kind):
            return presentation.card(key)?.rests.first { $0.kind == kind }?.seconds ?? 90
        case let .row(row):
            return presentation.row(row)?.restOverride ?? 90
        case let .roundRest(group):
            return presentation.blocks.compactMap(\.group).first { $0.key == group }?.roundRest ?? 90
        case .timer:
            return 60
        }
    }

    private func noteTitle(_ edit: CoverModel.NoteEdit) -> String {
        switch edit {
        case .session: "Session note"
        case let .slot(key): session.presentation?.card(key)?.name ?? "Note"
        case let .row(row): session.presentation?.row(row).map { $0.label.spoken.capitalizedSentence } ?? "Note"
        }
    }

    private func noteText(_ edit: CoverModel.NoteEdit) -> String? {
        switch edit {
        case .session: session.state?.session.notes
        case let .slot(key): session.presentation?.card(key)?.notes
        case let .row(row): session.presentation?.row(row)?.notes
        }
    }

    // MARK: - Dialog bindings

    private var entryTitle: String { session.entryCheck?.warning.title ?? "" }

    private var entryCheckShown: Binding<Bool> {
        Binding { session.entryCheck != nil } set: { shown in
            if !shown, session.entryCheck != nil { session.entryCheck = nil }
        }
    }

    private var removeExerciseShown: Binding<Bool> {
        Binding { cover.removeExercise != nil } set: { if !$0 { cover.removeExercise = nil } }
    }

    private var removeDoneRowShown: Binding<Bool> {
        Binding { cover.removeDoneRow != nil } set: { if !$0 { cover.removeDoneRow = nil } }
    }
}

#if DEBUG
// The Table layout's previews; the Rows layout's are in Rows/RowsPreviews.swift.
#Preview("Table, resting") {
    let app = PreviewData.app(layout: .table)
    SessionCover()
        .environment(app)
        .environment(PreviewData.session(app))
}

#Preview("Table, timed set") {
    let app = PreviewData.app(layout: .table)
    SessionCover()
        .environment(app)
        .environment(PreviewData.timedSession(app))
}

#Preview("Table, large text") {
    let app = PreviewData.app(layout: .table)
    SessionCover()
        .environment(app)
        .environment(PreviewData.session(app))
        .dynamicTypeSize(.xxxLarge)
}

#Preview("Table, dark") {
    let app = PreviewData.app(layout: .table)
    SessionCover()
        .environment(app)
        .environment(PreviewData.session(app))
        .preferredColorScheme(.dark)
}
#endif
