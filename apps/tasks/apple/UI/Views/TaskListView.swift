import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// Lists retain native selection, swipe actions and manual reordering. The
/// rows sit in one card per section, hairlines between them: To Do and Done
/// in a list; on Needs you one card per band under its heading, the chips
/// above and the Later band faded under a fold.
struct TaskListView: View {
    @Environment(TasksShell.self) private var shell
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.addsFromTabBar) private var addsFromTabBar
    let list: ListPresentation
    let app: TasksPresentation
    let link: LinkPresentation<TasksPresentation>
    @State private var naming: ListNaming?

    var body: some View {
        List(selection: selection) { rows }
            .grouped()
            .scrollContentBackground(.hidden)
            .background { TasksSymbols.screen }
            .navigationTitle(list.title)
            .safeAreaInset(edge: .top, spacing: 0) {
                Notices(link: link, app: app).padding(.horizontal)
            }
            .animation(.snappy(duration: 0.3), value: list.open.rows.map(\.id))
            .refreshable { shell.send(.refresh) }
            .safeAreaInset(edge: .bottom) {
                if !addsFromTabBar, list.quickAdd != nil, list.empty == nil || !list.closed.rows.isEmpty {
                    NewTaskButton(title: list.newTask)
                        .frame(maxWidth: .infinity, alignment: .trailing)
                        .padding(.horizontal, 20)
                        .padding(.bottom, 12)
                }
            }
            .toolbar { toolbar }
            .listNaming($naming)
    }

    /// On a phone a row is a button that pushes the task; beside a detail
    /// column, the open task is the list's selection.
    private var selection: Binding<String?> {
        Binding(
            get: { sizeClass == .compact ? nil : app.detail?.id },
            set: { task in
                // On a phone a row is a button and the list keeps no selection:
                // what it writes back is no choice of the owner's.
                guard sizeClass != .compact else { return }
                shell.send(.open(task: task))
            })
    }

    @ViewBuilder
    private var rows: some View {
        if !list.chips.isEmpty {
            Section {
                // Pulled out past the card's margin, so the chips start at
                // the cards' edge and scroll off the screen's.
                ChipsBar(chips: list.chips)
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
                    .listRowInsets(EdgeInsets(top: 0, leading: -20, bottom: 0, trailing: -20))
            }
            .tight()
        }
        if let queue = list.queue {
            Section {
                CountedRowView(row: queue, symbol: "suggestion")
                    .listRowBackground(TasksSymbols.card)
            }
        }
        if let empty = list.empty, list.closed.rows.isEmpty {
            Section {
                VStack(spacing: 16) {
                    Image(systemName: "checkmark.circle")
                        .font(.largeTitle)
                        .foregroundStyle(.secondary)
                        .accessibilityHidden(true)
                    Text(empty.title).font(.title2.weight(.semibold))
                    Text(empty.message)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                    if list.quickAdd != nil { NewTaskButton(title: list.newTask) }
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 24)
                .plainRow(top: 32, bottom: 24)
            }
        }
        if !list.bands.isEmpty {
            if let clear = list.clear {
                Section {
                    Label(clear, systemImage: "checkmark.circle")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .plainRow(top: 8, bottom: 0)
                }
                .tight()
            }
            ForEach(list.bands, id: \.band) { band in
                Section {
                    ForEach(list.open.rows.filter { $0.band == band.band }) { row in rowView(row) }
                } header: {
                    if band.band == .later {
                        fold(band.title)
                    } else {
                        heading(band.title, count: band.count, note: nil)
                    }
                }
            }
        } else if !list.open.rows.isEmpty {
            Section {
                ForEach(list.open.rows) { row in rowView(row) }
                    .onMove(perform: reorder)
            } header: {
                if list.showsClosed, !list.closed.rows.isEmpty {
                    heading(list.openTitle, count: nil, note: list.orderNote)
                } else if let note = list.orderNote {
                    Text(note).font(.footnote).foregroundStyle(.secondary).textCase(nil)
                }
            }
        }
        if list.showsClosed, !list.closed.rows.isEmpty {
            Section {
                ForEach(list.closed.rows) { row in rowView(row) }
            } header: {
                heading(list.closedTitle, count: list.closed.rows.count, note: nil)
            }
        }
        if let noDate = list.noDate {
            Section {
                CountedRowView(row: noDate, symbol: "nodate")
                    .listRowBackground(TasksSymbols.card)
            }
        }
    }

    /// A section's name, its count in a pill beside it, and what orders it.
    private func heading(_ title: String, count: Int?, note: String?) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(title).font(.subheadline.weight(.semibold)).foregroundStyle(Color.primary)
                if let count {
                    Text(count, format: .number)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 7)
                        .padding(.vertical, 2)
                        .background(Color.primary.opacity(0.06), in: .capsule)
                }
            }
            if let note { Text(note).font(.footnote).foregroundStyle(.secondary) }
        }
        .textCase(nil)
        .padding(.top, 4)
    }

    /// The Later band's fold: its name between two dashed lines.
    private func fold(_ title: String) -> some View {
        HStack(spacing: 10) {
            Line()
            Text(title.uppercased())
                .font(.caption.weight(.semibold))
                .tracking(0.6)
                .foregroundStyle(.tertiary)
            Line()
        }
        .textCase(nil)
        .padding(.top, 10)
        .padding(.bottom, 2)
        .accessibilityElement(children: .combine)
    }

    /// A row of its section's card that opens its task, beside its round
    /// mark; the open task's row is washed with the accent.
    @ViewBuilder
    private func rowView(_ row: TaskRow) -> some View {
        let selected = app.detail?.id == row.id
        Group {
            if sizeClass == .compact {
                TaskRowView(row: row, open: { shell.send(.open(task: row.id)) }, selected: selected)
            } else {
                TaskRowView(row: row, selected: selected).tag(row.id)
            }
        }
        .listRowBackground(selected ? Color.brandAccent.opacity(0.08) : TasksSymbols.card)
        .listRowInsets(EdgeInsets(top: 2, leading: 16, bottom: 2, trailing: 6))
        .swipeActions(edge: .leading) {
            if row.suggestion, !row.pending, row.decisions.contains(where: { $0.action == .accept }) {
                Button(list.rowMenu.accept) { shell.send(.accept(row.id)) }.tint(Color.brandAccent)
            } else if let toggle = row.toggle {
                Button(toggle) { shell.send(row.standing == .open && !row.pending ? .complete(row.id) : .reopen(row.id)) }
                    .tint(.green)
            }
        }
        // Deleting is never a full swipe: it takes the revealed button.
        .swipeActions(edge: .trailing, allowsFullSwipe: row.suggestion) {
            if row.suggestion, !row.pending, row.decisions.contains(where: { $0.action == .dismiss }) {
                Button(list.rowMenu.dismiss) { shell.send(.dismiss(row.id)) }
            } else {
                if row.editable {
                    Button(list.rowMenu.swipeDelete, role: .destructive) { shell.send(.deleteTask(row.id)) }
                }
                if row.standing == .open, row.decisions.contains(where: { $0.action == .abandon }) || row.toggle != nil {
                    Button(list.rowMenu.abandon) { shell.send(.abandon(row.id)) }
                }
            }
        }
        .contextMenu {
            TaskMenu(
                task: row.id, standing: row.standing, pending: row.pending, editable: row.editable,
                priority: row.priority?.priority ?? .none, sidebar: app.sidebar, words: list.rowMenu)
        }
    }

    /// Dragging, only where the order is the owner's.
    private var reorder: ((IndexSet, Int) -> Void)? {
        guard list.reorders else { return nil }
        return { from, to in move(from, to) }
    }

    private func move(_ from: IndexSet, _ to: Int) {
        guard let source = from.first else { return }
        shell.send(.reorderTask(list.open.rows[source].id, to: to > source ? to - 1 : to))
    }

    /// The order, as Settings offers it: one choice for every list.
    private var sort: (label: String, selected: String, options: [SettingsOption])? {
        for section in app.settings {
            for row in section.rows where row.id == .app(TasksSettings.order) {
                if case let .choice(selected, options) = row.control { return (row.label, selected, options) }
            }
        }
        return nil
    }

    /// Needs you is every open task in its bands: nothing to sort or show.
    private var offersViewOptions: Bool { list.selection != .needsYou }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        if let pin = list.pin {
            ToolbarItem(placement: .primaryAction) {
                Button {
                    switch pin.action {
                    case let .pin(path): shell.send(.pin(path))
                    case let .unpin(path): shell.send(.unpin(path))
                    }
                } label: {
                    if case .unpin = pin.action {
                        Label(pin.title, systemImage: "pin.fill")
                    } else {
                        Label(pin.title, systemImage: "pin")
                    }
                }
            }
        }
        if offersViewOptions || !list.actions.isEmpty {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    if offersViewOptions {
                        if let sort {
                            Picker(
                                selection: Binding(
                                    get: { sort.selected },
                                    set: { shell.send(.settings(.choose(row: .app(TasksSettings.order), option: $0))) })
                            ) {
                                ForEach(sort.options, id: \.id) { Text($0.label).tag($0.id) }
                            } label: {
                                Label(sort.label, systemImage: "arrow.up.arrow.down")
                            }
                            .pickerStyle(.menu)
                        }
                        Button(list.toggleClosed, systemImage: list.showsClosed ? "eye.slash" : "eye") {
                            shell.send(.settings(.toggle(row: .app(TasksSettings.showCompleted), on: !list.showsClosed)))
                        }
                    }
                    if !list.actions.isEmpty {
                        Divider()
                        ForEach(list.actions, id: \.self) { action in
                            switch action.action {
                            case let .rename(id):
                                Button(action.title, systemImage: "pencil") { naming = ListNaming(id: id, name: list.title) }
                            case let .delete(id):
                                Button(action.title, systemImage: "trash", role: .destructive) { shell.send(.deleteList(id)) }
                            }
                        }
                    }
                } label: {
                    Label("List Options", systemImage: "ellipsis")
                }
            }
        }
    }
}

