import WorkoutsCore
import SwiftUI

/// Four tabs in the system's Liquid Glass tab bar (DESIGN.md §7.1). While a
/// session is open its bar rides above them in the tab view's bottom
/// accessory, and the session itself is a full-screen cover that zooms out
/// of that bar.
struct RootView: View {
    @Environment(AppModel.self) private var app
    @Environment(SessionController.self) private var session
    @Namespace private var zoom

    var body: some View {
        @Bindable var app = app
        TabView(selection: $app.tab) {
            Tab("Routines", systemImage: "dumbbell", value: AppTab.routines) {
                RoutinesView()
            }
            Tab("History", systemImage: "clock.arrow.circlepath", value: AppTab.history) {
                HistoryView()
            }
            .badge(app.historyBadge)
            Tab("Exercises", systemImage: "list.bullet", value: AppTab.exercises) {
                ExercisesView()
            }
            Tab("Settings", systemImage: "gearshape", value: AppTab.settings) {
                SettingsView()
            }
        }
        .tabBarMinimizeBehavior(.onScrollDown)
        .tabViewBottomAccessory(isEnabled: session.presentation != nil) {
            if let presentation = session.presentation {
                InProgressAccessory(accessory: presentation.accessory, header: presentation.header) {
                    app.isSessionPresented = true
                }
                .matchedTransitionSource(id: presentation.sessionID.rawValue, in: zoom)
            }
        }
        .fullScreenCover(isPresented: $app.isSessionPresented) {
            SessionCover()
                .navigationTransition(.zoom(sourceID: session.presentation?.sessionID.rawValue ?? "", in: zoom))
        }
        // Rows pre-fills the plan's RPE and the Table does not (§4.14): a
        // session in progress is presented again when Settings switches.
        .onChange(of: app.settings.sessionLayout) { _, _ in
            session.present()
        }
    }
}

#if DEBUG
#Preview("Session open") {
    let app = PreviewData.app()
    RootView()
        .environment(app)
        .environment(PreviewData.session(app))
        .environmentObject(Updater())
}

#Preview("No session") {
    let app = PreviewData.app()
    RootView()
        .environment(app)
        .environment(PreviewData.noSession(app))
        .environmentObject(Updater())
}
#endif
