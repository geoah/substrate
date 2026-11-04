import Foundation
import SubstrateCore

/// Hevy's workout export, `workout_data.csv` (Profile → Settings → Export &
/// Import Data → Export Data → Export Workouts): one row per set, a workout
/// told apart by its title, start and end, an exercise by its title only.
/// The header names the unit: `weight_kg` and `distance_km`, or
/// `weight_lbs` and `distance_miles`. Times are local wall-clock minutes,
/// `30 Oct 2026, 13:43`, with no zone.
public enum HevyCSV {
    public struct Row: Hashable, Sendable {
        /// The line the row starts on, from 1, the header being line 1.
        public var line: Int
        public var title: String
        public var start: DateComponents
        public var end: DateComponents?
        public var description: String
        public var exercise: String
        /// Hevy's group number within the workout; equal numbers on
        /// neighbouring exercises are one superset.
        public var superset: String?
        public var exerciseNotes: String
        public var setIndex: Int?
        /// `normal`, `warmup`, `failure`, `dropset`, as Hevy wrote it.
        public var setType: String
        public var weight: Mass?
        public var reps: Int?
        public var distance: Length?
        public var duration: Seconds?
        public var rpe: Decimal?
        /// Hevy's exercise template id, which the API gives and the CSV does not.
        public var templateID: String? = nil
        /// The Hevy routine the workout was started from, from the API.
        public var routineID: String? = nil
        /// Hevy's workout id, from the API: what tells two workouts apart.
        public var workoutID: String? = nil
        /// The workout's start and end as instants, from the API; the CSV
        /// has only the wall clock.
        public var startInstant: Date? = nil
        public var endInstant: Date? = nil
        /// A stair machine's floors or steps (Hevy's `custom_metric`).
        public var customMetric: Decimal? = nil
        /// No set: it carries a workout with no exercise, or an exercise with
        /// no set, so the session and its notes are kept.
        public var placeholder = false
    }

    public struct File: Sendable {
        public var rows: [Row]
        /// Rows skipped, with their line and why.
        public var unreadable: [HevyImport.Problem]
    }

    static let required = ["title", "start_time", "exercise_title", "set_index", "set_type"]

    /// The rows of an export. A file that is not one throws; a row that does
    /// not read is skipped and listed, the rest still read.
    public static func read(_ text: String) throws(HevyImportError) -> File {
        let lines = CSVReader.lines(text)
        guard let header = lines.first else { throw .empty }
        var columns: [String: Int] = [:]
        for (index, name) in header.fields.enumerated() {
            columns[name.trimmingCharacters(in: .whitespaces).lowercased()] = index
        }
        if columns["exercise name"] != nil, columns["set order"] != nil { throw .strongExport }
        let missing = required.filter { columns[$0] == nil }
        guard missing.isEmpty else { throw .missingColumns(missing) }
        let massUnit: MassUnit = columns["weight_lbs"] != nil && columns["weight_kg"] == nil ? .lb : .kg
        let weightColumn = massUnit == .lb ? "weight_lbs" : "weight_kg"
        let distanceColumns: [(String, Decimal)] = [
            ("distance_km", 1000), ("distance_miles", Decimal(string: "1609.344")!),
            ("distance_meters", 1), ("distance_m", 1),
        ]

        var rows: [Row] = []
        var unreadable: [HevyImport.Problem] = []
        for line in lines.dropFirst() {
            func field(_ name: String) -> String {
                guard let index = columns[name], index < line.fields.count else { return "" }
                return line.fields[index].trimmingCharacters(in: .whitespacesAndNewlines)
            }
            func problem(_ reason: String) { unreadable.append(HevyImport.Problem(line: line.number, reason: reason)) }

            guard let start = parseTime(field("start_time")) else {
                problem("the start time \u{201C}\(field("start_time"))\u{201D} is not a date")
                continue
            }
            let exercise = field("exercise_title")
            guard !exercise.isEmpty else {
                problem("the row names no exercise")
                continue
            }
            var row = Row(
                line: line.number, title: field("title"), start: start, end: parseTime(field("end_time")),
                description: field("description"), exercise: exercise,
                superset: field("superset_id").nilIfEmpty, exerciseNotes: field("exercise_notes"),
                setIndex: Int(field("set_index")), setType: field("set_type").lowercased())
            do {
                if let weight = try number(field(weightColumn), weightColumn) { row.weight = Mass(value: weight, unit: massUnit) }
                row.reps = try number(field("reps"), "reps").map { NSDecimalNumber(decimal: $0).intValue }
                for (name, metres) in distanceColumns where row.distance == nil {
                    if let distance = try number(field(name), name) { row.distance = Length(metres: distance * metres) }
                }
                row.duration = try number(field("duration_seconds"), "duration_seconds").map { value in
                    var rounded = Decimal()
                    var value = value
                    NSDecimalRound(&rounded, &value, 0, .plain)
                    return NSDecimalNumber(decimal: rounded).intValue
                }
                if let rpe = try number(field("rpe"), "rpe") {
                    guard rpe >= 0, rpe <= 10 else { throw RowProblem("the RPE \(DecimalText.format(rpe)) is outside 0 to 10") }
                    row.rpe = rpe
                }
            } catch let error as RowProblem {
                problem(error.reason)
                continue
            } catch {
                continue
            }
            rows.append(row)
        }
        return File(rows: rows, unreadable: unreadable)
    }

