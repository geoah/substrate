import Foundation

/// A region of the body map, front or back (the figures the Stats pages
/// draw): which shape it colours and which muscles colour it. Deltoids and
/// trapezius appear on both figures, each from the muscles that face it.
public enum BodyRegion: String, CaseIterable, Sendable {
    case chest, abs, obliques, frontDelts, rearDelts, biceps, triceps, forearms, upperTraps, traps, lats, lowerBack, glutes,
        quadriceps, hamstrings, adductors, calves, tibialis, neck

    public enum Figure: Sendable { case front, back }

    /// The figures it is drawn on, and the shape's name in each.
    public var shapes: [(figure: Figure, slug: String)] {
        switch self {
        case .chest: [(.front, "chest")]
        case .abs: [(.front, "abs")]
        case .obliques: [(.front, "obliques")]
        case .frontDelts: [(.front, "deltoids")]
        case .rearDelts: [(.back, "deltoids")]
        case .biceps: [(.front, "biceps")]
        case .triceps: [(.front, "triceps"), (.back, "triceps")]
        case .forearms: [(.front, "forearm"), (.back, "forearm")]
        case .upperTraps: [(.front, "trapezius")]
        case .traps: [(.back, "trapezius")]
        case .lats: [(.back, "upper-back")]
        case .lowerBack: [(.back, "lower-back")]
        case .glutes: [(.back, "gluteal")]
        case .quadriceps: [(.front, "quadriceps")]
        case .hamstrings: [(.back, "hamstring")]
        case .adductors: [(.front, "adductors"), (.back, "adductors")]
        case .calves: [(.front, "calves"), (.back, "calves")]
        case .tibialis: [(.front, "tibialis")]
        case .neck: [(.front, "neck"), (.back, "neck")]
        }
    }

    /// The muscles that colour it. Each muscle shades a region of its own
    /// group's (`groups`), so an exercise shades the same regions whether it
    /// names its muscles or only their groups; the serratus is the chest's,
    /// and the hip flexors, which neither figure draws, shade nothing. The
    /// shin is the one exception: Hevy counts it with the calves, which a
    /// calf raise works and the shin does not.
    public var muscles: [MuscleDetail] {
        switch self {
        case .chest: [.pecclavicular, .pecsternal, .serratus]
        case .abs: [.rectusabdominis, .transverseabdominis]
        case .obliques: [.obliques]
        case .frontDelts: [.deltfront, .deltside]
        case .rearDelts: [.deltrear, .deltside, .rotatorcuff]
        case .biceps: [.bicepsbrachii, .brachialis]
        case .triceps: [.tricepslong, .tricepslateral]
        case .forearms: [.brachioradialis, .forearmflexors, .forearmextensors]
        case .upperTraps: [.trapsupper]
        case .traps: [.trapsupper, .trapsmiddle, .trapslower, .rhomboids]
        case .lats: [.lats]
        case .lowerBack: [.erectors]
        case .glutes: [.glutemax, .glutemed]
        case .quadriceps: [.rectusfemoris, .vasti]
        case .hamstrings: [.hamstrings]
        case .adductors: [.adductors]
        case .calves: [.gastrocnemius, .soleus]
        case .tibialis: [.tibialis]
        case .neck: [.neck]
        }
    }

    /// The groups that colour it for an exercise that names only groups.
    public var groups: [Muscle] {
        switch self {
        case .chest: [.chest]
        case .abs, .obliques: [.abdominals]
        case .frontDelts, .rearDelts: [.shoulders]
        case .biceps: [.biceps]
        case .triceps: [.triceps]
        case .forearms: [.forearms]
        case .upperTraps: [.traps]
        // Rhomboids and the middle and lower traps are the upper back's.
        case .traps: [.traps, .upperback]
        case .lats: [.lats]
        case .lowerBack: [.lowerback]
        case .glutes: [.glutes, .abductors]
        case .quadriceps: [.quadriceps]
        case .hamstrings: [.hamstrings]
        case .adductors: [.adductors]
        case .calves: [.calves]
        case .tibialis: []
        case .neck: [.neck]
        }
    }

    public var label: String {
        switch self {
        case .chest: "Chest"
        case .abs: "Abs"
        case .obliques: "Obliques"
        case .frontDelts: "Front and side delts"
        case .rearDelts: "Rear delts"
        case .biceps: "Biceps"
        case .triceps: "Triceps"
        case .forearms: "Forearms"
        case .upperTraps: "Upper traps"
        case .traps: "Traps and rhomboids"
        case .lats: "Lats"
        case .lowerBack: "Lower back"
        case .glutes: "Glutes"
        case .quadriceps: "Quadriceps"
        case .hamstrings: "Hamstrings"
        case .adductors: "Adductors"
        case .calves: "Calves"
        case .tibialis: "Shins"
        case .neck: "Neck"
        }
    }
}

public enum BodyMap {
    /// Working sets per region, each set counted by the largest share among
    /// the region's muscles (its exercise's own, else its movement's), or,
    /// for an exercise that names only groups, its share of the region's
    /// groups: so the triceps' two heads never count one set twice, as
    /// `Exercise.groupShares` reckons a group. A drop chain counts once.
    public static func sets(_ sets: [PerformedSet], exercises: [ExerciseID: Exercise]) -> [BodyRegion: Decimal] {
        var out: [BodyRegion: Decimal] = [:]
        for set in sets where set.role == .working && set.continues == nil {
            guard let exercise = exercises[set.exercise] else { continue }
            let own = exercise.muscles.contains { $0.muscle != nil } ? exercise.muscles : exercise.inheritedMuscles
            var shares: [MuscleDetail: Decimal] = [:]
            for share in own { if let muscle = share.muscle { shares[muscle] = max(shares[muscle] ?? 0, share.involvement) } }
            let groups = exercise.groupShares
            for region in BodyRegion.allCases {
                let share =
                    shares.isEmpty
                    ? region.groups.compactMap { groups[$0] }.max() ?? 0
                    : region.muscles.compactMap { shares[$0] }.max() ?? 0
                if share > 0 { out[region, default: 0] += share }
            }
        }
        return out
    }
}
