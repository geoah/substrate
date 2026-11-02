import SwiftUI

@main
struct SubstrateWorkoutsApp: App {
    /// Keeps a released build current from apps.internal.rxtx.dev
    /// (apps/apple/README.md). A local build has no update address and
    /// never checks.
    @StateObject private var updates = Updater()
    @Environment(\.scenePhase) private var scenePhase
    /// An offer that came while a session was open: Install replaces the
    /// running app, so it waits for Finish or Discard.
    @State private var heldOffer: ReleaseIndex.Release?

    private let app = AppModel.shared
    private let session = SessionController.shared
    private let link = SubstrateLink.shared

    init() {
        // Everything a launch from a Live Activity button or a notification
        // tap needs is wired here, before the app finishes launching: the
        // system performs the intent, or delivers the tap, right after.
        CountdownCoordinator.install()
        let session = SessionController.shared
        LiveActivityActions.handler = { action in
            // `handle` loads and folds the journal before it applies (§6.3).
            await session.handle(action)
        }
        Notifications.shared.onOpenSession = { DeepLinks.openSession() }
        HealthExporter.shared.onEntryChanged = { entry in AppModel.shared.replace(entry) }
    }

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
                .task { await session.prepare() }
        }
        .onChange(of: scenePhase, initial: true) { _, phase in
            guard phase == .active else { return }
            Task { await session.sceneBecameActive() }
            Task {
                await link.status.refresh()
                await link.sync()
            }
        }
    }
}
