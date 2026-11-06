import SwiftUI
import WorkoutsCore
import WorkoutsSync

/// A routine's plan as a started session draws it (DESIGN.md §7.3, §7.6a),
/// read only or being edited: one opaque card per exercise or group, the
/// name and the plan in a line under it, then a line per planned set with
/// its label, its targets, the plan's effort, its tags and last time. A
/// group reads by round. The lines are the session's own (`RowsSetLine`,
/// `RowsRoundHeaderView`, `RowsChainHeaderView`), drawn from
/// `RoutinePlanPresentation`.
///
/// Read only, the plan is only read: no line has a glyph or answers a tap,
/// and an exercise's name, with its chevron, opens its history. Edit (the
/// toolbar's) is the one way into the editor, so a tap never changes what
/// the page is. Editing, the lines take the session's glyphs, a tap opens
/// the set in place (`PlanSetPanel`), each line's `…` and each card's hold
/// the rest, and Add set, Add warm-up and Add exercise sit where a session
/// has them.
struct RoutinePlanCards: View {
    let plan: RoutinePlanPresentation
    var edit: PlanEditState?
    /// Read only and empty: begins an edit from "Add an exercise"; nil
    /// where the routine cannot be edited.
    var beginEdit: (() -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if plan.isEmpty {
                emptyCard
            }
            ForEach(Array(plan.blocks.enumerated()), id: \.element.id) { index, block in
                PlanBlockCard(block: block, index: index, count: plan.blocks.count, plan: plan, edit: edit)
            }
            if let edit {
                Button {
                    edit.picker = .add(group: nil)
                } label: {
                    Label("Add exercise", systemImage: "plus")
                        .font(.headline)
                        .frame(maxWidth: .infinity, minHeight: 36)
                }
                .buttonStyle(.bordered)
                .padding(.top, 4)
            }
            if !plan.unreadable.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    Label("Could not be read", systemImage: "exclamationmark.triangle")
                        .font(.subheadline.weight(.semibold))
                    Text("A session leaves these out: \(plan.unreadable.joined(separator: ", ")).")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                .padding(16)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(RowsStyle.card, in: .rect(cornerRadius: RowsStyle.cardRadius))
            }
        }
        .animation(.smooth(duration: 0.25), value: edit?.openSet)
    }

    private var emptyCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(RoutinePlanPresentation.empty)
                .font(.body)
                .foregroundStyle(.secondary)
            if edit == nil, let beginEdit {
                Button("Add an exercise") { beginEdit() }
                    .font(.subheadline.weight(.semibold))
                    .frame(minHeight: 44)
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RowsStyle.card, in: .rect(cornerRadius: RowsStyle.cardRadius))
    }
}

/// One exercise or group of the plan.
private struct PlanBlockCard: View {
    let block: RoutinePlanBlock
    let index: Int
    let count: Int
    let plan: RoutinePlanPresentation
    let edit: PlanEditState?

    @Environment(AppModel.self) private var app
    @ScaledMetric(relativeTo: .body) private var glyphIndent: CGFloat = 48

