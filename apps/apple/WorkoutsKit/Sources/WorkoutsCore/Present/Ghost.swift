import Foundation

/// The grey values a pending row shows where nothing is typed (§5.5). Each
/// field takes the first present of the suggestion, the copied target and
/// PREVIOUS; RPE is never ghosted.
public struct GhostValues: Equatable, Sendable {
    /// The load, or the added load on a bodyweight-plus exercise.
    public var load: Mass?
    public var assistance: Mass?
    public var reps: Int?
    public var duration: Seconds?
    public var distance: Length?
    /// The reps are a hint (`≈15`) the tick does not commit: a `rirstop`
    /// set ends at its effort, and the owner types the reps done (§5.3).
    public var repsIsHint: Bool
    /// Words in place of a rep count: a drop or an open mini-set goes "to
    /// failure", and nothing is committed for it.
    public var repsPlaceholder: String?

    public init(load: Mass? = nil, assistance: Mass? = nil, reps: Int? = nil, duration: Seconds? = nil, distance: Length? = nil,
                repsIsHint: Bool = false, repsPlaceholder: String? = nil)
    {
        self.load = load
        self.assistance = assistance
        self.reps = reps
        self.duration = duration
        self.distance = distance
        self.repsIsHint = repsIsHint
        self.repsPlaceholder = repsPlaceholder
    }

    /// The words a reps cell shows for a drop or an open mini-set.
    public static let toFailure = "to failure"
}

/// Everything the presenter derives per row, computed once per call and
/// memoized: the suggestion inputs (§5.2), the suggestion, PREVIOUS (§5.4),
/// the ghost values (§5.5) and the values a tick commits. A chain's later
/// link reads its earlier links' shown values (a drop's load comes from the
/// load its parent shows), so the parts call each other; display order
/// guarantees no cycle. Local to one presentation, never shared.
final class SessionEvaluator {
    let state: SessionState
    let history: HistoryIndex
    let now: Date
    let calendar: Calendar
    /// Today's body mass in kilograms.
    let bodyMass: Double?

    private var configs: [ExerciseID: ExerciseConfig] = [:]
    private var pasts: [ExerciseID: [PastSession]] = [:]
    private var suggestions: [RowID: Suggestion?] = [:]
    private var previouses: [RowID: PreviousSet?] = [:]
    private var lastTimes: [ExerciseID: ExerciseHistory?] = [:]
    private var ghosts: [RowID: GhostValues] = [:]

    init(state: SessionState, history: HistoryIndex, now: Date, calendar: Calendar) {
        self.state = state
        self.history = history
        self.now = now
        self.calendar = calendar
        bodyMass = state.session.bodyMass.map(SuggestionInputs.kilograms)
    }

    // MARK: - Inputs

    func exercise(ofRow row: Row) -> Exercise? {
        state.exercise(ofSlot: row.slot)
    }

    func config(_ exercise: Exercise) -> ExerciseConfig {
        if let cached = configs[exercise.id] { return cached }
        let config = SuggestionInputs.config(for: exercise, defaultUnit: state.settings.defaultUnit)
        configs[exercise.id] = config
        return config
    }

    func past(_ exercise: Exercise) -> [PastSession] {
        if let cached = pasts[exercise.id] { return cached }
        let sessions = SuggestionInputs.history(of: exercise, config: config(exercise), index: history,
                                                excluding: state.session.id, now: now, calendar: calendar)
        pasts[exercise.id] = sessions
        return sessions
    }

    /// Today (§5.2): this session's completed rows of the exercise, every
    /// slot of it, in display order; a side pair folded into one set. A side
    /// chain's own sets are left out when its second side asks (`excluding`).
    func today(_ exercise: Exercise, excludingChain excluded: RowID? = nil) -> [DoneSet] {
        let config = config(exercise)
        var result: [DoneSet] = []
        for slot in state.slots where slot.exercise == exercise.id {
            let ordinals = Ordinals.ordinals(inSlot: slot.key, of: state)
            for unit in state.units(inSlot: slot.key) {
                guard let head = state.rows[unit[0]], head.id != excluded else { continue }
                var pair: DoneSet?
                for id in unit {
                    guard let row = state.rows[id], let performed = row.performed else { continue }
                    let done = doneSet(row, head: head, values: performed.values, completedAt: performed.completedAt,
                                       exercise: exercise, config: config, ordinal: ordinals[head.id])
                    if row.side != nil {
                        pair = pair.map { SuggestionInputs.merged($0, with: done) } ?? done
                    } else {
                        result.append(done)
                    }
                }
                if let pair { result.append(pair) }
            }
        }
        for index in result.indices {
            result[index].position = index + 1
        }
        return result
    }