/// What a task offers from its row's context menu: its state, its priority,
/// its list, edit and delete, in the presentation's words.
struct TaskMenu: View {
    @Environment(TasksShell.self) private var shell
    let task: String
    let standing: KindStatus.Standing
    var pending = false
    var editable = true
    let priority: TaskPriority
    let sidebar: SidebarPresentation
    let words: TaskMenuWords

    var body: some View {
        if pending {
            Button(words.undo, systemImage: "arrow.uturn.backward") { shell.send(.undo(task)) }
        } else if standing == .proposed {
            Button(words.accept, systemImage: "checkmark.circle") { shell.send(.accept(task)) }
            Button(words.dismiss, systemImage: "xmark.circle") { shell.send(.dismiss(task)) }
        } else if standing == .open {
            Button(words.complete, systemImage: "checkmark.circle") { shell.send(.complete(task)) }
            Button(words.abandon, systemImage: "xmark.circle") { shell.send(.abandon(task)) }
        } else {
            Button(words.reopen, systemImage: "circle") { shell.send(.reopen(task)) }
        }
        if editable { editing }
    }

    @ViewBuilder
    private var editing: some View {
        Button(words.edit, systemImage: "pencil") { shell.send(.edit(task)) }
        Menu(words.priority) {
            ForEach(words.priorities) { level in
                Toggle(
                    level.title,
                    isOn: Binding(
                        get: { priority.rawValue == level.id },
                        set: { _ in shell.send(.setPriority(task, TaskPriority(rawValue: level.id) ?? .none)) }))
            }
        }
        Menu(words.move) {
            Button(InboxWords.title) { shell.send(.move(task, toList: nil)) }
            ForEach(sidebar.sections.filter(\.reorders).flatMap { ($0.rows + $0.quiet).filter { $0.selection.listID != nil } }) { row in
                Button(row.title) { shell.send(.move(task, toList: row.selection.listID)) }
            }
        }
        Divider()
        Button(words.delete, systemImage: "trash", role: .destructive) { shell.send(.deleteTask(task)) }
    }
}