    /// Under the labels: read only, the lines have no glyph before them.
    private var separatorIndent: CGFloat { edit == nil ? 16 : glyphIndent }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            ForEach(Array(block.items.enumerated()), id: \.element.id) { position, item in
                if position > 0, isLine(item), isLine(block.items[position - 1]) {
                    separator
                }
                itemView(item)
            }
            if let edit {
                addRows(edit)
            }
        }
        .padding(.bottom, 4)
        .background(RowsStyle.card, in: .rect(cornerRadius: RowsStyle.cardRadius))
    }

    private var separator: some View {
        Rectangle()
            .fill(Color(.separator))
            .frame(height: 0.5)
            .padding(.leading, separatorIndent)
            .accessibilityHidden(true)
    }

    // MARK: - Header

    private var header: some View {
        HStack(alignment: .top, spacing: 8) {
            VStack(alignment: .leading, spacing: 1) {
                title
                if let subtitle = block.subtitle {
                    Text(subtitle)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                if let cue = block.cue {
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Image(systemName: "lightbulb")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.tint)
                            .accessibilityHidden(true)
                        Text(cue)
                            .font(.footnote.weight(.medium))
                            .foregroundStyle(.secondary)
                    }
                    .padding(.top, 3)
                    .accessibilityElement(children: .combine)
                    .accessibilityLabel("Cue: \(cue)")
                }
                if let notes = block.notes {
                    NotesText(notes)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .padding(.top, 3)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if let edit {
                menu(edit)
            }
        }
        .padding(.top, 14)
        .padding(.leading, 16)
        .padding(.trailing, 12)
        .padding(.bottom, 6)
    }

    @ViewBuilder
    private var title: some View {
        let text = Text(block.title)
            .font(.title3.weight(.semibold))
            .foregroundStyle(.primary)
            .multilineTextAlignment(.leading)
        if let edit {
            Button {
                edit.route = block.group.map(RoutineEditorRoute.group) ?? block.members.first.map { .slot($0.slot) }
            } label: {
                text
            }
            .buttonStyle(.plain)
            .accessibilityLabel(block.spokenTitle)
            .accessibilityAddTraits(.isHeader)
            .accessibilityHint(block.group == nil ? "Opens the exercise's rests, notes and sets" : "Opens the group's kind, rounds and rest")
        } else if block.group == nil, let member = block.members.first, member.isKnown {
            NavigationLink(value: member.exercise) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    text
                    Image(systemName: "chevron.right")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.tertiary)
                        .accessibilityHidden(true)
                }
            }
            .buttonStyle(.plain)
            .accessibilityAddTraits(.isHeader)
            .accessibilityHint("Shows the exercise's history")
        } else {
            text
                .accessibilityLabel(block.spokenTitle)
                .accessibilityAddTraits(.isHeader)
        }
    }

    private func menu(_ edit: PlanEditState) -> some View {
        Menu {
            if let group = block.group {
                Button("Group Settings\u{2026}", systemImage: "slider.horizontal.3") { edit.route = .group(group) }
                Button("Add Exercise to \(block.title)", systemImage: "plus") { edit.picker = .add(group: group) }
                Divider()
                ForEach(block.members) { member in
                    Menu(member.name) {
                        Button("Exercise Settings\u{2026}", systemImage: "slider.horizontal.3") { edit.route = .slot(member.slot) }
                        Button("Replace Exercise\u{2026}", systemImage: "arrow.left.arrow.right") { edit.picker = .replace(slot: member.slot) }
                        Button("Leave \(block.title)", systemImage: "arrow.down.right") {
                            change(edit) { $0.leaveGroup(member.slot) }
                        }
                        Button("Remove from Routine", systemImage: "trash", role: .destructive) {
                            change(edit) { $0.removeSlot(member.slot) }
                        }
                    }
                }
                Divider()
                Button("Ungroup", systemImage: "rectangle.split.1x2") { change(edit) { $0.ungroup(group) } }
            } else if let member = block.members.first {
                Button("Exercise Settings\u{2026}", systemImage: "slider.horizontal.3") { edit.route = .slot(member.slot) }
                Button("Replace Exercise\u{2026}", systemImage: "arrow.left.arrow.right") { edit.picker = .replace(slot: member.slot) }
            }
            Divider()
            Button("Move Up", systemImage: "arrow.up") { change(edit) { $0.moveBlocks(from: [index], to: index - 1) } }
                .disabled(index == 0)
            Button("Move Down", systemImage: "arrow.down") { change(edit) { $0.moveBlocks(from: [index], to: index + 2) } }
                .disabled(index + 1 >= count)
            if index + 1 < count {
                Button("Group with Next", systemImage: "link") { change(edit) { $0.groupWithNext(index) } }
            }
            Button("Reorder Exercises\u{2026}", systemImage: "arrow.up.arrow.down") { edit.reordering = true }
            Divider()
            Button(block.group == nil ? "Remove Exercise" : "Remove \(block.title)", systemImage: "trash", role: .destructive) {
                change(edit) { $0.removeBlocks(at: [index]) }
            }
        } label: {
            RowsGlassIcon(systemImage: "ellipsis")
        }
        .rowsGlassCircle(tint: .accentColor)
        .accessibilityLabel("\(block.spokenTitle) options")
    }

    private func change(_ edit: PlanEditState, _ body: (inout RoutineEditor) -> Void) {
        withAnimation(.smooth(duration: 0.25)) { edit.model.change(body) }
    }

    // MARK: - Lines

    private func isLine(_ item: RowsItem) -> Bool {
        guard case let .set(line) = item else { return false }
        return !isOpen(line)
    }

    private func isOpen(_ line: RowsSetItem) -> Bool {
        guard let edit, let ref = plan.ref(line.id) else { return false }
        return edit.openSet == ref
    }

    @ViewBuilder
    private func itemView(_ item: RowsItem) -> some View {
        switch item {
        case let .set(line):
            lineView(line)
        case let .round(header):
            RowsRoundHeaderView(header: header, isPlan: edit == nil)
        case let .chainHeader(header):
            RowsChainHeaderView(header: header, isPlan: edit == nil)
        case .warmups, .miniSets:
            EmptyView()
        }
    }

    @ViewBuilder
    private func lineView(_ line: RowsSetItem) -> some View {
        let ref = plan.ref(line.id)
        if let edit, let ref, edit.openSet == ref, let editor = editor(ref, edit) {
            PlanSetPanel(
                editor: editor, apply: { edit.apply($0, to: ref) }, customRest: { edit.customRest = ref },
                close: { edit.openSet = nil }
            ) {
                setMenu(ref, edit)
            }
            .id(line.id)
        } else if let edit, let ref {
            RowsSetLine(item: tappable(line), tapHint: "Opens it to change its targets") {
                edit.openSet = ref
            } menuItems: {
                setMenu(ref, edit)
            }
            .id(line.id)
        } else {
            RowsSetLine(item: line, isPlan: edit == nil, onTap: {}) {
                EmptyView()
            }
        }
    }

    /// The line drawn as one that opens on a tap.
    private func tappable(_ line: RowsSetItem) -> RowsSetItem {
        var item = line
        item.tap = .fix
        return item
    }

    private func editor(_ ref: RoutinePlanSetRef, _ edit: PlanEditState) -> RoutinePlanSetEditor? {
        RoutinePlanPresenter.editor(
            ref, in: edit.model.routine, exercises: app.exercises, history: app.historyIndex,
            rests: app.settings.session, defaultUnit: app.settings.session.defaultUnit)
    }

    @ViewBuilder
    private func setMenu(_ ref: RoutinePlanSetRef, _ edit: PlanEditState) -> some View {
        let role = edit.model.editor.slot(ref.slot)?.sets.first { $0.key == ref.set }?.role
        if role == .warmup {
            Button("Make Working Set", systemImage: "flame") { change(edit) { $0.setRole(ref.slot, ref.set, .working) } }
        } else {
            Button("Make Warm-up", systemImage: "thermometer.low") { change(edit) { $0.setRole(ref.slot, ref.set, .warmup) } }
        }
        Button("Move Up", systemImage: "arrow.up") { withAnimation { edit.move(ref, by: -1) } }
            .disabled(edit.neighbour(ref, by: -1) == nil)
        Button("Move Down", systemImage: "arrow.down") { withAnimation { edit.move(ref, by: 1) } }
            .disabled(edit.neighbour(ref, by: 1) == nil)
        Button("More Options\u{2026}", systemImage: "slider.horizontal.3") { edit.route = .set(slot: ref.slot, set: ref.set) }
        Divider()
        Button("Delete Set", systemImage: "trash", role: .destructive) { withAnimation { edit.remove(ref) } }
    }

    // MARK: - Add rows

    @ViewBuilder
    private func addRows(_ edit: PlanEditState) -> some View {
        if block.group == nil, let member = block.members.first {
            HStack(spacing: 0) {
                addButton("Add set", label: "Add a set to \(member.name)", tint: .accentColor) {
                    change(edit) { $0.addSet(to: member.slot) }
                }
                addButton("Add warm-up", label: "Add a warm-up to \(member.name)", tint: RowsStyle.orange) {
                    change(edit) { $0.addSet(to: member.slot, role: .warmup) }
                }
            }
        } else {
            ForEach(block.members) { member in
                addButton("Add \(member.name) set", label: "Add a set to \(member.name)", tint: .accentColor) {
                    change(edit) { $0.addSet(to: member.slot) }
                }
            }
        }
    }

    private func addButton(_ title: String, label: String, tint: Color, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Image(systemName: "plus")
                    .font(.body.weight(.medium))
                    .frame(width: 22)
                Text(title)
                    .font(.subheadline.weight(.medium))
                    .multilineTextAlignment(.leading)
            }
            .foregroundStyle(tint)
            .padding(.horizontal, 16)
            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
    }
}

