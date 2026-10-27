import CoreLocation
import Foundation
import HealthKit

/// Health Auto Export's JSON, Export Version 2, as the applehealth provider
/// reads it: `{"data":{"metrics":[{"name","units","data":[rows]}],
/// "workouts":[…]}}`. The names, units and field spellings are the ones
/// internal/synth/applehealth.go emits and the provider's tests accept, and
/// the goldens under internal/providertest/testdata/applehealth hold this
/// encoder to the receiver.
enum Grouping: String, CaseIterable, Sendable {
    case minutes
    case seconds

    var automationName: String { "Substrate Sync \(rawValue)" }
}

struct HealthMetric: Sendable {
    let name: String
    let units: String
    var rows: [JSON]
}

/// One local day at one grouping. A delivery replaces a metric's rows
/// between its first and last row, so it always holds the whole day.
struct HealthDelivery: Sendable {
    let grouping: Grouping
    var metrics: [HealthMetric]
    var workouts: [JSON]
    /// Cycle tracking entries, the `cycleTracking` section.
    var cycle: [JSON] = []
    /// What could not be read, as `name: reason`; never sent.
    var skipped: [String] = []

    var isEmpty: Bool { metrics.allSatisfy { $0.rows.isEmpty } && workouts.isEmpty && cycle.isEmpty }

    /// The bodies to POST, each under `limit` bytes: the day whole when it
    /// fits, else split by metric (a metric is never split, because its rows
    /// replace a span), with the workouts in bodies of their own.
    func bodies(limit: Int = 4 * 1024 * 1024) -> [Data] {
        let metrics = self.metrics.filter { !$0.rows.isEmpty }
        let whole = Self.body(metrics: metrics, workouts: workouts, cycle: cycle)
        if whole.count < limit { return metrics.isEmpty && workouts.isEmpty && cycle.isEmpty ? [] : [whole] }
        var out: [Data] = []
        var group: [HealthMetric] = []
        for metric in metrics {
            let next = Self.body(metrics: group + [metric], workouts: [])
            if next.count >= limit, !group.isEmpty {
                out.append(Self.body(metrics: group, workouts: []))
                group = [metric]
            } else {
                group.append(metric)
            }
        }
        if !group.isEmpty { out.append(Self.body(metrics: group, workouts: [])) }
        var batch: [JSON] = []
        for workout in workouts {
            if Self.body(metrics: [], workouts: batch + [workout]).count >= limit, !batch.isEmpty {
                out.append(Self.body(metrics: [], workouts: batch))
                batch = []
            }
            batch.append(workout)
        }
        if !batch.isEmpty { out.append(Self.body(metrics: [], workouts: batch)) }
        if !cycle.isEmpty { out.append(Self.body(metrics: [], workouts: [], cycle: cycle)) }
        return out
    }

    static func body(metrics: [HealthMetric], workouts: [JSON], cycle: [JSON] = []) -> Data {
        var data: [(String, JSON)] = [("metrics", .array(metrics.map { m in
            .object([("name", .string(m.name)), ("units", .string(m.units)), ("data", .array(m.rows))])
        }))]
        if !workouts.isEmpty { data.append(("workouts", .array(workouts))) }
        if !cycle.isEmpty { data.append(("cycleTracking", .array(cycle))) }
        return JSON.object([("data", .object(data))]).encoded
    }

    /// The headers the provider reads, beside the key: the five Health Auto
    /// Export adds on its own and the account.
    static func headers(
        grouping: Grouping, key: String, account: String, automationID: String, sessionID: String
    ) -> [(String, String)] {
        [
            ("Content-Type", "application/json"),
            ("Authorization", "Bearer \(key)"),
            ("X-Substrate-Account", account),
            ("automation-name", grouping.automationName),
            ("automation-id", automationID),
            ("automation-aggregation", grouping.rawValue),
            ("automation-period", "Since Last Sync"),
            ("session-id", sessionID),
        ]
    }
}

/// The rows, one builder per shape. `date` is the minute's start or the
/// sample's own instant, written in the phone's zone.
struct HealthRows {
    let zone: TimeZone

    func date(_ d: Date) -> JSON { .string(Formats.health(d, in: zone)) }

    static func source(_ names: [String]) -> String? {
        var seen: [String] = []
        for name in names where !name.isEmpty && !seen.contains(name) { seen.append(name) }
        return seen.isEmpty ? nil : seen.joined(separator: "|")
    }

    /// heart_rate: one minute's discrete min, average and max.
    func minMaxAvg(at: Date, min: Double, avg: Double, max: Double, sources: [String]) -> JSON {
        var f = Fields()
        f.set("Min", rounded(min, 6))
        f.set("Avg", rounded(avg, 6))
        f.set("Max", rounded(max, 6))
        f.set("date", date(at))
        f.set("source", Self.source(sources))
        return f.json
    }

