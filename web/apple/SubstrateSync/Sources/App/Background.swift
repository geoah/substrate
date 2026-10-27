import BackgroundTasks
import HealthKit
import UIKit

/// Syncs without the app open: a refresh task the system runs when it
/// chooses, a processing task for a longer catch-up, and HealthKit's
/// observer queries, which wake the app when heart rate, steps, sleep or a
/// workout is written.
enum Background {
    static let refresh = "dev.reamde.substrate.sync.refresh"
    static let processing = "dev.reamde.substrate.sync.processing"

    /// At launch, before the app finishes launching: the scheduler refuses a
    /// handler registered later.
    static func register() {
        BGTaskScheduler.shared.register(forTaskWithIdentifier: refresh, using: nil) { task in
            run(task)
        }
        BGTaskScheduler.shared.register(forTaskWithIdentifier: processing, using: nil) { task in
            run(task)
        }
    }

    /// Asks for the next background run at the owner's frequency, or
    /// cancels every request when they sync by hand.
    static func schedule() {
        guard let interval = Settings.frequency.interval else {
            BGTaskScheduler.shared.cancelAllTaskRequests()
            return
        }
        let refreshRequest = BGAppRefreshTaskRequest(identifier: refresh)
        refreshRequest.earliestBeginDate = Date(timeIntervalSinceNow: interval)
        try? BGTaskScheduler.shared.submit(refreshRequest)
        let processingRequest = BGProcessingTaskRequest(identifier: processing)
        processingRequest.requiresNetworkConnectivity = true
        processingRequest.earliestBeginDate = Date(timeIntervalSinceNow: max(interval, 6 * 3600))
        try? BGTaskScheduler.shared.submit(processingRequest)
    }

    private static func run(_ task: BGTask) {
        schedule()
        let done = Once()
        // On expiry the running sync is stopped where it can stop cleanly, and
        // the task completes at once: iOS kills an app that overruns.
        task.expirationHandler = {
            Task { await SyncEngine.shared.cancel() }
            done.run { task.setTaskCompleted(success: false) }
        }
        Task {
            await SyncEngine.shared.syncIfDue()
            done.run { task.setTaskCompleted(success: true) }
        }
    }

    private static var observing = false

    /// Observer queries live only as long as the process, so every launch
    /// starts them again; background delivery is what launches it.
    static func observeHealth() {
        guard HealthReader.isAvailable, !observing, Keychain.load() != nil,
              UserDefaults.standard.bool(forKey: "health.asked"), Settings.enabled(.health)
        else { return }
        observing = true
        let store = SyncEngine.shared.health
        for type in HealthReader.observed {
            let query = HKObserverQuery(sampleType: type, predicate: nil) { _, completion, error in
                guard error == nil else {
                    completion()
                    return
                }
                // HealthKit wants the answer promptly and backs off an app that
                // keeps it waiting; the sync goes on without it.
                completion()
                Task { await SyncEngine.shared.syncIfDue([.health]) }
            }
            store.execute(query)
        }
        updateHealthDelivery()
    }

    /// HealthKit's wake-ups follow the frequency; by hand means none.
    static func updateHealthDelivery() {
        guard HealthReader.isAvailable else { return }
        let store = SyncEngine.shared.health
        let frequency: HKUpdateFrequency? = switch Settings.frequency {
        case .manual: nil
        case .hourly, .sixHours: .hourly
        case .daily: .daily
        }
        for type in HealthReader.observed {
            if let frequency {
                store.enableBackgroundDelivery(for: type, frequency: frequency) { _, _ in }
            } else {
                store.disableBackgroundDelivery(for: type) { _, _ in }
            }
        }
    }
}

/// Runs its block once, from whichever thread gets there first.
final class Once: @unchecked Sendable {
    private let lock = NSLock()
    private var done = false

    func run(_ block: () -> Void) {
        lock.lock()
        defer { lock.unlock() }
        guard !done else { return }
        done = true
        block()
    }
}

final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(
        _ application: UIApplication, didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        Background.register()
        Background.observeHealth()
        Background.schedule()
        return true
    }
}