struct NewTaskButton: View {
    @Environment(TasksShell.self) private var shell
    let title: String

    var body: some View {
        Button(title, systemImage: "plus") { shell.send(.newTask) }
            .font(.body.weight(.semibold))
            .buttonStyle(.glassProminent)
            .buttonBorderShape(.capsule)
            .controlSize(.large)
    }
}

/// Needs you's chips: All, then each list, one chosen, in a row that
/// scrolls where they do not fit.
private struct ChipsBar: View {
    @Environment(TasksShell.self) private var shell
    let chips: [FilterChip]

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(chips) { chip in
                    Button {
                        shell.send(.filter(chip.selection))
                    } label: {
                        Text("\(chip.title)\(Text(chip.count.map { "  \($0)" } ?? "").foregroundStyle(.secondary))")
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(chip.selected ? TasksSymbols.card : Color.primary)
                            .padding(.horizontal, 13)
                            .padding(.vertical, 7)
                            .background(chip.selected ? Color.primary : Color.primary.opacity(0.07), in: .capsule)
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(chip.selected ? .isSelected : [])
                }
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 4)
        }
        .animation(.snappy(duration: 0.2), value: chips.map(\.selected))
    }
}

/// A row that stands for a list of its own: the suggestions, the tasks with
/// no date; its mark, its title and words, its count, and a disclosure.
struct CountedRowView: View {
    @Environment(TasksShell.self) private var shell
    let row: CountedRow
    let symbol: String

