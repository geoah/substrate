import Foundation

/// Builds a session's presentation (§2.2 step 5): pure, re-run after every
/// change. Suggestions are computed here and never written into the state;
/// the tick commits what a row shows (§5.5).
public enum SessionPresenter {
    /// How long "Rest over" stays in the rest bar before the Up next chip (§7.6).
    public static let restOverSeconds: Seconds = 10

    /// - Parameters:
    ///   - catalog: names for exercises the state's snapshot lacks; optional.
    ///   - history: every finished session; the active one is left out.
    ///   - now: the instant the clock, the countdowns and the gaps to past sessions are read at.
    public static func present(_ state: SessionState, catalog: Catalog? = nil, history: HistoryIndex,
                               settings: PresenterSettings, now: Date) -> SessionPresentation
    {
        Presenting(state: state, catalog: catalog, history: history, settings: settings, now: now).presentation()
    }

    /// The Live Activity's last words after Finish: `Saved: 46 min, 24 sets`.
    public static func finishedSummary(_ records: SessionRecords) -> String {
        let session = records.session
        let seconds = session.endsAt.map { activeSeconds(from: session.at, to: $0, pauses: session.pauses) } ?? 0
        return "Saved: \(Format.minutes(seconds)), \(Format.count(Counting.workingSets(records.sets), "set"))"
    }

    /// Seconds from `start` to `end` with the pauses inside them left out; an
    /// open pause runs to `end`.
    public static func activeSeconds(from start: Date, to end: Date, pauses: [Pause]) -> Seconds {
        let paused = pauses.reduce(0.0) { total, pause in
            let from = max(pause.start, start)
            let to = min(pause.end ?? end, end)
            return total + max(0, to.timeIntervalSince(from))
        }
        return max(0, Seconds((end.timeIntervalSince(start) - paused).rounded()))
    }
}

/// One presentation's work: the state, the evaluator's memo, and the pieces.
/// One presentation's work: the state, the memoized evaluator and the ids
/// every part reads. The Rows layout extends it (Present/Rows/).
struct Presenting {
    let state: SessionState
    let catalog: Catalog?
    let settings: PresenterSettings
    let now: Date
    let evaluator: SessionEvaluator
    let upNextID: RowID?
    let timedID: RowID?
    let groupColors: [String: Int]

    init(state: SessionState, catalog: Catalog?, history: HistoryIndex, settings: PresenterSettings, now: Date) {
        self.state = state
        self.catalog = catalog
        self.settings = settings
        self.now = now
        evaluator = SessionEvaluator(state: state, history: history, now: now, calendar: settings.calendar)
        upNextID = state.upNext
        timedID = state.timedRow
        var colors: [String: Int] = [:]
        for slot in state.slots {
            guard let group = slot.group, colors[group] == nil else { continue }
            colors[group] = colors.count % GroupHeader.paletteSize
        }
        groupColors = colors
    }

    var title: String { state.plan?.name ?? "Training session" }

    /// Whether committing `values` on the row passes §5.7's entry check.
    func passesEntryCheck(_ id: RowID, _ values: SetValues?) -> Bool {
        guard let values else { return false }
        return EntryCheck.warning(for: id, values: values, in: state, history: evaluator.history, now: now,
                                  calendar: settings.calendar) == nil
    }