    private struct RowProblem: Error {
        var reason: String
        init(_ reason: String) { self.reason = reason }
    }

    private static func number(_ text: String, _ column: String) throws -> Decimal? {
        guard !text.isEmpty else { return nil }
        guard let value = DecimalText.parse(text), value >= 0 else { throw RowProblem("\(column) \u{201C}\(text)\u{201D} is not a number") }
        return value
    }

    /// `30 Oct 2026, 13:43`, the day padded or not, as Hevy writes it in
    /// every locale; an ISO 8601 local time as a fallback. The minute on the
    /// wall, with no zone: the importer places it in one.
    static func parseTime(_ text: String) -> DateComponents? {
        guard !text.isEmpty else { return nil }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(identifier: "UTC")
        for format in [
            "d MMM yyyy, HH:mm", "d MMM yyyy HH:mm", "d MMM yyyy, HH:mm:ss", "yyyy-MM-dd'T'HH:mm:ss", "yyyy-MM-dd HH:mm:ss",
            "yyyy-MM-dd'T'HH:mm", "yyyy-MM-dd HH:mm",
        ] {
            formatter.dateFormat = format
            if let date = formatter.date(from: text) {
                var calendar = Calendar(identifier: .gregorian)
                calendar.timeZone = formatter.timeZone
                return calendar.dateComponents([.year, .month, .day, .hour, .minute, .second], from: date)
            }
        }
        return nil
    }
}

public enum HevyImportError: Error, Hashable, Sendable, LocalizedError {
    case empty
    case missingColumns([String])
    case strongExport
    case unreadableText

    public var errorDescription: String? {
        switch self {
        case .empty:
            "The file is empty."
        case let .missingColumns(columns):
            "This is not a Hevy workout export: it has no \(columns.joined(separator: ", ")) column."
        case .strongExport:
            "This looks like a Strong export. Only Hevy\u{2019}s workout export can be imported."
        case .unreadableText:
            "The file is not text."
        }
    }
}

/// Hevy's workouts as the training package's sessions and sets (the
/// package README, "Importing"), and the exercises they name that the
/// catalogue lacks. Pure: what it reads comes in, what it would write comes
/// out, and the caller decides.
public enum HevyImport {
    public struct Problem: Hashable, Sendable {
        public var line: Int
        public var reason: String

        public init(line: Int, reason: String) {
            self.line = line
            self.reason = reason
        }
    }

    /// Why a workout of the file is not imported.
    public enum Skip: Hashable, Sendable {
        /// Its session is in History already, from an earlier import.
        case imported
        /// Its session was imported once and deleted from History since.
        case deleted
        /// Its time overlaps a session History holds: the same workout
        /// logged in this app as well.
        case overlaps(SessionID)
    }

    public struct Skipped: Hashable, Sendable {
        public var title: String
        public var start: Date
        public var reason: Skip
    }

    public struct Plan: Sendable {
        /// Oldest first.
        public var sessions: [SessionRecords] = []
        /// Exercises the sessions and routines name that the catalogue
        /// lacks, and the account's own Hevy exercises, to add before them.
        public var exercises: [Exercise] = []
        /// Catalogue exercises the file's titles were matched to, by title.
        public var matched: [String: ExerciseID] = [:]
        /// Hevy's routines as workout routines the catalogue lacks.
        public var routines: [Routine] = []
        /// Hevy routines the catalogue holds already, by title: left as they
        /// are, and their sessions linked to them.
        public var routinesHeld: [String] = []
        /// Body measurements, one per Hevy entry, as
        /// `~/training/bodymeasurement` records.
        public var measurements: [FlatRecord] = []
        public var skipped: [Skipped] = []
        public var problems: [Problem] = []
        /// Sets whose weight the matched exercise does not record (one that
        /// counts reps or time only), by Hevy's title: imported without it.
        public var weightsNotRecorded: [String: Int] = [:]
        public var timeZone: TimeZone

        public var setCount: Int { sessions.reduce(0) { $0 + $1.sets.count } }
    }

    /// How the account logged a pair of dumbbells or kettlebells: the
    /// library counts one implement's weight, so a pair logged together is
    /// halved on import.
    public enum PairedWeights: String, Sendable, CaseIterable {
        case each, together
    }

    /// The plan for an account read through Hevy's API: its workouts as the
    /// CSV's rows (at their instants, keyed by Hevy's workout id), its
    /// routines, its own exercises and its body measurements.
    public static func plan(
        _ export: HevyAPI.Export, exercises: [ExerciseID: Exercise], routines: [RoutineID: Routine],
        history: [SessionRecords], deleted: Set<String>, timeZone: TimeZone, adapter: String,
        library: [ExerciseID: Exercise] = [:], pairs: PairedWeights = .each
    ) -> Plan {
        var plan = plan(
            HevyAPI.rows(export.workouts, timeZone: timeZone), exercises: exercises, history: history, deleted: deleted,
            timeZone: timeZone, adapter: adapter, library: library, templates: export.templates,
            hevyRoutines: export.routines, folders: export.folders, routines: routines, measurements: export.measurements,
            pairs: pairs)
        for id in export.missingTemplates.sorted() {
            plan.problems.append(Problem(line: 0, reason: "Hevy no longer serves exercise \(id); its sets came in under the workout's own name"))
        }
        return plan
    }

