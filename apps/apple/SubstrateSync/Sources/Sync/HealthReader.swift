import Foundation
import HealthKit

/// Reads one local day out of HealthKit as the two deliveries the provider
/// takes: the minutes delivery (every metric, one row per minute) and the
/// seconds delivery (sleep segments and workouts). A metric is only ever in
/// one of them, because its rows replace a span at the grouping they came in.
struct HealthReader: Sendable {
    let store: HKHealthStore

    static var isAvailable: Bool { HKHealthStore.isHealthDataAvailable() }

    private static let perMinute = HKUnit.count().unitDivided(by: .minute())

    /// A metric read as a cumulative sum per minute.
    private struct Cumulative {
        let id: HKQuantityTypeIdentifier
        let name: String
        let units: String
        let unit: HKUnit
    }

    /// A metric whose samples are written at their minute, averaged.
    private struct Discrete {
        let id: HKQuantityTypeIdentifier
        let name: String
        let units: String
        let unit: HKUnit
        var scale: Double = 1
    }

    private static let cumulative: [Cumulative] = [
        .init(id: .stepCount, name: "step_count", units: "count", unit: .count()),
        .init(id: .distanceWalkingRunning, name: "walking_running_distance", units: "km", unit: .meterUnit(with: .kilo)),
        .init(id: .flightsClimbed, name: "flights_climbed", units: "count", unit: .count()),
        .init(id: .activeEnergyBurned, name: "active_energy", units: "kcal", unit: .kilocalorie()),
        .init(id: .basalEnergyBurned, name: "basal_energy_burned", units: "kcal", unit: .kilocalorie()),
    ]

    private static let discrete: [Discrete] = [
        .init(id: .restingHeartRate, name: "resting_heart_rate", units: "count/min", unit: perMinute),
        .init(id: .heartRateVariabilitySDNN, name: "heart_rate_variability", units: "ms", unit: .secondUnit(with: .milli)),
        .init(id: .respiratoryRate, name: "respiratory_rate", units: "count/min", unit: perMinute),
        .init(id: .oxygenSaturation, name: "blood_oxygen_saturation", units: "%", unit: .percent(), scale: 100),
        .init(id: .vo2Max, name: "vo2_max", units: "ml/(kg·min)", unit: HKUnit(from: "ml/kg*min")),
        .init(id: .bodyMass, name: "weight_body_mass", units: "kg", unit: .gramUnit(with: .kilo)),
        .init(id: .bodyFatPercentage, name: "body_fat_percentage", units: "%", unit: .percent(), scale: 100),
        .init(id: .bodyMassIndex, name: "body_mass_index", units: "count", unit: .count()),
        .init(id: .height, name: "height", units: "cm", unit: .meterUnit(with: .centi)),
        .init(id: .appleSleepingWristTemperature, name: "apple_sleeping_wrist_temperature", units: "degC", unit: .degreeCelsius()),
    ]

    /// Everything the app reads.
    static var readTypes: Set<HKObjectType> {
        var types: Set<HKObjectType> = [
            HKQuantityType(.heartRate), HKQuantityType(.bloodGlucose),
            HKQuantityType(.bloodPressureSystolic), HKQuantityType(.bloodPressureDiastolic),
            HKCategoryType(.sleepAnalysis), HKObjectType.workoutType(),
            HKQuantityType(.distanceCycling), HKQuantityType(.distanceSwimming),
        ]
        for m in cumulative { types.insert(HKQuantityType(m.id)) }
        for m in discrete { types.insert(HKQuantityType(m.id)) }
        return types
    }

    /// What the observer queries wake the app for.
    static var observed: [HKSampleType] {
        [HKQuantityType(.heartRate), HKQuantityType(.stepCount), HKCategoryType(.sleepAnalysis), HKObjectType.workoutType()]
    }

