import Foundation

// RPE provenance in the session (DESIGN §4.14; the package README, "Effort
// provenance"). The Rows screen shows the plan's RPE pre-filled on a pending
// row; one tick logs the set with it (`prefilled`). A tap on a value is a
// rating (`entered`) even when it equals the pre-fill, PREVIOUS copies a
// value (`copied`), and clearing removes the value, its method and the
// pre-fill. Equality never decides the method: the event says it.

/// What a row's RPE cell shows.
public enum RowRPE: Equatable, Sendable {
    /// No value, and nothing to pre-fill.
    case none
    /// The plan's value, pre-filled on a pending row and not yet the owner's.
    /// A tick that accepts it (`SessionState.acceptingRPEPrefill`) logs it as
    /// `prefilled`.
    case prefill(Decimal)
    /// A value the row holds: `entered` or `copied` on a pending row; on a
    /// completed row also `prefilled`.
    case value(Decimal, EffortMethod)

    /// The number the cell shows, pre-fill or value.
    public var shown: Decimal? {
        switch self {
        case .none: nil
        case let .prefill(value), let .value(value, _): value
        }
    }
}

public extension SessionState {
    /// The plan's RPE a pending row pre-fills: the top of its target's effort
    /// range (`SetTarget.effortTop`; a second side reads its head's). Nil for
    /// a completed row, one that holds an RPE, one whose RPE the owner
    /// cleared, and one whose plan names no effort (an added set, a drop, a
    /// mini-set).
    func rpePrefill(ofRow id: RowID) -> Decimal? {
        guard let row = rows[id], row.isPending, row.entered.rpe == nil, !row.rpePrefillDeclined else { return nil }
        let target = row.side != nil && !row.isHead ? rows[row.headID]?.target : row.target
        return target?.effortTop
    }

    /// The row's RPE as its cell shows it.
    func rpe(ofRow id: RowID) -> RowRPE {
        guard let row = rows[id] else { return .none }
        let values = row.performed?.values ?? row.entered
        if let rpe = values.rpe { return .value(rpe, values.rpeMethod ?? .entered) }
        return rpePrefill(ofRow: id).map(RowRPE.prefill) ?? .none
    }

    /// The values a tick commits when the owner accepts the pre-fill: with no
    /// RPE of their own, the row's pre-fill as `prefilled`; otherwise as given.
    /// The Rows screen's tick, Done and "Mark them done as shown" send these.
    func acceptingRPEPrefill(_ values: SetValues, row id: RowID) -> SetValues {
        guard values.rpe == nil, let prefill = rpePrefill(ofRow: id) else { return values }
        var accepted = values
        accepted.rpe = prefill
        accepted.rpeMethod = .prefilled
        return accepted
    }

    /// The RPE fields a commit of `id` stores: no method without a value; a
    /// value with none is `entered`; `prefilled` only as the row's own
    /// pre-fill; never `imported`.
    internal func committedEffort(_ values: SetValues, row id: RowID) throws(EngineError) -> SetValues {
        var values = values
        guard let rpe = values.rpe else {
            values.rpeMethod = nil
            return values
        }
        switch values.rpeMethod ?? .entered {
        case .entered, .copied:
            values.rpeMethod = values.rpeMethod ?? .entered
        case .prefilled:
            guard rpePrefill(ofRow: id) == rpe else { throw .invalidEffort }
        case .imported:
            throw .invalidEffort
        }
        return values
    }
}
