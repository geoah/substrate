import WorkoutsCore
import SwiftUI

/// Finished sessions, newest first, in month sections (DESIGN.md §7.8). Each
/// row: the routine, then the date and a short summary on their own lines,
/// and a glyph for the Apple Health save. Swipe to delete asks first.
struct HistoryView: View {
    @Environment(AppModel.self) private var app
    /// Untyped: a session pushes its exercises (`ExerciseID`), and an
    /// exercise its sessions. A `[SessionID]` path refused the exercise and
    /// left the tapped card highlighted.
    @State private var path = NavigationPath()
    @State private var deleting: SessionID?
    @State private var failure: String?

    var body: some View {
        NavigationStack(path: $path) {
            content
                .navigationTitle("History")
                .navigationDestination(for: SessionID.self) { id in
                    SessionDetailView(sessionID: id)
                }
                .navigationDestination(for: ExerciseID.self) { id in
                    ExerciseDetailView(exerciseID: id)
                }
        }
        .confirmationDialog(HistoryPresenter.deleteQuestion, isPresented: deletingShown, titleVisibility: .visible) {
            Button("Delete", role: .destructive) {
                if let deleting { delete(deleting) }
                deleting = nil
            }
            Button("Cancel", role: .cancel) { deleting = nil }
        }
        .alert("The session could not be deleted", isPresented: failureShown) {
            Button("OK", role: .cancel) {}
        } message: {
            Text(failure ?? "")
        }
    }

    @ViewBuilder
    private var content: some View {
        if !app.isHistoryLoaded {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if app.history.isEmpty {
            ContentUnavailableView {
                Label(HistoryPresenter.emptyTitle, systemImage: "clock.arrow.circlepath")
            } description: {
                Text(HistoryPresenter.emptyMessage)
            }
        } else {
            List {
                ForEach(app.historySections) { section in
                    Section(section.title) {
                        ForEach(section.rows) { row in
                            NavigationLink(value: row.id) {
                                HistoryRowView(row: row)
                            }
                            .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                                Button(role: .destructive) {
                                    deleting = row.id
                                } label: {
                                    Label("Delete", systemImage: "trash")
                                }
                            }
                        }
                    }
                }
            }
            .listStyle(.insetGrouped)
        }
    }

    private func delete(_ id: SessionID) {
        // Swipe to delete is on the list itself, so nothing pushed shows the session.
        do {
            try app.deleteSession(id)
        } catch {
            failure = error.localizedDescription
        }
    }

    private var deletingShown: Binding<Bool> {
        Binding { deleting != nil } set: { if !$0 { deleting = nil } }
    }

    private var failureShown: Binding<Bool> {
        Binding { failure != nil } set: { if !$0 { failure = nil } }
    }
}

/// A session in the list: the title, then the date and the summary on their
/// own lines, the Health glyph trailing.
struct HistoryRowView: View {
    let row: HistoryRow

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(row.title)
                    .font(.headline)
                Text(row.date)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Text(row.summary)
                    .font(.subheadline.monospacedDigit())
                    .foregroundStyle(.secondary)
                if row.isDeload {
                    Text("Deload")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if let symbol = row.health.symbol {
                Image(systemName: symbol)
                    .foregroundStyle(row.health.tint)
                    .accessibilityLabel(row.health.accessibilityText)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

#if DEBUG
#Preview {
    let app = PreviewData.app()
    HistoryView()
        .environment(app)
        .environment(PreviewData.noSession(app))
}

#Preview("Empty") {
    let app = AppModel(catalog: PreviewData.catalog, history: [])
    HistoryView()
        .environment(app)
        .environment(PreviewData.noSession(app))
}
#endif