    /// - Parameters:
    ///   - exercises: the catalogue. A row with a Hevy template id resolves
    ///     to the exercise carrying that id (`codes`), else by name or alias
    ///     (case and spacing aside) to one that carries no other Hevy id; a
    ///     CSV row by name alone.
    ///   - history: the sessions History holds.
    ///   - deleted: sessions deleted from History, never imported again.
    ///   - timeZone: the zone the CSV's wall-clock times are in; the API's
    ///     instants keep their own.
    ///   - adapter: the session record's `adapter`.
    ///   - library: the exercise library this build carries; an exercise
    ///     the catalogue lacks is built from its exercise of that template
    ///     id or name, movement and conventions included, before any is made
    ///     from Hevy's template or guessed from the title.
    ///   - templates: Hevy's templates for the account's own exercises,
    ///     each added as an exercise whether a workout names it or not.
    ///   - hevyRoutines, folders: Hevy's routines and their folders.
    ///   - routines: the catalogue's routines, which a Hevy routine of the
    ///     same id is never written over.
    ///   - measurements: Hevy's body measurements; a session takes the
    ///     latest body mass on or before its day.
    ///   - pairs: how the account logged a pair of implements.
    public static func plan(
        _ file: HevyCSV.File, exercises: [ExerciseID: Exercise], history: [SessionRecords],
        deleted: Set<String>, timeZone: TimeZone, adapter: String,
        library: [ExerciseID: Exercise] = [:], templates: [String: HevyAPI.Template] = [:],
        hevyRoutines: [HevyAPI.Routine] = [], folders: [HevyAPI.Folder] = [],
        routines: [RoutineID: Routine] = [:], measurements: [HevyAPI.BodyMeasurement] = [],
        pairs: PairedWeights = .each
    ) -> Plan {
        var plan = Plan(problems: file.unreadable, timeZone: timeZone)
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone

        func index(_ catalogue: [ExerciseID: Exercise]) -> (names: [String: ExerciseID], hevy: [String: ExerciseID]) {
            var names: [String: ExerciseID] = [:]
            var hevy: [String: ExerciseID] = [:]
            for exercise in catalogue.values.sorted(by: { $0.id < $1.id }) {
                for name in [exercise.name] + exercise.aliases where names[normalized(name)] == nil {
                    names[normalized(name)] = exercise.id
                }
                for coding in exercise.codes where coding.system == hevySystem && hevy[coding.code] == nil {
                    hevy[coding.code] = exercise.id
                }
            }
            return (names, hevy)
        }
        func hevyCodes(_ exercise: Exercise) -> Set<String> { Set(exercise.codes.filter { $0.system == hevySystem }.map(\.code)) }
        /// A name match stands for a template only when the exercise names
        /// no other Hevy template: two templates are two exercises.
        func admits(_ exercise: Exercise, _ template: String?) -> Bool {
            guard let template else { return true }
            let codes = hevyCodes(exercise)
            return codes.isEmpty || codes.contains(template)
        }
        var (byName, byHevy) = index(exercises)
        let (libraryByName, libraryByHevy) = index(library)
        var known = exercises
        var taken = Set(exercises.keys)
        var newByTemplate: [String: ExerciseID] = [:]
        let rowsByExercise = Dictionary(grouping: file.rows, by: { normalized($0.exercise) })
        func resolve(_ title: String, template: String?) -> Exercise {
            let key = normalized(title)
            if let template, let id = byHevy[template] ?? newByTemplate[template], let exercise = known[id] {
                if exercises[id] != nil { plan.matched[title] = id }
                return exercise
            }
            if let id = byName[key], let exercise = known[id], admits(exercise, template) {
                if exercises[id] != nil { plan.matched[title] = id }
                if let template { byHevy[template] = id }
                return exercise
            }
            // The library's own exercise, or the record a renamed copy of it
            // still holds at its id.
            if let id = template.flatMap({ libraryByHevy[$0] }) ?? libraryByName[key], let entry = library[id], admits(entry, template) {
                if let exercise = known[id] {
                    if exercises[id] != nil { plan.matched[title] = id }
                    if let template { byHevy[template] = id }
                    return exercise
                }
                var copy = entry
                copy.isLocal = true
                return add(copy, key: key, template: template)
            }
            let exercise = newExercise(title, rows: rowsByExercise[key] ?? [], taken: taken, template: template.flatMap { templates[$0] })
            return add(exercise, key: key, template: template)
        }
        func add(_ exercise: Exercise, key: String, template: String?) -> Exercise {
            var exercise = exercise
            if let template, !hevyCodes(exercise).contains(template) {
                exercise.codes.append(Coding(system: hevySystem, code: template))
            }
            taken.insert(exercise.id)
            known[exercise.id] = exercise
            if byName[key] == nil { byName[key] = exercise.id }
            if let template { newByTemplate[template] = exercise.id }
            plan.exercises.append(exercise)
            return exercise
        }

        // Routines first: a session names the routine it was started from.
        // A routine's id is Hevy's (`hevy-<routine id>`), so two routines of
        // one name stay two, an import again finds its own, and an owner's
        // routine of the same name is never touched.
        var routineIDs: [String: RoutineID] = [:]
        let folderNames = Dictionary(folders.map { ($0.id, $0.title) }, uniquingKeysWith: { first, _ in first })
        var positions: [String: Int] = [:]
        for hevy in hevyRoutines {
            let id = RoutineID(rawValue: "hevy-\(hevy.id.lowercased())")
            routineIDs[hevy.id] = id
            if routines[id] != nil {
                plan.routinesHeld.append(hevy.title)
                continue
            }
            let folder = hevy.folderId.flatMap { folderNames[$0] }
            // From 1, in Hevy's order within the folder (`routine.position`).
            let position = positions[folder ?? "", default: 0] + 1
            positions[folder ?? ""] = position
            plan.routines.append(
                routine(hevy, id: id, folder: folder, position: position, pairs: pairs) {
                    resolve($0.title, template: $0.exerciseTemplateId)
                })
        }
        // The account's own exercises, whether a workout names them or not.
        for template in templates.values.sorted(by: { $0.id < $1.id }) where template.isCustom == true {
            _ = resolve(template.title, template: template.id)
        }

        // Body measurements, oldest first: what a session reads its body
        // mass from.
        let dayFormat = DateFormatter()
        dayFormat.locale = Locale(identifier: "en_US_POSIX")
        dayFormat.timeZone = timeZone
        dayFormat.dateFormat = "yyyy-MM-dd"
        var masses: [(day: Date, mass: Mass)] = []
        for entry in measurements {
            guard let day = dayFormat.date(from: entry.date) else {
                plan.problems.append(Problem(line: 0, reason: "body measurement \(entry.date): not a date"))
                continue
            }
            plan.measurements.append(measurementRecord(entry, day: day, adapter: adapter))
            if let kg = entry.weightKg.flatMap(HevyAPI.decimal), kg > 0 { masses.append((day, Mass(value: kg, unit: .kg))) }
        }
        masses.sort { $0.day < $1.day }

        let held = Set(history.map(\.id.rawValue))
        let spans = history.map { ($0.session.id, $0.session.at, $0.session.endsAt ?? $0.session.at) }
        var ids = Set<String>()
        for workout in workouts(file.rows) {
            let first = workout.rows[0]
            guard let start = first.startInstant ?? calendar.date(from: workout.start) else {
                plan.problems.append(Problem(line: first.line, reason: "the start time is not a time in \(timeZone.identifier)"))
                continue
            }
            let end = (first.endInstant ?? workout.end.flatMap(calendar.date(from:))).map { max($0, start) } ?? start
            var id = first.workoutID.map { SessionID("hevy-\($0.lowercased())") } ?? sessionID(workout.start)
            let base = id
            var suffix = 2
            while ids.contains(id.rawValue) {
                id = SessionID("\(base.rawValue)-\(suffix)")
                suffix += 1
            }
            ids.insert(id.rawValue)
            let title = workout.title.isEmpty ? "Hevy workout" : workout.title
            if held.contains(id.rawValue) {
                plan.skipped.append(Skipped(title: title, start: start, reason: .imported))
                continue
            }
            if deleted.contains(id.rawValue) {
                plan.skipped.append(Skipped(title: title, start: start, reason: .deleted))
                continue
            }
            // A session at the same time is this workout logged here too, or
            // imported by the other path (the CSV keys a workout by its start,
            // the API by Hevy's id). Two workouts the API names apart are two,
            // whenever they ran.
            let overlaps = spans.filter { $0.1 < end && start < $0.2 || $0.1 == start }
                .filter { !(first.workoutID != nil && isAPIImport($0.0)) }
            if let overlap = overlaps.first {
                let reason: Skip = overlap.0.rawValue.hasPrefix("hevy-") ? .imported : .overlaps(overlap.0)
                plan.skipped.append(Skipped(title: title, start: start, reason: reason))
                continue
            }
            var source: [SetID: HevyCSV.Row] = [:]
            let routine = first.routineID.flatMap { routineIDs[$0] }
            var records = session(
                workout, id: id, title: title, start: start, end: end, timeZone: timeZone, adapter: adapter,
                routine: routine, pairs: pairs, source: &source
            ) { row in
                resolve(row.exercise, template: row.templateID)
            }
            if records.session.bodyMass == nil, let mass = masses.last(where: { $0.day <= start })?.mass {
                records.session.bodyMass = mass
            }
            for set in records.sets {
                guard let exercise = known[set.exercise], exercise.measurement.loadMode == nil || exercise.measurement.loadMode == .bodyweight,
                    let row = source[set.id], (row.weight?.value ?? 0) > 0
                else { continue }
                plan.weightsNotRecorded[row.exercise, default: 0] += 1
            }
            plan.sessions.append(records)
        }
        plan.sessions.sort { $0.session.at < $1.session.at }
        return plan
    }