    /// Every metric of one day, each read on its own: a metric the device
    /// holds nothing for is an empty one, and one HealthKit refuses is left
    /// out and named in `skipped`, so one bad type never stops the rest.
    func minutes(_ day: Range<Date>, zone: TimeZone) async -> HealthDelivery {
        let rows = HealthRows(zone: zone)
        var delivery = HealthDelivery(grouping: .minutes, metrics: [], workouts: [])
        func add(_ name: String, _ units: String, _ read: () async throws -> [JSON]) async {
            do {
                delivery.metrics.append(HealthMetric(name: name, units: units, rows: try await read()))
            } catch {
                delivery.skipped.append("\(name): \(error.localizedDescription)")
            }
        }

        await add("heart_rate", "count/min") {
            let hr = try await collection(.heartRate, day, options: [.discreteMin, .discreteAverage, .discreteMax, .separateBySource])
            return buckets(hr, day).compactMap { s in
                guard let mn = s.minimumQuantity(), let avg = s.averageQuantity(), let mx = s.maximumQuantity() else { return nil }
                return rows.minMaxAvg(
                    at: s.startDate, min: mn.doubleValue(for: Self.perMinute), avg: avg.doubleValue(for: Self.perMinute),
                    max: mx.doubleValue(for: Self.perMinute), sources: names(s.sources))
            }
        }

        for m in Self.cumulative {
            await add(m.name, m.units) {
                let c = try await collection(m.id, day, options: [.cumulativeSum, .separateBySource])
                return buckets(c, day).compactMap { s in
                    guard let sum = s.sumQuantity() else { return nil }
                    let v = sum.doubleValue(for: m.unit)
                    return v > 0 ? rows.quantity(at: s.startDate, qty: v, sources: names(s.sources)) : nil
                }
            }
        }

        for m in Self.discrete {
            await add(m.name, m.units) {
                byMinute(try await quantitySamples(m.id, day)).map { minute, group in
                    let avg = group.map { $0.quantity.doubleValue(for: m.unit) }.reduce(0, +) / Double(group.count)
                    return rows.quantity(at: minute, qty: avg * m.scale, sources: group.map(\.sourceRevision.source.name))
                }
            }
        }

        let mgdl = HKUnit(from: "mg/dL")
        await add("blood_glucose", "mg/dL") {
            byMinute(try await quantitySamples(.bloodGlucose, day)).map { minute, group in
                let avg = group.map { $0.quantity.doubleValue(for: mgdl) }.reduce(0, +) / Double(group.count)
                let meal = group.lazy.compactMap { ($0.metadata?[HKMetadataKeyBloodGlucoseMealTime] as? NSNumber)?.intValue }.first
                var extra: [(String, JSON)] = []
                switch meal.flatMap(HKBloodGlucoseMealTime.init(rawValue:)) {
                case .preprandial: extra.append(("mealTime", .string("Before Meal")))
                case .postprandial: extra.append(("mealTime", .string("After Meal")))
                default: break
                }
                return rows.quantity(at: minute, qty: avg, sources: group.map(\.sourceRevision.source.name), extra: extra)
            }
        }

        // A blood pressure correlation is a systolic and a diastolic sample at
        // one instant; a minute that has both is one reading.
        let mmHg = HKUnit.millimeterOfMercury()
        await add("blood_pressure", "mmHg") {
            let systolic = Dictionary(uniqueKeysWithValues: byMinute(try await quantitySamples(.bloodPressureSystolic, day)))
            let diastolic = Dictionary(uniqueKeysWithValues: byMinute(try await quantitySamples(.bloodPressureDiastolic, day)))
            return systolic.keys.sorted().compactMap { minute in
                guard let sys = systolic[minute], let dia = diastolic[minute] else { return nil }
                func avg(_ g: [HKQuantitySample]) -> Double { g.map { $0.quantity.doubleValue(for: mmHg) }.reduce(0, +) / Double(g.count) }
                return rows.pressure(at: minute, systolic: avg(sys), diastolic: avg(dia), sources: sys.map(\.sourceRevision.source.name))
            }
        }

        return delivery
    }

