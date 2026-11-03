import Foundation
import SubstrateRuntime

/// What Workouts asks of the device beyond the kit's services: Apple
/// Health's body mass, and the queue that saves a finished session there.
/// The app implements it on HealthKit; a test or a preview passes a fake or
/// none, which does nothing.
public protocol WorkoutsServices: Sendable {
    /// The latest body mass Apple Health holds, else `typed`. Never prompts.
    func bodyMass(typed: Mass?) async -> Mass?
    /// Queues the session's Health save, kept on disk before it returns.
    func queueHealthSave(_ session: SessionID) async
    /// Queues a session Finish saved but never queued (the app died in
    /// between), without the permission sheet, and works the queue.
    func recoverHealthSave(_ session: SessionID) async
    /// Works the Health queue.
    func processHealth(_ trigger: HealthTrigger) async
}

/// Why the Health queue is worked now: only Finish may show the permission
/// sheet, and over a cover that opened again for the next session it does
/// not.
public enum HealthTrigger: String, Codable, Equatable, Sendable {
    case finish
    case finishCovered
    case foreground
}

private enum WorkoutsServicesKey: AppEnvironmentKey {
    static let defaultValue: (any WorkoutsServices)? = nil
}

public extension AppEnvironment {
    /// Workouts' own device services; nil does nothing.
    var workouts: (any WorkoutsServices)? {
        get { self[WorkoutsServicesKey.self] }
        set { self[WorkoutsServicesKey.self] = newValue }
    }
}
