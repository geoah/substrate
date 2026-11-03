import WorkoutsCore
import SwiftUI

/// A routine's page (DESIGN.md §7.3): its folder, notes and what the plan
/// holds, then Plan or Progress. The plan is the routine as a started
/// session draws it, read only (`RoutinePlanCards`); Progress is the Stats
/// tab's view for this routine's sessions (`StatsProgressView`). Start
/// floats in a bar of its own at the bottom, above the tab bar and the
/// session's bar when one runs: the page scrolls clear of all three, and
/// neither glass ever lies over Start.
///
/// A routine on the signed-in substrate is edited in place: Edit, and only
/// Edit (the read-only plan answers no tap but an exercise's history),
/// turns the plan into its editor, with Cancel and Save where Back and Edit
/// were; Duplicate opens the editor sheet for the copy, and Delete asks
/// first. Signed out, the sample the app carries cannot be edited, and says
/// so.
struct RoutinePreview: View {
    let routineID: RoutineID

    enum Page: Hashable {
        case plan, progress
    }

    @Environment(AppModel.self) private var app
    @Environment(SubstrateLink.self) private var link
    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var typeSize
    @State private var page: Page = .plan
    @State private var edit: PlanEditState?
    @State private var deleting: RoutineToDelete?
    @State private var duplicating: RoutineEditRequest?
    @State private var replacingDraft: PendingEdit?

    /// An edit waiting on the owner's answer about another routine's draft.
    struct PendingEdit {
        var editor: RoutineEditor
        var open: RoutinePlanSetRef?
    }

    var body: some View {
        if let catalog = app.catalog, let routine = catalog.routines[routineID] {
            content(routine, catalog: catalog)
        } else {
            ContentUnavailableView("Routine not found", systemImage: "list.bullet.clipboard")
        }
    }

    private func content(_ routine: Routine, catalog: Catalog) -> some View {
        let shown = edit?.model.routine ?? routine
        let plan = RoutinePlanPresenter.present(shown, exercises: app.exercises, history: app.historyIndex, rests: app.settings.session)
        return ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                if let edit {
                    RoutineDetailsFields(model: edit.model)
                    RoutinePlanCards(plan: plan, edit: edit)
                } else {
                    RoutineEditStatus(routineID: routineID) { if let draft = link.draft { begin(draft, open: nil) } }
                    RoutineHeader(name: typeSize.isAccessibilitySize ? routine.name : nil,
                                  summary: StatsPresenter.summary(routine, history: app.history, settings: app.presenterSettings),
                                  notes: routine.notes)
                    Picker("Show", selection: $page) {
                        Text("Plan").tag(Page.plan)
                        Text("Progress").tag(Page.progress)
                    }
                    .pickerStyle(.segmented)
                    switch page {
                    case .plan:
                        RoutinePlanCards(plan: plan, beginEdit: canBeginEdit ? { begin(RoutineEditor(routine), open: nil) } : nil)
                        if case .bundled = app.catalogOrigin {
                            Text("This sample comes with the app and can\u{2019}t be changed. Sign in to your substrate to edit routines or make your own copy.")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                                .padding(.horizontal, 4)
                        }
                    case .progress:
                        StatsProgressView(scope: .routine(routineID))
                    }
                }
            }
            .padding(.horizontal, 16)
            .padding(.bottom, 16)
        }
        .scrollDismissesKeyboard(.interactively)
        .background(Color(.systemGroupedBackground))
        .navigationTitle(edit.map { $0.model.editor.isNew ? "New routine" : "Edit routine" } ?? routine.name)
        // A large title truncates a long name at the accessibility sizes;
        // there the header wraps it instead.
        .navigationBarTitleDisplayMode(edit == nil && !typeSize.isAccessibilitySize ? .large : .inline)
        // Back would leave an edit half done: Cancel and Save end it.
        .navigationBarBackButtonHidden(edit != nil)
        .toolbar {
            if edit == nil, canEdit {
                ToolbarItem(placement: .topBarTrailing) {
                    Button("Edit") { begin(RoutineEditor(routine), open: nil) }
                        .disabled(!canBeginEdit)
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Menu("More", systemImage: "ellipsis") {
                        Button("Duplicate", systemImage: "plus.square.on.square") {
                            begin(RoutineEditor.duplicate(routine, taken: Set(catalog.routines.keys)), open: nil)
                        }
                        Button("Delete Routine", systemImage: "trash", role: .destructive) {
                            deleting = RoutineToDelete(id: routineID, name: routine.name)
                        }
                    }
                }
            }
        }
        .routineEditControls(edit?.model) { withAnimation(.smooth(duration: 0.25)) { edit = nil } }
        .planEditing(edit)
        .safeAreaBar(edge: .bottom) {
            if edit == nil {
                StartSessionButton(request: .routine(routineID)) {
                    Text("Start routine")
                        .font(.body.weight(.semibold))
                        .frame(maxWidth: .infinity, minHeight: 36)
                }
                .buttonStyle(.glassProminent)
                .padding(.horizontal, 20)
                .padding(.bottom, 8)
            }
        }
        .confirmsRoutineDelete($deleting) { dismiss() }
        .routineEditor($duplicating)
        .confirmationDialog(replacingDraft.map { _ in "Discard your unsaved edit of \u{201C}\(link.draft?.routine.name ?? "")\u{201D}?" } ?? "",
                            isPresented: Binding { replacingDraft != nil } set: { if !$0 { replacingDraft = nil } },
                            titleVisibility: .visible, presenting: replacingDraft) { pending in
            Button("Discard and Edit", role: .destructive) {
                replacingDraft = nil
                open(pending.editor, set: pending.open)
            }
            Button("Cancel", role: .cancel) { replacingDraft = nil }
        }
    }

    /// Routines on the signed-in substrate can be edited, duplicated and
    /// deleted; the sample the app carries signed out cannot.
    private var canEdit: Bool {
        guard case .server = app.catalogOrigin else { return false }
        return link.canDeleteRoutines
    }

    /// An edit can begin: the routine can be edited and no conflict waits
    /// for the owner's choice.
    private var canBeginEdit: Bool {
        canEdit && link.conflict(for: routineID) == nil
    }

    /// Begins an edit: the unsaved draft of this routine when there is
    /// one, else `editor`. A draft of another routine is replaced only
    /// after the owner says so.
    private func begin(_ editor: RoutineEditor, open set: RoutinePlanSetRef?) {
        if let draft = link.draft, draft.hasChanges {
            if !editor.isNew, !draft.isNew, draft.routine.id == routineID { return open(draft, set: set) }
            replacingDraft = PendingEdit(editor: editor, open: set)
            return
        }
        open(editor, set: set)
    }

    /// A duplicate is a new routine: it opens in the editor sheet, and is a
    /// change already, kept before the first edit. This routine's own edit
    /// opens in place, on the plan.
    private func open(_ editor: RoutineEditor, set: RoutinePlanSetRef?) {
        let model = RoutineEditorModel(editor) { [link] in link.keepDraft($0) }
        if editor.hasChanges { link.keepDraft(editor) }
        if editor.isNew {
            duplicating = RoutineEditRequest(model: model)
            return
        }
        page = .plan
        withAnimation(.smooth(duration: 0.25)) { edit = PlanEditState(model: model, openSet: set) }
    }
}