    static let hevySystem = "hevy"

    /// A session an API import wrote: `hevy-` and Hevy's workout id, not the
    /// CSV's `hevy-<yyyyMMdd>-<HHmm>`.
    static func isAPIImport(_ id: SessionID) -> Bool {
        guard id.rawValue.hasPrefix("hevy-") else { return false }
        let rest = id.rawValue.dropFirst(5)
        let parts = rest.split(separator: "-")
        let isStamp =
            parts.count >= 2 && parts[0].count == 8 && parts[1].count == 4 && parts[0].allSatisfy(\.isNumber)
            && parts[1].allSatisfy(\.isNumber)
        return !isStamp
    }

    /// A load as the library counts it: a pair logged together is halved
    /// onto one implement, digits kept.
    static func perImplement(_ mass: Mass?, exercise: Exercise, pairs: PairedWeights) -> Mass? {
        guard let mass, pairs == .together, exercise.resolvedLoadBasis == .perimplement, exercise.resolvedImplements > 1 else { return mass }
        return Mass(value: mass.value / Decimal(exercise.resolvedImplements), unit: mass.unit)
    }

    /// One Hevy body measurement entry as a `~/training/bodymeasurement`
    /// record at noon of its day, every value Hevy holds kept.
    static func measurementRecord(_ entry: HevyAPI.BodyMeasurement, day: Date, adapter: String) -> FlatRecord {
        var properties = JSONObject()
        properties["at"] = .string(Timestamp.format(day.addingTimeInterval(12 * 3600), precision: .seconds))
        func mass(_ key: String, _ value: Double?) {
            guard let value = value.flatMap(HevyAPI.decimal) else { return }
            properties[key] = .object(["value": .string(DecimalText.format(value)), "unit": .string("kg")])
        }
        func length(_ key: String, _ value: Double?) {
            guard let value = value.flatMap(HevyAPI.decimal) else { return }
            properties[key] = .object(["value": .string(DecimalText.format(value)), "unit": .string("cm")])
        }
        mass("bodyMass", entry.weightKg)
        mass("leanMass", entry.leanMassKg)
        if let fat = entry.fatPercent.flatMap(HevyAPI.decimal) { properties["bodyFat"] = .string(DecimalText.format(fat)) }
        for (key, value) in [
            ("neck", entry.neckCm), ("shoulders", entry.shoulderCm), ("chest", entry.chestCm),
            ("leftBicep", entry.leftBicepCm), ("rightBicep", entry.rightBicepCm),
            ("leftForearm", entry.leftForearmCm), ("rightForearm", entry.rightForearmCm),
            ("abdomen", entry.abdomen), ("waist", entry.waist), ("hips", entry.hips),
            ("leftThigh", entry.leftThigh), ("rightThigh", entry.rightThigh),
            ("leftCalf", entry.leftCalf), ("rightCalf", entry.rightCalf),
        ] {
            length(key, value)
        }
        properties["adapter"] = .string(adapter)
        let id = entry.id.map { "hevy-\($0)" } ?? "hevy-\(entry.date)"
        return FlatRecord(kind: WorkoutsKinds.v1.bodyMeasurement, id: id, properties: properties)
    }

