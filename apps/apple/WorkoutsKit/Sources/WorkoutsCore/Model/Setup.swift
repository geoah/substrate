import Foundation

/// One station an exercise is done on, such as the hotel gym's chest press
/// (`setup.yaml`; no traits). A set names it (`set.setup`), and PREVIOUS and
/// suggestions keep each setup's history apart. Its load grid overrides the
/// exercise's field by field. V1 writes none.
public struct Setup: Codable, Hashable, Sendable, Identifiable {
    public var id: SetupID
    public var name: String
    public var exercise: ExerciseID
    public var gym: String?
    /// Absent means the exercise's.
    public var loadBase: Mass?
    /// Its unit is the setup's unit.
    public var loadStep: Mass?
    public var loadMax: Mass?
    public var notes: String?
    /// Unknown properties, kept for round trips (§3.4 rule 7).
    public var extra: [String: JSONValue]

    public init(
        id: SetupID,
        name: String,
        exercise: ExerciseID,
        gym: String? = nil,
        loadBase: Mass? = nil,
        loadStep: Mass? = nil,
        loadMax: Mass? = nil,
        notes: String? = nil,
        extra: [String: JSONValue] = [:]
    ) {
        self.id = id
        self.name = name
        self.exercise = exercise
        self.gym = gym
        self.loadBase = loadBase
        self.loadStep = loadStep
        self.loadMax = loadMax
        self.notes = notes
        self.extra = extra
    }
}

public extension Exercise {
    /// The exercise as done on `setup`: the setup's `loadBase`, `loadStep`
    /// and `loadMax` where it names them, the exercise's elsewhere.
    func on(_ setup: Setup?) -> Exercise {
        guard let setup, setup.exercise == id else { return self }
        var exercise = self
        exercise.loadBase = setup.loadBase ?? loadBase
        exercise.loadStep = setup.loadStep ?? loadStep
        exercise.loadMax = setup.loadMax ?? loadMax
        return exercise
    }
}
