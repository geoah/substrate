import SwiftUI

@main
struct SubstrateSyncApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var model = AppModel()
    @StateObject private var updates = Updater()
    @Environment(\.scenePhase) private var phase

    init() {
        #if DEBUG
        if SampleData.withoutHealth { Settings.setEnabled(.health, false) }
        #endif
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(model)
                .environmentObject(updates)
                .updateOffer(updates)
                .onChange(of: phase) { _, phase in
                    guard phase == .active else { return }
                    model.refreshAccess()
                    updates.checkIfDue()
                }
        }
    }
}

/// The server refused the token: what happened, and the one way out.
struct SessionExpiredView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        Form {
            Section {
                ErrorBox(
                    title: "Signed out by the server",
                    message: "\(model.session?.server.absoluteString ?? "The server") no longer accepts this phone's sign-in. The token was revoked, or the repository was reset. Sign in again to carry on; what this phone pushed is checked against the server and sent again where it is missing.")
            }
            Section {
                Button {
                    model.signOut()
                } label: {
                    WideLabel(title: "Sign In Again", systemImage: "person.badge.key")
                }
                .primaryAction()
                .buttonRow()
            }
        }
        .navigationTitle("Substrate Sync")
    }
}

/// Signed out, set up, or syncing: one screen at a time, the same layout on
/// iPhone and iPad.
struct RootView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        NavigationStack {
            Group {
                if model.session == nil {
                    SignInView()
                } else if model.sessionExpired {
                    SessionExpiredView()
                } else if !model.setupChecked || !model.setupComplete {
                    SetupView()
                } else {
                    HomeView()
                }
            }
            .readableWidth()
        }
        .task(id: model.session?.tokenID) {
            if model.session != nil, !model.setupChecked { await model.checkSetup() }
        }
    }
}
