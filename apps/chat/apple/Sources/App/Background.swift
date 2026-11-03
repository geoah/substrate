#if os(iOS)
import BackgroundTasks
import SwiftUI
import UIKit
import SubstrateKit

final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(
        _ application: UIApplication, didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        Background.register()
        return true
    }
}

/// What runs while the app is off screen. iOS decides most of it, so the app
/// promises only what it observes: a turn streaming when the app leaves is
/// kept alive as a continued processing task, and a refresh task reads the
/// list now and then to notify what settled or asked since.
@MainActor
enum Background {
    static let refreshID = "dev.reamde.substrate.chat.refresh"

    static func register() {
        BGTaskScheduler.shared.register(forTaskWithIdentifier: refreshID, using: .main) { task in
            MainActor.assumeIsolated { refresh(task) }
        }
    }

    static func schedule() {
        let request = BGAppRefreshTaskRequest(identifier: refreshID)
        request.earliestBeginDate = Date(timeIntervalSinceNow: 15 * 60)
        try? BGTaskScheduler.shared.submit(request)
    }

    private static func refresh(_ task: BGTask) {
        schedule()
        guard let store = AppModel.shared.store else {
            task.setTaskCompleted(success: true)
            return
        }
        let work = Task { await store.backgroundRefresh() }
        task.expirationHandler = { work.cancel() }
        Task {
            await work.value
            task.setTaskCompleted(success: !work.isCancelled)
        }
    }

    /// The app is leaving the screen: every turn still streaming is kept
    /// running, if iOS allows, until it settles.
    static func leaving(_ store: ChatStore) {
        for conversation in store.conversations.values where conversation.streaming {
            ContinuedRun.start(conversation, title: store.thread(conversation.threadID).map(store.title) ?? "New chat")
        }
    }
}

/// A turn kept running while the app is off screen: a continued processing
/// task, which iOS draws itself with a Stop button, then a notification when
/// the answer settles. The bar never knows how long a turn takes, so it moves
/// a little on every event and heartbeat; iOS expires a task whose bar stands
/// still, and a long tool call emits nothing but heartbeats.
@MainActor
final class ContinuedRun {
    /// The permitted identifier is this followed by `*`. Every run takes a
    /// new one: registering an identifier twice ends the app.
    static let prefix = "dev.reamde.substrate.chat.continued."
    private static let units: Int64 = 1_000_000
    private static var running: [UUID: ContinuedRun] = [:]

    private var task: BGContinuedProcessingTask?
    private var fallback: UIBackgroundTaskIdentifier = .invalid
    private var ticks = 0.0
    private var ended = false
    private weak var conversation: Conversation?
    private var watch: Task<Void, Never>?

    static func start(_ conversation: Conversation, title: String) {
        guard running[conversation.id] == nil else { return }
        let run = ContinuedRun()
        run.conversation = conversation
        running[conversation.id] = run
        conversation.onActivity = { [weak run] in run?.tick() }
        // Ended by the settle, whichever way the run went.
        let settles = conversation.settles
        run.watch = Task { [weak run, weak conversation] in
            while let conversation, conversation.settles == settles, conversation.active {
                try? await Task.sleep(for: .milliseconds(500))
            }
            run?.finish()
        }

        let id = prefix + UUID().uuidString
        let registered = BGTaskScheduler.shared.register(forTaskWithIdentifier: id, using: .main) { task in
            MainActor.assumeIsolated { run.began(task) }
        }
        let request = BGContinuedProcessingTaskRequest(
            identifier: id, title: "Waiting for \(conversation.name)", subtitle: title)
        request.strategy = .fail
        if registered, (try? BGTaskScheduler.shared.submit(request)) != nil { return }
        // iOS will not run one now: the half minute a background task gives.
        run.fallback = UIApplication.shared.beginBackgroundTask(withName: "Waiting for an answer") {
            MainActor.assumeIsolated { run.finish() }
        }
    }

    private func began(_ task: BGTask) {
        guard let task = task as? BGContinuedProcessingTask, !ended else {
            task.setTaskCompleted(success: ended)
            return
        }
        self.task = task
        task.progress.totalUnitCount = Self.units
        // The Stop in the system's progress, or iOS taking the time back:
        // the turn itself runs on, on the server.
        task.expirationHandler = { [weak self] in
            Task { @MainActor in self?.finish() }
        }
        tick()
    }

    private func tick() {
        ticks += 1
        guard let task else { return }
        // Never reaches the end: an answer's length is unknown.
        let fraction = 1 - exp(-ticks / 60)
        task.progress.completedUnitCount = Int64(fraction * 0.95 * Double(Self.units))
    }

    private func finish() {
        guard !ended else { return }
        ended = true
        watch?.cancel()
        if let conversation {
            conversation.onActivity = nil
            Self.running[conversation.id] = nil
        }
        task?.progress.completedUnitCount = Self.units
        task?.setTaskCompleted(success: true)
        task = nil
        if fallback != .invalid {
            UIApplication.shared.endBackgroundTask(fallback)
            fallback = .invalid
        }
    }
}
#endif
