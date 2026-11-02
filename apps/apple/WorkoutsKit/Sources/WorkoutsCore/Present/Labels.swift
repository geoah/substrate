import Foundation

// The words the screens use for the model's values: the kind files' own
// labels where they have one, and §7's copy elsewhere. `SetLabel` (W, 2, 2a,
// M, 1L) lives with the ordinals in Engine/Ordinals.swift; the records form
// of it, for history, is at the end of this file.

public extension MeasurementType {
    /// The kind's label: `Weight and reps`.
    var label: String {
        switch self {
        case .weightreps: "Weight and reps"
        case .repsonly: "Reps only"
        case .bodyweightreps: "Bodyweight reps"
        case .weightedbodyweightreps: "Bodyweight plus load and reps"
        case .bodyweightassistedreps: "Assisted bodyweight reps"
        case .duration: "Duration"
        case .weightduration: "Weight and duration"
        case .distanceduration: "Distance and duration"
        case .shortdistanceweight: "Weight and distance"
        }
    }
}

public extension Equipment {
    var label: String {
        switch self {
        case .none: "None"
        case .barbell: "Barbell"
        case .dumbbell: "Dumbbell"
        case .kettlebell: "Kettlebell"
        case .machine: "Machine"
        case .plateloaded: "Plate-loaded machine"
        case .cable: "Cable"
        case .plate: "Plate"
        case .resistanceband: "Resistance band"
        case .suspension: "Suspension"
        case .other: "Other"
        }
    }
}

public extension Exercise {
    /// The equipment as the lists and the detail say it: its label
    /// (`Machine`); with none, `Bodyweight` for a bodyweight measurement and
    /// nil otherwise, never "None". The new-exercise form's picker keeps
    /// `Equipment.label`, where "None" is a choice.
    var equipmentText: String? {
        if let equipment, equipment != .none { return equipment.label }
        switch measurement.loadMode {
        case .bodyweight, .bodyweightplus, .assisted: return "Bodyweight"
        case .external, nil: return nil
        }
    }
}

public extension Laterality {
    var label: String {
        switch self {
        case .bilateral: "Both sides"
        case .unilateral: "One side at a time"
        case .alternating: "Alternating sides"
        }
    }
}

public extension Muscle {
    var label: String {
        switch self {
        case .fullbody: "Full body"
        case .lowerback: "Lower back"
        case .upperback: "Upper back"
        default: rawValue.prefix(1).uppercased() + rawValue.dropFirst()
        }
    }
}

public extension RestClass {
    /// The kind's label without its rest: `Heavy compound`.
    var label: String {
        switch self {
        case .heavycompound: "Heavy compound"
        case .compound: "Compound"
        case .isolation: "Isolation"
        case .highrep: "High-rep small muscles and trunk"
        case .timed: "Timed hold"
        case .carry: "Carry or sled"
        case .power: "Power and jumps"
        }
    }
}

public extension GroupKind {
    /// `Superset`, `Tri-set`, `Giant set`, `Circuit`, `Paired sets`.
    var label: String {
        switch self {
        case .superset: "Superset"
        case .triset: "Tri-set"
        case .giantset: "Giant set"
        case .circuit: "Circuit"
        case .paired: "Paired sets"
        }
    }
}

public extension Pairing {
    var label: String {
        switch self {
        case .antagonist: "Antagonists"
        case .samemuscle: "Same muscle"
        case .preexhaust: "Pre-exhaust"
        case .upperlower: "Upper with lower body"
        case .unrelated: "Unrelated"
        case .mechanicaldrop: "Mechanical drop"
        }
    }
}

public extension SetKind {
    /// The set-kind menu's items (§7.6).
    var label: String {
        switch self {
        case .warmup: "Warm-up"
        case .working: "Working"
        case .failure: "Failure"
        case .drop: "Drop set"
        case .myo: "Myo-reps"
        case .myomatch: "Myo-rep match"
        }
    }
}

public extension SlotRestKind {
    /// The exercise menu's "Rest…" items.
    var label: String {
        switch self {
        case .setRest: "Set rest"
        case .warmupRest: "Warm-up rest"
        case .transition: "Transition"
        case .sideSwitch: "Side switch"
        }
    }
}

public extension RestSlot {
    /// The rest bar's countdown label (§7.6): `Rest`, `Warm-up rest`,
    /// `Round rest`, `Rest before next exercise`, `Switch sides`, `Pause`.
    /// A transition reads `Go: Bird Dog (A2) in`, which the presenter builds.
    var label: String {
        switch self {
        case .override, .setRest: "Rest"
        case .warmupRest: "Warm-up rest"
        case .intraSetPause: "Pause"
        case .sideSwitch: "Switch sides"
        case .transition: "Transition"
        case .roundRest: "Round rest"
        case .blockRest: "Rest before next exercise"
        }
    }
}

/// The group member label, `A1`: the group key upper-cased and the member's
/// place from 1 (§7.6).
public func groupMemberLabel(group key: String, position: Int) -> String {
    "\(key.uppercased())\(position)"
}

// MARK: - Effort scales

/// The RPE sheet's words (§7.6: the RIR-based scale with Hevy's wording, 02 §5.2).
public enum RPEScaleText {
    /// The values the sheet offers: 6 to 10 in half steps.
    public static let values: [Decimal] = [6, 6.5, 7, 7.5, 8, 8.5, 9, 9.5, 10].map { Decimal($0) }