    func doneSet(_ row: Row, head: Row, values: SetValues, completedAt: Date, exercise: Exercise, config: ExerciseConfig,
                 ordinal: Int?) -> DoneSet
    {
        let kind = SuggestionInputs.kind(role: head.role, isSegment: !row.isHead, side: row.side, structure: head.structure,
                                         measurement: exercise.measurement, reachedFailure: values.reachedFailure == true,
                                         termination: head.termination)
        let copied = SuggestionInputs.target(row.isHead || row.side != nil ? head.target : row.target,
                                             termination: head.termination, config: config)
        let target = SuggestionInputs.clusterHead(copied, structure: head.structure, isHead: row.isHead)
        return SuggestionInputs.doneSet(kind: kind, values: values, side: row.side, config: config, target: target,
                                        completedAt: completedAt, ordinal: ordinal)
    }

    /// The set to suggest for `row` (03 §9.4), with the exercise settings
    /// its chain needs. `row` may be one the state does not hold yet: the
    /// next mini-set of a chain that has stopped, whose stop text is wanted.
    func pending(_ row: Row, exercise: Exercise) -> (PendingSet, ExerciseConfig)? {
        guard let head = row.isHead ? row : state.rows[row.headID] else { return nil }
        var config = config(exercise)
        let ordinal = Ordinals.ordinal(of: head.id, in: state) ?? 1
        let kind = SuggestionInputs.kind(role: head.role, isSegment: !row.isHead, side: row.side, structure: head.structure,
                                         measurement: exercise.measurement,
                                         reachedFailure: row.entered.reachedFailure == true, termination: row.termination)
        var target = SuggestionInputs.clusterHead(SuggestionInputs.target(row.target, termination: row.termination, config: config),
                                                  structure: head.structure, isHead: row.isHead)
        var parent: DoneSet?
        var miniSets: [Int] = []
        let chain = state.chain(of: head.id)
        switch kind {
        case .drop:
            // The planned step or percentage lives on the head (§4.9).
            let planned = SuggestionInputs.target(head.target, termination: head.termination, config: config)
            target.dropPercent = planned.dropPercent
            target.dropStep = planned.dropStep
            guard let link = chain.last(where: { $0.chainPosition < row.chainPosition }) else { return nil }
            parent = shownDoneSet(link, head: head, exercise: exercise, config: config, ordinal: ordinal)
        case .myoMini:
            parent = shownDoneSet(head, head: head, exercise: exercise, config: config, ordinal: ordinal)
            miniSets = chain.filter { !$0.isHead && $0.chainPosition < row.chainPosition }.compactMap { shown($0.id).reps }
            let headReps = parent?.reps ?? 0
            let planned = head.target
            target.repsMin = nil
            target.repsMax = nil
            switch head.structure {
            case .myo:
                config.myoMaxMiniSets = planned?.miniSets ?? exercise.resolvedMyoMaxMiniSets
                config.myoStopRule = state.plannedSet(ofRow: head.id)?.myoStopRule ?? exercise.resolvedMyoStopRule
                target.myoMiniReps = planned?.miniSetReps
                target.myoTotalReps = planned?.totalReps
            case .myomatch:
                // The engine's rule (§4.9): the total or the cap, never a rep
                // drop; each mini-set aims at min(miniSetReps ?? t1, T − done).
                config.myoMaxMiniSets = planned?.miniSets ?? exercise.resolvedMyoMaxMiniSets
                config.myoStopRule = nil
                target.myoMiniReps = planned?.miniSetReps ?? min(max((headReps + 2) / 4, 3), 5)
                target.myoTotalReps = state.matchTotal(of: head.id).map { $0 - headReps }
            case .restpause:
                // A rest-pause total counts the first bout (§4.9).
                config.myoMaxMiniSets = planned?.miniSets ?? 2
                config.myoStopRule = nil
                target.myoMiniReps = planned?.miniSetReps
                target.myoTotalReps = planned?.totalReps.map { $0 - headReps }
            case .cluster:
                config.myoMaxMiniSets = max(chain.count - 1, row.chainPosition - 1)
                config.myoStopRule = nil
                target.myoMiniReps = planned?.miniSetReps ?? row.target?.repsMin
                target.myoTotalReps = nil
            case .straight, .drop:
                break
            }
        default:
            if head.roleLabel == .backoff, target.dropPercent != nil || target.dropStep != nil,
               let source = backoffParent(of: head)
            {
                parent = shownDoneSet(source, head: source, exercise: exercise, config: config,
                                      ordinal: Ordinals.ordinal(of: source.id, in: state))
            }
        }
        let pending = PendingSet(kind: kind, ordinal: ordinal, target: target, parent: parent, miniSetsDone: miniSets)
        return (pending, config)
    }

