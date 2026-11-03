import SubstrateKit
import WorkoutsCore
import SwiftUI

@main
struct SubstrateWorkoutsApp: App {
    /// Keeps a released build current from apps.internal.rxtx.dev
    /// (apps/README.md). A local build has no update address and
    /// never checks.
    @StateObject private var updates = Updater()
    @Environment(\.scenePhase) private var scenePhase
    /// An offer that came while a session was open: Install replaces the
    /// running app, so it waits for Finish or Discard.
    @State private var heldOffer: ReleaseIndex.Release?

    private let root = WorkoutsRoot.shared
    private var app: AppModel { root.app }
    private var session: SessionModel { root.session }
    private var link: SubstrateLink { root.link }

    init() {
        // Everything a launch from a Live Activity button or a notification
        // tap needs is wired here, before the app finishes launching: the
        // system performs the intent, or delivers the tap, right after.
        SystemNotifications.installDelegate()
        let root = WorkoutsRoot.shared
        LiveActivityActions.handler = { action in
            // The runtime folds the journal before it applies (§6.3).
            await root.session.handle(action)
        }
        HealthExporter.shared.onEntryChanged = { entry in root.app.replace(entry) }
        HealthExporter.shared.onLinked = { id, workout, revision in root.send(.healthLinked(id, workout: workout, revision: revision)) }
        #if DEBUG
        Self.seedPreviewHistory()
        #endif
    }

    #if DEBUG
    /// `-WorkoutsSeedPreviewHistory`: the previews' two sessions of the
    /// sample written into an empty History before it loads, so a
    /// simulator shows a routine's last times and its progress signed out
    /// (the README's screenshots).
    private static func seedPreviewHistory() {
        guard ProcessInfo.processInfo.arguments.contains("-WorkoutsSeedPreviewHistory"),
              let directory = AppModel.workoutsDirectory() else { return }
        let store = HistoryStore(directory: directory)
        guard store.loadAll().entries.isEmpty else { return }
        for entry in PreviewData.history {
            try? store.write(entry)
        }
    }
    #endif

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(app)
                .environment(session)
                .environment(link)
                .environmentObject(updates)
                .updateOffer(updates)
                .onChange(of: updates.offer?.build) { _, build in
                    guard build != nil, session.isActive else { return }
                    heldOffer = updates.offer
                    updates.offer = nil
                }
                .onChange(of: session.isActive) { _, active in
                    guard !active, let held = heldOffer else { return }
                    heldOffer = nil
                    updates.offer = held
                }
                .workoutsHaptics()
                .onOpenURL { url in DeepLinks.handle(url) }
        }
        .onChange(of: scenePhase, initial: true) { _, phase in
            guard phase == .active else { return }
            session.send(.foreground)
            link.refresh()
        }
    }
}
