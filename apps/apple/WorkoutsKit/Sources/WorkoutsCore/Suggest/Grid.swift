import Foundation

/// A ladder of loadable values `origin + j × step` for `j` in `lowest ... highest`
/// (either end may be open), with 03 §10.5's operations. Every point it returns
/// is rounded to 6 decimals.
struct LoadGrid: Sendable, Hashable {
    var origin: Double
    var step: Double
    /// Nil: unbounded below.
    var lowest: Int?
    /// Nil: unbounded above.
    var highest: Int?

    /// A grid of one point: plain bodyweight, or an exercise with no grid around
    /// the load a rule starts from (DESIGN §5.3: the rule keeps that load).
    static func single(_ value: Double) -> LoadGrid {
        LoadGrid(origin: value, step: 1, lowest: 0, highest: 0)
    }

    var isSinglePoint: Bool { lowest != nil && lowest == highest }

    func point(_ j: Int) -> Double { SuggestMath.round6(origin + Double(j) * step) }

    /// `floorG`: the heaviest point at most `x + 1e-9`; if none, the lightest point.
    func floor(_ x: Double) -> Double {
        var j = SuggestMath.index((x - origin) / step + 1e-9, .down)
        if let highest, j > highest { j = highest }
        if let lowest, j < lowest { j = lowest }
        return point(j)
    }

    /// `next`: the lightest point above `value + 1e-9`, or nil.
    func next(_ value: Double) -> Double? {
        var j = SuggestMath.index((value - origin) / step, .down) + 1
        if let lowest, j < lowest { j = lowest }
        // The index arithmetic can be one off near a point; settle it on the definition.
        while point(j) <= value + 1e-9 { j += 1 }
        while lowest.map({ j - 1 >= $0 }) ?? true, point(j - 1) > value + 1e-9 { j -= 1 }
        if let highest, j > highest { return nil }
        return point(j)
    }

    /// `prev`: the heaviest point below `value − 1e-9`, or nil.
    func prev(_ value: Double) -> Double? {
        var j = SuggestMath.index((value - origin) / step, .up) - 1
        if let highest, j > highest { j = highest }
        while point(j) >= value - 1e-9 { j -= 1 }
        while highest.map({ j + 1 <= $0 }) ?? true, point(j + 1) < value - 1e-9 { j += 1 }
        if let lowest, j < lowest { return nil }
        return point(j)
    }
}

/// Where the load rules work for one body mass: on effective loads (03 §10.4),
/// or, in a bodyweight mode with no body mass, on the settings as logged
/// (03 §11.5). A value is `offset ± setting`; the assistance counts down.
struct LoadSpace: Sendable {
    var mode: LoadMode03
    /// `bodyShare × body mass` for effective loads in a bodyweight mode; else 0.
    var offset: Double
    /// False when the values are settings (a bodyweight mode with no body mass).
    var isEffective: Bool
    /// Nil when the exercise has no grid (DESIGN §5.3).
    var grid: LoadGrid?

    /// The space for an exercise and a body mass in the exercise unit; nil for
    /// an exercise that moves no load.
    init?(exercise: ExerciseConfig, bodyMass: Double?) {
        guard exercise.mode != .none else { return nil }
        // A step under 0.01 is no real equipment; reading it as no grid also keeps
        // the index arithmetic (and `next`'s settling loops) short.
        let step = exercise.step.flatMap { $0 >= 0.01 ? $0 : nil }
        let mostSteps = step.flatMap { step in
            exercise.maxLoad.map { max(0, SuggestMath.index($0 / step + 1e-9, .down)) }
        }
        let offset = exercise.mode == .external ? 0 : bodyMass.map { exercise.bodyShare * $0 } ?? 0
        let isEffective = exercise.mode == .external || bodyMass != nil
        var grid: LoadGrid?
        switch exercise.mode {
        case .none:
            return nil
        case .external:
            if let step {
                let base = exercise.base ?? 0
                // A 0 kg load is not a set (03 §10.3), so a grid based at 0 starts one step up.
                let lowest = base > 1e-9 ? 0 : 1
                let highest = exercise.maxLoad.map { max(lowest, SuggestMath.index(($0 - base) / step + 1e-9, .down)) }
                grid = LoadGrid(origin: base, step: step, lowest: lowest, highest: highest)
            }
        case .bodyweightAdded:
            if !exercise.canAddLoad {
                grid = .single(offset)
            } else if let step {
                grid = LoadGrid(origin: offset, step: step, lowest: 0, highest: mostSteps)
            }
        case .bodyweightAssisted:
            if let step {
                // j counts steps of assistance down from no assistance, the heaviest point.
                var most = mostSteps
                if isEffective {
                    // Every point stays above 0 (03 §10.5).
                    let positive = max(0, SuggestMath.index(offset / step - 1e-9, .up) - 1)
                    most = min(most ?? positive, positive)
                }
                grid = LoadGrid(origin: offset, step: step, lowest: most.map { -$0 }, highest: 0)
            }
        }
        self.mode = exercise.mode
        self.offset = offset
        self.isEffective = isEffective
        self.grid = grid
    }

    private var sign: Double { mode == .bodyweightAssisted ? -1 : 1 }

    /// The value of a setting (a load, an added load or an assistance).
    func value(ofSetting setting: Double?) -> Double {
        SuggestMath.round6(offset + sign * (setting ?? 0))
    }

    /// The setting a value stands for: the load, the added load or the assistance.
    func setting(ofValue value: Double) -> Double {
        SuggestMath.round6(sign * (value - offset))
    }

    /// The grid the rules use around `anchor`: the exercise's, or a single point
    /// at `anchor` when it has none, so every rule that moves a load keeps it.
    func grid(around anchor: Double) -> LoadGrid {
        grid ?? .single(anchor)
    }
}

/// The suggestion engine's number helpers, kept in one namespace so they never
/// meet another file's free functions in the module.
enum SuggestMath {
    /// Kilograms in a pound, exactly (03 §4).
    static let kilogramsPerPound = 0.45359237

    /// Rounds to 6 decimals (03 §10.5), never returning −0.
    static func round6(_ x: Double) -> Double {
        (x * 1_000_000).rounded() / 1_000_000 + 0
    }

    /// Rounds to 2 decimals, as a load converted between units (03 §13).
    static func round2(_ x: Double) -> Double {
        (x * 100).rounded() / 100 + 0
    }

    /// An index from a double, clamped so a stray huge value or NaN cannot trap.
    static func index(_ x: Double, _ rule: FloatingPointRoundingRule) -> Int {
        guard x.isFinite else { return x.isNaN ? 0 : (x > 0 ? 1_000_000_000 : -1_000_000_000) }
        return Int(min(max(x, -1e9), 1e9).rounded(rule))
    }

    /// A load in `to`, converted with 1 lb = 0.45359237 kg and rounded to 0.01 (03 §13).
    static func convert(_ value: Double, from: MassUnit, to: MassUnit) -> Double {
        switch (from, to) {
        case (.kg, .lb): SuggestMath.round2(value / kilogramsPerPound)
        case (.lb, .kg): SuggestMath.round2(value * kilogramsPerPound)
        default: value
        }
    }

    /// Whole local calendar days from `start` to `now`.
    static func days(from start: Date, to now: Date, calendar: Calendar) -> Int {
        let from = calendar.startOfDay(for: start)
        return calendar.dateComponents([.day], from: from, to: calendar.startOfDay(for: now)).day ?? 0
    }
}
