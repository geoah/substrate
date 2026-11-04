import SwiftUI
import WorkoutsCore

/// Progress across every routine, or one of them: the routine page's
/// Progress (`StatsProgressView`) with a routine picker above it. The period
/// and the routine are kept between launches; a routine since deleted reads
/// as every routine.
struct StatsView: View {
    @Environment(AppModel.self) private var app
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
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    routinePicker(catalog)
                    StatsProgressView(scope: scope(catalog))
                }
                .padding(.horizontal, 16)
                .padding(.bottom, 24)
            }
            .background(Color(.systemGroupedBackground))
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    /// Which routine's sessions, as a label over its value with the menu's
    /// chevron: the one choice the page's numbers all depend on.
    private func routinePicker(_ catalog: Catalog) -> some View {
        let selection = Binding {
            scope(catalog).routineID?.rawValue ?? ""
        } set: {
            routine = $0
        }
        return Menu {
            Picker("Routine", selection: selection) {
                Text(StatsPresentation.allRoutines).tag("")
                ForEach(RoutinePresenter.list(catalog, history: app.historyIndex, settings: app.presenterSettings).sections) { folder in
                    Section(folder.title) {
                        ForEach(folder.routines) { card in
                            Text(card.name).tag(card.id.rawValue)
                        }
                    }
                }
            }
        } label: {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Showing")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    Text(title(catalog))
                        .font(.headline)
                        .foregroundStyle(.primary)
                        .multilineTextAlignment(.leading)
                }
                Spacer(minLength: 8)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 12)
            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
            .background(RowsStyle.card, in: .rect(cornerRadius: 20))
            .contentShape(.rect)
        }
        .accessibilityLabel("Routine")
        .accessibilityValue(title(catalog))
        .accessibilityHint("Picks whose sessions the stats count")
    }

    private func title(_ catalog: Catalog) -> String {
        scope(catalog).routineID.flatMap { catalog.routines[$0]?.name } ?? StatsPresentation.allRoutines
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

#Preview("Largest text, dark") {
    let app = PreviewData.app()
    StatsView()
        .environment(app)
        .environment(PreviewData.noSession(app))
        .environment(\.dynamicTypeSize, .accessibility3)
        .preferredColorScheme(.dark)
}
#endif