    /// Sleep and workouts of one day, each part read on its own as
    /// `minutes` reads its metrics.
    func seconds(_ day: Range<Date>, zone: TimeZone) async -> HealthDelivery {
        let rows = HealthRows(zone: zone)
        var delivery = HealthDelivery(grouping: .seconds, metrics: [], workouts: [])
        // A night is worked out over the segments around the day too, so a
        // night across midnight carries the same fields in both days.
        let around = day.lowerBound.addingTimeInterval(-18 * 3600) ..< day.upperBound.addingTimeInterval(18 * 3600)
        do {
            let predicate = HKQuery.predicateForSamples(withStart: around.lowerBound, end: around.upperBound)
            let sleep = try await noDataIsEmpty {
                try await HKSampleQueryDescriptor(
                    predicates: [.categorySample(type: HKCategoryType(.sleepAnalysis), predicate: predicate)],
                    sortDescriptors: [SortDescriptor(\.startDate)]
                ).result(for: store)
            } ?? []
            let segments = sleep.compactMap { s -> SleepSegment? in
                guard let value = Self.sleepValue(s.value) else { return nil }
                return SleepSegment(start: s.startDate, end: s.endDate, value: value, source: s.sourceRevision.source.name)
            }
            delivery.metrics.append(HealthMetric(name: "sleep_analysis", units: "hr", rows: SleepNights.rows(all: segments, day: day, rows: rows)))
        } catch {
            delivery.skipped.append("sleep_analysis: \(error.localizedDescription)")
        }

        do {
            let workouts = try await noDataIsEmpty {
                try await HKSampleQueryDescriptor(
                    predicates: [.workout(HKQuery.predicateForSamples(withStart: day.lowerBound, end: day.upperBound, options: .strictStartDate))],
                    sortDescriptors: [SortDescriptor(\.startDate)]
                ).result(for: store)
            } ?? []
            for w in workouts {
                do {
                    delivery.workouts.append(try await summary(w).json(rows))
                } catch {
                    delivery.skipped.append("workout \(w.uuid.uuidString): \(error.localizedDescription)")
                }
            }
        } catch {
            delivery.skipped.append("workouts: \(error.localizedDescription)")
        }
        return delivery
    }

    static func sleepValue(_ raw: Int) -> String? {
        switch HKCategoryValueSleepAnalysis(rawValue: raw) {
        case .inBed: "In Bed"
        case .awake: "Awake"
        case .asleepCore: "Core"
        case .asleepDeep: "Deep"
        case .asleepREM: "REM"
        case .asleepUnspecified: "Asleep"
        default: nil
        }
    }

    private func summary(_ w: HKWorkout) async throws -> WorkoutSummary {
        let meta = w.metadata ?? [:]
        let indoor = (meta[HKMetadataKeyIndoorWorkout] as? NSNumber)?.boolValue ?? false
        let openWater = (meta[HKMetadataKeySwimmingLocationType] as? NSNumber)?.intValue
            == HKWorkoutSwimmingLocationType.openWater.rawValue
        var s = WorkoutSummary(
            id: w.uuid, name: WorkoutNames.name(w.workoutActivityType, indoor: indoor, openWater: openWater),
            start: w.startDate, end: w.endDate, isIndoor: indoor)
        s.activeKcal = w.statistics(for: HKQuantityType(.activeEnergyBurned))?.sumQuantity()?.doubleValue(for: .kilocalorie())
        for id in [HKQuantityTypeIdentifier.distanceWalkingRunning, .distanceCycling, .distanceSwimming] {
            if let d = w.statistics(for: HKQuantityType(id))?.sumQuantity() {
                s.distanceKm = d.doubleValue(for: .meterUnit(with: .kilo))
                break
            }
        }
        if let hr = w.statistics(for: HKQuantityType(.heartRate)), let mn = hr.minimumQuantity(),
           let avg = hr.averageQuantity(), let mx = hr.maximumQuantity() {
            s.heartRate = (mn.doubleValue(for: Self.perMinute), avg.doubleValue(for: Self.perMinute), mx.doubleValue(for: Self.perMinute))
        }
        s.elevationUpM = (meta[HKMetadataKeyElevationAscended] as? HKQuantity)?.doubleValue(for: .meter())
        s.timeZone = meta[HKMetadataKeyTimeZone] as? String
        let span = w.startDate ..< max(w.endDate, w.startDate.addingTimeInterval(1))
        let minutes = try await collection(.heartRate, span, anchor: w.startDate,
                                           options: [.discreteMin, .discreteAverage, .discreteMax, .separateBySource])
        s.heartRateData = buckets(minutes, span).compactMap { b in
            guard let mn = b.minimumQuantity(), let avg = b.averageQuantity(), let mx = b.maximumQuantity() else { return nil }
            return .init(at: b.startDate, min: mn.doubleValue(for: Self.perMinute), avg: avg.doubleValue(for: Self.perMinute),
                         max: mx.doubleValue(for: Self.perMinute), sources: names(b.sources))
        }
        return s
    }

