import Foundation

/// A column of the session's set grid (§7.6). Every grid starts with SET and
/// PREVIOUS and ends with the tick; the middle follows the measurement. A
/// grid is bounded tabular data, so fixed columns are right for it.
public enum GridColumn: String, Codable, Sendable, CaseIterable {
    case set, previous
    /// KG or LB: the load moved.
    case load
    /// +KG: the load added to body mass.
    case addedLoad
    /// −KG: the assistance taken off body mass.
    case assistance
    case reps
    /// A timed set's TIME, with its play button and countdown.
    case time
    /// M: metres.
    case distance
    case rpe
    case tick

    /// The value a middle column edits; nil for SET, PREVIOUS, RPE and the tick.
    public var field: ValueField? {
        switch self {
        case .load, .addedLoad: .load
        case .assistance: .assistance
        case .reps: .reps
        case .time: .duration
        case .distance: .distance
        case .set, .previous, .rpe, .tick: nil
        }
    }

    /// The header: `SET`, `PREVIOUS`, `KG` or `LB`, `+KG`, `−KG`, `REPS`,
    /// `TIME`, `M`, `RPE`; empty for the tick, which the view draws as a glyph.
    public func title(unit: MassUnit) -> String {
        let unitTitle = unit.rawValue.uppercased()
        return switch self {
        case .set: "SET"
        case .previous: "PREVIOUS"
        case .load: unitTitle
        case .addedLoad: "+" + unitTitle
        case .assistance: "\u{2212}" + unitTitle
        case .reps: "REPS"
        case .time: "TIME"
        case .distance: "M"
        case .rpe: "RPE"
        case .tick: ""
        }
    }

    /// What VoiceOver calls the field, with its unit: `load in kilograms`,
    /// `added load in pounds`, `reps`, `time in minutes and seconds`.
    public func spokenName(unit: MassUnit) -> String {
        switch self {
        case .set: "set"
        case .previous: "previous"
        case .load: "load in \(unit.spokenName)"
        case .addedLoad: "added load in \(unit.spokenName)"
        case .assistance: "assistance in \(unit.spokenName)"
        case .reps: "reps"
        case .time: "time in minutes and seconds"
        case .distance: "distance in metres"
        case .rpe: "RPE"
        case .tick: "done"
        }
    }
}

public extension MassUnit {
    /// `kilograms`, `pounds`: the unit as VoiceOver should say it.
    var spokenName: String {
        switch self {
        case .kg: "kilograms"
        case .lb: "pounds"
        }
    }
}

public extension MeasurementType {
    /// The set grid's middle columns (§7.6's table).
    var valueColumns: [GridColumn] {
        switch self {
        case .weightreps: [.load, .reps]
        case .repsonly, .bodyweightreps: [.reps]
        case .weightedbodyweightreps: [.addedLoad, .reps]
        case .bodyweightassistedreps: [.assistance, .reps]
        case .duration: [.time]
        case .weightduration: [.load, .time]
        case .distanceduration: [.distance, .time]
        case .shortdistanceweight: [.load, .distance]
        }
    }

    /// Rep sets have an RPE column. Timed and distance sets take an RPE too,
    /// rated from the row's menu ("Rate effort") and shown under the row,
    /// so their grids keep room for the timer (§7.6).
    var hasRPEColumn: Bool { recordsReps }

    /// Every column of the grid, in order.
    var gridColumns: [GridColumn] {
        [.set, .previous] + valueColumns + (hasRPEColumn ? [.rpe] : []) + [.tick]
    }

    /// A carry times itself with a stopwatch below the row (§7.6); a TIME
    /// column holds its own play button.
    var hasStopwatchBelowRow: Bool { self == .shortdistanceweight }
}
