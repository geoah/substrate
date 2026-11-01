import Foundation

/// Where a rest's value came from, first present wins (§4.7).
public enum RestSource: String, Codable, Sendable {
    /// No timer: every set is done (line 0), or the rest after an exercise's
    /// last set is switched off.
    case none
    /// The planned set's `rest`, or a per-set override set in the gym.
    case plannedSet
    case sessionSlot
    case routineSlot
    case sessionGroup
    case routineGroup
    /// The owner's settings: the warm-up rest or the set rest by rest class.
    case settings
    /// §4.7's defaults table: transitions, round rests, intra-set pauses, side switches.
    case rule
}

/// One rest and where it came from.
public struct RestValue: Equatable, Sendable {
    public var seconds: Seconds
    public var source: RestSource

    public init(_ seconds: Seconds, _ source: RestSource) {
        self.seconds = seconds
        self.source = source
    }
}

/// The rest a tick starts (§4.7).
public struct RestDecision: Equatable, Sendable {
    /// Nil when no row is left (line 0).
    public var slot: RestSlot?
    /// 0 means no countdown: Up next simply moves on.
    public var seconds: Seconds
    public var source: RestSource
    /// The end alerts with a notification, not a haptic only.
    public var notifies: Bool

    public init(slot: RestSlot?, seconds: Seconds, source: RestSource) {
        self.slot = slot
        self.seconds = seconds
        self.source = source
        notifies = RestRules.notifies(slot, seconds: seconds)
    }
}

/// Picks every rest from the rest slots (§4.7): after a row C is ticked, from
/// C and the row expected next, N (§4.6 rule 2 with C as the anchor). The
/// first matching line wins.
public enum RestRules {
    /// The rest after `row` was completed in `state` (its segments already generated).
    public static func after(_ row: RowID, in state: SessionState) -> RestDecision {
        guard let c = state.rows[row], let head = state.rows[c.headID], let cSlot = state.slot(c.slot) else {
            return RestDecision(slot: nil, seconds: 0, source: .none)
        }
        // Line 0: nothing left.
        guard let nID = UpNext.expectedNext(after: row, in: state), let n = state.rows[nID],
              let nSlot = state.slot(n.slot)
        else {
            return RestDecision(slot: nil, seconds: 0, source: .none)
        }
        // Line 1: a per-set override is a rest after the whole set.
        if state.isChainComplete(head.id), let override = head.restOverride {
            return RestDecision(slot: .override, seconds: override, source: .plannedSet)
        }
        if n.headID == head.id {
            // Line 2: the other side.
            if n.side != nil {
                let value = sideSwitch(slot: cSlot.key, in: state)
                return RestDecision(slot: .sideSwitch, seconds: value.seconds, source: value.source)
            }
            // Line 3: the pause before the chain's next drop, mini-set or cluster.
            let value = intraSetPause(head: head)
            return RestDecision(slot: .intraSetPause, seconds: value.seconds, source: value.source)
        }
        // Line 4: after a warm-up.
        if head.role == .warmup {
            let value = warmupRest(slot: cSlot.key, in: state)
            return RestDecision(slot: .warmupRest, seconds: value.seconds, source: value.source)
        }
        if let group = cSlot.group, nSlot.group == group {
            let sameRound = UpNext.round(of: c.id, in: state) == UpNext.round(of: n.id, in: state)
            // Line 5: the next member of the same round.
            if sameRound, nSlot.key != cSlot.key {
                let value = transition(slot: cSlot.key, in: state)
                return RestDecision(slot: .transition, seconds: value.seconds, source: value.source)
            }
            // Line 6: the next round.
            let value = roundRest(group: group, in: state)
            return RestDecision(slot: .roundRest, seconds: value.seconds, source: value.source)
        }
        // Line 7: the next block.
        if nSlot.key != cSlot.key {
            guard state.settings.restAfterLastSet else {
                return RestDecision(slot: .blockRest, seconds: 0, source: .none)
            }
            let value = cSlot.group.map { roundRest(group: $0, in: state) } ?? setRest(slot: cSlot.key, in: state)
            return RestDecision(slot: .blockRest, seconds: value.seconds, source: value.source)
        }
        // Line 8: the next set of the same plain slot.
        let value = setRest(slot: cSlot.key, in: state)
        return RestDecision(slot: .setRest, seconds: value.seconds, source: value.source)
    }

