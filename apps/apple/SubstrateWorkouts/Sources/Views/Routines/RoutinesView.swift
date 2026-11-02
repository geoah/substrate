import WorkoutsCore
import WorkoutsSync
import SwiftUI

/// The owner's routines by folder (DESIGN.md §7.2): one collapsible section
/// per folder, a card per routine with its own Start, and a "+" for an empty
/// session. The console's agent writes them; signed out, the app shows the
/// sample routine it carries. A routine on the server is deleted by a swipe
/// or its menu, after a confirmation.
struct RoutinesView: View {
    @Environment(AppModel.self) private var app
    @Environment(SubstrateLink.self) private var link
    @State private var signingIn = false
    @State private var path: [RoutineID] = []
    @State private var collapsed: Set<String> = []
    @State private var deleting: RoutineToDelete?

    var body: some View {
        NavigationStack(path: $path) {
            content
                .navigationTitle("Routines")
                .toolbar {
                    ToolbarItem(placement: .topBarTrailing) {
                        StartSessionButton(request: .empty) {
                            Label("Start empty session", systemImage: "plus")
                        }
                    }
                }
                .navigationDestination(for: RoutineID.self) { id in
                    RoutinePreview(routineID: id)
                }
                .safeAreaInset(edge: .top) { banner }
                .signInSheet(isPresented: $signingIn)
        }
    }

    /// What stops the app from writing to its substrate, where the routines
    /// are: anything Settings would say needs the owner.
    @ViewBuilder
    private var banner: some View {
        switch link.syncStatus {
        case .packageNewer, .packageEdited, .packageRefused, .needsApproval, .signedOut, .serverTooOld, .notKept, .refused, .failed:
            VStack(alignment: .leading, spacing: 8) {
                Label(link.syncStatus.sentence, systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(.primary)
                    .fixedSize(horizontal: false, vertical: true)
                if case .needsApproval = link.syncStatus {
                    Button("Review Access") { Task { await link.reauthorize() } }
                        .font(.footnote.weight(.semibold))
                } else if case .signedOut = link.syncStatus {
                    Button("Sign In Again") { signingIn = true }
                        .font(.footnote.weight(.semibold))
                } else if case .refused = link.syncStatus {
                    Button("Review in Settings") { app.tab = .settings }
                        .font(.footnote.weight(.semibold))
                }
            }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.orange.opacity(0.15), in: .rect(cornerRadius: 14))
            .padding(.horizontal)
        default:
            EmptyView()
        }
    }