    func presentation() -> SessionPresentation {
        let header = headerStats()
        let upNext = upNextPresentation()
        let blocks = self.blocks()
        let rowsLayout = self.rowsLayout(blocks: blocks, header: header)
        let completed = state.rows.values.count { $0.isHead && $0.isCompleted }
        let started = Format.time(state.session.at, in: settings.timeZone)
        // The Live Activity's Done ticks Up next with its shown values only;
        // its Stop stops the timed set only when nothing but the time is
        // missing. No dialog can ask from the Lock Screen, so a set the entry
        // check would question offers neither: the owner answers in the app.
        let rows = blocks.lazy.flatMap(\.cards).flatMap(\.rows)
        let canComplete = if let timed = timedID, let row = rows.first(where: { $0.id == timed }) {
            row.canTick && passesEntryCheck(timed, row.stopValues)
        } else if timedID == nil, let next = upNext, let row = rows.first(where: { $0.id == next.row }) {
            passesEntryCheck(next.row, row.commitValues)
        } else {
            false
        }
        return SessionPresentation(
            sessionID: state.session.id,
            title: title,
            isFinished: state.phase == .finished,
            header: header,
            blocks: blocks,
            upNext: upNext,
            restBar: restBar(upNext),
            liveActivity: liveActivity(header, upNext, canComplete: canComplete),
            notification: notification(upNext),
            accessory: AccessoryPresentation(
                title: title,
                detail: upNext.map { "\($0.exerciseWithGroup), \($0.label.spoken)" },
                isPaused: header.isPaused,
                restEndsAt: state.countdown.flatMap { $0.isRestLike && !$0.hasEnded(by: now) ? $0.endsAt : nil }
            ),
            unticked: unticked(),
            completedSets: completed,
            summary: "\(title), started \(started), \(Format.count(completed, "set")) done.",
            timedRow: timedID,
            nextChange: nextChange(),
            rowsLayout: rowsLayout
        )
    }

    // MARK: - Header

    func headerStats() -> SessionHeaderStats {
        let pauses = state.session.pauses
        let closed = pauses.compactMap { pause in pause.end.map { $0.timeIntervalSince(pause.start) } }.reduce(0, +)
        let clockStart = state.session.at.addingTimeInterval(closed)
        let pausedAt = pauses.last.flatMap { $0.end == nil ? $0.start : nil }
        let elapsed = max(0, Seconds(((pausedAt ?? now).timeIntervalSince(clockStart)).rounded(.down)))
        let volume = Counting.volume(state)
        let done = Counting.workingSetsDone(state)
        let planned = Counting.workingSetsPlanned(state)
        return SessionHeaderStats(
            clockStart: clockStart,
            clockPausedAt: pausedAt,
            elapsed: elapsed,
            durationText: pausedAt == nil ? Format.duration(elapsed) : "Paused",
            isPaused: pausedAt != nil,
            volume: volume,
            volumeText: Format.volume(volume),
            setsDone: done,
            setsPlanned: planned,
            setsText: "\(done) of \(planned)"
        )
    }

    // MARK: - Blocks and cards

    func blocks() -> [ExerciseBlock] {
        var blocks: [ExerciseBlock] = []
        var placed: Set<String> = []
        for slot in state.slots {
            guard let key = slot.group else {
                blocks.append(ExerciseBlock(id: slot.key, group: nil, cards: [card(slot)]))
                continue
            }
            guard !placed.contains(key) else { continue }
            placed.insert(key)
            let members = state.members(ofGroup: key)
            blocks.append(ExerciseBlock(id: "group-\(key)", group: groupHeader(key, members: members),
                                        cards: members.map(card)))
        }
        return blocks
    }

    func groupHeader(_ key: String, members: [SlotState]) -> GroupHeader? {
        guard let group = state.group(key) else { return nil }
        let rounds = members.map { member in
            state.units(inSlot: member.key).count { state.rows[$0[0]]?.role == .working }
        }.max() ?? 0
        let rest = RestRules.roundRest(group: key, in: state).seconds
        let restText = rest > 0 ? "\(Format.duration(rest)) after each round" : "no rest between rounds"
        return GroupHeader(
            key: key,
            kind: group.groupKind,
            pairing: group.pairing ?? state.routineGroup(of: key)?.pairing,
            rounds: rounds,
            roundRest: rest,
            title: "\(group.groupKind.label) \(key.uppercased()): \(Format.count(rounds, "round")), \(restText)",
            colorIndex: groupColors[key] ?? 0,
            members: members.map(\.key)
        )
    }

    /// `A1`: the group key and the member's place.
    func memberLabel(_ slot: SlotState) -> String? {
        guard let key = slot.group, let index = state.members(ofGroup: key).firstIndex(where: { $0.key == slot.key }) else {
            return nil
        }
        return groupMemberLabel(group: key, position: index + 1)
    }

    func exerciseName(_ id: ExerciseID) -> String {
        state.exercises[id]?.name ?? catalog?.exercises[id]?.name ?? id.rawValue
    }

