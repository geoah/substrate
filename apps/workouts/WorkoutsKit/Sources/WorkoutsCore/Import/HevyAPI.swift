import Foundation

/// What Hevy's public API answers (api.hevyapp.com/v1, an account's API key
/// in the `api-key` header): workouts, routines, routine folders and
/// exercise templates, decoded with snake case converted. Its workouts carry
/// what the CSV export leaves out: the exercise template id, which names an
/// exercise exactly, and the routine a workout was started from.
public enum HevyAPI {
    public struct Workout: Codable, Hashable, Sendable {
        public var id: String
        public var title: String
        public var routineId: String?
        public var description: String?
        public var startTime: String
        public var endTime: String
        public var exercises: [Exercise]
    }

    public struct Exercise: Codable, Hashable, Sendable {
        public var index: Int
        public var title: String
        public var notes: String?
        public var exerciseTemplateId: String
        public var supersetId: Int?
        /// A routine's rest between this exercise's sets.
        public var restSeconds: Int?
        public var sets: [Set]
    }

    public struct Set: Codable, Hashable, Sendable {
        public var index: Int
        public var type: String
        public var weightKg: Double?
        public var reps: Int?
        public var distanceMeters: Double?
        public var durationSeconds: Double?
        public var rpe: Double?
        /// A stair machine's floors or steps.
        public var customMetric: Double?
        /// A routine set's rep range, where `reps` is absent.
        public var repRange: RepRange?
    }

    public struct RepRange: Codable, Hashable, Sendable {
        public var start: Int?
        public var end: Int?
    }

    /// One day's body measurements (`/v1/body_measurements`).
    public struct BodyMeasurement: Codable, Hashable, Sendable {
        public var id: Int?
        public var date: String
        public var weightKg: Double?
        public var leanMassKg: Double?
        public var fatPercent: Double?
        public var neckCm: Double?
        public var shoulderCm: Double?
        public var chestCm: Double?
        public var leftBicepCm: Double?
        public var rightBicepCm: Double?
        public var leftForearmCm: Double?
        public var rightForearmCm: Double?
        public var abdomen: Double?
        public var waist: Double?
        public var hips: Double?
        public var leftThigh: Double?
        public var rightThigh: Double?
        public var leftCalf: Double?
        public var rightCalf: Double?
    }

    public struct Routine: Codable, Hashable, Sendable {
        public var id: String
        public var title: String
        public var folderId: Int?
        public var notes: String?
        public var exercises: [Exercise]
    }

    public struct Folder: Codable, Hashable, Sendable {
        public var id: Int
        public var index: Int
        public var title: String
    }

    public struct Template: Codable, Hashable, Sendable {
        public var id: String
        public var title: String
        public var type: String
        public var primaryMuscleGroup: String?
        public var secondaryMuscleGroups: [String]?
        public var equipment: String?
        public var isCustom: Bool?
    }

    /// Everything one import reads from an account.
    public struct Export: Sendable {
        public var workouts: [Workout]
        public var routines: [Routine]
        public var folders: [Folder]
        /// The account's own exercises, every one, and any other template
        /// the workouts and routines name that the exercise library does not
        /// map, by id.
        public var templates: [String: Template]
        public var measurements: [BodyMeasurement]
        /// Template ids Hevy no longer serves: their exercises come from the
        /// workout's own title and sets.
        public var missingTemplates: [String]

        public init(
            workouts: [Workout] = [], routines: [Routine] = [], folders: [Folder] = [], templates: [String: Template] = [:],
            measurements: [BodyMeasurement] = [], missingTemplates: [String] = []
        ) {
            self.workouts = workouts
            self.routines = routines
            self.folders = folders
            self.templates = templates
            self.measurements = measurements
            self.missingTemplates = missingTemplates
        }
    }

    public static var decoder: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return decoder
    }

    /// The workouts as the CSV export's rows, their wall clock in
    /// `timeZone` for the CSV's session ids and names, and each row keeping
    /// its workout id, instants, template id and routine id. A workout with
    /// no exercise, and an exercise with no set, keep a placeholder row, so
    /// the session and the exercise's notes come through.
    public static func rows(_ workouts: [Workout], timeZone: TimeZone) -> HevyCSV.File {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        func wall(_ date: Date) -> DateComponents {
            calendar.dateComponents([.year, .month, .day, .hour, .minute, .second], from: date)
        }
        var rows: [HevyCSV.Row] = []
        var problems: [HevyImport.Problem] = []
        for workout in workouts {
            guard let startInstant = instant(workout.startTime) else {
                problems.append(HevyImport.Problem(line: 0, reason: "workout \(workout.title): the start time \(workout.startTime) is not a date"))
                continue
            }
            let endInstant = instant(workout.endTime)
            func row(_ exercise: Exercise?, _ set: Set?) -> HevyCSV.Row {
                HevyCSV.Row(
                    line: 0, title: workout.title, start: wall(startInstant), end: endInstant.map(wall), description: workout.description ?? "",
                    exercise: exercise?.title ?? "", superset: exercise?.supersetId.map(String.init), exerciseNotes: exercise?.notes ?? "",
                    setIndex: set?.index, setType: set?.type.lowercased() ?? "",
                    weight: set?.weightKg.flatMap(decimal).map { Mass(value: $0, unit: .kg) },
                    reps: set?.reps, distance: set?.distanceMeters.flatMap(decimal).map { Length(metres: $0) },
                    duration: set?.durationSeconds.map { Int($0.rounded()) },
                    rpe: set?.rpe.flatMap(decimal).flatMap { $0 >= 0 && $0 <= 10 ? $0 : nil },
                    templateID: exercise?.exerciseTemplateId, routineID: workout.routineId, workoutID: workout.id,
                    startInstant: startInstant, endInstant: endInstant, customMetric: set?.customMetric.flatMap(decimal),
                    placeholder: set == nil
                )
            }
            if workout.exercises.isEmpty { rows.append(row(nil, nil)) }
            for exercise in workout.exercises.sorted(by: { $0.index < $1.index }) {
                if exercise.sets.isEmpty { rows.append(row(exercise, nil)) }
                for set in exercise.sets.sorted(by: { $0.index < $1.index }) {
                    rows.append(row(exercise, set))
                }
            }
        }
        return HevyCSV.File(rows: rows, unreadable: problems)
    }

    /// A JSON number as the digits it was written with: 3.757, not
    /// 3.7570000000000001.
    static func decimal(_ value: Double) -> Decimal? {
        guard value.isFinite else { return nil }
        return Decimal(string: String(value), locale: Locale(identifier: "en_US_POSIX"))
    }

    static func instant(_ text: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        if let date = formatter.date(from: text) { return date }
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.date(from: text)
    }
}
