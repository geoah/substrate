import WorkoutsCore
import SwiftUI

/// Add or replace an exercise (DESIGN.md §7.6): a search whose words match
/// names and aliases in any order (db, bb, kb, rdl and ohp understood),
/// "Recent" first, then A to Z, and "New exercise…" for one the catalogue
/// lacks. A `.large` sheet.
struct ExercisePicker: View {
    let title: String
    let pick: (Exercise) -> Void

    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var query = ""

    var body: some View {
        NavigationStack {
            let sections = ExercisePresenter.picker(app.exercises, history: app.historyIndex, query: query, now: .now,
                                                    settings: app.presenterSettings)
            List {
                ForEach(sections) { section in
                    Section(section.title) {
                        ForEach(section.rows) { row in
                            Button {
                                guard let exercise = app.exercises[row.id] else { return }
                                pick(exercise)
                                dismiss()
                            } label: {
                                ExerciseRowLabel(row: row)
                                    .contentShape(.rect)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
                Section {
                    NavigationLink {
                        NewExerciseSheet(initialName: query) { exercise in
                            pick(exercise)
                            dismiss()
                        }
                    } label: {
                        Label("New exercise…", systemImage: "plus")
                    }
                } footer: {
                    if sections.isEmpty, !query.isEmpty {
                        Text("No exercise matches \u{201C}\(query)\u{201D}.")
                    }
                }
            }
            .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: "Search exercises")
            .autocorrectionDisabled()
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", role: .cancel) { dismiss() }
                }
            }
        }
        .presentationDetents([.large])
    }
}

/// An exercise in a list: the name, and under it the equipment, when it was
/// last done and "On this iPhone", each on its own line.
struct ExerciseRowLabel: View {
    let row: ExerciseRow

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(row.name)
                .font(.body)
                .foregroundStyle(.primary)
            if let equipment = row.equipment {
                Text(equipment)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            if let lastDone = row.lastDone {
                Text(lastDone)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            if row.isLocal {
                Label(ExerciseRow.localText, systemImage: "iphone")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

#if DEBUG
#Preview {
    let app = PreviewData.app()
    Color.clear.sheet(isPresented: .constant(true)) {
        ExercisePicker(title: "Add exercise") { _ in }
            .environment(app)
    }
}
#endif
