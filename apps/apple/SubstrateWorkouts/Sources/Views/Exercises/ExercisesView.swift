import WorkoutsCore
import SwiftUI

/// Every exercise, A to Z, searchable by the picker's rules (DESIGN.md
/// §7.9). Each row: the name, then the equipment and when it was last done.
struct ExercisesView: View {
    @Environment(AppModel.self) private var app
    @State private var query = ""

    var body: some View {
        NavigationStack {
            let sections = ExercisePresenter.list(app.exercises, history: app.historyIndex, query: query,
                                                  settings: app.presenterSettings)
            List {
                ForEach(sections) { section in
                    Section(section.title) {
                        ForEach(section.rows) { row in
                            NavigationLink(value: row.id) {
                                ExerciseRowLabel(row: row)
                            }
                        }
                    }
                }
            }
            .listStyle(.insetGrouped)
            .overlay {
                if sections.isEmpty {
                    if query.isEmpty {
                        ContentUnavailableView("No exercises", systemImage: "list.bullet")
                    } else {
                        ContentUnavailableView.search(text: query)
                    }
                }
            }
            .searchable(text: $query, prompt: "Search exercises")
            .autocorrectionDisabled()
            .navigationTitle("Exercises")
            .navigationDestination(for: ExerciseID.self) { id in
                ExerciseDetailView(exerciseID: id)
            }
            .navigationDestination(for: SessionID.self) { id in
                SessionDetailView(sessionID: id)
            }
        }
    }
}

#if DEBUG
#Preview {
    let app = PreviewData.app()
    ExercisesView()
        .environment(app)
        .environment(PreviewData.noSession(app))
}
#endif