    /// A cumulative sum per minute, or one minute's samples averaged.
    func quantity(at: Date, qty: Double, sources: [String], extra: [(String, JSON)] = []) -> JSON {
        var f = Fields()
        f.set("qty", rounded(qty, 6))
        f.set("date", date(at))
        for (k, v) in extra { f.set(k, v) }
        f.set("source", Self.source(sources))
        return f.json
    }

    func pressure(at: Date, systolic: Double, diastolic: Double, sources: [String]) -> JSON {
        var f = Fields()
        f.set("systolic", rounded(systolic, 6))
        f.set("diastolic", rounded(diastolic, 6))
        f.set("date", date(at))
        f.set("source", Self.source(sources))
        return f.json
    }
}

/// One `HKCategorySample` of sleep analysis, already named the way the app
/// names it ("In Bed", "Awake", "Core", "Deep", "REM", "Asleep").
struct SleepSegment: Sendable {
    let start: Date
    let end: Date
    let value: String
    let source: String

    var isAsleep: Bool { value != "In Bed" && value != "Awake" }
}

enum SleepNights {
    /// Segments closer than this belong to one night.
    static let gap: TimeInterval = 3600

    /// The sleep_analysis rows for the segments that START inside `day`, one
    /// per segment. Every segment but the In Bed one repeats its night's
    /// fields, worked out over `all` (which reaches past the day's ends, so
    /// a night crossing midnight is the same night in both deliveries).
    static func rows(all: [SleepSegment], day: Range<Date>, rows r: HealthRows) -> [JSON] {
        let sorted = all.sorted { ($0.start, $0.end, $0.source) < ($1.start, $1.end, $1.source) }
        var nights: [[SleepSegment]] = []
        var end = Date.distantPast
        for s in sorted {
            if nights.isEmpty || s.start.timeIntervalSince(end) > gap {
                nights.append([s])
                end = s.end
            } else {
                nights[nights.count - 1].append(s)
                end = max(end, s.end)
            }
        }
        var out: [JSON] = []
        for night in nights {
            let inBedStart = night.map(\.start).min()!
            let inBedEnd = night.map(\.end).max()!
            let asleep = night.filter(\.isAsleep)
            for s in night where day.contains(s.start) {
                var f = Fields()
                f.set("startDate", r.date(s.start))
                f.set("endDate", r.date(s.end))
                f.set("qty", rounded(s.end.timeIntervalSince(s.start) / 3600, 9))
                f.set("value", s.value)
                f.set("source", s.source)
                if s.value != "In Bed" {
                    if let first = asleep.map(\.start).min(), let last = asleep.map(\.end).max() {
                        f.set("sleepStart", r.date(first))
                        f.set("sleepEnd", r.date(last))
                    }
                    f.set("inBed", rounded(inBedEnd.timeIntervalSince(inBedStart) / 3600, 9))
                    f.set("inBedStart", r.date(inBedStart))
                    f.set("inBedEnd", r.date(inBedEnd))
                }
                out.append(f.json)
            }
        }
        return out
    }
}

/// One `HKWorkout`, read out of HealthKit into plain values.
struct WorkoutSummary: Sendable {
    struct Minute: Sendable {
        let at: Date
        let min: Double
        let avg: Double
        let max: Double
        let sources: [String]
    }

    let id: UUID
    let name: String
    let start: Date
    let end: Date
    let isIndoor: Bool
    /// HealthKit's own duration, which leaves out paused time; nil reads the
    /// span from start to end.
    var duration: TimeInterval?
    var activeKcal: Double?
    var distanceKm: Double?
    var heartRate: (min: Double, avg: Double, max: Double)?
    var elevationUpM: Double?
    var heartRateData: [Minute] = []
    var timeZone: String?
    /// The GPS track, thinned to WorkoutRoute.limit points.
    var route: [RoutePoint] = []