    func card(_ slot: SlotState) -> ExerciseCard {
        let exercise = state.exercises[slot.exercise]
        let measurement = exercise?.measurement ?? .weightreps
        let config = exercise.map(evaluator.config)
        let unit = config?.unit ?? state.settings.defaultUnit
        let columns = measurement.gridColumns
        let heads = state.orderedRows(inSlot: slot.key).filter(\.isHead)
        let working = heads.filter { $0.role == .working }
        let done = working.count(where: \.isCompleted)
        let loadStep: Mass? = config.flatMap { config in
            guard config.mode != .none, config.canAddLoad, let step = config.step else { return nil }
            return Mass(suggested: step, unit: config.unit)
        }
        return ExerciseCard(
            slot: slot.key,
            exercise: slot.exercise,
            name: exerciseName(slot.exercise),
            measurement: measurement,
            unit: unit,
            isLocal: exercise?.isLocal ?? false,
            group: slot.group,
            groupLabel: memberLabel(slot),
            colorIndex: slot.group.flatMap { groupColors[$0] },
            restChip: restChip(slot),
            rests: restOptions(slot, exercise: exercise),
            notes: slot.notes,
            planNotes: state.routineSlot(of: slot.key)?.notes,
            columns: columns,
            columnTitles: columns.map { $0.title(unit: unit) },
            rows: state.orderedRows(inSlot: slot.key).map {
                row($0, measurement: measurement, unit: unit, exercise: exerciseName(slot.exercise))
            },
            loadStep: loadStep,
            timeStep: exercise?.resolvedTimeStep ?? 5,
            workingDone: done,
            workingPlanned: working.count,
            progress: "\(done) of \(working.count) done",
            completedSets: heads.count(where: \.isCompleted),
            hasWarmups: heads.contains { $0.role == .warmup }
        )
    }

    func restChip(_ slot: SlotState) -> String {
        guard let key = slot.group else { return "Rest \(Format.rest(RestRules.setRest(slot: slot.key, in: state).seconds))" }
        let members = state.members(ofGroup: key)
        guard let index = members.firstIndex(where: { $0.key == slot.key }) else { return "Rest" }
        if index + 1 < members.count {
            let transition = RestRules.transition(slot: slot.key, in: state).seconds
            return "Then \(Format.duration(transition)) to \(groupMemberLabel(group: key, position: index + 2))"
        }
        return "Round rest \(Format.rest(RestRules.roundRest(group: key, in: state).seconds))"
    }

    func restOptions(_ slot: SlotState, exercise: Exercise?) -> [SlotRestOption] {
        var kinds: [SlotRestKind] = [.setRest]
        if state.orderedRows(inSlot: slot.key).contains(where: { $0.role == .warmup }) { kinds.append(.warmupRest) }
        if let key = slot.group, state.members(ofGroup: key).last?.key != slot.key { kinds.append(.transition) }
        if exercise?.resolvedLaterality == .unilateral { kinds.append(.sideSwitch) }
        guard let index = state.slotIndex(slot.key) else { return [] }
        return kinds.map { kind in
            let current = restValue(kind, slot: slot.key, in: state)
            var cleared = state
            cleared.slots[index].rests[kind] = nil
            let fallback = restValue(kind, slot: slot.key, in: cleared)
            return SlotRestOption(kind: kind, seconds: current, defaultSeconds: fallback, isSetInGym: slot.rests[kind] != nil,
                                  text: Format.rest(current), defaultText: "Default (\(Format.rest(fallback)))")
        }
    }

    func restValue(_ kind: SlotRestKind, slot key: String, in state: SessionState) -> Seconds {
        switch kind {
        case .setRest: RestRules.setRest(slot: key, in: state).seconds
        case .warmupRest: RestRules.warmupRest(slot: key, in: state).seconds
        case .transition: RestRules.transition(slot: key, in: state).seconds
        case .sideSwitch: RestRules.sideSwitch(slot: key, in: state).seconds
        }
    }

    // MARK: - Rows