extension View {
    /// What an edit on the plan opens: the exercise picker, the reorder
    /// sheet, a custom rest, and the settings pages of an exercise, a group
    /// or a set (the editor's own, `RoutineEditorRoute`).
    func planEditing(_ edit: PlanEditState?) -> some View {
        modifier(PlanEditSheets(edit: edit))
    }
}

private struct PlanEditSheets: ViewModifier {
    let edit: PlanEditState?

    func body(content: Content) -> some View {
        if let edit {
            content.modifier(Presented(edit: edit))
        } else {
            content
        }
    }

    private struct Presented: ViewModifier {
        @Bindable var edit: PlanEditState
        @Environment(SubstrateLink.self) private var link

        func body(content: Content) -> some View {
            content
                .sheet(item: $edit.picker) { picker in
                    ExercisePicker(title: title(picker)) { exercise in
                        pick(exercise, for: picker)
                    }
                }
                .sheet(isPresented: $edit.reordering) {
                    PlanReorderSheet(model: edit.model)
                }
                .sheet(
                    isPresented: Binding {
                        edit.customRest != nil
                    } set: {
                        if !$0 { edit.customRest = nil }
                    }
                ) {
                    if let ref = edit.customRest {
                        let current = edit.model.editor.slot(ref.slot)?.sets.first { $0.key == ref.set }?.rest
                        RestPickerSheet(title: "Rest after this set", initial: current ?? 90) { seconds in
                            edit.apply(.rest(seconds), to: ref)
                        }
                    }
                }
                .navigationDestination(item: $edit.route) { route in
                    RoutineEditorDestination(model: edit.model, route: route)
                }
                .navigationDestination(for: RoutineEditorRoute.self) { route in
                    RoutineEditorDestination(model: edit.model, route: route)
                }
        }