    /// A Hevy routine as a workout routine: its exercises in order as slots,
    /// neighbouring ones with one superset number a group, each exercise's
    /// rest its set rest (none kept as none), and each set a planned set
    /// (`warmup` a warm-up, `failure` a set to failure, its rep range or its
    /// reps, each `dropset` one more drop of the set before it, the first
    /// drop's reps and step kept).
    static func routine(
        _ hevy: HevyAPI.Routine, id: RoutineID, folder: String?, position: Int, pairs: PairedWeights = .each,
        exercise resolve: (HevyAPI.Exercise) -> Exercise
    ) -> Routine {
        var slots: [RoutineSlot] = []
        var keys = Set<String>()
        let ordered = hevy.exercises.sorted { $0.index < $1.index }
        for item in ordered {
            let exercise = resolve(item)
            var key = exercise.id.rawValue
            var suffix = 2
            while keys.contains(key) {
                key = "\(exercise.id.rawValue)-\(suffix)"
                suffix += 1
            }
            keys.insert(key)
            let mode = exercise.measurement.loadMode
            var sets: [PlannedSet] = []
            var counters: [Role: Int] = [:]
            for set in item.sets.sorted(by: { $0.index < $1.index }) {
                let type = set.type.lowercased()
                let weight = perImplement(
                    set.weightKg.flatMap(HevyAPI.decimal).flatMap { $0 > 0 ? Mass(value: $0, unit: .kg) : nil },
                    exercise: exercise, pairs: pairs)
                if type == "dropset" || type == "drop_set", !sets.isEmpty {
                    let last = sets.count - 1
                    if sets[last].drops == nil {
                        sets[last].dropReps = set.reps ?? set.repRange?.start
                        if let from = sets[last].load, let to = weight, from.unit == to.unit, from.value > to.value {
                            sets[last].dropStep = Mass(value: from.value - to.value, unit: from.unit)
                        }
                    }
                    sets[last].structure = .drop
                    sets[last].drops = (sets[last].drops ?? 0) + 1
                    continue
                }
                let role: Role = type == "warmup" ? .warmup : .working
                let number = (counters[role] ?? 0) + 1
                counters[role] = number
                var planned = PlannedSet(key: "\(role == .warmup ? "w" : "s")\(number)", role: role)
                planned.load = mode == .external || mode == .bodyweightplus ? weight : nil
                planned.assistance = mode == .assisted ? weight : nil
                planned.repsMin = set.reps ?? set.repRange?.start ?? set.repRange?.end
                planned.repsMax = set.reps ?? set.repRange?.end ?? set.repRange?.start
                if let seconds = set.durationSeconds.map({ Int($0.rounded()) }), seconds > 0 {
                    planned.durationMin = seconds
                    planned.durationMax = seconds
                }
                planned.distance = set.distanceMeters.flatMap(HevyAPI.decimal).map { Length(metres: $0) }
                if type == "failure" { planned.termination = .failure }
                sets.append(planned)
            }
            slots.append(
                RoutineSlot(
                    key: key, exercise: exercise.id, setRest: item.restSeconds.map { max(0, $0) },
                    notes: item.notes?.nilIfEmpty, sets: sets))
        }
        var groups: [RoutineGroup] = []
        var cursor = 0
        while cursor < ordered.count {
            guard let number = ordered[cursor].supersetId else {
                cursor += 1
                continue
            }
            var members = [cursor]
            while let next = members.last.map({ $0 + 1 }), next < ordered.count, ordered[next].supersetId == number {
                members.append(next)
            }
            if members.count >= 2 {
                let key = "superset-\(groups.count + 1)"
                for member in members { slots[member].group = key }
                let kind: GroupKind = members.count == 2 ? .superset : members.count == 3 ? .triset : .giantset
                groups.append(RoutineGroup(key: key, groupKind: kind))
            }
            cursor = members.last! + 1
        }
        return Routine(
            id: id, name: hevy.title, folder: folder, position: position, notes: hevy.notes?.nilIfEmpty, groups: groups,
            slots: slots)
    }

