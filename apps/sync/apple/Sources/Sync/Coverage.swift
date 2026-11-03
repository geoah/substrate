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

/// The local days the phone holds steps, heart rate or sleep on, kept
/// between launches. Reading years of samples takes minutes, so it is done
/// once, 90 days at a time and saved after each, and every later scan reads
/// only the last `recheck` days and any stretch further back than it has
/// read. Home, Coverage and a sync share it: an ask while a scan runs waits
/// for that scan, then reads what it still lacks.
actor HealthScan {
    static let shared = HealthScan()

    /// What the scans have read. It describes the phone, not a repository,
    /// so a new sign-in keeps it.
    struct Stored: Codable, Equatable, Sendable {
        var days: Set<String> = []
        /// The first and the last local day read whole.
        var from: String?
        var through: String?
    }

    /// One read: a stretch of whole days, and which end of what was read it
    /// moves.
    struct Read: Equatable, Sendable {
        enum End: Equatable, Sendable { case from, through }
        let days: Range<Date>
        let moves: End
    }

    /// The days at the end every scan reads again: a watch can reach the
    /// phone a day or more after its samples were taken.
    static let recheck = 7
    /// The days one read covers: a stall-free step for a sync's progress,
    /// and the most a stopped scan loses.
    static let step = 90

    /// Reports (reads done, reads planned, the days known so far).
    typealias Progress = @Sendable (Int, Int, Set<String>) async -> Void

    private var stored = HealthScan.load()
    private var running: Task<Set<String>, Error>?
    private var listeners: [UUID: Progress] = [:]

    private static var url: URL {
        let dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir.appendingPathComponent("health-scan.json")
    }

    /// What the last scan left, for a screen to show before any scan runs.
    nonisolated static func load() -> Stored {
        guard let data = try? Data(contentsOf: url) else { return Stored() }
        return (try? JSONDecoder().decode(Stored.self, from: data)) ?? Stored()
    }

    private func save() {
        guard let data = try? JSONEncoder().encode(stored) else { return }
        // Readable after the first unlock, for a background sync.
        try? data.write(to: Self.url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }

    /// The days with data from `since` (everything HealthKit keeps, when
    /// nil) to today, reading only what has not been read. A caller that
    /// stops waiting stops the scan; what it read stays.
    func days(since: Date?, store: HKHealthStore, calendar: Calendar, progress: Progress? = nil) async throws -> Set<String> {
        let id = UUID()
        if let progress { listeners[id] = progress }
        defer { listeners[id] = nil }
        while let running { _ = try? await running.value }
        let task = Task { try await self.scan(since: since, store: store, calendar: calendar) }
        running = task
        defer { running = nil }
        return try await withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }
    }

    /// Drops a day the sync read and found empty: data deleted from Health
    /// leaves no day waiting to be sent forever.
    func forget(_ day: String) {
        guard stored.days.remove(day) != nil else { return }
        save()
    }

    /// Forgets every read, so the next scan reads the whole history again:
    /// for data an app wrote into days already read.
    func reset() {
        guard running == nil else { return }
        stored = Stored()
        save()
    }

    private func scan(since: Date?, store: HKHealthStore, calendar: Calendar) async throws -> Set<String> {
        let parse = Coverage.dayParser(calendar)
        let today = calendar.startOfDay(for: Date())
        let earliest = calendar.startOfDay(for: store.earliestPermittedSampleDate())
        let target = since.map { max(calendar.startOfDay(for: $0), earliest) } ?? earliest
        let reads = Self.plan(
            target: target, today: today, from: stored.from.flatMap(parse.date(from:)),
            through: stored.through.flatMap(parse.date(from:)), calendar: calendar)
        for (i, read) in reads.enumerated() {
            await report(i, reads.count)
            try Task.checkCancellation()
            let found = try await Self.daysWithData(read.days, store: store, calendar: calendar)
            let first = Formats.day(read.days.lowerBound, in: calendar.timeZone)
            let last = Formats.day(calendar.date(byAdding: .day, value: -1, to: read.days.upperBound)!, in: calendar.timeZone)
            stored.days = stored.days.filter { $0 < first || $0 > last }.union(found)
            switch read.moves {
            case .from:
                stored.from = first
                if stored.through == nil { stored.through = last }
            case .through:
                if stored.from.map({ first < $0 }) ?? true { stored.from = first }
                stored.through = last
            }
            save()
        }
        await report(reads.count, reads.count)
        let first = Formats.day(target, in: calendar.timeZone)
        return stored.days.filter { $0 >= first }
    }

    private func report(_ done: Int, _ total: Int) async {
        for listener in listeners.values { await listener(done, total, stored.days) }
    }

    /// The reads a scan makes, in order, each at most `step` days: the
    /// stretch before what was read, newest first, so `from` only moves
    /// back; then from the last `recheck` days, or from where an unfinished
    /// first scan stopped, to today.
    static func plan(target: Date, today: Date, from: Date?, through: Date?, calendar: Calendar) -> [Read] {
        func day(_ d: Date, _ n: Int) -> Date { calendar.date(byAdding: .day, value: n, to: d)! }
        let end = day(today, 1)
        var reads: [Read] = []
        var forward = target
        if let from, let through {
            var upper = from
            while upper > target {
                let lower = max(day(upper, -step), target)
                reads.append(Read(days: lower ..< upper, moves: .from))
                upper = lower
            }
            forward = max(min(day(through, 1), day(today, -(recheck - 1))), from)
        }
        while forward < end {
            let upper = min(day(forward, step), end)
            reads.append(Read(days: forward ..< upper, moves: .through))
            forward = upper
        }
        return reads
    }

    /// The days in `range` holding steps, heart rate or sleep: one daily
    /// statistics query per quantity, so a stretch is a few buckets and not
    /// a pass over every sample.
    private static func daysWithData(_ range: Range<Date>, store: HKHealthStore, calendar: Calendar) async throws -> Set<String> {
        let end = min(range.upperBound, Date())
        guard range.lowerBound < end else { return [] }
        let predicate = HKQuery.predicateForSamples(withStart: range.lowerBound, end: end, options: .strictStartDate)
        var days: Set<String> = []
        for (id, options) in [(HKQuantityTypeIdentifier.stepCount, HKStatisticsOptions.cumulativeSum),
                              (.heartRate, .discreteAverage)] {
            guard let collection = try await noDataIsEmpty({
                try await HKStatisticsCollectionQueryDescriptor(
                    predicate: .quantitySample(type: HKQuantityType(id), predicate: predicate),
                    options: options, anchorDate: range.lowerBound, intervalComponents: DateComponents(day: 1)
                ).result(for: store)
            }) else { continue }
            collection.enumerateStatistics(from: range.lowerBound, to: end) { s, _ in
                if s.sumQuantity() != nil || s.averageQuantity() != nil {
                    days.insert(Formats.day(s.startDate, in: calendar.timeZone))
                }
            }
            try Task.checkCancellation()
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
