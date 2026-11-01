import SubstrateKit
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
                    Task { await model.status.refresh() }
                }
        }
    }
}

/// The server refused a password session's token: what happened, and the
/// one way out.
struct SessionExpiredView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        Form {
            Section {
                ProblemBox(
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

/// An app session's access ended, or was revoked: the kit's screen, whose
/// button runs the approval again, and the way back to the sign-in for when
/// it cannot.
struct AccessEndedScreen: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        Form {
            Section {
                AccessEndedView(appName: SyncManifest.appName, host: model.session.map { ServerAddress.display($0.server) } ?? "Your server") {
                    Task { await model.authorize() }
                }
                .frame(maxWidth: .infinity)
                .disabled(model.authorizing)
            }
            .listRowBackground(Color.clear)
            if let problem = model.authorizeError {
                Section {
                    ProblemBox(title: "Couldn't sign in again", message: problem)
                }
            }
            Section {
                Button(role: .destructive) {
                    model.signOut()
                } label: {
                    WideLabel(title: "Sign Out", systemImage: "rectangle.portrait.and.arrow.right")
                }
                .destructiveAction()
                .buttonRow()
            } footer: {
                Text("What this phone pushed is checked against the server once it is signed in again, and sent again where it is missing.")
            }
        }
        .navigationTitle("Substrate Sync")
    }
}

/// Signed out, set up, or syncing: one screen at a time, the same layout on
/// iPhone and iPad. A password session waits on Set Up; an app session goes
/// to Home unless the check finds set up only an approval brings back.
struct RootView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        NavigationStack {
            Group {
                if let session = model.session {
                    if session.isApp {
                        if model.accessEnded {
                            AccessEndedScreen()
                        } else if model.setupNeedsApproval {
                            ServerStatusView()
                        } else {
                            HomeView()
                        }
                    } else if model.sessionExpired {
                        SessionExpiredView()
                    } else if !model.setupChecked || !model.setupComplete {
                        SetupView()
                    } else {
                        HomeView()
                    }
                } else {
                    SignInView()
                }
            }
            .readableWidth()
        }
        .task(id: model.session) {
            guard let session = model.session else { return }
            if !session.isApp, !model.setupChecked { await model.checkSetup() }
            await model.status.refresh()
        }
        #if DEBUG
        .task {
            // ContinuedProbe's `sync`: Sync Now without a tap, so a real
            // sync's continued task can be tried from a terminal.
            if ProcessInfo.processInfo.environment["SUBSTRATE_SYNC_PROBE"] == "sync" {
                try? await Task.sleep(for: .seconds(3))
                await model.syncNow()
            }
        }
        #endif
    }
}
