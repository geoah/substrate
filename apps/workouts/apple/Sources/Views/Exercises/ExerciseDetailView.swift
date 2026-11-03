import WorkoutsCore
import SwiftUI

/// One exercise (DESIGN.md §7.9): its facts stacked label over value, the
/// estimated 1RM trend (or the longest hold, or the most reps), and every
/// session with it, newest first, its sets in order with their labels.
struct ExerciseDetailView: View {
    let exerciseID: ExerciseID

    @Environment(AppModel.self) private var app
    @Environment(SessionController.self) private var session
    @State private var months = ExerciseTrend.ranges[1]
    @State private var chartSession: SessionID?

    var body: some View {
        // An exercise added in the gym and not saved to the catalogue lives in the session's snapshot.
        if let exercise = app.exercises[exerciseID] ?? session.state?.exercises[exerciseID] {
            let detail = ExercisePresenter.detail(exercise, history: app.historyIndex, settings: app.presenterSettings)
            list(detail)
                .navigationTitle(detail.name)
                .navigationBarTitleDisplayMode(.inline)
                .navigationDestination(item: $chartSession) { id in
                    SessionDetailView(sessionID: id)
                }
        } else {
            ContentUnavailableView("Exercise not found", systemImage: "list.bullet")
        }
    }

    private func list(_ detail: ExerciseDetailPresentation) -> some View {
        List {
            Section {
                ForEach(detail.facts) { fact in
                    LabelledValueView(fact)
                }
                if detail.isLocal {
                    Label(ExerciseRow.localText, systemImage: "iphone")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            if detail.trend.metric != .none {
                Section {
                    trend(detail.trend)
                } header: {
                    Text(detail.trend.title)
                        .textCase(nil)
                }
            }
            Section("History") {
                if detail.sessions.isEmpty {
                    Text("Not done yet.")
                        .foregroundStyle(.secondary)
                }
                ForEach(detail.sessions) { session in
                    NavigationLink(value: session.id) {
                        VStack(alignment: .leading, spacing: 4) {
                            HStack(spacing: 6) {
                                Text(session.title)
                                    .font(.subheadline.weight(.semibold))
                                if session.isDeload {
                                    Text("Deload")
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                }
                            }
                            ForEach(Array(session.lines.enumerated()), id: \.offset) { _, line in
                                Text(line)
                                    .font(.subheadline.monospacedDigit())
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .accessibilityElement(children: .combine)
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
    }

    @ViewBuilder
    private func trend(_ trend: ExerciseTrend) -> some View {
        let points = trend.points(lastMonths: months, now: .now, calendar: app.presenterSettings.calendar)
        Picker("Range", selection: $months) {
            ForEach(ExerciseTrend.ranges, id: \.self) { months in
                Text(months == 12 ? "1Y" : "\(months)M")
                    .accessibilityLabel(months == 12 ? "1 year" : "\(months) months")
                    .tag(months)
            }
        }
        .pickerStyle(.segmented)
        if trend.hasTrend(points) {
            E1RMChart(trend: trend, points: points) { chartSession = $0 }
                .frame(height: 200)
                .padding(.vertical, 4)
        } else {
            Text(ExerciseTrend.notEnough)
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
    }
}

#if DEBUG
#Preview {
    let app = PreviewData.app()
    NavigationStack {
        ExerciseDetailView(exerciseID: "leg-press-machine")
    }
    .environment(app)
    .environment(PreviewData.noSession(app))
}
#endif
