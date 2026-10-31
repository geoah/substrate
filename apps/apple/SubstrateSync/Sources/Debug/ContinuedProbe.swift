#if DEBUG
import BackgroundTasks
import Foundation
import os

/// A debug build's test of iOS's continued processing tasks, apart from any
/// sync: SUBSTRATE_SYNC_PROBE in the launch environment submits one that
/// counts to a hundred, a percent a second, and logs what iOS does with it
/// (subsystem dev.reamde.substrate.sync, category probe).
///
/// - `apple`: Apple's sample exactly, the work inside the launch handler on
///   a background queue.
/// - `main`: the app's pattern, the handler returning at once and the
///   progress written later from the main actor.
enum ContinuedProbe {
    /// Into the Activity log too, which a phone without a cable still gives up.
    private struct Log {
        let logger = Logger(subsystem: "dev.reamde.substrate.sync", category: "probe")
        func notice(_ message: String) {
            logger.notice("\(message)")
            Activity.shared.log(nil, message, level: .warning)
        }
        func error(_ message: String) { notice(message) }
    }
    private static let log = Log()

    static func startIfAsked() {
        // `sync` is a real Sync Now, started by RootView.
        guard let mode = ProcessInfo.processInfo.environment["SUBSTRATE_SYNC_PROBE"], mode != "sync" else { return }
        // Submitted once the app is on screen, which iOS requires.
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) { submit(mode) }
    }

    private static func submit(_ mode: String) {
        let id = ContinuedSync.prefix + "probe-" + UUID().uuidString
        let registered = BGTaskScheduler.shared.register(forTaskWithIdentifier: id, using: mode == "apple" ? nil : .main) { task in
            guard let task = task as? BGContinuedProcessingTask else { return }
            log.notice("probe \(mode): began")
            switch mode {
            case "apple": runApple(task)
            case "reset": runMain(task, total: 100, start: 90, restart: 1)
            case "fine": runMain(task, total: 10000)
            case "retitle": runMain(task, title: "Probe retitled")
            case "burst": runMain(task, total: 1000, every: .milliseconds(100))
            // A first step that completes, as a sync's earlier-days check
            // does, then the count from the start.
            case "full": runMain(task, start: 100, restart: 1)
            // Up to 20% inside the warm-up, back to 5%, then on: a bar that
            // steps back below its highest point while iOS is watching.
            case "dip": runDip(task)
            default: runMain(task)
            }
        }
        let request = BGContinuedProcessingTaskRequest(identifier: id, title: "Probe \(mode)", subtitle: "Starting")
        request.strategy = .fail
        do {
            try BGTaskScheduler.shared.submit(request)
            log.notice("probe \(mode): submitted, registered \(registered)")
        } catch {
            log.error("probe \(mode): submit failed: \(error.localizedDescription)")
        }
    }

    private static func runDip(_ task: BGContinuedProcessingTask) {
        task.progress.totalUnitCount = 100
        let steps = Array(Int64(1) ... 20) + Array(Int64(5) ... 90)
        let work = Task { @MainActor in
            for i in steps {
                try await Task.sleep(for: .seconds(1))
                task.progress.completedUnitCount = i
                task.updateTitle(task.title, subtitle: "\(i)%")
                log.notice("probe dip: \(i)%")
            }
        }
        task.expirationHandler = {
            log.notice("probe dip: expired")
            work.cancel()
        }
        Task { @MainActor in
            let finished = (try? await work.value) != nil
            log.notice("probe dip: ended, finished \(finished)")
            task.setTaskCompleted(success: finished)
        }
    }

    private static func runApple(_ task: BGContinuedProcessingTask) {
        let go = Once()
        var expired = false
        task.expirationHandler = { go.run { expired = true } }
        task.progress.totalUnitCount = 100
        task.progress.completedUnitCount = 0
        while !expired, task.progress.completedUnitCount < 100 {
            sleep(1)
            task.progress.completedUnitCount += 1
            task.updateTitle(task.title, subtitle: "\(task.progress.completedUnitCount)%")
            log.notice("probe apple: \(task.progress.completedUnitCount)%")
        }
        log.notice("probe apple: ended, expired \(expired)")
        task.setTaskCompleted(success: !expired)
    }

    /// `start` is shown first, then the count runs on from `restart`: a bar
    /// that steps back below where it has been.
    private static func runMain(
        _ task: BGContinuedProcessingTask, total: Int64 = 100, start: Int64? = nil, restart: Int64 = 1, title: String? = nil,
        every: Duration = .seconds(1)
    ) {
        task.progress.totalUnitCount = total
        if let start { task.progress.completedUnitCount = start }
        let work = Task { @MainActor in
            for i in restart ..< restart + (every < .seconds(1) ? 1000 : 100) {
                try await Task.sleep(for: every)
                task.progress.completedUnitCount = i
                task.updateTitle(title ?? task.title, subtitle: "\(i) of \(total)")
                if every >= .seconds(1) || i % 10 == 0 { log.notice("probe main: \(i) of \(total)") }
            }
        }
        task.expirationHandler = {
            log.notice("probe main: expired")
            work.cancel()
        }
        Task { @MainActor in
            let finished = (try? await work.value) != nil
            log.notice("probe main: ended, finished \(finished)")
            task.setTaskCompleted(success: finished)
        }
    }
}
#endif