    /// - Parameter exercise: the exercise's name, for VoiceOver's labels.
    func row(_ row: Row, measurement: MeasurementType, unit: MassUnit, exercise: String) -> SetRowPresentation {
        let id = row.id
        let head = state.rows[row.headID] ?? row
        let locked = state.isLocked(id)
        let rowState: RowState = if row.isCompleted {
            .done
        } else if timedID == id {
            .timing
        } else if locked {
            .locked
        } else if upNextID == id {
            .upNext
        } else {
            .pending
        }
        let ghost = evaluator.ghost(id)
        let commit = committed(id, evaluator.commit(id))
        let missing = row.isPending ? measurement.missing(in: commit) : []
        let suggestion = evaluator.suggestion(id)
        var stopValues: SetValues?
        var stopMissing: [ValueField] = []
        if timedID == id {
            var values = commit
            values.duration = row.entered.duration
            stopValues = values
            stopMissing = measurement.missing(in: values).filter { $0 != .duration }
        }
        let otherTimer = timedID != nil && timedID != id
        let tickMissing = timedID == id ? stopMissing : missing
        let values = row.performed?.values ?? row.entered
        let isLast = state.chain(of: row.headID).last?.id == id
        let label = SetLabel.make(id, in: state) ?? SetLabel(short: "?", spoken: "set")
        let spoken = "\(exercise), \(label.spoken)"
        // A grid with no RPE column (timed and distance sets) still takes
        // one: from the row's menu, shown under the row.
        let rpeOutsideGrid = !measurement.hasRPEColumn
        return SetRowPresentation(
            id: id,
            slot: row.slot,
            label: label,
            state: rowState,
            kind: row.isHead ? currentKind(head) : nil,
            allowedKinds: allowedKinds(row),
            previous: evaluator.previous(id),
            cells: measurement.valueColumns.map {
                cell($0, row: row, ghost: ghost, missing: missing, unit: unit, spoken: spoken)
            },
            rpe: values.rpe,
            rpeText: values.rpe.map(Format.rpe),
            rpeEntry: state.rpe(ofRow: id),
            effortText: rpeOutsideGrid ? values.rpe.map { "RPE \(Format.rpe($0))" } : nil,
            canRateEffort: rpeOutsideGrid && !locked,
            rpeAccessibilityLabel: "\(spoken), RPE",
            timer: measurement.hasTimer ? timer(row, measurement: measurement) : nil,
            suggestion: suggestion,
            reason: upNextID == id && suggestion?.stop != true ? suggestion?.text : nil,
            stopText: row.isCompleted && isLast ? evaluator.stopText(ofChain: row.headID) : nil,
            commitValues: row.isPending && missing.isEmpty ? commit : nil,
            stopValues: stopValues,
            missing: missing,
            canTick: row.isPending && !locked && !otherTimer && tickMissing.isEmpty,
            restOverride: head.restOverride,
            notes: row.notes,
            isSegment: !row.isHead
        )
    }

    func currentKind(_ head: Row) -> SetKind {
        if head.role == .warmup { return .warmup }
        switch head.structure {
        case .drop: return .drop
        case .myo: return .myo
        case .myomatch: return .myomatch
        case .straight, .restpause, .cluster: return head.termination == .failure ? .failure : .working
        }
    }

    /// The kinds `changeSetKind` accepts (§4.3): none on a segment; on a head
    /// warm-up, working and failure, and a structure while the exercise is
    /// done on both sides at once and no segment of the chain is done.
    func allowedKinds(_ row: Row) -> [SetKind] {
        guard row.isHead else { return [] }
        var kinds: [SetKind] = [.warmup, .working, .failure]
        let unilateral = state.exercise(ofRow: row.id)?.resolvedLaterality == .unilateral
        let segmentDone = state.chain(of: row.id).contains { !$0.isHead && $0.isCompleted && $0.side == nil }
        if !unilateral, !segmentDone { kinds += [.drop, .myo, .myomatch] }
        return kinds
    }

