import Foundation

/// The rest and counting settings a session runs with (§6.4). The controller
/// snapshots them at Start, so a fold never depends on settings changed later.
public struct SessionSettings: Codable, Equatable, Sendable {
    /// The unit volume is shown in, and the unit of an exercise with no `loadStep`.
    public var defaultUnit: MassUnit
    /// The rest after a warm-up set when neither slot names one.
    public var warmupRest: Seconds
    /// The set rest by rest class, and the fallback for an exercise with none.
    public var setRest: SetRestDefaults
    /// "Rest before next exercise": a timer after an exercise's last set (§4.7 line 7).
    public var restAfterLastSet: Bool
    /// Warm-ups count in the volume load (req 32).
    public var countWarmupsInVolume: Bool

    public init(
        defaultUnit: MassUnit = .kg,
        warmupRest: Seconds = RestDefaults.warmupRest,
        setRest: SetRestDefaults = SetRestDefaults(),
        restAfterLastSet: Bool = true,
        countWarmupsInVolume: Bool = false
    ) {
        self.defaultUnit = defaultUnit
        self.warmupRest = warmupRest
        self.setRest = setRest
        self.restAfterLastSet = restAfterLastSet
        self.countWarmupsInVolume = countWarmupsInVolume
    }
}

/// The set rest by the exercise's rest class (02 §4.3), editable in Settings.
public struct SetRestDefaults: Codable, Equatable, Sendable {
    public var heavycompound: Seconds
    public var compound: Seconds
    public var isolation: Seconds
    public var highrep: Seconds
    public var timed: Seconds
    public var carry: Seconds
    public var power: Seconds
    /// An exercise with no rest class: 02 §4.2 says err long.
    public var fallback: Seconds

    public init(
        heavycompound: Seconds = 180,
        compound: Seconds = 120,
        isolation: Seconds = 90,
        highrep: Seconds = 60,
        timed: Seconds = 60,
        carry: Seconds = 120,
        power: Seconds = 180,
        fallback: Seconds = 120
    ) {
        self.heavycompound = heavycompound
        self.compound = compound
        self.isolation = isolation
        self.highrep = highrep
        self.timed = timed
        self.carry = carry
        self.power = power
        self.fallback = fallback
    }

    /// The set rest for an exercise of `restClass`; the fallback for none.
    public subscript(restClass: RestClass?) -> Seconds {
        get {
            switch restClass {
            case .heavycompound: heavycompound
            case .compound: compound
            case .isolation: isolation
            case .highrep: highrep
            case .timed: timed
            case .carry: carry
            case .power: power
            case nil: fallback
            }
        }
        set {
            switch restClass {
            case .heavycompound: heavycompound = newValue
            case .compound: compound = newValue
            case .isolation: isolation = newValue
            case .highrep: highrep = newValue
            case .timed: timed = newValue
            case .carry: carry = newValue
            case .power: power = newValue
            case nil: fallback = newValue
            }
        }
    }
}

/// The rests the routine and the owner leave unsaid (02 §4.3 and §4.4; §4.7's
/// defaults table). Only the warm-up and set rests are settings; these are not.
public enum RestDefaults {
    /// The warm-up rest the settings start from.
    public static let warmupRest: Seconds = 45
    /// One side to the other when the slot names no side switch.
    public static let sideSwitch: Seconds = 0
    /// Transitions, intra-set pauses and side switches up to this long end
    /// with a haptic only; longer ones notify too (disagreement 8).
    public static let quietLimit: Seconds = 20

    /// The pause between a structured set's segments when its planned set
    /// names none. A drop runs no timer.
    public static func intraSetPause(_ structure: Structure) -> Seconds {
        switch structure {
        case .straight, .drop: 0
        case .myo, .myomatch: 15
        case .restpause, .cluster: 20
        }
    }

    /// The move from one group member to the next inside a round: by the
    /// pairing when there is one, else by the kind (disagreement 9).
    public static func transition(kind: GroupKind, pairing: Pairing?) -> Seconds {
        switch pairing {
        case .samemuscle, .preexhaust, .mechanicaldrop: return 0
        case .antagonist, .upperlower, .unrelated: return 15
        case nil: break
        }
        switch kind {
        case .paired: return 60
        case .triset, .giantset, .circuit: return 15
        case .superset: return 0
        }
    }

    /// The rest after a round: a circuit 2:00; any other group 2:30 when a
    /// member is a heavy compound, compound, power or carry exercise, else 1:30.
    public static func roundRest(kind: GroupKind, memberRestClasses: [RestClass?]) -> Seconds {
        if kind == .circuit { return 120 }
        let heavy: Set<RestClass> = [.heavycompound, .compound, .power, .carry]
        return memberRestClasses.contains { $0.map(heavy.contains) ?? false } ? 150 : 90
    }
}