    /// The headline: `Hard` for 8 and 8.5. Nil for a value off the scale.
    public static func headline(_ rpe: Decimal) -> String? {
        switch rpe {
        case 10: "Maximum effort"
        case 9, 9.5: "Very hard"
        case 8, 8.5: "Hard"
        case 7, 7.5: "Challenging"
        case 6, 6.5: "Moderate effort"
        default: nil
        }
    }

    /// The reps-in-reserve line: `Could do 1 more rep, maybe 2` for 8.5.
    public static func reserve(_ rpe: Decimal) -> String? {
        switch rpe {
        case 10: "Could not do more reps"
        case 9.5: "Maybe 1 more rep"
        case 9: "Could do 1 more rep"
        case 8.5: "Could do 1 more rep, maybe 2"
        case 8: "Could do 2 more reps"
        case 7.5: "Could do 2 more reps, maybe 3"
        case 7: "Could do 3 more reps"
        case 6.5: "Could do 3 more reps, maybe 4"
        case 6: "Could do 4 or more reps"
        default: nil
        }
    }

    /// The reps-in-reserve line for a set of `measurement`: the line above
    /// for rep sets; nil for timed and distance sets ("Rate effort"), where
    /// reps in reserve mean nothing and the headline says it all.
    public static func reserve(_ rpe: Decimal, measurement: MeasurementType) -> String? {
        measurement.recordsReps ? reserve(rpe) : nil
    }

    /// The empty sheet's two lines.
    public static let emptyHeadline = "Select RPE"
    public static let emptyReserve = "Slide or tap to rate your effort"
}

/// The Finish sheet's session RPE words (§7.7, the CR10 scale's anchors
/// carried over to the unlabelled values).
public enum SessionRPEText {
    public static let values = Array(1 ... 10)

    public static func word(_ value: Int) -> String? {
        switch value {
        case 1: "very, very easy"
        case 2: "easy"
        case 3: "moderate"
        case 4: "somewhat hard"
        case 5, 6: "hard"
        case 7, 8, 9: "very hard"
        case 10: "maximal"
        default: nil
        }
    }
}

// MARK: - Labels of records

public extension SetLabel {
    /// The labels of one slot's records in a finished session (history, the
    /// session detail, the exercise detail), by the same rules as a live
    /// session's rows (§4.10): `W`, `2`, `2a`, `M`, `1L`. Records with no
    /// `ordinal` (an importer's) are numbered in order.
    static func make(records sets: [PerformedSet]) -> [SetID: SetLabel] {
        let ordinals = SetLabel.ordinals(records: sets)
        let byID = Dictionary(sets.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let warmups = sets.count { $0.role == .warmup && $0.continues == nil }
        var result: [SetID: SetLabel] = [:]
        var segmentIndex: [SetID: Int] = [:]
        for set in sets {
            let head = set.continues.flatMap { byID[$0] } ?? set
            let ordinal = ordinals[set.id] ?? 1
            let warmup = head.role == .warmup
            let number = warmup ? "W" : "\(ordinal)"
            let spokenSet = warmup ? (warmups > 1 ? "warm-up \(ordinal)" : "warm-up") : "set \(ordinal)"
            let isHead = set.continues == nil
            // A set switched to Failure in the gym says so with reachedFailure (§5.2).
            let failure = isHead && (set.target?.termination == .failure || set.target?.termination == .amrap
                || set.reachedFailure == true)
            let roleText = isHead ? head.roleLabel.map { $0 == .top ? "Top" : "Back-off" } : nil
            if let side = set.side {
                let letter = side == .left ? "L" : "R"
                result[set.id] = SetLabel(short: number + letter, spoken: "\(spokenSet), \(side.rawValue) side",
                                          failureBadge: failure, roleText: roleText)
                continue
            }
            guard let headID = set.continues else {
                result[set.id] = SetLabel(short: number, spoken: spokenSet, failureBadge: failure, roleText: roleText)
                continue
            }
            let index = (segmentIndex[headID] ?? 0) + 1
            segmentIndex[headID] = index
            let letter = String(UnicodeScalar(UInt8(ascii: "a") + UInt8(min(index - 1, 25))))
            result[set.id] = switch head.structure {
            case .drop: SetLabel(short: number + letter, spoken: "\(spokenSet), drop \(index)")
            case .restpause: SetLabel(short: number + letter, spoken: "\(spokenSet), mini-set \(index)")
            case .cluster: SetLabel(short: number + letter, spoken: "\(spokenSet), cluster \(index + 1)")
            case .myo, .myomatch: SetLabel(short: "M", spoken: "\(spokenSet), mini-set \(index)")
            case .straight: SetLabel(short: number + letter, spoken: spokenSet)
            }
        }
        return result
    }

    /// Each record's ordinal: its own, else counted by role over the chain
    /// heads in order, a segment sharing its head's.
    internal static func ordinals(records sets: [PerformedSet]) -> [SetID: Int] {
        var result: [SetID: Int] = [:]
        var counters: [Role: Int] = [:]
        for set in sets where set.continues == nil {
            let ordinal = set.ordinal ?? (counters[set.role] ?? 0) + 1
            counters[set.role] = max(counters[set.role] ?? 0, ordinal)
            result[set.id] = ordinal
        }
        for set in sets {
            if let head = set.continues { result[set.id] = set.ordinal ?? result[head] }
        }
        return result
    }
}

extension String {
    /// The first letter upper-cased: "set 2" becomes "Set 2".
    var capitalizedFirst: String {
        prefix(1).uppercased() + dropFirst()
    }
}