    /// The working set a back-off set backs off from (§5.3): the slot's last
    /// completed top set before it, else its last completed working set
    /// that is not itself a back-off.
    func backoffParent(of head: Row) -> Row? {
        let units = state.units(inSlot: head.slot)
        guard let mine = units.firstIndex(where: { $0.first == head.id }) else { return nil }
        let earlier = units[..<mine].compactMap { state.rows[$0[0]] }
            .filter { $0.role == .working && $0.isCompleted && $0.roleLabel != .backoff }
        return earlier.last { $0.roleLabel == .top } ?? earlier.last
    }

    /// A link as the set it is or will be: its performed values, else the
    /// values it shows.
    func shownDoneSet(_ row: Row, head: Row, exercise: Exercise, config: ExerciseConfig, ordinal: Int?) -> DoneSet {
        doneSet(row, head: head, values: shown(row.id), completedAt: row.performed?.completedAt ?? now,
                exercise: exercise, config: config, ordinal: ordinal)
    }

    // MARK: - The suggestion

    /// The suggestion for a pending row; nil for a completed one.
    func suggestion(_ id: RowID) -> Suggestion? {
        if let cached = suggestions[id] { return cached }
        let result = state.rows[id].flatMap { $0.isPending ? computeSuggestion($0) : nil }
        suggestions[id] = .some(result)
        return result
    }

    private func computeSuggestion(_ row: Row) -> Suggestion? {
        guard let exercise = exercise(ofRow: row), let head = state.rows[row.headID] else { return nil }
        if row.side != nil, !row.isHead {
            // The second side (§4.9): the first side's suggestion as it stood
            // before the pair began, at the load the first side shows; with
            // `sideMatch`, the first side's result, which the engine copied
            // into this row's target.
            guard var suggestion = runSuggestion(for: head, exercise: exercise, excludingChain: head.id) else { return nil }
            let first = shown(head.id)
            let unit = config(exercise).unit
            let firstLoad: Mass? = switch config(exercise).mode {
            case .external, .bodyweightAdded: first.load
            case .bodyweightAssisted: first.assistance
            case .none: nil
            }
            if let firstLoad { suggestion.load = firstLoad.converted(to: unit).value.double }
            if let reps = row.target?.repsMin { suggestion.reps = reps }
            if let duration = row.target?.durationMin { suggestion.duration = duration }
            return suggestion
        }
        return runSuggestion(for: row, exercise: exercise, excludingChain: nil)
    }

    /// Runs `suggest` for a row (pending, or one about to be generated).
    func runSuggestion(for row: Row, exercise: Exercise, excludingChain excluded: RowID?) -> Suggestion? {
        guard let built = pending(row, exercise: exercise) else { return nil }
        let sets: [DoneSet] = switch built.0.kind {
        case .drop, .myoMini, .warmup: []
        default: today(exercise, excludingChain: excluded)
        }
        return suggestNext(pending: built.0, exercise: built.1, today: sets, history: past(exercise),
                           bodyMass: bodyMass, now: now, calendar: calendar)
    }

