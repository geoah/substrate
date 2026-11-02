import Foundation

// Builds `RowsLayout` inside the session presenter (§2.2 step 5), from the
// same state, evaluator and row presentations the Table layout reads: the
// suggestion, the ghost values, PREVIOUS, the rest rules, Up next and the
// chains are reused, never worked out again.
//
// Sets are logged in any order: every pending set opens with a tap, and the
// suggested next set is only the one open when nothing else is. Nothing
// counts up for a set that is not timed.

extension Presenting {
    func rowsLayout(blocks: [ExerciseBlock]) -> RowsLayout {
        RowsBuilder(self, blocks: blocks).layout()
    }
}

/// One Rows layout's work. Local to one presentation.
struct RowsBuilder {
    let p: Presenting
    let blocks: [ExerciseBlock]
    /// Every row's Table presentation: commit values, PREVIOUS, labels, the RPE entry.
    let views: [RowID: SetRowPresentation]
    let cards: [String: ExerciseCard]
    /// The open row: the timed row while it runs, else Up next.
    let openRow: RowID?

    init(_ presenting: Presenting, blocks: [ExerciseBlock]) {
        p = presenting
        self.blocks = blocks
        let cards = blocks.flatMap(\.cards)
        views = Dictionary(cards.flatMap(\.rows).map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        self.cards = Dictionary(cards.map { ($0.slot, $0) }, uniquingKeysWith: { first, _ in first })
        openRow = presenting.timedID ?? presenting.upNextID
    }

    var state: SessionState { p.state }
    var evaluator: SessionEvaluator { p.evaluator }

    func layout() -> RowsLayout {
        let built = blocks.map(block)
        let open = openRow.flatMap { row in built.lazy.compactMap { $0.item(row) }.first }
        return RowsLayout(blocks: built, island: island(open: open, blocks: built), openRow: openRow)
    }

    // MARK: - Blocks

    func block(_ block: ExerciseBlock) -> RowsBlock {
        let slots = block.cards.map(\.slot)
        var items: [RowsItem] = []
        let isGroup = block.group != nil
        if let group = block.group {
            let lanes = UpNext.lanes(ofGroup: group.key, in: state)
            items += warmupItems(lanes.flatMap { $0.filter(isWarmup) }, id: block.id, isGroup: true)
            let working = lanes.map { $0.filter { !isWarmup($0) } }
            let rounds = working.map(\.count).max() ?? 0
            for round in 0 ..< rounds {
                let members = working.compactMap { round < $0.count ? $0[round] : nil }
                let done = members.joined().allSatisfy { state.rows[$0]?.isCompleted == true }
                items.append(.round(RowsRoundHeader(id: "\(block.id)-round-\(round + 1)", round: round + 1, rounds: rounds,
                                                    title: "Round \(round + 1) of \(rounds)", isDone: done)))
                // No rest lines between members or rounds: rest shows in the island.
                for unit in members {
                    items += unitItems(unit, isGroup: true)
                }
            }
        } else if let slot = slots.first {
            let units = state.units(inSlot: slot)
            items += warmupItems(units.filter(isWarmup), id: block.id, isGroup: false)
            for unit in units where !isWarmup(unit) {
                items += unitItems(unit, isGroup: false)
            }
        }
        let rows = slots.flatMap(state.orderedRows(inSlot:))
        let isFinished = !rows.isEmpty && rows.allSatisfy(\.isCompleted)
        // No group letter: the members are named by their exercises.
        let title = block.group.map(\.kind.label) ?? block.cards.first?.name ?? ""
        return RowsBlock(
            id: block.id,
            slots: slots,
            group: block.group?.key,
            title: title,
            subtitle: isGroup ? groupSubtitle(block) : slots.first.flatMap(plainSubtitle),
            items: items,
            addSets: block.cards.map { card in
                RowsButton(title: isGroup ? "Add \(card.name) set" : "Add set",
                           accessibilityLabel: "Add a set to \(card.name)", action: .addSet(slot: card.slot, role: .working))
            },
            isFinished: isFinished,
            summary: isFinished ? summary(block, title: title) : nil
        )
    }

    func isWarmup(_ unit: [RowID]) -> Bool {
        unit.first.flatMap { state.rows[$0] }?.role == .warmup
    }

    /// `6–10 reps at RPE 8, rest 2:30, set 2 ends in a drop`: the first working
    /// set's target, the set rest, and each structured set.
    func plainSubtitle(_ slot: String) -> String? {
        let heads = state.orderedRows(inSlot: slot).filter { $0.isHead && $0.role == .working }
        var parts: [String] = []
        if let target = heads.lazy.compactMap(\.target).first {
            var aim: String?
            switch (target.repsMin, target.repsMax) {
            case let (min?, max?): aim = "\(Format.range(min, max)) reps"
            case let (min?, nil): aim = "\(min) or more reps"
            case let (nil, max?): aim = "up to \(max) reps"
            case (nil, nil):
                if let min = target.durationMin ?? target.durationMax {
                    aim = "\(Format.durationRange(min, target.durationMax ?? min)) holds"
                }
            }
            let effort = Format.effort(rpeMin: target.rpeMin, rpeMax: target.rpeMax, rirMin: target.rirMin,
                                       rirMax: target.rirMax)
            switch (aim, effort) {
            case let (aim?, effort?): parts.append("\(aim) at \(effort)")
            case let (aim?, nil): parts.append(aim)
            case let (nil, effort?): parts.append("each set to \(effort)")
            case (nil, nil): break
            }
        }
        let rest = RestRules.setRest(slot: slot, in: state).seconds
        parts.append(rest > 0 ? "rest \(Format.duration(rest))" : "no rest")
        for head in heads where head.structure != .straight && head.side == nil {
            guard let ordinal = state.ordinal(of: head.id) else { continue }
            let drops = state.chain(of: head.id).count - 1
            switch head.structure {
            case .drop: parts.append("set \(ordinal) ends in \(drops > 1 ? "\(drops) drops" : "a drop")")
            case .myo: parts.append("set \(ordinal) as myo-reps")
            case .myomatch: parts.append("set \(ordinal) as a myo-rep match")
            case .restpause: parts.append("set \(ordinal) as rest-pause")
            case .cluster: parts.append("set \(ordinal) in clusters")
            case .straight: break
            }
        }
        return parts.joined(separator: ", ").capitalizedFirst
    }

    /// `Glute Bridge and Bird Dog, 2 rounds`.
    func groupSubtitle(_ block: ExerciseBlock) -> String? {
        let names = block.cards.map(\.name)
        guard !names.isEmpty else { return nil }
        let list = names.count == 1 ? names[0] : names.dropLast().joined(separator: ", ") + " and " + (names.last ?? "")
        let rounds = block.group?.rounds ?? 0
        return rounds > 0 ? "\(list), \(Format.count(rounds, "round"))" : list
    }

    // MARK: - Units

    /// A unit's lines: a head and its segments, by structure. A drop that
    /// cannot be done yet folds into a `+ drop` tag on its set; once the set
    /// is done the drop hangs off it. A myo-like chain gets a header, its
    /// activation, its done mini-sets as chips and the pending one.
    func unitItems(_ unit: [RowID], isGroup: Bool) -> [RowsItem] {
        guard let head = unit.first.flatMap({ state.rows[$0] }) else { return [] }
        let segments = unit.dropFirst().compactMap { state.rows[$0] }
        if head.side != nil || segments.contains(where: { $0.side != nil }) {
            return ([head] + segments).map { .set(item($0, isGroup: isGroup)) }
        }
        switch head.structure {
        case .straight:
            return [.set(item(head, isGroup: isGroup))]
        case .drop:
            let waiting = segments.count { state.isLocked($0.id) }
            var items: [RowsItem] = [.set(item(head, isGroup: isGroup, waitingDrops: waiting))]
            for segment in segments where !state.isLocked(segment.id) {
                items.append(.set(item(segment, isGroup: isGroup)))
            }
            return items
        case .myo, .myomatch, .restpause, .cluster:
            var items: [RowsItem] = [.chainHeader(chainHeader(head)), .set(item(head, isGroup: isGroup))]
            let done = segments.filter(\.isCompleted)
            if !done.isEmpty { items.append(.miniSets(miniSets(done, head: head, isGroup: isGroup))) }
            for segment in segments where segment.isPending {
                items.append(.set(item(segment, isGroup: isGroup)))
            }
            return items
        }
    }

    /// The warm-ups: one line once all are done, else each.
    func warmupItems(_ units: [[RowID]], id: String, isGroup: Bool) -> [RowsItem] {
        guard !units.isEmpty else { return [] }
        let rows = units.joined().compactMap { state.rows[$0] }
        guard rows.allSatisfy(\.isCompleted) else { return units.flatMap { unitItems($0, isGroup: isGroup) } }
        let sets = rows.map { item($0, isGroup: isGroup) }
        let values = sets.map(\.values)
        let previous = rows.map { evaluator.previous($0.id)?.values }
        var comparison: RowsChange?
        if previous.allSatisfy({ $0 != nil }) {
            let same = zip(rows, previous).allSatisfy { row, last in
                guard let done = row.performed?.values, let last, let exercise = state.exercise(ofRow: row.id) else {
                    return false
                }
                return RowsText.changes(done, last: last, measurement: exercise.measurement,
                                        unit: cards[row.slot]?.unit ?? .kg) == [RowsText.sameAsLastTime]
            }
            let lastTexts = zip(rows, previous).compactMap { row, last in last.map { compactText($0, row: row) } }
            comparison = same ? RowsText.sameAsLastTime
                : RowsChange(text: "last " + lastTexts.joined(separator: ", "), isGain: false)
        } else if previous.allSatisfy({ $0 == nil }) {
            comparison = RowsText.firstTime
        }
        let title = rows.count == 1 ? "Warm-up" : "Warm-ups"
        let spoken = rows.map { spokenValues(of: $0) }.joined(separator: "; ")
        let tail = comparison.map { change in
            ". " + (change == RowsText.sameAsLastTime ? "The same as last time" : change.text.capitalizedFirst)
        } ?? ""
        return [.warmups(RowsWarmupFold(id: "\(id)-warmups", title: title, values: values.joined(separator: ", "),
                                        comparison: comparison, sets: sets, sentence: "\(title) done: \(spoken)\(tail)."))]
    }

    func chainHeader(_ head: Row) -> RowsChainHeader {
        let label = views[head.id]?.label.spoken.capitalizedFirst ?? "Set"
        let pause = RestRules.intraSetPause(head: head).seconds
        let (what, noun): (String, String) = switch head.structure {
        case .myo: ("myo-reps", "mini-sets")
        case .myomatch: ("a myo-rep match", "mini-sets")
        case .restpause: ("rest-pause", "mini-sets")
        case .cluster: (head.target?.miniSetReps.map { "clusters of \($0)" } ?? "clusters", "clusters")
        case .straight, .drop: ("one set", "sets")
        }
        let gap = pause > 0 ? "\(Format.duration(pause)) between \(noun)" : "no rest between \(noun)"
        return RowsChainHeader(id: "\(head.id.rawValue)-chain", head: head.id, text: "\(label) as \(what), \(gap)")
    }

    func miniSets(_ done: [Row], head: Row, isGroup: Bool) -> RowsMiniSets {
        let sets = done.map { item($0, isGroup: isGroup) }
        let chips = done.map { row in
            let values = sets.first { $0.id == row.id }?.values ?? ""
            return RowsChip(row: row.id, text: row.performed?.values.reps.map(String.init) ?? values)
        }
        let previous = done.map { evaluator.previous($0.id)?.values }
        let comparison: RowsChange = if previous.allSatisfy({ $0 == nil }) {
            RowsText.firstTime
        } else if zip(done, previous).allSatisfy({ $0.performed?.values.reps == $1?.reps && $1 != nil }) {
            RowsText.sameAsLastTime
        } else {
            RowsChange(text: "last " + previous.map { $0?.reps.map(String.init) ?? "none" }.joined(separator: ", "),
                       isGain: false)
        }
        let title = head.structure == .cluster ? "Clusters" : "Mini-sets"
        let reps = chips.map(\.text)
        let list = reps.count == 1 ? reps.joined() : reps.dropLast().joined(separator: ", ") + " and " + (reps.last ?? "")
        let stop = done.last.flatMap { views[$0.id]?.stopText }
        let sentence = "\(title) done: \(list) reps. "
            + (comparison == RowsText.sameAsLastTime ? "The same as last time." : "\(comparison.text.capitalizedFirst).")
            + (stop.map { " \($0)." } ?? "")
        return RowsMiniSets(id: "\(head.id.rawValue)-minis", title: title, chips: chips, comparison: comparison, stopText: stop,
                            sets: sets, sentence: sentence)
    }

    // MARK: - Set rows

    func rowState(_ row: Row) -> RowsRowState {
        if row.isCompleted { return .done }
        if row.id == openRow { return .now }
        if state.isLocked(row.id) { return .locked }
        return .next
    }

    func role(_ row: Row, head: Row) -> RowsRowRole {
        if head.role == .warmup { return .warmup }
        if row.isHead { return .working }
        if row.side != nil { return .side }
        switch head.structure {
        case .drop: return .drop
        case .cluster: return .cluster
        case .myo, .myomatch, .restpause, .straight: return .miniSet
        }
    }

    /// The set as said aloud: `Set 1`, `Warm-up 2`, `Activation`, `Drop`, `Mini-set 3`,
    /// `Cluster 2`, `Set 1 left`; in a group after its exercise, `Glute Bridge, set 1`.
    func title(_ row: Row, head: Row, isGroup: Bool) -> String {
        if isGroup {
            let name = p.exerciseName(state.slot(row.slot)?.exercise ?? ExerciseID(""))
            return "\(name), \(views[row.id]?.label.spoken ?? "set")"
        }
        return part(row, head: head) ?? setTitle(row, head: head)
    }

    /// What a line shows beside its label: the exercise in a group (the part
    /// of a chain after it), the part of a chain alone, else nothing, since
    /// the label is the set's number (owner, 2026-11-02).
    func caption(_ row: Row, head: Row, isGroup: Bool) -> String? {
        let part = part(row, head: head)
        guard isGroup else { return part }
        let name = p.exerciseName(state.slot(row.slot)?.exercise ?? ExerciseID(""))
        return part.map { "\(name), \($0.lowercased())" } ?? name
    }

    /// `Set 1`, `Warm-up 2`, `Set 1 left`.
    func setTitle(_ row: Row, head: Row) -> String {
        let base = head.role == .warmup ? warmupTitle(row) : "Set \(state.ordinal(of: row.id) ?? 1)"
        return row.side.map { "\(base) \($0.rawValue)" } ?? base
    }

    /// A chain's part: `Activation`, `Drop`, `Drop 2`, `Mini-set 3`, `Cluster 2`; nil for a
    /// set's head or side, which its label names.
    func part(_ row: Row, head: Row) -> String? {
        guard row.side == nil else { return nil }
        let index = Ordinals.segmentIndex(of: row.id, in: state) ?? 0
        if !row.isHead {
            switch head.structure {
            case .drop: return state.chain(of: head.id).count > 2 ? "Drop \(index)" : "Drop"
            case .cluster: return "Cluster \(index + 1)"
            case .myo, .myomatch, .restpause: return "Mini-set \(index)"
            case .straight: return views[row.id]?.label.spoken.capitalizedFirst
            }
        }
        switch head.structure {
        case .myo, .myomatch, .restpause: return "Activation"
        case .cluster: return "Cluster 1"
        case .straight, .drop: return nil
        }
    }

    /// `Warm-up`, or `Warm-up 2` when the exercise has more than one.
    func warmupTitle(_ row: Row) -> String {
        let ordinal = state.ordinal(of: row.id) ?? 1
        return Ordinals.warmupCount(inSlot: row.slot, of: state) > 1 ? "Warm-up \(ordinal)" : "Warm-up"
    }

    /// The Log button's words before the values.
    func logPrefix(_ row: Row, head: Row) -> String {
        let index = Ordinals.segmentIndex(of: row.id, in: state) ?? 0
        if let side = row.side { return "Log \(side.rawValue) side," }
        if !row.isHead {
            switch head.structure {
            case .drop: return "Log drop"
            case .cluster: return "Log cluster \(index + 1),"
            case .myo, .myomatch, .restpause: return "Log mini-set \(index),"
            case .straight: break
            }
        }
        return head.role == .warmup ? "Log warm-up" : "Log"
    }

    func item(_ row: Row, isGroup: Bool, waitingDrops: Int = 0) -> RowsSetItem {
        let head = state.rows[row.headID] ?? row
        let view = views[row.id]
        let exercise = state.exercise(ofRow: row.id)
        let measurement = exercise?.measurement ?? .weightreps
        let perSide = exercise?.resolvedLaterality == .alternating
        let card = cards[row.slot]
        let unit = card?.unit ?? state.settings.defaultUnit
        let rowState = rowState(row)
        let title = title(row, head: head, isGroup: isGroup)
        let setLabel = view?.label.short ?? ""
        let caption = caption(row, head: head, isGroup: isGroup)
        let ghost = evaluator.ghost(row.id)
        let typedOnlyReps = row.isPending && row.entered.reps == nil
        let placeholder = typedOnlyReps ? ghost.repsPlaceholder : nil
        let values = RowsText.values(evaluator.shown(row.id), measurement: measurement, perSide: perSide,
                                     repsHint: typedOnlyReps && ghost.repsIsHint, placeholder: placeholder)
        let done = row.performed?.values
        let previous = view?.previous?.values

        // A warm-up says so in its orange `W`; no tag repeats it.
        var tags: [RowsTag] = []
        if waitingDrops > 0 {
            tags.append(RowsTag(text: waitingDrops == 1 ? "+ drop" : "+ \(waitingDrops) drops", style: .orange))
        }
        if view?.label.failureBadge == true { tags.append(RowsTag(text: "to failure", style: .red)) }
        if let roleText = view?.label.roleText {
            tags.append(RowsTag(text: roleText == "Top" ? "top set" : "back-off", style: .grey))
        }

        var changes: [RowsChange] = []
        var verdict: RowsVerdict?
        if let done {
            if let previous {
                changes = RowsText.changes(done, last: previous, measurement: measurement, unit: unit)
                verdict = head.role == .warmup ? nil : self.verdict(row, done: done, last: previous, changes: changes)
            } else {
                changes = [RowsText.firstTime]
            }
        }
        let who = title
        let spokenNow = RowsText.spoken(done ?? evaluator.shown(row.id), measurement: measurement, perSide: perSide,
                                        repsHint: done == nil && typedOnlyReps && ghost.repsIsHint, placeholder: placeholder)
        let spokenLast = previous.map { "Last time \(RowsText.spoken($0, measurement: measurement, perSide: perSide))" }
        let sentence: String
        switch rowState {
        case .done:
            let rpe = done?.rpe.map { ", RPE \(Format.rpe($0))" + (done?.rpeMethod == .prefilled ? " as planned" : "") } ?? ""
            let change = RowsText.spokenChanges(changes, unit: unit)
            let mark = verdict.flatMap { changes == [RowsText.sameAsLastTime] && $0 == .same ? nil : ", \($0.spoken)" } ?? ""
            sentence = "\(who), done: \(spokenNow)\(rpe). \(change)\(mark)."
        case .now:
            sentence = "\(who), now: \(spokenNow)." + (spokenLast.map { " \($0)." } ?? "")
        case .next:
            sentence = "\(who), coming up: \(spokenNow)." + (spokenLast.map { " \($0)." } ?? "")
        case .locked:
            let before = state.chain(of: row.id).last { $0.chainPosition < row.chainPosition }
            let after = before.map { self.title($0, head: head, isGroup: isGroup) } ?? "the set before it"
            sentence = "\(who), after \(after): \(spokenNow)."
        }
        let tap: RowsTap = switch rowState {
        case .done: .fix
        case .next: .doNow(.touch(row.id))
        case .now, .locked: .none
        }
        let rpeShown = done?.rpe
        var editor: RowsEditor?
        if rowState == .now, let view {
            let source = editorSource(row, head: head, view: view, mode: .log, title: title, setLabel: setLabel,
                                      caption: caption, isGroup: isGroup)
            let rpe: RowRPE = if case .prefill = view.rpeEntry, !p.settings.prefillsRPE { .none } else { view.rpeEntry }
            let running = p.timedID == row.id
            editor = source.editor(values: row.entered, rpe: rpe, commit: running ? view.stopValues : view.commitValues,
                                   canCommit: view.canTick, isMissing: view.missing)
        }
        var fix: RowsFix?
        if let done, let view {
            let source = editorSource(row, head: head, view: view, mode: .fix, title: title, setLabel: setLabel,
                                      caption: caption, isGroup: isGroup)
            fix = RowsFix(row: row.id, logged: done, source: source)
        }
        return RowsSetItem(
            id: row.id,
            slot: row.slot,
            state: rowState,
            role: role(row, head: head),
            title: title,
            setLabel: setLabel,
            caption: caption,
            isChained: !row.isHead && row.side == nil,
            values: values,
            valuesChanged: row.isPending && changedFromSuggestion(row.entered, ghost: ghost, measurement: measurement),
            rpe: rpeShown.map { "@ \(Format.rpe($0))" },
            rpeIsPrefilled: done?.rpeMethod == .prefilled,
            tags: tags,
            collapsedTag: rowState == .now ? RowsTag(text: "up next", style: .accent) : nil,
            last: row.isPending ? previous.map { "last \(compactText($0, row: row))" } : nil,
            changes: changes,
            verdict: verdict,
            sentence: sentence,
            tap: tap,
            editor: editor,
            fix: fix
        )
    }

    func compactText(_ values: SetValues, row: Row) -> String {
        let exercise = state.exercise(ofRow: row.id)
        return RowsText.compact(values, measurement: exercise?.measurement ?? .weightreps,
                                perSide: exercise?.resolvedLaterality == .alternating)
    }

    func spokenValues(of row: Row) -> String {
        let exercise = state.exercise(ofRow: row.id)
        return RowsText.spoken(row.performed?.values ?? evaluator.shown(row.id),
                               measurement: exercise?.measurement ?? .weightreps,
                               perSide: exercise?.resolvedLaterality == .alternating)
    }

    /// The ghost as values; a `rirstop` set's approximate reps pre-fill as any other.
    func ghostValues(_ ghost: GhostValues) -> SetValues {
        SetValues(load: ghost.load, assistance: ghost.assistance, reps: ghost.reps,
                  duration: ghost.duration, distance: ghost.distance)
    }

    /// A pending row's typed values differ from the suggestion somewhere.
    func changedFromSuggestion(_ entered: SetValues, ghost: GhostValues, measurement: MeasurementType) -> Bool {
        let suggested = ghostValues(ghost)
        return measurement.valueColumns.compactMap(\.field).contains { field in
            FieldAccess.number(field, entered) != nil && FieldAccess.number(field, suggested) != nil
                && FieldAccess.differs(field, entered, suggested)
        }
    }

    /// The verdict (§7.6 Rows): estimated one-rep max today against last time,
    /// adjusted for RPE when both sets carry a rating, else compared at equal
    /// effort; within 1 % is the same. With no estimate (no load, a timed or
    /// distance set), the changes decide when they all point one way.
    func verdict(_ row: Row, done: SetValues, last: SetValues, changes: [RowsChange]) -> RowsVerdict? {
        if let exercise = state.exercise(ofRow: row.id) {
            let config = evaluator.config(exercise)
            let rated = done.ratedRPE != nil && last.ratedRPE != nil
            func estimate(_ values: SetValues) -> Double? {
                var set = SuggestionInputs.doneSet(kind: .normal, values: values, side: nil, config: config, target: Target03(),
                                                   completedAt: p.now, ordinal: nil)
                set.rpe = rated ? values.ratedRPE?.double : 10
                set.unratedRPE = nil
                return config.oneRepMax(of: set, bodyMass: config.bodyMassInUnit(evaluator.bodyMass))?.value
            }
            if let today = estimate(done), let then = estimate(last), then > 0 {
                let ratio = today / then
                if ratio > 1.01 { return .better }
                if ratio < 0.99 { return .below }
                return .same
            }
        }
        if changes == [RowsText.sameAsLastTime] { return .same }
        if changes.allSatisfy(\.isGain) { return .better }
        if changes.allSatisfy({ !$0.isGain }) { return .below }
        return nil
    }

    // MARK: - Editors

    func editorSource(_ row: Row, head: Row, view: SetRowPresentation, mode: RowsEditor.Mode, title: String,
                      setLabel: String, caption: String?, isGroup: Bool) -> RowsEditorSource
    {
        let exercise = state.exercise(ofRow: row.id)
        let measurement = exercise?.measurement ?? .weightreps
        let card = cards[row.slot]
        let unit = card?.unit ?? state.settings.defaultUnit
        let ghost = evaluator.ghost(row.id)
        let role = role(row, head: head)
        let isMini = role == .miniSet || role == .cluster
        // A mini-set or cluster keeps its activation's load: its editor is the reps.
        var fields = measurement.valueColumns.compactMap(\.field)
        if isMini, fields.contains(.reps) { fields.removeAll { $0 == .load || $0 == .assistance } }
        let shown = evaluator.shown(row.id)
        let running = p.timedID == row.id
        let tint: RowsEditor.Tint = mode == .fix ? .neutral : (role == .warmup || role == .drop ? .orange : .accent)
        let pause = RestRules.intraSetPause(head: head).seconds

        var note: RowsNote?
        switch mode {
        case .fix:
            if let at = row.performed?.completedAt {
                note = RowsNote(text: "logged at \(Format.time(at, in: p.settings.timeZone))", kind: .status)
            }
        case .log:
            // No "after the rest": a set waits for nothing, the rest shows in the island.
            let warmups = Ordinals.warmupCount(inSlot: row.slot, of: state)
            if running {
                note = RowsNote(text: measurement.isTimedHold ? "holding" : "timing", kind: .running)
            } else if head.role == .warmup, row.isHead, warmups > 1 {
                note = RowsNote(text: "of \(warmups)", kind: .status)
            } else if role == .drop, ghost.repsPlaceholder != nil {
                note = RowsNote(text: GhostValues.toFailure, kind: .status)
            } else if isMini, let load = Format.load(shown, mode: measurement.loadMode), load != "BW" {
                // The reps field holds no words: the header says "to failure" when the plan does.
                let failure = ghost.repsPlaceholder.map { ", \($0)" } ?? ""
                note = RowsNote(text: "at \(load)\(failure)", kind: .status)
            } else if let text = view.reason {
                note = RowsNote(text: text, kind: .reason)
            }
        }

        var secondary: RowsButton?
        if mode == .log, row.generated, isMini, head.structure.growsOneAtATime {
            let words = switch head.structure {
            case .myomatch: "End the match here"
            case .restpause: "End rest-pause here"
            default: "End myo-reps here"
            }
            secondary = RowsButton(title: words, action: .removeSet(row.id))
        }

        var timer: RowsTimer?
        if mode == .log, measurement.hasTimer {
            let last = view.previous?.values.duration
            var phase = RowsTimer.Phase.ready
            var goal: Seconds?
            if running, let countdown = state.countdown {
                phase = .running(startedAt: countdown.startedAt, endsAt: countdown.endsAt)
                goal = countdown.endsAt.map { Seconds($0.timeIntervalSince(countdown.startedAt).rounded()) }
            } else if case let .ready(target) = view.timer {
                goal = target
            }
            timer = RowsTimer(phase: phase, goal: goal, last: last, trackEnd: RowsTimer.trackEnd(goal: goal, last: last),
                              goalText: goal.map { "goal \(Format.duration($0))" },
                              lastText: last.map { "last \(Format.duration($0))" })
        }

        let previousRated = view.previous?.values.rpe != nil
        let plannedEffort = (row.side != nil && !row.isHead ? head.target : row.target)?.effortTop != nil
        return RowsEditorSource(
            row: row.id,
            mode: mode,
            tint: tint,
            title: title,
            setLabel: setLabel,
            caption: caption,
            note: note,
            trailingNote: mode == .log && role == .drop && pause == 0 ? "no rest" : nil,
            reason: view.suggestion.flatMap { $0.stop ? nil : $0.text },
            spoken: "\(p.exerciseName(state.slot(row.slot)?.exercise ?? ExerciseID(""))), \(view.label.spoken)",
            measurement: measurement,
            unit: unit,
            perSide: exercise?.resolvedLaterality == .alternating,
            fields: fields,
            baseline: mode == .log ? ghostValues(ghost) : row.performed?.values ?? SetValues(),
            repsPlaceholder: mode == .log ? ghost.repsPlaceholder : nil,
            previous: view.previous?.values,
            loadStep: card?.loadStep ?? RowsBuilder.fallbackLoadStep(unit),
            grid: exercise.flatMap(grid),
            timeStep: card?.timeStep ?? exercise?.resolvedTimeStep ?? 5,
            rpeOffered: true,
            rpeOpen: head.role != .warmup && measurement.recordsReps && (previousRated || plannedEffort),
            logPrefix: logPrefix(row, head: head),
            timer: timer,
            secondary: secondary,
            isRunning: running,
            canStart: row.isPending && !state.isLocked(row.id) && p.timedID == nil
        )
    }

    /// The steppers' load step for an exercise with no load grid: 2.5 kg or 5 lb.
    static func fallbackLoadStep(_ unit: MassUnit) -> Mass {
        unit == .kg ? Mass(value: Decimal(25) / 10, unit: .kg) : .lb(5)
    }

    /// The exercise's load grid for the keypad's chips; nil with none.
    func grid(_ exercise: Exercise) -> RowsGrid? {
        let config = evaluator.config(exercise)
        guard config.mode != .none, config.canAddLoad, let step = config.step, step >= 0.01 else { return nil }
        let base = config.base ?? 0
        let lowest = config.mode == .external && base <= 1e-9 ? 1 : 0
        let highest = config.maxLoad.map { max(lowest, Int((($0 - base) / step + 1e-9).rounded(.down))) }
        return RowsGrid(unit: config.unit, base: base, step: step, lowest: lowest, highest: highest)
    }

    // MARK: - Finished blocks

    func summary(_ block: ExerciseBlock, title blockTitle: String) -> RowsBlockSummary {
        let isGroup = block.group != nil
        var texts: [String] = []
        var changes: [RowsChange] = []
        var spoken: [String] = []
        for card in block.cards {
            let text = summaryText(card.slot)
            texts.append(isGroup ? "\(card.name) \(text)" : text)
            let sets = state.orderedRows(inSlot: card.slot).filter { $0.role == .working && $0.isCompleted }.map { row in
                let head = state.rows[row.headID] ?? row
                let lead = row.isHead || row.side != nil ? "" : "\(title(row, head: head, isGroup: false).lowercased()), "
                return lead + spokenValues(of: row)
            }
            spoken.append((isGroup ? "\(card.name): " : "") + sets.joined(separator: "; "))
            changes.append(headline(card.slot))
        }
        // The members' real changes, each once; else as last time, else first time.
        var unique: [RowsChange] = []
        for change in changes where change != RowsText.sameAsLastTime && change != RowsText.firstTime {
            if !unique.contains(where: { $0.text == change.text }) { unique.append(change) }
        }
        let change = unique.isEmpty
            ? (changes.contains(RowsText.sameAsLastTime) ? RowsText.sameAsLastTime : RowsText.firstTime)
            : RowsChange(text: unique.map(\.text).joined(separator: ", "), isGain: unique.allSatisfy(\.isGain))
        let unit = block.cards.first?.unit ?? .kg
        let words = (unique.isEmpty ? [change] : unique).map { RowsText.spokenChanges([$0], unit: unit) }
        let sentence = "\(blockTitle), done: \(spoken.joined(separator: "; ")). \(words.joined(separator: "; "))."
        return RowsBlockSummary(text: texts.joined(separator: "; "), change: change, sentence: sentence)
    }

    /// A slot's done working sets on one line, a repeated load written once:
    /// `110 kg × 8, 7, 8`; `35 kg × 14, drop 25 kg × 12`; `6 kg × 17 + 4 + 4`;
    /// `10 kg × 10 left, 10 right`; reps alone `16, 16` (`per side`); holds `1:02, 0:53`.
    func summaryText(_ slot: String) -> String {
        guard let exercise = state.exercise(ofSlot: slot) else { return "" }
        let measurement = exercise.measurement
        let perSide = exercise.resolvedLaterality == .alternating
        let repsAlone = measurement == .repsonly || measurement == .bodyweightreps
        var parts: [String] = []
        var lastLoad: String?
        func text(_ values: SetValues) -> String {
            switch measurement {
            case .weightreps, .weightedbodyweightreps, .bodyweightassistedreps:
                let load = Format.load(values, mode: measurement.loadMode)
                let reps = values.reps.map(String.init) ?? ""
                defer { lastLoad = load }
                if load == lastLoad || load == "BW" { return reps }
                return load.map { "\($0) \(RowsText.times) \(reps)" } ?? reps
            case .repsonly, .bodyweightreps:
                return values.reps.map(String.init) ?? ""
            case .duration:
                return values.duration.map(Format.duration) ?? ""
            case .weightduration, .distanceduration, .shortdistanceweight:
                return Format.set(values, measurement: measurement)
            }
        }
        for unit in state.units(inSlot: slot) where !isWarmup(unit) {
            let rows = unit.compactMap { state.rows[$0] }.filter(\.isCompleted)
            guard let head = rows.first, let headValues = head.performed?.values else { continue }
            var part = text(headValues)
            if head.side != nil { part += " \(head.side?.rawValue ?? "")" }
            for segment in rows.dropFirst() {
                guard let values = segment.performed?.values else { continue }
                if let side = segment.side {
                    part += ", \(values.reps.map(String.init) ?? text(values)) \(side.rawValue)"
                } else if head.structure == .drop {
                    lastLoad = nil
                    part += ", drop \(text(values))"
                } else {
                    part += " + \(values.reps.map(String.init) ?? text(values))"
                }
            }
            parts.append(part)
        }
        var line = parts.joined(separator: ", ")
        if repsAlone, perSide, !line.isEmpty { line += " per side" }
        return line
    }

    /// The headline change of a slot against last time: the heaviest load,
    /// else the best reps (or the longest hold); `as last time`, `first time`.
    func headline(_ slot: String) -> RowsChange {
        guard let exercise = state.exercise(ofSlot: slot) else { return RowsText.firstTime }
        let unit = cards[slot]?.unit ?? state.settings.defaultUnit
        let heads = state.orderedRows(inSlot: slot).filter { row in
            row.role == .working && row.isCompleted && (row.isHead || row.side != nil)
        }
        let pairs = heads.compactMap { row -> (SetValues, SetValues)? in
            guard let done = row.performed?.values, let last = evaluator.previous(row.id)?.values else { return nil }
            return (done, last)
        }
        guard !pairs.isEmpty else { return RowsText.firstTime }
        let measurement = exercise.measurement
        let loadField: ValueField? = measurement.loadMode == .assisted ? .assistance
            : measurement.valueColumns.contains(where: { $0.field == .load }) ? .load : nil
        if let loadField {
            func top(_ values: [SetValues]) -> Decimal? {
                values.compactMap { values -> Decimal? in
                    let mass = loadField == .load ? values.load : values.assistance
                    return mass?.converted(to: unit).value
                }.max()
            }
            if let now = top(pairs.map(\.0)), let then = top(pairs.map(\.1)) {
                let diff = (now - then).roundedTo(2)
                if diff != 0 {
                    let gain = loadField == .assistance ? diff < 0 : diff > 0
                    let text = RowsText.signed(Format.number(abs(diff)), diff > 0) + " \(unit.rawValue)"
                    return RowsChange(text: text, isGain: gain)
                }
            }
        }
        if measurement.recordsReps, let now = pairs.compactMap(\.0.reps).max(), let then = pairs.compactMap(\.1.reps).max(),
           now != then
        {
            let diff = now - then
            return RowsChange(text: RowsText.signed("\(abs(diff))", diff > 0) + (abs(diff) == 1 ? " rep" : " reps"),
                              isGain: diff > 0)
        }
        if measurement.isTimedHold, let now = pairs.compactMap(\.0.duration).max(),
           let then = pairs.compactMap(\.1.duration).max(), now != then
        {
            return RowsChange(text: RowsText.signed(Format.duration(abs(now - then)), now > then), isGain: now > then)
        }
        return RowsText.sameAsLastTime
    }

    // MARK: - The island

    /// The island's parts: the rest while one runs, the open set compact, and
    /// the set after it. Nothing counts up: there is no "is up" moment after a
    /// Log, the next set simply shows.
    func island(open item: RowsSetItem?, blocks: [RowsBlock]) -> RowsIsland {
        if state.rows.isEmpty { return .empty }
        var rest: RowsRestPart?
        if p.timedID == nil, let countdown = state.countdown, countdown.isRestLike, let end = countdown.endsAt, end > p.now {
            rest = restPart(countdown, endsAt: end)
        }
        guard let open = openRow else {
            return RowsIsland(rest: rest, next: nil, after: nil,
                              end: .allDone(title: "All sets done", detail: "Finish saves the session"))
        }
        return RowsIsland(rest: rest, next: item.flatMap(nextPart), after: afterPart(open, blocks: blocks), end: nil)
    }

    /// The open set, compact: its exercise and label, the editor's fields, the
    /// RPE the Log commits, and the primary action in a word.
    func nextPart(_ item: RowsSetItem) -> RowsNextPart? {
        guard let editor = item.editor, let row = state.rows[item.id], let slot = state.slot(row.slot) else { return nil }
        let exercise = p.exerciseName(slot.exercise)
        // The label says the set; words follow only where they help: the
        // round in a group, a chain's part.
        let head = state.rows[row.headID] ?? row
        var label = part(row, head: head) ?? ""
        if label.isEmpty, slot.group != nil, item.role != .warmup {
            label = "Round \(max(1, UpNext.round(of: row.id, in: state)))"
        }
        let word: String = switch editor.primary.action {
        case .startTimedSet: "Start"
        case .stopTimedSet: "Stop"
        default: "Log"
        }
        var startedAt: Date?
        if case let .running(start, _)? = editor.timer?.phase { startedAt = start }
        return RowsNextPart(
            row: item.id, exercise: exercise, setLabel: item.setLabel, isWarmup: item.role == .warmup, label: label,
            tint: editor.tint, fields: editor.fields,
            rpe: editor.rpe?.shown.map { "@ \(Format.rpe($0))" },
            rpeStrip: editor.rpe,
            primary: RowsButton(title: word, accessibilityLabel: editor.primary.accessibilityLabel,
                                action: editor.primary.action, isEnabled: editor.primary.isEnabled),
            timerStartedAt: startedAt,
            sentence: "Next: \(exercise). \(item.sentence)",
            loggedText: editor.loggedText
        )
    }

    /// The set a Log of the open one would lead to, on one line.
    func afterPart(_ open: RowID, blocks: [RowsBlock]) -> RowsAfterPart? {
        let after = afterRow(open)
        guard let next = after.next, next != open else { return nil }
        let text = nextText(next, from: open, in: after.state).capitalizedFirst
        return RowsAfterPart(row: lineShowing(next, in: blocks), label: "Then", text: text, sentence: "Then \(text)")
    }

    /// The line a tap on "Then" takes the list to: the set's own, or for a
    /// drop that waits on its set (drawn as `+ drop` on it) the set's line.
    /// Nil when the set does not exist yet.
    func lineShowing(_ id: RowID, in blocks: [RowsBlock]) -> RowID? {
        guard let row = state.rows[id] else { return nil }
        for candidate in [id, row.headID] where blocks.contains(where: { $0.item(candidate) != nil }) {
            return candidate
        }
        return nil
    }

    func restPart(_ countdown: Countdown, endsAt end: Date) -> RowsRestPart {
        let title: String = switch countdown.kind {
        case .manual, .timedSet: "Timer"
        case let .rest(slot): slot.label
        }
        let total = Seconds(end.timeIntervalSince(countdown.startedAt).rounded())
        return RowsRestPart(
            kind: p.activityKind(countdown), title: title, startedAt: countdown.startedAt, endsAt: end,
            totalText: "of \(Format.duration(total))",
            minus: RowsButton(title: "\u{2212}15", accessibilityLabel: "15 seconds less", action: .adjustRest(-15)),
            plus: RowsButton(title: "+15", accessibilityLabel: "15 seconds more", action: .adjustRest(15)),
            skip: RowsButton(title: "Skip", accessibilityLabel: "Skip the rest", action: .skipRest)
        )
    }

    /// What a tick of `id` would lead to, as if it were done now with what it
    /// shows: the row expected after it (§4.6 rule 2), with the engine's own
    /// chain step (a myo chain's next mini-set appears only on a tick).
    /// `state` is that previewed session.
    func afterRow(_ id: RowID) -> (next: RowID?, state: SessionState) {
        var ticked = state
        ticked.rows[id]?.performed = Performed(recordID: SetID("rows-preview"), completedAt: p.now, values: evaluator.commit(id))
        ticked.focus = nil
        var ids = RowIDPool([], prefix: "rows-preview")
        try? ticked.afterTick(id, noLighterLoad: false, ids: &ids)
        return (UpNext.expectedNext(after: id, in: ticked), ticked)
    }

    /// The row after the open one in words with its values: `warm-up 2, 70 kg × 6`,
    /// `Glute Bridge, round 2, 16 reps`, `mini-set 2` (not made yet, so no values).
    func nextText(_ next: RowID, from anchor: RowID, in preview: SessionState) -> String {
        let what = describe(next, from: anchor, in: preview)
        guard state.rows[next] != nil else { return what }
        let values = valuesText(next)
        return values.isEmpty ? what : "\(what), \(values)"
    }

    /// A row in words, lower-case first, as seen from `anchor`: `mini-set 3`
    /// or `drop 1` in the anchor's chain, `set 3` in its exercise, `Bird
    /// Dog` in a group (`, round 2` when the round changes), else the exercise
    /// and the label: `Chest Press (Machine), warm-up`. `preview` is a state a
    /// tick was previewed in, which may hold a row the session does not have yet.
    func describe(_ id: RowID, from anchor: RowID?, in preview: SessionState? = nil) -> String {
        let s = preview ?? state
        guard let row = s.rows[id], let slot = s.slot(row.slot) else { return "the next set" }
        let label = (state.rows[id] != nil ? views[id]?.label : nil) ?? SetLabel.make(id, in: s)
        let spoken = label?.spoken ?? "set"
        let part = spoken.split(separator: ",").dropFirst().joined(separator: ",").trimmingCharacters(in: .whitespaces)
        let anchorRow = anchor.flatMap { s.rows[$0] }
        if let anchorRow, anchorRow.headID == row.headID, !row.isHead, !part.isEmpty { return part }
        let name = p.exerciseName(slot.exercise)
        if slot.group != nil {
            var text = name
            if !row.isHead, !part.isEmpty { text += ", \(part)" }
            let round = UpNext.round(of: id, in: s)
            if round > 0, let anchor, anchorRow != nil, UpNext.round(of: anchor, in: s) != round {
                text += ", round \(round)"
            }
            return text
        }
        if anchorRow?.slot == row.slot { return spoken }
        return "\(name), \(spoken)"
    }

    /// The values a row shows, as its line does.
    func valuesText(_ id: RowID) -> String {
        guard let row = state.rows[id], let exercise = state.exercise(ofRow: id) else { return "" }
        let ghost = evaluator.ghost(id)
        let typedOnlyReps = row.isPending && row.entered.reps == nil
        return RowsText.values(evaluator.shown(id), measurement: exercise.measurement,
                               perSide: exercise.resolvedLaterality == .alternating,
                               repsHint: typedOnlyReps && ghost.repsIsHint,
                               placeholder: typedOnlyReps ? ghost.repsPlaceholder : nil)
    }
}