    func json(_ r: HealthRows) -> JSON {
        func qty(_ v: Double, _ units: String) -> JSON {
            .object([("qty", .double(rounded(v, 6))), ("units", .string(units))])
        }
        var f = Fields()
        f.set("id", id.uuidString.uppercased())
        f.set("name", name)
        f.set("start", r.date(start))
        f.set("end", r.date(end))
        f.set("duration", (duration ?? end.timeIntervalSince(start)).rounded(.down))
        f.set("isIndoor", isIndoor)
        f.set("location", isIndoor ? "Indoor" : "Outdoor")
        if let activeKcal { f.set("activeEnergyBurned", qty(activeKcal, "kcal")) }
        if let distanceKm { f.set("distance", qty(distanceKm, "km")) }
        if let hr = heartRate {
            f.set("heartRate", JSON.object([("min", qty(hr.min, "bpm")), ("avg", qty(hr.avg, "bpm")), ("max", qty(hr.max, "bpm"))]))
            f.set("avgHeartRate", qty(hr.avg, "bpm"))
            f.set("maxHeartRate", qty(hr.max, "bpm"))
        }
        if let elevationUpM { f.set("elevationUp", qty(elevationUpM, "m")) }
        f.set("heartRateData", .array(heartRateData.map { m in
            var row = Fields()
            row.set("date", r.date(m.at))
            row.set("Min", rounded(m.min, 6))
            row.set("Avg", rounded(m.avg, 6))
            row.set("Max", rounded(m.max, 6))
            row.set("units", "bpm")
            row.set("source", HealthRows.source(m.sources))
            return row.json
        }))
        if !route.isEmpty { f.set("route", .array(route.map { $0.json(r) })) }
        var meta = Fields()
        meta.set("HKTimeZone", timeZone)
        // The app writes an empty metadata object for a workout with none.
        return .object(f.pairs + [("metadata", meta.json)])
    }
}

/// The Fitness app's name for a workout, which the applehealth provider's
/// adapter keys its SPORTS table on
/// (kinds/providers.substrate.reamde.dev/applehealth/bundle.yaml).
enum WorkoutNames {
    static func name(_ activity: HKWorkoutActivityType, indoor: Bool, openWater: Bool) -> String {
        switch activity {
        case .running: indoor ? "Indoor Run" : "Outdoor Run"
        case .walking: indoor ? "Indoor Walk" : "Outdoor Walk"
        case .cycling: indoor ? "Indoor Cycle" : "Outdoor Cycle"
        case .swimming: openWater ? "Open Water Swim" : "Pool Swim"
        case .hiking: "Hiking"
        case .yoga: "Yoga"
        case .pilates: "Pilates"
        case .rowing: "Rowing"
        case .elliptical: "Elliptical"
        case .stairClimbing: "Stair Stepper"
        case .stairs: "Stairs"
        case .traditionalStrengthTraining: "Traditional Strength Training"
        case .functionalStrengthTraining: "Functional Strength Training"
        case .highIntensityIntervalTraining: "High Intensity Interval Training"
        case .cardioDance: "Cardio Dance"
        case .socialDance: "Social Dance"
        case .boxing: "Boxing"
        case .kickboxing: "Kickboxing"
        case .climbing: "Climbing"
        case .tennis: "Tennis"
        case .golf: "Golf"
        case .downhillSkiing: "Downhill Skiing"
        case .crossCountrySkiing: "Cross Country Skiing"
        case .coreTraining: "Core Training"
        case .cooldown: "Cooldown"
        case .mixedCardio: "Mixed Cardio"
        case .flexibility: "Flexibility"
        case .mindAndBody: "Mind and Body"
        case .soccer: "Soccer"
        case .basketball: "Basketball"
        case .pickleball: "Pickleball"
        case .jumpRope: "Jump Rope"
        default: "Other"
        }
    }
}

/// One point of a workout's GPS track, as Health Auto Export writes it with
/// Include Route Data. A value CoreLocation marks invalid (negative) is left
/// out rather than sent as -1.
struct RoutePoint: Sendable {
    let at: Date
    let latitude: Double
    let longitude: Double
    var altitude: Double?
    var course: Double?
    var courseAccuracy: Double?
    var horizontalAccuracy: Double?
    var verticalAccuracy: Double?
    var speed: Double?
    var speedAccuracy: Double?

    init(at: Date, latitude: Double, longitude: Double) {
        self.at = at
        self.latitude = latitude
        self.longitude = longitude
    }

    init(_ l: CLLocation) {
        self.init(at: l.timestamp, latitude: l.coordinate.latitude, longitude: l.coordinate.longitude)
        func valid(_ v: Double) -> Double? { v >= 0 ? v : nil }
        if l.verticalAccuracy >= 0 {
            altitude = l.altitude
            verticalAccuracy = l.verticalAccuracy
        }
        course = valid(l.course)
        courseAccuracy = course == nil ? nil : valid(l.courseAccuracy)
        horizontalAccuracy = valid(l.horizontalAccuracy)
        speed = valid(l.speed)
        speedAccuracy = speed == nil ? nil : valid(l.speedAccuracy)
    }

