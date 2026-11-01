import Foundation
import HealthKit
import Observation
import os
import WorkoutsCore
import UIKit

/// Where Apple Health access stands, for Settings and the session detail.
enum HealthAccess: Equatable, Sendable {
    /// No Health on this device: show nothing.
    case unavailable
    /// Not asked yet: offer "Connect".
    case notDetermined
    /// Sharing refused: sessions stay "not saved"; "Try again" once access
    /// is given in the Health app.
    case denied
    case authorized
}

/// Saves each finished session to Apple Health as one strength workout
/// (DESIGN.md §9.5), through `health-queue.json` so Finish never waits:
/// Finish enqueues the session and calls `process(.finish)` without awaiting
/// it. The queue is processed again at launch and on each return to the
/// foreground. Each outcome is written to the session's history file, which
/// History shows.
@MainActor
@Observable
final class HealthExporter {
    static let shared = HealthExporter()

    /// The app's one store: the exporter and `BodyMassReader` share it.
    nonisolated static let store = HKHealthStore()

    /// Why the queue is processed. Only Finish, Connect and Try again may
    /// show the permission sheet: it is asked in context, never at launch.
    /// `finishCovered` is a Finish whose cover opened again at once for the
    /// next session ("Finish it and start this one"): no sheet over it.
    enum Trigger: Sendable {
        case finish, finishCovered, launch, foreground, connect, retry

        var mayPrompt: Bool {
            switch self {
            case .finish, .connect, .retry: true
            case .finishCovered, .launch, .foreground: false
            }
        }
    }

    enum ExportError: Error, CustomStringConvertible {
        case notFinished
        case noWorkout

        var description: String {
            switch self {
            case .notFinished: "the session has no end"
            case .noWorkout: "HealthKit returned no workout"
            }
        }
    }

    /// Read when `process` and `requestAuthorization` run; Settings shows it.
    private(set) var access: HealthAccess = .notDetermined
    /// Told about every history entry whose Health state changed, so the
    /// history in memory can replace it.
    @ObservationIgnored var onEntryChanged: (@MainActor (HistoryEntry) -> Void)?
    /// The outbox PATCHes the session with the workout's UUID.
    @ObservationIgnored var sink: any SessionSyncSink<EventEnvelope, SessionState> = LinkSink()

    @ObservationIgnored private let store: HKHealthStore
    @ObservationIgnored private let directory: () throws -> URL
    @ObservationIgnored private let logger = Logger(subsystem: "dev.reamde.substrate.workouts", category: "health")
    @ObservationIgnored private var running = false
    @ObservationIgnored private var runAgain: Trigger?
    /// The background task a run holds, ended by the run or by its expiry.
    @ObservationIgnored private var backgroundTask: UIBackgroundTaskIdentifier = .invalid
    /// Sessions whose save failed since the last foreground: any other error
    /// is retried at most once per foreground (§9.5).
    @ObservationIgnored private var failedThisForeground: Set<SessionID> = []

    init(store: HKHealthStore = HealthExporter.store, directory: @escaping () throws -> URL = WorkoutsFiles.applicationSupportDirectory) {
        self.store = store
        self.directory = directory
        access = readAccess()
    }

    private static var workoutType: HKWorkoutType { .workoutType() }
    private static var effortType: HKQuantityType { HKQuantityType(.workoutEffortScore) }

    // MARK: - Access

    /// Asks for sharing workouts and effort, and reading body mass (§9.5).
    /// The sheet shows once; later calls return at once.
    @discardableResult
    func requestAuthorization() async -> HealthAccess {
        guard HKHealthStore.isHealthDataAvailable() else {
            access = .unavailable
            return access
        }
        do {
            try await store.requestAuthorization(
                toShare: [Self.workoutType, Self.effortType],
                read: [HKQuantityType(.bodyMass)]
            )
        } catch {
            logger.error("Health authorization failed: \(error.localizedDescription, privacy: .public)")
        }
        access = readAccess()
        return access
    }