    /// - Parameter spoken: the exercise and the set, `Leg Press (Machine), set 1`, for VoiceOver.
    func cell(_ column: GridColumn, row: Row, ghost: GhostValues, missing: [ValueField], unit: MassUnit,
              spoken: String) -> GridCell
    {
        let field = column.field ?? .reps
        let isMissing = missing.contains(field)
        let label = "\(spoken), \(column.spokenName(unit: unit))"
        if let performed = row.performed {
            return GridCell(column: column, field: field, text: text(field, performed.values, unit: unit), isGhost: false,
                            isHint: false, isMissing: false, entered: nil, accessibilityLabel: label)
        }
        if let typed = text(field, row.entered, unit: unit) {
            return GridCell(column: column, field: field, text: typed, isGhost: false, isHint: false, isMissing: false,
                            entered: editText(field, row.entered), accessibilityLabel: label)
        }
        var ghostValues = SetValues(load: ghost.load, assistance: ghost.assistance, reps: ghost.reps, duration: ghost.duration,
                                    distance: ghost.distance)
        if field == .reps, let placeholder = ghost.repsPlaceholder {
            ghostValues.reps = nil
            return GridCell(column: column, field: field, text: placeholder, isGhost: true, isHint: true, isMissing: isMissing,
                            entered: nil, accessibilityLabel: label)
        }
        let hint = field == .reps && ghost.repsIsHint
        let shown = text(field, ghostValues, unit: unit).map { hint ? "\u{2248}\($0)" : $0 }
        return GridCell(column: column, field: field, text: shown, isGhost: shown != nil, isHint: hint, isMissing: isMissing,
                        entered: nil, accessibilityLabel: label)
    }

    /// A cell's text: a load's number (with its unit when it differs from the
    /// column's), reps, `1:07`, metres.
    func text(_ field: ValueField, _ values: SetValues, unit: MassUnit) -> String? {
        func load(_ mass: Mass?) -> String? {
            mass.map { $0.unit == unit ? Format.number($0.value) : Format.mass($0) }
        }
        return switch field {
        case .load: load(values.load)
        case .assistance: load(values.assistance)
        case .reps: values.reps.map(String.init)
        case .duration: values.duration.map(Format.duration)
        case .distance: values.distance.map { Format.number($0.metres) }
        }
    }

    /// A typed value as its field edits it: a load's bare number, `1:07`.
    func editText(_ field: ValueField, _ values: SetValues) -> String? {
        switch field {
        case .load: values.load.map { Format.number($0.value) }
        case .assistance: values.assistance.map { Format.number($0.value) }
        case .reps: values.reps.map(String.init)
        case .duration: values.duration.map(Format.duration)
        case .distance: values.distance.map { Format.number($0.metres) }
        }
    }

    func timer(_ row: Row, measurement: MeasurementType) -> TimedSetState {
        if let performed = row.performed { return .done(duration: performed.values.duration) }
        if timedID == row.id, let countdown = state.countdown {
            return .running(startedAt: countdown.startedAt, endsAt: countdown.endsAt)
        }
        // A hold counts down to the time it shows; distance work has no time
        // to aim at, so it runs a stopwatch (the spec is silent).
        guard measurement.isTimedHold, row.timerMode != .stopwatch else { return .ready(target: nil) }
        return .ready(target: evaluator.shown(row.id).duration)
    }

    /// What a pending row commits: its shown values, with its RPE pre-fill
    /// accepted on a screen that shows one (§4.14).
    func committed(_ id: RowID, _ values: SetValues) -> SetValues {
        guard settings.prefillsRPE, state.rows[id]?.isPending == true else { return values }
        return state.acceptingRPEPrefill(values, row: id)
    }

    func unticked() -> [UntickedRow] {
        state.slots.flatMap { slot in
            state.orderedRows(inSlot: slot.key).filter(\.isPending).map { row in
                let values = committed(row.id, evaluator.commit(row.id))
                let complete = state.exercise(ofRow: row.id).map { $0.measurement.missing(in: values).isEmpty } ?? false
                return UntickedRow(row: row.id, values: complete ? values : nil)
            }
        }
    }

    // MARK: - Up next

    func upNextPresentation() -> UpNextPresentation? {
        guard let id = upNextID else { return nil }
        return describe(id)
    }

