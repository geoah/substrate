import WorkoutsCore
import SwiftUI

/// The owner's exercises, A to Z, and every exercise once searched, by the
/// picker's rules (DESIGN.md §7.9). Each row: the name, then the equipment
/// and when it was last done.
struct ExercisesView: View {
    @Environment(AppModel.self) private var app
    @State private var query = ""

    var body: some View {
        NavigationStack {
            let sections = ExercisePresenter.owned(app.exercises, routines: app.catalog?.routines ?? [:], history: app.historyIndex,
                                                   query: query, settings: app.presenterSettings)
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