        private func title(_ picker: PlanPicker) -> String {
            switch picker {
            case .add(nil): "Add exercise"
            case let .add(group?): "Add to \(edit.model.editor.group(group)?.groupKind.label.lowercased() ?? "group")"
            case .replace: "Replace exercise"
            }
        }

        private func pick(_ exercise: Exercise, for picker: PlanPicker) {
            withAnimation(.smooth(duration: 0.25)) {
                switch picker {
                case let .add(group):
                    edit.model.change { $0.addExercise(exercise, toGroup: group) }
                case let .replace(slot):
                    if edit.openSet?.slot == slot { edit.openSet = nil }
                    edit.model.change { $0.replaceExercise(slot, with: exercise.id) }
                }
            }
            sendIfLocal(exercise, link: link)
        }
    }
}

/// An editor page pushed from the plan: an exercise's, a group's or a set's.
struct RoutineEditorDestination: View {
    let model: RoutineEditorModel
    let route: RoutineEditorRoute

    var body: some View {
        switch route {
        case let .slot(key): SlotEditorView(model: model, slotKey: key)
        case let .group(key): GroupEditorView(model: model, groupKey: key)
        case let .set(slot, set): PlannedSetEditorView(model: model, slotKey: slot, setKey: set)
        }
    }
}

/// The exercises and groups as a list to drag into a new order.
private struct PlanReorderSheet: View {
    let model: RoutineEditorModel
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                ForEach(model.editor.blocks) { block in
                    VStack(alignment: .leading, spacing: 2) {
                        if let key = block.group, let group = model.editor.group(key) {
                            Text(group.groupKind.label)
                                .font(.headline)
                            Text(block.slots.map(name).joined(separator: ", "))
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        } else {
                            Text(block.slots.first.map(name) ?? "")
                                .font(.headline)
                        }
                    }
                    .padding(.vertical, 2)
                    .accessibilityElement(children: .combine)
                }
                .onMove { source, destination in model.change { $0.moveBlocks(from: source, to: destination) } }
            }
            .environment(\.editMode, .constant(.active))
            .navigationTitle("Reorder")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func name(_ slotKey: String) -> String {
        guard let slot = model.editor.slot(slotKey) else { return slotKey }
        return app.exercises[slot.exercise]?.name ?? slot.exercise.rawValue
    }
}