    /// The stop text of a myo, myo-match or rest-pause chain that has ended
    /// (§5.6: "Stop: mini-set 4 got 3, first was 4"): what the engine says
    /// of the mini-set that will not come. Nil while the chain runs.
    func stopText(ofChain head: RowID) -> String? {
        guard let headRow = state.rows[head], headRow.structure.growsOneAtATime, headRow.side == nil,
              state.isChainComplete(head), state.chainStops(head), let exercise = exercise(ofRow: headRow)
        else { return nil }
        let next = SessionEngine.segment(of: headRow, position: state.chain(of: head).count + 1,
                                         id: RowID("\(head.rawValue)-stop"), reps: headRow.target?.miniSetReps)
        guard let suggestion = runSuggestion(for: next, exercise: exercise, excludingChain: nil), suggestion.stop else {
            return nil
        }
        return suggestion.text
    }

    // MARK: - PREVIOUS and ghosts

    func previous(_ id: RowID) -> PreviousSet? {
        if let cached = previouses[id] { return cached }
        guard let exercise = state.rows[id].flatMap(exercise(ofRow:)) else { return nil }
        let last: ExerciseHistory?
        if let cached = lastTimes[exercise.id] {
            last = cached
        } else {
            last = history.last(exercise.id, setup: nil, excluding: state.session.id)
            lastTimes[exercise.id] = .some(last)
        }
        let result = Previous.find(for: id, in: state, last: last)
        previouses[id] = .some(result)
        return result
    }

    /// §5.5's ghost values of a pending row; empty for a completed one.
    func ghost(_ id: RowID) -> GhostValues {
        if let cached = ghosts[id] { return cached }
        var ghost = GhostValues()
        if let row = state.rows[id], row.isPending, let exercise = exercise(ofRow: row) {
            let config = config(exercise)
            let fields = Set(exercise.measurement.valueColumns.compactMap(\.field))
            let suggestion = suggestion(id).flatMap { $0.stop ? nil : $0 }
            let target = row.target
            let previous = previous(id)?.values
            let suggestedLoad = suggestion?.load.map { Mass(suggested: $0, unit: config.unit) }
            if fields.contains(.load) {
                ghost.load = suggestedLoad ?? target?.load ?? previous?.load
            }
            if fields.contains(.assistance) {
                ghost.assistance = suggestedLoad ?? target?.assistance ?? previous?.assistance
            }
            if fields.contains(.reps) {
                if let suggestion, suggestion.reps == nil, suggestion.reason == .drop || suggestion.reason == .myoMini {
                    // The suggestion says "to failure"; a number from PREVIOUS would contradict it.
                    ghost.repsPlaceholder = GhostValues.toFailure
                } else {
                    ghost.reps = suggestion?.reps ?? target?.repsMin ?? previous?.reps
                    ghost.repsIsHint = ghost.reps != nil && row.termination == .rirstop
                }
            }
            if fields.contains(.duration) {
                ghost.duration = suggestion?.duration ?? target?.durationMin ?? previous?.duration
            }
            if fields.contains(.distance) {
                ghost.distance = target?.distance ?? previous?.distance
            }
        }
        ghosts[id] = ghost
        return ghost
    }

    /// What a row shows: its performed values, else what is typed with the
    /// ghost values in the gaps (a hint included).
    func shown(_ id: RowID) -> SetValues {
        guard let row = state.rows[id] else { return SetValues() }
        if let performed = row.performed { return performed.values }
        return filled(row.entered, ghost(id), hints: true)
    }

    /// What a tick of the row commits (§5.5): what it shows, typed or ghost,
    /// except a `rirstop` hint and a "to failure" placeholder.
    func commit(_ id: RowID) -> SetValues {
        guard let row = state.rows[id] else { return SetValues() }
        if let performed = row.performed { return performed.values }
        return filled(row.entered, ghost(id), hints: false)
    }

    private func filled(_ entered: SetValues, _ ghost: GhostValues, hints: Bool) -> SetValues {
        var values = entered
        values.load = values.load ?? ghost.load
        values.assistance = values.assistance ?? ghost.assistance
        values.reps = values.reps ?? (ghost.repsIsHint && !hints ? nil : ghost.reps)
        values.duration = values.duration ?? ghost.duration
        values.distance = values.distance ?? ghost.distance
        return values
    }
}

/// The suggestion engine's entry point under a name no member shadows.
private func suggestNext(pending: PendingSet, exercise: ExerciseConfig, today: [DoneSet], history: [PastSession],
                         bodyMass: Double?, now: Date, calendar: Calendar) -> Suggestion?
{
    suggest(pending: pending, exercise: exercise, today: today, history: history, bodyMass: bodyMass, now: now,
            calendar: calendar)
}