    @ViewBuilder
    private var content: some View {
        if let catalog = app.catalog {
            let list = RoutinePresenter.list(catalog, history: app.historyIndex, settings: app.presenterSettings)
            if list.isEmpty, app.catalogFailure != nil {
                // A build fault, not an empty folder: say so and point at the details.
                ContentUnavailableView {
                    Label("Routines could not be read", systemImage: "exclamationmark.triangle")
                } description: {
                    Text("This build\u{2019}s routines and exercises did not load. Settings lists what failed.")
                } actions: {
                    Button("Open Settings") { app.tab = .settings }
                        .buttonStyle(.borderedProminent)
                }
            } else if list.isEmpty, case let .server(host) = app.catalogOrigin {
                // A repository with no routines: the agent writes them. The
                // sample comes back only on request, once the owner deleted it.
                let offersSample = SampleInstall.offersSample(state: link.sampleState, catalog: catalog)
                ContentUnavailableView {
                    Label("No routines on \(host) yet", systemImage: "list.bullet.clipboard")
                } description: {
                    VStack(spacing: 8) {
                        Text(RoutineListPresentation.serverEmptyText)
                        if let problem = link.sampleProblem {
                            // The first run's write failed; the next sync tries again.
                            Text("The sample routine could not be added: \(problem) The app tries again at the next sync.")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                        if offersSample, let reason = link.writeBlockedReason {
                            Text(reason)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                } actions: {
                    if offersSample {
                        Button {
                            Task { await link.addSampleRoutine() }
                        } label: {
                            HStack(spacing: 8) {
                                if link.addingSample { ProgressView().controlSize(.small) }
                                Text("Add the sample routine")
                            }
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(link.addingSample || !link.canWrite)
                    }
                    StartSessionButton(request: .empty) {
                        Text("Start empty session")
                    }
                }
            } else if list.isEmpty {
                ContentUnavailableView {
                    Label("No routines yet", systemImage: "list.bullet.clipboard")
                } description: {
                    Text(RoutineListPresentation.emptyText)
                } actions: {
                    StartSessionButton(request: .empty) {
                        Text("Start empty session")
                    }
                    .buttonStyle(.borderedProminent)
                }
            } else {
                routineList(list)
            }
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func routineList(_ list: RoutineListPresentation) -> some View {
        List {
            ForEach(list.sections) { section in
                Section(isExpanded: expanded(section.title)) {
                    ForEach(section.routines) { card in
                        RoutineCard(card: card) { path.append(card.id) }
                            .swipeActions(edge: .trailing) {
                                if canDelete {
                                    Button("Delete", systemImage: "trash", role: .destructive) {
                                        deleting = RoutineToDelete(id: card.id, name: card.name)
                                    }
                                }
                            }
                            .contextMenu {
                                Button("Show Routine", systemImage: "list.bullet.clipboard") { path.append(card.id) }
                                if canDelete {
                                    Button("Delete Routine", systemImage: "trash", role: .destructive) {
                                        deleting = RoutineToDelete(id: card.id, name: card.name)
                                    }
                                }
                            }
                    }
                } header: {
                    folderHeader(section)
                }
            }
            if link.session == nil {
                Section {
                    Button("Sign In to Your Substrate…") { signingIn = true }
                } footer: {
                    Text("This sample comes with the app. Sign in to train from the routines your agent writes and keep your sessions there.")
                }
            } else if case let .server(host) = app.catalogOrigin {
                Section {} footer: {
                    Text("From \(host).")
                }
            }
            if let unreadable = list.unreadable {
                Section {
                    Button {
                        app.tab = .settings
                    } label: {
                        Label(unreadable, systemImage: "exclamationmark.triangle")
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
        .confirmsRoutineDelete($deleting)
    }

    /// Routines on the signed-in substrate can be deleted; the sample the
    /// app carries signed out cannot, since there is nothing to delete it from.
    private var canDelete: Bool {
        guard case .server = app.catalogOrigin else { return false }
        return link.canDeleteRoutines
    }

    private func expanded(_ folder: String) -> Binding<Bool> {
        Binding {
            !collapsed.contains(folder)
        } set: { isExpanded in
            if isExpanded { collapsed.remove(folder) } else { collapsed.insert(folder) }
        }
    }

    /// The folder's name and, as a separate secondary label, its count. The
    /// whole header folds the section.
    private func folderHeader(_ section: RoutineSection) -> some View {
        let isExpanded = !collapsed.contains(section.title)
        return Button {
            withAnimation { expanded(section.title).wrappedValue.toggle() }
        } label: {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Image(systemName: "chevron.right")
                    .font(.caption.weight(.semibold))
                    .rotationEffect(.degrees(isExpanded ? 90 : 0))
                    .accessibilityHidden(true)
                Text(section.title)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(.primary)
                    .multilineTextAlignment(.leading)
                Spacer(minLength: 8)
                Text(section.count)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            .textCase(nil)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(.isHeader)
        .accessibilityLabel("\(section.title), \(section.count)")
        .accessibilityValue(isExpanded ? "Expanded" : "Collapsed")
        .accessibilityHint(isExpanded ? "Folds the folder" : "Opens the folder")
    }
}

#if DEBUG
#Preview {
    let app = PreviewData.app()
    RoutinesView()
        .environment(app)
        .environment(PreviewData.noSession(app))
}
#endif