    /// A row as Up next describes it.
    func describe(_ id: RowID) -> UpNextPresentation? {
        guard let row = state.rows[id], let slot = state.slot(row.slot), let label = SetLabel.make(id, in: state) else {
            return nil
        }
        let name = exerciseName(slot.exercise)
        let groupLabel = memberLabel(slot)
        let values = valuesText(id)
        let described = groupLabel.map { "\(name) (\($0))" } ?? name
        let suggestion = evaluator.suggestion(id)
        return UpNextPresentation(
            row: id,
            slot: row.slot,
            exercise: name,
            groupLabel: groupLabel,
            label: label,
            round: slot.group == nil ? nil : UpNext.round(of: id, in: state),
            values: values,
            line: "Next: \(described), \(label.spoken)" + (values.isEmpty ? "" : ": \(values)"),
            reason: suggestion?.stop == true ? nil : suggestion?.text
        )
    }

    /// The values a row shows, as a set: `40 kg × 11`, `× ≈15`, `25 kg to failure`.
    func valuesText(_ id: RowID) -> String {
        guard let row = state.rows[id], let exercise = state.exercise(ofRow: id) else { return "" }
        let shown = evaluator.shown(id)
        let ghost = evaluator.ghost(id)
        let hint = row.isPending && row.entered.reps == nil && ghost.repsIsHint
        var text = Format.set(shown, measurement: exercise.measurement, repsHint: hint)
        if row.isPending, row.entered.reps == nil, let placeholder = ghost.repsPlaceholder {
            text += text.isEmpty ? placeholder : " \(placeholder)"
        }
        return text
    }

    /// `Glute Bridge (A1), set 2`, or with `byRound` in a group `Bird Dog (A2), round 1`.
    func target(_ next: UpNextPresentation, byRound: Bool) -> String {
        if byRound, let round = next.round, round > 0 { return "\(next.exerciseWithGroup), round \(round)" }
        return "\(next.exerciseWithGroup), \(next.label.spoken)"
    }

    // MARK: - The rest bar

    func restBar(_ next: UpNextPresentation?) -> RestBarPresentation {
        if let timed = timedID, let countdown = state.countdown, let row = describe(timed) {
            return .timedSet(TimedSetBar(row: timed, label: "\(row.exercise), \(row.label.spoken)",
                                         startedAt: countdown.startedAt, endsAt: countdown.endsAt))
        }
        if let countdown = state.countdown, countdown.isRestLike, let end = countdown.endsAt {
            if end > now {
                return .resting(RestCountdownPresentation(
                    label: countdownLabel(countdown, next: next, long: true),
                    slot: restSlot(countdown),
                    startedAt: countdown.startedAt,
                    endsAt: end,
                    next: next?.line,
                    notifies: countdown.notifies
                ))
            }
            if now < end.addingTimeInterval(TimeInterval(SessionPresenter.restOverSeconds)) {
                guard let next else { return state.rows.isEmpty ? .empty : .allDone }
                // The exercise is named when the next set is another exercise's.
                let lastSlot = state.lastCompleted.flatMap { state.rows[$0]?.slot }
                let what = next.slot == lastSlot ? next.label.spoken : target(next, byRound: false)
                return .restOver("Rest over. Go: \(what)" + (next.values.isEmpty ? "" : ", \(next.values)"))
            }
        }
        // A session with no rows yet is not done: it asks for an exercise.
        guard let next else { return state.rows.isEmpty ? .empty : .allDone }
        if state.countdown == nil, let clock = state.restClock, clock.endedAt == nil, clock.planned == 0, clock.slot != nil {
            return .go("Go: \(target(next, byRound: clock.slot == .transition))")
        }
        return .upNext(next.line)
    }

    func restSlot(_ countdown: Countdown) -> RestSlot? {
        if case let .rest(slot) = countdown.kind { return slot }
        return nil
    }