    // MARK: Workouts

    struct Workout {
        var title: String
        var start: DateComponents
        var end: DateComponents?
        var rows: [HevyCSV.Row]
    }

    /// The rows by workout, in file order: by Hevy's workout id where the
    /// API gave one, else by the title, start and end the CSV keys a workout by.
    static func workouts(_ rows: [HevyCSV.Row]) -> [Workout] {
        var out: [Workout] = []
        var index: [String: Int] = [:]
        for row in rows {
            let key = row.workoutID.map { "id\u{1F}\($0)" } ?? "\(row.title)\u{1F}\(stamp(row.start))\u{1F}\(row.end.map(stamp) ?? "")"
            if let at = index[key] {
                out[at].rows.append(row)
            } else {
                index[key] = out.count
                out.append(Workout(title: row.title, start: row.start, end: row.end, rows: [row]))
            }
        }
        return out
    }

    /// `hevy-20261030-1343`: the start on the wall, which the export keys a
    /// workout by and which stays the same in any zone, so importing the
    /// same file again finds the sessions it wrote.
    static func sessionID(_ start: DateComponents) -> SessionID {
        SessionID("hevy-\(stamp(start))")
    }

    private static func stamp(_ c: DateComponents) -> String {
        func pad(_ value: Int?, _ width: Int) -> String {
            let text = String(value ?? 0)
            return String(repeating: "0", count: max(0, width - text.count)) + text
        }
        return "\(pad(c.year, 4))\(pad(c.month, 2))\(pad(c.day, 2))-\(pad(c.hour, 2))\(pad(c.minute, 2))"
    }