    func json(_ r: HealthRows) -> JSON {
        var f = Fields()
        f.set("latitude", rounded(latitude, 7))
        f.set("longitude", rounded(longitude, 7))
        f.set("altitude", altitude.map { rounded($0, 2) })
        f.set("course", course.map { rounded($0, 2) })
        f.set("courseAccuracy", courseAccuracy.map { rounded($0, 2) })
        f.set("horizontalAccuracy", horizontalAccuracy.map { rounded($0, 2) })
        f.set("verticalAccuracy", verticalAccuracy.map { rounded($0, 2) })
        f.set("timestamp", r.date(at))
        f.set("speed", speed.map { rounded($0, 3) })
        f.set("speedAccuracy", speedAccuracy.map { rounded($0, 3) })
        return f.json
    }
}

enum WorkoutRoute {
    /// The provider skips a workout record over 6 MiB; a point is about 250
    /// bytes stored, so a track is thinned to this many, about four hours at
    /// one a second.
    static let limit = 15_000

    /// Every point when they fit, else every nth and always the last.
    static func thinned(_ points: [CLLocation], limit: Int = limit) -> [RoutePoint] {
        thinned(points.map(RoutePoint.init), limit: limit)
    }

    static func thinned(_ points: [RoutePoint], limit: Int = limit) -> [RoutePoint] {
        guard points.count > limit, limit > 1 else { return points }
        let step = Int((Double(points.count - 1) / Double(limit - 1)).rounded(.up))
        var out = stride(from: 0, to: points.count, by: step).map { points[$0] }
        if out.count < limit, let last = points.last, (points.count - 1) % step != 0 { out.append(last) }
        return out
    }
}

/// Cycle tracking as Health Auto Export's `cycleTracking` section carries it:
/// one entry per sample, by the Health app's name for the type and for its
/// value.
enum CycleTracking {
    static let types: [(HKCategoryTypeIdentifier, String)] = [
        (.menstrualFlow, "Menstrual Flow"),
        (.intermenstrualBleeding, "Intermenstrual Bleeding"),
        (.ovulationTestResult, "Ovulation Test Result"),
        (.cervicalMucusQuality, "Cervical Mucus Quality"),
        (.sexualActivity, "Sexual Activity"),
        (.contraceptive, "Contraceptive"),
        (.pregnancyTestResult, "Pregnancy Test Result"),
        (.progesteroneTestResult, "Progesterone Test Result"),
        (.lactation, "Lactation"),
        (.pregnancy, "Pregnancy"),
    ]

    static func value(_ id: HKCategoryTypeIdentifier, _ raw: Int, _ metadata: [String: Any]?) -> String {
        switch id {
        case .menstrualFlow:
            switch HKCategoryValueVaginalBleeding(rawValue: raw) {
            case .light: return "Light"
            case .medium: return "Medium"
            case .heavy: return "Heavy"
            case .none: return "None"
            default: return "Unspecified"
            }
        case .ovulationTestResult:
            switch HKCategoryValueOvulationTestResult(rawValue: raw) {
            case .negative: return "Negative"
            case .luteinizingHormoneSurge: return "LH Surge"
            case .estrogenSurge: return "Estrogen Surge"
            default: return "Indeterminate"
            }
        case .cervicalMucusQuality:
            switch HKCategoryValueCervicalMucusQuality(rawValue: raw) {
            case .dry: return "Dry"
            case .sticky: return "Sticky"
            case .creamy: return "Creamy"
            case .watery: return "Watery"
            case .eggWhite: return "Egg White"
            default: return ""
            }
        case .sexualActivity:
            switch (metadata?[HKMetadataKeySexualActivityProtectionUsed] as? NSNumber)?.boolValue {
            case true?: return "Protection Used"
            case false?: return "Protection Not Used"
            case nil: return "Unspecified"
            }
        case .contraceptive:
            switch HKCategoryValueContraceptive(rawValue: raw) {
            case .implant: return "Implant"
            case .injection: return "Injection"
            case .intrauterineDevice: return "IUD"
            case .intravaginalRing: return "Intravaginal Ring"
            case .oral: return "Oral"
            case .patch: return "Patch"
            default: return "Unspecified"
            }
        case .pregnancyTestResult:
            switch HKCategoryValuePregnancyTestResult(rawValue: raw) {
            case .negative: return "Negative"
            case .positive: return "Positive"
            default: return "Indeterminate"
            }
        case .progesteroneTestResult:
            switch HKCategoryValueProgesteroneTestResult(rawValue: raw) {
            case .negative: return "Negative"
            case .positive: return "Positive"
            default: return "Indeterminate"
            }
        default:
            return ""
        }
    }

    static func entry(
        name: String, value: String, start: Date, end: Date, cycleStart: Bool?, source: String, rows r: HealthRows
    ) -> JSON {
        var f = Fields()
        f.set("start", r.date(start))
        f.set("end", end > start ? r.date(end) : nil)
        f.set("name", name)
        f.set("value", value)
        f.set("isCycleStart", cycleStart)
        f.set("source", source)
        return f.json
    }
}
