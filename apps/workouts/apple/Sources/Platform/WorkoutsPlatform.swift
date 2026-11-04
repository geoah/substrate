import Foundation
import WorkoutsCore

/// Workouts' own device services on Apple's frameworks: the body mass and
/// the Health queue, both on the main actor, where HealthKit's objects live.
struct WorkoutsPlatform: WorkoutsServices {
    func bodyMass(typed: Mass?) async -> Mass? {
        await BodyMassReader.shared.bodyMass(typed: typed)
    }

    func queueHealthSave(_ session: SessionID) async {
        await HealthExporter.shared.enqueue(session)
    }

    func recoverHealthSave(_ session: SessionID) async {
        await HealthExporter.shared.enqueueAndProcess(session)
    }

    func processHealth(_ trigger: HealthTrigger) async {
        switch trigger {
        case .finish: await HealthExporter.shared.process(.finish)
        case .finishCovered: await HealthExporter.shared.process(.finishCovered)
        case .foreground:
            // A session History holds as pending that the queue lost is queued again.
            await HealthExporter.shared.requeueLost(WorkoutsRoot.shared.app.history)
            await HealthExporter.shared.process(.foreground)
        }
    }
}