    // MARK: - queries

    /// A statistics query over a span, or nil where the device holds nothing
    /// of the type: HealthKit answers that with an error ("no data source
    /// available") rather than an empty collection.
    private func collection(
        _ id: HKQuantityTypeIdentifier, _ span: Range<Date>, anchor: Date? = nil, options: HKStatisticsOptions
    ) async throws -> HKStatisticsCollection? {
        let predicate = HKQuery.predicateForSamples(withStart: span.lowerBound, end: span.upperBound)
        return try await noDataIsEmpty {
            try await HKStatisticsCollectionQueryDescriptor(
                predicate: .quantitySample(type: HKQuantityType(id), predicate: predicate),
                options: options, anchorDate: anchor ?? span.lowerBound, intervalComponents: DateComponents(minute: 1)
            ).result(for: store)
        }
    }

    private func buckets(_ c: HKStatisticsCollection?, _ span: Range<Date>) -> [HKStatistics] {
        guard let c else { return [] }
        var out: [HKStatistics] = []
        c.enumerateStatistics(from: span.lowerBound, to: span.upperBound) { s, _ in
            if span.contains(s.startDate) { out.append(s) }
        }
        return out
    }

    private func quantitySamples(_ id: HKQuantityTypeIdentifier, _ day: Range<Date>) async throws -> [HKQuantitySample] {
        let predicate = HKQuery.predicateForSamples(withStart: day.lowerBound, end: day.upperBound, options: .strictStartDate)
        return try await noDataIsEmpty {
            try await HKSampleQueryDescriptor(
                predicates: [.quantitySample(type: HKQuantityType(id), predicate: predicate)],
                sortDescriptors: [SortDescriptor(\.startDate)]
            ).result(for: store)
        } ?? []
    }

    /// Samples grouped by the minute they start in, in order.
    private func byMinute(_ samples: [HKQuantitySample]) -> [(Date, [HKQuantitySample])] {
        var groups: [Date: [HKQuantitySample]] = [:]
        for s in samples {
            let minute = Date(timeIntervalSinceReferenceDate: (s.startDate.timeIntervalSinceReferenceDate / 60).rounded(.down) * 60)
            groups[minute, default: []].append(s)
        }
        return groups.keys.sorted().map { ($0, groups[$0]!) }
    }

    private func names(_ sources: [HKSource]?) -> [String] {
        (sources ?? []).map(\.name).sorted()
    }
}

/// Runs a HealthKit read, answering nil for HealthKit's "no data" error,
/// which it raises for a type the device has no source of (heart rate on a
/// phone with no watch) instead of answering empty.
func noDataIsEmpty<T>(_ read: () async throws -> T) async throws -> T? {
    do {
        return try await read()
    } catch let error as HKError where error.code == .errorNoData {
        return nil
    } catch let error as NSError where error.domain == HKErrorDomain && error.code == HKError.Code.errorNoData.rawValue {
        return nil
    }
}