    /// Whether a rest of `slot` alerts with a notification: every rest and
    /// the standalone timer do; transitions, pauses and side switches only
    /// when longer than 20 s (disagreement 8). A rest of 0 runs no timer.
    public static func notifies(_ slot: RestSlot?, seconds: Seconds) -> Bool {
        guard let slot, seconds > 0 else { return false }
        switch slot {
        case .override, .warmupRest, .roundRest, .blockRest, .setRest: return true
        case .transition, .intraSetPause, .sideSwitch: return seconds > RestDefaults.quietLimit
        }
    }

    // MARK: - Each rest, first present wins

    /// Session slot, routine slot, the settings for the exercise's rest
    /// class, else the settings' fallback.
    public static func setRest(slot key: String, in state: SessionState) -> RestValue {
        if let value = state.slot(key)?.rests.setRest { return RestValue(value, .sessionSlot) }
        if let value = state.routineSlot(of: key)?.setRest { return RestValue(value, .routineSlot) }
        return RestValue(state.settings.setRest[state.exercise(ofSlot: key)?.restClass], .settings)
    }

    /// Session slot, routine slot, the settings' warm-up rest.
    public static func warmupRest(slot key: String, in state: SessionState) -> RestValue {
        if let value = state.slot(key)?.rests.warmupRest { return RestValue(value, .sessionSlot) }
        if let value = state.routineSlot(of: key)?.warmupRest { return RestValue(value, .routineSlot) }
        return RestValue(state.settings.warmupRest, .settings)
    }

    /// Session slot, routine slot, the default by pairing or kind. A slot in
    /// no group transitions in 0.
    public static func transition(slot key: String, in state: SessionState) -> RestValue {
        if let value = state.slot(key)?.rests.transition { return RestValue(value, .sessionSlot) }
        if let value = state.routineSlot(of: key)?.transition { return RestValue(value, .routineSlot) }
        guard let groupKey = state.slot(key)?.group, let group = state.group(groupKey) else { return RestValue(0, .rule) }
        let pairing = group.pairing ?? state.routineGroup(of: groupKey)?.pairing
        return RestValue(RestDefaults.transition(kind: group.groupKind, pairing: pairing), .rule)
    }

    /// Session group, routine group, the default by kind and members.
    public static func roundRest(group key: String, in state: SessionState) -> RestValue {
        guard let group = state.group(key) else { return RestValue(0, .rule) }
        if let value = group.roundRest { return RestValue(value, .sessionGroup) }
        if let value = state.routineGroup(of: key)?.roundRest { return RestValue(value, .routineGroup) }
        let classes = state.members(ofGroup: key).map { state.exercises[$0.exercise]?.restClass }
        return RestValue(RestDefaults.roundRest(kind: group.groupKind, memberRestClasses: classes), .rule)
    }

    /// Session slot, routine slot, 0.
    public static func sideSwitch(slot key: String, in state: SessionState) -> RestValue {
        if let value = state.slot(key)?.rests.sideSwitch { return RestValue(value, .sessionSlot) }
        if let value = state.routineSlot(of: key)?.sideSwitch { return RestValue(value, .routineSlot) }
        return RestValue(RestDefaults.sideSwitch, .rule)
    }

    /// The head's planned pause, else by structure.
    public static func intraSetPause(head: Row) -> RestValue {
        if let value = head.intraSetPause { return RestValue(value, .plannedSet) }
        return RestValue(RestDefaults.intraSetPause(head.structure), .rule)
    }
}