/// The routine's folder, its notes, and what the plan holds as label over
/// value: Exercises, Sets per session, Last done.
private struct RoutineHeader: View {
    /// Shown when the navigation title is not the large one.
    let name: String?
    let summary: RoutineSummaryPresentation
    let notes: String?
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if let name {
                Text(name)
                    .font(.title.bold())
                    .accessibilityAddTraits(.isHeader)
            }
            if let folder = summary.folder {
                Label(folder, systemImage: "folder")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .accessibilityLabel("Folder, \(folder)")
            }
            if let notes {
                Text(notes)
                    .font(.subheadline)
            }
            let layout = typeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
                : AnyLayout(HStackLayout(alignment: .top, spacing: 12))
            layout {
                ForEach(summary.facts) { fact in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(fact.label)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        Text(fact.value)
                            .font(.headline)
                            .monospacedDigit()
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .accessibilityElement(children: .combine)
                }
            }
        }
        .padding(.horizontal, 4)
        .padding(.top, 4)
    }
}

#if DEBUG
#Preview("Plan, with history") {
    let app = PreviewData.app()
    NavigationStack {
        RoutinePreview(routineID: SampleRoutine.id)
            .statsDestinations()
    }
    .environment(app)
    .environment(PreviewData.noSession(app))
    .environment(WorkoutsRoot.shared.link)
}

#Preview("Progress, with history") {
    let app = PreviewData.app()
    NavigationStack {
        ScrollView {
            StatsProgressView(scope: .routine(SampleRoutine.id))
                .padding(16)
        }
        .background(Color(.systemGroupedBackground))
        .statsDestinations()
    }
    .environment(app)
    .environment(PreviewData.noSession(app))
    .environment(WorkoutsRoot.shared.link)
}

#Preview("Progress, no sessions yet") {
    let app = AppModel(catalog: PreviewData.catalog, history: [])
    NavigationStack {
        ScrollView {
            StatsProgressView(scope: .routine(SampleRoutine.id))
                .padding(16)
        }
        .background(Color(.systemGroupedBackground))
    }
    .environment(app)
    .environment(PreviewData.noSession(app))
    .environment(WorkoutsRoot.shared.link)
}

#Preview("Editing, a set open") {
    let app = PreviewData.app()
    let routine = PreviewData.catalog.routines[SampleRoutine.id]!
    let edit = PlanEditState(model: RoutineEditorModel(RoutineEditor(routine)) { _ in },
                             openSet: routine.slots.first.flatMap { slot in
                                 slot.sets.first { $0.role == .working }.map { RoutinePlanSetRef(slot: slot.key, set: $0.key) }
                             })
    NavigationStack {
        ScrollView {
            RoutinePlanCards(plan: RoutinePlanPresenter.present(routine, exercises: PreviewData.catalog.exercises,
                                                                history: app.historyIndex),
                             edit: edit)
                .padding(16)
        }
        .background(Color(.systemGroupedBackground))
        .planEditing(edit)
    }
    .environment(app)
    .environment(PreviewData.noSession(app))
    .environment(WorkoutsRoot.shared.link)
}

#Preview("No exercises") {
    var catalog = PreviewData.catalog
    catalog.routines["empty"] = Routine(id: "empty", name: "Empty routine", folder: "Drafts")
    let app = AppModel(catalog: catalog, history: [])
    return NavigationStack {
        RoutinePreview(routineID: "empty")
            .statsDestinations()
    }
    .environment(app)
    .environment(PreviewData.noSession(app))
    .environment(WorkoutsRoot.shared.link)
}

#Preview("Largest text, dark") {
    let app = PreviewData.app()
    NavigationStack {
        RoutinePreview(routineID: SampleRoutine.id)
            .statsDestinations()
    }
    .environment(app)
    .environment(PreviewData.noSession(app))
    .environment(WorkoutsRoot.shared.link)
    .environment(\.dynamicTypeSize, .accessibility3)
    .preferredColorScheme(.dark)
}
#endif
