import SubstrateCore
import SubstrateRuntime
import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// One list's tasks as cards on a washed screen: what the list holds at a
/// glance, then To Do, in the order the presentation gives and dragged to
/// reorder where it allows, then Done when shown. Adding is the round +:
/// the tab bar's on a phone (`addsFromTabBar`), this column's own beside a
/// detail. The list's options are one tap away. Every change is a command.
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
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .background { TasksSymbols.screen }
            .overlay {
                if let empty = list.empty, list.closed.rows.isEmpty {
                    ContentUnavailableView(empty.title, systemImage: "checkmark.circle", description: Text(empty.message))
                        .padding(.top, 120)
                }
            }
            .navigationTitle(list.title)
            .refreshable { shell.send(.refresh) }
            .safeAreaInset(edge: .bottom) {
                if !addsFromTabBar {
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
        SummaryCard(summary: list.summary)
            .plainRow(top: 4, bottom: 10)
        if !list.open.rows.isEmpty {
            Section {
                heading(list.openTitle, count: list.open.rows.count, note: list.orderNote)
                ForEach(list.open.rows) { row in rowView(row) }
                    .onMove(perform: reorder)
            }
        }
        if list.showsClosed, !list.closed.rows.isEmpty {
            Section {
                heading(list.closedTitle, count: list.closed.rows.count, note: nil)
                ForEach(list.closed.rows) { row in rowView(row) }
            }
        }
    }

    /// A section's name, its count in a pill beside it, and what orders it.
    private func heading(_ title: String, count: Int?, note: String?) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(title).font(.title3.weight(.semibold))
            if let count {
                Text(count, format: .number)
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(.quaternary, in: .capsule)
            }
            Spacer()
            if let note { Text(note).font(.footnote).foregroundStyle(.secondary) }
        }
        .plainRow(top: 12, bottom: 2)
    }

    /// A card that opens its task, beside its round mark; the open task's
    /// card is outlined.
    @ViewBuilder
    private func rowView(_ row: TaskRow) -> some View {
        Group {
            if sizeClass == .compact {
                TaskRowView(row: row, open: { shell.send(.open(task: row.id)) }, selected: app.detail?.id == row.id)
            } else {
                TaskRowView(row: row, selected: app.detail?.id == row.id).tag(row.id)
            }
        }
        .plainRow(top: 5, bottom: 5)
        .swipeActions(edge: .leading) {
            Button(row.toggle) { shell.send(row.status == .open ? .complete(row.id) : .reopen(row.id)) }
                .tint(.green)
        }
        // Deleting is never a full swipe: it takes the revealed button.
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            Button(list.rowMenu.swipeDelete, role: .destructive) { shell.send(.deleteTask(row.id)) }
            if row.status == .open {
                Button(list.rowMenu.abandon) { shell.send(.abandon(row.id)) }
            }
        }
        .contextMenu { TaskMenu(task: row.id, status: row.status, priority: row.priority?.priority ?? .none, sidebar: app.sidebar, words: list.rowMenu) }
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

    /// Today is what is due, in due order and open: nothing to sort or show.
    private var offersViewOptions: Bool { list.selection != .today }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        if offersViewOptions || !list.actions.isEmpty {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    if offersViewOptions {
                        if let sort {
                            Picker(
                                sort.label,
                                selection: Binding(
                                    get: { sort.selected },
                                    set: { shell.send(.settings(.choose(row: .app(TasksSettings.order), option: $0))) })
                            ) {
                                ForEach(sort.options, id: \.id) { Text($0.label).tag($0.id) }
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
    let status: TaskStatus
    let priority: TaskPriority
    let sidebar: SidebarPresentation
    let words: TaskMenuWords

    var body: some View {
        if status == .open {
            Button(words.complete, systemImage: "checkmark.circle") { shell.send(.complete(task)) }
            Button(words.abandon, systemImage: "xmark.circle") { shell.send(.abandon(task)) }
        } else {
            Button(words.reopen, systemImage: "circle") { shell.send(.reopen(task)) }
        }
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
            ForEach(sidebar.fixed.filter { $0.selection != .today } + sidebar.lists.rows) { row in
                Button(row.title) { shell.send(.move(task, toList: row.selection.listID)) }
            }
        }
        Divider()
        Button(words.delete, systemImage: "trash", role: .destructive) { shell.send(.deleteTask(task)) }
    }
}

/// What a list holds at a glance: how many are left in the lists' serif,
/// the day in Today, and what is late.
private struct SummaryCard: View {
    let summary: ListSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(summary.headline)
                .font(.system(.title, design: .serif).weight(.semibold))
            if let detail = summary.detail {
                Text(detail).font(.subheadline).foregroundStyle(.secondary)
            }
            if let overdue = summary.overdue {
                Label(overdue, systemImage: "exclamationmark.circle.fill")
                    .labelStyle(TightLabelStyle(spacing: 5))
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.red)
            }
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            RoundedRectangle(cornerRadius: 24, style: .continuous)
                .fill(TasksSymbols.card)
                .shadow(color: .black.opacity(0.05), radius: 12, y: 4)
        }
        .accessibilityElement(children: .combine)
    }
}

/// Adding a task: a round, dark +, the one way in. It opens the editor,
/// seeded with the list shown (and today, in Today).
struct NewTaskButton: View {
    @Environment(TasksShell.self) private var shell
    let title: String

    var body: some View {
        Button {
            shell.send(.newTask)
        } label: {
            Image(systemName: "plus")
                .font(.system(size: 22, weight: .semibold))
                .foregroundStyle(TasksSymbols.card)
                .frame(width: 52, height: 52)
                .background(Circle().fill(Color.primary))
                .shadow(color: .black.opacity(0.2), radius: 10, y: 4)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
    }
}

extension EnvironmentValues {
    /// The tab bar carries the round + (a phone), so a list draws none.
    @Entry var addsFromTabBar = false
}

private extension View {
    /// A row with no list chrome: its own card, or bare words, on the screen.
    func plainRow(top: CGFloat, bottom: CGFloat) -> some View {
        listRowBackground(Color.clear)
            .listRowSeparator(.hidden)
            .listRowInsets(EdgeInsets(top: top, leading: 16, bottom: bottom, trailing: 16))
    }
}
