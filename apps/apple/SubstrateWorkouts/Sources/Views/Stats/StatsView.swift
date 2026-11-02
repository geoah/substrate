import WorkoutsCore
import SwiftUI

/// Progress across every routine, or one of them: the routine page's stats
/// sections with a routine picker above them. The period and the routine
/// are kept between launches; a routine since deleted reads as every
/// routine.
struct StatsView: View {
    @Environment(AppModel.self) private var app
    @AppStorage("stats.period") private var period: StatsPeriod = .threeMonths
    /// A routine's id; empty for every routine.
    @AppStorage("stats.routine") private var routine = ""
    @State private var path = NavigationPath()

    var body: some View {
        NavigationStack(path: $path) {
            content
                .navigationTitle("Stats")
                .statsDestinations()
        }
    }

    @ViewBuilder
    private var content: some View {
        if !app.isHistoryLoaded {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if let catalog = app.catalog {
            let stats = StatsPresenter.present(app.history, catalog: catalog, scope: scope(catalog), period: period, now: .now,
                                               settings: app.presenterSettings)
            List {
                Section {
                    Picker("Routine", selection: Binding { scope(catalog).routineID?.rawValue ?? "" } set: { routine = $0 }) {
                        Text(StatsPresentation.allRoutines).tag("")
                        ForEach(RoutinePresenter.list(catalog, history: app.historyIndex, settings: app.presenterSettings).sections) { folder in
                            Section(folder.title) {
                                ForEach(folder.routines) { card in
                                    Text(card.name).tag(card.id.rawValue)
                                }
                            }
                        }
                    }
                    .pickerStyle(.menu)
                }
                StatsSections(stats: stats, period: $period)
            }
            .listStyle(.insetGrouped)
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func scope(_ catalog: Catalog) -> StatsScope {
        let id = RoutineID(routine)
        return routine.isEmpty || catalog.routines[id] == nil ? .all : .routine(id)
    }
}

private extension StatsScope {
    var routineID: RoutineID? {
        if case let .routine(id) = self { id } else { nil }
    }
}

#if DEBUG
#Preview("Every routine") {
    let app = PreviewData.app()
    StatsView()
        .environment(app)
        .environment(PreviewData.noSession(app))
}

#Preview("No sessions") {
    let app = AppModel(catalog: PreviewData.catalog, history: [])
    StatsView()
        .environment(app)
        .environment(PreviewData.noSession(app))
}
#endif