    /// The pill's words. A transition names where to go: `Go: Bird Dog (A2) in`
    /// in the bar (`long`), `Go: Bird Dog (A2)` on the Lock Screen.
    func countdownLabel(_ countdown: Countdown, next: UpNextPresentation?, long: Bool) -> String {
        switch countdown.kind {
        case .manual: return "Timer"
        case let .timedSet(row): return describe(row).map { "\($0.exercise), \($0.label.spoken)" } ?? "Timed set"
        case let .rest(slot):
            switch slot {
            case .transition:
                guard let next else { return slot.label }
                return "Go: \(next.exerciseWithGroup)" + (long ? " in" : "")
            case .blockRest where !long: return "Rest"
            default: return slot.label
            }
        }
    }

    // MARK: - The Live Activity and the notification

    func liveActivity(_ header: SessionHeaderStats, _ upNext: UpNextPresentation?, canComplete: Bool) -> LiveActivityContent {
        // While a timed set runs, the activity describes it: Stop stops that row.
        let subject = timedID.flatMap(describe) ?? upNext
        let next = subject.map { $0.label.spoken.capitalizedFirst + ($0.values.isEmpty ? "" : ": \($0.values)") }
        return LiveActivityContent(
            title: title,
            clockStart: header.clockStart,
            clockPausedAt: header.clockPausedAt,
            exercise: subject?.exercise ?? "",
            groupLabel: subject?.groupLabel,
            next: next ?? (state.rows.isEmpty ? "No exercises yet" : "All sets done"),
            nextRowID: subject?.row,
            canComplete: canComplete,
            countdown: state.countdown.map { countdown in
                LiveActivityCountdown(kind: activityKind(countdown), label: countdownLabel(countdown, next: upNext, long: false),
                                      startedAt: countdown.startedAt, endsAt: countdown.endsAt)
            },
            setsDone: header.setsDone,
            setsPlanned: header.setsPlanned
        )
    }

    func activityKind(_ countdown: Countdown) -> LiveActivityCountdown.Kind {
        switch countdown.kind {
        case .manual: .manual
        case .timedSet: .timedSet
        case let .rest(slot):
            switch slot {
            case .override, .setRest, .warmupRest: .rest
            case .roundRest: .roundRest
            case .blockRest: .blockRest
            case .transition: .transition
            case .intraSetPause: .pause
            case .sideSwitch: .sideSwitch
            }
        }
    }

    /// §9.2's copy, for a countdown that notifies.
    func notification(_ next: UpNextPresentation?) -> CountdownNotification? {
        guard let countdown = state.countdown, let end = countdown.endsAt, countdown.notifies else { return nil }
        func body(_ prefix: String, byRound: Bool) -> String {
            guard let next else { return "All sets done" }
            return "\(prefix): \(target(next, byRound: byRound))" + (next.values.isEmpty ? "" : ": \(next.values)")
        }
        switch countdown.kind {
        case .manual:
            return CountdownNotification(title: "Timer done", body: nil, fireDate: end)
        case let .timedSet(row):
            let label = describe(row).map { "\($0.exercise), \($0.label.spoken)" } ?? "Timed set"
            let target = Seconds(end.timeIntervalSince(countdown.startedAt).rounded())
            return CountdownNotification(title: "Time", body: "\(label): \(Format.duration(target)) reached", fireDate: end)
        case let .rest(slot):
            switch slot {
            case .override, .setRest, .warmupRest:
                return CountdownNotification(title: "Rest over", body: body("Next", byRound: false), fireDate: end)
            case .roundRest:
                // A warm-up has round 0; its round rest is a plain "Rest over".
                let round = next?.round.flatMap { $0 > 0 ? "Round \($0)" : nil } ?? "Rest over"
                return CountdownNotification(title: round, body: body("Next", byRound: false), fireDate: end)
            case .blockRest:
                return CountdownNotification(title: "Rest over", body: body("Next exercise", byRound: false), fireDate: end)
            case .transition:
                return CountdownNotification(title: "Go", body: body("Next", byRound: true), fireDate: end)
            case .intraSetPause, .sideSwitch:
                return CountdownNotification(title: "Go", body: body("Next", byRound: false), fireDate: end)
            }
        }
    }

    func nextChange() -> Date? {
        guard let countdown = state.countdown, let end = countdown.endsAt else { return nil }
        let instants = [end, end.addingTimeInterval(TimeInterval(SessionPresenter.restOverSeconds))]
        return instants.first { $0 > now }
    }
}
