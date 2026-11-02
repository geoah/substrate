import WorkoutsCore
import SwiftUI

/// The owner's routines by folder (DESIGN.md §7.2): one collapsible section
/// per folder, a card per routine with its own Start, and a "+" for an empty
/// session. Routines come with this build in V1; the console's agent writes
/// them.
struct RoutinesView: View {
    @Environment(AppModel.self) private var app
    @Environment(SubstrateLink.self) private var link
    @State private var signingIn = false
    @State private var path: [RoutineID] = []
    @State private var collapsed: Set<String> = []

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
                } else if case .packageEdited = link.syncStatus {
                    Button("Open Settings") { app.tab = .settings }
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
                // A repository with no routines yet: the agent writes them, or
                // the app copies the ones it carries.
                ContentUnavailableView {
                    Label("No routines on \(host) yet", systemImage: "list.bullet.clipboard")
                } description: {
                    VStack(spacing: 8) {
                        Text("Ask your agent for a routine, or start from the four full-body routines and thirty exercises this app carries.")
                        if let reason = link.writeBlockedReason {
                            Text(reason)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                } actions: {
                    Button {
                        Task { await link.copyStarterRoutines() }
                    } label: {
                        HStack(spacing: 8) {
                            if link.copyingStarter { ProgressView().controlSize(.small) }
                            Text("Add the Starter Routines")
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(link.copyingStarter || !link.canWrite)
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
                    }
                } header: {
                    folderHeader(section)
                }
            }
            if link.session == nil {
                Section {
                    Button("Sign In to Your Substrate…") { signingIn = true }
                } footer: {
                    Text("These routines come with the app. Sign in to train from your own and keep your sessions there.")
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
