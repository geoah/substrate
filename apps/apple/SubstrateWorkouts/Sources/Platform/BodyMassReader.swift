import Foundation
import HealthKit
import os
import WorkoutsCore

/// The body mass a session snapshots at Start (DESIGN.md §9.5, open question
/// 22): the latest Apple Health sample ending on or before now, in kg to
/// 0.1; else the value typed in Settings; else none, and bodyweight
/// exercises fall back to 03 §11.5. Start never prompts for Health: read
/// access is asked with the workout at the first Finish, and a refused read
/// simply comes back empty.
@MainActor
final class BodyMassReader {
    static let shared = BodyMassReader()

    private let store: HKHealthStore
    private let logger = Logger(subsystem: "dev.reamde.substrate.workouts", category: "body-mass")

    init(store: HKHealthStore = HealthExporter.store) {
        self.store = store
    }

    /// What `Start.bodyMass` carries: Health's latest, else `typed`.
    func bodyMass(typed: Mass?, now: Date = .now) async -> Mass? {
        await latest(now: now) ?? typed
    }

    /// The latest Apple Health sample ending on or before `now`; nil when
    /// there is none, Health is unavailable, or reading is not allowed.
    func latest(now: Date = .now) async -> Mass? {
        guard HKHealthStore.isHealthDataAvailable() else { return nil }
        let predicate = HKQuery.predicateForSamples(withStart: nil, end: now, options: .strictEndDate)
        let query = HKSampleQueryDescriptor(
            predicates: [.quantitySample(type: HKQuantityType(.bodyMass), predicate: predicate)],
            sortDescriptors: [SortDescriptor(\.endDate, order: .reverse)],
            limit: 1
        )
        do {
            guard let sample = try await query.result(for: store).first else { return nil }
            let kilograms = sample.quantity.doubleValue(for: .gramUnit(with: .kilo))
            guard kilograms.isFinite, kilograms > 0 else { return nil }
            // Tenths as an integer, so the Decimal holds exactly 83.9, not
            // the binary 83.900000000000006.
            let tenths = Int((kilograms * 10).rounded())
            return .kg(Decimal(tenths) / 10)
        } catch {
            // Not determined yet, or locked: Start goes on without it.
            logger.info("No body mass from Health: \(error.localizedDescription, privacy: .public)")
            return nil
        }
    }
}
