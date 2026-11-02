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
        /// The templates the workouts and routines name that the exercise
        /// library does not map, by id: the account's own custom exercises.
        public var templates: [String: Template]

        public init(workouts: [Workout] = [], routines: [Routine] = [], folders: [Folder] = [], templates: [String: Template] = [:]) {
            self.workouts = workouts
            self.routines = routines
            self.folders = folders
            self.templates = templates
        }
    }

    public static var decoder: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return decoder
    }

    /// The workouts as the CSV export's rows, their times on the wall in
    /// `timeZone`, so a workout imported from either lands at the same
    /// session id. Each row keeps its template id and its routine id.
    public static func rows(_ workouts: [Workout], timeZone: TimeZone) -> HevyCSV.File {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        func wall(_ text: String) -> DateComponents? {
            guard let date = instant(text) else { return nil }
            return calendar.dateComponents([.year, .month, .day, .hour, .minute, .second], from: date)
        }
        var rows: [HevyCSV.Row] = []
        var problems: [HevyImport.Problem] = []
        for workout in workouts {
            guard let start = wall(workout.startTime) else {
                problems.append(HevyImport.Problem(line: 0, reason: "workout \(workout.title): the start time \(workout.startTime) is not a date"))
                continue
            }
            let end = wall(workout.endTime)
            for exercise in workout.exercises.sorted(by: { $0.index < $1.index }) {
                for set in exercise.sets.sorted(by: { $0.index < $1.index }) {
                    rows.append(HevyCSV.Row(
                        line: 0, title: workout.title, start: start, end: end, description: workout.description ?? "",
                        exercise: exercise.title, superset: exercise.supersetId.map(String.init), exerciseNotes: exercise.notes ?? "",
                        setIndex: set.index, setType: set.type.lowercased(),
                        weight: set.weightKg.flatMap(decimal).map { Mass(value: $0, unit: .kg) },
                        reps: set.reps, distance: set.distanceMeters.flatMap(decimal).map { Length(metres: $0) },
                        duration: set.durationSeconds.map { Int($0.rounded()) },
                        rpe: set.rpe.flatMap(decimal).flatMap { $0 >= 0 && $0 <= 10 ? $0 : nil },
                        templateID: exercise.exerciseTemplateId, routineID: workout.routineId
                    ))
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
