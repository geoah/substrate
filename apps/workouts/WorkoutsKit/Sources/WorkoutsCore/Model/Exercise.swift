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
    /// Each muscle and its share of a set; the count's source where present.
    @AbsentEmpty public var muscles: [MuscleShare]
    public var restClass: RestClass?
    public var variationOf: ExerciseID?
    /// The movement it is the equipment version of.
    public var movement: MovementID?
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
    /// Its movement's muscles, which it counts from when it names none of
    /// its own (`Catalog.resolveMovements`); not a property.
    @AbsentEmpty public var inheritedMuscles: [MuscleShare]

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
        muscles: [MuscleShare] = [],
        restClass: RestClass? = nil,
        variationOf: ExerciseID? = nil,
        movement: MovementID? = nil,
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
        self.muscles = muscles
        self.restClass = restClass
        self.variationOf = variationOf
        self.movement = movement
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

    /// Seeded from the exercise library (`ExerciseLibrary`): it carries the
    /// library's code.
    var isFromLibrary: Bool { codes.contains { $0.system == ExerciseLibrary.system } }

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

/// One muscle, finer than the twenty groups of `Muscle` (`muscles[].muscle`):
/// the pectoralis' two heads, the three delts, the triceps' long head apart
/// from the others. Each belongs to one group, which is how a reader that
/// knows only the groups counts it.
public enum MuscleDetail: String, Codable, Sendable, CaseIterable {
    case pecclavicular, pecsternal, serratus, deltfront, deltside, deltrear,
         rotatorcuff, lats, trapsupper, trapsmiddle, trapslower, rhomboids,
         erectors, bicepsbrachii, brachialis, brachioradialis, forearmflexors, forearmextensors,
         tricepslong, tricepslateral, rectusabdominis, obliques, transverseabdominis, iliopsoas,
         glutemax, glutemed, adductors, rectusfemoris, vasti, hamstrings,
         gastrocnemius, soleus, tibialis, neck

    /// The group it rolls up to, in Hevy's twenty.
    public var group: Muscle {
        switch self {
        case .pecclavicular: .chest
        case .pecsternal: .chest
        case .serratus: .chest
        case .deltfront: .shoulders
        case .deltside: .shoulders
        case .deltrear: .shoulders
        case .rotatorcuff: .shoulders
        case .lats: .lats
        case .trapsupper: .traps
        case .trapsmiddle: .upperback
        case .trapslower: .upperback
        case .rhomboids: .upperback
        case .erectors: .lowerback
        case .bicepsbrachii: .biceps
        case .brachialis: .biceps
        case .brachioradialis: .forearms
        case .forearmflexors: .forearms
        case .forearmextensors: .forearms
        case .tricepslong: .triceps
        case .tricepslateral: .triceps
        case .rectusabdominis: .abdominals
        case .obliques: .abdominals
        case .transverseabdominis: .abdominals
        case .iliopsoas: .other
        case .glutemax: .glutes
        case .glutemed: .abductors
        case .adductors: .adductors
        case .rectusfemoris: .quadriceps
        case .vasti: .quadriceps
        case .hamstrings: .hamstrings
        case .gastrocnemius: .calves
        case .soleus: .calves
        case .tibialis: .calves
        case .neck: .neck
        }
    }
}

/// A muscle an exercise works and the share of a set it counts for
/// (`muscles[]`): 1 for a prime mover, 0.5 for a synergist, less for one that
/// helps or steadies.
public struct MuscleShare: Codable, Hashable, Sendable {
    /// Nil for a muscle this build does not know, kept raw in `extra`.
    public var muscle: MuscleDetail?
    public var involvement: Decimal
    /// Unknown fields, kept for round trips (§3.4 rule 7).
    @UnknownFields public var extra: [String: JSONValue]

    public init(_ muscle: MuscleDetail, _ involvement: Decimal, extra: [String: JSONValue] = [:]) {
        self.muscle = muscle
        self.involvement = involvement
        self.extra = extra
    }
}

public extension Exercise {
    /// The share of a set each group is credited with: the largest share
    /// among its muscles where `muscles` is set, else 1 for a primary group
    /// and 0.5 for a secondary one.
    var groupShares: [Muscle: Decimal] {
        var out: [Muscle: Decimal] = [:]
        let own = muscles.contains { $0.muscle != nil } ? muscles : inheritedMuscles
        let known = own.compactMap { share in share.muscle.map { ($0, share.involvement) } }
        if !known.isEmpty {
            for (muscle, involvement) in known where involvement > 0 {
                out[muscle.group] = max(out[muscle.group] ?? 0, involvement)
            }
            return out
        }
        for muscle in secondaryMuscles { out[muscle] = max(out[muscle] ?? 0, Decimal(string: "0.5")!) }
        for muscle in primaryMuscles { out[muscle] = 1 }
        return out
    }

    /// The two group lists `muscles` implies, which a writer that sets it
    /// keeps in step: primary where a group's share is at least 0.75,
    /// secondary from 0.25, in the order of `muscles`.
    static func groupLists(_ muscles: [MuscleShare]) -> (primary: [Muscle], secondary: [Muscle]) {
        var shares: [Muscle: Decimal] = [:]
        var order: [Muscle] = []
        for share in muscles {
            guard let muscle = share.muscle else { continue }
            if shares[muscle.group] == nil { order.append(muscle.group) }
            shares[muscle.group] = max(shares[muscle.group] ?? 0, share.involvement)
        }
        let primary = order.filter { (shares[$0] ?? 0) >= Decimal(string: "0.75")! }
        let secondary = order.filter { (shares[$0] ?? 0) >= Decimal(string: "0.25")! && (shares[$0] ?? 0) < Decimal(string: "0.75")! }
        return (primary, secondary)
    }
}

public enum RestClass: String, Codable, Sendable, CaseIterable {
    case heavycompound, compound, isolation, highrep, timed, carry, power
}

public enum MyoStopRule: String, Codable, Sendable, CaseIterable {
    case repdrop, belowtarget
}