    private func readAccess() -> HealthAccess {
        guard HKHealthStore.isHealthDataAvailable() else { return .unavailable }
        return switch store.authorizationStatus(for: Self.workoutType) {
        case .sharingAuthorized: .authorized
        case .sharingDenied: .denied
        default: .notDetermined
        }
    }

    // MARK: - The queue

    /// Saves what the queue holds. One run at a time: a call during a run
    /// makes it go round once more. A run holds a background task, so a quick
    /// lock does not cut the save off; the permission sheet, which waits on
    /// the owner, is asked before it, never inside it.
    func process(_ trigger: Trigger) async {
        if running {
            // Keep the stronger trigger: one that may prompt wins.
            if runAgain == nil || trigger.mayPrompt { runAgain = trigger }
            return
        }
        running = true
        defer { running = false }
        if trigger.mayPrompt, readAccess() == .notDetermined, (try? queue().load())?.isEmpty == false {
            await requestAuthorization()
        }
        backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "Workouts Health save") { [weak self] in
            // UIKit ends an app whose task outlives its time: end it here; the queue keeps the session.
            self?.logger.error("Health save ran out of background time; the queue keeps the session")
            self?.endBackgroundTask()
        }
        defer { endBackgroundTask() }

        var next: Trigger? = trigger
        while let current = next {
            runAgain = nil
            await run(current)
            next = runAgain
        }
    }

    private func endBackgroundTask() {
        guard backgroundTask != .invalid else { return }
        UIApplication.shared.endBackgroundTask(backgroundTask)
        backgroundTask = .invalid
    }

    /// Finish, step one: writes the queue entry, with no `await` between the
    /// history file and it, so no death in between can lose the save. The
    /// save itself starts with `process(.finish)` once the cover has closed.
    func enqueue(_ session: SessionID, at date: Date = .now) {
        do {
            try writableQueue(at: date).enqueue(session, at: date)
        } catch {
            // `requeueLost` finds the session again from History at the next foreground.
            logger.error("Health queue not written: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// A Finish the app died in: queued, then saved without the permission
    /// sheet, which only Finish, Connect and Try again may show.
    func enqueueAndProcess(_ session: SessionID) {
        enqueue(session)
        Task { await process(.launch) }
    }

    /// Launch and foreground: queues again every finished session History
    /// holds as pending with no workout that the queue has lost (a death
    /// between the history file and the queue, an unreadable queue file).
    func requeueLost(_ history: [HistoryEntry]) {
        let lost = HealthQueueFile.lost(in: history)
        guard !lost.isEmpty else { return }
        do {
            let added = try writableQueue(at: .now).enqueue(missing: lost, at: .now)
            for session in added {
                logger.error("Health queue had lost session \(session.rawValue, privacy: .public); queued again")
            }
        } catch {
            logger.error("Health queue not written: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// The queue, after setting aside a file that no longer decodes: a
    /// corrupt queue must not refuse every later Finish.
    private func writableQueue(at date: Date) throws -> HealthQueueFile {
        let queue = try queue()
        if let aside = queue.setAsideIfUnreadable(at: date) {
            logger.fault("Health queue unreadable; moved to \(aside.lastPathComponent, privacy: .public)")
        }
        return queue
    }

    /// The session detail's "Try again" (and "Connect" there): queues the
    /// session again and saves it, asking for access first if never asked.
    func retry(_ session: SessionID) async {
        failedThisForeground.remove(session)
        do {
            try writableQueue(at: .now).enqueue(session, at: .now)
            try record(session, .pending, error: nil)
        } catch {
            logger.error("Health retry not queued: \(error.localizedDescription, privacy: .public)")
        }
        await process(.retry)
    }

    /// Settings' "Connect": asks for access, then saves what waits.
    func connect() async {
        await requestAuthorization()
        await process(.connect)
    }

    private func run(_ trigger: Trigger) async {
        if trigger == .launch || trigger == .foreground { failedThisForeground.removeAll() }
        let entries: [HealthQueueEntry]
        do {
            entries = try writableQueue(at: .now).load()
        } catch {
            logger.error("Health queue unreadable: \(error.localizedDescription, privacy: .public)")
            return
        }
        access = readAccess()
        guard !entries.isEmpty else { return }

        if access == .unavailable {
            for entry in entries {
                settle(entry.session, .notAvailable, error: nil)
            }
            return
        }
        if access == .notDetermined, trigger.mayPrompt {
            await requestAuthorization()
        }
        switch access {
        case .unavailable, .notDetermined:
            // Stays queued: Settings and the session detail offer "Connect".
            return
        case .denied:
            // Kept as "not saved"; the queue does not retry it on its own.
            for entry in entries {
                settle(entry.session, .notSaved, error: "Apple Health sharing is off")
            }
            return
        case .authorized:
            break
        }

        for entry in entries where !failedThisForeground.contains(entry.session) {
            await save(queued: entry)
        }
    }

    private func save(queued item: HealthQueueEntry) async {
        let session = item.session
        let history: HistoryStore
        let entry: HistoryEntry
        do {
            history = try self.history()
            guard FileManager.default.fileExists(atPath: history.url(for: session).path) else {
                // Deleted from History: the workout is not wanted any more.
                logger.info("Health queue names a deleted session: \(session.rawValue, privacy: .public)")
                try queue().remove(session)
                return
            }
            entry = try history.read(session)
        } catch {
            // An unreadable file is listed in Settings; it can never be saved.
            logger.error("Health queue entry dropped, \(session.rawValue, privacy: .public): \(String(describing: error), privacy: .public)")
            _ = try? queue().remove(session)
            return
        }
        if let existing = entry.records.session.healthKitWorkout {
            // Saved before the queue was told (the app died in between).
            logger.info("Session already linked to workout \(existing, privacy: .public)")
            _ = try? queue().remove(session)
            return
        }
        do {
            let workout = try await save(entry.records, attempts: item.attempts)
            let updated = try history.recordHealthSave(session, status: .saved, healthKitWorkout: workout, at: .now)
            try queue().remove(session)
            onEntryChanged?(updated)
            logger.info("Saved session \(session.rawValue, privacy: .public) as workout \(workout, privacy: .public)")
            let sink = sink
            let revision = updated.records.session.revision ?? 1
            Task.detached { await sink.healthLinked(session, healthKitWorkout: workout, revision: revision) }
        } catch let error as HKError where error.code == .errorAuthorizationDenied || error.code == .errorAuthorizationNotDetermined {
            access = readAccess()
            settle(session, .notSaved, error: error.localizedDescription)
        } catch {
            failedThisForeground.insert(session)
            let message = String(describing: error)
            logger.error("Health save failed for \(session.rawValue, privacy: .public): \(message, privacy: .public)")
            do {
                try queue().recordFailure(session, error: message, at: .now)
                try record(session, .pending, error: message)
            } catch {
                logger.error("Health failure not recorded: \(error.localizedDescription, privacy: .public)")
            }
        }
    }

    /// Takes the session off the queue with its final state.
    private func settle(_ session: SessionID, _ status: HealthSaveStatus, error: String?) {
        do {
            try record(session, status, error: error)
        } catch {
            logger.error("Health state not recorded: \(error.localizedDescription, privacy: .public)")
        }
        do {
            try queue().remove(session)
        } catch {
            logger.error("Health queue not written: \(error.localizedDescription, privacy: .public)")
        }
    }

    private func record(_ session: SessionID, _ status: HealthSaveStatus, error: String?) throws {
        let history = try history()
        guard FileManager.default.fileExists(atPath: history.url(for: session).path) else { return }
        let updated = try history.recordHealthSave(session, status: status, error: error, at: .now)
        onEntryChanged?(updated)
    }

    private func queue() throws -> HealthQueueFile {
        try HealthQueueFile(directory: directory())
    }

    private func history() throws -> HistoryStore {
        try HistoryStore(directory: directory())
    }

    // MARK: - The workout

    /// Writes one workout for the finished session and returns HealthKit's
    /// UUID, uppercase, for the session's `healthKitWorkout`. A workout an
    /// earlier attempt wrote (the app died before the history file was told)
    /// is found by its sync identifier and linked rather than written twice;
    /// each failed attempt raises the sync version, so a retry HealthKit
    /// would not take as newer replaces the old workout instead.
    func save(_ records: SessionRecords, attempts: Int = 0) async throws -> String {
        let session = records.session
        guard let end = session.endsAt else { throw ExportError.notFinished }
        let start = session.at
        if let existing = try? await savedWorkout(session.id) {
            logger.info("Workout for \(session.id.rawValue, privacy: .public) already in Health; linked")
            return existing.uuid.uuidString
        }

        let configuration = HKWorkoutConfiguration()
        configuration.activityType = Self.activityType(session.activityType)
        configuration.locationType = .indoor
        let builder = HKWorkoutBuilder(healthStore: store, configuration: configuration, device: .local())

        try await builder.beginCollection(at: start)
        let events = Self.pauseEvents(session.pauses, start: start, end: end)
        if !events.isEmpty {
            try await builder.addWorkoutEvents(events)
        }
        try await builder.addMetadata(Self.metadata(session, attempts: attempts))
        try await builder.endCollection(at: end)
        guard let workout = try await builder.finishWorkout() else { throw ExportError.noWorkout }

        // The workout exists now: a failed effort sample is logged, never
        // turned into a second workout by a retry.
        // A session RPE of 0 has no effort score: Apple's runs from 1.
        if let score = session.healthEffortScore, store.authorizationStatus(for: Self.effortType) == .sharingAuthorized {
            do {
                let effort = HKQuantitySample(
                    type: Self.effortType,
                    quantity: HKQuantity(unit: .appleEffortScore(), doubleValue: score),
                    start: start,
                    end: end
                )
                try await store.save(effort)
                try await store.relateWorkoutEffortSample(effort, with: workout, activity: nil)
            } catch {
                logger.error("Effort score not saved: \(error.localizedDescription, privacy: .public)")
            }
        }
        return workout.uuid.uuidString
    }

    static func activityType(_ type: ActivityType?) -> HKWorkoutActivityType {
        switch type {
        case .traditionalstrengthtraining, nil: .traditionalStrengthTraining
        case .functionalstrengthtraining: .functionalStrengthTraining
        case .coretraining: .coreTraining
        case .highintensityintervaltraining: .highIntensityIntervalTraining
        }
    }

    /// The workout this app saved for the session, found by its sync
    /// identifier. HealthKit shows an app the samples it wrote without read
    /// access, so this needs none.
    private func savedWorkout(_ session: SessionID) async throws -> HKWorkout? {
        let predicate = HKQuery.predicateForObjects(withMetadataKey: HKMetadataKeySyncIdentifier, allowedValues: [session.rawValue])
        let query = HKSampleQueryDescriptor(predicates: [.workout(predicate)], sortDescriptors: [], limit: 1)
        return try await query.result(for: store).first
    }

    /// §9.5's keys. No brand name in V1 (open question 24). The sync
    /// identifier and version let a later re-save replace this workout
    /// rather than add a second one: the version is the session's revision
    /// plus the failed attempts before this one.
    static func metadata(_ session: TrainingSession, attempts: Int = 0) -> [String: Any] {
        [
            HKMetadataKeyExternalUUID: session.id.rawValue,
            HKMetadataKeySyncIdentifier: session.id.rawValue,
            HKMetadataKeySyncVersion: (session.revision ?? 1) + max(0, attempts),
            HKMetadataKeyIndoorWorkout: true,
            HKMetadataKeyTimeZone: session.timezone ?? TimeZone.current.identifier,
        ]
    }

    /// A `.pause` and a `.resume` per pause, inside the workout's span.
    static func pauseEvents(_ pauses: [Pause], start: Date, end: Date) -> [HKWorkoutEvent] {
        pauses.flatMap { pause -> [HKWorkoutEvent] in
            let from = max(start, pause.start)
            let to = min(end, pause.end ?? end)
            guard from < to else { return [] }
            return [
                HKWorkoutEvent(type: .pause, dateInterval: DateInterval(start: from, duration: 0), metadata: nil),
                HKWorkoutEvent(type: .resume, dateInterval: DateInterval(start: to, duration: 0), metadata: nil),
            ]
        }
    }
}
