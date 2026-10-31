import CoreLocation
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
        // What is logged: a drink, a meal's nutrients.
        .init(id: .dietaryWater, name: "dietary_water", units: "mL", unit: .literUnit(with: .milli)),
        .init(id: .dietaryEnergyConsumed, name: "dietary_energy", units: "kcal", unit: .kilocalorie()),
        .init(id: .dietaryProtein, name: "protein", units: "g", unit: .gram()),
        .init(id: .dietaryCarbohydrates, name: "carbohydrates", units: "g", unit: .gram()),
        .init(id: .dietaryFatTotal, name: "total_fat", units: "g", unit: .gram()),
        .init(id: .dietaryFatSaturated, name: "saturated_fat", units: "g", unit: .gram()),
        .init(id: .dietarySugar, name: "dietary_sugar", units: "g", unit: .gram()),
        .init(id: .dietaryFiber, name: "fiber", units: "g", unit: .gram()),
        .init(id: .dietarySodium, name: "sodium", units: "mg", unit: .gramUnit(with: .milli)),
        .init(id: .dietaryCholesterol, name: "cholesterol", units: "mg", unit: .gramUnit(with: .milli)),
        .init(id: .dietaryCaffeine, name: "caffeine", units: "mg", unit: .gramUnit(with: .milli)),
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
        .init(id: .basalBodyTemperature, name: "basal_body_temperature", units: "degC", unit: .degreeCelsius()),
    ]

    /// Everything the app reads.
    static var readTypes: Set<HKObjectType> {
        var types: Set<HKObjectType> = [
            HKQuantityType(.heartRate), HKQuantityType(.bloodGlucose),
            HKQuantityType(.bloodPressureSystolic), HKQuantityType(.bloodPressureDiastolic),
            HKCategoryType(.sleepAnalysis), HKObjectType.workoutType(),
            HKQuantityType(.distanceCycling), HKQuantityType(.distanceSwimming),
            HKCategoryType(.mindfulSession), HKSeriesType.workoutRoute(),
        ]
        for (id, _) in CycleTracking.types { types.insert(HKCategoryType(id)) }
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

        // A session's minutes at the minute it starts, the part inside the day.
        await add("mindful_minutes", "min") {
            var minutes: [Date: (qty: Double, sources: [String])] = [:]
            for s in try await categorySamples(.mindfulSession, overlapping: day) {
                let start = max(s.startDate, day.lowerBound), end = min(s.endDate, day.upperBound)
                guard end > start else { continue }
                let minute = Date(timeIntervalSinceReferenceDate: (start.timeIntervalSinceReferenceDate / 60).rounded(.down) * 60)
                minutes[minute, default: (0, [])].qty += end.timeIntervalSince(start) / 60
                minutes[minute, default: (0, [])].sources.append(s.sourceRevision.source.name)
            }
            return minutes.keys.sorted().map { rows.quantity(at: $0, qty: minutes[$0]!.qty, sources: minutes[$0]!.sources) }
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

        // Cycle tracking: each entry that starts in the day, by its Health
        // name and its value's.
        for (id, name) in CycleTracking.types {
            do {
                for s in try await categorySamples(id, startingIn: day) {
                    delivery.cycle.append(CycleTracking.entry(
                        name: name, value: CycleTracking.value(id, s.value, s.metadata), start: s.startDate, end: s.endDate,
                        cycleStart: id == .menstrualFlow ? (s.metadata?[HKMetadataKeyMenstrualCycleStart] as? NSNumber)?.boolValue : nil,
                        source: s.sourceRevision.source.name, rows: rows))
                }
            } catch {
                delivery.skipped.append("\(name): \(error.localizedDescription)")
            }
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
            start: w.startDate, end: w.endDate, isIndoor: indoor, duration: w.duration)
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
        s.route = WorkoutRoute.thinned(try await route(w))
        return s
    }

    /// The workout's GPS track, every point of every route sample.
    private func route(_ w: HKWorkout) async throws -> [CLLocation] {
        let routes = try await noDataIsEmpty {
            try await HKSampleQueryDescriptor(
                predicates: [.workoutRoute(HKQuery.predicateForObjects(from: w))],
                sortDescriptors: [SortDescriptor(\.startDate)]
            ).result(for: store)
        } ?? []
        var points: [CLLocation] = []
        for r in routes {
            for try await location in HKWorkoutRouteQueryDescriptor(r).results(for: store) {
                points.append(location)
            }
        }
        return points
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

    private func categorySamples(_ id: HKCategoryTypeIdentifier, startingIn day: Range<Date>) async throws -> [HKCategorySample] {
        let predicate = HKQuery.predicateForSamples(withStart: day.lowerBound, end: day.upperBound, options: .strictStartDate)
        return try await categorySamples(id, predicate)
    }

    private func categorySamples(_ id: HKCategoryTypeIdentifier, overlapping day: Range<Date>) async throws -> [HKCategorySample] {
        try await categorySamples(id, HKQuery.predicateForSamples(withStart: day.lowerBound, end: day.upperBound))
    }

    private func categorySamples(_ id: HKCategoryTypeIdentifier, _ predicate: NSPredicate) async throws -> [HKCategorySample] {
        try await noDataIsEmpty {
            try await HKSampleQueryDescriptor(
                predicates: [.categorySample(type: HKCategoryType(id), predicate: predicate)],
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
/// phone with no watch) instead of answering empty, and for a type the owner
/// was never asked about, which reads as a refused one does: as nothing.
/// Failing it instead failed every day of a phone that allowed an earlier
/// build's types, so no day was ever counted sent.
func noDataIsEmpty<T>(_ read: () async throws -> T) async throws -> T? {
    do {
        return try await read()
    } catch let error as HKError where HealthReader.readsAsEmpty.contains(error.code) {
        return nil
    } catch let error as NSError where error.domain == HKErrorDomain
        && HealthReader.readsAsEmpty.map(\.rawValue).contains(error.code) {
        return nil
    }
}

extension HealthReader {
    /// The HealthKit errors a read answers as nothing.
    static let readsAsEmpty: [HKError.Code] = [.errorNoData, .errorAuthorizationNotDetermined]

    /// Whether HealthKit would ask the owner about a type the app reads: a
    /// type a later build added was never asked about, and reads as nothing
    /// until it is.
    static func shouldRequestAccess(_ store: HKHealthStore) async -> Bool {
        guard isAvailable else { return false }
        return (try? await store.statusForAuthorizationRequest(toShare: SampleData.shareTypes, read: readTypes)) == .shouldRequest
    }
}
