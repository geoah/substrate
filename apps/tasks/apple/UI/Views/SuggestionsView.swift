import SubstrateScreens
import SubstrateUI
import SwiftUI
import TasksCore

/// The suggestions: each row with Dismiss and Accept, swiped either way on
/// a phone, and Select to pick several for one decision.
struct SuggestionsView: View {
    @Environment(TasksShell.self) private var shell
    @Environment(\.horizontalSizeClass) private var sizeClass
    let queue: QueuePresentation
    let app: TasksPresentation
    let link: LinkPresentation<TasksPresentation>

    var body: some View {
        List(selection: selection) {
            Section {
                Text(queue.intro)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets(top: 0, leading: 4, bottom: 4, trailing: 4))
            }
            if let empty = queue.empty {
                Section {
                    VStack(spacing: 12) {
                        Image(systemName: "sparkle").font(.largeTitle).foregroundStyle(.secondary).accessibilityHidden(true)
                        Text(empty.title).font(.title2.weight(.semibold))
                        Text(empty.message).foregroundStyle(.secondary).multilineTextAlignment(.center)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 24)
                    .listRowBackground(Color.clear)
                }
            } else {
                Section {
                    ForEach(queue.rows.rows) { row in
                        let selected = app.suggestion?.id == row.id
                        Group {
                            if sizeClass == .compact {
                                TaskRowView(row: row, open: { open(row) }, selected: selected)
                            } else {
                                TaskRowView(row: row, selected: selected).tag(row.id)
                            }
                        }
                        .listRowBackground(selected ? Color.brandAccent.opacity(0.08) : TasksSymbols.card)
                        .listRowInsets(EdgeInsets(top: 2, leading: 16, bottom: 2, trailing: 6))
                        .swipeActions(edge: .leading) {
                            if !row.pending, row.decisions.contains(where: { $0.action == .accept }) {
                                Button(app.list?.rowMenu.accept ?? "Accept") { shell.send(.accept(row.id)) }.tint(Color.brandAccent)
                            }
                        }
                        .swipeActions(edge: .trailing) {
                            if !row.pending, row.decisions.contains(where: { $0.action == .dismiss }) {
                                Button(app.list?.rowMenu.dismiss ?? "Dismiss") { shell.send(.dismiss(row.id)) }
                            }
                        }
                    }
                }
            }
        }
        #if os(iOS)
        .listStyle(.insetGrouped)
        #else
        .listStyle(.inset)
        #endif
        .scrollContentBackground(.hidden)
        .background { TasksSymbols.screen }
        .navigationTitle(queue.title)
        .safeAreaInset(edge: .top, spacing: 0) {
            Notices(link: link, app: app).padding(.horizontal)
        }
        .safeAreaInset(edge: .bottom) {
            if queue.picking, let picked = queue.picked {
                HStack(spacing: 12) {
                    Text(picked).font(.subheadline).foregroundStyle(.secondary)
                    Spacer()
                    if let dismiss = queue.dismissPicked, let accept = queue.acceptPicked {
                        Button(dismiss) { shell.send(.dismissPicked) }
                            .buttonStyle(.bordered)
                        Button(accept) { shell.send(.acceptPicked) }
                            .buttonStyle(.borderedProminent)
                    }
                }
                .padding(.horizontal, 20)
                .font(.body.weight(.semibold))
                .controlSize(.large)
                .buttonBorderShape(.capsule)
                .padding(.bottom, 12)
            }
        }
        .animation(.snappy(duration: 0.3), value: queue.rows.rows.map(\.id))
        .refreshable { shell.send(.refresh) }
        .toolbar {
            if queue.empty == nil {
                ToolbarItem(placement: .primaryAction) {
                    Button(queue.select) { shell.send(.pick(!queue.picking)) }
                }
            }
        }
    }

    private func open(_ row: TaskRow) {
        if queue.picking { shell.send(.picked(row.id)) } else { shell.send(.open(task: row.id)) }
    }

    private var selection: Binding<String?> {
        Binding(
            get: { sizeClass == .compact ? nil : app.suggestion?.id },
            set: { task in
                guard sizeClass != .compact else { return }
                shell.send(.open(task: task))
            })
    }
}

/// One suggestion: its collection and when it was written, its title, the
/// fields set before accepting, its description under its label, every
/// other property under Details, and Dismiss and Accept docked.
struct SuggestionView: View {
    @Environment(TasksShell.self) private var shell
    let suggestion: SuggestionPresentation

    var body: some View {
        Form {
            Section {
                VStack(alignment: .leading, spacing: 10) {
                    Label(suggestion.header, systemImage: "sparkle")
                        .labelStyle(TightLabelStyle(spacing: 6))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Text(suggestion.title)
                        .font(.title2.weight(.semibold))
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                    if let pending = suggestion.pendingWords, suggestion.pending {
                        Text(pending).font(.subheadline.weight(.semibold)).foregroundStyle(Color.brandAccent)
                    }
                }
                .padding(.vertical, 4)
            }
            if !suggestion.fields.isEmpty {
                Section {
                    ForEach(suggestion.fields) { field in
                        if let choice = field.choice, let property = field.property {
                            Picker(field.label, selection: Binding(get: { choice.selected }, set: { set(property, $0) })) {
                                ForEach(choice.options) { Text($0.title).tag($0.id) }
                            }
                        } else {
                            LabeledContent(field.label, value: field.value)
                        }
                    }
                }
            }
            if let description = suggestion.description {
                Section(description.label) {
                    Text(description.value).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            Section(suggestion.detailsTitle) {
                ForEach(suggestion.details) { fact in DetailFactRow(fact: fact) }
            }
        }
        .formStyle(.grouped)
        .scrollContentBackground(.hidden)
        .background { TasksSymbols.screen }
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .toolbar {
            if let place = suggestion.place {
                ToolbarItem(placement: .primaryAction) { Text(place).foregroundStyle(.secondary) }
            }
        }
        .safeAreaInset(edge: .bottom) {
            HStack(spacing: 12) {
                ForEach(suggestion.actions, id: \.self) { action in
                    if action.action == .accept {
                        Button {
                            shell.perform(action.action, on: suggestion.id)
                        } label: {
                            Text(action.title).frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.borderedProminent)
                    } else {
                        Button {
                            shell.perform(action.action, on: suggestion.id)
                        } label: {
                            Text(action.title).frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.bordered)
                    }
                }
            }
            .font(.body.weight(.semibold))
            .controlSize(.large)
            .buttonBorderShape(.capsule)
            .padding(.horizontal, 20)
            .padding(.bottom, 8)
        }
    }

    private func set(_ property: String, _ id: String) {
        shell.send(.setSuggestion(suggestion.id, property: property, to: id.isEmpty ? nil : .string(id)))
    }
}