    /// One workout as a finished session and its sets. Every set sits at the
    /// session's start (`timeBasis: session`): Hevy records no set times.
    static func session(
        _ workout: Workout, id: SessionID, title: String, start: Date, end: Date, timeZone: TimeZone, adapter: String,
        routine: RoutineID? = nil, pairs: PairedWeights = .each, source: inout [SetID: HevyCSV.Row],
        exercise resolve: (HevyCSV.Row) -> Exercise
    ) -> SessionRecords {
        // An exercise's rows are one slot while they run on, the set index
        // rising; the same title again later, or an index starting over, is
        // a slot of its own.
        var runs: [[HevyCSV.Row]] = []
        // A workout's placeholder with no exercise carries the session alone.
        for row in workout.rows where !(row.placeholder && row.exercise.isEmpty) {
            if let last = runs.last?.last, last.exercise == row.exercise, last.superset == row.superset,
                (row.setIndex ?? Int.max) > (last.setIndex ?? Int.min)
            {
                runs[runs.count - 1].append(row)
            } else {
                runs.append([row])
            }
        }

        var slots: [SessionSlot] = []
        var sets: [PerformedSet] = []
        var keys = Set<String>()
        for (index, run) in runs.enumerated() {
            let exercise = resolve(run[0])
            var key = exercise.id.rawValue
            var suffix = 2
            while keys.contains(key) {
                key = "\(exercise.id.rawValue)-\(suffix)"
                suffix += 1
            }
            keys.insert(key)
            slots.append(SessionSlot(key: key, exercise: exercise.id, notes: run[0].exerciseNotes.nilIfEmpty))
            let performed = run.filter { !$0.placeholder }
            let made = setRecords(performed, slot: key, slotIndex: index, session: id, at: start, exercise: exercise, pairs: pairs)
            for (set, row) in zip(made, performed) { source[set.id] = row }
            sets += made
        }

        // Neighbouring slots with one superset number are a group; a lone
        // slot keeps its number to itself, as the engine drops a group of one.
        var groups: [SessionGroup] = []
        var position = 0
        while position < runs.count {
            guard let number = runs[position][0].superset else {
                position += 1
                continue
            }
            var members = [position]
            while let next = members.last.map({ $0 + 1 }), next < runs.count, runs[next][0].superset == number {
                members.append(next)
            }
            if members.count >= 2 {
                let key = "superset-\(groups.count + 1)"
                for member in members { slots[member].group = key }
                let kind: GroupKind = members.count == 2 ? .superset : members.count == 3 ? .triset : .giantset
                groups.append(SessionGroup(key: key, groupKind: kind))
            }
            position = members.last! + 1
        }

        let offset = { (date: Date) in TrainingSession.utcOffset(in: timeZone, at: date) }
        let description = workout.rows.first?.description.nilIfEmpty
        let session = TrainingSession(
            id: id,
            at: start,
            endsAt: end,
            name: TrainingSession.name(routine: title, start: start, timeZone: timeZone),
            status: .finished,
            routine: routine,
            sport: .strength,
            slots: slots,
            groups: groups,
            utcOffsetStart: offset(start),
            utcOffsetEnd: offset(end),
            timezone: timeZone.identifier,
            notes: description,
            revision: 1,
            adapter: adapter
        )
        return SessionRecords(session: session, sets: sets)
    }

    /// One slot's rows as set records. `warmup` is a warm-up, `failure` a
    /// working set that reached failure, and each `dropset` row a drop of
    /// the set before it, which becomes the chain's head.
    static func setRecords(
        _ run: [HevyCSV.Row], slot: String, slotIndex: Int, session: SessionID, at: Date,
        exercise: Exercise, pairs: PairedWeights = .each
    ) -> [PerformedSet] {
        var out: [PerformedSet] = []
        var counters: [Role: Int] = [:]
        var head: Int?
        for (position, row) in run.enumerated() {
            let drop = row.setType == "dropset" || row.setType == "drop_set" || row.setType == "drop"
            var set = setRecord(
                row, id: SetID("\(session.rawValue)-\(slotIndex)-\(position)"), slot: slot, order: position + 1,
                session: session, at: at, exercise: exercise, pairs: pairs)
            if drop, let headIndex = head {
                let chain = out.count(where: { $0.continues == out[headIndex].id }) + 2
                out[headIndex].structure = .drop
                out[headIndex].chainPosition = 1
                set.role = out[headIndex].role
                set.structure = .drop
                set.continues = out[headIndex].id
                set.chainPosition = chain
                set.ordinal = out[headIndex].ordinal
            } else {
                let ordinal = (counters[set.role] ?? 0) + 1
                counters[set.role] = ordinal
                set.ordinal = ordinal
                head = out.count
            }
            out.append(set)
        }
        return out
    }

    private static func setRecord(
        _ row: HevyCSV.Row, id: SetID, slot: String, order: Int, session: SessionID, at: Date,
        exercise: Exercise, pairs: PairedWeights
    ) -> PerformedSet {
        let mode = exercise.measurement.loadMode
        let laterality = exercise.resolvedLaterality
        let weight = perImplement(row.weight, exercise: exercise, pairs: pairs)
        return PerformedSet(
            id: id,
            at: at,
            session: session,
            exercise: exercise.id,
            slot: slot,
            order: order,
            role: row.setType == "warmup" ? .warmup : .working,
            structure: .straight,
            loadMode: mode,
            load: mode == .external || mode == .bodyweightplus ? weight : nil,
            assistance: mode == .assisted ? weight : nil,
            reps: row.reps,
            duration: row.duration,
            distance: row.distance,
            loadBasis: mode == nil ? nil : exercise.resolvedLoadBasis,
            implements: mode == nil ? nil : exercise.resolvedImplements,
            bodyShare: mode == nil || mode == .external ? nil : exercise.resolvedBodyShare,
            laterality: laterality,
            // Hevy counts a unilateral exercise's reps on both sides together.
            repsBasis: row.reps == nil ? nil : .total,
            rpe: row.rpe,
            rpeMethod: row.rpe == nil ? nil : .imported,
            rpeScale: row.rpe == nil ? nil : .hevy,
            reachedFailure: row.setType == "failure" ? true : nil,
            completionMethod: .imported,
            notes: row.customMetric.map { customMetricNote($0, exercise: exercise) },
            sourceType: row.setType.nilIfEmpty,
            timeBasis: .session,
            revision: 1
        )
    }

