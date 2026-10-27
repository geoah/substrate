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

/// Signed out, set up, or syncing: one screen at a time, the same layout on
/// iPhone and iPad.
struct RootView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        NavigationStack {
            Group {
                if model.session == nil {
                    SignInView()
                } else if !model.setupChecked || !model.setupComplete {
                    SetupView()
                } else {
                    HomeView()
                }
            }
            .frame(maxWidth: 640)
            .frame(maxWidth: .infinity)
        }
        .task(id: model.session?.tokenID) {
            if model.session != nil, !model.setupChecked { await model.checkSetup() }
        }
    }
}