    var body: some View {
        Button {
            shell.send(.show(row.selection))
        } label: {
            HStack(spacing: 14) {
                Image(systemName: TasksSymbols.symbol(symbol))
                    .font(.title3)
                    .foregroundStyle(Color.brandAccent)
                    .frame(width: 28)
                VStack(alignment: .leading, spacing: 3) {
                    Text(row.title).font(.body.weight(.semibold)).foregroundStyle(Color.primary)
                    Text(row.detail).font(.subheadline).foregroundStyle(.secondary)
                }
                Spacer(minLength: 8)
                if symbol == "nodate" { Text(row.count, format: .number).foregroundStyle(.secondary) }
                Image(systemName: "chevron.right").font(.footnote.weight(.semibold)).foregroundStyle(Color.secondary.opacity(0.6))
            }
            .padding(.vertical, 6)
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }
}

/// A fold's dashed rule.
private struct Line: View {
    var body: some View {
        Rectangle()
            .fill(.clear)
            .frame(height: 1)
            .overlay {
                Path { path in
                    path.move(to: .zero)
                    path.addLine(to: CGPoint(x: 4000, y: 0))
                }
                .stroke(Color.secondary.opacity(0.35), style: StrokeStyle(lineWidth: 1, dash: [3, 4]))
            }
            .clipped()
    }
}

extension EnvironmentValues {
    /// The tab bar carries the round + (a phone), so a list draws none.
    @Entry var addsFromTabBar = false
}

private extension View {
    /// A row with no list chrome: bare words on the screen.
    func plainRow(top: CGFloat, bottom: CGFloat) -> some View {
        listRowBackground(Color.clear)
            .listRowSeparator(.hidden)
            .listRowInsets(EdgeInsets(top: top, leading: 16, bottom: bottom, trailing: 16))
    }

    /// A section with no gap before it (the chips, the all-clear line).
    func tight() -> some View {
        #if os(iOS)
        listSectionSpacing(0)
        #else
        self
        #endif
    }

    /// One card per section, hairlines between its rows: the system's
    /// inset grouped list on iOS, its inset list on the Mac.
    func grouped() -> some View {
        #if os(iOS)
        listStyle(.insetGrouped)
        #else
        listStyle(.inset)
        #endif
    }
}
