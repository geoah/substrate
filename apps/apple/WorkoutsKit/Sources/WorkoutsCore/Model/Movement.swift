import Foundation

/// What a person searches for, such as Chest Press (`movement.yaml`; no
/// traits), and the muscles it works. Its exercises are the equipment
/// versions, each naming it (`exercise.movement`) and keeping its own
/// history, since their loads never compare.
public struct Movement: Codable, Hashable, Sendable, Identifiable {
    public var id: MovementID
    public var name: String
    public var aliases: [String]
    public var force: Force?
    public var mechanic: Mechanic?
    /// The muscles of every exercise of it that names none of its own.
    public var muscles: [MuscleShare]
    public var primaryMuscles: [Muscle]
    public var secondaryMuscles: [Muscle]
    public var codes: [Coding]
    /// Unknown properties, kept for round trips (§3.4 rule 7).
    public var extra: [String: JSONValue]

    public init(
        id: MovementID,
        name: String,
        aliases: [String] = [],
        force: Force? = nil,
        mechanic: Mechanic? = nil,
        muscles: [MuscleShare] = [],
        primaryMuscles: [Muscle] = [],
        secondaryMuscles: [Muscle] = [],
        codes: [Coding] = [],
        extra: [String: JSONValue] = [:]
    ) {
        self.id = id
        self.name = name
        self.aliases = aliases
        self.force = force
        self.mechanic = mechanic
        self.muscles = muscles
        self.primaryMuscles = primaryMuscles
        self.secondaryMuscles = secondaryMuscles
        self.codes = codes
        self.extra = extra
    }
}

public enum Force: String, Codable, Sendable, CaseIterable {
    case push, pull, `static`
}

public enum Mechanic: String, Codable, Sendable, CaseIterable {
    case compound, isolation
}
