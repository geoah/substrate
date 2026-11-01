import Foundation

/// One movement the owner trains (`exercise.yaml`; no traits). Fields follow
/// the kind file's order. The suggestion settings are optional; each says what
/// its absence reads as, and the `resolved…` accessors apply that.
public struct Exercise: Codable, Hashable, Sendable, Identifiable {
    public var id: ExerciseID
    public var name: String
    public var aliases: [String]
    public var measurement: MeasurementType
    public var equipment: Equipment?
    /// Absent reads as bilateral.
    public var laterality: Laterality?
    /// Absent reads as total.
    public var loadBasis: LoadBasis?
    /// Absent reads as 1.
    public var implements: Int?
    public var primaryMuscles: [Muscle]
    public var secondaryMuscles: [Muscle]
    public var restClass: RestClass?
    public var variationOf: ExerciseID?
    public var codes: [Coding]
    public var loadBase: Mass?
    /// Its unit is the exercise's unit.
    public var loadStep: Mass?
    public var loadMax: Mass?
    /// 0...1, absent reads as 1.
    public var bodyShare: Decimal?
    public var defaultRepsMin: Int?
    public var defaultRepsMax: Int?
    public var defaultRPE: Decimal?
    public var repGain: Int?
    /// 0...100, absent reads as 20.
    public var dropPercent: Decimal?
    /// Absent reads as 5.
    public var timeStep: Seconds?
    /// Absent reads as repdrop.
    public var myoStopRule: MyoStopRule?
    /// Absent reads as 5.
    public var myoMaxMiniSets: Int?
    /// Unknown properties, kept for round trips (§3.4 rule 7).
    public var extra: [String: JSONValue]
    /// Added on this phone; not a property.
    public var isLocal: Bool

    public init(
        id: ExerciseID,
        name: String,
        aliases: [String] = [],
        measurement: MeasurementType,
        equipment: Equipment? = nil,
        laterality: Laterality? = nil,
        loadBasis: LoadBasis? = nil,
        implements: Int? = nil,
        primaryMuscles: [Muscle] = [],
        secondaryMuscles: [Muscle] = [],
        restClass: RestClass? = nil,
        variationOf: ExerciseID? = nil,
        codes: [Coding] = [],
        loadBase: Mass? = nil,
        loadStep: Mass? = nil,
        loadMax: Mass? = nil,
        bodyShare: Decimal? = nil,
        defaultRepsMin: Int? = nil,
        defaultRepsMax: Int? = nil,
        defaultRPE: Decimal? = nil,
        repGain: Int? = nil,
        dropPercent: Decimal? = nil,
        timeStep: Seconds? = nil,
        myoStopRule: MyoStopRule? = nil,
        myoMaxMiniSets: Int? = nil,
        extra: [String: JSONValue] = [:],
        isLocal: Bool = false
    ) {
        self.id = id
        self.name = name
        self.aliases = aliases
        self.measurement = measurement
        self.equipment = equipment
        self.laterality = laterality
        self.loadBasis = loadBasis
        self.implements = implements
        self.primaryMuscles = primaryMuscles
        self.secondaryMuscles = secondaryMuscles
        self.restClass = restClass
        self.variationOf = variationOf
        self.codes = codes
        self.loadBase = loadBase
        self.loadStep = loadStep
        self.loadMax = loadMax
        self.bodyShare = bodyShare
        self.defaultRepsMin = defaultRepsMin
        self.defaultRepsMax = defaultRepsMax
        self.defaultRPE = defaultRPE
        self.repGain = repGain
        self.dropPercent = dropPercent
        self.timeStep = timeStep
        self.myoStopRule = myoStopRule
        self.myoMaxMiniSets = myoMaxMiniSets
        self.extra = extra
        self.isLocal = isLocal
    }
}

public extension Exercise {
    var resolvedLaterality: Laterality { laterality ?? .bilateral }
    var resolvedLoadBasis: LoadBasis { loadBasis ?? .total }
    var resolvedImplements: Int { implements ?? 1 }
    var resolvedBodyShare: Decimal { bodyShare ?? 1 }
    var resolvedDropPercent: Decimal { dropPercent ?? 20 }
    var resolvedTimeStep: Seconds { timeStep ?? 5 }
    var resolvedMyoStopRule: MyoStopRule { myoStopRule ?? .repdrop }
    var resolvedMyoMaxMiniSets: Int { myoMaxMiniSets ?? 5 }

    /// The exercise's unit: its `loadStep`'s, else the owner's default.
    func unit(default defaultUnit: MassUnit) -> MassUnit { loadStep?.unit ?? defaultUnit }
}

/// A code for the exercise in another system (`codes[]`), as
/// `observations/metric.codes` spells it.
public struct Coding: Codable, Hashable, Sendable {
    public var system: String
    public var code: String
    public var version: String?
    public var display: String?
    /// Unknown fields, kept for round trips (§3.4 rule 7).
    @UnknownFields public var extra: [String: JSONValue]

    public init(system: String, code: String, version: String? = nil, display: String? = nil, extra: [String: JSONValue] = [:]) {
        self.system = system
        self.code = code
        self.version = version
        self.display = display
        self.extra = extra
    }
}

/// Which numbers a set records (the kind's `measurement`). Not `Measurement`:
/// Foundation's generic `Measurement` would make that name ambiguous in every
/// file that imports both modules.
public enum MeasurementType: String, Codable, Sendable, CaseIterable {
    case weightreps, repsonly, bodyweightreps, weightedbodyweightreps,
         bodyweightassistedreps, duration, weightduration, distanceduration, shortdistanceweight

    /// How a set's load reads (§3.5); nil when the exercise moves no load.
    public var loadMode: LoadMode? {
        switch self {
        case .weightreps, .weightduration, .shortdistanceweight: .external
        case .bodyweightreps: .bodyweight
        case .weightedbodyweightreps: .bodyweightplus
        case .bodyweightassistedreps: .assisted
        case .repsonly, .duration, .distanceduration: nil
        }
    }
}

public enum Equipment: String, Codable, Sendable, CaseIterable {
    case none, barbell, dumbbell, kettlebell, machine, plateloaded, cable,
         plate, resistanceband, suspension, other
}

public enum Laterality: String, Codable, Sendable, CaseIterable {
    case bilateral, unilateral, alternating
}

public enum LoadBasis: String, Codable, Sendable, CaseIterable {
    case total, perimplement, perside
}

public enum Muscle: String, Codable, Sendable, CaseIterable {
    case abdominals, abductors, adductors, biceps, calves, cardio, chest,
         forearms, fullbody, glutes, hamstrings, lats, lowerback, neck, quadriceps, shoulders, traps, triceps,
         upperback, other
}

public enum RestClass: String, Codable, Sendable, CaseIterable {
    case heavycompound, compound, isolation, highrep, timed, carry, power
}

public enum MyoStopRule: String, Codable, Sendable, CaseIterable {
    case repdrop, belowtarget
}