    /// A stair machine's floors or steps, which no set field holds, kept in
    /// the set's notes in Hevy's words.
    static func customMetricNote(_ value: Decimal, exercise: Exercise) -> String {
        let name = exercise.name.lowercased()
        let unit = name.contains("floor") ? "floors" : name.contains("step") ? "steps" : "custom metric"
        return "\(DecimalText.format(value)) \(unit) (from Hevy)"
    }

    // MARK: Exercises

    static func normalized(_ name: String) -> String {
        name.folding(options: [.caseInsensitive, .diacriticInsensitive], locale: Locale(identifier: "en_US_POSIX"))
            .split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }

    /// An exercise the catalogue lacks, named as Hevy names it: what its sets
    /// record decides the measurement, and the equipment in its title's
    /// brackets (`Bench Press (Barbell)`) the equipment.
    static func newExercise(_ title: String, rows: [HevyCSV.Row], taken: Set<ExerciseID>, template: HevyAPI.Template? = nil) -> Exercise {
        if let template { return newExercise(template, taken: taken) }
        let qualifier = bracketed(title)?.lowercased() ?? ""
        let weight = rows.contains { ($0.weight?.value ?? 0) > 0 }
        let reps = rows.contains { $0.reps != nil }
        let duration = rows.contains { $0.duration != nil }
        let distance = rows.contains { $0.distance != nil }
        let measurement: MeasurementType =
            switch (weight, reps, duration, distance) {
            case (_, _, true, true): .distanceduration
            case (true, _, _, true): .shortdistanceweight
            case (true, false, true, false): .weightduration
            case (false, false, true, false): .duration
            case (_, true, _, _) where qualifier.contains("assisted"): .bodyweightassistedreps
            case (_, true, _, _) where qualifier.contains("weighted"): .weightedbodyweightreps
            case (true, _, _, _): .weightreps
            default: qualifier.contains("bodyweight") ? .bodyweightreps : .repsonly
            }
        return Exercise(
            id: ExerciseID.slug(for: title, taken: taken), name: title, measurement: measurement,
            equipment: equipment(qualifier), isLocal: true)
    }

    /// An exercise from the account's own Hevy template: its type decides
    /// the measurement, its equipment (and the title's brackets, since Hevy
    /// files a cable under machine) the equipment, its muscle groups the two
    /// group lists; its id goes in `codes`.
    static func newExercise(_ template: HevyAPI.Template, taken: Set<ExerciseID>) -> Exercise {
        let measurement: MeasurementType =
            switch template.type {
            case "weight_reps": .weightreps
            case "bodyweight_reps": .bodyweightreps
            case "bodyweight_weighted": .weightedbodyweightreps
            case "bodyweight_assisted", "bodyweight_assisted_reps": .bodyweightassistedreps
            case "duration", "floors_duration", "steps_duration": .duration
            case "weight_duration": .weightduration
            case "distance_duration": .distanceduration
            case "short_distance_weight": .shortdistanceweight
            default: .repsonly
            }
        let qualifier = bracketed(template.title)?.lowercased() ?? ""
        let equipment: Equipment? =
            switch template.equipment {
            case "machine" where qualifier.contains("cable") || template.title.lowercased().contains("cable"): .cable
            case "resistance_band": .resistanceband
            case let value?: Equipment(rawValue: value) ?? equipment(qualifier)
            case nil: equipment(qualifier)
            }
        func group(_ name: String) -> Muscle? { Muscle(rawValue: name.replacingOccurrences(of: "_", with: "")) }
        let primary = template.primaryMuscleGroup.flatMap(group).map { [$0] } ?? []
        let secondary = (template.secondaryMuscleGroups ?? []).compactMap(group).filter { !primary.contains($0) }
        return Exercise(
            id: ExerciseID.slug(for: template.title, taken: taken), name: template.title, measurement: measurement,
            equipment: equipment, primaryMuscles: primary, secondaryMuscles: secondary,
            codes: [Coding(system: hevySystem, code: template.id)], isLocal: true)
    }

    private static func bracketed(_ title: String) -> String? {
        guard title.hasSuffix(")"), let open = title.lastIndex(of: "(") else { return nil }
        return String(title[title.index(after: open) ..< title.index(before: title.endIndex)])
    }

    private static func equipment(_ qualifier: String) -> Equipment? {
        switch qualifier {
        case "barbell", "ez bar", "trap bar": .barbell
        case "dumbbell": .dumbbell
        case "kettlebell": .kettlebell
        case "machine", "smith machine": .machine
        case "plate loaded", "plate-loaded": .plateloaded
        case "cable": .cable
        case "plate": .plate
        case "band", "resistance band": .resistanceband
        case "suspension", "trx": .suspension
        case "bodyweight", "weighted", "assisted": Equipment.none
        default: nil
        }
    }
}

private extension String {
    var nilIfEmpty: String? { isEmpty ? nil : self }
}
