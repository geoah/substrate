import WorkoutsCore
import SwiftUI

/// A finished session (DESIGN.md §7.8): when, the session RPE, the Apple
/// Health state with "Try again" when it was not saved, every exercise's
/// sets with their labels and rests, working sets per muscle, and notes.
/// Read-only in V1, apart from Delete.
struct SessionDetailView: View {
    let sessionID: SessionID

    @Environment(AppModel.self) private var app
    @Environment(SessionController.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var askDelete = false
    @State private var retrying = false
    @State private var deleteFailure: String?

    var body: some View {
        if let entry = app.history.first(where: { $0.id == sessionID }) {
            let detail = HistoryPresenter.detail(entry, exercises: app.exercises, settings: app.presenterSettings)
            list(detail)
                .navigationTitle(detail.title)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button("Delete", systemImage: "trash", role: .destructive) { askDelete = true }
                    }
                }
                .confirmationDialog(HistoryPresenter.deleteQuestion, isPresented: $askDelete, titleVisibility: .visible) {
                    Button("Delete", role: .destructive) {
                        // The page closes only once the session is gone.
                        do {
                            try app.deleteSession(sessionID)
                            dismiss()
                        } catch {
                            deleteFailure = error.localizedDescription
                        }
                    }
                    Button("Cancel", role: .cancel) {}
                }
                .alert("The session could not be deleted", isPresented: deleteFailureShown) {
                    Button("OK", role: .cancel) {}
                } message: {
                    Text(deleteFailure ?? "")
                }
        } else {
            ContentUnavailableView("Session not found", systemImage: "clock.arrow.circlepath")
        }
    }

    private var deleteFailureShown: Binding<Bool> {
        Binding { deleteFailure != nil } set: { if !$0 { deleteFailure = nil } }
    }

    private func list(_ detail: SessionDetailPresentation) -> some View {
        List {
            Section {
                VStack(alignment: .leading, spacing: 4) {
                    Text(detail.when)
                        .font(.subheadline)
                    Text(detail.summary)
                        .font(.subheadline.monospacedDigit())
                        .foregroundStyle(.secondary)
                    if let rpe = detail.sessionRPE {
                        Text(rpe)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
                if let health = detail.healthText {
                    HStack(spacing: 10) {
                        if let symbol = detail.health.symbol {
                            Image(systemName: symbol)
                                .foregroundStyle(detail.health.tint)
                                .accessibilityHidden(true)
                        }
                        Text(health)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        if detail.canRetryHealth {
                            Button("Try again") {
                                retrying = true
                                Task {
                                    await session.retryHealth(sessionID)
                                    retrying = false
                                }
                            }
                            .buttonStyle(.bordered)
                            .disabled(retrying)
                        } else if detail.health == .pending, session.healthAccess == .notDetermined {
                            // Never asked (the sheet did not show at Finish): §9.5's Connect, here too.
                            Button("Connect") {
                                retrying = true
                                Task {
                                    await session.connectHealth()
                                    retrying = false
                                }
                            }
                            .buttonStyle(.bordered)
                            .disabled(retrying)
                        }
                    }
                    .font(.subheadline)
                }
            }
            ForEach(detail.blocks) { block in
                Section {
                    ForEach(block.exercises) { exercise in
                        DetailExercise(exercise: exercise)
                    }
                } header: {
                    if let title = block.title {
                        Text(title)
                            .textCase(nil)
                    }
                }
            }
            if let muscles = detail.muscles {
                Section("Working sets per muscle") {
                    Text(muscles)
                        .font(.subheadline)
                }
            }
            if let notes = detail.notes {
                Section("Notes") {
                    Text(notes)
                        .font(.subheadline)
                }
            }
        }
        .listStyle(.insetGrouped)
    }
}

/// One exercise of a finished session: its name, then a line per set. The
/// whole card is the link to the exercise (§7.9), the row the list
/// highlights and clears on the way back.
private struct DetailExercise: View {
    let exercise: SessionDetailExercise

    var body: some View {
        NavigationLink(value: exercise.exercise) {
            content
        }
        .accessibilityHint("Shows the exercise's history")
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                if let label = exercise.groupLabel {
                    Text(label)
                        .fontWeight(.bold)
                        .foregroundStyle(.secondary)
                }
                Text(exercise.name)
            }
            .font(.headline)
            .foregroundStyle(.primary)
            ForEach(exercise.sets) { set in
                VStack(alignment: .leading, spacing: 2) {
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        // The label column is bounded (W, 2, 2a, 1L): fine as a column.
                        Text(set.label.short)
                            .font(.subheadline.weight(.semibold).monospacedDigit())
                            .foregroundStyle(set.label.short == "W" ? GroupPalette.warmup : .primary)
                            .frame(minWidth: 28, alignment: .leading)
                        Text(set.text)
                            .font(.subheadline.monospacedDigit())
                            .frame(maxWidth: .infinity, alignment: .leading)
                        if let rest = set.rest {
                            Text(rest)
                                .font(.footnote.monospacedDigit())
                                .foregroundStyle(.secondary)
                        }
                    }
                    if let notes = set.notes {
                        Text(notes)
                            .font(.footnote)
                            .italic()
                            .foregroundStyle(.secondary)
                            .padding(.leading, 38)
                    }
                }
                .accessibilityElement(children: .combine)
                .accessibilityLabel("\(set.label.spoken.capitalizedSentence), \(set.text)\(set.rest.map { ", \($0)" } ?? "")")
            }
            if let notes = exercise.notes {
                Text(notes)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
    }
}

#if DEBUG
#Preview {
    let app = PreviewData.app()
    NavigationStack {
        SessionDetailView(sessionID: PreviewData.history.first?.id ?? "none")
    }
    .environment(app)
    .environment(PreviewData.noSession(app))
}
#endif
