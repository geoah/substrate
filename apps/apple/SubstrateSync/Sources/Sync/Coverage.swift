import Foundation
import HealthKit

/// How a coverage view groups time.
enum Granularity: String, CaseIterable, Identifiable, Sendable {
    case day, week, month, year

    var id: String { rawValue }
    var title: String { rawValue.capitalized + "s" }
    var component: Calendar.Component {
        switch self {
        case .day: .day
        case .week: .weekOfYear
        case .month: .month
        case .year: .year
        }
    }

    /// The grouping that keeps a chart of `days` to a few dozen bars at most.
    static func fitting(days: Int) -> Granularity {
        switch days {
        case ..<32: .day
        case ..<200: .week
        case ..<1100: .month
        default: .year
        }
    }
}

/// One stretch of time: how much the device holds in it and how much of
/// that has been synced.
struct CoverageBucket: Identifiable, Equatable, Sendable {
    let start: Date
    let label: String
    let available: Int
    let synced: Int
    /// Health: the days with data that were never sent, oldest first.
    let missing: [String]

    var id: Date { start }
    var fraction: Double { available == 0 ? 1 : Double(synced) / Double(available) }
}

enum Coverage {
    /// Health, by day: a day counts as available when the device has data
    /// for it, and as synced when both its deliveries were accepted.
    static func days(
        available: Set<String>, synced: Set<String>, by granularity: Granularity, calendar: Calendar
    ) -> [CoverageBucket] {
        let parse = dayParser(calendar)
        var groups: [Date: (available: Int, synced: Int, missing: [String])] = [:]
        for day in available {
            guard let date = parse.date(from: day), let start = calendar.dateInterval(of: granularity.component, for: date)?.start
            else { continue }
            var g = groups[start] ?? (0, 0, [])
            g.available += 1
            if synced.contains(day) { g.synced += 1 } else { g.missing.append(day) }
            groups[start] = g
        }
        return groups.keys.sorted().map { start in
            let g = groups[start]!
            return CoverageBucket(
                start: start, label: label(start, granularity, calendar), available: g.available, synced: g.synced,
                missing: g.missing.sorted())
        }
    }

    /// Calendars, by occurrence start: what the device's window holds and
    /// what was pushed.
    static func events(
        available: [Date], synced: [Date], by granularity: Granularity, calendar: Calendar
    ) -> [CoverageBucket] {
        var groups: [Date: (available: Int, synced: Int)] = [:]
        for (dates, isSynced) in [(available, false), (synced, true)] {
            for date in dates {
                guard let start = calendar.dateInterval(of: granularity.component, for: date)?.start else { continue }
                var g = groups[start] ?? (0, 0)
                if isSynced { g.synced += 1 } else { g.available += 1 }
                groups[start] = g
            }
        }
        return groups.keys.sorted().map { start in
            let g = groups[start]!
            return CoverageBucket(
                start: start, label: label(start, granularity, calendar), available: g.available,
                synced: min(g.synced, g.available), missing: [])
        }
    }

    static func label(_ start: Date, _ granularity: Granularity, _ calendar: Calendar) -> String {
        let f = DateFormatter()
        f.calendar = calendar
        f.timeZone = calendar.timeZone
        f.locale = Locale(identifier: "en_US_POSIX")
        switch granularity {
        case .day: f.dateFormat = "EEE d MMM yyyy"
        case .week: f.dateFormat = "'Week of' d MMM yyyy"
        case .month: f.dateFormat = "MMMM yyyy"
        case .year: f.dateFormat = "yyyy"
        }
        return f.string(from: start)
    }

    static func dayParser(_ calendar: Calendar) -> DateFormatter {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.calendar = calendar
        f.timeZone = calendar.timeZone
        f.dateFormat = "yyyy-MM-dd"
        return f
    }
}

extension HealthReader {
    /// The local days, back to `years` ago, on which the device holds steps,
    /// heart rate or sleep: one daily statistics query per type, so a decade
    /// is a few thousand buckets and not a scan of every sample.
    func availableDays(years: Int, calendar: Calendar) async throws -> Set<String> {
        let floor = calendar.date(byAdding: .year, value: -years, to: calendar.startOfDay(for: Date()))!
        return try await availableDays(since: floor, calendar: calendar)
    }

    /// The days from `since` (everything HealthKit keeps, when nil) that
    /// hold steps, heart rate or sleep.
    func availableDays(since: Date?, calendar: Calendar) async throws -> Set<String> {
        let now = Date()
        let earliest = calendar.startOfDay(for: store.earliestPermittedSampleDate())
        let start = since.map { max(calendar.startOfDay(for: $0), earliest) } ?? earliest
        let predicate = HKQuery.predicateForSamples(withStart: start, end: now)
        var days: Set<String> = []
        for (id, options) in [(HKQuantityTypeIdentifier.stepCount, HKStatisticsOptions.cumulativeSum),
                              (.heartRate, .discreteAverage)] {
            guard let collection = try await noDataIsEmpty({
                try await HKStatisticsCollectionQueryDescriptor(
                    predicate: .quantitySample(type: HKQuantityType(id), predicate: predicate),
                    options: options, anchorDate: start, intervalComponents: DateComponents(day: 1)
                ).result(for: store)
            }) else { continue }
            collection.enumerateStatistics(from: start, to: now) { s, _ in
                if s.sumQuantity() != nil || s.averageQuantity() != nil {
                    days.insert(Formats.day(s.startDate, in: calendar.timeZone))
                }
            }
        }
        let sleep = try await noDataIsEmpty {
            try await HKSampleQueryDescriptor(
                predicates: [.categorySample(type: HKCategoryType(.sleepAnalysis), predicate: predicate)],
                sortDescriptors: []
            ).result(for: store)
        } ?? []
        for s in sleep { days.insert(Formats.day(s.startDate, in: calendar.timeZone)) }
        return days
    }
}
